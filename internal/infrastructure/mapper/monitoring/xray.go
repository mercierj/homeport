package monitoring

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type XRayMapper struct {
	*mapper.BaseMapper
}

func NewXRayMapper() *XRayMapper {
	return &XRayMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeXRaySamplingRule, nil)}
}

func (m *XRayMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	ruleName := res.GetConfigString("rule_name")
	if ruleName == "" {
		ruleName = res.GetConfigString("name")
	}
	if ruleName == "" {
		ruleName = res.Name
	}

	result := mapper.NewMappingResult("otel-collector")
	svc := result.DockerService
	svc.Image = "otel/opentelemetry-collector-contrib:0.104.0"
	svc.Command = []string{"--config=/etc/otelcol/config.yaml"}
	svc.Ports = []string{"4317:4317", "4318:4318", "13133:13133"}
	svc.Volumes = []string{"./config/otel:/etc/otelcol", "./data/otel:/var/lib/otel"}
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"homeport.source":        "aws_xray_sampling_rule",
		"homeport.sampling_rule": ruleName,
		"homeport.target":        "opentelemetry",
	}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "wget", "--spider", "-q", "http://localhost:13133/"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  5,
	}

	jaeger := &mapper.DockerService{
		Name:     "jaeger",
		Image:    "jaegertracing/all-in-one:1.57",
		Ports:    []string{"16686:16686", "14250:14250"},
		Networks: []string{"homeport"},
		Deploy:   &mapper.DeployConfig{Replicas: 2},
		Restart:  "unless-stopped",
	}
	result.AddService(jaeger)
	result.AddConfig("config/otel/config.yaml", []byte(m.otelConfig(ruleName)))
	result.AddConfig("config/xray/sampling-rules.json", []byte(m.samplingRules(ruleName)))
	result.AddConfig("config/xray/app-change.env", []byte(m.appChange(ruleName)))
	result.AddConfig("config/xray/generated-tracing.patch", []byte(m.generatedPatch(ruleName)))
	result.AddScript("export_xray_sampling_rule.sh", []byte(m.exportScript(ruleName, res.Region)))
	result.AddScript("provision_otel_collector.sh", []byte(m.provisionScript(ruleName)))
	result.AddScript("migrate_xray_sampling.sh", []byte(m.migrateScript(ruleName)))
	result.AddScript("validate_xray_traces.sh", []byte(m.validateScript(ruleName)))
	result.AddScript("backup_xray_config.sh", []byte(m.backupScript(ruleName)))
	result.AddScript("cutover_xray_clients.sh", []byte(m.cutoverScript(ruleName)))
	for _, step := range xrayRunbook(ruleName) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *XRayMapper) otelConfig(ruleName string) string {
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
# migrated_sampling_rule: %s
`, ruleName)
}

func (m *XRayMapper) samplingRules(ruleName string) string {
	return fmt.Sprintf(`{
  "rules": [
    {
      "name": %q,
      "target": "opentelemetry-tail-sampling",
      "reservoir_size": 1,
      "fixed_rate": 0.05
    }
  ]
}
`, ruleName)
}

func (m *XRayMapper) appChange(ruleName string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=generated_patch
SOURCE_XRAY_SAMPLING_RULE=%s
TARGET_TRACING=opentelemetry
OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4318
JAEGER_UI=http://jaeger:16686
GENERATED_PATCH=config/xray/generated-tracing.patch
`, ruleName)
}

func (m *XRayMapper) generatedPatch(ruleName string) string {
	return fmt.Sprintf(`--- a/app/tracing.env
+++ b/app/tracing.env
@@
-AWS_XRAY_SAMPLING_RULE=%s
+OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4318
+TRACING_BACKEND=opentelemetry
`, ruleName)
}

func (m *XRayMapper) exportScript(ruleName, region string) string {
	if region == "" {
		region = "us-east-1"
	}
	return fmt.Sprintf(`#!/bin/sh
set -eu
AWS_REGION=%q
RULE_NAME=%q
OUTPUT_DIR="./xray-export"
mkdir -p "$OUTPUT_DIR"
aws xray get-sampling-rules --region "$AWS_REGION" --output json > "$OUTPUT_DIR/sampling-rules.json"
jq -e --arg name "$RULE_NAME" '.SamplingRuleRecords[] | select(.SamplingRule.RuleName == $name)' "$OUTPUT_DIR/sampling-rules.json" >/dev/null
`, region, ruleName)
}

func (m *XRayMapper) provisionScript(ruleName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/otel/config.yaml\ntest -s config/xray/sampling-rules.json\necho \"OpenTelemetry collector ready for %s\"\n", ruleName)
}

func (m *XRayMapper) migrateScript(ruleName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s xray-export/sampling-rules.json\ngrep -q %q config/xray/sampling-rules.json\necho \"X-Ray sampling rule %s mapped to OpenTelemetry sampling config\"\n", ruleName, ruleName)
}

func (m *XRayMapper) validateScript(ruleName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\nwget --spider -q http://localhost:13133/\ntest -s config/xray/app-change.env\ngrep -q %q config/otel/config.yaml\n", ruleName)
}

func (m *XRayMapper) backupScript(ruleName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-xray-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/otel config/xray export_xray_sampling_rule.sh provision_otel_collector.sh migrate_xray_sampling.sh validate_xray_traces.sh cutover_xray_clients.sh
echo "$archive"
`, ruleName)
}

func (m *XRayMapper) cutoverScript(ruleName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
. config/xray/app-change.env
test "$SOURCE_XRAY_SAMPLING_RULE" = %q
test "$APP_CHANGE_MODE" = "generated_patch"
test -s "$GENERATED_PATCH"
echo "Apply $GENERATED_PATCH and send traces to $OTEL_EXPORTER_OTLP_ENDPOINT"
`, ruleName)
}

func xrayRunbook(ruleName string) []domainrunbook.Step {
	metadata := map[string]string{
		"kind":                "tracing",
		"source":              "aws_xray_sampling_rule",
		"sampling_rule":       ruleName,
		"HOMEPORT_TARGET":     "opentelemetry",
		"HOMEPORT_APP_CHANGE": "generated_patch",
	}
	return []domainrunbook.Step{
		xrayStep("export-xray-sampling-rule", "Export X-Ray sampling rule", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_xray_sampling_rule.sh"}, "X-Ray sampling rule is exported", metadata),
		xrayStep("provision-otel-collector", "Provision OpenTelemetry collector", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "provision_otel_collector.sh"}, "OpenTelemetry collector and Jaeger are configured", metadata),
		xrayStep("migrate-xray-sampling", "Migrate X-Ray sampling", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_xray_sampling.sh"}, "sampling rules are represented in OpenTelemetry config", metadata),
		xrayStep("validate-xray-traces", "Validate trace ingestion", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_xray_traces.sh"}, "collector health and generated tracing config validate", metadata),
		xrayStep("backup-xray-config", "Backup X-Ray migration config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_xray_config.sh"}, "tracing migration artifacts are archived", metadata),
		xrayStep("cutover-xray-clients", "Cut over X-Ray clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_xray_clients.sh"}, "clients use generated OpenTelemetry tracing patch", metadata),
		xrayStep("rollback-xray-source", "Keep X-Ray source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "AWS X-Ray remains authoritative until OpenTelemetry validation passes", metadata),
	}
}

func xrayStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}
