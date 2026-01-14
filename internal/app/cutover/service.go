// Package cutover provides the application layer for cutover orchestration.
package cutover

import (
	"context"
	"fmt"
	gosync "sync"
	"time"

	"github.com/google/uuid"
	domaincutover "github.com/homeport/homeport/internal/domain/cutover"
	infracutover "github.com/homeport/homeport/internal/infrastructure/cutover"
)

// Service orchestrates cutover operations.
type Service struct {
	orchestrator *infracutover.Orchestrator
	plans        map[string]*CutoverExecution
	mu           gosync.RWMutex
}

// CutoverExecution tracks the execution state of a cutover.
type CutoverExecution struct {
	Plan      *domaincutover.CutoverPlan
	StartedAt *time.Time
	Logs      []string
	cancel    context.CancelFunc
}

// NewService creates a new cutover service.
func NewService() *Service {
	return &Service{
		orchestrator: infracutover.NewOrchestrator(),
		plans:        make(map[string]*CutoverExecution),
	}
}

// CreatePlanRequest contains the data needed to create a cutover plan.
type CreatePlanRequest struct {
	BundleID    string                        `json:"bundle_id"`
	Name        string                        `json:"name"`
	PreChecks   []*domaincutover.HealthCheck  `json:"pre_checks"`
	DNSChanges  []*domaincutover.DNSChange    `json:"dns_changes"`
	PostChecks  []*domaincutover.HealthCheck  `json:"post_checks"`
	DryRun      bool                          `json:"dry_run"`
	DNSProvider string                        `json:"dns_provider"`
}

// CreatePlan creates a new cutover plan.
func (s *Service) CreatePlan(req *CreatePlanRequest) (*domaincutover.CutoverPlan, error) {
	planID := uuid.New().String()[:8]
	plan := domaincutover.NewCutoverPlan(planID, req.BundleID)
	plan.Name = req.Name
	plan.DryRun = req.DryRun

	for _, check := range req.PreChecks {
		plan.AddPreCheck(check)
	}

	for _, change := range req.DNSChanges {
		plan.AddDNSChange(change)
	}

	for _, check := range req.PostChecks {
		plan.AddPostCheck(check)
	}

	// Build the execution steps
	plan.BuildSteps()

	s.mu.Lock()
	s.plans[planID] = &CutoverExecution{
		Plan: plan,
		Logs: make([]string, 0),
	}
	s.mu.Unlock()

	return plan, nil
}

// GetPlan returns a cutover plan by ID.
func (s *Service) GetPlan(planID string) (*CutoverExecution, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	exec, ok := s.plans[planID]
	if !ok {
		return nil, fmt.Errorf("cutover plan not found: %s", planID)
	}
	return exec, nil
}

// CutoverCallback is called when a cutover event occurs.
type CutoverCallback func(event CutoverEvent)

// CutoverEvent represents a cutover progress event.
type CutoverEvent struct {
	Type        string `json:"type"` // "step_start", "step_complete", "step_failed", "complete", "rollback", "error"
	PlanID      string `json:"plan_id"`
	StepIndex   int    `json:"step_index,omitempty"`
	StepType    string `json:"step_type,omitempty"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status,omitempty"`
	Error       string `json:"error,omitempty"`
	Message     string `json:"message,omitempty"`
}

// Execute starts executing a cutover plan.
func (s *Service) Execute(ctx context.Context, planID string, callback CutoverCallback) error {
	s.mu.Lock()
	exec, ok := s.plans[planID]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("cutover plan not found: %s", planID)
	}

	if exec.Plan.Status == domaincutover.CutoverStatusRunning {
		s.mu.Unlock()
		return fmt.Errorf("cutover already running")
	}

	ctx, cancel := context.WithCancel(ctx)
	exec.cancel = cancel
	now := time.Now()
	exec.StartedAt = &now
	exec.Plan.Status = domaincutover.CutoverStatusRunning
	s.mu.Unlock()

	// Run cutover in background
	go s.executePlan(ctx, exec, callback)

	return nil
}

// executePlan runs the cutover steps.
func (s *Service) executePlan(ctx context.Context, exec *CutoverExecution, callback CutoverCallback) {
	plan := exec.Plan

	addLog := func(msg string) {
		s.mu.Lock()
		exec.Logs = append(exec.Logs, fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), msg))
		s.mu.Unlock()
	}

	for i, step := range plan.Steps {
		select {
		case <-ctx.Done():
			s.mu.Lock()
			plan.Status = domaincutover.CutoverStatusFailed
			plan.Error = "cancelled"
			s.mu.Unlock()
			return
		default:
		}

		plan.CurrentStepIndex = i
		step.Status = domaincutover.CutoverStepStatusRunning
		now := time.Now()
		step.StartedAt = &now

		addLog(fmt.Sprintf("Starting step %d: %s", i+1, step.Description))

		if callback != nil {
			callback(CutoverEvent{
				Type:        "step_start",
				PlanID:      plan.ID,
				StepIndex:   i,
				StepType:    string(step.Type),
				Description: step.Description,
				Status:      "running",
			})
		}

		// Execute step based on type
		var stepErr error
		switch step.Type {
		case domaincutover.CutoverStepTypePreCheck, domaincutover.CutoverStepTypePostCheck:
			// Find the health check
			var check *domaincutover.HealthCheck
			if step.Type == domaincutover.CutoverStepTypePreCheck {
				for _, c := range plan.PreChecks {
					if c.ID == step.ReferenceID {
						check = c
						break
					}
				}
			} else {
				for _, c := range plan.PostChecks {
					if c.ID == step.ReferenceID {
						check = c
						break
					}
				}
			}
			if check != nil {
				stepErr = s.executeHealthCheck(ctx, check, plan.DryRun)
			}

		case domaincutover.CutoverStepTypeDNSChange:
			// Find the DNS change
			for _, change := range plan.DNSChanges {
				if change.ID == step.ReferenceID {
					stepErr = s.executeDNSChange(ctx, change, plan.DryRun)
					break
				}
			}
		}

		endTime := time.Now()
		step.CompletedAt = &endTime
		step.Duration = endTime.Sub(*step.StartedAt)

		if stepErr != nil {
			step.Status = domaincutover.CutoverStepStatusFailed
			step.Error = stepErr.Error()
			addLog(fmt.Sprintf("Step %d failed: %s", i+1, stepErr))

			if callback != nil {
				callback(CutoverEvent{
					Type:        "step_failed",
					PlanID:      plan.ID,
					StepIndex:   i,
					StepType:    string(step.Type),
					Description: step.Description,
					Status:      "failed",
					Error:       stepErr.Error(),
				})
			}

			// Check if we should rollback (only for post-checks)
			if step.Type == domaincutover.CutoverStepTypePostCheck {
				addLog("Post-check failed, initiating rollback...")
				s.rollback(ctx, exec, callback)
				return
			}

			// Pre-check failure stops execution
			if step.Type == domaincutover.CutoverStepTypePreCheck {
				s.mu.Lock()
				plan.Status = domaincutover.CutoverStatusFailed
				plan.Error = fmt.Sprintf("Pre-check failed: %s", stepErr)
				s.mu.Unlock()
				return
			}
		} else {
			step.Status = domaincutover.CutoverStepStatusCompleted
			addLog(fmt.Sprintf("Step %d completed", i+1))

			if callback != nil {
				callback(CutoverEvent{
					Type:        "step_complete",
					PlanID:      plan.ID,
					StepIndex:   i,
					StepType:    string(step.Type),
					Description: step.Description,
					Status:      "completed",
				})
			}
		}

		// Wait for DNS propagation after DNS changes
		if step.Type == domaincutover.CutoverStepTypeDNSChange {
			// Check if this is the last DNS change
			isLastDNS := true
			for j := i + 1; j < len(plan.Steps); j++ {
				if plan.Steps[j].Type == domaincutover.CutoverStepTypeDNSChange {
					isLastDNS = false
					break
				}
			}
			if isLastDNS && plan.DNSPropagationWait > 0 {
				addLog(fmt.Sprintf("Waiting %s for DNS propagation...", plan.DNSPropagationWait))
				select {
				case <-time.After(plan.DNSPropagationWait):
				case <-ctx.Done():
					return
				}
			}
		}
	}

	// Mark as completed
	s.mu.Lock()
	plan.Status = domaincutover.CutoverStatusCompleted
	now := time.Now()
	plan.CompletedAt = &now
	s.mu.Unlock()

	addLog("Cutover completed successfully!")

	if callback != nil {
		callback(CutoverEvent{
			Type:    "complete",
			PlanID:  plan.ID,
			Status:  "completed",
			Message: "Cutover completed successfully",
		})
	}
}

func (s *Service) executeHealthCheck(ctx context.Context, check *domaincutover.HealthCheck, dryRun bool) error {
	if dryRun {
		// Simulate check
		time.Sleep(500 * time.Millisecond)
		return nil
	}

	// Use the infrastructure health checker
	checker := infracutover.NewHealthChecker()
	result := checker.Execute(ctx, check)
	if !result.Passed {
		return fmt.Errorf("health check failed: %s", result.Error)
	}
	return nil
}

func (s *Service) executeDNSChange(ctx context.Context, change *domaincutover.DNSChange, dryRun bool) error {
	if dryRun {
		// Simulate DNS change
		time.Sleep(300 * time.Millisecond)
		return nil
	}

	// For now, mark as applied (actual DNS provider integration would go here)
	change.Status = domaincutover.DNSChangeStatusApplied
	now := time.Now()
	change.AppliedAt = &now
	return nil
}

func (s *Service) rollback(ctx context.Context, exec *CutoverExecution, callback CutoverCallback) {
	plan := exec.Plan

	addLog := func(msg string) {
		s.mu.Lock()
		exec.Logs = append(exec.Logs, fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), msg))
		s.mu.Unlock()
	}

	addLog("Starting rollback...")

	if callback != nil {
		callback(CutoverEvent{
			Type:    "rollback",
			PlanID:  plan.ID,
			Status:  "rolling_back",
			Message: "Rolling back DNS changes",
		})
	}

	// Rollback DNS changes in reverse order
	for i := len(plan.DNSChanges) - 1; i >= 0; i-- {
		change := plan.DNSChanges[i]
		if change.IsApplied() {
			addLog(fmt.Sprintf("Reverting DNS change for %s", change.Domain))
			// Revert by swapping old and new values
			oldNew := change.NewValue
			change.NewValue = change.OldValue
			change.OldValue = oldNew

			if err := s.executeDNSChange(ctx, change, plan.DryRun); err != nil {
				addLog(fmt.Sprintf("Failed to revert DNS change: %s", err))
			} else {
				addLog(fmt.Sprintf("Reverted DNS change for %s", change.Domain))
			}
		}
	}

	s.mu.Lock()
	plan.Status = domaincutover.CutoverStatusRolledBack
	now := time.Now()
	plan.RolledBackAt = &now
	s.mu.Unlock()

	addLog("Rollback completed")

	if callback != nil {
		callback(CutoverEvent{
			Type:    "complete",
			PlanID:  plan.ID,
			Status:  "rolled_back",
			Message: "Rollback completed",
		})
	}
}

// Cancel cancels a running cutover.
func (s *Service) Cancel(planID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	exec, ok := s.plans[planID]
	if !ok {
		return fmt.Errorf("cutover plan not found: %s", planID)
	}

	if exec.cancel != nil {
		exec.cancel()
	}
	exec.Plan.Status = domaincutover.CutoverStatusFailed
	exec.Plan.Error = "cancelled by user"
	return nil
}

// Rollback manually triggers a rollback for a completed cutover.
func (s *Service) Rollback(ctx context.Context, planID string, callback CutoverCallback) error {
	s.mu.Lock()
	exec, ok := s.plans[planID]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("cutover plan not found: %s", planID)
	}

	if exec.Plan.Status != domaincutover.CutoverStatusCompleted {
		s.mu.Unlock()
		return fmt.Errorf("can only rollback completed cutovers")
	}
	s.mu.Unlock()

	go s.rollback(ctx, exec, callback)
	return nil
}
