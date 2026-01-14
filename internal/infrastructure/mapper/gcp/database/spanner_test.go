package database

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestNewSpannerMapper(t *testing.T) {
	m := NewSpannerMapper()
	if m == nil {
		t.Fatal("NewSpannerMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeSpanner {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeSpanner)
	}
}

func TestSpannerMapper_ResourceType(t *testing.T) {
	m := NewSpannerMapper()
	got := m.ResourceType()
	want := resource.TypeSpanner

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestSpannerMapper_Dependencies(t *testing.T) {
	m := NewSpannerMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestSpannerMapper_Validate(t *testing.T) {
	m := NewSpannerMapper()

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
				Type: resource.TypeSpanner,
				Name: "test-spanner",
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

func TestSpannerMapper_Map(t *testing.T) {
	m := NewSpannerMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Spanner instance",
			res: &resource.AWSResource{
				ID:   "my-project/my-spanner",
				Type: resource.TypeSpanner,
				Name: "my-spanner",
				Config: map[string]interface{}{
					"name": "my-spanner",
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
				if result.DockerService.Image != "cockroachdb/cockroach:v23.2.0" {
					t.Errorf("Expected CockroachDB image, got %s", result.DockerService.Image)
				}
			},
		},
		{
			name: "Spanner instance with config",
			res: &resource.AWSResource{
				ID:   "my-project/production-spanner",
				Type: resource.TypeSpanner,
				Name: "production-spanner",
				Config: map[string]interface{}{
					"name":         "production-spanner",
					"display_name": "Production Spanner Instance",
					"num_nodes":    float64(3),
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warnings about Spanner specifics
				if len(result.Warnings) == 0 {
					t.Error("Expected warnings about Spanner to CockroachDB migration")
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
