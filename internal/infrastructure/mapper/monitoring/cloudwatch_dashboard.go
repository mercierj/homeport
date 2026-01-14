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

// CloudWatchDashboardMapper converts AWS CloudWatch Dashboards to Grafana.
type CloudWatchDashboardMapper struct {
	*mapper.BaseMapper
}

// NewCloudWatchDashboardMapper creates a new CloudWatch Dashboard to Grafana mapper.
func NewCloudWatchDashboardMapper() *CloudWatchDashboardMapper {
	return &CloudWatchDashboardMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeCloudWatchDashboard, nil),
	}
}

// Map converts a CloudWatch Dashboard to a Grafana service.
func (m *CloudWatchDashboardMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	dashboardName := res.GetConfigString("dashboard_name")
	if dashboardName == "" {
		dashboardName = res.Name
	}

	result := mapper.NewMappingResult("grafana")
	svc := result.DockerService

	// Configure Grafana service
	svc.Image = "grafana/grafana:10.2.0"
	svc.Ports = []string{
		"3000:3000",
	}
	svc.Volumes = []string{
		"./data/grafana:/var/lib/grafana",
		"./config/grafana/provisioning:/etc/grafana/provisioning",
		"./config/grafana/dashboards:/var/lib/grafana/dashboards",
	}
	svc.Environment = map[string]string{
		"GF_SECURITY_ADMIN_USER":       "${GRAFANA_ADMIN_USER:-admin}",
		"GF_SECURITY_ADMIN_PASSWORD":   "${GRAFANA_ADMIN_PASSWORD:-admin}",
		"GF_USERS_ALLOW_SIGN_UP":       "false",
		"GF_SERVER_ROOT_URL":           "http://grafana.localhost",
		"GF_INSTALL_PLUGINS":           "grafana-clock-panel,grafana-piechart-panel",
		"GF_DASHBOARDS_DEFAULT_HOME_DASHBOARD_PATH": "/var/lib/grafana/dashboards/home.json",
	}
	svc.Networks = []string{"homeport"}
	svc.Labels = map[string]string{
		"homeport.source":         "aws_cloudwatch_dashboard",
		"homeport.dashboard_name": dashboardName,
		"traefik.enable":          "true",
		"traefik.http.routers.grafana.rule":                      "Host(`grafana.localhost`)",
		"traefik.http.services.grafana.loadbalancer.server.port": "3000",
	}
	svc.Restart = "unless-stopped"
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD-SHELL", "wget --no-verbose --tries=1 --spider http://localhost:3000/api/health || exit 1"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  3,
	}

	// Generate Grafana provisioning configuration for datasources
	datasourcesConfig := m.generateDatasourcesConfig()
	result.AddConfig("config/grafana/provisioning/datasources/datasources.yaml", []byte(datasourcesConfig))

	// Generate Grafana provisioning configuration for dashboards
	dashboardProvisioningConfig := m.generateDashboardProvisioningConfig()
	result.AddConfig("config/grafana/provisioning/dashboards/dashboards.yaml", []byte(dashboardProvisioningConfig))

	// Generate Grafana dashboard JSON from CloudWatch dashboard config
	grafanaDashboard := m.convertCloudWatchDashboard(res, dashboardName)
	result.AddConfig("config/grafana/dashboards/home.json", []byte(grafanaDashboard))

	// Generate export script
	exportScript := m.generateExportScript(res)
	result.AddScript("scripts/cloudwatch-dashboard-export.sh", []byte(exportScript))

	// Generate conversion script
	conversionScript := m.generateConversionScript()
	result.AddScript("scripts/convert-dashboard.sh", []byte(conversionScript))

	// Generate import script
	importScript := m.generateImportScript()
	result.AddScript("scripts/grafana-import.sh", []byte(importScript))

	// Add warnings and manual steps
	m.addMigrationWarnings(result, res, dashboardName)

	return result, nil
}

// generateDatasourcesConfig creates the Grafana datasources provisioning configuration.
func (m *CloudWatchDashboardMapper) generateDatasourcesConfig() string {
	return `# Grafana Datasources Provisioning
# Auto-provisioned datasources for migrated CloudWatch dashboards

apiVersion: 1

datasources:
  # Prometheus - for metrics
  - name: Prometheus
    type: prometheus
    access: proxy
    url: http://prometheus:9090
    isDefault: true
    editable: false
    jsonData:
      timeInterval: "15s"
      httpMethod: POST

  # Loki - for logs
  - name: Loki
    type: loki
    access: proxy
    url: http://loki:3100
    editable: false
    jsonData:
      maxLines: 1000

  # Alertmanager - for alerts
  - name: Alertmanager
    type: alertmanager
    access: proxy
    url: http://alertmanager:9093
    editable: false
    jsonData:
      implementation: prometheus

  # Add additional datasources as needed:
  #
  # - name: PostgreSQL
  #   type: postgres
  #   url: postgresql:5432
  #   database: grafana
  #   user: grafana
  #   secureJsonData:
  #     password: ${POSTGRES_PASSWORD}
  #
  # - name: InfluxDB
  #   type: influxdb
  #   url: http://influxdb:8086
  #   database: metrics
`
}

// generateDashboardProvisioningConfig creates the Grafana dashboard provisioning configuration.
func (m *CloudWatchDashboardMapper) generateDashboardProvisioningConfig() string {
	return `# Grafana Dashboard Provisioning
# Auto-provision dashboards from filesystem

apiVersion: 1

providers:
  - name: 'default'
    orgId: 1
    folder: 'Migrated from CloudWatch'
    folderUid: cloudwatch-migrated
    type: file
    disableDeletion: false
    updateIntervalSeconds: 30
    allowUiUpdates: true
    options:
      path: /var/lib/grafana/dashboards
      foldersFromFilesStructure: false
`
}

// convertCloudWatchDashboard converts a CloudWatch dashboard JSON to Grafana format.
func (m *CloudWatchDashboardMapper) convertCloudWatchDashboard(res *resource.AWSResource, dashboardName string) string {
	// Try to parse the CloudWatch dashboard body
	dashboardBody := res.GetConfigString("dashboard_body")

	var cwWidgets []CloudWatchWidget
	if dashboardBody != "" {
		var cwDashboard CloudWatchDashboard
		if err := json.Unmarshal([]byte(dashboardBody), &cwDashboard); err == nil {
			cwWidgets = cwDashboard.Widgets
		}
	}

	// Convert CloudWatch widgets to Grafana panels
	panels := m.convertWidgetsToPanels(cwWidgets)

	// Build Grafana dashboard
	grafanaDashboard := GrafanaDashboard{
		ID:            nil,
		UID:           m.sanitizeName(dashboardName),
		Title:         dashboardName,
		Description:   fmt.Sprintf("Migrated from CloudWatch Dashboard: %s", dashboardName),
		Tags:          []string{"cloudwatch-migration", "auto-generated"},
		Style:         "dark",
		Timezone:      "browser",
		Editable:      true,
		GraphTooltip:  1,
		Refresh:       "30s",
		SchemaVersion: 38,
		Version:       1,
		Panels:        panels,
		Time: GrafanaTimeRange{
			From: "now-6h",
			To:   "now",
		},
		Templating: GrafanaTemplating{
			List: []GrafanaVariable{
				{
					Name:    "datasource",
					Label:   "Datasource",
					Type:    "datasource",
					Query:   "prometheus",
					Current: map[string]interface{}{"text": "Prometheus", "value": "Prometheus"},
				},
			},
		},
		Annotations: GrafanaAnnotations{
			List: []GrafanaAnnotation{
				{
					Name:       "Annotations & Alerts",
					Datasource: map[string]string{"type": "grafana", "uid": "-- Grafana --"},
					Enable:     true,
					Hide:       true,
					IconColor:  "rgba(0, 211, 255, 1)",
					Type:       "dashboard",
				},
			},
		},
	}

	content, _ := json.MarshalIndent(grafanaDashboard, "", "  ")
	return string(content)
}

// convertWidgetsToPanels converts CloudWatch widgets to Grafana panels.
func (m *CloudWatchDashboardMapper) convertWidgetsToPanels(widgets []CloudWatchWidget) []GrafanaPanel {
	var panels []GrafanaPanel

	for i, widget := range widgets {
		panel := m.convertWidget(widget, i+1)
		if panel != nil {
			panels = append(panels, *panel)
		}
	}

	// Add default panels if no widgets found
	if len(panels) == 0 {
		panels = m.generateDefaultPanels()
	}

	return panels
}

// convertWidget converts a single CloudWatch widget to a Grafana panel.
func (m *CloudWatchDashboardMapper) convertWidget(widget CloudWatchWidget, id int) *GrafanaPanel {
	panel := &GrafanaPanel{
		ID:    id,
		Title: m.getWidgetTitle(widget),
		GridPos: GrafanaGridPos{
			X: widget.X,
			Y: widget.Y,
			W: widget.Width,
			H: widget.Height,
		},
		Datasource: map[string]string{
			"type": "prometheus",
			"uid":  "${datasource}",
		},
	}

	// Set default grid position if not specified
	if panel.GridPos.W == 0 {
		panel.GridPos.W = 12
	}
	if panel.GridPos.H == 0 {
		panel.GridPos.H = 8
	}

	switch widget.Type {
	case "metric":
		panel.Type = "timeseries"
		panel.Targets = m.convertMetricTargets(widget)
		panel.FieldConfig = m.defaultFieldConfig()
		panel.Options = map[string]interface{}{
			"legend": map[string]interface{}{
				"displayMode": "list",
				"placement":   "bottom",
				"showLegend":  true,
			},
			"tooltip": map[string]interface{}{
				"mode": "single",
				"sort": "none",
			},
		}

	case "text":
		panel.Type = "text"
		panel.Options = map[string]interface{}{
			"mode":    "markdown",
			"content": m.getWidgetMarkdown(widget),
		}

	case "log":
		panel.Type = "logs"
		panel.Datasource = map[string]string{
			"type": "loki",
			"uid":  "loki",
		}
		panel.Targets = []GrafanaTarget{
			{
				RefID:      "A",
				Datasource: map[string]string{"type": "loki", "uid": "loki"},
				Expr:       "{job=~\".+\"} |= ``",
			},
		}

	case "alarm":
		panel.Type = "alertlist"
		panel.Datasource = map[string]string{
			"type": "alertmanager",
			"uid":  "alertmanager",
		}
		panel.Options = map[string]interface{}{
			"alertName":    "",
			"dashboardAlerts": false,
			"maxItems":     20,
			"sortOrder":    1,
			"stateFilter":  map[string]bool{"firing": true, "pending": true},
		}

	default:
		// Default to stat panel
		panel.Type = "stat"
		panel.Targets = m.convertMetricTargets(widget)
		panel.FieldConfig = m.defaultFieldConfig()
		panel.Options = map[string]interface{}{
			"reduceOptions": map[string]interface{}{
				"calcs":  []string{"lastNotNull"},
				"fields": "",
				"values": false,
			},
			"textMode":    "auto",
			"colorMode":   "value",
			"graphMode":   "area",
			"justifyMode": "auto",
		}
	}

	return panel
}

// convertMetricTargets converts CloudWatch metrics to Prometheus queries.
func (m *CloudWatchDashboardMapper) convertMetricTargets(widget CloudWatchWidget) []GrafanaTarget {
	var targets []GrafanaTarget

	if widget.Properties == nil {
		// Return a placeholder target
		return []GrafanaTarget{
			{
				RefID:      "A",
				Datasource: map[string]string{"type": "prometheus", "uid": "${datasource}"},
				Expr:       "# TODO: Replace with actual Prometheus query",
				LegendFormat: "{{instance}}",
			},
		}
	}

	metrics, ok := widget.Properties["metrics"].([]interface{})
	if !ok {
		return []GrafanaTarget{
			{
				RefID:      "A",
				Datasource: map[string]string{"type": "prometheus", "uid": "${datasource}"},
				Expr:       "# TODO: Replace with actual Prometheus query",
				LegendFormat: "{{instance}}",
			},
		}
	}

	refID := 'A'
	for _, metric := range metrics {
		metricArray, ok := metric.([]interface{})
		if !ok || len(metricArray) < 2 {
			continue
		}

		namespace := ""
		metricName := ""
		if len(metricArray) >= 1 {
			if ns, ok := metricArray[0].(string); ok {
				namespace = ns
			}
		}
		if len(metricArray) >= 2 {
			if mn, ok := metricArray[1].(string); ok {
				metricName = mn
			}
		}

		promQuery := m.convertMetricToPromQL(namespace, metricName)

		targets = append(targets, GrafanaTarget{
			RefID:        string(refID),
			Datasource:   map[string]string{"type": "prometheus", "uid": "${datasource}"},
			Expr:         promQuery,
			LegendFormat: fmt.Sprintf("%s/%s", namespace, metricName),
		})

		refID++
	}

	if len(targets) == 0 {
		return []GrafanaTarget{
			{
				RefID:      "A",
				Datasource: map[string]string{"type": "prometheus", "uid": "${datasource}"},
				Expr:       "# TODO: Replace with actual Prometheus query",
				LegendFormat: "{{instance}}",
			},
		}
	}

	return targets
}

// convertMetricToPromQL converts a CloudWatch metric to a PromQL query.
func (m *CloudWatchDashboardMapper) convertMetricToPromQL(namespace, metricName string) string {
	key := fmt.Sprintf("%s/%s", namespace, metricName)

	mappings := map[string]string{
		// EC2 metrics
		"AWS/EC2/CPUUtilization":   "100 - (avg by(instance) (irate(node_cpu_seconds_total{mode=\"idle\"}[5m])) * 100)",
		"AWS/EC2/NetworkIn":        "rate(node_network_receive_bytes_total[5m])",
		"AWS/EC2/NetworkOut":       "rate(node_network_transmit_bytes_total[5m])",
		"AWS/EC2/DiskReadBytes":    "rate(node_disk_read_bytes_total[5m])",
		"AWS/EC2/DiskWriteBytes":   "rate(node_disk_written_bytes_total[5m])",
		"AWS/EC2/StatusCheckFailed": "up == 0",

		// RDS metrics
		"AWS/RDS/CPUUtilization":       "rate(process_cpu_seconds_total[5m]) * 100",
		"AWS/RDS/DatabaseConnections":  "pg_stat_activity_count",
		"AWS/RDS/FreeableMemory":       "pg_settings_shared_buffers_bytes",
		"AWS/RDS/ReadIOPS":             "rate(pg_stat_bgwriter_buffers_backend_total[5m])",
		"AWS/RDS/WriteIOPS":            "rate(pg_stat_bgwriter_buffers_alloc_total[5m])",

		// ELB/ALB metrics
		"AWS/ELB/RequestCount":         "rate(traefik_entrypoint_requests_total[5m])",
		"AWS/ELB/Latency":              "histogram_quantile(0.99, rate(traefik_entrypoint_request_duration_seconds_bucket[5m]))",
		"AWS/ELB/HTTPCode_ELB_5XX":     "rate(traefik_entrypoint_requests_total{code=~\"5..\"}[5m])",
		"AWS/ELB/HTTPCode_Target_2XX":  "rate(traefik_entrypoint_requests_total{code=~\"2..\"}[5m])",
		"AWS/ApplicationELB/RequestCount": "rate(traefik_service_requests_total[5m])",

		// Lambda metrics
		"AWS/Lambda/Invocations": "rate(http_requests_total[5m])",
		"AWS/Lambda/Errors":      "rate(http_requests_total{status=~\"5..\"}[5m])",
		"AWS/Lambda/Duration":    "histogram_quantile(0.99, rate(http_request_duration_seconds_bucket[5m]))",
		"AWS/Lambda/Throttles":   "rate(http_requests_total{status=\"429\"}[5m])",

		// ElastiCache metrics
		"AWS/ElastiCache/CPUUtilization":    "rate(redis_cpu_user_seconds_total[5m]) * 100",
		"AWS/ElastiCache/CurrConnections":   "redis_connected_clients",
		"AWS/ElastiCache/CacheHits":         "rate(redis_keyspace_hits_total[5m])",
		"AWS/ElastiCache/CacheMisses":       "rate(redis_keyspace_misses_total[5m])",

		// SQS metrics
		"AWS/SQS/NumberOfMessagesReceived": "rate(rabbitmq_queue_messages_delivered_total[5m])",
		"AWS/SQS/NumberOfMessagesSent":     "rate(rabbitmq_queue_messages_published_total[5m])",
		"AWS/SQS/ApproximateNumberOfMessagesVisible": "rabbitmq_queue_messages_ready",

		// DynamoDB metrics
		"AWS/DynamoDB/ConsumedReadCapacityUnits":  "rate(mongodb_op_counters_total{type=\"query\"}[5m])",
		"AWS/DynamoDB/ConsumedWriteCapacityUnits": "rate(mongodb_op_counters_total{type=\"insert\"}[5m])",
	}

	if promQuery, ok := mappings[key]; ok {
		return promQuery
	}

	// Return a placeholder with helpful comment
	return fmt.Sprintf("# TODO: Map CloudWatch metric %s to Prometheus\n# Original: %s/%s\nup", key, namespace, metricName)
}

// getWidgetTitle extracts the title from a CloudWatch widget.
func (m *CloudWatchDashboardMapper) getWidgetTitle(widget CloudWatchWidget) string {
	if widget.Properties == nil {
		return "Untitled Panel"
	}

	if title, ok := widget.Properties["title"].(string); ok && title != "" {
		return title
	}

	return "Untitled Panel"
}

// getWidgetMarkdown extracts markdown content from a text widget.
func (m *CloudWatchDashboardMapper) getWidgetMarkdown(widget CloudWatchWidget) string {
	if widget.Properties == nil {
		return "# Dashboard Panel\n\nEdit this panel to add content."
	}

	if markdown, ok := widget.Properties["markdown"].(string); ok {
		return markdown
	}

	return "# Dashboard Panel\n\nEdit this panel to add content."
}

// defaultFieldConfig returns default Grafana field configuration.
func (m *CloudWatchDashboardMapper) defaultFieldConfig() map[string]interface{} {
	return map[string]interface{}{
		"defaults": map[string]interface{}{
			"color": map[string]interface{}{
				"mode": "palette-classic",
			},
			"custom": map[string]interface{}{
				"axisCenteredZero": false,
				"axisColorMode":    "text",
				"axisLabel":        "",
				"axisPlacement":    "auto",
				"barAlignment":     0,
				"drawStyle":        "line",
				"fillOpacity":      10,
				"gradientMode":     "none",
				"hideFrom": map[string]bool{
					"legend":  false,
					"tooltip": false,
					"viz":     false,
				},
				"lineInterpolation": "linear",
				"lineWidth":         1,
				"pointSize":         5,
				"scaleDistribution": map[string]string{
					"type": "linear",
				},
				"showPoints":   "auto",
				"spanNulls":    false,
				"stacking":     map[string]string{"group": "A", "mode": "none"},
				"thresholdsStyle": map[string]string{"mode": "off"},
			},
			"mappings": []interface{}{},
			"thresholds": map[string]interface{}{
				"mode": "absolute",
				"steps": []map[string]interface{}{
					{"color": "green", "value": nil},
					{"color": "red", "value": 80},
				},
			},
		},
		"overrides": []interface{}{},
	}
}

// generateDefaultPanels creates default panels when no widgets are found.
func (m *CloudWatchDashboardMapper) generateDefaultPanels() []GrafanaPanel {
	return []GrafanaPanel{
		{
			ID:    1,
			Title: "System Overview",
			Type:  "row",
			GridPos: GrafanaGridPos{
				X: 0, Y: 0, W: 24, H: 1,
			},
			Collapsed: false,
		},
		{
			ID:    2,
			Title: "CPU Usage",
			Type:  "timeseries",
			GridPos: GrafanaGridPos{
				X: 0, Y: 1, W: 12, H: 8,
			},
			Datasource: map[string]string{"type": "prometheus", "uid": "${datasource}"},
			Targets: []GrafanaTarget{
				{
					RefID:        "A",
					Datasource:   map[string]string{"type": "prometheus", "uid": "${datasource}"},
					Expr:         "100 - (avg by(instance) (irate(node_cpu_seconds_total{mode=\"idle\"}[5m])) * 100)",
					LegendFormat: "{{instance}}",
				},
			},
			FieldConfig: m.defaultFieldConfig(),
			Options: map[string]interface{}{
				"legend":  map[string]interface{}{"displayMode": "list", "placement": "bottom"},
				"tooltip": map[string]interface{}{"mode": "single"},
			},
		},
		{
			ID:    3,
			Title: "Memory Usage",
			Type:  "timeseries",
			GridPos: GrafanaGridPos{
				X: 12, Y: 1, W: 12, H: 8,
			},
			Datasource: map[string]string{"type": "prometheus", "uid": "${datasource}"},
			Targets: []GrafanaTarget{
				{
					RefID:        "A",
					Datasource:   map[string]string{"type": "prometheus", "uid": "${datasource}"},
					Expr:         "(1 - node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes) * 100",
					LegendFormat: "{{instance}}",
				},
			},
			FieldConfig: m.defaultFieldConfig(),
			Options: map[string]interface{}{
				"legend":  map[string]interface{}{"displayMode": "list", "placement": "bottom"},
				"tooltip": map[string]interface{}{"mode": "single"},
			},
		},
		{
			ID:    4,
			Title: "Network Traffic",
			Type:  "timeseries",
			GridPos: GrafanaGridPos{
				X: 0, Y: 9, W: 12, H: 8,
			},
			Datasource: map[string]string{"type": "prometheus", "uid": "${datasource}"},
			Targets: []GrafanaTarget{
				{
					RefID:        "A",
					Datasource:   map[string]string{"type": "prometheus", "uid": "${datasource}"},
					Expr:         "rate(node_network_receive_bytes_total[5m])",
					LegendFormat: "{{device}} received",
				},
				{
					RefID:        "B",
					Datasource:   map[string]string{"type": "prometheus", "uid": "${datasource}"},
					Expr:         "rate(node_network_transmit_bytes_total[5m])",
					LegendFormat: "{{device}} transmitted",
				},
			},
			FieldConfig: m.defaultFieldConfig(),
			Options: map[string]interface{}{
				"legend":  map[string]interface{}{"displayMode": "list", "placement": "bottom"},
				"tooltip": map[string]interface{}{"mode": "single"},
			},
		},
		{
			ID:    5,
			Title: "Disk I/O",
			Type:  "timeseries",
			GridPos: GrafanaGridPos{
				X: 12, Y: 9, W: 12, H: 8,
			},
			Datasource: map[string]string{"type": "prometheus", "uid": "${datasource}"},
			Targets: []GrafanaTarget{
				{
					RefID:        "A",
					Datasource:   map[string]string{"type": "prometheus", "uid": "${datasource}"},
					Expr:         "rate(node_disk_read_bytes_total[5m])",
					LegendFormat: "{{device}} read",
				},
				{
					RefID:        "B",
					Datasource:   map[string]string{"type": "prometheus", "uid": "${datasource}"},
					Expr:         "rate(node_disk_written_bytes_total[5m])",
					LegendFormat: "{{device}} write",
				},
			},
			FieldConfig: m.defaultFieldConfig(),
			Options: map[string]interface{}{
				"legend":  map[string]interface{}{"displayMode": "list", "placement": "bottom"},
				"tooltip": map[string]interface{}{"mode": "single"},
			},
		},
	}
}

// sanitizeName converts a name to a valid Grafana UID.
func (m *CloudWatchDashboardMapper) sanitizeName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.ReplaceAll(name, "_", "-")
	// Remove any characters that aren't alphanumeric or hyphens
	var result strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// generateExportScript creates a script to export CloudWatch dashboard.
func (m *CloudWatchDashboardMapper) generateExportScript(res *resource.AWSResource) string {
	dashboardName := res.GetConfigString("dashboard_name")
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
OUTPUT_DIR="./cloudwatch-dashboard-export"

echo "============================================"
echo "Exporting CloudWatch Dashboard"
echo "============================================"
echo "Dashboard: $DASHBOARD_NAME"
echo "Region: $AWS_REGION"
echo ""

mkdir -p "$OUTPUT_DIR"

# Export dashboard definition
echo "Exporting dashboard definition..."
aws cloudwatch get-dashboard \
  --dashboard-name "$DASHBOARD_NAME" \
  --region "$AWS_REGION" \
  --output json > "$OUTPUT_DIR/$DASHBOARD_NAME.json"

# Extract dashboard body
echo "Extracting dashboard body..."
jq -r '.DashboardBody' "$OUTPUT_DIR/$DASHBOARD_NAME.json" | jq '.' > "$OUTPUT_DIR/$DASHBOARD_NAME-body.json"

# List all dashboards
echo "Listing all dashboards..."
aws cloudwatch list-dashboards \
  --region "$AWS_REGION" \
  --output json > "$OUTPUT_DIR/all-dashboards.json"

# Export dashboard metrics info
echo "Analyzing dashboard widgets..."
jq -r '.widgets[].properties.metrics // empty' "$OUTPUT_DIR/$DASHBOARD_NAME-body.json" 2>/dev/null | \
  jq -s 'flatten | unique' > "$OUTPUT_DIR/dashboard-metrics.json" || echo "[]" > "$OUTPUT_DIR/dashboard-metrics.json"

echo ""
echo "============================================"
echo "Export complete!"
echo "============================================"
echo "Files saved to: $OUTPUT_DIR"
echo ""
echo "Files created:"
echo "  - $DASHBOARD_NAME.json (raw API response)"
echo "  - $DASHBOARD_NAME-body.json (dashboard definition)"
echo "  - all-dashboards.json (list of all dashboards)"
echo "  - dashboard-metrics.json (metrics used in dashboard)"
echo ""
echo "Next steps:"
echo "1. Run convert-dashboard.sh to generate Grafana dashboard"
echo "2. Run grafana-import.sh to import into Grafana"
`, dashboardName, region, dashboardName)
}

// generateConversionScript creates a script to convert CloudWatch dashboard to Grafana.
func (m *CloudWatchDashboardMapper) generateConversionScript() string {
	return `#!/bin/bash
# CloudWatch to Grafana Dashboard Conversion Script

set -e

INPUT_FILE="${1:-./cloudwatch-dashboard-export/*-body.json}"
OUTPUT_DIR="${2:-./config/grafana/dashboards}"

# Find the first matching file if glob pattern used
if [[ "$INPUT_FILE" == *"*"* ]]; then
  INPUT_FILE=$(ls $INPUT_FILE 2>/dev/null | head -1)
fi

if [ ! -f "$INPUT_FILE" ]; then
  echo "Error: Input file not found: $INPUT_FILE"
  echo "Run cloudwatch-dashboard-export.sh first"
  exit 1
fi

mkdir -p "$OUTPUT_DIR"

DASHBOARD_NAME=$(basename "$INPUT_FILE" -body.json)
OUTPUT_FILE="$OUTPUT_DIR/${DASHBOARD_NAME}.json"

echo "============================================"
echo "Converting CloudWatch Dashboard to Grafana"
echo "============================================"
echo "Input: $INPUT_FILE"
echo "Output: $OUTPUT_FILE"
echo ""

# This is a simplified conversion - the actual mapper does more
# This script is for manual adjustments

cat > "$OUTPUT_FILE" << 'GRAFANA_EOF'
{
  "annotations": {
    "list": [
      {
        "builtIn": 1,
        "datasource": { "type": "grafana", "uid": "-- Grafana --" },
        "enable": true,
        "hide": true,
        "iconColor": "rgba(0, 211, 255, 1)",
        "name": "Annotations & Alerts",
        "type": "dashboard"
      }
    ]
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
  "style": "dark",
  "tags": ["cloudwatch-migration", "converted"],
  "templating": {
    "list": [
      {
        "current": { "text": "Prometheus", "value": "Prometheus" },
        "hide": 0,
        "includeAll": false,
        "multi": false,
        "name": "datasource",
        "options": [],
        "query": "prometheus",
        "queryValue": "",
        "refresh": 1,
        "regex": "",
        "skipUrlSync": false,
        "type": "datasource"
      }
    ]
  },
  "time": { "from": "now-6h", "to": "now" },
  "timepicker": {},
  "timezone": "browser",
  "title": "DASHBOARD_TITLE_PLACEHOLDER",
  "uid": "DASHBOARD_UID_PLACEHOLDER",
  "version": 1,
  "weekStart": ""
}
GRAFANA_EOF

# Update title and UID
sed -i.bak "s/DASHBOARD_TITLE_PLACEHOLDER/$DASHBOARD_NAME/g" "$OUTPUT_FILE"
sed -i.bak "s/DASHBOARD_UID_PLACEHOLDER/$(echo $DASHBOARD_NAME | tr '[:upper:]' '[:lower:]' | tr ' _' '-')/g" "$OUTPUT_FILE"
rm -f "$OUTPUT_FILE.bak"

echo "Basic Grafana dashboard created."
echo ""
echo "IMPORTANT: Manual steps required:"
echo "1. Open $OUTPUT_FILE in a text editor"
echo "2. Add panels to the 'panels' array"
echo "3. Map CloudWatch metrics to Prometheus queries"
echo ""
echo "Reference CloudWatch widgets from: $INPUT_FILE"
echo ""
echo "Common metric mappings:"
echo "  AWS/EC2/CPUUtilization -> 100 - (avg(irate(node_cpu_seconds_total{mode=\"idle\"}[5m])) * 100)"
echo "  AWS/RDS/DatabaseConnections -> pg_stat_activity_count"
echo "  AWS/ELB/RequestCount -> rate(traefik_entrypoint_requests_total[5m])"
echo ""
echo "Run grafana-import.sh when ready to import"
`
}

// generateImportScript creates a script to import dashboards into Grafana.
func (m *CloudWatchDashboardMapper) generateImportScript() string {
	return `#!/bin/bash
# Grafana Dashboard Import Script

set -e

GRAFANA_URL="${GRAFANA_URL:-http://localhost:3000}"
GRAFANA_USER="${GRAFANA_ADMIN_USER:-admin}"
GRAFANA_PASS="${GRAFANA_ADMIN_PASSWORD:-admin}"
DASHBOARD_DIR="${1:-./config/grafana/dashboards}"

echo "============================================"
echo "Importing Dashboards to Grafana"
echo "============================================"
echo "Grafana URL: $GRAFANA_URL"
echo "Dashboard directory: $DASHBOARD_DIR"
echo ""

# Wait for Grafana to be ready
echo "Waiting for Grafana to be ready..."
until curl -sf "$GRAFANA_URL/api/health" > /dev/null 2>&1; do
  echo "Waiting..."
  sleep 2
done
echo "Grafana is ready!"
echo ""

# Create folder for migrated dashboards
echo "Creating folder for migrated dashboards..."
FOLDER_RESPONSE=$(curl -s -X POST "$GRAFANA_URL/api/folders" \
  -u "$GRAFANA_USER:$GRAFANA_PASS" \
  -H "Content-Type: application/json" \
  -d '{"title": "Migrated from CloudWatch"}' || true)

FOLDER_UID=$(echo "$FOLDER_RESPONSE" | jq -r '.uid // "cloudwatch-migrated"')
echo "Using folder UID: $FOLDER_UID"
echo ""

# Import each dashboard
for dashboard_file in "$DASHBOARD_DIR"/*.json; do
  if [ -f "$dashboard_file" ]; then
    filename=$(basename "$dashboard_file")
    echo "Importing: $filename"

    # Wrap dashboard in import format
    IMPORT_PAYLOAD=$(jq -n --slurpfile dashboard "$dashboard_file" '{
      "dashboard": $dashboard[0],
      "overwrite": true,
      "inputs": [],
      "folderId": 0,
      "folderUid": "'"$FOLDER_UID"'"
    }')

    RESPONSE=$(curl -s -X POST "$GRAFANA_URL/api/dashboards/import" \
      -u "$GRAFANA_USER:$GRAFANA_PASS" \
      -H "Content-Type: application/json" \
      -d "$IMPORT_PAYLOAD")

    if echo "$RESPONSE" | jq -e '.uid' > /dev/null 2>&1; then
      DASHBOARD_UID=$(echo "$RESPONSE" | jq -r '.uid')
      echo "  Success! Dashboard UID: $DASHBOARD_UID"
      echo "  URL: $GRAFANA_URL/d/$DASHBOARD_UID"
    else
      echo "  Warning: Import may have failed"
      echo "  Response: $RESPONSE"
    fi
    echo ""
  fi
done

echo "============================================"
echo "Import complete!"
echo "============================================"
echo ""
echo "Access Grafana at: $GRAFANA_URL"
echo "Credentials: $GRAFANA_USER / $GRAFANA_PASS"
echo ""
echo "Dashboards are in folder: 'Migrated from CloudWatch'"
`
}

// addMigrationWarnings adds warnings and manual steps for the migration.
func (m *CloudWatchDashboardMapper) addMigrationWarnings(result *mapper.MappingResult, res *resource.AWSResource, dashboardName string) {
	// Dashboard body warning
	dashboardBody := res.GetConfigString("dashboard_body")
	if dashboardBody == "" {
		result.AddWarning("No dashboard body found. Default panels will be generated.")
	} else {
		var cwDashboard CloudWatchDashboard
		if err := json.Unmarshal([]byte(dashboardBody), &cwDashboard); err != nil {
			result.AddWarning("Could not parse CloudWatch dashboard JSON. Manual conversion required.")
		} else if len(cwDashboard.Widgets) > 0 {
			result.AddWarning(fmt.Sprintf("Found %d widgets in CloudWatch dashboard. Review converted panels.", len(cwDashboard.Widgets)))
		}
	}

	// General warnings
	result.AddWarning("CloudWatch metrics use different names than Prometheus. Review and update panel queries.")
	result.AddWarning("CloudWatch dimension filters need to be converted to Prometheus label selectors.")
	result.AddWarning("CloudWatch math expressions need to be converted to PromQL.")

	// Credential warning
	result.AddWarning("Default Grafana credentials are admin/admin. Change in production using GRAFANA_ADMIN_USER and GRAFANA_ADMIN_PASSWORD environment variables.")

	// Manual steps
	result.AddManualStep("Run scripts/cloudwatch-dashboard-export.sh to export the CloudWatch dashboard")
	result.AddManualStep("Review config/grafana/dashboards/home.json and update PromQL queries")
	result.AddManualStep("Set GRAFANA_ADMIN_USER and GRAFANA_ADMIN_PASSWORD environment variables")
	result.AddManualStep("Access Grafana at http://grafana.localhost")
	result.AddManualStep("Configure additional datasources as needed (PostgreSQL, InfluxDB, etc.)")
	result.AddManualStep("Set up Grafana alerting rules to replace CloudWatch alarms")

	// Volumes
	result.AddVolume(mapper.Volume{
		Name:   "grafana-data",
		Driver: "local",
	})
}

// CloudWatch dashboard types for parsing

// CloudWatchDashboard represents a CloudWatch dashboard structure.
type CloudWatchDashboard struct {
	Widgets []CloudWatchWidget `json:"widgets"`
}

// CloudWatchWidget represents a CloudWatch dashboard widget.
type CloudWatchWidget struct {
	Type       string                 `json:"type"`
	X          int                    `json:"x"`
	Y          int                    `json:"y"`
	Width      int                    `json:"width"`
	Height     int                    `json:"height"`
	Properties map[string]interface{} `json:"properties"`
}

// Grafana dashboard types

// GrafanaDashboard represents a Grafana dashboard structure.
type GrafanaDashboard struct {
	ID            interface{}         `json:"id"`
	UID           string              `json:"uid"`
	Title         string              `json:"title"`
	Description   string              `json:"description,omitempty"`
	Tags          []string            `json:"tags"`
	Style         string              `json:"style"`
	Timezone      string              `json:"timezone"`
	Editable      bool                `json:"editable"`
	GraphTooltip  int                 `json:"graphTooltip"`
	Refresh       string              `json:"refresh"`
	SchemaVersion int                 `json:"schemaVersion"`
	Version       int                 `json:"version"`
	Panels        []GrafanaPanel      `json:"panels"`
	Time          GrafanaTimeRange    `json:"time"`
	Templating    GrafanaTemplating   `json:"templating"`
	Annotations   GrafanaAnnotations  `json:"annotations"`
}

// GrafanaPanel represents a Grafana dashboard panel.
type GrafanaPanel struct {
	ID          int                    `json:"id"`
	Title       string                 `json:"title"`
	Type        string                 `json:"type"`
	GridPos     GrafanaGridPos         `json:"gridPos"`
	Datasource  map[string]string      `json:"datasource,omitempty"`
	Targets     []GrafanaTarget        `json:"targets,omitempty"`
	FieldConfig map[string]interface{} `json:"fieldConfig,omitempty"`
	Options     map[string]interface{} `json:"options,omitempty"`
	Collapsed   bool                   `json:"collapsed,omitempty"`
}

// GrafanaGridPos represents panel position in Grafana grid.
type GrafanaGridPos struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

// GrafanaTarget represents a Grafana panel query target.
type GrafanaTarget struct {
	RefID        string            `json:"refId"`
	Datasource   map[string]string `json:"datasource,omitempty"`
	Expr         string            `json:"expr,omitempty"`
	LegendFormat string            `json:"legendFormat,omitempty"`
}

// GrafanaTimeRange represents a Grafana time range.
type GrafanaTimeRange struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// GrafanaTemplating represents Grafana dashboard templating.
type GrafanaTemplating struct {
	List []GrafanaVariable `json:"list"`
}

// GrafanaVariable represents a Grafana template variable.
type GrafanaVariable struct {
	Name    string                 `json:"name"`
	Label   string                 `json:"label,omitempty"`
	Type    string                 `json:"type"`
	Query   string                 `json:"query,omitempty"`
	Current map[string]interface{} `json:"current,omitempty"`
}

// GrafanaAnnotations represents Grafana dashboard annotations.
type GrafanaAnnotations struct {
	List []GrafanaAnnotation `json:"list"`
}

// GrafanaAnnotation represents a Grafana annotation.
type GrafanaAnnotation struct {
	Name       string            `json:"name"`
	Datasource map[string]string `json:"datasource"`
	Enable     bool              `json:"enable"`
	Hide       bool              `json:"hide"`
	IconColor  string            `json:"iconColor"`
	Type       string            `json:"type"`
}
