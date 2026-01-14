package database

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestNewBigtableMapper(t *testing.T) {
	m := NewBigtableMapper()
	if m == nil {
		t.Fatal("NewBigtableMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeBigtable {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeBigtable)
	}
}

func TestBigtableMapper_ResourceType(t *testing.T) {
	m := NewBigtableMapper()
	got := m.ResourceType()
	want := resource.TypeBigtable

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestBigtableMapper_Dependencies(t *testing.T) {
	m := NewBigtableMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestBigtableMapper_Validate(t *testing.T) {
	m := NewBigtableMapper()

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
				Type: resource.TypeBigtable,
				Name: "test-bigtable",
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

func TestBigtableMapper_Map(t *testing.T) {
	m := NewBigtableMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Bigtable instance",
			res: &resource.AWSResource{
				ID:   "my-project/my-bigtable",
				Type: resource.TypeBigtable,
				Name: "my-bigtable",
				Config: map[string]interface{}{
					"name": "my-bigtable",
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
				if result.DockerService.Image != "cassandra:4.1" {
					t.Errorf("Expected Cassandra image, got %s", result.DockerService.Image)
				}
			},
		},
		{
			name: "Bigtable instance with display name",
			res: &resource.AWSResource{
				ID:   "my-project/prod-bigtable",
				Type: resource.TypeBigtable,
				Name: "prod-bigtable",
				Config: map[string]interface{}{
					"name":         "prod-bigtable",
					"display_name": "Production Bigtable",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Check environment variables are set
				if result.DockerService.Environment["CASSANDRA_CLUSTER_NAME"] != "homeport_cluster" {
					t.Error("Expected CASSANDRA_CLUSTER_NAME to be set")
				}
			},
		},
		{
			name: "Bigtable with warnings",
			res: &resource.AWSResource{
				ID:   "my-project/analytics-bigtable",
				Type: resource.TypeBigtable,
				Name: "analytics-bigtable",
				Config: map[string]interface{}{
					"name": "analytics-bigtable",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warnings about Bigtable to Cassandra migration
				if len(result.Warnings) == 0 {
					t.Error("Expected warnings about Bigtable to Cassandra migration")
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
