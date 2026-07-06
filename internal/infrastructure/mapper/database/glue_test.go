package database

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestGlueConformanceManagedAToZ(t *testing.T) {
	if !resource.TypeGlueCatalogDatabase.IsValid() {
		t.Fatalf("%s should be a valid AWS resource type", resource.TypeGlueCatalogDatabase)
	}
	result, err := NewGlueMapper().Map(context.Background(), managedGlueFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Glue catalog migration", result.ManualSteps)
	}
	if result.DockerService.Image != "apache/hive:4.0.0" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Hive metastore: %#v", result.DockerService)
	}
	for _, file := range []string{"config/glue/catalog.yaml", "config/glue/jobs.yaml", "config/glue/app-change.env"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/glue/app-change.env"])
	for _, want := range []string{"SOURCE_DATABASE=orders_catalog", "TARGET_METASTORE=hive-metastore:9083", "APP_CHANGE_MODE=generated_patch"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_glue_catalog.sh", "import_hive_metastore.sh", "backup_glue_catalog.sh", "validate_glue_catalog.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-glue-catalog":      domainrunbook.StepTypeCommand,
		"provision-hive-metastore": domainrunbook.StepTypeCommand,
		"import-hive-metastore":    domainrunbook.StepTypeCommand,
		"validate-glue-catalog":    domainrunbook.StepTypeCommand,
		"backup-glue-catalog":      domainrunbook.StepTypeCommand,
		"cutover-glue-metastore":   domainrunbook.StepTypeAPICall,
		"rollback-glue-source":     domainrunbook.StepTypeRollback,
	} {
		if !hasGlueRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func managedGlueFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "orders_catalog",
		Type: resource.TypeGlueCatalogDatabase,
		Name: "orders_catalog",
		Config: map[string]interface{}{
			"name":         "orders_catalog",
			"description":  "orders lake catalog",
			"location_uri": "s3://orders-lake/",
			"jobs": []interface{}{
				map[string]interface{}{"name": "orders-etl", "command": "glueetl", "script_location": "s3://jobs/orders.py"},
			},
		},
	}
}

func hasGlueRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
