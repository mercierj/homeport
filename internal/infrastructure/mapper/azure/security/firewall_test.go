package security

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestNewFirewallMapper(t *testing.T) {
	m := NewFirewallMapper()
	if m == nil {
		t.Fatal("NewFirewallMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeAzureFirewall {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeAzureFirewall)
	}
}

func TestFirewallMapper_ResourceType(t *testing.T) {
	m := NewFirewallMapper()
	got := m.ResourceType()
	want := resource.TypeAzureFirewall

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestFirewallMapper_Dependencies(t *testing.T) {
	m := NewFirewallMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestFirewallMapper_Validate(t *testing.T) {
	m := NewFirewallMapper()

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
				Type: resource.TypeAzureFirewall,
				Name: "test-firewall",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeAzureFirewall,
				Name: "test-firewall",
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

func TestFirewallMapper_Map(t *testing.T) {
	m := NewFirewallMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Azure Firewall",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Network/azureFirewalls/my-fw",
				Type: resource.TypeAzureFirewall,
				Name: "my-fw",
				Config: map[string]interface{}{
					"name":     "my-firewall",
					"sku_tier": "Standard",
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
			name: "Premium tier Firewall",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Network/azureFirewalls/premium-fw",
				Type: resource.TypeAzureFirewall,
				Name: "premium-fw",
				Config: map[string]interface{}{
					"name":     "premium-firewall",
					"sku_tier": "Premium",
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
			name: "Firewall with threat intelligence",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Network/azureFirewalls/intel-fw",
				Type: resource.TypeAzureFirewall,
				Name: "intel-fw",
				Config: map[string]interface{}{
					"name":             "intel-firewall",
					"threat_intel_mode": "Alert",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if !result.HasWarnings() {
					t.Error("Expected warnings for threat intelligence")
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
