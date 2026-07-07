package devops

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestDataprocConformanceManagedAToZ(t *testing.T) {
	result, err := NewDataprocMapper().Map(context.Background(), managedDataprocFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Dataproc migration", result.ManualSteps)
	}
	if result.DockerService.Image != "bitnami/spark:3.5" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Spark target: %#v", result.DockerService)
	}
	for _, file := range []string{"docker-compose.spark.yml", "config/dataproc/app-change.env", "config/dataproc/cluster-report.yaml"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/dataproc/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_DATAPROC_CLUSTER=orders-spark", "TARGET_SPARK_MASTER=spark://spark-master:7077"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_dataproc_cluster.sh", "migrate_dataproc_jobs.sh", "validate_dataproc_spark.sh", "backup_dataproc_config.sh", "cutover_dataproc_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-dataproc-cluster":  domainrunbook.StepTypeCommand,
		"provision-spark-cluster":  domainrunbook.StepTypeCommand,
		"migrate-dataproc-jobs":    domainrunbook.StepTypeCommand,
		"validate-dataproc-spark":  domainrunbook.StepTypeCommand,
		"backup-dataproc-config":   domainrunbook.StepTypeCommand,
		"cutover-dataproc-clients": domainrunbook.StepTypeAPICall,
		"rollback-dataproc-source": domainrunbook.StepTypeRollback,
	} {
		if !hasDataprocRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewDataprocMapper(t *testing.T) {
	m := NewDataprocMapper()
	if m == nil {
		t.Fatal("NewDataprocMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeDataprocCluster {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeDataprocCluster)
	}
}

func managedDataprocFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "projects/demo/regions/europe-west1/clusters/orders-spark",
		Type: resource.TypeDataprocCluster,
		Name: "orders-spark",
		Config: map[string]interface{}{
			"name":         "orders-spark",
			"region":       "europe-west1",
			"worker_count": "3",
		},
	}
}

func hasDataprocRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
