package database

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestBigQueryConformanceManagedAToZ(t *testing.T) {
	result, err := NewBigQueryMapper().Map(context.Background(), managedBigQueryFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated BigQuery migration", result.ManualSteps)
	}
	if result.DockerService.Image != "trinodb/trino:445" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Trino target: %#v", result.DockerService)
	}
	for _, file := range []string{
		"config/bigquery/catalog/iceberg.properties",
		"config/bigquery/app-change.env",
		"config/bigquery/bigquery-api-routes.yaml",
	} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/bigquery/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_BIGQUERY_DATASET=analytics", "TARGET_QUERY_ENDPOINT=http://trino:8080"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	routes := string(result.Configs["config/bigquery/bigquery-api-routes.yaml"])
	for _, want := range []string{"bigquery.jobs.query", "bigquery.jobs.getQueryResults", "bigquery.datasets.get", "bigquery.tables.get"} {
		if !strings.Contains(routes, want) {
			t.Fatalf("API compatibility routes missing %q:\n%s", want, routes)
		}
	}
	for _, file := range []string{"export_bigquery_dataset.sh", "load_bigquery_iceberg.sh", "backup_bigquery.sh", "validate_bigquery.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"discover-bigquery-dataset":      domainrunbook.StepTypeCommand,
		"provision-trino-iceberg":        domainrunbook.StepTypeCommand,
		"export-bigquery-dataset":        domainrunbook.StepTypeCommand,
		"load-bigquery-iceberg":          domainrunbook.StepTypeCommand,
		"validate-bigquery-api":          domainrunbook.StepTypeCommand,
		"backup-bigquery-iceberg":        domainrunbook.StepTypeCommand,
		"cutover-bigquery-client-config": domainrunbook.StepTypeAPICall,
		"rollback-bigquery-source":       domainrunbook.StepTypeRollback,
	} {
		if !hasBigQueryRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewBigQueryMapper(t *testing.T) {
	m := NewBigQueryMapper()
	if m == nil {
		t.Fatal("NewBigQueryMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeBigQuery {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeBigQuery)
	}
}

func managedBigQueryFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:     "projects/demo/datasets/analytics",
		Type:   resource.TypeBigQuery,
		Name:   "analytics",
		Region: "EU",
		Config: map[string]interface{}{
			"dataset_id": "analytics",
			"location":   "EU",
		},
	}
}

func hasBigQueryRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
