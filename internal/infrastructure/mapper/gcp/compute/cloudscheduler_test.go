package compute

import (
	"context"
	"testing"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

func TestNewCloudSchedulerMapper(t *testing.T) {
	m := NewCloudSchedulerMapper()
	if m == nil {
		t.Fatal("NewCloudSchedulerMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeCloudScheduler {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeCloudScheduler)
	}
}

func TestCloudSchedulerMapper_ResourceType(t *testing.T) {
	m := NewCloudSchedulerMapper()
	got := m.ResourceType()
	want := resource.TypeCloudScheduler

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestCloudSchedulerMapper_Dependencies(t *testing.T) {
	m := NewCloudSchedulerMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestCloudSchedulerMapper_Validate(t *testing.T) {
	m := NewCloudSchedulerMapper()

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
				Type: resource.TypeGCSBucket,
				Name: "test",
			},
			wantErr: true,
		},
		{
			name: "valid resource",
			res: &resource.AWSResource{
				ID:   "test-id",
				Type: resource.TypeCloudScheduler,
				Name: "test-job",
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

func TestCloudSchedulerMapper_Map(t *testing.T) {
	m := NewCloudSchedulerMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic scheduler job",
			res: &resource.AWSResource{
				ID:   "my-project/us-central1/my-job",
				Type: resource.TypeCloudScheduler,
				Name: "my-job",
				Config: map[string]interface{}{
					"name":     "my-job",
					"schedule": "0 * * * *",
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
				if result.DockerService.Image != "mcuadros/ofelia:latest" {
					t.Errorf("Expected Ofelia image, got %s", result.DockerService.Image)
				}
			},
		},
		{
			name: "scheduler with HTTP target",
			res: &resource.AWSResource{
				ID:   "my-project/us-central1/http-job",
				Type: resource.TypeCloudScheduler,
				Name: "http-job",
				Config: map[string]interface{}{
					"name":     "http-job",
					"schedule": "*/5 * * * *",
					"http_target": map[string]interface{}{
						"uri":         "https://example.com/api/trigger",
						"http_method": "POST",
						"headers": map[string]interface{}{
							"Content-Type": "application/json",
						},
						"body": `{"trigger": true}`,
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about HTTP target
				hasHTTPWarning := false
				for _, w := range result.Warnings {
					if w == "HTTP target: POST https://example.com/api/trigger" {
						hasHTTPWarning = true
						break
					}
				}
				if !hasHTTPWarning {
					t.Error("Expected warning about HTTP target")
				}
			},
		},
		{
			name: "scheduler with Pub/Sub target",
			res: &resource.AWSResource{
				ID:   "my-project/us-central1/pubsub-job",
				Type: resource.TypeCloudScheduler,
				Name: "pubsub-job",
				Config: map[string]interface{}{
					"name":     "pubsub-job",
					"schedule": "0 0 * * *",
					"pubsub_target": map[string]interface{}{
						"topic_name": "projects/my-project/topics/my-topic",
						"data":       "SGVsbG8gV29ybGQ=",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about Pub/Sub target
				hasPubSubWarning := false
				for _, w := range result.Warnings {
					if w == "Pub/Sub target: topic projects/my-project/topics/my-topic" {
						hasPubSubWarning = true
						break
					}
				}
				if !hasPubSubWarning {
					t.Error("Expected warning about Pub/Sub target")
				}
			},
		},
		{
			name: "scheduler with App Engine target",
			res: &resource.AWSResource{
				ID:   "my-project/us-central1/appengine-job",
				Type: resource.TypeCloudScheduler,
				Name: "appengine-job",
				Config: map[string]interface{}{
					"name":     "appengine-job",
					"schedule": "0 12 * * 1",
					"app_engine_http_target": map[string]interface{}{
						"relative_uri": "/cron/weekly-cleanup",
						"http_method":  "GET",
						"app_engine_routing": map[string]interface{}{
							"service": "worker",
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about App Engine target
				hasAppEngineWarning := false
				for _, w := range result.Warnings {
					if w == "App Engine target: GET /cron/weekly-cleanup (service: worker)" {
						hasAppEngineWarning = true
						break
					}
				}
				if !hasAppEngineWarning {
					t.Error("Expected warning about App Engine target")
				}
			},
		},
		{
			name: "scheduler with timezone",
			res: &resource.AWSResource{
				ID:   "my-project/us-central1/tz-job",
				Type: resource.TypeCloudScheduler,
				Name: "tz-job",
				Config: map[string]interface{}{
					"name":      "tz-job",
					"schedule":  "0 9 * * *",
					"time_zone": "America/New_York",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if result.DockerService.Environment["TZ"] != "America/New_York" {
					t.Errorf("Expected TZ=America/New_York, got %s", result.DockerService.Environment["TZ"])
				}
			},
		},
		{
			name: "scheduler with retry config",
			res: &resource.AWSResource{
				ID:   "my-project/us-central1/retry-job",
				Type: resource.TypeCloudScheduler,
				Name: "retry-job",
				Config: map[string]interface{}{
					"name":     "retry-job",
					"schedule": "*/10 * * * *",
					"retry_config": map[string]interface{}{
						"retry_count":          float64(3),
						"max_retry_duration":   "30s",
						"min_backoff_duration": "5s",
						"max_backoff_duration": "60s",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about retry config
				hasRetryWarning := false
				for _, w := range result.Warnings {
					if w == "Retry configured with 3 attempts. Configure retry logic in your job." {
						hasRetryWarning = true
						break
					}
				}
				if !hasRetryWarning {
					t.Error("Expected warning about retry config")
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

func TestCloudSchedulerMapper_sanitizeName(t *testing.T) {
	m := NewCloudSchedulerMapper()

	tests := []struct {
		input string
		want  string
	}{
		{"my-job", "my-job"},
		{"MY_JOB", "my-job"},
		{"my job", "my-job"},
		{"123job", "job"},
		{"", "scheduler"},
		{"---", "scheduler"},
		{"daily-backup", "daily-backup"},
		{"Weekly_Report", "weekly-report"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := m.sanitizeName(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
