package database

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestNewCacheMapper(t *testing.T) {
	m := NewCacheMapper()
	if m == nil {
		t.Fatal("NewCacheMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeAzureCache {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeAzureCache)
	}
}

func TestCacheMapper_ResourceType(t *testing.T) {
	m := NewCacheMapper()
	got := m.ResourceType()
	want := resource.TypeAzureCache

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestCacheMapper_Dependencies(t *testing.T) {
	m := NewCacheMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestCacheMapper_Validate(t *testing.T) {
	m := NewCacheMapper()

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
				Type: resource.TypeEC2Instance,
				Name: "test",
			},
			wantErr: true,
		},
		{
			name: "valid resource",
			res: &resource.AWSResource{
				ID:   "test-id",
				Type: resource.TypeAzureCache,
				Name: "test-cache",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeAzureCache,
				Name: "test-cache",
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

func TestCacheMapper_Map(t *testing.T) {
	m := NewCacheMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Redis cache",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Cache/Redis/my-cache",
				Type: resource.TypeAzureCache,
				Name: "my-cache",
				Config: map[string]interface{}{
					"name":          "my-redis-cache",
					"capacity":      float64(2),
					"sku_name":      "Standard",
					"family":        "C",
					"redis_version": "6",
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
				if result.DockerService.HealthCheck == nil {
					t.Error("HealthCheck is nil")
				}
			},
		},
		{
			name: "Premium tier cache",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Cache/Redis/premium-cache",
				Type: resource.TypeAzureCache,
				Name: "premium-cache",
				Config: map[string]interface{}{
					"name":     "premium-redis-cache",
					"capacity": float64(1),
					"sku_name": "Premium",
					"family":   "P",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if !result.HasWarnings() {
					t.Error("Expected warnings for Premium tier")
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
				Type: resource.TypeEC2Instance,
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
