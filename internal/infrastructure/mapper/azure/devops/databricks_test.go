package devops

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestDatabricksConformanceManagedAToZ(t *testing.T) {
	result, err := NewDatabricksMapper().Map(context.Background(), managedDatabricksFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Databricks migration", result.ManualSteps)
	}
	if result.DockerService.Image != "bitnami/spark:3.5" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Spark target: %#v", result.DockerService)
	}
	for _, file := range []string{"docker-compose.databricks.yml", "config/databricks/app-change.env", "config/databricks/workspace-report.yaml"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/databricks/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_DATABRICKS_WORKSPACE=orders-dbx", "TARGET_SPARK_MASTER=spark://spark-master:7077"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_databricks_workspace.sh", "migrate_databricks_jobs.sh", "validate_databricks_spark.sh", "backup_databricks_config.sh", "cutover_databricks_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-databricks-workspace": domainrunbook.StepTypeCommand,
		"provision-databricks-spark":  domainrunbook.StepTypeCommand,
		"migrate-databricks-jobs":     domainrunbook.StepTypeCommand,
		"validate-databricks-spark":   domainrunbook.StepTypeCommand,
		"backup-databricks-config":    domainrunbook.StepTypeCommand,
		"cutover-databricks-clients":  domainrunbook.StepTypeAPICall,
		"rollback-databricks-source":  domainrunbook.StepTypeRollback,
	} {
		if !hasDatabricksRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func managedDatabricksFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:     "/subscriptions/demo/resourceGroups/rg/providers/Microsoft.Databricks/workspaces/orders-dbx",
		Type:   resource.TypeAzureDatabricks,
		Name:   "orders-dbx",
		Region: "westeurope",
		Config: map[string]interface{}{"name": "orders-dbx", "location": "westeurope"},
	}
}

func hasDatabricksRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
