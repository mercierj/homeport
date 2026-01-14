package database

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestNewMemorystoreMapper(t *testing.T) {
	m := NewMemorystoreMapper()
	if m == nil {
		t.Fatal("NewMemorystoreMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeMemorystore {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeMemorystore)
	}
}

func TestMemorystoreMapper_ResourceType(t *testing.T) {
	m := NewMemorystoreMapper()
	got := m.ResourceType()
	want := resource.TypeMemorystore

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestMemorystoreMapper_Dependencies(t *testing.T) {
	m := NewMemorystoreMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestMemorystoreMapper_Validate(t *testing.T) {
	m := NewMemorystoreMapper()

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
				Type: resource.TypeMemorystore,
				Name: "test-redis",
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

func TestMemorystoreMapper_Map(t *testing.T) {
	m := NewMemorystoreMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Memorystore instance",
			res: &resource.AWSResource{
				ID:   "my-project/us-central1/my-redis",
				Type: resource.TypeMemorystore,
				Name: "my-redis",
				Config: map[string]interface{}{
					"name":           "my-redis",
					"memory_size_gb": float64(2),
					"redis_version":  "7.0",
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
				if result.DockerService.Image != "redis:7.0-alpine" {
					t.Errorf("Expected redis:7.0-alpine image, got %s", result.DockerService.Image)
				}
			},
		},
		{
			name: "Memorystore with STANDARD_HA tier",
			res: &resource.AWSResource{
				ID:   "my-project/us-central1/ha-redis",
				Type: resource.TypeMemorystore,
				Name: "ha-redis",
				Config: map[string]interface{}{
					"name":           "ha-redis",
					"memory_size_gb": float64(5),
					"tier":           "STANDARD_HA",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about HA tier
				hasHAWarning := false
				for _, w := range result.Warnings {
					if contains(w, "Standard HA tier") {
						hasHAWarning = true
						break
					}
				}
				if !hasHAWarning {
					t.Error("Expected warning about Standard HA tier")
				}
			},
		},
		{
			name: "Memorystore with default settings",
			res: &resource.AWSResource{
				ID:   "my-project/us-central1/default-redis",
				Type: resource.TypeMemorystore,
				Name: "default-redis",
				Config: map[string]interface{}{
					"name": "default-redis",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should use default values
				if result.DockerService.Image == "" {
					t.Error("Expected Docker image to be set")
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

// contains is a helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
