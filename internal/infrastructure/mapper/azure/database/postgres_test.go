package database

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestNewPostgresMapper(t *testing.T) {
	m := NewPostgresMapper()
	if m == nil {
		t.Fatal("NewPostgresMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeAzurePostgres {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeAzurePostgres)
	}
}

func TestPostgresMapper_ResourceType(t *testing.T) {
	m := NewPostgresMapper()
	got := m.ResourceType()
	want := resource.TypeAzurePostgres

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestPostgresMapper_Dependencies(t *testing.T) {
	m := NewPostgresMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestPostgresMapper_Validate(t *testing.T) {
	m := NewPostgresMapper()

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
				Type: resource.TypeAzurePostgres,
				Name: "test-postgres",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeAzurePostgres,
				Name: "test-postgres",
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

func TestPostgresMapper_Map(t *testing.T) {
	m := NewPostgresMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic PostgreSQL server",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.DBforPostgreSQL/flexibleServers/my-postgres",
				Type: resource.TypeAzurePostgres,
				Name: "my-postgres",
				Config: map[string]interface{}{
					"name":    "my-postgres-server",
					"version": "15",
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
			name: "PostgreSQL with high availability",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.DBforPostgreSQL/flexibleServers/my-postgres-ha",
				Type: resource.TypeAzurePostgres,
				Name: "my-postgres-ha",
				Config: map[string]interface{}{
					"name":                      "my-postgres-ha-server",
					"version":                   "16",
					"high_availability_enabled": true,
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if !result.HasWarnings() {
					t.Error("Expected warnings for HA configuration")
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
