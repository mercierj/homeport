package messaging

import (
	"context"
	"testing"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

func TestNewCloudTasksMapper(t *testing.T) {
	m := NewCloudTasksMapper()
	if m == nil {
		t.Fatal("NewCloudTasksMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeCloudTasks {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeCloudTasks)
	}
}

func TestCloudTasksMapper_ResourceType(t *testing.T) {
	m := NewCloudTasksMapper()
	got := m.ResourceType()
	want := resource.TypeCloudTasks

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestCloudTasksMapper_Dependencies(t *testing.T) {
	m := NewCloudTasksMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestCloudTasksMapper_Validate(t *testing.T) {
	m := NewCloudTasksMapper()

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
				Type: resource.TypeCloudTasks,
				Name: "test-queue",
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

func TestCloudTasksMapper_Map(t *testing.T) {
	m := NewCloudTasksMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Cloud Tasks queue",
			res: &resource.AWSResource{
				ID:   "my-project/us-central1/my-queue",
				Type: resource.TypeCloudTasks,
				Name: "my-queue",
				Config: map[string]interface{}{
					"name": "my-queue",
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
				if result.DockerService.Image != "redis:7-alpine" {
					t.Errorf("Expected Redis image, got %s", result.DockerService.Image)
				}
			},
		},
		{
			name: "Cloud Tasks with retry config",
			res: &resource.AWSResource{
				ID:   "my-project/us-central1/retry-queue",
				Type: resource.TypeCloudTasks,
				Name: "retry-queue",
				Config: map[string]interface{}{
					"name":         "retry-queue",
					"max_attempts": float64(5),
					"retry_config": map[string]interface{}{
						"max_retry_duration": "300s",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about retry configuration
				hasRetryWarning := false
				for _, w := range result.Warnings {
					if containsStr(w, "Retry configuration") {
						hasRetryWarning = true
						break
					}
				}
				if !hasRetryWarning {
					t.Error("Expected warning about retry configuration")
				}
			},
		},
		{
			name: "Cloud Tasks with dispatch deadline",
			res: &resource.AWSResource{
				ID:   "my-project/us-central1/deadline-queue",
				Type: resource.TypeCloudTasks,
				Name: "deadline-queue",
				Config: map[string]interface{}{
					"name":              "deadline-queue",
					"dispatch_deadline": "600s",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about dispatch deadline
				hasDeadlineWarning := false
				for _, w := range result.Warnings {
					if containsStr(w, "Dispatch deadline") {
						hasDeadlineWarning = true
						break
					}
				}
				if !hasDeadlineWarning {
					t.Error("Expected warning about dispatch deadline")
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

func TestCloudTasksMapper_hasRetryConfig(t *testing.T) {
	m := NewCloudTasksMapper()

	tests := []struct {
		name   string
		res    *resource.AWSResource
		expect bool
	}{
		{
			name: "with retry_config block",
			res: &resource.AWSResource{
				ID:   "test",
				Type: resource.TypeCloudTasks,
				Config: map[string]interface{}{
					"retry_config": map[string]interface{}{},
				},
			},
			expect: true,
		},
		{
			name: "with max_attempts",
			res: &resource.AWSResource{
				ID:   "test",
				Type: resource.TypeCloudTasks,
				Config: map[string]interface{}{
					"max_attempts": float64(3),
				},
			},
			expect: true,
		},
		{
			name: "without retry config",
			res: &resource.AWSResource{
				ID:     "test",
				Type:   resource.TypeCloudTasks,
				Config: map[string]interface{}{},
			},
			expect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.hasRetryConfig(tt.res)
			if got != tt.expect {
				t.Errorf("hasRetryConfig() = %v, want %v", got, tt.expect)
			}
		})
	}
}

// containsStr is a helper to check if a string contains a substring
func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
