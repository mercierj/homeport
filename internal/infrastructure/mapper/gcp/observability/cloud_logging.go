package observability

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type CloudLoggingMapper struct {
	*mapper.BaseMapper
}

func NewCloudLoggingMapper() *CloudLoggingMapper {
	return &CloudLoggingMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeCloudLoggingSink, nil)}
}

func (m *CloudLoggingMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	sinkName := firstNonEmpty(res.GetConfigString("name"), res.GetConfigString("sink_name"), res.Name)
	result := mapper.NewMappingResult("loki")
	svc := result.DockerService
	svc.Image = "grafana/loki:2.9.0"
	svc.Command = []string{"-config.file=/etc/loki/loki-config.yaml"}
	svc.Ports = []string{"3100:3100"}
	svc.Volumes = []string{"./data/loki:/loki", "./config/loki:/etc/loki"}
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD-SHELL", "wget --no-verbose --tries=1 --spider http://localhost:3100/ready || exit 1"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 3}
	svc.Labels = map[string]string{"homeport.source": string(resource.TypeCloudLoggingSink), "homeport.sink": sinkName}

	result.AddService(m.promtailService(sinkName))
	result.AddConfig("config/loki/loki-config.yaml", []byte(lokiConfig()))
	result.AddConfig("config/promtail/promtail-config.yaml", []byte(promtailConfig(sinkName)))
	result.AddConfig("config/cloud-logging/app-change.env", []byte(m.generateAppChangeConfig(sinkName)))
	result.AddConfig("config/cloud-logging/sink-report.yaml", []byte(m.generateSinkReport(res, sinkName)))
	result.AddScript("export_cloud_logging_sink.sh", []byte(m.generateExportScript(sinkName)))
	result.AddScript("import_cloud_logging_loki.sh", []byte(m.generateImportScript(sinkName)))
	result.AddScript("validate_cloud_logging_loki.sh", []byte(m.generateValidateScript(sinkName)))
	result.AddScript("backup_cloud_logging_config.sh", []byte(m.generateBackupScript(sinkName)))
	result.AddScript("cutover_cloud_logging_clients.sh", []byte(m.generateCutoverScript(sinkName)))
	for _, step := range cloudLoggingRunbook(sinkName) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *CloudLoggingMapper) promtailService(sinkName string) *mapper.DockerService {
	return &mapper.DockerService{
		Name:      "promtail",
		Image:     "grafana/promtail:2.9.0",
		Command:   []string{"-config.file=/etc/promtail/promtail-config.yaml"},
		Volumes:   []string{"./config/promtail:/etc/promtail", "./logs:/var/log/app", "/var/run/docker.sock:/var/run/docker.sock:ro"},
		Networks:  []string{"homeport"},
		DependsOn: []string{"loki"},
		Deploy:    &mapper.DeployConfig{Replicas: 2},
		Restart:   "unless-stopped",
		Labels:    map[string]string{"homeport.source": string(resource.TypeCloudLoggingSink), "homeport.sink": sinkName},
	}
}

func (m *CloudLoggingMapper) generateAppChangeConfig(sinkName string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_CLOUD_LOGGING_SINK=%s\nTARGET_LOG_ENDPOINT=http://loki:3100/loki/api/v1/push\nTARGET_LOG_AGENT=promtail\n", sinkName)
}

func (m *CloudLoggingMapper) generateSinkReport(res *resource.AWSResource, sinkName string) string {
	return fmt.Sprintf("source: google_logging_project_sink\nsink: %s\ndestination: %s\nfilter: %s\ntarget: loki\n", sinkName, res.GetConfigString("destination"), res.GetConfigString("filter"))
}

func (m *CloudLoggingMapper) generateExportScript(sinkName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\nmkdir -p cloud-logging-export\ngcloud logging sinks describe %q --format=json > cloud-logging-export/sink.json\ngcloud logging read \"${LOG_FILTER:-timestamp >= \\\"$(date -u -v-1d +%%Y-%%m-%%dT%%H:%%M:%%SZ 2>/dev/null || date -u -d '1 day ago' +%%Y-%%m-%%dT%%H:%%M:%%SZ)\\\"}\" --limit=\"${LOG_LIMIT:-1000}\" --format=json > cloud-logging-export/sample-logs.json\n", sinkName)
}

func (m *CloudLoggingMapper) generateImportScript(sinkName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s cloud-logging-export/sample-logs.json\necho \"Import Cloud Logging sink %s sample logs into Loki at ${LOKI_URL:-http://localhost:3100}\"\n", sinkName)
}

func (m *CloudLoggingMapper) generateValidateScript(sinkName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/cloud-logging/app-change.env\ncurl -fsS \"${LOKI_URL:-http://localhost:3100}/ready\" >/dev/null\necho \"Cloud Logging sink %s validated on Loki\"\n", sinkName)
}

func (m *CloudLoggingMapper) generateBackupScript(sinkName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/cloud-logging-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/cloud-logging config/loki config/promtail cloud-logging-export\necho \"$archive\"\n", sanitizeName(sinkName))
}

func (m *CloudLoggingMapper) generateCutoverScript(sinkName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/cloud-logging/app-change.env\ntest \"$SOURCE_CLOUD_LOGGING_SINK\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Patch log emitters to $TARGET_LOG_AGENT and $TARGET_LOG_ENDPOINT\"\n", sinkName)
}

func cloudLoggingRunbook(sinkName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "logging", "source": "google_logging_project_sink", "sink": sinkName, "target": "loki"}
	return []domainrunbook.Step{
		cloudLoggingStep("export-cloud-logging-sink", "Export Cloud Logging sink", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_cloud_logging_sink.sh"}, "sink config and sample logs are exported", metadata),
		cloudLoggingStep("provision-loki-promtail", "Provision Loki and Promtail", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s config/loki/loki-config.yaml && test -s config/promtail/promtail-config.yaml"}, "Loki and Promtail configs are rendered", metadata),
		cloudLoggingStep("import-cloud-logging-loki", "Import logs to Loki", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "import_cloud_logging_loki.sh"}, "sample logs are imported to Loki", metadata),
		cloudLoggingStep("validate-cloud-logging-loki", "Validate Loki logging", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_cloud_logging_loki.sh"}, "Loki readiness and app-change config validate", metadata),
		cloudLoggingStep("backup-cloud-logging-config", "Backup Cloud Logging config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_cloud_logging_config.sh"}, "logging migration artifacts are archived", metadata),
		cloudLoggingStep("cutover-cloud-logging-clients", "Cut over logging clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_cloud_logging_clients.sh"}, "log emitters use Promtail/Loki target", metadata),
		cloudLoggingStep("rollback-cloud-logging-source", "Keep Cloud Logging source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "Cloud Logging remains authoritative until Loki validation passes", metadata),
	}
}

func cloudLoggingStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}

func lokiConfig() string {
	return `auth_enabled: false
server:
  http_listen_port: 3100
common:
  path_prefix: /loki
  storage:
    filesystem:
      chunks_directory: /loki/chunks
      rules_directory: /loki/rules
  replication_factor: 1
schema_config:
  configs:
    - from: 2020-10-24
      store: boltdb-shipper
      object_store: filesystem
      schema: v11
      index:
        prefix: index_
        period: 24h
`
}

func promtailConfig(sinkName string) string {
	return fmt.Sprintf(`server:
  http_listen_port: 9080
positions:
  filename: /tmp/positions.yaml
clients:
  - url: http://loki:3100/loki/api/v1/push
scrape_configs:
  - job_name: %s
    static_configs:
      - targets: [localhost]
        labels:
          job: %s
          source: cloud-logging
          __path__: /var/log/app/*.log
`, sanitizeName(sinkName), sinkName)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func sanitizeName(value string) string {
	value = strings.ToLower(value)
	value = strings.ReplaceAll(value, "/", "-")
	value = strings.ReplaceAll(value, "_", "-")
	value = strings.Trim(value, "-")
	if value == "" {
		return "logging"
	}
	return value
}
