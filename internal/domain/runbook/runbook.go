package runbook

import (
	"fmt"
	"time"
)

type StepType string

const (
	StepTypeInput      StepType = "input"
	StepTypeCommand    StepType = "command"
	StepTypeAPICall    StepType = "api_call"
	StepTypeDNSCheck   StepType = "dns_check"
	StepTypeHealth     StepType = "health_check"
	StepTypeDataVerify StepType = "data_verify"
	StepTypeApproval   StepType = "approval"
	StepTypeRollback   StepType = "rollback"
)

type StepStatus string

const (
	StepStatusPending StepStatus = "pending"
	StepStatusRunning StepStatus = "running"
	StepStatusPassed  StepStatus = "passed"
	StepStatusFailed  StepStatus = "failed"
	StepStatusSkipped StepStatus = "skipped"
	StepStatusBlocked StepStatus = "blocked"
)

type Runbook struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Steps     []Step    `json:"steps"`
	CreatedAt time.Time `json:"created_at,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

type Step struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Description      string            `json:"description,omitempty"`
	Group            string            `json:"group,omitempty"`
	Type             StepType          `json:"type"`
	Status           StepStatus        `json:"status"`
	Optional         bool              `json:"optional,omitempty"`
	Executor         string            `json:"executor,omitempty"`
	SuccessCondition string            `json:"success_condition,omitempty"`
	Command          []string          `json:"command,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
	Result           *StepResult       `json:"result,omitempty"`
}

type StepResult struct {
	Status    StepStatus `json:"status"`
	Output    string     `json:"output,omitempty"`
	Error     string     `json:"error,omitempty"`
	StartedAt time.Time  `json:"started_at,omitempty"`
	EndedAt   time.Time  `json:"ended_at,omitempty"`
}

func (r Runbook) Validate() error {
	if r.ID == "" {
		return fmt.Errorf("runbook id is required")
	}
	if r.Name == "" {
		return fmt.Errorf("runbook name is required")
	}
	for i := range r.Steps {
		if err := r.Steps[i].Validate(); err != nil {
			return fmt.Errorf("step %d: %w", i, err)
		}
	}
	return nil
}

func (s Step) Validate() error {
	if s.ID == "" {
		return fmt.Errorf("id is required")
	}
	if s.Name == "" {
		return fmt.Errorf("name is required")
	}
	if !validStepType(s.Type) {
		return fmt.Errorf("invalid type %q", s.Type)
	}
	if !validStepStatus(s.Status) {
		return fmt.Errorf("invalid status %q", s.Status)
	}
	if !s.Optional && (s.Executor == "" || s.SuccessCondition == "") {
		return fmt.Errorf("required step needs executor and success condition")
	}
	return nil
}

func (r Runbook) FirstUnpassedStep() *Step {
	for i := range r.Steps {
		if r.Steps[i].Status != StepStatusPassed && r.Steps[i].Status != StepStatusSkipped {
			return &r.Steps[i]
		}
	}
	return nil
}

func (r Runbook) HasBlockedRequiredStep() bool {
	for _, step := range r.Steps {
		if !step.Optional && step.Status == StepStatusBlocked {
			return true
		}
	}
	return false
}

func validStepType(t StepType) bool {
	switch t {
	case StepTypeInput, StepTypeCommand, StepTypeAPICall, StepTypeDNSCheck, StepTypeHealth, StepTypeDataVerify, StepTypeApproval, StepTypeRollback:
		return true
	default:
		return false
	}
}

func validStepStatus(s StepStatus) bool {
	switch s {
	case StepStatusPending, StepStatusRunning, StepStatusPassed, StepStatusFailed, StepStatusSkipped, StepStatusBlocked:
		return true
	default:
		return false
	}
}
