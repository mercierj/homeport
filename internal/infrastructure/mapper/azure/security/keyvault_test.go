package security

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestKeyVaultConformanceManagedAToZ(t *testing.T) {
	result, err := NewKeyVaultMapper().Map(context.Background(), managedKeyVaultFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Key Vault migration", result.ManualSteps)
	}
	if result.DockerService.Image != "hashicorp/vault:1.15" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Vault target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/vault/config.hcl", "config/vault/policies.hcl", "config/keyvault/app-change.env", "config/keyvault/generated-client.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/keyvault/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_KEY_VAULT=orders-kv", "VAULT_ADDR=http://vault:8200", "VAULT_SECRETS_PATH=azure-secrets/orders_kv"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_keyvault_metadata.sh", "init_vault.sh", "migrate_keyvault.sh", "validate_keyvault_vault.sh", "backup_keyvault_config.sh", "cutover_keyvault_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-keyvault-metadata": domainrunbook.StepTypeCommand,
		"init-vault-keyvault":      domainrunbook.StepTypeCommand,
		"migrate-keyvault-secrets": domainrunbook.StepTypeCommand,
		"validate-keyvault-vault":  domainrunbook.StepTypeCommand,
		"backup-keyvault-config":   domainrunbook.StepTypeCommand,
		"cutover-keyvault-clients": domainrunbook.StepTypeAPICall,
		"rollback-keyvault-source": domainrunbook.StepTypeRollback,
	} {
		if !hasKeyVaultRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewKeyVaultMapper(t *testing.T) {
	m := NewKeyVaultMapper()
	if m == nil {
		t.Fatal("NewKeyVaultMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeKeyVault {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeKeyVault)
	}
}

func managedKeyVaultFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "/subscriptions/demo/resourceGroups/rg/providers/Microsoft.KeyVault/vaults/orders-kv",
		Type: resource.TypeKeyVault,
		Name: "orders-kv",
		Config: map[string]interface{}{
			"name":                "orders-kv",
			"resource_group_name": "orders-rg",
			"sku_name":            "standard",
			"access_policy": []interface{}{
				map[string]interface{}{
					"object_id":          "app-principal",
					"secret_permissions": []interface{}{"Get", "List"},
				},
			},
		},
	}
}

func hasKeyVaultRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
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
