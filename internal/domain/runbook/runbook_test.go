package runbook

import "testing"

func TestStepValidateRequiresExecutorAndSuccessCondition(t *testing.T) {
	step := Step{
		ID:       "deploy",
		Name:     "Deploy stack",
		Type:     StepTypeCommand,
		Status:   StepStatusPending,
		Optional: false,
	}

	if err := step.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want missing executor/success condition error")
	}

	step.Executor = "shell"
	step.SuccessCondition = "exit_code == 0"

	if err := step.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestRunbookValidateAllowsOptionalGuidanceWithoutExecutor(t *testing.T) {
	book := Runbook{
		ID:   "rb-1",
		Name: "Migration",
		Steps: []Step{{
			ID:       "note",
			Name:     "Optional review",
			Type:     StepTypeApproval,
			Status:   StepStatusBlocked,
			Optional: true,
		}},
	}

	if err := book.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}
