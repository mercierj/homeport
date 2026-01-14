// Package monitoring provides mappers for AWS monitoring services.
package monitoring

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

// CloudWatchMetricAlarmMapper converts AWS CloudWatch Metric Alarms to Prometheus Alertmanager.
type CloudWatchMetricAlarmMapper struct {
	*mapper.BaseMapper
}

// NewCloudWatchMetricAlarmMapper creates a new CloudWatch Metric Alarm to Alertmanager mapper.
func NewCloudWatchMetricAlarmMapper() *CloudWatchMetricAlarmMapper {
	return &CloudWatchMetricAlarmMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeCloudWatchMetricAlarm, nil),
	}
}

// Map converts a CloudWatch Metric Alarm to Prometheus + Alertmanager stack.
func (m *CloudWatchMetricAlarmMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	alarmName := res.GetConfigString("alarm_name")
	if alarmName == "" {
		alarmName = res.GetConfigString("name")
	}
	if alarmName == "" {
		alarmName = res.Name
	}

	result := mapper.NewMappingResult("alertmanager")
	svc := result.DockerService

	// Configure Alertmanager service
	svc.Image = "prom/alertmanager:v0.26.0"
	svc.Ports = []string{
		"9093:9093",
	}
	svc.Volumes = []string{
		"./config/alertmanager:/etc/alertmanager",
		"./data/alertmanager:/alertmanager",
	}
	svc.Command = []string{
		"--config.file=/etc/alertmanager/alertmanager.yml",
		"--storage.path=/alertmanager",
		"--web.external-url=http://alertmanager.localhost",
		"--cluster.listen-address=",
	}
	svc.Networks = []string{"homeport"}
	svc.Labels = map[string]string{
		"homeport.source":     "aws_cloudwatch_metric_alarm",
		"homeport.alarm_name": alarmName,
		"traefik.enable":      "true",
		"traefik.http.routers.alertmanager.rule":                      "Host(`alertmanager.localhost`)",
		"traefik.http.routers.alertmanager.entrypoints":               "websecure",
		"traefik.http.routers.alertmanager.tls":                       "true",
		"traefik.http.services.alertmanager.loadbalancer.server.port": "9093",
	}
	svc.Restart = "unless-stopped"
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:9093/-/healthy"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  3,
	}
	svc.DependsOn = []string{"prometheus"}

	// Add Prometheus service for metric collection
	prometheus := m.createPrometheusService()
	result.AddService(prometheus)

	// Generate Alertmanager configuration with routes and receivers
	alertmanagerConfig := m.generateAlertmanagerConfig(res, alarmName)
	result.AddConfig("config/alertmanager/alertmanager.yml", []byte(alertmanagerConfig))

	// Generate Prometheus alert rules from CloudWatch alarm configuration
	alertRules := m.generatePrometheusAlertRules(res, alarmName)
	result.AddConfig("config/prometheus/rules/cloudwatch-metric-alarms.yml", []byte(alertRules))

	// Generate Prometheus configuration
	prometheusConfig := m.generatePrometheusConfig()
	result.AddConfig("config/prometheus/prometheus.yml", []byte(prometheusConfig))

	// Generate migration scripts
	migrationScript := m.generateMigrationScript(res, alarmName)
	result.AddScript("scripts/migrate-cloudwatch-alarm.sh", []byte(migrationScript))

	// Generate alarm export script
	exportScript := m.generateExportScript(res)
	result.AddScript("scripts/export-cloudwatch-alarms.sh", []byte(exportScript))

	// Generate alarm testing script
	testScript := m.generateTestScript(alarmName)
	result.AddScript("scripts/test-alert.sh", []byte(testScript))

	// Add warnings and manual steps based on alarm configuration
	m.addMigrationWarnings(result, res, alarmName)

	// Add volumes
	result.AddVolume(mapper.Volume{
		Name:   "alertmanager-data",
		Driver: "local",
	})
	result.AddVolume(mapper.Volume{
		Name:   "prometheus-data",
		Driver: "local",
	})

	return result, nil
}

// createPrometheusService creates the Prometheus Docker service for metric collection.
func (m *CloudWatchMetricAlarmMapper) createPrometheusService() *mapper.DockerService {
	return &mapper.DockerService{
		Name:  "prometheus",
		Image: "prom/prometheus:v2.47.0",
		Ports: []string{"9090:9090"},
		Volumes: []string{
			"./config/prometheus:/etc/prometheus",
			"./data/prometheus:/prometheus",
		},
		Command: []string{
			"--config.file=/etc/prometheus/prometheus.yml",
			"--storage.tsdb.path=/prometheus",
			"--storage.tsdb.retention.time=15d",
			"--web.console.libraries=/etc/prometheus/console_libraries",
			"--web.console.templates=/etc/prometheus/consoles",
			"--web.enable-lifecycle",
			"--web.enable-admin-api",
		},
		Networks: []string{"homeport"},
		Labels: map[string]string{
			"homeport.source":                                            "aws_cloudwatch_metric_alarm",
			"traefik.enable":                                             "true",
			"traefik.http.routers.prometheus.rule":                       "Host(`prometheus.localhost`)",
			"traefik.http.routers.prometheus.entrypoints":                "websecure",
			"traefik.http.routers.prometheus.tls":                        "true",
			"traefik.http.services.prometheus.loadbalancer.server.port":  "9090",
		},
		Restart: "unless-stopped",
		HealthCheck: &mapper.HealthCheck{
			Test:     []string{"CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:9090/-/healthy"},
			Interval: 30 * time.Second,
			Timeout:  10 * time.Second,
			Retries:  3,
		},
		Environment: make(map[string]string),
		Sysctls:     make(map[string]string),
		Ulimits:     make(map[string]mapper.Ulimit),
	}
}

// generateAlertmanagerConfig creates Alertmanager configuration with routes and receivers
// based on CloudWatch alarm actions.
func (m *CloudWatchMetricAlarmMapper) generateAlertmanagerConfig(res *resource.AWSResource, alarmName string) string {
	// Extract alarm actions to determine receiver configuration
	alarmActions := m.extractAlarmActions(res)
	okActions := m.extractOKActions(res)
	insufficientDataActions := m.extractInsufficientDataActions(res)

	// Determine severity based on alarm configuration
	severity := m.determineSeverity(res, alarmName)

	// Build receivers section
	receiversConfig := m.buildReceiversConfig(alarmActions, okActions)

	// Build routes section
	routesConfig := m.buildRoutesConfig(severity)

	return fmt.Sprintf(`# Alertmanager Configuration
# Migrated from CloudWatch Metric Alarm: %s
# Generated by Homeport Migration Tool

global:
  resolve_timeout: 5m
  # SMTP configuration (uncomment and configure for email notifications)
  # smtp_smarthost: 'smtp.example.com:587'
  # smtp_from: 'alertmanager@example.com'
  # smtp_auth_username: 'alertmanager'
  # smtp_auth_password: '${SMTP_PASSWORD}'
  # smtp_require_tls: true

  # Slack configuration (uncomment and configure)
  # slack_api_url: '${SLACK_WEBHOOK_URL}'

route:
  group_by: ['alertname', 'severity', 'namespace']
  group_wait: 30s
  group_interval: 5m
  repeat_interval: 4h
  receiver: 'default'

  routes:
%s

receivers:
  - name: 'default'
    # Default receiver - configure based on your needs
    # webhook_configs:
    #   - url: 'http://webhook-handler:5000/alerts'
    #     send_resolved: true

%s

# Inhibition rules to suppress less severe alerts when critical alerts fire
inhibit_rules:
  - source_match:
      severity: 'critical'
    target_match:
      severity: 'warning'
    equal: ['alertname', 'namespace']

  - source_match:
      severity: 'warning'
    target_match:
      severity: 'info'
    equal: ['alertname', 'namespace']

# CloudWatch Alarm Actions Migration Notes:
# =========================================
%s

# Templates for notification formatting
templates:
  - '/etc/alertmanager/templates/*.tmpl'
`, alarmName, routesConfig, receiversConfig, m.generateActionsNotes(alarmActions, okActions, insufficientDataActions))
}

// extractAlarmActions extracts alarm actions from the resource configuration.
func (m *CloudWatchMetricAlarmMapper) extractAlarmActions(res *resource.AWSResource) []string {
	if actions := res.Config["alarm_actions"]; actions != nil {
		if actionSlice, ok := actions.([]interface{}); ok {
			result := make([]string, 0, len(actionSlice))
			for _, a := range actionSlice {
				if s, ok := a.(string); ok {
					result = append(result, s)
				}
			}
			return result
		}
		if actionSlice, ok := actions.([]string); ok {
			return actionSlice
		}
	}
	return nil
}

// extractOKActions extracts OK actions from the resource configuration.
func (m *CloudWatchMetricAlarmMapper) extractOKActions(res *resource.AWSResource) []string {
	if actions := res.Config["ok_actions"]; actions != nil {
		if actionSlice, ok := actions.([]interface{}); ok {
			result := make([]string, 0, len(actionSlice))
			for _, a := range actionSlice {
				if s, ok := a.(string); ok {
					result = append(result, s)
				}
			}
			return result
		}
		if actionSlice, ok := actions.([]string); ok {
			return actionSlice
		}
	}
	return nil
}

// extractInsufficientDataActions extracts insufficient data actions.
func (m *CloudWatchMetricAlarmMapper) extractInsufficientDataActions(res *resource.AWSResource) []string {
	if actions := res.Config["insufficient_data_actions"]; actions != nil {
		if actionSlice, ok := actions.([]interface{}); ok {
			result := make([]string, 0, len(actionSlice))
			for _, a := range actionSlice {
				if s, ok := a.(string); ok {
					result = append(result, s)
				}
			}
			return result
		}
		if actionSlice, ok := actions.([]string); ok {
			return actionSlice
		}
	}
	return nil
}

// determineSeverity determines the severity level based on alarm configuration.
func (m *CloudWatchMetricAlarmMapper) determineSeverity(res *resource.AWSResource, alarmName string) string {
	// Check alarm name for severity hints
	lowerName := strings.ToLower(alarmName)
	if strings.Contains(lowerName, "critical") || strings.Contains(lowerName, "crit") {
		return "critical"
	}
	if strings.Contains(lowerName, "warning") || strings.Contains(lowerName, "warn") {
		return "warning"
	}

	// Check treat_missing_data configuration
	treatMissingData := res.GetConfigString("treat_missing_data")
	if treatMissingData == "breaching" {
		return "critical"
	}

	// Check actions_enabled
	actionsEnabled := res.GetConfigBool("actions_enabled")
	if !actionsEnabled {
		return "info"
	}

	// Default to warning
	return "warning"
}

// buildRoutesConfig builds the routes configuration section.
func (m *CloudWatchMetricAlarmMapper) buildRoutesConfig(severity string) string {
	return fmt.Sprintf(`    # Route for critical alerts - page on-call
    - match:
        severity: critical
      receiver: 'critical'
      continue: true

    # Route for warning alerts
    - match:
        severity: warning
      receiver: 'warning'

    # Route for info alerts
    - match:
        severity: info
      receiver: 'info'

    # Default severity for migrated alarm: %s`, severity)
}

// buildReceiversConfig builds the receivers configuration section.
func (m *CloudWatchMetricAlarmMapper) buildReceiversConfig(alarmActions, okActions []string) string {
	var config strings.Builder

	// Critical receiver
	config.WriteString(`  - name: 'critical'
    # PagerDuty for critical alerts (recommended for on-call)
    # pagerduty_configs:
    #   - service_key: '${PAGERDUTY_SERVICE_KEY}'
    #     send_resolved: true
    #
    # Slack for critical alerts
    # slack_configs:
    #   - channel: '#alerts-critical'
    #     send_resolved: true
    #     title: '{{ .Status | toUpper }}: {{ .CommonLabels.alertname }}'
    #     text: '{{ range .Alerts }}{{ .Annotations.description }}{{ end }}'
    #
    # Email for critical alerts
    # email_configs:
    #   - to: 'oncall@example.com'
    #     send_resolved: true

`)

	// Warning receiver
	config.WriteString(`  - name: 'warning'
    # Slack for warnings
    # slack_configs:
    #   - channel: '#alerts-warning'
    #     send_resolved: true
    #     title: '{{ .Status | toUpper }}: {{ .CommonLabels.alertname }}'
    #     text: '{{ range .Alerts }}{{ .Annotations.description }}{{ end }}'
    #
    # Email for warnings
    # email_configs:
    #   - to: 'team@example.com'
    #     send_resolved: true

`)

	// Info receiver
	config.WriteString(`  - name: 'info'
    # Slack for info alerts
    # slack_configs:
    #   - channel: '#alerts-info'
    #     send_resolved: true
`)

	// Add SNS action notes
	if len(alarmActions) > 0 {
		config.WriteString("\n  # Original CloudWatch alarm actions detected:\n")
		for _, action := range alarmActions {
			if strings.Contains(action, "sns") {
				config.WriteString(fmt.Sprintf("  # SNS: %s\n", action))
				config.WriteString("  # -> Consider using webhook, Slack, or email receiver\n")
			}
			if strings.Contains(action, "autoscaling") {
				config.WriteString(fmt.Sprintf("  # AutoScaling: %s\n", action))
				config.WriteString("  # -> Use webhook to trigger scaling scripts\n")
			}
			if strings.Contains(action, "lambda") {
				config.WriteString(fmt.Sprintf("  # Lambda: %s\n", action))
				config.WriteString("  # -> Use webhook to call equivalent function\n")
			}
		}
	}

	return config.String()
}

// generateActionsNotes generates comments about original CloudWatch actions.
func (m *CloudWatchMetricAlarmMapper) generateActionsNotes(alarmActions, okActions, insufficientDataActions []string) string {
	var notes strings.Builder

	if len(alarmActions) > 0 {
		notes.WriteString("# Alarm Actions:\n")
		for _, action := range alarmActions {
			notes.WriteString(fmt.Sprintf("#   - %s\n", action))
		}
	}

	if len(okActions) > 0 {
		notes.WriteString("# OK Actions (use send_resolved: true):\n")
		for _, action := range okActions {
			notes.WriteString(fmt.Sprintf("#   - %s\n", action))
		}
	}

	if len(insufficientDataActions) > 0 {
		notes.WriteString("# Insufficient Data Actions:\n")
		for _, action := range insufficientDataActions {
			notes.WriteString(fmt.Sprintf("#   - %s\n", action))
		}
		notes.WriteString("# Note: Prometheus uses 'absent()' function for missing data detection\n")
	}

	return notes.String()
}

// generatePrometheusAlertRules generates Prometheus alerting rules from CloudWatch alarm config.
func (m *CloudWatchMetricAlarmMapper) generatePrometheusAlertRules(res *resource.AWSResource, alarmName string) string {
	// Extract CloudWatch alarm configuration
	namespace := res.GetConfigString("namespace")
	metricName := res.GetConfigString("metric_name")
	statistic := res.GetConfigString("statistic")
	comparisonOp := res.GetConfigString("comparison_operator")
	description := res.GetConfigString("alarm_description")
	if description == "" {
		description = res.GetConfigString("description")
	}

	// Extract numeric values
	threshold := m.getFloatValue(res.Config["threshold"])
	period := m.getIntValue(res.Config["period"])
	evalPeriods := m.getIntValue(res.Config["evaluation_periods"])
	datapointsToAlarm := m.getIntValue(res.Config["datapoints_to_alarm"])

	// Calculate for duration
	forDuration := m.calculateForDuration(period, evalPeriods)

	// Convert CloudWatch comparison to PromQL operator
	promqlOp := m.convertComparisonOperator(comparisonOp)

	// Map CloudWatch metric to Prometheus expression
	promqlExpr := m.mapMetricToPromQL(namespace, metricName, statistic, res)

	// Determine severity
	severity := m.determineSeverity(res, alarmName)

	// Sanitize alarm name for Prometheus
	sanitizedName := m.sanitizeAlertName(alarmName)

	// Build dimensions info
	dimensionsInfo := m.extractDimensionsInfo(res)

	return fmt.Sprintf(`# Prometheus Alert Rules
# Migrated from CloudWatch Metric Alarm: %s
# Generated by Homeport Migration Tool

groups:
  - name: cloudwatch_migrated_alarms
    interval: 1m
    rules:
      # ============================================================
      # Original CloudWatch Alarm: %s
      # Namespace: %s
      # Metric: %s
      # Statistic: %s
      # Comparison: %s %v
      # Period: %ds
      # Evaluation Periods: %d
      # Datapoints to Alarm: %d
      # Dimensions: %s
      # ============================================================

      - alert: %s
        # Original CloudWatch metric expression (needs conversion)
        # %s/%s %s %v
        #
        # Prometheus equivalent (REVIEW AND ADJUST):
        expr: %s %s %v
        for: %s
        labels:
          severity: %s
          source: cloudwatch_migration
          original_namespace: "%s"
          original_metric: "%s"
          original_statistic: "%s"
        annotations:
          summary: "CloudWatch Alarm: %s"
          description: "%s"
          runbook_url: "https://docs.example.com/runbooks/%s"

      # Absent data alert (equivalent to CloudWatch INSUFFICIENT_DATA)
      - alert: %s_NoData
        expr: absent(%s)
        for: 10m
        labels:
          severity: warning
          source: cloudwatch_migration
        annotations:
          summary: "No data for %s"
          description: "The metric used by this alert has not reported data for 10 minutes. This may indicate a collection issue."

# ============================================================
# Common CloudWatch to Prometheus Metric Mappings
# ============================================================
#
# AWS/EC2 Metrics:
#   CPUUtilization → 100 - (avg by(instance) (irate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)
#   NetworkIn → rate(node_network_receive_bytes_total[5m])
#   NetworkOut → rate(node_network_transmit_bytes_total[5m])
#   DiskReadBytes → rate(node_disk_read_bytes_total[5m])
#   DiskWriteBytes → rate(node_disk_written_bytes_total[5m])
#
# AWS/RDS Metrics:
#   DatabaseConnections → pg_stat_activity_count OR mysql_global_status_threads_connected
#   CPUUtilization → rate(process_cpu_seconds_total[5m]) * 100
#   FreeableMemory → pg_settings_shared_buffers_bytes
#   ReadIOPS → rate(pg_stat_database_blks_read[5m])
#
# AWS/ELB Metrics:
#   RequestCount → rate(traefik_entrypoint_requests_total[5m])
#   TargetResponseTime → histogram_quantile(0.99, rate(traefik_entrypoint_request_duration_seconds_bucket[5m]))
#   HTTPCode_Target_5XX_Count → rate(traefik_entrypoint_requests_total{code=~"5.."}[5m])
#
# AWS/Lambda Metrics:
#   Invocations → rate(http_requests_total[5m])
#   Duration → histogram_quantile(0.99, rate(http_request_duration_seconds_bucket[5m]))
#   Errors → rate(http_requests_total{status=~"5.."}[5m])
#
# AWS/ElastiCache Metrics:
#   CurrConnections → redis_connected_clients
#   CacheHits → rate(redis_keyspace_hits_total[5m])
#   CacheMisses → rate(redis_keyspace_misses_total[5m])
#   BytesUsedForCache → redis_memory_used_bytes
`,
		alarmName, alarmName, namespace, metricName, statistic,
		comparisonOp, threshold, period, evalPeriods, datapointsToAlarm,
		dimensionsInfo, sanitizedName,
		namespace, metricName, promqlOp, threshold,
		promqlExpr, promqlOp, threshold, forDuration,
		severity, namespace, metricName, statistic,
		alarmName, m.escapeDescription(description), sanitizedName,
		sanitizedName, promqlExpr, alarmName)
}

// getFloatValue safely extracts a float64 value from interface{}.
func (m *CloudWatchMetricAlarmMapper) getFloatValue(v interface{}) float64 {
	if v == nil {
		return 0
	}
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case int32:
		return float64(val)
	case *float64:
		if val != nil {
			return *val
		}
	case *int:
		if val != nil {
			return float64(*val)
		}
	case json.Number:
		f, _ := val.Float64()
		return f
	}
	return 0
}

// getIntValue safely extracts an int value from interface{}.
func (m *CloudWatchMetricAlarmMapper) getIntValue(v interface{}) int {
	if v == nil {
		return 0
	}
	switch val := v.(type) {
	case int:
		return val
	case int64:
		return int(val)
	case int32:
		return int(val)
	case float64:
		return int(val)
	case float32:
		return int(val)
	case *int:
		if val != nil {
			return *val
		}
	case *int32:
		if val != nil {
			return int(*val)
		}
	case json.Number:
		i, _ := val.Int64()
		return int(i)
	}
	return 0
}

// calculateForDuration calculates the Prometheus 'for' duration from CloudWatch period/evaluation periods.
func (m *CloudWatchMetricAlarmMapper) calculateForDuration(period, evalPeriods int) string {
	if period == 0 {
		period = 60 // Default to 60 seconds
	}
	if evalPeriods == 0 {
		evalPeriods = 1
	}

	totalSeconds := period * evalPeriods

	if totalSeconds >= 3600 {
		return fmt.Sprintf("%dh", totalSeconds/3600)
	}
	if totalSeconds >= 60 {
		return fmt.Sprintf("%dm", totalSeconds/60)
	}
	return fmt.Sprintf("%ds", totalSeconds)
}

// convertComparisonOperator converts CloudWatch comparison operator to PromQL.
func (m *CloudWatchMetricAlarmMapper) convertComparisonOperator(cwOp string) string {
	switch cwOp {
	case "GreaterThanThreshold":
		return ">"
	case "GreaterThanOrEqualToThreshold":
		return ">="
	case "LessThanThreshold":
		return "<"
	case "LessThanOrEqualToThreshold":
		return "<="
	case "LessThanLowerOrGreaterThanUpperThreshold":
		return "!="
	case "LessThanLowerThreshold":
		return "<"
	case "GreaterThanUpperThreshold":
		return ">"
	default:
		return ">"
	}
}

// mapMetricToPromQL maps CloudWatch namespace/metric to Prometheus expression.
func (m *CloudWatchMetricAlarmMapper) mapMetricToPromQL(namespace, metricName, statistic string, res *resource.AWSResource) string {
	key := fmt.Sprintf("%s/%s", namespace, metricName)

	// Common mappings
	mappings := map[string]string{
		// EC2 metrics -> Node Exporter
		"AWS/EC2/CPUUtilization":    "100 - (avg by(instance) (irate(node_cpu_seconds_total{mode=\"idle\"}[5m])) * 100)",
		"AWS/EC2/NetworkIn":         "rate(node_network_receive_bytes_total[5m])",
		"AWS/EC2/NetworkOut":        "rate(node_network_transmit_bytes_total[5m])",
		"AWS/EC2/DiskReadBytes":     "rate(node_disk_read_bytes_total[5m])",
		"AWS/EC2/DiskWriteBytes":    "rate(node_disk_written_bytes_total[5m])",
		"AWS/EC2/DiskReadOps":       "rate(node_disk_reads_completed_total[5m])",
		"AWS/EC2/DiskWriteOps":      "rate(node_disk_writes_completed_total[5m])",
		"AWS/EC2/StatusCheckFailed": "up == 0",

		// RDS metrics -> PostgreSQL/MySQL Exporter
		"AWS/RDS/CPUUtilization":       "rate(process_cpu_seconds_total[5m]) * 100",
		"AWS/RDS/DatabaseConnections":  "pg_stat_activity_count",
		"AWS/RDS/FreeableMemory":       "pg_settings_shared_buffers_bytes",
		"AWS/RDS/ReadIOPS":             "rate(pg_stat_database_blks_read[5m])",
		"AWS/RDS/WriteIOPS":            "rate(pg_stat_database_blks_hit[5m])",
		"AWS/RDS/FreeStorageSpace":     "pg_database_size_bytes",
		"AWS/RDS/ReplicaLag":           "pg_replication_lag",

		// ELB/ALB metrics -> Traefik
		"AWS/ELB/RequestCount":            "rate(traefik_entrypoint_requests_total[5m])",
		"AWS/ELB/Latency":                 "histogram_quantile(0.99, rate(traefik_entrypoint_request_duration_seconds_bucket[5m]))",
		"AWS/ELB/HTTPCode_Target_2XX_Count": "rate(traefik_entrypoint_requests_total{code=~\"2..\"}[5m])",
		"AWS/ELB/HTTPCode_Target_5XX_Count": "rate(traefik_entrypoint_requests_total{code=~\"5..\"}[5m])",
		"AWS/ELB/HealthyHostCount":        "up",
		"AWS/ELB/UnHealthyHostCount":      "up == 0",

		// ALB metrics
		"AWS/ApplicationELB/RequestCount":      "rate(traefik_service_requests_total[5m])",
		"AWS/ApplicationELB/TargetResponseTime": "histogram_quantile(0.99, rate(traefik_service_request_duration_seconds_bucket[5m]))",

		// Lambda metrics -> Application metrics
		"AWS/Lambda/Invocations":           "rate(http_requests_total[5m])",
		"AWS/Lambda/Duration":              "histogram_quantile(0.99, rate(http_request_duration_seconds_bucket[5m]))",
		"AWS/Lambda/Errors":                "rate(http_requests_total{status=~\"5..\"}[5m])",
		"AWS/Lambda/ConcurrentExecutions":  "process_open_fds",
		"AWS/Lambda/Throttles":             "rate(http_requests_total{status=\"429\"}[5m])",

		// ElastiCache/Redis metrics -> Redis Exporter
		"AWS/ElastiCache/CPUUtilization":    "rate(redis_cpu_user_seconds_total[5m]) * 100",
		"AWS/ElastiCache/CurrConnections":   "redis_connected_clients",
		"AWS/ElastiCache/CacheHits":         "rate(redis_keyspace_hits_total[5m])",
		"AWS/ElastiCache/CacheMisses":       "rate(redis_keyspace_misses_total[5m])",
		"AWS/ElastiCache/BytesUsedForCache": "redis_memory_used_bytes",
		"AWS/ElastiCache/Evictions":         "rate(redis_evicted_keys_total[5m])",

		// SQS metrics -> RabbitMQ metrics
		"AWS/SQS/ApproximateNumberOfMessagesVisible": "rabbitmq_queue_messages_ready",
		"AWS/SQS/ApproximateAgeOfOldestMessage":      "rabbitmq_queue_head_message_timestamp",
		"AWS/SQS/NumberOfMessagesSent":               "rate(rabbitmq_channel_messages_published_total[5m])",
		"AWS/SQS/NumberOfMessagesReceived":           "rate(rabbitmq_channel_messages_delivered_total[5m])",

		// SNS metrics
		"AWS/SNS/NumberOfMessagesPublished": "rate(rabbitmq_channel_messages_published_total[5m])",

		// S3 metrics (limited - needs custom exporter)
		"AWS/S3/BucketSizeBytes":        "minio_bucket_usage_total_bytes",
		"AWS/S3/NumberOfObjects":        "minio_bucket_usage_object_total",
	}

	if expr, ok := mappings[key]; ok {
		return expr
	}

	// Return a placeholder for unknown metrics
	return fmt.Sprintf("# TODO: Map %s/%s to Prometheus metric\nunknown_cloudwatch_metric{namespace=\"%s\", metric=\"%s\"}", namespace, metricName, namespace, metricName)
}

// sanitizeAlertName converts CloudWatch alarm name to valid Prometheus alert name.
func (m *CloudWatchMetricAlarmMapper) sanitizeAlertName(name string) string {
	// Replace invalid characters
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ReplaceAll(name, "-", "_")
	name = strings.ReplaceAll(name, ".", "_")
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, ":", "_")

	// Ensure it starts with a letter
	if len(name) > 0 && (name[0] >= '0' && name[0] <= '9') {
		name = "alert_" + name
	}

	return name
}

// extractDimensionsInfo extracts dimensions from CloudWatch alarm configuration.
func (m *CloudWatchMetricAlarmMapper) extractDimensionsInfo(res *resource.AWSResource) string {
	dimensions := res.Config["dimensions"]
	if dimensions == nil {
		return "none"
	}

	switch d := dimensions.(type) {
	case map[string]interface{}:
		parts := make([]string, 0, len(d))
		for k, v := range d {
			parts = append(parts, fmt.Sprintf("%s=%v", k, v))
		}
		return strings.Join(parts, ", ")
	case []interface{}:
		parts := make([]string, 0, len(d))
		for _, dim := range d {
			if dimMap, ok := dim.(map[string]interface{}); ok {
				name := dimMap["name"]
				value := dimMap["value"]
				parts = append(parts, fmt.Sprintf("%v=%v", name, value))
			}
		}
		return strings.Join(parts, ", ")
	}

	return "unknown"
}

// escapeDescription escapes special characters in description for YAML.
func (m *CloudWatchMetricAlarmMapper) escapeDescription(desc string) string {
	desc = strings.ReplaceAll(desc, "\"", "\\\"")
	desc = strings.ReplaceAll(desc, "\n", " ")
	return desc
}

// generatePrometheusConfig generates the Prometheus configuration file.
func (m *CloudWatchMetricAlarmMapper) generatePrometheusConfig() string {
	return `# Prometheus Configuration
# Migrated from CloudWatch - Generated by Homeport

global:
  scrape_interval: 15s
  evaluation_interval: 15s
  external_labels:
    monitor: 'homeport'
    environment: 'production'

# Alertmanager configuration
alerting:
  alertmanagers:
    - static_configs:
        - targets:
            - alertmanager:9093

# Rule files
rule_files:
  - /etc/prometheus/rules/*.yml

# Scrape configurations
scrape_configs:
  # Prometheus self-monitoring
  - job_name: 'prometheus'
    static_configs:
      - targets: ['localhost:9090']

  # Alertmanager metrics
  - job_name: 'alertmanager'
    static_configs:
      - targets: ['alertmanager:9093']

  # Node exporter for host metrics (replaces EC2 CloudWatch metrics)
  - job_name: 'node'
    static_configs:
      - targets: ['node-exporter:9100']

  # Traefik metrics (replaces ELB/ALB CloudWatch metrics)
  - job_name: 'traefik'
    static_configs:
      - targets: ['traefik:8082']

  # Docker container metrics via cAdvisor
  # Uncomment if using cAdvisor
  # - job_name: 'cadvisor'
  #   static_configs:
  #     - targets: ['cadvisor:8080']

  # PostgreSQL exporter (replaces RDS CloudWatch metrics)
  # Uncomment if using PostgreSQL
  # - job_name: 'postgres'
  #   static_configs:
  #     - targets: ['postgres-exporter:9187']

  # MySQL exporter (replaces RDS MySQL CloudWatch metrics)
  # Uncomment if using MySQL
  # - job_name: 'mysql'
  #   static_configs:
  #     - targets: ['mysql-exporter:9104']

  # Redis exporter (replaces ElastiCache CloudWatch metrics)
  # Uncomment if using Redis
  # - job_name: 'redis'
  #   static_configs:
  #     - targets: ['redis-exporter:9121']

  # RabbitMQ exporter (replaces SQS/SNS CloudWatch metrics)
  # Uncomment if using RabbitMQ
  # - job_name: 'rabbitmq'
  #   static_configs:
  #     - targets: ['rabbitmq:15692']

  # Application metrics - add your services here
  # - job_name: 'app'
  #   static_configs:
  #     - targets: ['app:8080']
  #   metrics_path: '/metrics'

  # Docker service discovery
  - job_name: 'docker'
    docker_sd_configs:
      - host: unix:///var/run/docker.sock
        refresh_interval: 15s
    relabel_configs:
      - source_labels: [__meta_docker_container_label_prometheus_scrape]
        regex: 'true'
        action: keep
      - source_labels: [__meta_docker_container_name]
        regex: '/(.*)'
        target_label: container
      - source_labels: [__meta_docker_container_label_prometheus_port]
        target_label: __address__
        regex: '(.+)'
        replacement: '${1}'
`
}

// generateMigrationScript generates a migration script for CloudWatch alarms.
func (m *CloudWatchMetricAlarmMapper) generateMigrationScript(res *resource.AWSResource, alarmName string) string {
	region := res.Region
	if region == "" {
		region = "us-east-1"
	}

	return fmt.Sprintf(`#!/bin/bash
# CloudWatch Metric Alarm Migration Script
# Alarm: %s
# Generated by Homeport Migration Tool

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}CloudWatch to Prometheus/Alertmanager Migration${NC}"
echo -e "${GREEN}Alarm: %s${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""

# Step 1: Validate prerequisites
echo -e "${YELLOW}Step 1: Validating prerequisites...${NC}"
command -v docker >/dev/null 2>&1 || { echo -e "${RED}Docker is required but not installed.${NC}"; exit 1; }
command -v docker-compose >/dev/null 2>&1 || command -v docker compose >/dev/null 2>&1 || { echo -e "${RED}Docker Compose is required but not installed.${NC}"; exit 1; }
echo -e "${GREEN}Prerequisites validated.${NC}"
echo ""

# Step 2: Create directory structure
echo -e "${YELLOW}Step 2: Creating directory structure...${NC}"
mkdir -p ./config/alertmanager
mkdir -p ./config/alertmanager/templates
mkdir -p ./config/prometheus
mkdir -p ./config/prometheus/rules
mkdir -p ./data/alertmanager
mkdir -p ./data/prometheus
echo -e "${GREEN}Directories created.${NC}"
echo ""

# Step 3: Set permissions
echo -e "${YELLOW}Step 3: Setting permissions...${NC}"
chmod -R 755 ./config
chmod -R 777 ./data
echo -e "${GREEN}Permissions set.${NC}"
echo ""

# Step 4: Validate configuration files
echo -e "${YELLOW}Step 4: Validating configuration files...${NC}"
if [ -f "./config/alertmanager/alertmanager.yml" ]; then
    docker run --rm -v $(pwd)/config/alertmanager:/etc/alertmanager prom/alertmanager:v0.26.0 \
        --config.file=/etc/alertmanager/alertmanager.yml --check-config 2>/dev/null && \
        echo -e "${GREEN}Alertmanager config is valid.${NC}" || \
        echo -e "${YELLOW}Warning: Alertmanager config validation failed. Check the configuration.${NC}"
fi

if [ -f "./config/prometheus/prometheus.yml" ]; then
    docker run --rm -v $(pwd)/config/prometheus:/etc/prometheus prom/prometheus:v2.47.0 \
        promtool check config /etc/prometheus/prometheus.yml 2>/dev/null && \
        echo -e "${GREEN}Prometheus config is valid.${NC}" || \
        echo -e "${YELLOW}Warning: Prometheus config validation failed. Check the configuration.${NC}"
fi
echo ""

# Step 5: Start services
echo -e "${YELLOW}Step 5: Starting monitoring stack...${NC}"
if command -v docker compose &> /dev/null; then
    docker compose up -d prometheus alertmanager
else
    docker-compose up -d prometheus alertmanager
fi
echo ""

# Step 6: Wait for services to be ready
echo -e "${YELLOW}Step 6: Waiting for services to be ready...${NC}"
echo "Waiting for Prometheus..."
until curl -sf http://localhost:9090/-/ready > /dev/null 2>&1; do
    printf '.'
    sleep 2
done
echo -e "${GREEN} Ready!${NC}"

echo "Waiting for Alertmanager..."
until curl -sf http://localhost:9093/-/ready > /dev/null 2>&1; do
    printf '.'
    sleep 2
done
echo -e "${GREEN} Ready!${NC}"
echo ""

# Step 7: Load alert rules
echo -e "${YELLOW}Step 7: Reloading Prometheus configuration...${NC}"
curl -X POST http://localhost:9090/-/reload 2>/dev/null && \
    echo -e "${GREEN}Prometheus configuration reloaded.${NC}" || \
    echo -e "${YELLOW}Warning: Could not reload Prometheus. Enable --web.enable-lifecycle flag.${NC}"
echo ""

# Step 8: Summary
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}Migration Complete!${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo "Access points:"
echo "  Prometheus:   http://localhost:9090"
echo "  Alertmanager: http://localhost:9093"
echo ""
echo "Next steps:"
echo "  1. Review alert rules in config/prometheus/rules/"
echo "  2. Configure Alertmanager receivers in config/alertmanager/alertmanager.yml"
echo "  3. Install appropriate exporters for your services"
echo "  4. Test alerts using scripts/test-alert.sh"
echo ""
echo "Useful commands:"
echo "  Check alerts:  curl http://localhost:9090/api/v1/alerts"
echo "  Check rules:   curl http://localhost:9090/api/v1/rules"
echo "  Reload config: curl -X POST http://localhost:9090/-/reload"
`, alarmName, alarmName)
}

// generateExportScript generates a script to export CloudWatch alarms.
func (m *CloudWatchMetricAlarmMapper) generateExportScript(res *resource.AWSResource) string {
	region := res.Region
	if region == "" {
		region = "us-east-1"
	}

	return fmt.Sprintf(`#!/bin/bash
# CloudWatch Alarms Export Script
# Generated by Homeport Migration Tool

set -e

AWS_REGION="${AWS_REGION:-%s}"
OUTPUT_DIR="./cloudwatch-export"

echo "Exporting CloudWatch alarms from region: $AWS_REGION"
mkdir -p "$OUTPUT_DIR"

# Export all metric alarms
echo "Exporting metric alarms..."
aws cloudwatch describe-alarms \
    --region "$AWS_REGION" \
    --alarm-types "MetricAlarm" \
    --output json > "$OUTPUT_DIR/metric-alarms.json"

# Export composite alarms
echo "Exporting composite alarms..."
aws cloudwatch describe-alarms \
    --region "$AWS_REGION" \
    --alarm-types "CompositeAlarm" \
    --output json > "$OUTPUT_DIR/composite-alarms.json" 2>/dev/null || echo "No composite alarms found"

# Export alarm history
echo "Exporting alarm history..."
aws cloudwatch describe-alarm-history \
    --region "$AWS_REGION" \
    --history-item-type StateUpdate \
    --max-records 100 \
    --output json > "$OUTPUT_DIR/alarm-history.json"

# Generate summary
echo ""
echo "================================"
echo "CloudWatch Alarms Export Summary"
echo "================================"
echo ""

# Count alarms by state
echo "Alarms by State:"
jq -r '.MetricAlarms | group_by(.StateValue) | map({state: .[0].StateValue, count: length}) | .[] | "  \(.state): \(.count)"' \
    "$OUTPUT_DIR/metric-alarms.json" 2>/dev/null || echo "  Unable to parse"

echo ""
echo "Alarms by Namespace:"
jq -r '.MetricAlarms | group_by(.Namespace) | map({namespace: .[0].Namespace, count: length}) | .[] | "  \(.namespace): \(.count)"' \
    "$OUTPUT_DIR/metric-alarms.json" 2>/dev/null || echo "  Unable to parse"

echo ""
echo "Export complete! Files saved to: $OUTPUT_DIR"
echo ""
echo "Next steps:"
echo "  1. Review exported alarms in $OUTPUT_DIR/metric-alarms.json"
echo "  2. Update config/prometheus/rules/cloudwatch-metric-alarms.yml"
echo "  3. Configure receivers in config/alertmanager/alertmanager.yml"
echo "  4. Run scripts/migrate-cloudwatch-alarm.sh to deploy"
`, region)
}

// generateTestScript generates a script to test alerting.
func (m *CloudWatchMetricAlarmMapper) generateTestScript(alarmName string) string {
	sanitizedName := m.sanitizeAlertName(alarmName)

	return fmt.Sprintf(`#!/bin/bash
# Alert Testing Script
# Tests the Prometheus/Alertmanager setup
# Generated by Homeport Migration Tool

set -e

PROMETHEUS_URL="${PROMETHEUS_URL:-http://localhost:9090}"
ALERTMANAGER_URL="${ALERTMANAGER_URL:-http://localhost:9093}"

echo "========================================"
echo "Prometheus/Alertmanager Alert Testing"
echo "========================================"
echo ""

# Test 1: Check Prometheus is running
echo "Test 1: Checking Prometheus..."
if curl -sf "$PROMETHEUS_URL/-/ready" > /dev/null; then
    echo "  ✓ Prometheus is ready"
else
    echo "  ✗ Prometheus is not ready"
    exit 1
fi

# Test 2: Check Alertmanager is running
echo ""
echo "Test 2: Checking Alertmanager..."
if curl -sf "$ALERTMANAGER_URL/-/ready" > /dev/null; then
    echo "  ✓ Alertmanager is ready"
else
    echo "  ✗ Alertmanager is not ready"
    exit 1
fi

# Test 3: Check alert rules are loaded
echo ""
echo "Test 3: Checking alert rules..."
RULES=$(curl -sf "$PROMETHEUS_URL/api/v1/rules" | jq -r '.data.groups[].rules | length' | paste -sd+ | bc)
echo "  Loaded rules: $RULES"

# Test 4: Check for migrated alarm rule
echo ""
echo "Test 4: Checking for migrated alarm rule '%s'..."
if curl -sf "$PROMETHEUS_URL/api/v1/rules" | jq -e '.data.groups[].rules[] | select(.name == "%s")' > /dev/null 2>&1; then
    echo "  ✓ Alert rule '%s' found"
else
    echo "  ✗ Alert rule '%s' not found"
    echo "    Make sure the rule file is in config/prometheus/rules/"
fi

# Test 5: Send a test alert
echo ""
echo "Test 5: Sending test alert to Alertmanager..."
curl -sf -X POST "$ALERTMANAGER_URL/api/v1/alerts" \
    -H "Content-Type: application/json" \
    -d '[{
        "labels": {
            "alertname": "TestAlert",
            "severity": "info",
            "source": "test_script"
        },
        "annotations": {
            "summary": "Test alert from migration script",
            "description": "This is a test alert to verify Alertmanager is working"
        },
        "startsAt": "'$(date -u +%%Y-%%m-%%dT%%H:%%M:%%SZ)'",
        "generatorURL": "http://localhost:9090"
    }]' && echo "  ✓ Test alert sent" || echo "  ✗ Failed to send test alert"

# Test 6: Check active alerts
echo ""
echo "Test 6: Checking active alerts..."
ALERTS=$(curl -sf "$ALERTMANAGER_URL/api/v2/alerts" | jq '. | length')
echo "  Active alerts: $ALERTS"

# Summary
echo ""
echo "========================================"
echo "Testing Complete"
echo "========================================"
echo ""
echo "View alerts:"
echo "  Prometheus:   $PROMETHEUS_URL/alerts"
echo "  Alertmanager: $ALERTMANAGER_URL/#/alerts"
echo ""
echo "Debug endpoints:"
echo "  Rules:        $PROMETHEUS_URL/api/v1/rules"
echo "  Targets:      $PROMETHEUS_URL/api/v1/targets"
echo "  Alert status: $ALERTMANAGER_URL/api/v2/status"
`, sanitizedName, sanitizedName, sanitizedName, sanitizedName)
}

// addMigrationWarnings adds warnings and manual steps based on alarm configuration.
func (m *CloudWatchMetricAlarmMapper) addMigrationWarnings(result *mapper.MappingResult, res *resource.AWSResource, alarmName string) {
	// Check current alarm state
	stateValue := res.GetConfigString("state_value")
	if stateValue == "ALARM" {
		result.AddWarning(fmt.Sprintf("Alarm '%s' is currently in ALARM state. Investigate before migration.", alarmName))
	}

	// Check for alarm actions
	if alarmActions := m.extractAlarmActions(res); len(alarmActions) > 0 {
		result.AddWarning("CloudWatch alarm actions (SNS, Lambda, Auto Scaling) need manual migration to Alertmanager receivers.")
		for _, action := range alarmActions {
			if strings.Contains(action, "sns") {
				result.AddWarning(fmt.Sprintf("SNS action detected: %s. Configure equivalent Alertmanager receiver.", action))
			}
			if strings.Contains(action, "autoscaling") {
				result.AddWarning(fmt.Sprintf("Auto Scaling action detected: %s. Use webhook receiver with scaling script.", action))
			}
			if strings.Contains(action, "lambda") {
				result.AddWarning(fmt.Sprintf("Lambda action detected: %s. Use webhook receiver to trigger function.", action))
			}
		}
	}

	// Check for OK actions
	if okActions := m.extractOKActions(res); len(okActions) > 0 {
		result.AddWarning("CloudWatch OK actions detected. Configure 'send_resolved: true' in Alertmanager receivers.")
	}

	// Check for insufficient data actions
	if insufficientDataActions := m.extractInsufficientDataActions(res); len(insufficientDataActions) > 0 {
		result.AddWarning("Insufficient data actions detected. Use Prometheus absent() function for equivalent behavior.")
	}

	// Check treat_missing_data
	treatMissingData := res.GetConfigString("treat_missing_data")
	if treatMissingData != "" && treatMissingData != "missing" {
		result.AddWarning(fmt.Sprintf("treat_missing_data is set to '%s'. Review how missing data should be handled in Prometheus.", treatMissingData))
	}

	// Check for extended statistics
	extendedStatistic := res.GetConfigString("extended_statistic")
	if extendedStatistic != "" {
		result.AddWarning(fmt.Sprintf("Extended statistic '%s' used. Use histogram_quantile() in Prometheus for percentiles.", extendedStatistic))
	}

	// General warnings
	result.AddWarning("CloudWatch metrics use different names than Prometheus. Review and update alert expressions.")
	result.AddWarning("Install appropriate exporters (node-exporter, postgres-exporter, redis-exporter) based on your services.")

	// Manual steps
	result.AddManualStep("Run scripts/export-cloudwatch-alarms.sh to export all CloudWatch alarms")
	result.AddManualStep("Review and update config/prometheus/rules/cloudwatch-metric-alarms.yml with correct Prometheus metrics")
	result.AddManualStep("Configure Alertmanager receivers in config/alertmanager/alertmanager.yml (Slack, PagerDuty, email, etc.)")
	result.AddManualStep("Run scripts/migrate-cloudwatch-alarm.sh to deploy the monitoring stack")
	result.AddManualStep("Test alerting using scripts/test-alert.sh")
	result.AddManualStep("Access Prometheus at http://prometheus.localhost")
	result.AddManualStep("Access Alertmanager at http://alertmanager.localhost")
}
