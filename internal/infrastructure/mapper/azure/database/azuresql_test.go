package database

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestNewAzureSQLMapper(t *testing.T) {
	m := NewAzureSQLMapper()
	if m == nil {
		t.Fatal("NewAzureSQLMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeAzureSQL {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeAzureSQL)
	}
}

func TestAzureSQLMapper_ResourceType(t *testing.T) {
	m := NewAzureSQLMapper()
	got := m.ResourceType()
	want := resource.TypeAzureSQL

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestAzureSQLMapper_Dependencies(t *testing.T) {
	m := NewAzureSQLMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestAzureSQLMapper_Validate(t *testing.T) {
	m := NewAzureSQLMapper()

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
				Type: resource.TypeAzureVM,
				Name: "test",
			},
			wantErr: true,
		},
		{
			name: "valid resource",
			res: &resource.AWSResource{
				ID:   "test-id",
				Type: resource.TypeAzureSQL,
				Name: "test-sql",
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

func TestAzureSQLMapper_Map(t *testing.T) {
	m := NewAzureSQLMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Azure SQL Database",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Sql/servers/myserver/databases/mydb",
				Type: resource.TypeAzureSQL,
				Name: "mydb",
				Config: map[string]interface{}{
					"name":                        "mydb",
					"server_name":                 "myserver",
					"sku_name":                    "S0",
					"max_size_gb":                 float64(250),
					"zone_redundant":              false,
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
