package observability

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestCloudTraceConformanceManagedAToZ(t *testing.T) {
	result, err := NewCloudTraceMapper().Map(context.Background(), managedCloudTraceFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Cloud Trace migration", result.ManualSteps)
	}
	if result.DockerService.Image != "otel/opentelemetry-collector-contrib:0.104.0" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA OpenTelemetry target: %#v", result.DockerService)
	}
	if len(result.AdditionalServices) == 0 || result.AdditionalServices[0].Image != "jaegertracing/all-in-one:1.57" {
		t.Fatalf("missing Jaeger target: %#v", result.AdditionalServices)
	}
	for _, file := range []string{"config/otel/config.yaml", "config/cloud-trace/app-change.env", "config/cloud-trace/generated-tracing.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/cloud-trace/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_CLOUD_TRACE_SERVICE=cloudtrace.googleapis.com", "TARGET_TRACING=opentelemetry", "OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4318"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_cloud_trace_config.sh", "provision_cloud_trace_otel.sh", "migrate_cloud_trace.sh", "validate_cloud_trace.sh", "backup_cloud_trace_config.sh", "cutover_cloud_trace_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-cloud-trace-config":   domainrunbook.StepTypeCommand,
		"provision-cloud-trace-otel":  domainrunbook.StepTypeCommand,
		"migrate-cloud-trace":         domainrunbook.StepTypeCommand,
		"validate-cloud-trace":        domainrunbook.StepTypeCommand,
		"backup-cloud-trace-config":   domainrunbook.StepTypeCommand,
		"cutover-cloud-trace-clients": domainrunbook.StepTypeAPICall,
		"rollback-cloud-trace-source": domainrunbook.StepTypeRollback,
	} {
		if !hasCloudTraceRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func managedCloudTraceFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "projects/demo/services/cloudtrace.googleapis.com",
		Type: resource.TypeCloudTraceService,
		Name: "cloudtrace.googleapis.com",
		Config: map[string]interface{}{
			"service": "cloudtrace.googleapis.com",
		},
	}
}

func hasCloudTraceRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
