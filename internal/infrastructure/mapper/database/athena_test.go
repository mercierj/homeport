package database

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestAthenaConformanceManagedAToZ(t *testing.T) {
	result, err := NewAthenaMapper().Map(context.Background(), managedAthenaFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Athena migration", result.ManualSteps)
	}
	if result.DockerService.Image != "trinodb/trino:443" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Trino: %#v", result.DockerService)
	}
	for _, file := range []string{"config/trino/catalog/hive.properties", "config/trino/migration.sql"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing %s", file)
		}
	}
	sql := string(result.Configs["config/trino/migration.sql"])
	for _, want := range []string{"CREATE SCHEMA IF NOT EXISTS analytics", "CREATE VIEW analytics.recent_orders"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration SQL missing %q:\n%s", want, sql)
		}
	}
	if _, ok := result.Scripts["backup_athena_config.sh"]; !ok {
		t.Fatal("missing backup script")
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"render-trino-catalog":   domainrunbook.StepTypeCommand,
		"migrate-athena-ddl":     domainrunbook.StepTypeCommand,
		"validate-trino-query":   domainrunbook.StepTypeCommand,
		"backup-athena-config":   domainrunbook.StepTypeCommand,
		"cutover-athena-dsn":     domainrunbook.StepTypeCommand,
		"rollback-athena-source": domainrunbook.StepTypeRollback,
	} {
		if !hasAthenaRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func managedAthenaFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "primary",
		Type: resource.TypeAthenaWorkgroup,
		Name: "primary",
		Config: map[string]interface{}{
			"name":            "primary",
			"database":        "analytics",
			"output_location": "s3://query-results/",
			"views": []interface{}{
				map[string]interface{}{
					"name": "recent_orders",
					"sql":  "SELECT * FROM orders WHERE created_at > current_date - interval '7' day",
				},
			},
		},
	}
}

func hasAthenaRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
