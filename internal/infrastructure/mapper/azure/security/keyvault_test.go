package security

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestNewKeyVaultMapper(t *testing.T) {
	m := NewKeyVaultMapper()
	if m == nil {
		t.Fatal("NewKeyVaultMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeKeyVault {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeKeyVault)
	}
}

func TestKeyVaultMapper_ResourceType(t *testing.T) {
	m := NewKeyVaultMapper()
	got := m.ResourceType()
	want := resource.TypeKeyVault

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestKeyVaultMapper_Dependencies(t *testing.T) {
	m := NewKeyVaultMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestKeyVaultMapper_Validate(t *testing.T) {
	m := NewKeyVaultMapper()

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
				Type: resource.TypeKeyVault,
				Name: "test-keyvault",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeKeyVault,
				Name: "test-keyvault",
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

func TestKeyVaultMapper_Map(t *testing.T) {
	m := NewKeyVaultMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Key Vault",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.KeyVault/vaults/my-vault",
				Type: resource.TypeKeyVault,
				Name: "my-vault",
				Config: map[string]interface{}{
					"name":                "my-keyvault",
					"resource_group_name": "my-rg",
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
			name: "Premium SKU Key Vault",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.KeyVault/vaults/premium-vault",
				Type: resource.TypeKeyVault,
				Name: "premium-vault",
				Config: map[string]interface{}{
					"name":                "premium-keyvault",
					"resource_group_name": "my-rg",
					"sku_name":            "premium",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if !result.HasWarnings() {
					t.Error("Expected warnings for Premium SKU")
				}
			},
		},
		{
			name: "Key Vault with soft delete",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.KeyVault/vaults/softdelete-vault",
				Type: resource.TypeKeyVault,
				Name: "softdelete-vault",
				Config: map[string]interface{}{
					"name":                       "softdelete-keyvault",
					"resource_group_name":        "my-rg",
					"soft_delete_retention_days": float64(90),
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if !result.HasWarnings() {
					t.Error("Expected warnings for soft delete configuration")
				}
			},
		},
		{
			name: "Key Vault with network ACLs",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.KeyVault/vaults/network-vault",
				Type: resource.TypeKeyVault,
				Name: "network-vault",
				Config: map[string]interface{}{
					"name":                "network-keyvault",
					"resource_group_name": "my-rg",
					"network_acls": map[string]interface{}{
						"default_action": "Deny",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if !result.HasWarnings() {
					t.Error("Expected warnings for network ACLs")
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
