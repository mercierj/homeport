// Package monitoring provides mappers for AWS monitoring services.
package monitoring

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

// CloudWatchAlarmsMapper converts AWS CloudWatch Alarms to Prometheus Alertmanager.
type CloudWatchAlarmsMapper struct {
	*mapper.BaseMapper
}

// NewCloudWatchAlarmsMapper creates a new CloudWatch Alarms to Alertmanager mapper.
func NewCloudWatchAlarmsMapper() *CloudWatchAlarmsMapper {
	return &CloudWatchAlarmsMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeCloudWatchMetricAlarm, nil),
	}
}

// Map converts a CloudWatch Alarm to an Alertmanager + Prometheus rules configuration.
func (m *CloudWatchAlarmsMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	alarmName := res.GetConfigString("name")
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
	}
	svc.Networks = []string{"homeport"}
	svc.Labels = map[string]string{
		"homeport.source":     "aws_cloudwatch_metric_alarm",
		"homeport.alarm_name": alarmName,
		"traefik.enable":       "true",
		"traefik.http.routers.alertmanager.rule":                      "Host(`alertmanager.localhost`)",
		"traefik.http.services.alertmanager.loadbalancer.server.port": "9093",
	}
	svc.Restart = "unless-stopped"
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:9093/-/healthy"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  3,
	}

	// Generate Alertmanager configuration
	alertmanagerConfig := m.generateAlertmanagerConfig(res)
	result.AddConfig("config/alertmanager/alertmanager.yml", []byte(alertmanagerConfig))

	// Generate Prometheus alert rules from CloudWatch alarm
	alertRules := m.generateAlertRules(res)
	result.AddConfig("config/prometheus/rules/cloudwatch-alarms.yml", []byte(alertRules))

	// Generate export script
	exportScript := m.generateExportScript(res)
	result.AddScript("scripts/cloudwatch-alarms-export.sh", []byte(exportScript))

	// Generate conversion script
	conversionScript := m.generateConversionScript()
	result.AddScript("scripts/convert-alarms.sh", []byte(conversionScript))

	// Add warnings and manual steps
	m.addMigrationWarnings(result, res, alarmName)

	return result, nil
}

func (m *CloudWatchAlarmsMapper) generateAlertmanagerConfig(res *resource.AWSResource) string {
	alarmName := res.GetConfigString("name")
	alarmActions := res.Config["alarm_actions"]

	// Try to detect notification targets
	receivers := m.detectReceivers(alarmActions)

	return fmt.Sprintf(`# Alertmanager Configuration
# Migrated from CloudWatch Alarm: %s

global:
  resolve_timeout: 5m
  # SMTP configuration for email notifications
  # smtp_smarthost: 'smtp.example.com:587'
  # smtp_from: 'alertmanager@example.com'
  # smtp_auth_username: 'alertmanager'
  # smtp_auth_password: 'password'

route:
  group_by: ['alertname', 'severity']
  group_wait: 30s
  group_interval: 5m
  repeat_interval: 4h
  receiver: 'default'

  routes:
    - match:
        severity: critical
      receiver: 'critical'
      continue: true

    - match:
        severity: warning
      receiver: 'warning'

receivers:
  - name: 'default'
    # Configure your notification channels here
    # webhook_configs:
    #   - url: 'http://your-webhook-endpoint'

  - name: 'critical'
    # Email for critical alerts
    # email_configs:
    #   - to: 'oncall@example.com'
    #     send_resolved: true
    #
    # PagerDuty for critical alerts
    # pagerduty_configs:
    #   - service_key: '<your-service-key>'
    #
    # Slack for critical alerts
    # slack_configs:
    #   - api_url: 'https://hooks.slack.com/services/xxx/xxx/xxx'
    #     channel: '#alerts-critical'

  - name: 'warning'
    # Slack for warnings
    # slack_configs:
    #   - api_url: 'https://hooks.slack.com/services/xxx/xxx/xxx'
    #     channel: '#alerts-warning'

%s

inhibit_rules:
  - source_match:
      severity: 'critical'
    target_match:
      severity: 'warning'
    equal: ['alertname']

# Templates
templates:
  - '/etc/alertmanager/templates/*.tmpl'
`, alarmName, receivers)
}

func (m *CloudWatchAlarmsMapper) detectReceivers(alarmActions interface{}) string {
	if alarmActions == nil {
		return "# No alarm actions detected - configure receivers manually"
	}

	actions, ok := alarmActions.([]string)
	if !ok {
		return "# Could not parse alarm actions - configure receivers manually"
	}

	var comments []string
	for _, action := range actions {
		if strings.Contains(action, "sns") {
			comments = append(comments, fmt.Sprintf("# SNS topic detected: %s", action))
			comments = append(comments, "# Consider using webhook or email receiver")
		}
		if strings.Contains(action, "autoscaling") {
			comments = append(comments, fmt.Sprintf("# Auto Scaling action detected: %s", action))
			comments = append(comments, "# Consider using webhook to trigger scaling scripts")
		}
	}

	if len(comments) > 0 {
		return strings.Join(comments, "\n")
	}
	return ""
}

func (m *CloudWatchAlarmsMapper) generateAlertRules(res *resource.AWSResource) string {
	alarmName := res.GetConfigString("name")
	description := res.GetConfigString("description")
	metricName := res.GetConfigString("metric_name")
	namespace := res.GetConfigString("namespace")
	statistic := res.GetConfigString("statistic")
	period := res.Config["period"]
	threshold := res.Config["threshold"]
	comparisonOp := res.GetConfigString("comparison_operator")
	evalPeriods := res.Config["evaluation_periods"]

	// Convert CloudWatch comparison to PromQL
	promqlOp := m.convertComparisonOperator(comparisonOp)

	// Convert CloudWatch metric to Prometheus metric (example mapping)
	promMetric := m.convertMetricName(namespace, metricName)

	// Build for duration
	forDuration := "5m"
	if period != nil && evalPeriods != nil {
		if p, ok := period.(*int32); ok && p != nil {
			if e, ok := evalPeriods.(*int32); ok && e != nil {
				seconds := *p * *e
				forDuration = fmt.Sprintf("%ds", seconds)
			}
		}
	}

	// Determine severity based on alarm name or actions
	severity := "warning"
	if strings.Contains(strings.ToLower(alarmName), "critical") {
		severity = "critical"
	}

	thresholdStr := "0"
	if threshold != nil {
		if t, ok := threshold.(*float64); ok && t != nil {
			thresholdStr = fmt.Sprintf("%v", *t)
		}
	}

	return fmt.Sprintf(`# Prometheus Alert Rules
# Migrated from CloudWatch Alarm: %s

groups:
  - name: cloudwatch_migrated_alarms
    rules:
      # Original CloudWatch Alarm: %s
      # Metric: %s/%s
      # Statistic: %s
      # Comparison: %s %s
      - alert: %s
        expr: %s %s %s
        for: %s
        labels:
          severity: %s
          source: cloudwatch_migration
          original_namespace: %s
          original_metric: %s
        annotations:
          summary: "%s"
          description: "%s"

      # TODO: Add more alert rules here
      # Use the pattern above to convert other CloudWatch alarms

# Common alert examples (uncomment and customize as needed):

#  - name: infrastructure_alerts
#    rules:
#      - alert: HighCPUUsage
#        expr: 100 - (avg by(instance) (irate(node_cpu_seconds_total{mode="idle"}[5m])) * 100) > 80
#        for: 5m
#        labels:
#          severity: warning
#        annotations:
#          summary: "High CPU usage on {{ $labels.instance }}"
#          description: "CPU usage is above 80%% for more than 5 minutes"
#
#      - alert: HighMemoryUsage
#        expr: (1 - node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes) * 100 > 85
#        for: 5m
#        labels:
#          severity: warning
#        annotations:
#          summary: "High memory usage on {{ $labels.instance }}"
#          description: "Memory usage is above 85%% for more than 5 minutes"
#
#      - alert: DiskSpaceLow
#        expr: (node_filesystem_avail_bytes / node_filesystem_size_bytes) * 100 < 15
#        for: 10m
#        labels:
#          severity: critical
#        annotations:
#          summary: "Low disk space on {{ $labels.instance }}"
#          description: "Disk space is below 15%% on {{ $labels.mountpoint }}"
`, alarmName, alarmName, namespace, metricName, statistic, comparisonOp, thresholdStr,
		m.sanitizeAlertName(alarmName), promMetric, promqlOp, thresholdStr, forDuration,
		severity, namespace, metricName, alarmName, description)
}

func (m *CloudWatchAlarmsMapper) convertComparisonOperator(cwOp string) string {
	switch cwOp {
	case "GreaterThanThreshold":
		return ">"
	case "GreaterThanOrEqualToThreshold":
		return ">="
	case "LessThanThreshold":
		return "<"
	case "LessThanOrEqualToThreshold":
		return "<="
	default:
		return ">"
	}
}

func (m *CloudWatchAlarmsMapper) convertMetricName(namespace, metricName string) string {
	// Map common CloudWatch metrics to Prometheus equivalents
	key := fmt.Sprintf("%s/%s", namespace, metricName)

	mappings := map[string]string{
		"AWS/EC2/CPUUtilization":        "100 - (avg by(instance) (irate(node_cpu_seconds_total{mode=\"idle\"}[5m])) * 100)",
		"AWS/EC2/NetworkIn":             "rate(node_network_receive_bytes_total[5m])",
		"AWS/EC2/NetworkOut":            "rate(node_network_transmit_bytes_total[5m])",
		"AWS/EC2/DiskReadBytes":         "rate(node_disk_read_bytes_total[5m])",
		"AWS/EC2/DiskWriteBytes":        "rate(node_disk_written_bytes_total[5m])",
		"AWS/RDS/CPUUtilization":        "rate(process_cpu_seconds_total[5m]) * 100",
		"AWS/RDS/DatabaseConnections":   "pg_stat_activity_count",
		"AWS/RDS/FreeableMemory":        "pg_settings_shared_buffers_bytes",
		"AWS/ELB/RequestCount":          "rate(traefik_entrypoint_requests_total[5m])",
		"AWS/ELB/Latency":               "histogram_quantile(0.99, rate(traefik_entrypoint_request_duration_seconds_bucket[5m]))",
		"AWS/Lambda/Errors":             "rate(http_requests_total{status=~\"5..\"}[5m])",
		"AWS/Lambda/Duration":           "histogram_quantile(0.99, rate(http_request_duration_seconds_bucket[5m]))",
		"AWS/ElastiCache/CPUUtilization": "rate(redis_cpu_user_seconds_total[5m]) * 100",
		"AWS/ElastiCache/CurrConnections": "redis_connected_clients",
	}

	if promMetric, ok := mappings[key]; ok {
		return promMetric
	}

	// Return a placeholder for unknown metrics
	return fmt.Sprintf("# TODO: Map %s to Prometheus metric\nunknown_metric", key)
}

func (m *CloudWatchAlarmsMapper) sanitizeAlertName(name string) string {
	// Convert CloudWatch alarm name to valid Prometheus alert name
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ReplaceAll(name, "-", "_")
	name = strings.ReplaceAll(name, ".", "_")
	return name
}

func (m *CloudWatchAlarmsMapper) generateExportScript(res *resource.AWSResource) string {
	region := res.Region
	if region == "" {
		region = "us-east-1"
	}

	return fmt.Sprintf(`#!/bin/bash
# CloudWatch Alarms Export Script

set -e

AWS_REGION="%s"
OUTPUT_DIR="./cloudwatch-alarms-export"

echo "Exporting CloudWatch alarms..."
mkdir -p "$OUTPUT_DIR"

# Export all alarms
echo "Exporting alarm definitions..."
aws cloudwatch describe-alarms \
  --region "$AWS_REGION" \
  --output json > "$OUTPUT_DIR/all-alarms.json"

# Export alarm history
echo "Exporting alarm history..."
aws cloudwatch describe-alarm-history \
  --region "$AWS_REGION" \
  --history-item-type StateUpdate \
  --max-records 100 \
  --output json > "$OUTPUT_DIR/alarm-history.json"

# Count alarms by state
echo ""
echo "Alarm Summary:"
echo "=============="
jq -r '.MetricAlarms | group_by(.StateValue) | map({state: .[0].StateValue, count: length}) | .[]' \
  "$OUTPUT_DIR/all-alarms.json" 2>/dev/null || echo "Unable to parse alarms"

echo ""
echo "Export complete! Files saved to: $OUTPUT_DIR"
echo ""
echo "Next steps:"
echo "1. Review all-alarms.json for alarm definitions"
echo "2. Run convert-alarms.sh to generate Prometheus rules"
echo "3. Copy rules to config/prometheus/rules/"
`, region)
}

func (m *CloudWatchAlarmsMapper) generateConversionScript() string {
	return `#!/bin/bash
# CloudWatch Alarms to Prometheus Rules Conversion Script

set -e

INPUT_FILE="${1:-./cloudwatch-alarms-export/all-alarms.json}"
OUTPUT_FILE="${2:-./config/prometheus/rules/converted-alarms.yml}"

if [ ! -f "$INPUT_FILE" ]; then
  echo "Error: Input file not found: $INPUT_FILE"
  echo "Run cloudwatch-alarms-export.sh first"
  exit 1
fi

mkdir -p "$(dirname "$OUTPUT_FILE")"

echo "Converting CloudWatch alarms to Prometheus rules..."
echo "Input: $INPUT_FILE"
echo "Output: $OUTPUT_FILE"

# Generate Prometheus rules header
cat > "$OUTPUT_FILE" << 'EOF'
# Prometheus Alert Rules
# Converted from CloudWatch Alarms

groups:
  - name: cloudwatch_converted
    rules:
EOF

# Parse each alarm and generate a rule
jq -c '.MetricAlarms[]' "$INPUT_FILE" | while read -r alarm; do
  name=$(echo "$alarm" | jq -r '.AlarmName' | tr ' -.' '_')
  metric=$(echo "$alarm" | jq -r '.MetricName')
  namespace=$(echo "$alarm" | jq -r '.Namespace')
  threshold=$(echo "$alarm" | jq -r '.Threshold')
  comparison=$(echo "$alarm" | jq -r '.ComparisonOperator')
  description=$(echo "$alarm" | jq -r '.AlarmDescription // "No description"')

  # Convert comparison operator
  case "$comparison" in
    "GreaterThanThreshold") op=">" ;;
    "GreaterThanOrEqualToThreshold") op=">=" ;;
    "LessThanThreshold") op="<" ;;
    "LessThanOrEqualToThreshold") op="<=" ;;
    *) op=">" ;;
  esac

  cat >> "$OUTPUT_FILE" << EOF

      # Original: $namespace/$metric
      - alert: $name
        # TODO: Replace with actual Prometheus metric
        expr: unknown_metric{namespace="$namespace", metric="$metric"} $op $threshold
        for: 5m
        labels:
          severity: warning
          source: cloudwatch_conversion
        annotations:
          summary: "$name"
          description: "$description"
EOF
done

echo ""
echo "Conversion complete!"
echo "IMPORTANT: Review $OUTPUT_FILE and replace 'unknown_metric' with actual Prometheus metrics"
echo ""
echo "Common replacements:"
echo "  AWS/EC2/CPUUtilization → 100 - (avg(irate(node_cpu_seconds_total{mode=\"idle\"}[5m])) * 100)"
echo "  AWS/RDS/DatabaseConnections → pg_stat_activity_count"
echo "  AWS/ELB/RequestCount → rate(traefik_entrypoint_requests_total[5m])"
`
}

func (m *CloudWatchAlarmsMapper) addMigrationWarnings(result *mapper.MappingResult, res *resource.AWSResource, alarmName string) {
	// State warning
	stateValue := res.GetConfigString("state_value")
	if stateValue == "ALARM" {
		result.AddWarning(fmt.Sprintf("Alarm '%s' is currently in ALARM state!", alarmName))
	}

	// Actions warning
	if alarmActions := res.Config["alarm_actions"]; alarmActions != nil {
		result.AddWarning("CloudWatch alarm actions (SNS, Auto Scaling) need manual migration to Alertmanager receivers.")
	}

	// OK actions
	if okActions := res.Config["ok_actions"]; okActions != nil {
		result.AddWarning("CloudWatch OK actions detected. Configure 'send_resolved: true' in Alertmanager receivers.")
	}

	// Standard warnings
	result.AddWarning("CloudWatch metrics use different names than Prometheus. Update alert expressions.")
	result.AddWarning("SNS topics need to be replaced with Alertmanager receivers (email, Slack, PagerDuty, etc.)")

	// Manual steps
	result.AddManualStep("Run scripts/cloudwatch-alarms-export.sh to export all alarms")
	result.AddManualStep("Review config/prometheus/rules/cloudwatch-alarms.yml and update expressions")
	result.AddManualStep("Configure Alertmanager receivers in config/alertmanager/alertmanager.yml")
	result.AddManualStep("Access Alertmanager at http://alertmanager.localhost")
	result.AddManualStep("Test alert routing by triggering a test alert")

	// Volumes
	result.AddVolume(mapper.Volume{
		Name:   "alertmanager-data",
		Driver: "local",
	})
}
