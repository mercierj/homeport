// Package cutover implements the cutover orchestrator for managing
// migration cutover operations including DNS changes, health checks,
// and automatic rollback.
package cutover

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/homeport/homeport/internal/domain/cutover"
)

// Orchestrator coordinates cutover execution including pre-checks,
// DNS changes, post-checks, and rollback operations.
type Orchestrator struct {
	// dnsProviders maps provider names to implementations.
	dnsProviders map[string]cutover.DNSProvider

	// healthChecker executes health checks.
	healthChecker *HealthChecker

	// mu protects concurrent access.
	mu sync.RWMutex
}

// OrchestratorOptions configures the orchestrator behavior.
type OrchestratorOptions struct {
	// DryRun simulates operations without making changes.
	DryRun bool

	// DNSProvider specifies which DNS provider to use.
	DNSProvider string

	// DNSConfig contains provider-specific configuration.
	DNSConfig *cutover.DNSProviderConfig

	// Manual mode generates instructions instead of executing.
	Manual bool

	// Timeout is the maximum time for the entire cutover.
	Timeout time.Duration

	// Verbose enables detailed output.
	Verbose bool

	// OnStepStart is called when a step starts.
	OnStepStart func(step *cutover.CutoverStep)

	// OnStepComplete is called when a step completes.
	OnStepComplete func(step *cutover.CutoverStep)

	// OnProgress is called with progress updates.
	OnProgress func(current, total int, message string)
}

// DefaultOptions returns default orchestrator options.
func DefaultOptions() *OrchestratorOptions {
	return &OrchestratorOptions{
		DryRun:      false,
		DNSProvider: "manual",
		Manual:      false,
		Timeout:     30 * time.Minute,
		Verbose:     false,
	}
}

// ExecutionResult contains the results of a cutover execution.
type ExecutionResult struct {
	// Plan is the executed cutover plan.
	Plan *cutover.CutoverPlan

	// Success indicates if the cutover completed successfully.
	Success bool

	// StepsCompleted is the number of steps completed.
	StepsCompleted int

	// StepsFailed is the number of steps that failed.
	StepsFailed int

	// Duration is how long the execution took.
	Duration time.Duration

	// Error is the error if execution failed.
	Error error

	// RolledBack indicates if a rollback was performed.
	RolledBack bool

	// Logs contains execution logs.
	Logs []string

	// ManualInstructions contains manual steps (if manual mode).
	ManualInstructions []string
}

// NewOrchestrator creates a new cutover orchestrator.
func NewOrchestrator() *Orchestrator {
	return &Orchestrator{
		dnsProviders:  make(map[string]cutover.DNSProvider),
		healthChecker: NewHealthChecker(),
	}
}

// RegisterDNSProvider registers a DNS provider implementation.
func (o *Orchestrator) RegisterDNSProvider(name string, provider cutover.DNSProvider) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.dnsProviders[name] = provider
}

// GetDNSProvider returns a DNS provider by name.
func (o *Orchestrator) GetDNSProvider(name string) (cutover.DNSProvider, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	provider, ok := o.dnsProviders[name]
	return provider, ok
}

// Execute runs the cutover plan.
func (o *Orchestrator) Execute(ctx context.Context, plan *cutover.CutoverPlan, opts *OrchestratorOptions) (*ExecutionResult, error) {
	if opts == nil {
		opts = DefaultOptions()
	}

	result := &ExecutionResult{
		Plan: plan,
		Logs: make([]string, 0),
	}

	startTime := time.Now()
	defer func() {
		result.Duration = time.Since(startTime)
	}()

	// Validate the plan
	if errs := plan.Validate(); len(errs) > 0 {
		result.Error = fmt.Errorf("plan validation failed: %v", errs)
		return result, result.Error
	}

	// Build steps if not already built
	if len(plan.Steps) == 0 {
		plan.BuildSteps()
	}

	// Handle manual mode
	if opts.Manual {
		return o.generateManualInstructions(plan, result)
	}

	// Set up timeout
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = plan.Timeout
	}
	if timeout == 0 {
		timeout = 30 * time.Minute
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Mark plan as running
	now := time.Now()
	plan.Status = cutover.CutoverStatusRunning
	plan.ExecutedAt = &now
	plan.DryRun = opts.DryRun

	// Execute steps
	for i, step := range plan.Steps {
		select {
		case <-ctx.Done():
			result.Error = ctx.Err()
			plan.Status = cutover.CutoverStatusFailed
			return result, result.Error
		default:
		}

		plan.CurrentStepIndex = i

		if opts.OnStepStart != nil {
			opts.OnStepStart(step)
		}

		if opts.OnProgress != nil {
			opts.OnProgress(i+1, len(plan.Steps), step.Description)
		}

		err := o.executeStep(ctx, plan, step, opts)

		if opts.OnStepComplete != nil {
			opts.OnStepComplete(step)
		}

		if err != nil {
			step.Status = cutover.CutoverStepStatusFailed
			step.Error = err.Error()
			result.StepsFailed++
			result.Logs = append(result.Logs, fmt.Sprintf("Step %d failed: %s - %v", i+1, step.Description, err))

			// Check if we should rollback
			if o.shouldRollback(plan, step) && !opts.DryRun {
				result.Logs = append(result.Logs, "Initiating rollback...")
				if rollbackErr := o.Rollback(ctx, plan, opts); rollbackErr != nil {
					result.Logs = append(result.Logs, fmt.Sprintf("Rollback failed: %v", rollbackErr))
				} else {
					result.RolledBack = true
					result.Logs = append(result.Logs, "Rollback completed")
				}
			}

			result.Error = err
			plan.Status = cutover.CutoverStatusFailed
			plan.Error = err.Error()
			return result, err
		}

		step.Status = cutover.CutoverStepStatusCompleted
		result.StepsCompleted++
		result.Logs = append(result.Logs, fmt.Sprintf("Step %d completed: %s", i+1, step.Description))
	}

	// Mark plan as completed
	completedAt := time.Now()
	plan.Status = cutover.CutoverStatusCompleted
	plan.CompletedAt = &completedAt
	result.Success = true

	return result, nil
}

// executeStep executes a single cutover step.
func (o *Orchestrator) executeStep(ctx context.Context, plan *cutover.CutoverPlan, step *cutover.CutoverStep, opts *OrchestratorOptions) error {
	startTime := time.Now()
	step.Status = cutover.CutoverStepStatusRunning
	step.StartedAt = &startTime

	defer func() {
		completedAt := time.Now()
		step.CompletedAt = &completedAt
		step.Duration = time.Since(startTime)
	}()

	switch step.Type {
	case cutover.CutoverStepTypePreCheck:
		return o.executeHealthCheck(ctx, plan, step, true, opts)
	case cutover.CutoverStepTypeDNSChange:
		return o.executeDNSChange(ctx, plan, step, opts)
	case cutover.CutoverStepTypePostCheck:
		return o.executeHealthCheck(ctx, plan, step, false, opts)
	default:
		return fmt.Errorf("unknown step type: %s", step.Type)
	}
}

// executeHealthCheck runs a health check step.
func (o *Orchestrator) executeHealthCheck(ctx context.Context, plan *cutover.CutoverPlan, step *cutover.CutoverStep, isPreCheck bool, opts *OrchestratorOptions) error {
	// Find the health check
	var check *cutover.HealthCheck
	checks := plan.PreChecks
	if !isPreCheck {
		checks = plan.PostChecks
	}

	for _, c := range checks {
		if c.ID == step.ReferenceID {
			check = c
			break
		}
	}

	if check == nil {
		return fmt.Errorf("health check not found: %s", step.ReferenceID)
	}

	if opts.DryRun {
		step.Output = fmt.Sprintf("[DRY RUN] Would execute %s health check: %s", check.Type, check.Endpoint)
		return nil
	}

	result := o.healthChecker.Execute(ctx, check)
	step.Output = result.Response
	if result.Error != "" {
		step.Output = result.Error
	}

	if !result.Passed {
		return fmt.Errorf("health check failed: %s", result.Error)
	}

	return nil
}

// executeDNSChange applies a DNS change.
func (o *Orchestrator) executeDNSChange(ctx context.Context, plan *cutover.CutoverPlan, step *cutover.CutoverStep, opts *OrchestratorOptions) error {
	// Find the DNS change
	var change *cutover.DNSChange
	for _, c := range plan.DNSChanges {
		if c.ID == step.ReferenceID {
			change = c
			break
		}
	}

	if change == nil {
		return fmt.Errorf("DNS change not found: %s", step.ReferenceID)
	}

	if opts.DryRun {
		step.Output = fmt.Sprintf("[DRY RUN] Would change %s record %s from %s to %s",
			change.RecordType, change.FullName(), change.OldValue, change.NewValue)
		return nil
	}

	// Get DNS provider
	providerName := opts.DNSProvider
	if providerName == "" {
		providerName = change.Provider
	}
	if providerName == "" || providerName == "manual" {
		// For manual provider, just mark as applied (user handles it)
		change.Status = cutover.DNSChangeStatusApplied
		now := time.Now()
		change.AppliedAt = &now
		step.Output = fmt.Sprintf("DNS change marked as applied (manual): %s -> %s", change.FullName(), change.NewValue)
		return nil
	}

	provider, ok := o.GetDNSProvider(providerName)
	if !ok {
		return fmt.Errorf("DNS provider not found: %s", providerName)
	}

	// Apply the change
	if err := provider.UpdateRecord(ctx, change); err != nil {
		change.Status = cutover.DNSChangeStatusFailed
		change.Error = err.Error()
		return fmt.Errorf("failed to apply DNS change: %w", err)
	}

	change.Status = cutover.DNSChangeStatusApplied
	now := time.Now()
	change.AppliedAt = &now
	step.Output = fmt.Sprintf("DNS change applied: %s -> %s", change.FullName(), change.NewValue)

	return nil
}

// shouldRollback determines if a rollback should be triggered.
func (o *Orchestrator) shouldRollback(plan *cutover.CutoverPlan, failedStep *cutover.CutoverStep) bool {
	// Check if any rollback triggers apply
	for _, trigger := range plan.RollbackTriggers {
		if !trigger.Enabled || !trigger.AutoRollback {
			continue
		}

		switch trigger.ConditionType {
		case cutover.RollbackConditionHealthCheck:
			if failedStep.Type == cutover.CutoverStepTypePostCheck {
				return true
			}
		case cutover.RollbackConditionTimeout:
			// Handled by context timeout
			return true
		}
	}

	// Default: rollback on post-check failure
	return failedStep.Type == cutover.CutoverStepTypePostCheck
}

// Rollback reverts all applied DNS changes.
func (o *Orchestrator) Rollback(ctx context.Context, plan *cutover.CutoverPlan, opts *OrchestratorOptions) error {
	if opts == nil {
		opts = DefaultOptions()
	}

	// Revert DNS changes in reverse order
	for i := len(plan.DNSChanges) - 1; i >= 0; i-- {
		change := plan.DNSChanges[i]
		if !change.CanRollback() {
			continue
		}

		if opts.DryRun {
			continue
		}

		// Create reverse change
		reverseChange := &cutover.DNSChange{
			ID:               change.ID + "-rollback",
			Domain:           change.Domain,
			RecordType:       change.RecordType,
			Name:             change.Name,
			OldValue:         change.NewValue,
			NewValue:         change.OldValue,
			TTL:              change.TTL,
			Provider:         change.Provider,
			ProviderRecordID: change.ProviderRecordID,
			ProxyEnabled:     change.ProxyEnabled,
		}

		providerName := opts.DNSProvider
		if providerName == "" {
			providerName = change.Provider
		}
		if providerName == "" || providerName == "manual" {
			change.Status = cutover.DNSChangeStatusRolledBack
			now := time.Now()
			change.RolledBackAt = &now
			continue
		}

		provider, ok := o.GetDNSProvider(providerName)
		if !ok {
			return fmt.Errorf("DNS provider not found for rollback: %s", providerName)
		}

		if err := provider.UpdateRecord(ctx, reverseChange); err != nil {
			return fmt.Errorf("failed to rollback DNS change %s: %w", change.FullName(), err)
		}

		change.Status = cutover.DNSChangeStatusRolledBack
		now := time.Now()
		change.RolledBackAt = &now
	}

	// Mark plan as rolled back
	plan.Status = cutover.CutoverStatusRolledBack
	now := time.Now()
	plan.RolledBackAt = &now

	return nil
}

// generateManualInstructions creates manual cutover instructions.
func (o *Orchestrator) generateManualInstructions(plan *cutover.CutoverPlan, result *ExecutionResult) (*ExecutionResult, error) {
	instructions := make([]string, 0)

	instructions = append(instructions, "=== MANUAL CUTOVER INSTRUCTIONS ===")
	instructions = append(instructions, "")

	// Pre-checks
	if len(plan.PreChecks) > 0 {
		instructions = append(instructions, "## Pre-Cutover Checks")
		instructions = append(instructions, "")
		for i, check := range plan.PreChecks {
			instructions = append(instructions, fmt.Sprintf("%d. %s", i+1, check.Name))
			instructions = append(instructions, fmt.Sprintf("   Type: %s", check.Type))
			instructions = append(instructions, fmt.Sprintf("   Endpoint: %s", check.Endpoint))
			if check.ExpectedStatus > 0 {
				instructions = append(instructions, fmt.Sprintf("   Expected Status: %d", check.ExpectedStatus))
			}
			instructions = append(instructions, "")
		}
	}

	// DNS changes
	if len(plan.DNSChanges) > 0 {
		instructions = append(instructions, "## DNS Changes")
		instructions = append(instructions, "")
		for i, change := range plan.DNSChanges {
			instructions = append(instructions, fmt.Sprintf("%d. Update %s record for %s", i+1, change.RecordType, change.FullName()))
			instructions = append(instructions, fmt.Sprintf("   Old Value: %s", change.OldValue))
			instructions = append(instructions, fmt.Sprintf("   New Value: %s", change.NewValue))
			instructions = append(instructions, fmt.Sprintf("   TTL: %d seconds", change.TTL))
			if change.Provider != "" && change.Provider != "manual" {
				instructions = append(instructions, fmt.Sprintf("   Provider: %s", change.Provider))
			}
			instructions = append(instructions, "")
		}
	}

	// Wait for propagation
	instructions = append(instructions, "## DNS Propagation")
	instructions = append(instructions, "")
	instructions = append(instructions, fmt.Sprintf("Wait %s for DNS propagation before proceeding.", plan.DNSPropagationWait))
	instructions = append(instructions, "You can verify propagation using: dig +short <domain>")
	instructions = append(instructions, "")

	// Post-checks
	if len(plan.PostChecks) > 0 {
		instructions = append(instructions, "## Post-Cutover Validation")
		instructions = append(instructions, "")
		for i, check := range plan.PostChecks {
			instructions = append(instructions, fmt.Sprintf("%d. %s", i+1, check.Name))
			instructions = append(instructions, fmt.Sprintf("   Type: %s", check.Type))
			instructions = append(instructions, fmt.Sprintf("   Endpoint: %s", check.Endpoint))
			instructions = append(instructions, "")
		}
	}

	// Rollback instructions
	instructions = append(instructions, "## Rollback Instructions")
	instructions = append(instructions, "")
	instructions = append(instructions, "If issues occur, revert DNS changes:")
	for i, change := range plan.DNSChanges {
		instructions = append(instructions, fmt.Sprintf("%d. Revert %s record for %s to: %s", i+1, change.RecordType, change.FullName(), change.OldValue))
	}

	result.ManualInstructions = instructions
	result.Success = true
	return result, nil
}

// ValidatePlan validates a cutover plan without executing it.
func (o *Orchestrator) ValidatePlan(ctx context.Context, plan *cutover.CutoverPlan, opts *OrchestratorOptions) ([]string, error) {
	issues := make([]string, 0)

	// Basic validation
	if errs := plan.Validate(); len(errs) > 0 {
		issues = append(issues, errs...)
	}

	// Validate DNS provider availability
	for _, change := range plan.DNSChanges {
		providerName := opts.DNSProvider
		if providerName == "" {
			providerName = change.Provider
		}
		if providerName != "" && providerName != "manual" {
			if _, ok := o.GetDNSProvider(providerName); !ok {
				issues = append(issues, fmt.Sprintf("DNS provider not available: %s", providerName))
			}
		}
	}

	// Validate health check endpoints are reachable
	for _, check := range plan.PreChecks {
		if errs := check.Validate(); len(errs) > 0 {
			for _, e := range errs {
				issues = append(issues, fmt.Sprintf("pre-check %s: %s", check.Name, e))
			}
		}
	}

	for _, check := range plan.PostChecks {
		if errs := check.Validate(); len(errs) > 0 {
			for _, e := range errs {
				issues = append(issues, fmt.Sprintf("post-check %s: %s", check.Name, e))
			}
		}
	}

	return issues, nil
}
