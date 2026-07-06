package wizard

import (
	"fmt"
	"time"
)

type Step string

const (
	StepAnalyze Step = "analyze"
	StepExport  Step = "export"
	StepSecrets Step = "secrets"
	StepDeploy  Step = "deploy"
	StepSync    Step = "sync"
	StepCutover Step = "cutover"
	StepDone    Step = "done"
)

type Session struct {
	ID                string            `json:"id"`
	CurrentStep       Step              `json:"current_step"`
	CompletedSteps    []Step            `json:"completed_steps"`
	SourceProvider    string            `json:"source_provider,omitempty"`
	SelectedResources []string          `json:"selected_resources,omitempty"`
	BundleID          string            `json:"bundle_id,omitempty"`
	SecretsResolved   bool              `json:"secrets_resolved"`
	DeploymentID      string            `json:"deployment_id,omitempty"`
	RunbookID         string            `json:"runbook_id,omitempty"`
	SyncPlanID        string            `json:"sync_plan_id,omitempty"`
	CutoverID         string            `json:"cutover_id,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

func (s Session) Validate() error {
	if s.ID == "" {
		return fmt.Errorf("session id is required")
	}
	switch s.CurrentStep {
	case StepAnalyze, StepExport, StepSecrets, StepDeploy, StepSync, StepCutover, StepDone:
		return nil
	default:
		return fmt.Errorf("invalid current step %q", s.CurrentStep)
	}
}
