package devops

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestDataflowConformanceManagedAToZ(t *testing.T) {
	result, err := NewDataflowMapper().Map(context.Background(), managedDataflowFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Dataflow migration", result.ManualSteps)
	}
	if result.DockerService.Image != "apache/flink:1.19" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Flink target: %#v", result.DockerService)
	}
	for _, file := range []string{"docker-compose.flink.yml", "config/dataflow/app-change.env", "config/dataflow/job-report.yaml"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/dataflow/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_DATAFLOW_JOB=orders-stream", "TARGET_RUNNER=apache-flink"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_dataflow_job.sh", "migrate_dataflow_pipeline.sh", "validate_dataflow_flink.sh", "backup_dataflow_config.sh", "cutover_dataflow_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-dataflow-job":       domainrunbook.StepTypeCommand,
		"provision-flink-runner":    domainrunbook.StepTypeCommand,
		"migrate-dataflow-pipeline": domainrunbook.StepTypeCommand,
		"validate-dataflow-flink":   domainrunbook.StepTypeCommand,
		"backup-dataflow-config":    domainrunbook.StepTypeCommand,
		"cutover-dataflow-clients":  domainrunbook.StepTypeAPICall,
		"rollback-dataflow-source":  domainrunbook.StepTypeRollback,
	} {
		if !hasDataflowRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewDataflowMapper(t *testing.T) {
	m := NewDataflowMapper()
	if m == nil {
		t.Fatal("NewDataflowMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeDataflowJob {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeDataflowJob)
	}
}

func managedDataflowFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "projects/demo/locations/europe-west1/jobs/orders-stream",
		Type: resource.TypeDataflowJob,
		Name: "orders-stream",
		Config: map[string]interface{}{
			"name":              "orders-stream",
			"region":            "europe-west1",
			"template_gcs_path": "gs://templates/orders",
		},
	}
}

func hasDataflowRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
