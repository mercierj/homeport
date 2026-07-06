package compute

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestBedrockConformanceManagedAToZ(t *testing.T) {
	result, err := NewBedrockMapper().Map(context.Background(), managedBedrockFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Bedrock adapter migration", result.ManualSteps)
	}
	if result.DockerService.Image != "ollama/ollama:0.5.7" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Ollama: %#v", result.DockerService)
	}
	for _, file := range []string{"config/bedrock/models.yaml", "config/bedrock/adapter.env"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing %s", file)
		}
	}
	models := string(result.Configs["config/bedrock/models.yaml"])
	for _, want := range []string{"anthropic.claude-3-haiku", "llama3.1"} {
		if !strings.Contains(models, want) {
			t.Fatalf("models config missing %q:\n%s", want, models)
		}
	}
	if _, ok := result.Scripts["backup_bedrock_config.sh"]; !ok {
		t.Fatal("missing backup script")
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"render-bedrock-model-map": domainrunbook.StepTypeCommand,
		"provision-ollama-runtime": domainrunbook.StepTypeCommand,
		"validate-bedrock-adapter": domainrunbook.StepTypeCommand,
		"backup-bedrock-config":    domainrunbook.StepTypeCommand,
		"cutover-bedrock-endpoint": domainrunbook.StepTypeCommand,
		"rollback-bedrock-source":  domainrunbook.StepTypeRollback,
	} {
		if !hasBedrockRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func managedBedrockFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "bedrock-runtime",
		Type: resource.TypeBedrockModel,
		Name: "bedrock-runtime",
		Config: map[string]interface{}{
			"name": "bedrock-runtime",
			"models": []interface{}{
				map[string]interface{}{"source": "anthropic.claude-3-haiku", "target": "llama3.1"},
			},
		},
	}
}

func hasBedrockRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
