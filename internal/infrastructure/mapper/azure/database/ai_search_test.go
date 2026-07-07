package database

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestAISearchConformanceManagedAToZ(t *testing.T) {
	result, err := NewAISearchMapper().Map(context.Background(), managedAISearchFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated AI Search migration", result.ManualSteps)
	}
	if result.DockerService.Image != "opensearchproject/opensearch:2.15.0" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA OpenSearch target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/ai-search/domain-map.yaml", "config/ai-search/app-change.env", "config/ai-search/snapshot-repository.json"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/ai-search/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_AI_SEARCH_SERVICE=orders-search", "OPENSEARCH_ENDPOINT=http://opensearch:9200"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_ai_search_service.sh", "provision_ai_search_opensearch.sh", "migrate_ai_search_indexes.sh", "validate_ai_search_indexes.sh", "backup_ai_search_config.sh", "cutover_ai_search_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-ai-search-service":  domainrunbook.StepTypeCommand,
		"provision-ai-search":       domainrunbook.StepTypeCommand,
		"migrate-ai-search-indexes": domainrunbook.StepTypeCommand,
		"validate-ai-search":        domainrunbook.StepTypeCommand,
		"backup-ai-search-config":   domainrunbook.StepTypeCommand,
		"cutover-ai-search-clients": domainrunbook.StepTypeAPICall,
		"rollback-ai-search-source": domainrunbook.StepTypeRollback,
	} {
		if !hasAISearchRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewAISearchMapper(t *testing.T) {
	if NewAISearchMapper().ResourceType() != resource.TypeAzureAISearch {
		t.Fatalf("AI Search mapper type = %s, want %s", NewAISearchMapper().ResourceType(), resource.TypeAzureAISearch)
	}
}

func managedAISearchFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "/subscriptions/demo/resourceGroups/rg/providers/Microsoft.Search/searchServices/orders-search",
		Type: resource.TypeAzureAISearch,
		Name: "orders-search",
		Config: map[string]interface{}{
			"name":     "orders-search",
			"location": "westeurope",
		},
	}
}

func hasAISearchRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
