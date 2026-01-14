package compute

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestNewGCEMapper(t *testing.T) {
	m := NewGCEMapper()
	if m == nil {
		t.Fatal("NewGCEMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeGCEInstance {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeGCEInstance)
	}
}

func TestGCEMapper_ResourceType(t *testing.T) {
	m := NewGCEMapper()
	got := m.ResourceType()
	want := resource.TypeGCEInstance

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestGCEMapper_Dependencies(t *testing.T) {
	m := NewGCEMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestGCEMapper_Validate(t *testing.T) {
	m := NewGCEMapper()

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
				Type: resource.TypeCloudRun,
				Name: "test",
			},
			wantErr: true,
		},
		{
			name: "valid resource",
			res: &resource.AWSResource{
				ID:   "test-id",
				Type: resource.TypeGCEInstance,
				Name: "test-instance",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeGCEInstance,
				Name: "test-instance",
			},
			wantErr: true,
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

func TestGCEMapper_Map(t *testing.T) {
	m := NewGCEMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic GCE instance",
			res: &resource.AWSResource{
				ID:   "projects/my-project/zones/us-central1-a/instances/my-instance",
				Type: resource.TypeGCEInstance,
				Name: "my-instance",
				Config: map[string]interface{}{
					"name":         "my-instance",
					"machine_type": "n2-standard-2",
					"zone":         "us-central1-a",
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
			name: "GCE with Ubuntu image",
			res: &resource.AWSResource{
				ID:   "projects/my-project/zones/us-central1-a/instances/ubuntu-instance",
				Type: resource.TypeGCEInstance,
				Name: "ubuntu-instance",
				Config: map[string]interface{}{
					"name":         "ubuntu-instance",
					"machine_type": "e2-medium",
					"boot_disk": map[string]interface{}{
						"initialize_params": map[string]interface{}{
							"image": "ubuntu-2204-jammy-v20231002",
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
			},
		},
		{
			name:    "nil resource",
			res:     nil,
			wantErr: true,
		},
		{
			name: "wrong resource type",
			res: &resource.AWSResource{
				ID:   "wrong-id",
				Type: resource.TypeCloudRun,
				Name: "wrong",
			},
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
