// Package cutover defines domain types for orchestrating the final migration cutover.
// It provides abstractions for DNS changes, health checks, and rollback triggers
// that coordinate the switch from cloud infrastructure to self-hosted Docker.
package cutover

import (
	"time"
)

// CutoverStatus represents the current state of a cutover plan execution.
type CutoverStatus string

const (
	// CutoverStatusPending indicates the cutover plan has not started.
	CutoverStatusPending CutoverStatus = "pending"

	// CutoverStatusRunning indicates the cutover is currently executing.
	CutoverStatusRunning CutoverStatus = "running"

	// CutoverStatusCompleted indicates the cutover finished successfully.
	CutoverStatusCompleted CutoverStatus = "completed"

	// CutoverStatusRolledBack indicates the cutover was rolled back.
	CutoverStatusRolledBack CutoverStatus = "rolled_back"

	// CutoverStatusFailed indicates the cutover failed without rollback.
	CutoverStatusFailed CutoverStatus = "failed"
)

// allCutoverStatuses contains all valid cutover statuses for iteration.
var allCutoverStatuses = []CutoverStatus{
	CutoverStatusPending,
	CutoverStatusRunning,
	CutoverStatusCompleted,
	CutoverStatusRolledBack,
	CutoverStatusFailed,
}

// AllCutoverStatuses returns all valid cutover statuses.
func AllCutoverStatuses() []CutoverStatus {
	result := make([]CutoverStatus, len(allCutoverStatuses))
	copy(result, allCutoverStatuses)
	return result
}

// String returns the string representation of the cutover status.
func (s CutoverStatus) String() string {
	return string(s)
}

// IsValid checks if the cutover status is a recognized status.
func (s CutoverStatus) IsValid() bool {
	for _, status := range allCutoverStatuses {
		if s == status {
			return true
		}
	}
	return false
}

// IsTerminal returns true if this status represents a final state.
func (s CutoverStatus) IsTerminal() bool {
	return s == CutoverStatusCompleted || s == CutoverStatusRolledBack || s == CutoverStatusFailed
}

// DisplayName returns a human-friendly display name for the cutover status.
func (s CutoverStatus) DisplayName() string {
	switch s {
	case CutoverStatusPending:
		return "Pending"
	case CutoverStatusRunning:
		return "Running"
	case CutoverStatusCompleted:
		return "Completed"
	case CutoverStatusRolledBack:
		return "Rolled Back"
	case CutoverStatusFailed:
		return "Failed"
	default:
		return string(s)
	}
}

// CutoverStepType represents the type of step in a cutover plan.
type CutoverStepType string

const (
	// CutoverStepTypePreCheck indicates a pre-cutover health check.
	CutoverStepTypePreCheck CutoverStepType = "pre_check"

	// CutoverStepTypeDNSChange indicates a DNS record change.
	CutoverStepTypeDNSChange CutoverStepType = "dns_change"

	// CutoverStepTypePostCheck indicates a post-cutover health check.
	CutoverStepTypePostCheck CutoverStepType = "post_check"

	// CutoverStepTypeRollback indicates a rollback operation.
	CutoverStepTypeRollback CutoverStepType = "rollback"
)

// String returns the string representation of the step type.
func (t CutoverStepType) String() string {
	return string(t)
}

// DisplayName returns a human-friendly display name for the step type.
func (t CutoverStepType) DisplayName() string {
	switch t {
	case CutoverStepTypePreCheck:
		return "Pre-Cutover Check"
	case CutoverStepTypeDNSChange:
		return "DNS Change"
	case CutoverStepTypePostCheck:
		return "Post-Cutover Check"
	case CutoverStepTypeRollback:
		return "Rollback"
	default:
		return string(t)
	}
}

// CutoverStepStatus represents the status of a single cutover step.
type CutoverStepStatus string

const (
	// CutoverStepStatusPending indicates the step has not started.
	CutoverStepStatusPending CutoverStepStatus = "pending"

	// CutoverStepStatusRunning indicates the step is currently executing.
	CutoverStepStatusRunning CutoverStepStatus = "running"

	// CutoverStepStatusCompleted indicates the step finished successfully.
	CutoverStepStatusCompleted CutoverStepStatus = "completed"

	// CutoverStepStatusFailed indicates the step failed.
	CutoverStepStatusFailed CutoverStepStatus = "failed"

	// CutoverStepStatusSkipped indicates the step was skipped.
	CutoverStepStatusSkipped CutoverStepStatus = "skipped"
)

// String returns the string representation of the step status.
func (s CutoverStepStatus) String() string {
	return string(s)
}

// CutoverPlan defines the complete plan for executing a migration cutover.
// It includes pre-flight checks, DNS changes, post-cutover validation,
// and automatic rollback triggers.
type CutoverPlan struct {
	// ID is the unique identifier for this cutover plan.
	ID string `json:"id"`

	// BundleID links this cutover plan to its migration bundle.
	BundleID string `json:"bundle_id"`

	// Name is a human-readable name for the cutover plan.
	Name string `json:"name,omitempty"`

	// Description provides additional context about the cutover.
	Description string `json:"description,omitempty"`

	// PreChecks are health checks to run before making DNS changes.
	// All pre-checks must pass before proceeding with cutover.
	PreChecks []*HealthCheck `json:"pre_checks"`

	// DNSChanges are the DNS record changes to execute during cutover.
	// These redirect traffic from cloud infrastructure to self-hosted.
	DNSChanges []*DNSChange `json:"dns_changes"`

	// PostChecks are health checks to run after DNS changes propagate.
	// Failed post-checks may trigger automatic rollback.
	PostChecks []*HealthCheck `json:"post_checks"`

	// RollbackTriggers define conditions that trigger automatic rollback.
	RollbackTriggers []*RollbackTrigger `json:"rollback_triggers"`

	// Timeout is the maximum duration for the entire cutover operation.
	Timeout time.Duration `json:"timeout"`

	// Status is the current state of the cutover plan.
	Status CutoverStatus `json:"status"`

	// Steps contains the ordered list of all cutover steps.
	Steps []*CutoverStep `json:"steps,omitempty"`

	// CurrentStepIndex tracks which step is currently executing.
	CurrentStepIndex int `json:"current_step_index"`

	// DNSPropagationWait is how long to wait for DNS propagation.
	DNSPropagationWait time.Duration `json:"dns_propagation_wait"`

	// CreatedAt is when this cutover plan was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when this cutover plan was last modified.
	UpdatedAt time.Time `json:"updated_at"`

	// ExecutedAt is when the cutover was executed (nil if not executed).
	ExecutedAt *time.Time `json:"executed_at,omitempty"`

	// CompletedAt is when the cutover finished (nil if not completed).
	CompletedAt *time.Time `json:"completed_at,omitempty"`

	// RolledBackAt is when the cutover was rolled back (nil if not rolled back).
	RolledBackAt *time.Time `json:"rolled_back_at,omitempty"`

	// Error contains the error message if the cutover failed.
	Error string `json:"error,omitempty"`

	// DryRun indicates this is a simulation run without actual changes.
	DryRun bool `json:"dry_run"`
}

// CutoverStep represents a single step in the cutover execution.
type CutoverStep struct {
	// Order is the execution order of this step (1-indexed).
	Order int `json:"order"`

	// Type identifies the kind of step (pre_check, dns_change, post_check).
	Type CutoverStepType `json:"type"`

	// Description explains what this step does.
	Description string `json:"description"`

	// Status is the current state of this step.
	Status CutoverStepStatus `json:"status"`

	// ReferenceID links to the specific check or change (HealthCheck.ID or DNSChange.ID).
	ReferenceID string `json:"reference_id,omitempty"`

	// StartedAt is when this step started executing.
	StartedAt *time.Time `json:"started_at,omitempty"`

	// CompletedAt is when this step finished.
	CompletedAt *time.Time `json:"completed_at,omitempty"`

	// Duration is how long the step took to execute.
	Duration time.Duration `json:"duration,omitempty"`

	// Error contains the error message if the step failed.
	Error string `json:"error,omitempty"`

	// Output contains any output or logs from the step.
	Output string `json:"output,omitempty"`
}

// NewCutoverPlan creates a new CutoverPlan with default values.
func NewCutoverPlan(id, bundleID string) *CutoverPlan {
	now := time.Now()
	return &CutoverPlan{
		ID:                 id,
		BundleID:           bundleID,
		PreChecks:          make([]*HealthCheck, 0),
		DNSChanges:         make([]*DNSChange, 0),
		PostChecks:         make([]*HealthCheck, 0),
		RollbackTriggers:   make([]*RollbackTrigger, 0),
		Steps:              make([]*CutoverStep, 0),
		Timeout:            30 * time.Minute,
		DNSPropagationWait: 5 * time.Minute,
		Status:             CutoverStatusPending,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}

// AddPreCheck adds a health check to run before cutover.
func (p *CutoverPlan) AddPreCheck(check *HealthCheck) {
	p.PreChecks = append(p.PreChecks, check)
	p.UpdatedAt = time.Now()
}

// AddDNSChange adds a DNS change to the cutover plan.
func (p *CutoverPlan) AddDNSChange(change *DNSChange) {
	p.DNSChanges = append(p.DNSChanges, change)
	p.UpdatedAt = time.Now()
}

// AddPostCheck adds a health check to run after cutover.
func (p *CutoverPlan) AddPostCheck(check *HealthCheck) {
	p.PostChecks = append(p.PostChecks, check)
	p.UpdatedAt = time.Now()
}

// AddRollbackTrigger adds a rollback trigger condition.
func (p *CutoverPlan) AddRollbackTrigger(trigger *RollbackTrigger) {
	p.RollbackTriggers = append(p.RollbackTriggers, trigger)
	p.UpdatedAt = time.Now()
}

// BuildSteps creates the ordered list of steps from checks and DNS changes.
// Should be called after all pre-checks, DNS changes, and post-checks are added.
func (p *CutoverPlan) BuildSteps() {
	p.Steps = make([]*CutoverStep, 0)
	order := 1

	// Add pre-checks
	for _, check := range p.PreChecks {
		p.Steps = append(p.Steps, &CutoverStep{
			Order:       order,
			Type:        CutoverStepTypePreCheck,
			Description: check.Name,
			Status:      CutoverStepStatusPending,
			ReferenceID: check.ID,
		})
		order++
	}

	// Add DNS changes
	for _, change := range p.DNSChanges {
		desc := change.RecordType + " record for " + change.Name + "." + change.Domain
		p.Steps = append(p.Steps, &CutoverStep{
			Order:       order,
			Type:        CutoverStepTypeDNSChange,
			Description: desc,
			Status:      CutoverStepStatusPending,
			ReferenceID: change.ID,
		})
		order++
	}

	// Add post-checks
	for _, check := range p.PostChecks {
		p.Steps = append(p.Steps, &CutoverStep{
			Order:       order,
			Type:        CutoverStepTypePostCheck,
			Description: check.Name,
			Status:      CutoverStepStatusPending,
			ReferenceID: check.ID,
		})
		order++
	}

	p.UpdatedAt = time.Now()
}

// TotalSteps returns the total number of steps in the cutover plan.
func (p *CutoverPlan) TotalSteps() int {
	return len(p.Steps)
}

// CompletedSteps returns the number of completed steps.
func (p *CutoverPlan) CompletedSteps() int {
	count := 0
	for _, step := range p.Steps {
		if step.Status == CutoverStepStatusCompleted {
			count++
		}
	}
	return count
}

// Progress returns the completion percentage (0-100).
func (p *CutoverPlan) Progress() float64 {
	total := p.TotalSteps()
	if total == 0 {
		return 0
	}
	return float64(p.CompletedSteps()) / float64(total) * 100
}

// CanStart returns true if the cutover plan can be started.
func (p *CutoverPlan) CanStart() bool {
	return p.Status == CutoverStatusPending && len(p.DNSChanges) > 0
}

// CanRollback returns true if the cutover can be rolled back.
func (p *CutoverPlan) CanRollback() bool {
	return p.Status == CutoverStatusRunning ||
		p.Status == CutoverStatusCompleted ||
		p.Status == CutoverStatusFailed
}

// HasAutoRollback returns true if any rollback trigger is set to auto-rollback.
func (p *CutoverPlan) HasAutoRollback() bool {
	for _, trigger := range p.RollbackTriggers {
		if trigger.AutoRollback {
			return true
		}
	}
	return false
}

// Validate checks if the cutover plan is valid and ready for execution.
func (p *CutoverPlan) Validate() []string {
	var errors []string

	if p.ID == "" {
		errors = append(errors, "cutover plan ID is required")
	}

	if p.BundleID == "" {
		errors = append(errors, "bundle ID is required")
	}

	if len(p.DNSChanges) == 0 {
		errors = append(errors, "at least one DNS change is required")
	}

	if p.Timeout <= 0 {
		errors = append(errors, "timeout must be positive")
	}

	// Validate each DNS change
	for i, change := range p.DNSChanges {
		if change.Domain == "" {
			errors = append(errors, "DNS change "+string(rune(i+1))+": domain is required")
		}
		if change.NewValue == "" {
			errors = append(errors, "DNS change "+string(rune(i+1))+": new value is required")
		}
	}

	// Validate each health check
	for _, check := range p.PreChecks {
		if errs := check.Validate(); len(errs) > 0 {
			errors = append(errors, errs...)
		}
	}

	for _, check := range p.PostChecks {
		if errs := check.Validate(); len(errs) > 0 {
			errors = append(errors, errs...)
		}
	}

	return errors
}
