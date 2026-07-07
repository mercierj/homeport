package compute

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestVertexAIConformanceManagedAToZ(t *testing.T) {
	result, err := NewVertexAIMapper().Map(context.Background(), managedVertexAIFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Vertex AI migration", result.ManualSteps)
	}
	if result.DockerService.Image != "vllm/vllm-openai:latest" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA vLLM target: %#v", result.DockerService)
	}
	for _, file := range []string{"docker-compose.vertex-ai.yml", "config/vertex-ai/app-change.env", "config/vertex-ai/endpoint-report.yaml", "config/vertex-ai/generated-client.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/vertex-ai/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_VERTEX_AI_ENDPOINT=fraud-endpoint", "TARGET_OPENAI_BASE_URL=http://vertex-ai:8000/v1"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_vertex_ai_endpoint.sh", "migrate_vertex_ai_model.sh", "validate_vertex_ai_endpoint.sh", "backup_vertex_ai_config.sh", "cutover_vertex_ai_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-vertex-ai-endpoint":   domainrunbook.StepTypeCommand,
		"provision-vllm-endpoint":     domainrunbook.StepTypeCommand,
		"migrate-vertex-ai-model":     domainrunbook.StepTypeCommand,
		"validate-vllm-endpoint":      domainrunbook.StepTypeCommand,
		"backup-vertex-ai-config":     domainrunbook.StepTypeCommand,
		"cutover-vertex-ai-clients":   domainrunbook.StepTypeAPICall,
		"rollback-vertex-ai-endpoint": domainrunbook.StepTypeRollback,
	} {
		if !hasVertexAIRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewVertexAIMapper(t *testing.T) {
	m := NewVertexAIMapper()
	if m == nil {
		t.Fatal("NewVertexAIMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeVertexAIEndpoint {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeVertexAIEndpoint)
	}
}

func managedVertexAIFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "projects/demo/locations/europe-west1/endpoints/fraud-endpoint",
		Type: resource.TypeVertexAIEndpoint,
		Name: "fraud-endpoint",
		Config: map[string]interface{}{
			"name":         "fraud-endpoint",
			"display_name": "Fraud endpoint",
			"region":       "europe-west1",
			"model":        "fraud-model",
		},
	}
}

func hasVertexAIRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
