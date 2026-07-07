package compute

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestFoundryOpenAIConformanceManagedAToZ(t *testing.T) {
	result, err := NewFoundryOpenAIMapper().Map(context.Background(), managedFoundryOpenAIFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Foundry/OpenAI migration", result.ManualSteps)
	}
	if result.DockerService.Image != "vllm/vllm-openai:latest" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA vLLM target: %#v", result.DockerService)
	}
	for _, file := range []string{"docker-compose.foundry-openai.yml", "config/foundry-openai/app-change.env", "config/foundry-openai/account-report.yaml", "config/foundry-openai/generated-client.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/foundry-openai/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_AZURE_OPENAI_ACCOUNT=fraud-openai", "TARGET_OPENAI_BASE_URL=http://foundry-openai:8000/v1"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_foundry_openai_account.sh", "migrate_foundry_openai_models.sh", "validate_foundry_openai_endpoint.sh", "backup_foundry_openai_config.sh", "cutover_foundry_openai_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-foundry-openai-account":   domainrunbook.StepTypeCommand,
		"provision-foundry-openai-vllm":   domainrunbook.StepTypeCommand,
		"migrate-foundry-openai-models":   domainrunbook.StepTypeCommand,
		"validate-foundry-openai-vllm":    domainrunbook.StepTypeCommand,
		"backup-foundry-openai-config":    domainrunbook.StepTypeCommand,
		"cutover-foundry-openai-clients":  domainrunbook.StepTypeAPICall,
		"rollback-foundry-openai-account": domainrunbook.StepTypeRollback,
	} {
		if !hasFoundryOpenAIRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewFoundryOpenAIMapper(t *testing.T) {
	m := NewFoundryOpenAIMapper()
	if m == nil {
		t.Fatal("NewFoundryOpenAIMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeAzureFoundryOpenAI {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeAzureFoundryOpenAI)
	}
}

func managedFoundryOpenAIFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "/subscriptions/demo/resourceGroups/rg/providers/Microsoft.CognitiveServices/accounts/fraud-openai",
		Type: resource.TypeAzureFoundryOpenAI,
		Name: "fraud-openai",
		Config: map[string]interface{}{
			"name":     "fraud-openai",
			"kind":     "OpenAI",
			"location": "westeurope",
			"model":    "mistralai/Mistral-7B-Instruct-v0.2",
		},
	}
}

func hasFoundryOpenAIRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
