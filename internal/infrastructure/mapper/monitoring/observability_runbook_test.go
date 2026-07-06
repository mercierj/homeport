package monitoring

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
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

func hasObservabilityRunbookKind(result *mapper.MappingResult, kind string) bool {
	for _, step := range result.RunbookSteps {
		if step.Metadata["kind"] == kind {
			return true
		}
	}
	return false
}
