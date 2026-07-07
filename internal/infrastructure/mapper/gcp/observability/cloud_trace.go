package observability

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type CloudTraceMapper struct {
	*mapper.BaseMapper
}

func NewCloudTraceMapper() *CloudTraceMapper {
	return &CloudTraceMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeCloudTraceService, nil)}
}

func (m *CloudTraceMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	serviceName := firstNonEmpty(res.GetConfigString("service"), res.GetConfigString("name"), "cloudtrace.googleapis.com")
	result := mapper.NewMappingResult("otel-collector")
	svc := result.DockerService
	svc.Image = "otel/opentelemetry-collector-contrib:0.104.0"
	svc.Command = []string{"--config=/etc/otelcol/config.yaml"}
	svc.Ports = []string{"4317:4317", "4318:4318", "13133:13133"}
	svc.Volumes = []string{"./config/otel:/etc/otelcol", "./data/otel:/var/lib/otel"}
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{"homeport.source": string(resource.TypeCloudTraceService), "homeport.service": serviceName, "homeport.target": "opentelemetry"}
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD", "wget", "--spider", "-q", "http://localhost:13133/"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 5}

	result.AddService(&mapper.DockerService{Name: "jaeger", Image: "jaegertracing/all-in-one:1.57", Ports: []string{"16686:16686", "14250:14250"}, Networks: []string{"homeport"}, Deploy: &mapper.DeployConfig{Replicas: 2}, Restart: "unless-stopped"})
	result.AddConfig("config/otel/config.yaml", []byte(m.otelConfig(serviceName)))
	result.AddConfig("config/cloud-trace/app-change.env", []byte(m.appChange(serviceName)))
	result.AddConfig("config/cloud-trace/generated-tracing.patch", []byte(m.generatedPatch(serviceName)))
	result.AddScript("export_cloud_trace_config.sh", []byte(m.exportScript(serviceName)))
	result.AddScript("provision_cloud_trace_otel.sh", []byte(m.provisionScript(serviceName)))
	result.AddScript("migrate_cloud_trace.sh", []byte(m.migrateScript(serviceName)))
	result.AddScript("validate_cloud_trace.sh", []byte(m.validateScript(serviceName)))
	result.AddScript("backup_cloud_trace_config.sh", []byte(m.backupScript(serviceName)))
	result.AddScript("cutover_cloud_trace_clients.sh", []byte(m.cutoverScript(serviceName)))
	for _, step := range cloudTraceRunbook(serviceName) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *CloudTraceMapper) otelConfig(serviceName string) string {
	return fmt.Sprintf(`receivers:
  otlp:
    protocols:
      grpc:
      http:
processors:
  batch:
exporters:
  otlp/jaeger:
    endpoint: jaeger:14250
    tls:
      insecure: true
  logging:
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [otlp/jaeger, logging]
extensions:
  health_check:
    endpoint: 0.0.0.0:13133
# source_cloud_trace_service: %s
`, serviceName)
}

func (m *CloudTraceMapper) appChange(serviceName string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_CLOUD_TRACE_SERVICE=%s\nTARGET_TRACING=opentelemetry\nOTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4318\nJAEGER_UI=http://jaeger:16686\nGENERATED_PATCH=config/cloud-trace/generated-tracing.patch\n", serviceName)
}

func (m *CloudTraceMapper) generatedPatch(serviceName string) string {
	return fmt.Sprintf("--- a/app/tracing.env\n+++ b/app/tracing.env\n@@\n-GOOGLE_CLOUD_TRACE_SERVICE=%s\n+OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4318\n+TRACING_BACKEND=opentelemetry\n", serviceName)
}

func (m *CloudTraceMapper) exportScript(serviceName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\nmkdir -p cloud-trace-export\ngcloud services list --enabled --filter='config.name=%s' --format=json > cloud-trace-export/service.json\n", serviceName)
}

func (m *CloudTraceMapper) provisionScript(serviceName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/otel/config.yaml\necho \"OpenTelemetry collector ready for Cloud Trace service %s\"\n", serviceName)
}

func (m *CloudTraceMapper) migrateScript(serviceName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ngrep -q %q config/otel/config.yaml\necho \"Cloud Trace service %s mapped to OpenTelemetry\"\n", serviceName, serviceName)
}

func (m *CloudTraceMapper) validateScript(serviceName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\nwget --spider -q http://localhost:13133/\ntest -s config/cloud-trace/app-change.env\ngrep -q %q config/otel/config.yaml\n", serviceName)
}

func (m *CloudTraceMapper) backupScript(serviceName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/cloud-trace-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/otel config/cloud-trace cloud-trace-export\necho \"$archive\"\n", sanitizeName(serviceName))
}

func (m *CloudTraceMapper) cutoverScript(serviceName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/cloud-trace/app-change.env\ntest \"$SOURCE_CLOUD_TRACE_SERVICE\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\ntest -s \"$GENERATED_PATCH\"\necho \"Apply $GENERATED_PATCH and send traces to $OTEL_EXPORTER_OTLP_ENDPOINT\"\n", serviceName)
}

func cloudTraceRunbook(serviceName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "tracing", "source": "google_project_service", "service": serviceName, "target": "opentelemetry"}
	return []domainrunbook.Step{
		cloudTraceStep("export-cloud-trace-config", "Export Cloud Trace config", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_cloud_trace_config.sh"}, "Cloud Trace service config is exported", metadata),
		cloudTraceStep("provision-cloud-trace-otel", "Provision OpenTelemetry collector", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "provision_cloud_trace_otel.sh"}, "OpenTelemetry collector and Jaeger are configured", metadata),
		cloudTraceStep("migrate-cloud-trace", "Migrate Cloud Trace config", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_cloud_trace.sh"}, "Cloud Trace handoff is represented in OpenTelemetry config", metadata),
		cloudTraceStep("validate-cloud-trace", "Validate trace ingestion", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_cloud_trace.sh"}, "collector health and generated tracing config validate", metadata),
		cloudTraceStep("backup-cloud-trace-config", "Backup Cloud Trace config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_cloud_trace_config.sh"}, "tracing migration artifacts are archived", metadata),
		cloudTraceStep("cutover-cloud-trace-clients", "Cut over Cloud Trace clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_cloud_trace_clients.sh"}, "clients use generated OpenTelemetry tracing patch", metadata),
		cloudTraceStep("rollback-cloud-trace-source", "Keep Cloud Trace source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "Cloud Trace remains authoritative until OpenTelemetry validation passes", metadata),
	}
}

func cloudTraceStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}
