package database

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestOpenSearchConformanceManagedAToZ(t *testing.T) {
	if !resource.TypeOpenSearchDomain.IsValid() {
		t.Fatalf("%s should be a valid AWS resource type", resource.TypeOpenSearchDomain)
	}
	result, err := NewOpenSearchMapper().Map(context.Background(), managedOpenSearchFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated OpenSearch migration", result.ManualSteps)
	}
	if result.DockerService.Image != "opensearchproject/opensearch:2.15.0" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA OpenSearch target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/opensearch/domain-map.yaml", "config/opensearch/app-change.env", "config/opensearch/snapshot-repository.json"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/opensearch/app-change.env"])
	for _, want := range []string{"SOURCE_DOMAIN=orders-search", "OPENSEARCH_ENDPOINT=http://opensearch:9200", "APP_CHANGE_MODE=generated_patch"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_opensearch_domain.sh", "provision_opensearch.sh", "migrate_opensearch_snapshots.sh", "validate_opensearch_indexes.sh", "backup_opensearch_config.sh", "cutover_opensearch_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-opensearch-domain":     domainrunbook.StepTypeCommand,
		"provision-opensearch":         domainrunbook.StepTypeCommand,
		"migrate-opensearch-snapshots": domainrunbook.StepTypeCommand,
		"validate-opensearch-indexes":  domainrunbook.StepTypeCommand,
		"backup-opensearch-config":     domainrunbook.StepTypeCommand,
		"cutover-opensearch-clients":   domainrunbook.StepTypeAPICall,
		"rollback-opensearch-source":   domainrunbook.StepTypeRollback,
	} {
		if !hasOpenSearchRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func managedOpenSearchFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:     "arn:aws:es:eu-west-1:123456789012:domain/orders-search",
		Type:   resource.TypeOpenSearchDomain,
		Name:   "orders-search",
		Region: "eu-west-1",
		Config: map[string]interface{}{"domain_name": "orders-search", "engine_version": "OpenSearch_2.11"},
	}
}

func hasOpenSearchRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
