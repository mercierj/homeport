package database

import (
	"context"
	"testing"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

func TestNewMySQLMapper(t *testing.T) {
	m := NewMySQLMapper()
	if m == nil {
		t.Fatal("NewMySQLMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeAzureMySQL {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeAzureMySQL)
	}
}

func TestMySQLMapper_ResourceType(t *testing.T) {
	m := NewMySQLMapper()
	got := m.ResourceType()
	want := resource.TypeAzureMySQL

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestMySQLMapper_Dependencies(t *testing.T) {
	m := NewMySQLMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestMySQLMapper_Validate(t *testing.T) {
	m := NewMySQLMapper()

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
				Type: resource.TypeAzureMySQL,
				Name: "test-mysql",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeAzureMySQL,
				Name: "test-mysql",
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

func TestMySQLMapper_Map(t *testing.T) {
	m := NewMySQLMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic MySQL server",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.DBforMySQL/flexibleServers/my-mysql",
				Type: resource.TypeAzureMySQL,
				Name: "my-mysql",
				Config: map[string]interface{}{
					"name":    "my-mysql-server",
					"version": "8.0",
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
			name: "MySQL with high availability",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.DBforMySQL/flexibleServers/my-mysql-ha",
				Type: resource.TypeAzureMySQL,
				Name: "my-mysql-ha",
				Config: map[string]interface{}{
					"name":                      "my-mysql-ha-server",
					"version":                   "8.0",
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
