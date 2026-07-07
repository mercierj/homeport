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

type LogAnalyticsMapper struct {
	*mapper.BaseMapper
}

func NewLogAnalyticsMapper() *LogAnalyticsMapper {
	return &LogAnalyticsMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeAzureLogAnalytics, nil)}
}

func (m *LogAnalyticsMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	workspaceName := logAnalyticsFirstNonEmpty(res.GetConfigString("name"), res.GetConfigString("workspace_name"), res.Name)
	location := logAnalyticsFirstNonEmpty(res.GetConfigString("location"), res.Region)

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
	svc.Labels = map[string]string{"homeport.source": string(resource.TypeAzureLogAnalytics), "homeport.workspace": workspaceName, "homeport.target": "loki"}

	result.AddService(m.collectorService(workspaceName))
	result.AddConfig("config/loki/loki-config.yaml", []byte(logAnalyticsLokiConfig()))
	result.AddConfig("config/log-analytics/otel-collector.yaml", []byte(logAnalyticsCollectorConfig(workspaceName)))
	result.AddConfig("config/log-analytics/app-change.env", []byte(m.generateAppChangeConfig(workspaceName)))
	result.AddConfig("config/log-analytics/workspace-report.yaml", []byte(m.generateWorkspaceReport(res, workspaceName, location)))
	result.AddScript("export_log_analytics_workspace.sh", []byte(m.generateExportScript(workspaceName)))
	result.AddScript("import_log_analytics_loki.sh", []byte(m.generateImportScript(workspaceName)))
	result.AddScript("validate_log_analytics_loki.sh", []byte(m.generateValidateScript(workspaceName)))
	result.AddScript("backup_log_analytics_config.sh", []byte(m.generateBackupScript(workspaceName)))
	result.AddScript("cutover_log_analytics_clients.sh", []byte(m.generateCutoverScript(workspaceName)))
	for _, step := range logAnalyticsRunbook(workspaceName) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *LogAnalyticsMapper) collectorService(workspaceName string) *mapper.DockerService {
	return &mapper.DockerService{
		Name:      "otel-collector",
		Image:     "otel/opentelemetry-collector-contrib:0.103.0",
		Command:   []string{"--config=/etc/otelcol-contrib/config.yaml"},
		Ports:     []string{"4317:4317", "4318:4318", "8888:8888"},
		Volumes:   []string{"./config/log-analytics/otel-collector.yaml:/etc/otelcol-contrib/config.yaml:ro", "./logs:/var/log/app"},
		Networks:  []string{"homeport"},
		DependsOn: []string{"loki"},
		Deploy:    &mapper.DeployConfig{Replicas: 2},
		Restart:   "unless-stopped",
		Labels:    map[string]string{"homeport.source": string(resource.TypeAzureLogAnalytics), "homeport.workspace": workspaceName, "homeport.component": "otel-collector"},
	}
}

func (m *LogAnalyticsMapper) generateAppChangeConfig(workspaceName string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_LOG_ANALYTICS_WORKSPACE=%s\nTARGET_LOG_ENDPOINT=http://loki:3100/loki/api/v1/push\nTARGET_LOG_AGENT=otel-collector\nOTLP_ENDPOINT=http://otel-collector:4318\n", workspaceName)
}

func (m *LogAnalyticsMapper) generateWorkspaceReport(res *resource.AWSResource, workspaceName, location string) string {
	return fmt.Sprintf("source: azurerm_log_analytics_workspace\nworkspace: %s\nlocation: %s\nresource_group: %s\nretention_days: %d\ntarget: loki\n", workspaceName, location, res.GetConfigString("resource_group_name"), res.GetConfigInt("retention_in_days"))
}

func (m *LogAnalyticsMapper) generateExportScript(workspaceName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\nWORKSPACE_NAME=%q\nOUTPUT_DIR=\"${OUTPUT_DIR:-./log-analytics-export}\"\nmkdir -p \"$OUTPUT_DIR\"\naz monitor log-analytics workspace show --workspace-name \"$WORKSPACE_NAME\" --resource-group \"${AZURE_RESOURCE_GROUP}\" > \"$OUTPUT_DIR/workspace.json\"\naz monitor log-analytics query --workspace \"$WORKSPACE_NAME\" --analytics-query \"${LOG_ANALYTICS_QUERY:-Heartbeat | take 100}\" > \"$OUTPUT_DIR/sample-logs.json\"\n", workspaceName)
}

func (m *LogAnalyticsMapper) generateImportScript(workspaceName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s log-analytics-export/sample-logs.json\necho \"Import Log Analytics workspace %s sample logs into Loki at ${LOKI_URL:-http://localhost:3100}\"\n", workspaceName)
}

func (m *LogAnalyticsMapper) generateValidateScript(workspaceName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/log-analytics/app-change.env\ntest -s config/log-analytics/otel-collector.yaml\ngrep -q %q config/log-analytics/app-change.env\ncurl -fsS \"${LOKI_URL:-http://localhost:3100}/ready\" >/dev/null\n", workspaceName)
}

func (m *LogAnalyticsMapper) generateBackupScript(workspaceName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/log-analytics-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/log-analytics config/loki log-analytics-export 2>/dev/null || tar -czf \"$archive\" config/log-analytics config/loki\necho \"$archive\"\n", logAnalyticsSanitizeName(workspaceName))
}

func (m *LogAnalyticsMapper) generateCutoverScript(workspaceName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/log-analytics/app-change.env\ntest \"$SOURCE_LOG_ANALYTICS_WORKSPACE\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Patch log emitters to $TARGET_LOG_AGENT and $TARGET_LOG_ENDPOINT\"\n", workspaceName)
}

func logAnalyticsRunbook(workspaceName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "logging", "source": "azurerm_log_analytics_workspace", "workspace": workspaceName, "target": "loki-otel"}
	return []domainrunbook.Step{
		logAnalyticsStep("export-log-analytics-workspace", "Export Log Analytics workspace", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_log_analytics_workspace.sh"}, "workspace config and sample logs are exported", metadata),
		logAnalyticsStep("provision-log-analytics-loki", "Provision Loki and collector", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s config/loki/loki-config.yaml && test -s config/log-analytics/otel-collector.yaml"}, "Loki and collector configs are rendered", metadata),
		logAnalyticsStep("import-log-analytics-loki", "Import logs to Loki", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "import_log_analytics_loki.sh"}, "sample logs are imported to Loki", metadata),
		logAnalyticsStep("validate-log-analytics-loki", "Validate Log Analytics target", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_log_analytics_loki.sh"}, "Loki readiness and app-change config validate", metadata),
		logAnalyticsStep("backup-log-analytics-config", "Backup Log Analytics config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_log_analytics_config.sh"}, "logging migration artifacts are archived", metadata),
		logAnalyticsStep("cutover-log-analytics-clients", "Cut over logging clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_log_analytics_clients.sh"}, "log emitters use collector/Loki target", metadata),
		logAnalyticsStep("rollback-log-analytics-source", "Keep Log Analytics source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "Log Analytics remains authoritative until Loki validation passes", metadata),
	}
}

func logAnalyticsStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}

func logAnalyticsLokiConfig() string {
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

func logAnalyticsCollectorConfig(workspaceName string) string {
	return fmt.Sprintf(`receivers:
  filelog:
    include: [/var/log/app/*.log]
processors:
  resource/loganalytics:
    attributes:
      - key: source.log_analytics_workspace
        value: %s
        action: upsert
  batch: {}
exporters:
  loki:
    endpoint: http://loki:3100/loki/api/v1/push
service:
  pipelines:
    logs:
      receivers: [filelog]
      processors: [resource/loganalytics, batch]
      exporters: [loki]
`, workspaceName)
}

func logAnalyticsFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return "log-analytics"
}

func logAnalyticsSanitizeName(value string) string {
	value = strings.ToLower(value)
	value = strings.ReplaceAll(value, "/", "-")
	value = strings.ReplaceAll(value, "_", "-")
	value = strings.ReplaceAll(value, " ", "-")
	if value == "" {
		return "log-analytics"
	}
	return value
}
