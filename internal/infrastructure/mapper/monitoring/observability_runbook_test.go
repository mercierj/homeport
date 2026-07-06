package monitoring

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestObservabilityRunbookFixtureCoversLogsMetricsAndAlerts(t *testing.T) {
	fixture := []struct {
		name   string
		mapper interface {
			Map(context.Context, *resource.AWSResource) (*mapper.MappingResult, error)
		}
		resource *resource.AWSResource
		kind     string
	}{
		{
			name:   "cloudwatch logs",
			mapper: NewCloudWatchLogsMapper(),
			resource: &resource.AWSResource{
				ID:     "logs-1",
				Type:   resource.TypeCloudWatchLogGroup,
				Name:   "/aws/lambda/app",
				Config: map[string]interface{}{"name": "/aws/lambda/app"},
			},
			kind: "observability",
		},
		{
			name:   "cloudwatch dashboard",
			mapper: NewCloudWatchDashboardMapper(),
			resource: &resource.AWSResource{
				ID:     "dash-1",
				Type:   resource.TypeCloudWatchDashboard,
				Name:   "ops",
				Config: map[string]interface{}{"dashboard_name": "ops"},
			},
			kind: "observability",
		},
		{
			name:   "cloudwatch alarm",
			mapper: NewCloudWatchMetricAlarmMapper(),
			resource: &resource.AWSResource{
				ID:   "alarm-1",
				Type: resource.TypeCloudWatchMetricAlarm,
				Name: "cpu-high",
				Config: map[string]interface{}{
					"alarm_name":          "cpu-high",
					"metric_name":         "CPUUtilization",
					"namespace":           "AWS/EC2",
					"comparison_operator": "GreaterThanThreshold",
					"threshold":           float64(80),
				},
			},
			kind: "alerts",
		},
	}

	for _, tt := range fixture {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.mapper.Map(context.Background(), tt.resource)
			if err != nil {
				t.Fatalf("Map() error = %v", err)
			}
			if !hasObservabilityRunbookKind(result, tt.kind) {
				t.Fatalf("missing %s runbook steps: %#v", tt.kind, result.RunbookSteps)
			}
		})
	}
}

func TestCloudWatchConformanceManagedAToZ(t *testing.T) {
	fixture := []struct {
		name   string
		mapper interface {
			Map(context.Context, *resource.AWSResource) (*mapper.MappingResult, error)
		}
		resource  *resource.AWSResource
		configs   []string
		scripts   []string
		runbookID string
	}{
		{
			name:   "logs",
			mapper: NewCloudWatchLogsMapper(),
			resource: &resource.AWSResource{
				ID:     "logs-1",
				Type:   resource.TypeCloudWatchLogGroup,
				Name:   "/aws/lambda/app",
				Config: map[string]interface{}{"name": "/aws/lambda/app", "retention_in_days": 30},
			},
			configs:   []string{"config/loki/loki-config.yaml", "config/promtail/promtail-config.yaml"},
			scripts:   []string{"scripts/cloudwatch-logs-export.sh", "scripts/loki-import.sh", "scripts/backup-cloudwatch-logs.sh"},
			runbookID: "validate-observability-ingestion",
		},
		{
			name:   "dashboard",
			mapper: NewCloudWatchDashboardMapper(),
			resource: &resource.AWSResource{
				ID:   "dash-1",
				Type: resource.TypeCloudWatchDashboard,
				Name: "ops",
				Config: map[string]interface{}{
					"dashboard_name": "ops",
					"dashboard_body": `{"widgets":[{"type":"metric","x":0,"y":0,"width":6,"height":6,"properties":{"metrics":[["AWS/EC2","CPUUtilization"]],"title":"CPU"}}]}`,
				},
			},
			configs:   []string{"config/grafana/provisioning/datasources/datasources.yaml", "config/grafana/dashboards/home.json"},
			scripts:   []string{"scripts/cloudwatch-dashboard-export.sh", "scripts/grafana-import.sh", "scripts/backup-cloudwatch-dashboard.sh"},
			runbookID: "render-prometheus-scrapes",
		},
		{
			name:   "alarm",
			mapper: NewCloudWatchMetricAlarmMapper(),
			resource: &resource.AWSResource{
				ID:   "alarm-1",
				Type: resource.TypeCloudWatchMetricAlarm,
				Name: "cpu-high",
				Config: map[string]interface{}{
					"alarm_name":          "cpu-high",
					"metric_name":         "CPUUtilization",
					"namespace":           "AWS/EC2",
					"comparison_operator": "GreaterThanThreshold",
					"threshold":           float64(80),
					"period":              float64(300),
					"evaluation_periods":  float64(2),
					"statistic":           "Average",
				},
			},
			configs:   []string{"config/alertmanager/alertmanager.yml", "config/prometheus/rules/cloudwatch-metric-alarms.yml", "config/prometheus/prometheus.yml"},
			scripts:   []string{"scripts/migrate-cloudwatch-alarm.sh", "scripts/test-alert.sh", "scripts/backup-cloudwatch-alarms.sh"},
			runbookID: "validate-alert-route",
		},
	}

	for _, tt := range fixture {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.mapper.Map(context.Background(), tt.resource)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.ManualSteps) != 0 {
				t.Fatalf("manual steps = %#v, want generated CloudWatch observability migration", result.ManualSteps)
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
			if !hasCloudWatchRunbookStep(result, tt.runbookID) {
				t.Fatalf("missing runbook step %s: %#v", tt.runbookID, result.RunbookSteps)
			}
		})
	}
}

func hasCloudWatchRunbookStep(result *mapper.MappingResult, id string) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type != domainrunbook.StepTypeRollback {
			return true
		}
	}
	return false
}

func hasObservabilityRunbookKind(result *mapper.MappingResult, kind string) bool {
	for _, step := range result.RunbookSteps {
		if step.Metadata["kind"] == kind {
			return true
		}
	}
	return false
}
