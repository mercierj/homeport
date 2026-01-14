// Package sync provides the application layer for data synchronization operations.
package sync

import (
	"context"
	"fmt"
	gosync "sync"
	"time"

	"github.com/google/uuid"
	domainsync "github.com/homeport/homeport/internal/domain/sync"
	infrasync "github.com/homeport/homeport/internal/infrastructure/sync"
)

// Service orchestrates data synchronization operations.
type Service struct {
	registry *domainsync.StrategyRegistry
	plans    map[string]*SyncExecution
	mu       gosync.RWMutex
}

// SyncExecution tracks the execution state of a sync plan.
type SyncExecution struct {
	Plan      *domainsync.SyncPlan
	Status    string // "pending", "running", "paused", "completed", "failed", "cancelled"
	StartedAt *time.Time
	Error     string
	cancel    context.CancelFunc
}

// NewService creates a new sync service with the default strategy registry.
func NewService() *Service {
	return &Service{
		registry: infrasync.NewDefaultRegistry(),
		plans:    make(map[string]*SyncExecution),
	}
}

// CreatePlan creates a new sync plan from a list of tasks.
func (s *Service) CreatePlan(tasks []*domainsync.SyncTask) (*domainsync.SyncPlan, error) {
	planID := uuid.New().String()[:8]
	plan := domainsync.NewSyncPlan(planID)

	for _, task := range tasks {
		plan.AddTask(task)
	}

	s.mu.Lock()
	s.plans[planID] = &SyncExecution{
		Plan:   plan,
		Status: "pending",
	}
	s.mu.Unlock()

	return plan, nil
}

// GetPlan returns a sync plan by ID.
func (s *Service) GetPlan(planID string) (*SyncExecution, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	exec, ok := s.plans[planID]
	if !ok {
		return nil, fmt.Errorf("sync plan not found: %s", planID)
	}
	return exec, nil
}

// StartCallback is called when a sync event occurs.
type StartCallback func(event SyncEvent)

// SyncEvent represents a sync progress event.
type SyncEvent struct {
	Type      string                   // "task_start", "task_progress", "task_complete", "task_error", "plan_complete"
	PlanID    string                   `json:"plan_id"`
	TaskID    string                   `json:"task_id,omitempty"`
	TaskName  string                   `json:"task_name,omitempty"`
	TaskType  string                   `json:"task_type,omitempty"`
	Status    string                   `json:"status,omitempty"`
	Progress  int                      `json:"progress,omitempty"`
	BytesTotal int64                   `json:"bytes_total,omitempty"`
	BytesDone  int64                   `json:"bytes_done,omitempty"`
	ItemsTotal int64                   `json:"items_total,omitempty"`
	ItemsDone  int64                   `json:"items_done,omitempty"`
	Error     string                   `json:"error,omitempty"`
	Message   string                   `json:"message,omitempty"`
}

// Start begins executing a sync plan.
func (s *Service) Start(ctx context.Context, planID string, callback StartCallback) error {
	s.mu.Lock()
	exec, ok := s.plans[planID]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("sync plan not found: %s", planID)
	}

	if exec.Status == "running" {
		s.mu.Unlock()
		return fmt.Errorf("sync plan already running")
	}

	// Create cancellable context
	ctx, cancel := context.WithCancel(ctx)
	exec.cancel = cancel
	exec.Status = "running"
	now := time.Now()
	exec.StartedAt = &now
	s.mu.Unlock()

	// Run sync in background
	go s.executePlan(ctx, exec, callback)

	return nil
}

// executePlan runs all tasks in the plan.
func (s *Service) executePlan(ctx context.Context, exec *SyncExecution, callback StartCallback) {
	plan := exec.Plan

	for _, task := range plan.Tasks {
		select {
		case <-ctx.Done():
			s.mu.Lock()
			exec.Status = "cancelled"
			s.mu.Unlock()
			return
		default:
		}

		// Get strategy for this task
		strategy := s.registry.Get(task.Strategy)
		if strategy == nil {
			// Try to infer from endpoint type
			strategy = s.registry.GetForEndpoint(task.Source.Type)
		}

		if strategy == nil {
			task.Fail(fmt.Errorf("no sync strategy found for %s", task.Strategy))
			if callback != nil {
				callback(SyncEvent{
					Type:     "task_error",
					PlanID:   plan.ID,
					TaskID:   task.ID,
					TaskName: task.Name,
					Error:    task.ErrorMessage,
				})
			}
			continue
		}

		// Notify task start
		task.Start()
		if callback != nil {
			callback(SyncEvent{
				Type:     "task_start",
				PlanID:   plan.ID,
				TaskID:   task.ID,
				TaskName: task.Name,
				TaskType: string(task.Type),
				Status:   "running",
			})
		}

		// Create progress channel
		progressCh := make(chan domainsync.Progress, 100)

		// Run sync in goroutine and collect progress
		errCh := make(chan error, 1)
		go func() {
			errCh <- strategy.Sync(ctx, task.Source, task.Target, progressCh)
		}()

		// Forward progress events
	progressLoop:
		for {
			select {
			case progress, ok := <-progressCh:
				if !ok {
					break progressLoop
				}
				task.Progress = &progress
				percentDone := 0
				if progress.BytesTotal > 0 {
					percentDone = int(float64(progress.BytesDone) / float64(progress.BytesTotal) * 100)
				}
				if callback != nil {
					callback(SyncEvent{
						Type:       "task_progress",
						PlanID:     plan.ID,
						TaskID:     task.ID,
						TaskName:   task.Name,
						TaskType:   string(task.Type),
						Status:     "running",
						Progress:   percentDone,
						BytesTotal: progress.BytesTotal,
						BytesDone:  progress.BytesDone,
						ItemsTotal: progress.ItemsTotal,
						ItemsDone:  progress.ItemsDone,
					})
				}
			case err := <-errCh:
				// Drain remaining progress
				for range progressCh {
				}
				if err != nil {
					task.Fail(err)
					if callback != nil {
						callback(SyncEvent{
							Type:     "task_error",
							PlanID:   plan.ID,
							TaskID:   task.ID,
							TaskName: task.Name,
							Error:    err.Error(),
						})
					}
				} else {
					task.Complete()
					if callback != nil {
						callback(SyncEvent{
							Type:     "task_complete",
							PlanID:   plan.ID,
							TaskID:   task.ID,
							TaskName: task.Name,
							Status:   "completed",
							Progress: 100,
						})
					}
				}
				break progressLoop
			case <-ctx.Done():
				task.Fail(ctx.Err())
				break progressLoop
			}
		}
	}

	// Mark plan as complete
	s.mu.Lock()
	if plan.HasFailed() {
		exec.Status = "failed"
	} else {
		exec.Status = "completed"
	}
	s.mu.Unlock()

	if callback != nil {
		callback(SyncEvent{
			Type:   "plan_complete",
			PlanID: plan.ID,
			Status: exec.Status,
		})
	}
}

// Pause pauses a running sync plan.
func (s *Service) Pause(planID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	exec, ok := s.plans[planID]
	if !ok {
		return fmt.Errorf("sync plan not found: %s", planID)
	}

	if exec.Status != "running" {
		return fmt.Errorf("sync plan is not running")
	}

	exec.Status = "paused"
	// Note: actual pause requires strategy support
	return nil
}

// Resume resumes a paused sync plan.
func (s *Service) Resume(ctx context.Context, planID string, callback StartCallback) error {
	s.mu.Lock()
	exec, ok := s.plans[planID]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("sync plan not found: %s", planID)
	}

	if exec.Status != "paused" {
		s.mu.Unlock()
		return fmt.Errorf("sync plan is not paused")
	}

	exec.Status = "running"
	s.mu.Unlock()

	// Continue executing remaining tasks
	go s.executePlan(ctx, exec, callback)
	return nil
}

// Cancel cancels a running sync plan.
func (s *Service) Cancel(planID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	exec, ok := s.plans[planID]
	if !ok {
		return fmt.Errorf("sync plan not found: %s", planID)
	}

	if exec.cancel != nil {
		exec.cancel()
	}
	exec.Status = "cancelled"
	return nil
}

// ListPlans returns all sync plans.
func (s *Service) ListPlans() []*SyncExecution {
	s.mu.RLock()
	defer s.mu.RUnlock()

	plans := make([]*SyncExecution, 0, len(s.plans))
	for _, exec := range s.plans {
		plans = append(plans, exec)
	}
	return plans
}

// GetStrategies returns available sync strategies.
func (s *Service) GetStrategies() []string {
	return s.registry.List()
}
