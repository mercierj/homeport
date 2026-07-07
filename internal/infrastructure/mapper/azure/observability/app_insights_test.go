package observability

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestAppInsightsConformanceManagedAToZ(t *testing.T) {
	result, err := NewAppInsightsMapper().Map(context.Background(), managedAppInsightsFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated App Insights migration", result.ManualSteps)
	}
	if result.DockerService.Image != "otel/opentelemetry-collector-contrib:0.103.0" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA OpenTelemetry target: %#v", result.DockerService)
	}
	for _, svc := range map[string]string{"prometheus": "prom/prometheus:v2.53.0", "tempo": "grafana/tempo:2.5.0", "grafana": "grafana/grafana:11.1.0"} {
		if !hasAppInsightsService(result, svc) {
			t.Fatalf("missing supporting service image %s: %#v", svc, result.AdditionalServices)
		}
	}
	for _, file := range []string{"config/app-insights/otel-collector.yaml", "config/app-insights/grafana-datasource.yaml", "config/app-insights/app-change.env", "config/app-insights/generated-client.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/app-insights/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_APP_INSIGHTS=checkout-ai", "OTLP_ENDPOINT=http://app-insights-otel:4318"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_app_insights.sh", "provision_app_insights_observability.sh", "validate_app_insights_observability.sh", "backup_app_insights_config.sh", "cutover_app_insights_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-app-insights":    domainrunbook.StepTypeCommand,
		"provision-app-insights": domainrunbook.StepTypeCommand,
		"validate-app-insights":  domainrunbook.StepTypeCommand,
		"backup-app-insights":    domainrunbook.StepTypeCommand,
		"cutover-app-insights":   domainrunbook.StepTypeAPICall,
		"rollback-app-insights":  domainrunbook.StepTypeRollback,
	} {
		if !hasAppInsightsRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func managedAppInsightsFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "/subscriptions/demo/resourceGroups/rg/providers/Microsoft.Insights/components/checkout-ai",
		Type: resource.TypeAppInsights,
		Name: "checkout-ai",
		Config: map[string]interface{}{
			"name":     "checkout-ai",
			"location": "westeurope",
		},
	}
}

func hasAppInsightsRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}

func hasAppInsightsService(result *mapper.MappingResult, image string) bool {
	for _, svc := range result.AdditionalServices {
		if svc.Image == image {
			return true
		}
	}
	return false
}
