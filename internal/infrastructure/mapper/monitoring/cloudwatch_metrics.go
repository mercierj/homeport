// Package monitoring provides mappers for AWS monitoring services.
package monitoring

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

// CloudWatchMetricsMapper converts AWS CloudWatch Dashboards to Prometheus + Grafana.
type CloudWatchMetricsMapper struct {
	*mapper.BaseMapper
}

// NewCloudWatchMetricsMapper creates a new CloudWatch Dashboard to Grafana mapper.
func NewCloudWatchMetricsMapper() *CloudWatchMetricsMapper {
	return &CloudWatchMetricsMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeCloudWatchDashboard, nil),
	}
}

// Map converts a CloudWatch Dashboard to a Prometheus + Grafana stack.
func (m *CloudWatchMetricsMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	dashboardName := res.GetConfigString("name")
	if dashboardName == "" {
		dashboardName = res.Name
	}

	result := mapper.NewMappingResult("prometheus")
	svc := result.DockerService

	// Configure Prometheus service
	svc.Image = "prom/prometheus:v2.47.0"
	svc.Ports = []string{
		"9090:9090",
	}
	svc.Volumes = []string{
		"./config/prometheus:/etc/prometheus",
		"./data/prometheus:/prometheus",
	}
	svc.Command = []string{
		"--config.file=/etc/prometheus/prometheus.yml",
		"--storage.tsdb.path=/prometheus",
		"--storage.tsdb.retention.time=15d",
		"--web.console.libraries=/etc/prometheus/console_libraries",
		"--web.console.templates=/etc/prometheus/consoles",
		"--web.enable-lifecycle",
	}
	svc.Networks = []string{"homeport"}
	svc.Labels = map[string]string{
		"homeport.source":        "aws_cloudwatch_dashboard",
		"homeport.dashboard":     dashboardName,
		"traefik.enable":          "true",
		"traefik.http.routers.prometheus.rule":                      "Host(`prometheus.localhost`)",
		"traefik.http.services.prometheus.loadbalancer.server.port": "9090",
	}
	svc.Restart = "unless-stopped"
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:9090/-/healthy"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  3,
	}

	// Add Grafana service
	grafana := m.createGrafanaService(dashboardName)
	result.AddService(grafana)

	// Add node exporter for system metrics
	nodeExporter := m.createNodeExporterService()
	result.AddService(nodeExporter)

	// Generate Prometheus configuration
	prometheusConfig := m.generatePrometheusConfig()
	result.AddConfig("config/prometheus/prometheus.yml", []byte(prometheusConfig))

	// Generate Grafana provisioning
	grafanaDS := m.generateGrafanaDatasource()
	result.AddConfig("config/grafana/provisioning/datasources/prometheus.yml", []byte(grafanaDS))

	// Generate dashboard export script
	exportScript := m.generateExportScript(res)
	result.AddScript("scripts/cloudwatch-dashboard-export.sh", []byte(exportScript))

	// Generate dashboard conversion script
	convertScript := m.generateConversionScript(res)
	result.AddScript("scripts/convert-dashboard.sh", []byte(convertScript))

	// Generate metrics mapping documentation
	metricsDoc := m.generateMetricsMapping()
	result.AddConfig("config/prometheus/cloudwatch-metrics-mapping.md", []byte(metricsDoc))

	// Add warnings and manual steps
	m.addMigrationWarnings(result, res, dashboardName)

	return result, nil
}

func (m *CloudWatchMetricsMapper) createGrafanaService(dashboardName string) *mapper.DockerService {
	return &mapper.DockerService{
		Name:  "grafana",
		Image: "grafana/grafana:10.2.0",
		Ports: []string{"3000:3000"},
		Environment: map[string]string{
			"GF_SECURITY_ADMIN_USER":       "${GRAFANA_ADMIN_USER:-admin}",
			"GF_SECURITY_ADMIN_PASSWORD":   "${GRAFANA_ADMIN_PASSWORD:-admin}",
			"GF_USERS_ALLOW_SIGN_UP":       "false",
			"GF_SERVER_ROOT_URL":           "http://grafana.localhost",
			"GF_INSTALL_PLUGINS":           "grafana-clock-panel,grafana-piechart-panel",
		},
		Volumes: []string{
			"./data/grafana:/var/lib/grafana",
			"./config/grafana/provisioning:/etc/grafana/provisioning",
		},
		Networks: []string{"homeport"},
		Labels: map[string]string{
			"homeport.source":    "aws_cloudwatch_dashboard",
			"homeport.dashboard": dashboardName,
			"traefik.enable":      "true",
			"traefik.http.routers.grafana.rule":                      "Host(`grafana.localhost`)",
			"traefik.http.services.grafana.loadbalancer.server.port": "3000",
		},
		DependsOn: []string{"prometheus"},
		Restart:   "unless-stopped",
		HealthCheck: &mapper.HealthCheck{
			Test:     []string{"CMD-SHELL", "wget --no-verbose --tries=1 --spider http://localhost:3000/api/health || exit 1"},
			Interval: 30 * time.Second,
			Timeout:  10 * time.Second,
			Retries:  3,
		},
	}
}

func (m *CloudWatchMetricsMapper) createNodeExporterService() *mapper.DockerService {
	return &mapper.DockerService{
		Name:  "node-exporter",
		Image: "prom/node-exporter:v1.6.1",
		Ports: []string{"9100:9100"},
		Volumes: []string{
			"/proc:/host/proc:ro",
			"/sys:/host/sys:ro",
			"/:/rootfs:ro",
		},
		Command: []string{
			"--path.procfs=/host/proc",
			"--path.sysfs=/host/sys",
			"--path.rootfs=/rootfs",
			"--collector.filesystem.mount-points-exclude=^/(sys|proc|dev|host|etc)($$|/)",
		},
		Networks: []string{"homeport"},
		Restart:  "unless-stopped",
	}
}

func (m *CloudWatchMetricsMapper) generatePrometheusConfig() string {
	return `# Prometheus Configuration
# Migrated from CloudWatch

global:
  scrape_interval: 15s
  evaluation_interval: 15s
  external_labels:
    monitor: 'homeport'

alerting:
  alertmanagers:
    - static_configs:
        - targets:
            - alertmanager:9093

rule_files:
  - /etc/prometheus/rules/*.yml

scrape_configs:
  # Prometheus self-monitoring
  - job_name: 'prometheus'
    static_configs:
      - targets: ['localhost:9090']

  # Node exporter for host metrics (replaces EC2 CloudWatch metrics)
  - job_name: 'node'
    static_configs:
      - targets: ['node-exporter:9100']

  # Docker container metrics via cAdvisor
  # Uncomment if using cAdvisor
  # - job_name: 'cadvisor'
  #   static_configs:
  #     - targets: ['cadvisor:8080']

  # Application metrics
  # Add your application endpoints here
  # - job_name: 'app'
  #   static_configs:
  #     - targets: ['app:8080']
  #   metrics_path: '/metrics'

  # Example: Traefik metrics
  - job_name: 'traefik'
    static_configs:
      - targets: ['traefik:8082']

  # Example: Redis exporter (replaces ElastiCache CloudWatch metrics)
  # - job_name: 'redis'
  #   static_configs:
  #     - targets: ['redis-exporter:9121']

  # Example: PostgreSQL exporter (replaces RDS CloudWatch metrics)
  # - job_name: 'postgres'
  #   static_configs:
  #     - targets: ['postgres-exporter:9187']

  # Service discovery for Docker containers
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

func (m *CloudWatchMetricsMapper) generateGrafanaDatasource() string {
	return `# Grafana Datasource Configuration
apiVersion: 1

datasources:
  - name: Prometheus
    type: prometheus
    access: proxy
    url: http://prometheus:9090
    isDefault: true
    editable: false

  - name: Loki
    type: loki
    access: proxy
    url: http://loki:3100
    editable: false
`
}

func (m *CloudWatchMetricsMapper) generateExportScript(res *resource.AWSResource) string {
	dashboardName := res.GetConfigString("name")
	if dashboardName == "" {
		dashboardName = res.Name
	}
	region := res.Region
	if region == "" {
		region = "us-east-1"
	}

	return fmt.Sprintf(`#!/bin/bash
# CloudWatch Dashboard Export Script
# Dashboard: %s

set -e

AWS_REGION="%s"
DASHBOARD_NAME="%s"
OUTPUT_DIR="./cloudwatch-export"

echo "Exporting CloudWatch dashboard: $DASHBOARD_NAME"
mkdir -p "$OUTPUT_DIR"

# Export dashboard
echo "Exporting dashboard definition..."
aws cloudwatch get-dashboard \
  --dashboard-name "$DASHBOARD_NAME" \
  --region "$AWS_REGION" \
  --output json > "$OUTPUT_DIR/dashboard-$DASHBOARD_NAME.json"

# Extract dashboard body
jq -r '.DashboardBody' "$OUTPUT_DIR/dashboard-$DASHBOARD_NAME.json" | jq '.' > "$OUTPUT_DIR/dashboard-body.json"

# List all metrics used in the dashboard
echo "Extracting metrics from dashboard..."
jq -r '.. | .metrics? // empty | .[]? | select(type == "array") | .[0:3] | join("/")' \
  "$OUTPUT_DIR/dashboard-body.json" | sort -u > "$OUTPUT_DIR/metrics-list.txt"

# Export metric data for common namespaces
echo "Exporting metric metadata..."
for namespace in AWS/EC2 AWS/RDS AWS/ELB AWS/Lambda AWS/S3; do
  aws cloudwatch list-metrics \
    --namespace "$namespace" \
    --region "$AWS_REGION" \
    --output json > "$OUTPUT_DIR/metrics-$namespace.json" 2>/dev/null || true
done

echo ""
echo "Export complete! Files saved to: $OUTPUT_DIR"
echo ""
echo "Next steps:"
echo "1. Review dashboard-body.json for widget definitions"
echo "2. Run convert-dashboard.sh to convert to Grafana format"
echo "3. Import the converted dashboard into Grafana"
`, dashboardName, region, dashboardName)
}

func (m *CloudWatchMetricsMapper) generateConversionScript(res *resource.AWSResource) string {
	return `#!/bin/bash
# CloudWatch to Grafana Dashboard Conversion Script

set -e

INPUT_FILE="${1:-./cloudwatch-export/dashboard-body.json}"
OUTPUT_FILE="${2:-./config/grafana/dashboards/converted-dashboard.json}"

if [ ! -f "$INPUT_FILE" ]; then
  echo "Error: Input file not found: $INPUT_FILE"
  echo "Run cloudwatch-dashboard-export.sh first"
  exit 1
fi

mkdir -p "$(dirname "$OUTPUT_FILE")"

echo "Converting CloudWatch dashboard to Grafana format..."
echo "Input: $INPUT_FILE"
echo "Output: $OUTPUT_FILE"

# Create basic Grafana dashboard structure
cat > "$OUTPUT_FILE" << 'EOF'
{
  "annotations": {
    "list": []
  },
  "editable": true,
  "fiscalYearStartMonth": 0,
  "graphTooltip": 0,
  "id": null,
  "links": [],
  "liveNow": false,
  "panels": [],
  "refresh": "30s",
  "schemaVersion": 38,
  "tags": ["converted", "cloudwatch"],
  "templating": {
    "list": []
  },
  "time": {
    "from": "now-6h",
    "to": "now"
  },
  "timepicker": {},
  "timezone": "",
  "title": "Converted CloudWatch Dashboard",
  "uid": "",
  "version": 1,
  "weekStart": ""
}
EOF

echo ""
echo "MANUAL CONVERSION REQUIRED"
echo "=========================="
echo ""
echo "CloudWatch dashboards use a different JSON format than Grafana."
echo "You need to manually convert the widgets:"
echo ""
echo "CloudWatch Metric Widget → Grafana Time Series Panel"
echo "CloudWatch Text Widget   → Grafana Text Panel"
echo "CloudWatch Alarm Widget  → Grafana Alert List Panel"
echo ""
echo "Example CloudWatch metric expression:"
echo '  ["AWS/EC2", "CPUUtilization", "InstanceId", "i-xxx"]'
echo ""
echo "Equivalent Prometheus query (with node_exporter):"
echo '  100 - (avg by(instance) (irate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)'
echo ""
echo "See config/prometheus/cloudwatch-metrics-mapping.md for common conversions."
`
}

func (m *CloudWatchMetricsMapper) generateMetricsMapping() string {
	return `# CloudWatch to Prometheus Metrics Mapping

## EC2 Metrics → Node Exporter

| CloudWatch Metric | Prometheus Metric |
|-------------------|-------------------|
| CPUUtilization | 100 - (avg(irate(node_cpu_seconds_total{mode="idle"}[5m])) * 100) |
| DiskReadBytes | rate(node_disk_read_bytes_total[5m]) |
| DiskWriteBytes | rate(node_disk_written_bytes_total[5m]) |
| NetworkIn | rate(node_network_receive_bytes_total[5m]) |
| NetworkOut | rate(node_network_transmit_bytes_total[5m]) |
| MemoryUtilization | (1 - node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes) * 100 |

## RDS Metrics → PostgreSQL Exporter

| CloudWatch Metric | Prometheus Metric |
|-------------------|-------------------|
| DatabaseConnections | pg_stat_activity_count |
| CPUUtilization | process_cpu_seconds_total |
| FreeableMemory | pg_settings_shared_buffers_bytes |
| ReadIOPS | rate(pg_stat_database_blks_read[5m]) |
| WriteIOPS | rate(pg_stat_database_blks_hit[5m]) |

## ELB/ALB Metrics → Traefik

| CloudWatch Metric | Prometheus Metric |
|-------------------|-------------------|
| RequestCount | traefik_entrypoint_requests_total |
| TargetResponseTime | traefik_entrypoint_request_duration_seconds |
| HTTPCode_Target_2XX_Count | traefik_entrypoint_requests_total{code=~"2.."} |
| HTTPCode_Target_5XX_Count | traefik_entrypoint_requests_total{code=~"5.."} |

## Lambda Metrics → Application Metrics

| CloudWatch Metric | Prometheus Metric |
|-------------------|-------------------|
| Invocations | http_requests_total |
| Duration | http_request_duration_seconds |
| Errors | http_requests_total{status=~"5.."} |
| ConcurrentExecutions | process_open_fds |

## ElastiCache/Redis Metrics → Redis Exporter

| CloudWatch Metric | Prometheus Metric |
|-------------------|-------------------|
| CurrConnections | redis_connected_clients |
| CacheHits | redis_keyspace_hits_total |
| CacheMisses | redis_keyspace_misses_total |
| BytesUsedForCache | redis_memory_used_bytes |
| CPUUtilization | rate(redis_cpu_user_seconds_total[5m]) |

## S3 Metrics

S3 metrics require a custom exporter or application-level metrics.

## Common PromQL Patterns

### Rate calculation (CloudWatch Sum with Period)
CloudWatch: Sum over 5 minutes
Prometheus: rate(metric_total[5m]) * 300

### Average (CloudWatch Average)
Prometheus: avg_over_time(metric[5m])

### Maximum (CloudWatch Maximum)
Prometheus: max_over_time(metric[5m])

### Percentile (CloudWatch p99)
Prometheus: histogram_quantile(0.99, rate(metric_bucket[5m]))
`
}

func (m *CloudWatchMetricsMapper) addMigrationWarnings(result *mapper.MappingResult, res *resource.AWSResource, dashboardName string) {
	// Dashboard body warning
	if dashboardBody := res.GetConfigString("dashboard_body"); dashboardBody != "" {
		result.AddWarning("CloudWatch dashboard JSON needs manual conversion to Grafana format.")
	}

	// Standard warnings
	result.AddWarning("CloudWatch metrics use different naming than Prometheus. Review metrics mapping.")
	result.AddWarning("CloudWatch dashboards use different widget formats. Manual conversion required.")

	// Manual steps
	result.AddManualStep("Run scripts/cloudwatch-dashboard-export.sh to export dashboard")
	result.AddManualStep("Review config/prometheus/cloudwatch-metrics-mapping.md for metric equivalents")
	result.AddManualStep("Create Grafana dashboards using Prometheus datasource")
	result.AddManualStep("Access Grafana at http://grafana.localhost (admin/admin)")
	result.AddManualStep("Access Prometheus at http://prometheus.localhost")
	result.AddManualStep("Install exporters for each service (node, postgres, redis, etc.)")

	// Volumes
	result.AddVolume(mapper.Volume{
		Name:   "prometheus-data",
		Driver: "local",
	})
	result.AddVolume(mapper.Volume{
		Name:   "grafana-data",
		Driver: "local",
	})
}
