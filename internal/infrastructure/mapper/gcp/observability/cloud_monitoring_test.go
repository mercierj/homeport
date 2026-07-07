package observability

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestCloudMonitoringConformanceManagedAToZ(t *testing.T) {
	tests := []struct {
		name     string
		mapper   *CloudMonitoringMapper
		resource *resource.AWSResource
		configs  []string
		scripts  []string
		runbook  string
	}{
		{
			name:   "alert policy",
			mapper: NewCloudMonitoringAlertPolicyMapper(),
			resource: &resource.AWSResource{
				ID:   "projects/demo/alertPolicies/cpu",
				Type: resource.TypeCloudMonitoringAlertPolicy,
				Name: "cpu-high",
				Config: map[string]interface{}{
					"display_name": "cpu-high",
					"filter":       `metric.type="compute.googleapis.com/instance/cpu/utilization"`,
				},
			},
			configs: []string{"config/alertmanager/alertmanager.yml", "config/prometheus/prometheus.yml", "config/prometheus/rules/gcp-monitoring-alerts.yml", "config/gcp-monitoring/app-change.env"},
			scripts: []string{"scripts/export-gcp-monitoring-alerts.sh", "scripts/test-gcp-monitoring-alert.sh", "scripts/backup-gcp-monitoring.sh"},
			runbook: "validate-alert-route",
		},
		{
			name:   "dashboard",
			mapper: NewCloudMonitoringDashboardMapper(),
			resource: &resource.AWSResource{
				ID:   "projects/demo/dashboards/orders",
				Type: resource.TypeCloudMonitoringDashboard,
				Name: "orders",
				Config: map[string]interface{}{
					"display_name": "orders",
				},
			},
			configs: []string{"config/grafana/provisioning/datasources/datasources.yaml", "config/grafana/provisioning/dashboards/dashboards.yaml", "config/grafana/dashboards/gcp-monitoring.json", "config/gcp-monitoring/app-change.env"},
			scripts: []string{"scripts/export-gcp-monitoring-dashboard.sh", "scripts/import-gcp-monitoring-dashboard.sh", "scripts/backup-gcp-monitoring.sh"},
			runbook: "validate-observability-ingestion",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.mapper.Map(context.Background(), tt.resource)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.ManualSteps) != 0 {
				t.Fatalf("manual steps = %#v, want generated Cloud Monitoring migration", result.ManualSteps)
			}
			if result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
				t.Fatalf("primary service is not HA: %#v", result.DockerService)
			}
			for _, file := range tt.configs {
				content, ok := result.Configs[file]
				if !ok {
					t.Fatalf("missing config %s", file)
				}
				if strings.Contains(string(content), "TODO") {
					t.Fatalf("config %s contains TODO:\n%s", file, content)
				}
			}
			for _, file := range tt.scripts {
				if _, ok := result.Scripts[file]; !ok {
					t.Fatalf("missing script %s", file)
				}
			}
			if !hasCloudMonitoringRunbookStep(result, tt.runbook, domainrunbook.StepTypeCommand) {
				t.Fatalf("missing runbook step %s: %#v", tt.runbook, result.RunbookSteps)
			}
			if !hasCloudMonitoringRunbookStep(result, "backup-gcp-monitoring-config", domainrunbook.StepTypeCommand) {
				t.Fatalf("missing backup runbook step: %#v", result.RunbookSteps)
			}
			if !hasCloudMonitoringRunbookStep(result, "cutover-gcp-monitoring-clients", domainrunbook.StepTypeAPICall) {
				t.Fatalf("missing cutover runbook step: %#v", result.RunbookSteps)
			}
		})
	}
}

func hasCloudMonitoringRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
