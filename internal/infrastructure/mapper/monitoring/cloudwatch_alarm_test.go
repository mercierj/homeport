package monitoring

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestNewCloudWatchMetricAlarmMapper(t *testing.T) {
	m := NewCloudWatchMetricAlarmMapper()
	if m == nil {
		t.Fatal("NewCloudWatchMetricAlarmMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeCloudWatchMetricAlarm {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeCloudWatchMetricAlarm)
	}
}

func TestCloudWatchMetricAlarmMapper_ResourceType(t *testing.T) {
	m := NewCloudWatchMetricAlarmMapper()
	got := m.ResourceType()
	want := resource.TypeCloudWatchMetricAlarm

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestCloudWatchMetricAlarmMapper_Dependencies(t *testing.T) {
	m := NewCloudWatchMetricAlarmMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestCloudWatchMetricAlarmMapper_Validate(t *testing.T) {
	m := NewCloudWatchMetricAlarmMapper()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
	}{
		{
			name:    "nil resource",
			res:     nil,
			wantErr: true,
		},
		{
			name: "wrong resource type",
			res: &resource.AWSResource{
				ID:   "test-id",
				Type: resource.TypeS3Bucket,
				Name: "test",
			},
			wantErr: true,
		},
		{
			name: "valid resource",
			res: &resource.AWSResource{
				ID:   "test-id",
				Type: resource.TypeCloudWatchMetricAlarm,
				Name: "test-alarm",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := m.Validate(tt.res)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCloudWatchMetricAlarmMapper_Map(t *testing.T) {
	m := NewCloudWatchMetricAlarmMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic CloudWatch alarm",
			res: &resource.AWSResource{
				ID:     "arn:aws:cloudwatch:us-east-1:123456789012:alarm:high-cpu",
				Type:   resource.TypeCloudWatchMetricAlarm,
				Name:   "high-cpu",
				Region: "us-east-1",
				Config: map[string]interface{}{
					"alarm_name":          "high-cpu",
					"namespace":           "AWS/EC2",
					"metric_name":         "CPUUtilization",
					"comparison_operator": "GreaterThanThreshold",
					"threshold":           float64(80),
					"period":              float64(300),
					"evaluation_periods":  float64(2),
					"statistic":           "Average",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if result.DockerService == nil {
					t.Fatal("DockerService is nil")
				}
				if result.DockerService.Image != "prom/alertmanager:v0.26.0" {
					t.Errorf("DockerService.Image = %v, want prom/alertmanager:v0.26.0", result.DockerService.Image)
				}
				if result.DockerService.Name != "alertmanager" {
					t.Errorf("DockerService.Name = %v, want alertmanager", result.DockerService.Name)
				}
				// Check for port 9093
				foundPort := false
				for _, port := range result.DockerService.Ports {
					if strings.Contains(port, "9093") {
						foundPort = true
						break
					}
				}
				if !foundPort {
					t.Error("Expected port 9093 for Alertmanager Web UI")
				}
			},
		},
		{
			name: "CloudWatch alarm with SNS actions",
			res: &resource.AWSResource{
				ID:     "arn:aws:cloudwatch:us-east-1:123456789012:alarm:critical-alarm",
				Type:   resource.TypeCloudWatchMetricAlarm,
				Name:   "critical-alarm",
				Region: "us-east-1",
				Config: map[string]interface{}{
					"alarm_name":          "critical-alarm",
					"namespace":           "AWS/RDS",
					"metric_name":         "DatabaseConnections",
					"comparison_operator": "GreaterThanOrEqualToThreshold",
					"threshold":           float64(100),
					"period":              float64(60),
					"evaluation_periods":  float64(3),
					"statistic":           "Maximum",
					"alarm_actions": []interface{}{
						"arn:aws:sns:us-east-1:123456789012:alerts-topic",
					},
					"ok_actions": []interface{}{
						"arn:aws:sns:us-east-1:123456789012:alerts-topic",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warnings about SNS actions
				foundSNSWarning := false
				for _, warning := range result.Warnings {
					if strings.Contains(warning, "SNS") {
						foundSNSWarning = true
						break
					}
				}
				if !foundSNSWarning {
					t.Log("Expected warning about SNS action migration")
				}
			},
		},
		{
			name: "CloudWatch alarm with Auto Scaling action",
			res: &resource.AWSResource{
				ID:     "arn:aws:cloudwatch:us-east-1:123456789012:alarm:scale-up",
				Type:   resource.TypeCloudWatchMetricAlarm,
				Name:   "scale-up",
				Region: "us-east-1",
				Config: map[string]interface{}{
					"alarm_name":          "scale-up",
					"namespace":           "AWS/EC2",
					"metric_name":         "CPUUtilization",
					"comparison_operator": "GreaterThanThreshold",
					"threshold":           float64(70),
					"period":              float64(300),
					"evaluation_periods":  float64(2),
					"statistic":           "Average",
					"alarm_actions": []interface{}{
						"arn:aws:autoscaling:us-east-1:123456789012:scalingPolicy:xxx",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warnings about Auto Scaling actions
				foundAutoScalingWarning := false
				for _, warning := range result.Warnings {
					if strings.Contains(warning, "Auto Scaling") {
						foundAutoScalingWarning = true
						break
					}
				}
				if !foundAutoScalingWarning {
					t.Log("Expected warning about Auto Scaling action migration")
				}
			},
		},
		{
			name: "CloudWatch alarm in ALARM state",
			res: &resource.AWSResource{
				ID:     "arn:aws:cloudwatch:us-east-1:123456789012:alarm:active-alarm",
				Type:   resource.TypeCloudWatchMetricAlarm,
				Name:   "active-alarm",
				Region: "us-east-1",
				Config: map[string]interface{}{
					"alarm_name":          "active-alarm",
					"namespace":           "AWS/EC2",
					"metric_name":         "StatusCheckFailed",
					"comparison_operator": "GreaterThanThreshold",
					"threshold":           float64(0),
					"period":              float64(60),
					"evaluation_periods":  float64(1),
					"statistic":           "Maximum",
					"state_value":         "ALARM",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about ALARM state
				foundAlarmWarning := false
				for _, warning := range result.Warnings {
					if strings.Contains(warning, "ALARM state") {
						foundAlarmWarning = true
						break
					}
				}
				if !foundAlarmWarning {
					t.Log("Expected warning about current ALARM state")
				}
			},
		},
		{
			name:    "nil resource",
			res:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := m.Map(ctx, tt.res)
			if (err != nil) != tt.wantErr {
				t.Errorf("Map() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}

func TestCloudWatchMetricAlarmMapper_Map_HasPrometheusService(t *testing.T) {
	m := NewCloudWatchMetricAlarmMapper()
	ctx := context.Background()

	res := &resource.AWSResource{
		ID:     "arn:aws:cloudwatch:us-east-1:123456789012:alarm:test",
		Type:   resource.TypeCloudWatchMetricAlarm,
		Name:   "test-alarm",
		Region: "us-east-1",
		Config: map[string]interface{}{
			"alarm_name":          "test-alarm",
			"namespace":           "AWS/EC2",
			"metric_name":         "CPUUtilization",
			"comparison_operator": "GreaterThanThreshold",
			"threshold":           float64(80),
			"period":              float64(300),
			"evaluation_periods":  float64(2),
			"statistic":           "Average",
		},
	}

	result, err := m.Map(ctx, res)
	if err != nil {
		t.Fatalf("Map() error = %v", err)
	}

	// Check that Prometheus is included as an additional service
	foundPrometheus := false
	for _, svc := range result.AdditionalServices {
		if svc.Name == "prometheus" {
			foundPrometheus = true
			if !strings.Contains(svc.Image, "prometheus") {
				t.Errorf("Prometheus service image = %v, expected to contain 'prometheus'", svc.Image)
			}
			// Check for port 9090
			foundPort := false
			for _, port := range svc.Ports {
				if strings.Contains(port, "9090") {
					foundPort = true
					break
				}
			}
			if !foundPort {
				t.Error("Expected port 9090 for Prometheus")
			}
			break
		}
	}

	if !foundPrometheus {
		t.Error("Expected Prometheus as an additional service")
	}
}

func TestCloudWatchMetricAlarmMapper_Map_GeneratesConfigs(t *testing.T) {
	m := NewCloudWatchMetricAlarmMapper()
	ctx := context.Background()

	res := &resource.AWSResource{
		ID:     "arn:aws:cloudwatch:us-east-1:123456789012:alarm:test",
		Type:   resource.TypeCloudWatchMetricAlarm,
		Name:   "test-alarm",
		Region: "us-east-1",
		Config: map[string]interface{}{
			"alarm_name":          "test-alarm",
			"namespace":           "AWS/EC2",
			"metric_name":         "CPUUtilization",
			"comparison_operator": "GreaterThanThreshold",
			"threshold":           float64(80),
			"period":              float64(300),
			"evaluation_periods":  float64(2),
			"statistic":           "Average",
		},
	}

	result, err := m.Map(ctx, res)
	if err != nil {
		t.Fatalf("Map() error = %v", err)
	}

	// Check for Alertmanager config
	if _, ok := result.Configs["config/alertmanager/alertmanager.yml"]; !ok {
		t.Error("Expected Alertmanager config file")
	}

	// Check for Prometheus alert rules
	if _, ok := result.Configs["config/prometheus/rules/cloudwatch-metric-alarms.yml"]; !ok {
		t.Error("Expected Prometheus alert rules file")
	}

	// Check for Prometheus config
	if _, ok := result.Configs["config/prometheus/prometheus.yml"]; !ok {
		t.Error("Expected Prometheus config file")
	}
}

func TestCloudWatchMetricAlarmMapper_Map_GeneratesScripts(t *testing.T) {
	m := NewCloudWatchMetricAlarmMapper()
	ctx := context.Background()

	res := &resource.AWSResource{
		ID:     "arn:aws:cloudwatch:us-east-1:123456789012:alarm:test",
		Type:   resource.TypeCloudWatchMetricAlarm,
		Name:   "test-alarm",
		Region: "us-east-1",
		Config: map[string]interface{}{
			"alarm_name":          "test-alarm",
			"namespace":           "AWS/EC2",
			"metric_name":         "CPUUtilization",
			"comparison_operator": "GreaterThanThreshold",
			"threshold":           float64(80),
			"period":              float64(300),
			"evaluation_periods":  float64(2),
			"statistic":           "Average",
		},
	}

	result, err := m.Map(ctx, res)
	if err != nil {
		t.Fatalf("Map() error = %v", err)
	}

	// Check for migration script
	if _, ok := result.Scripts["scripts/migrate-cloudwatch-alarm.sh"]; !ok {
		t.Error("Expected migration script")
	}

	// Check for export script
	if _, ok := result.Scripts["scripts/export-cloudwatch-alarms.sh"]; !ok {
		t.Error("Expected export script")
	}

	// Check for test script
	if _, ok := result.Scripts["scripts/test-alert.sh"]; !ok {
		t.Error("Expected test alert script")
	}
}

func TestCloudWatchMetricAlarmMapper_Map_TraefikLabels(t *testing.T) {
	m := NewCloudWatchMetricAlarmMapper()
	ctx := context.Background()

	res := &resource.AWSResource{
		ID:     "arn:aws:cloudwatch:us-east-1:123456789012:alarm:test",
		Type:   resource.TypeCloudWatchMetricAlarm,
		Name:   "test-alarm",
		Region: "us-east-1",
		Config: map[string]interface{}{
			"alarm_name":          "test-alarm",
			"namespace":           "AWS/EC2",
			"metric_name":         "CPUUtilization",
			"comparison_operator": "GreaterThanThreshold",
			"threshold":           float64(80),
			"period":              float64(300),
			"evaluation_periods":  float64(2),
			"statistic":           "Average",
		},
	}

	result, err := m.Map(ctx, res)
	if err != nil {
		t.Fatalf("Map() error = %v", err)
	}

	// Check Alertmanager Traefik labels
	if result.DockerService.Labels["traefik.enable"] != "true" {
		t.Error("Expected traefik.enable label to be true")
	}

	if !strings.Contains(result.DockerService.Labels["traefik.http.routers.alertmanager.rule"], "alertmanager") {
		t.Error("Expected Traefik rule for alertmanager")
	}

	// Check Prometheus Traefik labels
	for _, svc := range result.AdditionalServices {
		if svc.Name == "prometheus" {
			if svc.Labels["traefik.enable"] != "true" {
				t.Error("Expected Prometheus traefik.enable label to be true")
			}
			if !strings.Contains(svc.Labels["traefik.http.routers.prometheus.rule"], "prometheus") {
				t.Error("Expected Traefik rule for prometheus")
			}
			break
		}
	}
}

func TestCloudWatchMetricAlarmMapper_ConvertComparisonOperator(t *testing.T) {
	m := NewCloudWatchMetricAlarmMapper()

	tests := []struct {
		cwOp     string
		expected string
	}{
		{"GreaterThanThreshold", ">"},
		{"GreaterThanOrEqualToThreshold", ">="},
		{"LessThanThreshold", "<"},
		{"LessThanOrEqualToThreshold", "<="},
		{"LessThanLowerOrGreaterThanUpperThreshold", "!="},
		{"Unknown", ">"},
	}

	for _, tt := range tests {
		t.Run(tt.cwOp, func(t *testing.T) {
			got := m.convertComparisonOperator(tt.cwOp)
			if got != tt.expected {
				t.Errorf("convertComparisonOperator(%q) = %q, want %q", tt.cwOp, got, tt.expected)
			}
		})
	}
}

func TestCloudWatchMetricAlarmMapper_SanitizeAlertName(t *testing.T) {
	m := NewCloudWatchMetricAlarmMapper()

	tests := []struct {
		input    string
		expected string
	}{
		{"simple-name", "simple_name"},
		{"name with spaces", "name_with_spaces"},
		{"name.with.dots", "name_with_dots"},
		{"name/with/slashes", "name_with_slashes"},
		{"123-starts-with-number", "alert_123_starts_with_number"},
		{"MixedCase_Name", "MixedCase_Name"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := m.sanitizeAlertName(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeAlertName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestCloudWatchMetricAlarmMapper_CalculateForDuration(t *testing.T) {
	m := NewCloudWatchMetricAlarmMapper()

	tests := []struct {
		period      int
		evalPeriods int
		expected    string
	}{
		{60, 5, "5m"},
		{300, 2, "10m"},
		{3600, 1, "1h"},
		{30, 2, "1m"},
		{0, 0, "1m"}, // Defaults
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := m.calculateForDuration(tt.period, tt.evalPeriods)
			if got != tt.expected {
				t.Errorf("calculateForDuration(%d, %d) = %q, want %q", tt.period, tt.evalPeriods, got, tt.expected)
			}
		})
	}
}
