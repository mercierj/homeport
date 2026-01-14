// Package stacks provides stack-specific merger implementations for consolidation.
package stacks

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	"github.com/homeport/homeport/internal/domain/stack"
	"github.com/homeport/homeport/internal/infrastructure/consolidator"
	"gopkg.in/yaml.v3"
)

// ObservabilityMerger consolidates monitoring resources into Prometheus + Grafana + Loki.
// It handles CloudWatch (AWS), Cloud Monitoring (GCP), and Azure Monitor resources,
// converting them into a unified self-hosted observability stack.
type ObservabilityMerger struct {
	*consolidator.BaseMerger
}

// NewObservabilityMerger creates a new ObservabilityMerger.
func NewObservabilityMerger() *ObservabilityMerger {
	return &ObservabilityMerger{
		BaseMerger: consolidator.NewBaseMerger(stack.StackTypeObservability),
	}
}

// StackType returns the stack type this merger handles.
func (m *ObservabilityMerger) StackType() stack.StackType {
	return stack.StackTypeObservability
}

// CanMerge checks if this merger can handle the given results.
// Returns true if there are any monitoring-related resources.
func (m *ObservabilityMerger) CanMerge(results []*mapper.MappingResult) bool {
	if len(results) == 0 {
		return false
	}

	for _, result := range results {
		if result == nil {
			continue
		}
		if isObservabilityResource(result.SourceResourceType) {
			return true
		}
	}
	return false
}

// isObservabilityResource checks if a resource type is an observability resource.
func isObservabilityResource(resourceType string) bool {
	observabilityTypes := []string{
		// AWS CloudWatch
		string(resource.TypeCloudWatchMetricAlarm),
		string(resource.TypeCloudWatchLogGroup),
		string(resource.TypeCloudWatchDashboard),
		// GCP (placeholder for future types)
		"google_monitoring_alert_policy",
		"google_monitoring_dashboard",
		"google_logging_log_sink",
		// Azure
		"azurerm_monitor_metric_alert",
		"azurerm_monitor_action_group",
		"azurerm_log_analytics_workspace",
	}

	for _, t := range observabilityTypes {
		if resourceType == t {
			return true
		}
	}
	return false
}

// Merge creates a consolidated observability stack from monitoring resources.
// It generates:
// - Prometheus for metrics (replaces CloudWatch Metrics)
// - Grafana for dashboards
// - Loki for logs (replaces CloudWatch Logs)
// - Alertmanager for alerts (replaces CloudWatch Alarms)
func (m *ObservabilityMerger) Merge(ctx context.Context, results []*mapper.MappingResult, opts *consolidator.MergeOptions) (*stack.Stack, error) {
	if opts == nil {
		opts = consolidator.DefaultOptions()
	}

	// Create the observability stack
	name := "observability"
	if opts.NamePrefix != "" {
		name = opts.NamePrefix + "-" + name
	}

	stk := stack.NewStack(stack.StackTypeObservability, name)
	stk.Description = "Metrics, logs, and alerting stack (Prometheus + Grafana + Loki + Alertmanager)"

	// Extract services to monitor from results
	servicesToMonitor := extractServicesToMonitor(results)

	// Extract and convert CloudWatch alarms to Prometheus alert rules
	alertRules := m.extractAlertRules(results)

	// Generate Prometheus configuration
	promConfig := m.generatePrometheusConfig(servicesToMonitor, alertRules)
	promConfigYAML, err := yaml.Marshal(promConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal prometheus config: %w", err)
	}
	stk.AddConfig("prometheus/prometheus.yml", promConfigYAML)

	// Generate alert rules file
	if len(alertRules) > 0 {
		alertRulesConfig := AlertRulesConfig{
			Groups: []AlertRuleGroup{
				{
					Name:  "migrated_alerts",
					Rules: alertRules,
				},
			},
		}
		alertRulesYAML, err := yaml.Marshal(alertRulesConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal alert rules: %w", err)
		}
		stk.AddConfig("prometheus/alert_rules.yml", alertRulesYAML)
	}

	// Generate Loki configuration
	lokiConfig := m.generateLokiConfig()
	stk.AddConfig("loki/loki-config.yml", lokiConfig)

	// Generate Alertmanager configuration
	alertmanagerConfig := m.generateAlertmanagerConfig()
	stk.AddConfig("alertmanager/alertmanager.yml", alertmanagerConfig)

	// Generate Grafana datasources
	grafanaDatasources := m.generateGrafanaDatasources()
	stk.AddConfig("grafana/provisioning/datasources/datasources.yml", grafanaDatasources)

	// Generate Grafana dashboards provisioning
	dashboardsProvisioning := m.generateGrafanaDashboardsProvisioning()
	stk.AddConfig("grafana/provisioning/dashboards/dashboards.yml", dashboardsProvisioning)

	// Create Prometheus service (primary)
	promService := stack.NewService("prometheus", "prom/prometheus:latest")
	promService.Ports = []string{"9090:9090"}
	promService.Volumes = []string{
		"prometheus-data:/prometheus",
		"./prometheus/prometheus.yml:/etc/prometheus/prometheus.yml:ro",
		"./prometheus/alert_rules.yml:/etc/prometheus/alert_rules.yml:ro",
	}
	promService.Command = []string{
		"--config.file=/etc/prometheus/prometheus.yml",
		"--storage.tsdb.path=/prometheus",
		"--web.console.libraries=/usr/share/prometheus/console_libraries",
		"--web.console.templates=/usr/share/prometheus/consoles",
		"--web.enable-lifecycle",
	}
	promService.HealthCheck = &stack.HealthCheck{
		Test:        []string{"CMD", "wget", "-q", "--spider", "http://localhost:9090/-/healthy"},
		Interval:    "30s",
		Timeout:     "10s",
		Retries:     3,
		StartPeriod: "10s",
	}
	promService.Networks = []string{"observability"}
	stk.AddService(promService)

	// Create Grafana service
	grafanaService := stack.NewService("grafana", "grafana/grafana:latest")
	grafanaService.Ports = []string{"3000:3000"}
	grafanaService.Volumes = []string{
		"grafana-data:/var/lib/grafana",
		"./grafana/provisioning:/etc/grafana/provisioning:ro",
	}
	grafanaService.Environment = map[string]string{
		"GF_SECURITY_ADMIN_USER":     "${GRAFANA_ADMIN_USER:-admin}",
		"GF_SECURITY_ADMIN_PASSWORD": "${GRAFANA_ADMIN_PASSWORD:-admin}",
		"GF_USERS_ALLOW_SIGN_UP":     "false",
		"GF_SERVER_ROOT_URL":         "${GRAFANA_ROOT_URL:-http://localhost:3000}",
	}
	grafanaService.DependsOn = []string{"prometheus", "loki"}
	grafanaService.HealthCheck = &stack.HealthCheck{
		Test:        []string{"CMD", "curl", "-f", "http://localhost:3000/api/health"},
		Interval:    "30s",
		Timeout:     "10s",
		Retries:     3,
		StartPeriod: "30s",
	}
	grafanaService.Networks = []string{"observability"}
	stk.AddService(grafanaService)

	// Create Loki service
	lokiService := stack.NewService("loki", "grafana/loki:latest")
	lokiService.Ports = []string{"3100:3100"}
	lokiService.Volumes = []string{
		"loki-data:/loki",
		"./loki/loki-config.yml:/etc/loki/local-config.yaml:ro",
	}
	lokiService.Command = []string{"-config.file=/etc/loki/local-config.yaml"}
	lokiService.HealthCheck = &stack.HealthCheck{
		Test:        []string{"CMD", "wget", "-q", "--spider", "http://localhost:3100/ready"},
		Interval:    "30s",
		Timeout:     "10s",
		Retries:     3,
		StartPeriod: "30s",
	}
	lokiService.Networks = []string{"observability"}
	stk.AddService(lokiService)

	// Create Alertmanager service
	alertmanagerService := stack.NewService("alertmanager", "prom/alertmanager:latest")
	alertmanagerService.Ports = []string{"9093:9093"}
	alertmanagerService.Volumes = []string{
		"alertmanager-data:/alertmanager",
		"./alertmanager/alertmanager.yml:/etc/alertmanager/alertmanager.yml:ro",
	}
	alertmanagerService.Command = []string{
		"--config.file=/etc/alertmanager/alertmanager.yml",
		"--storage.path=/alertmanager",
	}
	alertmanagerService.HealthCheck = &stack.HealthCheck{
		Test:        []string{"CMD", "wget", "-q", "--spider", "http://localhost:9093/-/healthy"},
		Interval:    "30s",
		Timeout:     "10s",
		Retries:     3,
		StartPeriod: "10s",
	}
	alertmanagerService.Networks = []string{"observability"}
	stk.AddService(alertmanagerService)

	// Add volumes
	stk.AddVolume(stack.Volume{Name: "prometheus-data", Driver: "local"})
	stk.AddVolume(stack.Volume{Name: "grafana-data", Driver: "local"})
	stk.AddVolume(stack.Volume{Name: "loki-data", Driver: "local"})
	stk.AddVolume(stack.Volume{Name: "alertmanager-data", Driver: "local"})

	// Add network
	stk.AddNetwork(stack.Network{Name: "observability", Driver: "bridge"})

	// Add source resources
	for _, result := range results {
		if result == nil {
			continue
		}
		res := &resource.Resource{
			Type: resource.Type(result.SourceResourceType),
			Name: result.SourceResourceName,
		}
		stk.AddSourceResource(res)
	}

	// Add manual steps for migration
	stk.Metadata["manual_step_1"] = "Configure Prometheus scrape targets for your application services"
	stk.Metadata["manual_step_2"] = "Migrate CloudWatch dashboards to Grafana (manual JSON conversion required)"
	stk.Metadata["manual_step_3"] = "Update application logging to send logs to Loki (use Promtail or direct HTTP)"
	stk.Metadata["manual_step_4"] = "Configure Alertmanager notification channels (email, Slack, PagerDuty, etc.)"

	return stk, nil
}

// extractServicesToMonitor extracts service names that need monitoring from the results.
func extractServicesToMonitor(results []*mapper.MappingResult) []string {
	serviceSet := make(map[string]bool)

	for _, result := range results {
		if result == nil || result.DockerService == nil {
			continue
		}
		// Add the main service
		if result.DockerService.Name != "" {
			serviceSet[result.DockerService.Name] = true
		}
		// Add additional services
		for _, svc := range result.AdditionalServices {
			if svc != nil && svc.Name != "" {
				serviceSet[svc.Name] = true
			}
		}
	}

	services := make([]string, 0, len(serviceSet))
	for svc := range serviceSet {
		services = append(services, svc)
	}
	return services
}

// extractAlertRules extracts and converts CloudWatch alarms to Prometheus alert rules.
func (m *ObservabilityMerger) extractAlertRules(results []*mapper.MappingResult) []AlertRule {
	var rules []AlertRule

	for _, result := range results {
		if result == nil {
			continue
		}

		// Check if this is a CloudWatch alarm
		if result.SourceResourceType == string(resource.TypeCloudWatchMetricAlarm) {
			rule := m.convertCloudWatchAlarmToPrometheus(result)
			if rule != nil {
				rules = append(rules, *rule)
			}
		}
	}

	return rules
}

// convertCloudWatchAlarmToPrometheus converts a CloudWatch alarm to a Prometheus alert rule.
func (m *ObservabilityMerger) convertCloudWatchAlarmToPrometheus(result *mapper.MappingResult) *AlertRule {
	if result == nil {
		return nil
	}

	// Extract alarm name from the result
	alarmName := consolidator.NormalizeName(result.SourceResourceName)
	if alarmName == "" {
		alarmName = "migrated_alarm"
	}

	// Create a basic alert rule
	// Note: The actual metric expression would need to be converted based on the CloudWatch metric
	rule := &AlertRule{
		Alert: alarmName,
		Expr:  "up == 0", // Default expression - needs manual adjustment
		For:   "5m",
		Labels: map[string]string{
			"severity": "warning",
			"source":   "cloudwatch_migration",
		},
		Annotations: map[string]string{
			"summary":     fmt.Sprintf("Alert migrated from CloudWatch: %s", result.SourceResourceName),
			"description": "This alert was automatically migrated from CloudWatch. Please review and adjust the expression.",
		},
	}

	// Try to extract threshold and metric info from the result configs
	if result.Configs != nil {
		if configData, ok := result.Configs["alarm.json"]; ok {
			var alarmConfig map[string]interface{}
			if err := json.Unmarshal(configData, &alarmConfig); err == nil {
				// Extract threshold if present
				if threshold, ok := alarmConfig["threshold"].(float64); ok {
					rule.Annotations["original_threshold"] = fmt.Sprintf("%v", threshold)
				}
				// Extract metric name if present
				if metricName, ok := alarmConfig["metric_name"].(string); ok {
					rule.Annotations["original_metric"] = metricName
				}
			}
		}
	}

	return rule
}

// PrometheusConfig represents the Prometheus configuration file structure.
type PrometheusConfig struct {
	Global        GlobalConfig   `yaml:"global"`
	Alerting      AlertingConfig `yaml:"alerting"`
	RuleFiles     []string       `yaml:"rule_files"`
	ScrapeConfigs []ScrapeConfig `yaml:"scrape_configs"`
}

// GlobalConfig represents the global Prometheus configuration.
type GlobalConfig struct {
	ScrapeInterval     string `yaml:"scrape_interval"`
	EvaluationInterval string `yaml:"evaluation_interval"`
}

// AlertingConfig represents the alerting configuration.
type AlertingConfig struct {
	Alertmanagers []AlertmanagerRef `yaml:"alertmanagers"`
}

// AlertmanagerRef represents a reference to an Alertmanager instance.
type AlertmanagerRef struct {
	StaticConfigs []StaticConfig `yaml:"static_configs"`
}

// StaticConfig represents a static scrape configuration.
type StaticConfig struct {
	Targets []string `yaml:"targets"`
}

// ScrapeConfig represents a Prometheus scrape configuration.
type ScrapeConfig struct {
	JobName       string         `yaml:"job_name"`
	StaticConfigs []StaticConfig `yaml:"static_configs"`
	MetricsPath   string         `yaml:"metrics_path,omitempty"`
	Scheme        string         `yaml:"scheme,omitempty"`
}

// AlertRulesConfig represents the Prometheus alert rules file structure.
type AlertRulesConfig struct {
	Groups []AlertRuleGroup `yaml:"groups"`
}

// AlertRuleGroup represents a group of alert rules.
type AlertRuleGroup struct {
	Name  string      `yaml:"name"`
	Rules []AlertRule `yaml:"rules"`
}

// AlertRule represents a Prometheus alert rule.
type AlertRule struct {
	Alert       string            `yaml:"alert"`
	Expr        string            `yaml:"expr"`
	For         string            `yaml:"for"`
	Labels      map[string]string `yaml:"labels,omitempty"`
	Annotations map[string]string `yaml:"annotations,omitempty"`
}

// generatePrometheusConfig creates the Prometheus configuration.
func (m *ObservabilityMerger) generatePrometheusConfig(services []string, alertRules []AlertRule) *PrometheusConfig {
	config := &PrometheusConfig{
		Global: GlobalConfig{
			ScrapeInterval:     "15s",
			EvaluationInterval: "15s",
		},
		Alerting: AlertingConfig{
			Alertmanagers: []AlertmanagerRef{
				{
					StaticConfigs: []StaticConfig{
						{Targets: []string{"alertmanager:9093"}},
					},
				},
			},
		},
		RuleFiles: []string{
			"/etc/prometheus/alert_rules.yml",
		},
		ScrapeConfigs: []ScrapeConfig{
			{
				JobName: "prometheus",
				StaticConfigs: []StaticConfig{
					{Targets: []string{"localhost:9090"}},
				},
			},
		},
	}

	// Add scrape configs for each service
	for _, service := range services {
		config.ScrapeConfigs = append(config.ScrapeConfigs, ScrapeConfig{
			JobName: service,
			StaticConfigs: []StaticConfig{
				{Targets: []string{fmt.Sprintf("%s:9090", service)}},
			},
			MetricsPath: "/metrics",
		})
	}

	return config
}

// generateLokiConfig generates the Loki configuration.
func (m *ObservabilityMerger) generateLokiConfig() []byte {
	config := `auth_enabled: false

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

analytics:
  reporting_enabled: false
`
	return []byte(config)
}

// generateAlertmanagerConfig generates the Alertmanager configuration.
func (m *ObservabilityMerger) generateAlertmanagerConfig() []byte {
	config := `global:
  resolve_timeout: 5m

route:
  group_by: ['alertname', 'severity']
  group_wait: 10s
  group_interval: 10s
  repeat_interval: 1h
  receiver: 'default-receiver'
  routes:
    - match:
        severity: critical
      receiver: 'critical-receiver'
    - match:
        severity: warning
      receiver: 'warning-receiver'

receivers:
  - name: 'default-receiver'
    # Configure your notification channels here
    # Example for Slack:
    # slack_configs:
    #   - api_url: 'https://hooks.slack.com/services/YOUR/WEBHOOK/URL'
    #     channel: '#alerts'
    #     send_resolved: true

  - name: 'critical-receiver'
    # Configure critical alert notifications here
    # Example for PagerDuty:
    # pagerduty_configs:
    #   - service_key: 'YOUR_PAGERDUTY_SERVICE_KEY'

  - name: 'warning-receiver'
    # Configure warning alert notifications here

inhibit_rules:
  - source_match:
      severity: 'critical'
    target_match:
      severity: 'warning'
    equal: ['alertname']
`
	return []byte(config)
}

// generateGrafanaDatasources generates the Grafana datasources configuration.
func (m *ObservabilityMerger) generateGrafanaDatasources() []byte {
	config := `apiVersion: 1

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

  - name: Alertmanager
    type: alertmanager
    access: proxy
    url: http://alertmanager:9093
    editable: false
    jsonData:
      implementation: prometheus
`
	return []byte(config)
}

// generateGrafanaDashboardsProvisioning generates the Grafana dashboards provisioning configuration.
func (m *ObservabilityMerger) generateGrafanaDashboardsProvisioning() []byte {
	config := `apiVersion: 1

providers:
  - name: 'default'
    orgId: 1
    folder: ''
    folderUid: ''
    type: file
    disableDeletion: false
    updateIntervalSeconds: 10
    allowUiUpdates: true
    options:
      path: /var/lib/grafana/dashboards
`
	return []byte(config)
}

// Helper function to check if a string contains any of the given substrings.
func containsAny(s string, substrs []string) bool {
	for _, substr := range substrs {
		if strings.Contains(s, substr) {
			return true
		}
	}
	return false
}
