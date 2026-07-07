package devops

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestWorkflowsConformanceManagedAToZ(t *testing.T) {
	result, err := NewWorkflowsMapper().Map(context.Background(), managedWorkflowsFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Workflows migration", result.ManualSteps)
	}
	if result.DockerService.Image != "temporalio/auto-setup:1.23" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Temporal target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/workflows/source.yaml", "config/workflows/workflow-map.yaml", "config/workflows/app-change.env", "config/workflows/generated-client.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/workflows/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_GCP_WORKFLOW=checkout", "TARGET_WORKFLOW_ENGINE=temporal", "TEMPORAL_ADDRESS=temporal:7233"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_gcp_workflow.sh", "provision_temporal_workflow_namespace.sh", "migrate_gcp_workflow.sh", "validate_temporal_gcp_workflow.sh", "backup_gcp_workflow_config.sh", "cutover_gcp_workflow_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-gcp-workflow":             domainrunbook.StepTypeCommand,
		"provision-temporal-workflow":     domainrunbook.StepTypeCommand,
		"migrate-gcp-workflow":            domainrunbook.StepTypeCommand,
		"validate-temporal-gcp-workflow":  domainrunbook.StepTypeCommand,
		"backup-gcp-workflow-config":      domainrunbook.StepTypeCommand,
		"cutover-gcp-workflow-clients":    domainrunbook.StepTypeAPICall,
		"rollback-gcp-workflow-authority": domainrunbook.StepTypeRollback,
	} {
		if !hasWorkflowsRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewWorkflowsMapper(t *testing.T) {
	m := NewWorkflowsMapper()
	if m == nil {
		t.Fatal("NewWorkflowsMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeWorkflowsWorkflow {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeWorkflowsWorkflow)
	}
}

func managedWorkflowsFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "projects/demo/locations/europe-west1/workflows/checkout",
		Type: resource.TypeWorkflowsWorkflow,
		Name: "checkout",
		Config: map[string]interface{}{
			"name":            "checkout",
			"region":          "europe-west1",
			"source_contents": "main:\n  steps:\n    - done:\n        return: ok\n",
		},
	}
}

func hasWorkflowsRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
