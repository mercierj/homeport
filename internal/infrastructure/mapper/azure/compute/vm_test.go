package compute

import (
	"context"
	"testing"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

func TestNewVMMapper(t *testing.T) {
	m := NewVMMapper()
	if m == nil {
		t.Fatal("NewVMMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeAzureVM {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeAzureVM)
	}
}

func TestVMMapper_ResourceType(t *testing.T) {
	m := NewVMMapper()
	got := m.ResourceType()
	want := resource.TypeAzureVM

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestVMMapper_Dependencies(t *testing.T) {
	m := NewVMMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestVMMapper_Validate(t *testing.T) {
	m := NewVMMapper()

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
				Type: resource.TypeAzureVM,
				Name: "test-vm",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeAzureVM,
				Name: "test-vm",
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

func TestVMMapper_Map(t *testing.T) {
	m := NewVMMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Linux VM",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/my-vm",
				Type: resource.TypeAzureVM,
				Name: "my-vm",
				Config: map[string]interface{}{
					"name": "my-linux-vm",
					"size": "Standard_D2s_v3",
					"source_image_reference": map[string]interface{}{
						"publisher": "Canonical",
						"offer":     "UbuntuServer",
						"sku":       "22_04-lts",
					},
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
