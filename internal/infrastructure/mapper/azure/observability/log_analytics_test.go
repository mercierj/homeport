package observability

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestLogAnalyticsConformanceManagedAToZ(t *testing.T) {
	result, err := NewLogAnalyticsMapper().Map(context.Background(), managedLogAnalyticsFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Log Analytics migration", result.ManualSteps)
	}
	if result.DockerService.Image != "grafana/loki:2.9.0" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Loki target: %#v", result.DockerService)
	}
	if !hasLogAnalyticsService(result, "otel/opentelemetry-collector-contrib:0.103.0") {
		t.Fatalf("missing generated OpenTelemetry Collector service: %#v", result.AdditionalServices)
	}
	for _, file := range []string{"config/loki/loki-config.yaml", "config/log-analytics/otel-collector.yaml", "config/log-analytics/app-change.env", "config/log-analytics/workspace-report.yaml"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/log-analytics/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_LOG_ANALYTICS_WORKSPACE=orders-law", "TARGET_LOG_ENDPOINT=http://loki:3100/loki/api/v1/push", "TARGET_LOG_AGENT=otel-collector"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_log_analytics_workspace.sh", "import_log_analytics_loki.sh", "validate_log_analytics_loki.sh", "backup_log_analytics_config.sh", "cutover_log_analytics_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-log-analytics-workspace": domainrunbook.StepTypeCommand,
		"provision-log-analytics-loki":   domainrunbook.StepTypeCommand,
		"import-log-analytics-loki":      domainrunbook.StepTypeCommand,
		"validate-log-analytics-loki":    domainrunbook.StepTypeCommand,
		"backup-log-analytics-config":    domainrunbook.StepTypeCommand,
		"cutover-log-analytics-clients":  domainrunbook.StepTypeAPICall,
		"rollback-log-analytics-source":  domainrunbook.StepTypeRollback,
	} {
		if !hasLogAnalyticsRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewLogAnalyticsMapper(t *testing.T) {
	m := NewLogAnalyticsMapper()
	if m == nil {
		t.Fatal("NewLogAnalyticsMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeAzureLogAnalytics {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeAzureLogAnalytics)
	}
}

func managedLogAnalyticsFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "/subscriptions/demo/resourceGroups/rg/providers/Microsoft.OperationalInsights/workspaces/orders-law",
		Type: resource.TypeAzureLogAnalytics,
		Name: "orders-law",
		Config: map[string]interface{}{
			"name":                "orders-law",
			"location":            "westeurope",
			"resource_group_name": "orders-rg",
			"retention_in_days":   30,
		},
	}
}

func hasLogAnalyticsRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}

func hasLogAnalyticsService(result *mapper.MappingResult, image string) bool {
	for _, svc := range result.AdditionalServices {
		if svc.Image == image {
			return true
		}
	}
	return false
}
