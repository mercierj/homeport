package observability

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestCloudLoggingConformanceManagedAToZ(t *testing.T) {
	result, err := NewCloudLoggingMapper().Map(context.Background(), managedCloudLoggingFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Cloud Logging migration", result.ManualSteps)
	}
	if result.DockerService.Image != "grafana/loki:2.9.0" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Loki target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/loki/loki-config.yaml", "config/promtail/promtail-config.yaml", "config/cloud-logging/app-change.env", "config/cloud-logging/sink-report.yaml"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/cloud-logging/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_CLOUD_LOGGING_SINK=orders-logs", "TARGET_LOG_AGENT=promtail"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_cloud_logging_sink.sh", "import_cloud_logging_loki.sh", "validate_cloud_logging_loki.sh", "backup_cloud_logging_config.sh", "cutover_cloud_logging_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-cloud-logging-sink":     domainrunbook.StepTypeCommand,
		"provision-loki-promtail":       domainrunbook.StepTypeCommand,
		"import-cloud-logging-loki":     domainrunbook.StepTypeCommand,
		"validate-cloud-logging-loki":   domainrunbook.StepTypeCommand,
		"backup-cloud-logging-config":   domainrunbook.StepTypeCommand,
		"cutover-cloud-logging-clients": domainrunbook.StepTypeAPICall,
		"rollback-cloud-logging-source": domainrunbook.StepTypeRollback,
	} {
		if !hasCloudLoggingRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewCloudLoggingMapper(t *testing.T) {
	m := NewCloudLoggingMapper()
	if m == nil {
		t.Fatal("NewCloudLoggingMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeCloudLoggingSink {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeCloudLoggingSink)
	}
}

func managedCloudLoggingFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "projects/demo/sinks/orders-logs",
		Type: resource.TypeCloudLoggingSink,
		Name: "orders-logs",
		Config: map[string]interface{}{
			"name":        "orders-logs",
			"destination": "storage.googleapis.com/orders-log-archive",
			"filter":      "resource.type=\"cloud_run_revision\"",
		},
	}
}

func hasCloudLoggingRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
