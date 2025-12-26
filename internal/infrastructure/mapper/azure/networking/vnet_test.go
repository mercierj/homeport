package networking

import (
	"context"
	"testing"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

func TestNewVNetMapper(t *testing.T) {
	m := NewVNetMapper()
	if m == nil {
		t.Fatal("NewVNetMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeAzureVNet {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeAzureVNet)
	}
}

func TestVNetMapper_ResourceType(t *testing.T) {
	m := NewVNetMapper()
	got := m.ResourceType()
	want := resource.TypeAzureVNet

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestVNetMapper_Dependencies(t *testing.T) {
	m := NewVNetMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestVNetMapper_Validate(t *testing.T) {
	m := NewVNetMapper()

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
				Type: resource.TypeAzureVNet,
				Name: "test-vnet",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeAzureVNet,
				Name: "test-vnet",
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

func TestVNetMapper_Map(t *testing.T) {
	m := NewVNetMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Virtual Network",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Network/virtualNetworks/my-vnet",
				Type: resource.TypeAzureVNet,
				Name: "my-vnet",
				Config: map[string]interface{}{
					"name":          "my-virtual-network",
					"address_space": []interface{}{"10.0.0.0/16"},
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
			name: "VNet with subnets",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Network/virtualNetworks/my-vnet",
				Type: resource.TypeAzureVNet,
				Name: "my-vnet",
				Config: map[string]interface{}{
					"name":          "my-vnet-subnets",
					"address_space": []interface{}{"10.0.0.0/16"},
					"subnet": []interface{}{
						map[string]interface{}{
							"name":           "frontend",
							"address_prefix": "10.0.1.0/24",
						},
						map[string]interface{}{
							"name":           "backend",
							"address_prefix": "10.0.2.0/24",
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if !result.HasWarnings() {
					t.Error("Expected warnings for VNet configuration")
				}
			},
		},
		{
			name: "VNet with custom DNS",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Network/virtualNetworks/my-vnet",
				Type: resource.TypeAzureVNet,
				Name: "my-vnet",
				Config: map[string]interface{}{
					"name":          "my-vnet-dns",
					"address_space": []interface{}{"10.0.0.0/16"},
					"dns_servers":   []interface{}{"8.8.8.8", "8.8.4.4"},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if !result.HasWarnings() {
					t.Error("Expected warnings for DNS configuration")
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
