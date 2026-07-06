package messaging

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestStepFunctionsConformanceManagedAToZ(t *testing.T) {
	result, err := NewStepFunctionsMapper().Map(context.Background(), managedStepFunctionsFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Step Functions migration", result.ManualSteps)
	}
	if result.DockerService.Image != "temporalio/auto-setup:1.23" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Temporal target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/stepfunctions/asl.json", "config/stepfunctions/workflow-map.yaml", "config/stepfunctions/app-change.env", "config/stepfunctions/generated-client.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/stepfunctions/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_STATE_MACHINE=checkout", "TARGET_WORKFLOW_ENGINE=temporal", "TEMPORAL_ADDRESS=temporal:7233"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_stepfunctions_state_machine.sh", "provision_temporal_namespace.sh", "migrate_stepfunctions_workflow.sh", "validate_temporal_workflow.sh", "backup_stepfunctions_config.sh", "cutover_stepfunctions_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-stepfunctions-state-machine": domainrunbook.StepTypeCommand,
		"provision-temporal-namespace":       domainrunbook.StepTypeCommand,
		"migrate-stepfunctions-workflow":     domainrunbook.StepTypeCommand,
		"validate-temporal-workflow":         domainrunbook.StepTypeCommand,
		"backup-stepfunctions-config":        domainrunbook.StepTypeCommand,
		"cutover-stepfunctions-clients":      domainrunbook.StepTypeAPICall,
		"rollback-stepfunctions-source":      domainrunbook.StepTypeRollback,
	} {
		if !hasStepFunctionsRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewStepFunctionsMapper(t *testing.T) {
	m := NewStepFunctionsMapper()
	if m == nil {
		t.Fatal("NewStepFunctionsMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeStepFunctionsStateMachine {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeStepFunctionsStateMachine)
	}
}

func managedStepFunctionsFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:     "arn:aws:states:eu-west-1:123456789012:stateMachine:checkout",
		Type:   resource.TypeStepFunctionsStateMachine,
		Name:   "checkout",
		Region: "eu-west-1",
		Config: map[string]interface{}{
			"name":       "checkout",
			"definition": `{"StartAt":"Charge","States":{"Charge":{"Type":"Pass","End":true}}}`,
		},
	}
}

func hasStepFunctionsRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
