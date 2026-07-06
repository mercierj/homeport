package monitoring

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestXRayConformanceManagedAToZ(t *testing.T) {
	result, err := NewXRayMapper().Map(context.Background(), managedXRayFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated X-Ray migration", result.ManualSteps)
	}
	if result.DockerService.Image != "otel/opentelemetry-collector-contrib:0.104.0" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA OpenTelemetry target: %#v", result.DockerService)
	}
	if len(result.AdditionalServices) == 0 || result.AdditionalServices[0].Image != "jaegertracing/all-in-one:1.57" {
		t.Fatalf("missing Jaeger target: %#v", result.AdditionalServices)
	}
	for _, file := range []string{"config/otel/config.yaml", "config/xray/sampling-rules.json", "config/xray/app-change.env", "config/xray/generated-tracing.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/xray/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_XRAY_SAMPLING_RULE=prod-sampling", "TARGET_TRACING=opentelemetry", "OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4318"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_xray_sampling_rule.sh", "provision_otel_collector.sh", "migrate_xray_sampling.sh", "validate_xray_traces.sh", "backup_xray_config.sh", "cutover_xray_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-xray-sampling-rule": domainrunbook.StepTypeCommand,
		"provision-otel-collector":  domainrunbook.StepTypeCommand,
		"migrate-xray-sampling":     domainrunbook.StepTypeCommand,
		"validate-xray-traces":      domainrunbook.StepTypeCommand,
		"backup-xray-config":        domainrunbook.StepTypeCommand,
		"cutover-xray-clients":      domainrunbook.StepTypeAPICall,
		"rollback-xray-source":      domainrunbook.StepTypeRollback,
	} {
		if !hasXRayRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewXRayMapper(t *testing.T) {
	m := NewXRayMapper()
	if m == nil {
		t.Fatal("NewXRayMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeXRaySamplingRule {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeXRaySamplingRule)
	}
}

func managedXRayFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:     "prod-sampling",
		Type:   resource.TypeXRaySamplingRule,
		Name:   "prod-sampling",
		Region: "eu-west-1",
		Config: map[string]interface{}{
			"rule_name": "prod-sampling",
		},
	}
}

func hasXRayRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
