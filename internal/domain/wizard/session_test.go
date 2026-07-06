package wizard

import "testing"

func TestSessionValidateRejectsInvalidStep(t *testing.T) {
	err := Session{ID: "s1", CurrentStep: Step("bogus")}.Validate()
	if err == nil {
		t.Fatal("expected invalid step error")
	}
}

func TestSessionValidateAcceptsDeploy(t *testing.T) {
	if err := (Session{ID: "s1", CurrentStep: StepDeploy}).Validate(); err != nil {
		t.Fatal(err)
	}
}
