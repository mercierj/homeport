package messaging

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestEventarcConformanceManagedAToZ(t *testing.T) {
	result, err := NewEventarcMapper().Map(context.Background(), managedEventarcFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Eventarc routing migration", result.ManualSteps)
	}
	if result.DockerService.Image != "n8nio/n8n:latest" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA n8n target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/n8n/workflows/eventarc_workflow.json", "config/eventarc/app-change.env", "config/eventarc/trigger-filter.json"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/eventarc/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_EVENTARC_TRIGGER=orders-created", "TARGET_WEBHOOK=http://localhost:5678/webhook/orders-created"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"setup_n8n_eventarc.sh", "dispatch_eventarc_event.sh", "backup_eventarc_workflow.sh", "validate_eventarc_route.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"render-eventarc-workflow":   domainrunbook.StepTypeCommand,
		"provision-eventarc-router":  domainrunbook.StepTypeCommand,
		"dispatch-eventarc-sample":   domainrunbook.StepTypeCommand,
		"validate-eventarc-route":    domainrunbook.StepTypeCommand,
		"backup-eventarc-workflow":   domainrunbook.StepTypeCommand,
		"cutover-eventarc-producers": domainrunbook.StepTypeAPICall,
		"rollback-eventarc-source":   domainrunbook.StepTypeRollback,
	} {
		if !hasEventarcRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewEventarcMapper(t *testing.T) {
	m := NewEventarcMapper()
	if m == nil {
		t.Fatal("NewEventarcMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeEventarcTrigger {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeEventarcTrigger)
	}
}

func managedEventarcFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "projects/demo/locations/europe-west1/triggers/orders-created",
		Type: resource.TypeEventarcTrigger,
		Name: "orders-created",
		Config: map[string]interface{}{
			"name": "orders-created",
			"matching_criteria": []interface{}{
				map[string]interface{}{"attribute": "type", "value": "google.cloud.audit.log.v1.written"},
			},
			"destination": map[string]interface{}{"cloud_run_service": "orders-handler"},
		},
	}
}

func hasEventarcRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
