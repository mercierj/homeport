package security

import (
	"context"
	"testing"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

func TestNewADB2CMapper(t *testing.T) {
	m := NewADB2CMapper()
	if m == nil {
		t.Fatal("NewADB2CMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeAzureADB2C {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeAzureADB2C)
	}
}

func TestADB2CMapper_ResourceType(t *testing.T) {
	m := NewADB2CMapper()
	got := m.ResourceType()
	want := resource.TypeAzureADB2C

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestADB2CMapper_Dependencies(t *testing.T) {
	m := NewADB2CMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestADB2CMapper_Validate(t *testing.T) {
	m := NewADB2CMapper()

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
				Type: resource.TypeAzureADB2C,
				Name: "test-b2c",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeAzureADB2C,
				Name: "test-b2c",
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

func TestADB2CMapper_Map(t *testing.T) {
	m := NewADB2CMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Azure AD B2C directory",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.AzureActiveDirectory/b2cDirectories/myb2c",
				Type: resource.TypeAzureADB2C,
				Name: "myb2c",
				Config: map[string]interface{}{
					"domain_name": "myb2c.onmicrosoft.com",
					"tenant_id":   "tenant-123",
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
			name: "Azure AD B2C with tenant",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.AzureActiveDirectory/b2cDirectories/myb2c",
				Type: resource.TypeAzureADB2C,
				Name: "myb2c",
				Config: map[string]interface{}{
					"domain_name": "myapp.b2clogin.com",
					"tenant_id":   "my-tenant-id-123",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if !result.HasWarnings() {
					t.Error("Expected warnings for B2C configuration")
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
