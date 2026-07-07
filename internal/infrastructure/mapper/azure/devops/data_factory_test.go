package devops

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestDataFactoryConformanceManagedAToZ(t *testing.T) {
	result, err := NewDataFactoryMapper().Map(context.Background(), managedDataFactoryFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Data Factory migration", result.ManualSteps)
	}
	if result.DockerService.Image != "apache/airflow:2.9.3" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Airflow target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/data-factory/pipeline-map.yaml", "config/data-factory/app-change.env", "config/data-factory/generated-client.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/data-factory/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_DATA_FACTORY=orders-factory", "TARGET_WORKFLOW_ENGINE=airflow"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_data_factory.sh", "migrate_data_factory_pipelines.sh", "validate_data_factory_airflow.sh", "backup_data_factory_config.sh", "cutover_data_factory_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-data-factory":            domainrunbook.StepTypeCommand,
		"provision-airflow-target":       domainrunbook.StepTypeCommand,
		"migrate-data-factory-pipelines": domainrunbook.StepTypeCommand,
		"validate-data-factory-airflow":  domainrunbook.StepTypeCommand,
		"backup-data-factory-config":     domainrunbook.StepTypeCommand,
		"cutover-data-factory-clients":   domainrunbook.StepTypeAPICall,
		"rollback-data-factory-source":   domainrunbook.StepTypeRollback,
	} {
		if !hasDataFactoryRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func managedDataFactoryFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:     "/subscriptions/demo/resourceGroups/rg/providers/Microsoft.DataFactory/factories/orders-factory",
		Type:   resource.TypeAzureDataFactory,
		Name:   "orders-factory",
		Region: "westeurope",
		Config: map[string]interface{}{"name": "orders-factory", "location": "westeurope"},
	}
}

func hasDataFactoryRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
