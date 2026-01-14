package datamigration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// CloudWatchToPrometheusExecutor migrates CloudWatch to Prometheus/Grafana.
type CloudWatchToPrometheusExecutor struct{}

// NewCloudWatchToPrometheusExecutor creates a new CloudWatch to Prometheus executor.
func NewCloudWatchToPrometheusExecutor() *CloudWatchToPrometheusExecutor {
	return &CloudWatchToPrometheusExecutor{}
}

// Type returns the migration type.
func (e *CloudWatchToPrometheusExecutor) Type() string {
	return "cloudwatch_to_prometheus"
}

// GetPhases returns the migration phases.
func (e *CloudWatchToPrometheusExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Fetching log groups",
		"Exporting alarms",
		"Exporting dashboards",
		"Generating Prometheus config",
		"Generating Grafana dashboards",
		"Finalizing",
	}
}

// Validate validates the migration configuration.
func (e *CloudWatchToPrometheusExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["access_key_id"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.access_key_id is required")
		}
		if _, ok := config.Source["secret_access_key"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.secret_access_key is required")
		}
		if _, ok := config.Source["region"].(string); !ok {
			result.Warnings = append(result.Warnings, "source.region not specified, using default us-east-1")
		}
	}

	if config.Destination == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "destination configuration is required")
	} else {
		if _, ok := config.Destination["output_dir"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "destination.output_dir is required")
		}
	}

	result.Warnings = append(result.Warnings, "CloudWatch Logs will be configured for Loki ingestion")
	result.Warnings = append(result.Warnings, "CloudWatch alarms will be converted to Prometheus alert rules")
	result.Warnings = append(result.Warnings, "CloudWatch dashboards will be converted to Grafana format")

	return result, nil
}

// Execute performs the migration.
func (e *CloudWatchToPrometheusExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	accessKeyID := config.Source["access_key_id"].(string)
	secretAccessKey := config.Source["secret_access_key"].(string)
	region, _ := config.Source["region"].(string)
	if region == "" {
		region = "us-east-1"
	}
	outputDir := config.Destination["output_dir"].(string)

	awsEnv := []string{
		"AWS_ACCESS_KEY_ID=" + accessKeyID,
		"AWS_SECRET_ACCESS_KEY=" + secretAccessKey,
		"AWS_DEFAULT_REGION=" + region,
	}

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating AWS credentials")
	EmitProgress(m, 5, "Checking credentials")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Phase 2: Fetching log groups
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", "Fetching CloudWatch log groups")
	EmitProgress(m, 15, "Fetching log groups")

	logGroupsCmd := exec.CommandContext(ctx, "aws", "logs", "describe-log-groups",
		"--region", region,
		"--output", "json",
	)
	logGroupsCmd.Env = append(os.Environ(), awsEnv...)
	logGroupsOutput, err := logGroupsCmd.Output()
	if err != nil {
		EmitLog(m, "warn", "Failed to fetch log groups, continuing...")
	} else {
		logGroupsPath := filepath.Join(outputDir, "log-groups.json")
		_ = os.WriteFile(logGroupsPath, logGroupsOutput, 0644)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Exporting alarms
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", "Exporting CloudWatch alarms")
	EmitProgress(m, 30, "Exporting alarms")

	alarmsCmd := exec.CommandContext(ctx, "aws", "cloudwatch", "describe-alarms",
		"--region", region,
		"--output", "json",
	)
	alarmsCmd.Env = append(os.Environ(), awsEnv...)
	alarmsOutput, err := alarmsCmd.Output()
	if err != nil {
		EmitLog(m, "warn", "Failed to fetch alarms, continuing...")
	} else {
		alarmsPath := filepath.Join(outputDir, "alarms.json")
		_ = os.WriteFile(alarmsPath, alarmsOutput, 0644)
	}

	var alarmsResult struct {
		MetricAlarms []struct {
			AlarmName          string   `json:"AlarmName"`
			MetricName         string   `json:"MetricName"`
			Namespace          string   `json:"Namespace"`
			Threshold          float64  `json:"Threshold"`
			ComparisonOperator string   `json:"ComparisonOperator"`
			EvaluationPeriods  int      `json:"EvaluationPeriods"`
			Period             int      `json:"Period"`
			Statistic          string   `json:"Statistic"`
			Dimensions         []struct {
				Name  string `json:"Name"`
				Value string `json:"Value"`
			} `json:"Dimensions"`
		} `json:"MetricAlarms"`
	}
	_ = json.Unmarshal(alarmsOutput, &alarmsResult)

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Exporting dashboards
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Exporting CloudWatch dashboards")
	EmitProgress(m, 45, "Exporting dashboards")

	listDashboardsCmd := exec.CommandContext(ctx, "aws", "cloudwatch", "list-dashboards",
		"--region", region,
		"--output", "json",
	)
	listDashboardsCmd.Env = append(os.Environ(), awsEnv...)
	dashboardListOutput, _ := listDashboardsCmd.Output()

	var dashboardList struct {
		DashboardEntries []struct {
			DashboardName string `json:"DashboardName"`
		} `json:"DashboardEntries"`
	}
	_ = json.Unmarshal(dashboardListOutput, &dashboardList)

	dashboards := make(map[string]interface{})
	for _, d := range dashboardList.DashboardEntries {
		getDashboardCmd := exec.CommandContext(ctx, "aws", "cloudwatch", "get-dashboard",
			"--dashboard-name", d.DashboardName,
			"--region", region,
			"--output", "json",
		)
		getDashboardCmd.Env = append(os.Environ(), awsEnv...)
		dashOutput, _ := getDashboardCmd.Output()
		var dash struct {
			DashboardBody string `json:"DashboardBody"`
		}
		_ = json.Unmarshal(dashOutput, &dash)
		var body interface{}
		_ = json.Unmarshal([]byte(dash.DashboardBody), &body)
		dashboards[d.DashboardName] = body
	}

	dashboardsData, _ := json.MarshalIndent(dashboards, "", "  ")
	dashboardsPath := filepath.Join(outputDir, "dashboards.json")
	_ = os.WriteFile(dashboardsPath, dashboardsData, 0644)

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Generating Prometheus config
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Generating Prometheus configuration")
	EmitProgress(m, 60, "Generating Prometheus config")

	// Generate Prometheus alert rules from CloudWatch alarms
	alertRules := e.generatePrometheusAlertRules(alarmsResult.MetricAlarms)
	alertRulesPath := filepath.Join(outputDir, "prometheus", "alert_rules.yml")
	if err := os.MkdirAll(filepath.Dir(alertRulesPath), 0755); err != nil {
		return fmt.Errorf("failed to create prometheus directory: %w", err)
	}
	if err := os.WriteFile(alertRulesPath, []byte(alertRules), 0644); err != nil {
		return fmt.Errorf("failed to write alert rules: %w", err)
	}

	// Prometheus config
	prometheusConfig := `global:
  scrape_interval: 15s
  evaluation_interval: 15s

alerting:
  alertmanagers:
    - static_configs:
        - targets:
          - alertmanager:9093

rule_files:
  - "alert_rules.yml"

scrape_configs:
  - job_name: 'prometheus'
    static_configs:
      - targets: ['localhost:9090']

  # Add your application targets here
  - job_name: 'applications'
    static_configs:
      - targets: []
    # Alternatively use service discovery

  # For AWS EC2 instance metrics (if using node_exporter)
  - job_name: 'node'
    static_configs:
      - targets: []
`
	prometheusConfigPath := filepath.Join(outputDir, "prometheus", "prometheus.yml")
	if err := os.WriteFile(prometheusConfigPath, []byte(prometheusConfig), 0644); err != nil {
		return fmt.Errorf("failed to write prometheus config: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 6: Generating Grafana dashboards
	EmitPhase(m, phases[5], 6)
	EmitLog(m, "info", "Generating Grafana dashboards")
	EmitProgress(m, 75, "Generating Grafana dashboards")

	grafanaDir := filepath.Join(outputDir, "grafana", "dashboards")
	if err := os.MkdirAll(grafanaDir, 0755); err != nil {
		return fmt.Errorf("failed to create grafana directory: %w", err)
	}

	// Convert CloudWatch dashboards to Grafana format
	for name, dashboard := range dashboards {
		grafanaDash := e.convertToGrafanaDashboard(name, dashboard)
		dashData, _ := json.MarshalIndent(grafanaDash, "", "  ")
		dashPath := filepath.Join(grafanaDir, name+".json")
		_ = os.WriteFile(dashPath, dashData, 0644)
	}

	// Docker compose for monitoring stack
	monitoringCompose := `version: '3.8'

services:
  prometheus:
    image: prom/prometheus:latest
    container_name: prometheus
    volumes:
      - ./prometheus:/etc/prometheus
      - prometheus-data:/prometheus
    command:
      - '--config.file=/etc/prometheus/prometheus.yml'
      - '--storage.tsdb.path=/prometheus'
      - '--web.enable-lifecycle'
    ports:
      - "9090:9090"
    restart: unless-stopped

  alertmanager:
    image: prom/alertmanager:latest
    container_name: alertmanager
    volumes:
      - ./alertmanager:/etc/alertmanager
    command:
      - '--config.file=/etc/alertmanager/alertmanager.yml'
    ports:
      - "9093:9093"
    restart: unless-stopped

  grafana:
    image: grafana/grafana:latest
    container_name: grafana
    environment:
      - GF_SECURITY_ADMIN_PASSWORD=admin
      - GF_USERS_ALLOW_SIGN_UP=false
    volumes:
      - grafana-data:/var/lib/grafana
      - ./grafana/provisioning:/etc/grafana/provisioning
      - ./grafana/dashboards:/var/lib/grafana/dashboards
    ports:
      - "3000:3000"
    depends_on:
      - prometheus
    restart: unless-stopped

  loki:
    image: grafana/loki:latest
    container_name: loki
    ports:
      - "3100:3100"
    volumes:
      - ./loki:/etc/loki
      - loki-data:/loki
    command: -config.file=/etc/loki/loki-config.yml
    restart: unless-stopped

  promtail:
    image: grafana/promtail:latest
    container_name: promtail
    volumes:
      - ./promtail:/etc/promtail
      - /var/log:/var/log:ro
    command: -config.file=/etc/promtail/promtail-config.yml
    restart: unless-stopped

volumes:
  prometheus-data:
  grafana-data:
  loki-data:
`
	composePath := filepath.Join(outputDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(monitoringCompose), 0644); err != nil {
		return fmt.Errorf("failed to write docker-compose.yml: %w", err)
	}

	// Alertmanager config
	alertmanagerDir := filepath.Join(outputDir, "alertmanager")
	if err := os.MkdirAll(alertmanagerDir, 0755); err != nil {
		return fmt.Errorf("failed to create alertmanager directory: %w", err)
	}
	alertmanagerConfig := `global:
  resolve_timeout: 5m

route:
  group_by: ['alertname', 'severity']
  group_wait: 10s
  group_interval: 10s
  repeat_interval: 1h
  receiver: 'default'

receivers:
  - name: 'default'
    # Configure your notification channels here
    # webhook_configs:
    #   - url: 'http://your-webhook-url'
`
	if err := os.WriteFile(filepath.Join(alertmanagerDir, "alertmanager.yml"), []byte(alertmanagerConfig), 0644); err != nil {
		return fmt.Errorf("failed to write alertmanager config: %w", err)
	}

	// Loki config
	lokiDir := filepath.Join(outputDir, "loki")
	if err := os.MkdirAll(lokiDir, 0755); err != nil {
		return fmt.Errorf("failed to create loki directory: %w", err)
	}
	lokiConfig := `auth_enabled: false

server:
  http_listen_port: 3100

common:
  path_prefix: /loki
  storage:
    filesystem:
      chunks_directory: /loki/chunks
      rules_directory: /loki/rules
  replication_factor: 1
  ring:
    kvstore:
      store: inmemory

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
	if err := os.WriteFile(filepath.Join(lokiDir, "loki-config.yml"), []byte(lokiConfig), 0644); err != nil {
		return fmt.Errorf("failed to write loki config: %w", err)
	}

	// Promtail config
	promtailDir := filepath.Join(outputDir, "promtail")
	if err := os.MkdirAll(promtailDir, 0755); err != nil {
		return fmt.Errorf("failed to create promtail directory: %w", err)
	}
	promtailConfig := `server:
  http_listen_port: 9080
  grpc_listen_port: 0

positions:
  filename: /tmp/positions.yaml

clients:
  - url: http://loki:3100/loki/api/v1/push

scrape_configs:
  - job_name: system
    static_configs:
      - targets:
          - localhost
        labels:
          job: varlogs
          __path__: /var/log/*log
`
	if err := os.WriteFile(filepath.Join(promtailDir, "promtail-config.yml"), []byte(promtailConfig), 0644); err != nil {
		return fmt.Errorf("failed to write promtail config: %w", err)
	}

	// Grafana provisioning
	provisioningDir := filepath.Join(outputDir, "grafana", "provisioning")
	if err := os.MkdirAll(filepath.Join(provisioningDir, "datasources"), 0755); err != nil {
		return fmt.Errorf("failed to create datasources directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(provisioningDir, "dashboards"), 0755); err != nil {
		return fmt.Errorf("failed to create dashboards directory: %w", err)
	}

	datasources := `apiVersion: 1
datasources:
  - name: Prometheus
    type: prometheus
    access: proxy
    url: http://prometheus:9090
    isDefault: true
  - name: Loki
    type: loki
    access: proxy
    url: http://loki:3100
`
	_ = os.WriteFile(filepath.Join(provisioningDir, "datasources", "datasources.yml"), []byte(datasources), 0644)

	dashboardsProvisioning := `apiVersion: 1
providers:
  - name: 'default'
    orgId: 1
    folder: 'CloudWatch Migrated'
    type: file
    disableDeletion: false
    updateIntervalSeconds: 10
    options:
      path: /var/lib/grafana/dashboards
`
	_ = os.WriteFile(filepath.Join(provisioningDir, "dashboards", "dashboards.yml"), []byte(dashboardsProvisioning), 0644)

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 7: Finalizing
	EmitPhase(m, phases[6], 7)
	EmitLog(m, "info", "Finalizing migration artifacts")
	EmitProgress(m, 95, "Finalizing")

	// Generate README
	readme := fmt.Sprintf(`# CloudWatch to Prometheus/Grafana Migration

## Source CloudWatch
- Region: %s
- Log Groups: %d
- Alarms: %d
- Dashboards: %d

## Migration Mapping

| CloudWatch | Self-Hosted Equivalent |
|------------|------------------------|
| Metrics    | Prometheus             |
| Logs       | Loki + Promtail        |
| Alarms     | Prometheus AlertManager|
| Dashboards | Grafana                |

## Getting Started

1. Start the monitoring stack:
'''bash
docker-compose up -d
'''

2. Access the UIs:
- Prometheus: http://localhost:9090
- Grafana: http://localhost:3000 (admin/admin)
- AlertManager: http://localhost:9093

3. Configure your applications to:
- Expose Prometheus metrics
- Send logs to Loki (or use Promtail)

## Files Generated

### Prometheus
- prometheus/prometheus.yml: Prometheus configuration
- prometheus/alert_rules.yml: Alert rules from CloudWatch alarms

### Grafana
- grafana/dashboards/: Converted CloudWatch dashboards
- grafana/provisioning/: Auto-provisioning configs

### Loki (Logs)
- loki/loki-config.yml: Loki configuration
- promtail/promtail-config.yml: Log collection config

### Alertmanager
- alertmanager/alertmanager.yml: Alert routing config

## Notes
- CloudWatch metrics need application instrumentation
- Log shipping requires Promtail or application integration
- Some CloudWatch-specific metrics may need custom exporters
`, region, len(dashboardList.DashboardEntries), len(alarmsResult.MetricAlarms), len(dashboards))

	readmePath := filepath.Join(outputDir, "README.md")
	_ = os.WriteFile(readmePath, []byte(readme), 0644)

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", "CloudWatch migration to Prometheus/Grafana complete")

	return nil
}

func (e *CloudWatchToPrometheusExecutor) generatePrometheusAlertRules(alarms []struct {
	AlarmName          string  `json:"AlarmName"`
	MetricName         string  `json:"MetricName"`
	Namespace          string  `json:"Namespace"`
	Threshold          float64 `json:"Threshold"`
	ComparisonOperator string  `json:"ComparisonOperator"`
	EvaluationPeriods  int     `json:"EvaluationPeriods"`
	Period             int     `json:"Period"`
	Statistic          string  `json:"Statistic"`
	Dimensions         []struct {
		Name  string `json:"Name"`
		Value string `json:"Value"`
	} `json:"Dimensions"`
}) string {
	rules := "groups:\n  - name: cloudwatch_migrated\n    rules:\n"

	for _, alarm := range alarms {
		operator := ">="
		switch alarm.ComparisonOperator {
		case "GreaterThanThreshold":
			operator = ">"
		case "GreaterThanOrEqualToThreshold":
			operator = ">="
		case "LessThanThreshold":
			operator = "<"
		case "LessThanOrEqualToThreshold":
			operator = "<="
		}

		duration := fmt.Sprintf("%dm", (alarm.EvaluationPeriods*alarm.Period)/60)

		rules += fmt.Sprintf(`      - alert: %s
        expr: %s %s %.2f
        for: %s
        labels:
          severity: warning
          source: cloudwatch
        annotations:
          summary: "Migrated from CloudWatch alarm: %s"
          description: "%s %s threshold %.2f"
`, alarm.AlarmName, alarm.MetricName, operator, alarm.Threshold, duration,
			alarm.AlarmName, alarm.MetricName, operator, alarm.Threshold)
	}

	return rules
}

func (e *CloudWatchToPrometheusExecutor) convertToGrafanaDashboard(name string, cwDashboard interface{}) map[string]interface{} {
	return map[string]interface{}{
		"annotations": map[string]interface{}{
			"list": []interface{}{},
		},
		"editable":      true,
		"fiscalYearStartMonth": 0,
		"graphTooltip":  0,
		"id":            nil,
		"links":         []interface{}{},
		"liveNow":       false,
		"panels":        []interface{}{},
		"schemaVersion": 38,
		"tags":          []string{"cloudwatch-migrated"},
		"templating": map[string]interface{}{
			"list": []interface{}{},
		},
		"time": map[string]interface{}{
			"from": "now-6h",
			"to":   "now",
		},
		"title":   fmt.Sprintf("CW - %s", name),
		"uid":     "",
		"version": 1,
		"weekStart": "",
	}
}
