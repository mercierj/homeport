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

// CloudWatchLogsMapper converts AWS CloudWatch Log Groups to Loki + Promtail.
type CloudWatchLogsMapper struct {
	*mapper.BaseMapper
}

// NewCloudWatchLogsMapper creates a new CloudWatch Logs to Loki mapper.
func NewCloudWatchLogsMapper() *CloudWatchLogsMapper {
	return &CloudWatchLogsMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeCloudWatchLogGroup, nil),
	}
}

// Map converts a CloudWatch Log Group to a Loki service.
func (m *CloudWatchLogsMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	logGroupName := res.GetConfigString("name")
	if logGroupName == "" {
		logGroupName = res.Name
	}

	result := mapper.NewMappingResult("loki")
	svc := result.DockerService

	// Configure Loki service
	svc.Image = "grafana/loki:2.9.0"
	svc.Ports = []string{
		"3100:3100",
	}
	svc.Volumes = []string{
		"./data/loki:/loki",
		"./config/loki:/etc/loki",
	}
	svc.Command = []string{"-config.file=/etc/loki/loki-config.yaml"}
	svc.Networks = []string{"homeport"}
	svc.Labels = map[string]string{
		"homeport.source":     "aws_cloudwatch_log_group",
		"homeport.log_group":  logGroupName,
		"traefik.enable":       "true",
		"traefik.http.routers.loki.rule":                      "Host(`loki.localhost`)",
		"traefik.http.services.loki.loadbalancer.server.port": "3100",
	}
	svc.Restart = "unless-stopped"
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD-SHELL", "wget --no-verbose --tries=1 --spider http://localhost:3100/ready || exit 1"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  3,
	}

	// Add Promtail service for log collection
	promtail := m.createPromtailService(logGroupName)
	result.AddService(promtail)

	// Generate Loki configuration
	lokiConfig := m.generateLokiConfig(res)
	result.AddConfig("config/loki/loki-config.yaml", []byte(lokiConfig))

	// Generate Promtail configuration
	promtailConfig := m.generatePromtailConfig(res, logGroupName)
	result.AddConfig("config/promtail/promtail-config.yaml", []byte(promtailConfig))

	// Generate export script
	exportScript := m.generateExportScript(res)
	result.AddScript("scripts/cloudwatch-logs-export.sh", []byte(exportScript))

	// Generate import script
	importScript := m.generateImportScript(res, logGroupName)
	result.AddScript("scripts/loki-import.sh", []byte(importScript))

	// Add warnings and manual steps
	m.addMigrationWarnings(result, res, logGroupName)

	return result, nil
}

func (m *CloudWatchLogsMapper) createPromtailService(logGroupName string) *mapper.DockerService {
	return &mapper.DockerService{
		Name:  "promtail",
		Image: "grafana/promtail:2.9.0",
		Volumes: []string{
			"./config/promtail:/etc/promtail",
			"./logs:/var/log/app",
			"/var/run/docker.sock:/var/run/docker.sock:ro",
		},
		Command:  []string{"-config.file=/etc/promtail/promtail-config.yaml"},
		Networks: []string{"homeport"},
		Labels: map[string]string{
			"homeport.source":    "aws_cloudwatch_log_group",
			"homeport.log_group": logGroupName,
		},
		DependsOn: []string{"loki"},
		Restart:   "unless-stopped",
	}
}

func (m *CloudWatchLogsMapper) generateLokiConfig(res *resource.AWSResource) string {
	retentionDays := res.Config["retention_in_days"]
	retentionPeriod := "744h" // 31 days default
	if retentionDays != nil {
		if days, ok := retentionDays.(*int32); ok && days != nil {
			retentionPeriod = fmt.Sprintf("%dh", *days*24)
		}
	}

	return fmt.Sprintf(`# Loki Configuration
# Migrated from CloudWatch Logs

auth_enabled: false

server:
  http_listen_port: 3100
  grpc_listen_port: 9096

common:
  instance_addr: 127.0.0.1
  path_prefix: /loki
  storage:
    filesystem:
      chunks_directory: /loki/chunks
      rules_directory: /loki/rules
  replication_factor: 1
  ring:
    kvstore:
      store: inmemory

query_range:
  results_cache:
    cache:
      embedded_cache:
        enabled: true
        max_size_mb: 100

schema_config:
  configs:
    - from: 2020-10-24
      store: boltdb-shipper
      object_store: filesystem
      schema: v11
      index:
        prefix: index_
        period: 24h

ruler:
  alertmanager_url: http://alertmanager:9093

limits_config:
  retention_period: %s
  enforce_metric_name: false
  reject_old_samples: true
  reject_old_samples_max_age: 168h
  max_entries_limit_per_query: 5000

chunk_store_config:
  max_look_back_period: 0s

table_manager:
  retention_deletes_enabled: true
  retention_period: %s

# Analytics
analytics:
  reporting_enabled: false
`, retentionPeriod, retentionPeriod)
}

func (m *CloudWatchLogsMapper) generatePromtailConfig(res *resource.AWSResource, logGroupName string) string {
	// Sanitize log group name for job label
	jobName := strings.ReplaceAll(logGroupName, "/", "_")
	jobName = strings.TrimPrefix(jobName, "_")

	return fmt.Sprintf(`# Promtail Configuration
# Migrated from CloudWatch Log Group: %s

server:
  http_listen_port: 9080
  grpc_listen_port: 0

positions:
  filename: /tmp/positions.yaml

clients:
  - url: http://loki:3100/loki/api/v1/push

scrape_configs:
  # Scrape Docker container logs
  - job_name: docker
    docker_sd_configs:
      - host: unix:///var/run/docker.sock
        refresh_interval: 5s
    relabel_configs:
      - source_labels: ['__meta_docker_container_name']
        regex: '/(.*)'
        target_label: 'container'
      - source_labels: ['__meta_docker_container_log_stream']
        target_label: 'logstream'
      - source_labels: ['__meta_docker_container_label_com_docker_compose_service']
        target_label: 'service'

  # Scrape application logs (equivalent to CloudWatch log group)
  - job_name: %s
    static_configs:
      - targets:
          - localhost
        labels:
          job: %s
          log_group: %s
          __path__: /var/log/app/*.log

    pipeline_stages:
      - json:
          expressions:
            timestamp: timestamp
            level: level
            message: message
      - labels:
          level:
      - timestamp:
          source: timestamp
          format: RFC3339

  # Scrape system logs
  - job_name: system
    static_configs:
      - targets:
          - localhost
        labels:
          job: system
          __path__: /var/log/syslog

# Add more scrape configs for specific log sources
# Example for nginx:
#  - job_name: nginx
#    static_configs:
#      - targets:
#          - localhost
#        labels:
#          job: nginx
#          __path__: /var/log/nginx/*.log
`, logGroupName, jobName, jobName, logGroupName)
}

func (m *CloudWatchLogsMapper) generateExportScript(res *resource.AWSResource) string {
	logGroupName := res.GetConfigString("name")
	if logGroupName == "" {
		logGroupName = res.Name
	}
	region := res.Region
	if region == "" {
		region = "us-east-1"
	}

	return fmt.Sprintf(`#!/bin/bash
# CloudWatch Logs Export Script
# Log Group: %s

set -e

AWS_REGION="%s"
LOG_GROUP="%s"
OUTPUT_DIR="./cloudwatch-logs-export"
START_TIME=$(date -d "7 days ago" +%%s000)  # Last 7 days
END_TIME=$(date +%%s000)

echo "Exporting CloudWatch Logs..."
echo "Log Group: $LOG_GROUP"
echo "Region: $AWS_REGION"
echo "Time Range: Last 7 days"
echo ""

mkdir -p "$OUTPUT_DIR"

# Export log group configuration
echo "Exporting log group configuration..."
aws logs describe-log-groups \
  --log-group-name-prefix "$LOG_GROUP" \
  --region "$AWS_REGION" \
  --output json > "$OUTPUT_DIR/log-group-config.json"

# List log streams
echo "Listing log streams..."
aws logs describe-log-streams \
  --log-group-name "$LOG_GROUP" \
  --region "$AWS_REGION" \
  --output json > "$OUTPUT_DIR/log-streams.json"

# Export metric filters
echo "Exporting metric filters..."
aws logs describe-metric-filters \
  --log-group-name "$LOG_GROUP" \
  --region "$AWS_REGION" \
  --output json > "$OUTPUT_DIR/metric-filters.json"

# Export subscription filters
echo "Exporting subscription filters..."
aws logs describe-subscription-filters \
  --log-group-name "$LOG_GROUP" \
  --region "$AWS_REGION" \
  --output json > "$OUTPUT_DIR/subscription-filters.json"

# Export recent logs (sample)
echo "Exporting sample logs..."
aws logs filter-log-events \
  --log-group-name "$LOG_GROUP" \
  --start-time "$START_TIME" \
  --end-time "$END_TIME" \
  --limit 1000 \
  --region "$AWS_REGION" \
  --output json > "$OUTPUT_DIR/sample-logs.json"

# For full log export, use CloudWatch Logs Insights or S3 export task
echo ""
echo "============================================"
echo "Export complete! Files saved to: $OUTPUT_DIR"
echo "============================================"
echo ""
echo "For full historical log export, consider:"
echo "1. Create an S3 export task:"
echo "   aws logs create-export-task \\"
echo "     --log-group-name '$LOG_GROUP' \\"
echo "     --from $START_TIME --to $END_TIME \\"
echo "     --destination '<s3-bucket>' \\"
echo "     --destination-prefix 'logs-export'"
echo ""
echo "2. Use CloudWatch Logs Insights for querying"
`, logGroupName, region, logGroupName)
}

func (m *CloudWatchLogsMapper) generateImportScript(res *resource.AWSResource, logGroupName string) string {
	return fmt.Sprintf(`#!/bin/bash
# Loki Import Script
# Imports exported CloudWatch logs into Loki

set -e

LOKI_URL="${LOKI_URL:-http://localhost:3100}"
INPUT_FILE="${1:-./cloudwatch-logs-export/sample-logs.json}"
LOG_GROUP="%s"

echo "============================================"
echo "Importing logs to Loki"
echo "============================================"
echo "Loki URL: $LOKI_URL"
echo "Input file: $INPUT_FILE"
echo "Log Group: $LOG_GROUP"
echo ""

if [ ! -f "$INPUT_FILE" ]; then
  echo "Error: Input file not found: $INPUT_FILE"
  echo "Run cloudwatch-logs-export.sh first"
  exit 1
fi

# Check if Loki is ready
echo "Checking Loki connection..."
until curl -sf "$LOKI_URL/ready" > /dev/null; do
  echo "Waiting for Loki..."
  sleep 2
done
echo "Loki is ready."

# Parse and push logs
echo "Pushing logs to Loki..."

# This is a simplified example - for production, use a proper tool
jq -c '.events[]' "$INPUT_FILE" | while read -r event; do
  timestamp=$(echo "$event" | jq -r '.timestamp')
  message=$(echo "$event" | jq -r '.message')
  stream=$(echo "$event" | jq -r '.logStreamName')

  # Convert to Loki push format
  ns_timestamp=$((timestamp * 1000000))

  curl -s -X POST "$LOKI_URL/loki/api/v1/push" \
    -H "Content-Type: application/json" \
    -d "{
      \"streams\": [{
        \"stream\": {
          \"job\": \"cloudwatch-import\",
          \"log_group\": \"$LOG_GROUP\",
          \"log_stream\": \"$stream\"
        },
        \"values\": [[\"$ns_timestamp\", \"$message\"]]
      }]
    }"
done

echo ""
echo "Import complete!"
echo "View logs in Grafana or query Loki:"
echo "  curl '$LOKI_URL/loki/api/v1/query?query={job=\"cloudwatch-import\"}&limit=100'"
`, logGroupName)
}

func (m *CloudWatchLogsMapper) addMigrationWarnings(result *mapper.MappingResult, res *resource.AWSResource, logGroupName string) {
	// Retention warning
	if retentionDays := res.Config["retention_in_days"]; retentionDays != nil {
		result.AddWarning(fmt.Sprintf("Log retention configured in CloudWatch. Loki retention set accordingly."))
	} else {
		result.AddWarning("No retention set in CloudWatch (logs kept forever). Configure Loki retention as needed.")
	}

	// KMS encryption warning
	if kmsKeyID := res.GetConfigString("kms_key_id"); kmsKeyID != "" {
		result.AddWarning(fmt.Sprintf("Logs encrypted with KMS key %s. Configure encryption at rest in Loki if needed.", kmsKeyID))
	}

	// Metric filters warning
	if metricFilterCount := res.Config["metric_filter_count"]; metricFilterCount != nil {
		result.AddWarning("CloudWatch metric filters detected. Create equivalent Loki recording rules.")
		result.AddManualStep("Review metric filters and create Loki LogQL recording rules")
	}

	// Standard manual steps
	result.AddManualStep("Run scripts/cloudwatch-logs-export.sh to export log configuration")
	result.AddManualStep("Update application logging to send logs to Promtail/Loki")
	result.AddManualStep("Configure Grafana to visualize Loki logs")
	result.AddManualStep("Set up log-based alerts in Grafana using Loki as data source")

	// Volumes
	result.AddVolume(mapper.Volume{
		Name:   "loki-data",
		Driver: "local",
	})
}
