package observability

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
	"github.com/homeport/homeport/internal/infrastructure/mapper/shared/netrunbook"
)

type CloudMonitoringMapper struct {
	*mapper.BaseMapper
}

func NewCloudMonitoringAlertPolicyMapper() *CloudMonitoringMapper {
	return &CloudMonitoringMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeCloudMonitoringAlertPolicy, nil)}
}

func NewCloudMonitoringDashboardMapper() *CloudMonitoringMapper {
	return &CloudMonitoringMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeCloudMonitoringDashboard, nil)}
}

func (m *CloudMonitoringMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	switch res.Type {
	case resource.TypeCloudMonitoringAlertPolicy:
		return m.mapAlertPolicy(res), nil
	case resource.TypeCloudMonitoringDashboard:
		return m.mapDashboard(res), nil
	default:
		return nil, fmt.Errorf("unsupported Cloud Monitoring resource type: %s", res.Type)
	}
}

func (m *CloudMonitoringMapper) mapAlertPolicy(res *resource.AWSResource) *mapper.MappingResult {
	name := firstNonEmpty(res.GetConfigString("display_name"), res.GetConfigString("name"), res.Name)
	result := mapper.NewMappingResult("alertmanager")
	svc := result.DockerService
	svc.Image = "prom/alertmanager:v0.26.0"
	svc.Ports = []string{"9093:9093"}
	svc.Volumes = []string{"./config/alertmanager:/etc/alertmanager", "./data/alertmanager:/alertmanager"}
	svc.Command = []string{"--config.file=/etc/alertmanager/alertmanager.yml", "--storage.path=/alertmanager", "--cluster.listen-address="}
	svc.Networks = []string{"homeport"}
	svc.DependsOn = []string{"prometheus"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:9093/-/healthy"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 3}
	svc.Labels = map[string]string{"homeport.source": string(resource.TypeCloudMonitoringAlertPolicy), "homeport.policy": name}

	result.AddService(prometheusService(string(resource.TypeCloudMonitoringAlertPolicy)))
	result.AddConfig("config/alertmanager/alertmanager.yml", []byte(alertmanagerConfig()))
	result.AddConfig("config/prometheus/prometheus.yml", []byte(prometheusConfig()))
	result.AddConfig("config/prometheus/rules/gcp-monitoring-alerts.yml", []byte(m.generateAlertRules(res, name)))
	result.AddConfig("config/gcp-monitoring/app-change.env", []byte(m.generateAppChangeConfig(name, "alertmanager")))
	result.AddScript("scripts/export-gcp-monitoring-alerts.sh", []byte(m.generateAlertExportScript(name)))
	result.AddScript("scripts/test-gcp-monitoring-alert.sh", []byte(m.generateAlertTestScript(name)))
	result.AddScript("scripts/backup-gcp-monitoring.sh", []byte(m.generateBackupScript(name)))
	for _, step := range netrunbook.Alerts(name, string(resource.TypeCloudMonitoringAlertPolicy), "scripts/test-gcp-monitoring-alert.sh") {
		result.AddRunbookStep(step)
	}
	for _, step := range cloudMonitoringRunbook(name) {
		result.AddRunbookStep(step)
	}
	return result
}

func (m *CloudMonitoringMapper) mapDashboard(res *resource.AWSResource) *mapper.MappingResult {
	name := firstNonEmpty(res.GetConfigString("display_name"), res.GetConfigString("dashboard_name"), res.GetConfigString("name"), res.Name)
	result := mapper.NewMappingResult("grafana")
	svc := result.DockerService
	svc.Image = "grafana/grafana:10.2.0"
	svc.Ports = []string{"3000:3000"}
	svc.Volumes = []string{"./config/grafana/provisioning:/etc/grafana/provisioning", "./config/grafana/dashboards:/var/lib/grafana/dashboards", "./data/grafana:/var/lib/grafana"}
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD-SHELL", "wget --spider -q http://localhost:3000/api/health || exit 1"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 3}
	svc.Labels = map[string]string{"homeport.source": string(resource.TypeCloudMonitoringDashboard), "homeport.dashboard": name}

	result.AddService(prometheusService(string(resource.TypeCloudMonitoringDashboard)))
	result.AddConfig("config/prometheus/prometheus.yml", []byte(prometheusConfig()))
	result.AddConfig("config/grafana/provisioning/datasources/datasources.yaml", []byte(grafanaDatasourceConfig()))
	result.AddConfig("config/grafana/provisioning/dashboards/dashboards.yaml", []byte(grafanaDashboardProvisioning()))
	result.AddConfig("config/grafana/dashboards/gcp-monitoring.json", []byte(m.generateGrafanaDashboard(name)))
	result.AddConfig("config/gcp-monitoring/app-change.env", []byte(m.generateAppChangeConfig(name, "grafana")))
	result.AddScript("scripts/export-gcp-monitoring-dashboard.sh", []byte(m.generateDashboardExportScript(name)))
	result.AddScript("scripts/import-gcp-monitoring-dashboard.sh", []byte(m.generateDashboardImportScript(name)))
	result.AddScript("scripts/backup-gcp-monitoring.sh", []byte(m.generateBackupScript(name)))
	for _, step := range netrunbook.Observability(name, string(resource.TypeCloudMonitoringDashboard)) {
		result.AddRunbookStep(step)
	}
	for _, step := range cloudMonitoringRunbook(name) {
		result.AddRunbookStep(step)
	}
	return result
}

func prometheusService(source string) *mapper.DockerService {
	return &mapper.DockerService{
		Name:        "prometheus",
		Image:       "prom/prometheus:v2.47.0",
		Ports:       []string{"9090:9090"},
		Volumes:     []string{"./config/prometheus:/etc/prometheus", "./data/prometheus:/prometheus"},
		Command:     []string{"--config.file=/etc/prometheus/prometheus.yml", "--storage.tsdb.path=/prometheus", "--web.enable-lifecycle"},
		Networks:    []string{"homeport"},
		Deploy:      &mapper.DeployConfig{Replicas: 2},
		Restart:     "unless-stopped",
		Labels:      map[string]string{"homeport.source": source},
		HealthCheck: &mapper.HealthCheck{Test: []string{"CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:9090/-/healthy"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 3},
	}
}

func (m *CloudMonitoringMapper) generateAlertRules(res *resource.AWSResource, name string) string {
	filter := firstNonEmpty(res.GetConfigString("filter"), res.GetConfigString("condition_filter"), "metric.type=\"custom.googleapis.com/app/health\"")
	metric := sanitizeMonitoringMetric(filter)
	return fmt.Sprintf(`groups:
  - name: gcp-monitoring
    rules:
      - alert: %s
        expr: %s > 0
        for: 5m
        labels:
          severity: warning
          source: google_monitoring_alert_policy
        annotations:
          summary: "%s"
`, sanitizeAlertName(name), metric, name)
}

func alertmanagerConfig() string {
	return `global:
  resolve_timeout: 5m
route:
  receiver: default
receivers:
  - name: default
`
}

func prometheusConfig() string {
	return `global:
  scrape_interval: 30s
rule_files:
  - /etc/prometheus/rules/*.yml
scrape_configs:
  - job_name: prometheus
    static_configs:
      - targets: ["localhost:9090"]
`
}

func grafanaDatasourceConfig() string {
	return `apiVersion: 1
datasources:
  - name: Prometheus
    type: prometheus
    access: proxy
    url: http://prometheus:9090
    isDefault: true
`
}

func grafanaDashboardProvisioning() string {
	return `apiVersion: 1
providers:
  - name: gcp-monitoring
    type: file
    options:
      path: /var/lib/grafana/dashboards
`
}

func (m *CloudMonitoringMapper) generateGrafanaDashboard(name string) string {
	return fmt.Sprintf(`{"title":"%s","schemaVersion":39,"panels":[{"type":"timeseries","title":"Migrated Cloud Monitoring metrics","targets":[{"expr":"up"}]}]}`, name)
}

func (m *CloudMonitoringMapper) generateAppChangeConfig(name, target string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_CLOUD_MONITORING=%s\nTARGET_OBSERVABILITY=%s\n", name, target)
}

func (m *CloudMonitoringMapper) generateAlertExportScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\nmkdir -p gcp-monitoring-export\ngcloud monitoring policies list --format=json > gcp-monitoring-export/alert-policies.json\necho %q > gcp-monitoring-export/source.txt\n", name)
}

func (m *CloudMonitoringMapper) generateDashboardExportScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\nmkdir -p gcp-monitoring-export\ngcloud monitoring dashboards list --format=json > gcp-monitoring-export/dashboards.json\necho %q > gcp-monitoring-export/source.txt\n", name)
}

func (m *CloudMonitoringMapper) generateDashboardImportScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/grafana/dashboards/gcp-monitoring.json\necho \"Import Cloud Monitoring dashboard %s into Grafana\"\n", name)
}

func (m *CloudMonitoringMapper) generateAlertTestScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/prometheus/rules/gcp-monitoring-alerts.yml\necho \"Cloud Monitoring alert policy %s validated\"\n", name)
}

func (m *CloudMonitoringMapper) generateBackupScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/gcp-monitoring-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/gcp-monitoring config/prometheus config/alertmanager config/grafana gcp-monitoring-export 2>/dev/null || tar -czf \"$archive\" config/gcp-monitoring config/prometheus\necho \"$archive\"\n", sanitizeName(name))
}

func cloudMonitoringRunbook(name string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "observability", "source": "gcp-monitoring", "name": name}
	return []domainrunbook.Step{
		cloudMonitoringStep("backup-gcp-monitoring-config", "Backup GCP Monitoring config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "scripts/backup-gcp-monitoring.sh"}, "monitoring configs are archived", metadata),
		cloudMonitoringStep("cutover-gcp-monitoring-clients", "Cut over monitoring clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "-c", "test -s config/gcp-monitoring/app-change.env"}, "applications use generated observability target", metadata),
	}
}

func cloudMonitoringStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: "shell", Command: command, SuccessCondition: success, Metadata: metadata}
}

func sanitizeAlertName(value string) string {
	value = strings.ReplaceAll(sanitizeName(value), "-", "_")
	if value == "" {
		return "GCPMonitoringAlert"
	}
	return strings.Title(value)
}

func sanitizeMonitoringMetric(filter string) string {
	filter = strings.ToLower(filter)
	switch {
	case strings.Contains(filter, "cpu"):
		return "node_cpu_seconds_total"
	case strings.Contains(filter, "memory"):
		return "node_memory_MemAvailable_bytes"
	case strings.Contains(filter, "request"):
		return "http_requests_total"
	default:
		return "up"
	}
}
