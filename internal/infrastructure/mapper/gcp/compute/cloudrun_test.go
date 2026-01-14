package compute

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestNewCloudRunMapper(t *testing.T) {
	m := NewCloudRunMapper()
	if m == nil {
		t.Fatal("NewCloudRunMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeCloudRun {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeCloudRun)
	}
}

func TestCloudRunMapper_ResourceType(t *testing.T) {
	m := NewCloudRunMapper()
	got := m.ResourceType()
	want := resource.TypeCloudRun

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestCloudRunMapper_Dependencies(t *testing.T) {
	m := NewCloudRunMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestCloudRunMapper_Validate(t *testing.T) {
	m := NewCloudRunMapper()

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
				Type: resource.TypeCloudRun,
				Name: "test-service",
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

func TestCloudRunMapper_Map(t *testing.T) {
	m := NewCloudRunMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Cloud Run service",
			res: &resource.AWSResource{
				ID:   "my-project/us-central1/my-service",
				Type: resource.TypeCloudRun,
				Name: "my-service",
				Config: map[string]interface{}{
					"name": "my-service",
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
				if result.DockerService.Image == "" {
					t.Error("DockerService.Image is empty")
				}
			},
		},
		{
			name: "Cloud Run with container image",
			res: &resource.AWSResource{
				ID:   "my-project/us-central1/api-service",
				Type: resource.TypeCloudRun,
				Name: "api-service",
				Config: map[string]interface{}{
					"name": "api-service",
					"template": map[string]interface{}{
						"spec": map[string]interface{}{
							"containers": []interface{}{
								map[string]interface{}{
									"image": "gcr.io/my-project/my-app:latest",
									"ports": []interface{}{
										map[string]interface{}{
											"container_port": float64(8080),
										},
									},
								},
							},
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if result.DockerService.Image != "gcr.io/my-project/my-app:latest" {
					t.Errorf("Expected image gcr.io/my-project/my-app:latest, got %s", result.DockerService.Image)
				}
			},
		},
		{
			name: "Cloud Run with environment variables",
			res: &resource.AWSResource{
				ID:   "my-project/us-central1/env-service",
				Type: resource.TypeCloudRun,
				Name: "env-service",
				Config: map[string]interface{}{
					"name": "env-service",
					"template": map[string]interface{}{
						"spec": map[string]interface{}{
							"containers": []interface{}{
								map[string]interface{}{
									"image": "nginx:latest",
									"env": []interface{}{
										map[string]interface{}{
											"name":  "API_KEY",
											"value": "secret-key",
										},
									},
								},
							},
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if result.DockerService.Environment["API_KEY"] != "secret-key" {
					t.Error("Environment variable API_KEY not set correctly")
				}
			},
		},
		{
			name: "Cloud Run with autoscaling",
			res: &resource.AWSResource{
				ID:   "my-project/us-central1/scaled-service",
				Type: resource.TypeCloudRun,
				Name: "scaled-service",
				Config: map[string]interface{}{
					"name": "scaled-service",
					"autoscaling": map[string]interface{}{
						"min_instance_count": float64(1),
						"max_instance_count": float64(10),
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about autoscaling
				if len(result.Warnings) == 0 {
					t.Error("Expected warnings about autoscaling")
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

func TestCloudRunMapper_sanitizeName(t *testing.T) {
	m := NewCloudRunMapper()

	tests := []struct {
		input string
		want  string
	}{
		{"my-service", "my-service"},
		{"MY_SERVICE", "my-service"},
		{"my service", "my-service"},
		{"123service", "service"},
		{"", "service"},
		{"---", "service"},
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
