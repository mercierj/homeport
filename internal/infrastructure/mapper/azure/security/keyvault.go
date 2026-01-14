// Package security provides mappers for Azure security services.
package security

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/policy"
	"github.com/homeport/homeport/internal/domain/resource"
)

// KeyVaultMapper converts Azure Key Vault to HashiCorp Vault.
type KeyVaultMapper struct {
	*mapper.BaseMapper
}

// NewKeyVaultMapper creates a new Azure Key Vault to Vault mapper.
func NewKeyVaultMapper() *KeyVaultMapper {
	return &KeyVaultMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeKeyVault, nil),
	}
}

// Map converts an Azure Key Vault to a HashiCorp Vault service.
func (m *KeyVaultMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	vaultName := res.GetConfigString("name")
	if vaultName == "" {
		vaultName = res.Name
	}

	resourceGroup := res.GetConfigString("resource_group_name")

	result := mapper.NewMappingResult("vault")
	svc := result.DockerService

	svc.Image = "hashicorp/vault:1.15"
	svc.Environment = map[string]string{
		"VAULT_DEV_ROOT_TOKEN_ID":  "root",
		"VAULT_DEV_LISTEN_ADDRESS": "0.0.0.0:8200",
		"VAULT_ADDR":               "http://0.0.0.0:8200",
	}
	svc.Ports = []string{"8200:8200"}
	svc.Command = []string{"server", "-dev"}
	svc.CapAdd = []string{"IPC_LOCK"}
	svc.Volumes = []string{
		"./data/vault:/vault/data",
		"./config/vault:/vault/config",
	}
	svc.Networks = []string{"homeport"}
	svc.Labels = map[string]string{
		"homeport.source":         "azurerm_key_vault",
		"homeport.vault_name":     vaultName,
		"homeport.resource_group": resourceGroup,
		"traefik.enable":           "false",
	}
	svc.Restart = "unless-stopped"

	vaultConfig := m.generateVaultConfig()
	result.AddConfig("config/vault/config.hcl", []byte(vaultConfig))

	initScript := m.generateInitScript()
	result.AddScript("init_vault.sh", []byte(initScript))

	migrationScript := m.generateMigrationScript(vaultName, resourceGroup)
	result.AddScript("migrate_keyvault.sh", []byte(migrationScript))

	if skuName := res.GetConfigString("sku_name"); skuName == "premium" {
		result.AddWarning("Premium Key Vault with HSM-backed keys. Vault Enterprise required for HSM support.")
		result.AddManualStep("Review Vault Enterprise for HSM key storage requirements")
	}

	if softDelete := res.Config["soft_delete_retention_days"]; softDelete != nil {
		result.AddWarning("Soft delete is enabled. Vault has similar deletion protection with policies.")
	}

	if networkAcls := res.Config["network_acls"]; networkAcls != nil {
		result.AddWarning("Network ACLs configured. Set up Vault network policies accordingly.")
		result.AddManualStep("Configure Vault listener for network access control")
	}

	result.AddManualStep("Access Vault UI at http://localhost:8200")
	result.AddManualStep("Default root token: root (change in production)")
	result.AddManualStep("Migrate secrets using migrate_keyvault.sh")
	result.AddManualStep("Map Key Vault access policies to Vault policies")

	result.AddWarning("Key Vault access policies must be mapped to Vault policies manually")
	result.AddWarning("Certificate management differs between Key Vault and Vault PKI")

	return result, nil
}

func (m *KeyVaultMapper) generateVaultConfig() string {
	return `ui = true

storage "file" {
  path = "/vault/data"
}

listener "tcp" {
  address     = "0.0.0.0:8200"
  tls_disable = 1
}

disable_mlock = true
api_addr = "http://0.0.0.0:8200"
log_level = "info"
`
}

func (m *KeyVaultMapper) generateInitScript() string {
	return `#!/bin/bash
set -e

VAULT_ADDR="http://localhost:8200"
VAULT_TOKEN="root"

echo "Waiting for Vault..."
until curl -sf "$VAULT_ADDR/v1/sys/health" > /dev/null 2>&1; do
  sleep 2
done

export VAULT_ADDR VAULT_TOKEN

# Enable secrets engines for Key Vault equivalents
vault secrets enable -version=2 -path=azure-secrets kv || echo "secrets engine exists"
vault secrets enable -path=azure-keys transit || echo "transit engine exists"
vault secrets enable -path=azure-pki pki || echo "pki engine exists"

echo "Vault initialized for Azure Key Vault migration!"
`
}

func (m *KeyVaultMapper) generateMigrationScript(vaultName, resourceGroup string) string {
	vaultPath := strings.ToLower(strings.ReplaceAll(vaultName, "-", "_"))
	return fmt.Sprintf(`#!/bin/bash
set -e

KEYVAULT_NAME="%s"
RESOURCE_GROUP="%s"
VAULT_ADDR="${VAULT_ADDR:-http://localhost:8200}"
VAULT_TOKEN="${VAULT_TOKEN:-root}"

export VAULT_ADDR VAULT_TOKEN

echo "Azure Key Vault to HashiCorp Vault Migration"
echo "============================================="

# Migrate Secrets
echo "Migrating secrets..."
az keyvault secret list --vault-name "$KEYVAULT_NAME" --query "[].name" -o tsv | while read SECRET_NAME; do
  echo "  Migrating secret: $SECRET_NAME"
  SECRET_VALUE=$(az keyvault secret show --vault-name "$KEYVAULT_NAME" --name "$SECRET_NAME" --query "value" -o tsv)
  vault kv put "azure-secrets/%s/$SECRET_NAME" value="$SECRET_VALUE"
done

# List Keys (manual migration required)
echo ""
echo "Keys to migrate manually:"
az keyvault key list --vault-name "$KEYVAULT_NAME" --query "[].name" -o tsv

# List Certificates (manual migration required)
echo ""
echo "Certificates to migrate manually:"
az keyvault certificate list --vault-name "$KEYVAULT_NAME" --query "[].name" -o tsv

echo ""
echo "Migration complete!"
echo "Note: Keys and certificates require manual export and import"
`, vaultName, resourceGroup, vaultPath)
}

// ExtractPolicies extracts access policies from the Key Vault.
func (m *KeyVaultMapper) ExtractPolicies(ctx context.Context, res *resource.AWSResource) ([]*policy.Policy, error) {
	var policies []*policy.Policy

	vaultName := res.GetConfigString("name")
	if vaultName == "" {
		vaultName = res.Name
	}

	// Extract access policies
	if accessPolicies := res.Config["access_policy"]; accessPolicies != nil {
		policyList := m.toSlice(accessPolicies)
		for i, ap := range policyList {
			if apMap, ok := ap.(map[string]interface{}); ok {
				apJSON, _ := json.Marshal(apMap)

				objectID := ""
				if oid, ok := apMap["object_id"].(string); ok {
					objectID = oid
				}

				p := policy.NewPolicy(
					fmt.Sprintf("%s-access-policy-%d", res.ID, i),
					fmt.Sprintf("%s Access Policy for %s", vaultName, objectID),
					policy.PolicyTypeIAM,
					policy.ProviderAzure,
				)
				p.ResourceID = res.ID
				p.ResourceType = "azurerm_key_vault_access_policy"
				p.ResourceName = vaultName
				p.OriginalDocument = apJSON
				p.OriginalFormat = "json"
				p.NormalizedPolicy = m.normalizeAccessPolicy(apMap)

				// Check for sensitive permissions
				if m.hasSecretPermissions(apMap, "get", "list", "set", "delete") {
					p.AddWarning("Full secret permissions granted")
				}
				if m.hasKeyPermissions(apMap, "decrypt", "sign", "unwrapKey") {
					p.AddWarning("Cryptographic key permissions granted")
				}

				policies = append(policies, p)
			}
		}
	}

	// Extract network ACLs
	if networkAcls := res.Config["network_acls"]; networkAcls != nil {
		aclJSON, _ := json.Marshal(networkAcls)
		p := policy.NewPolicy(
			res.ID+"-network-acls",
			vaultName+" Network ACLs",
			policy.PolicyTypeNetwork,
			policy.ProviderAzure,
		)
		p.ResourceID = res.ID
		p.ResourceType = "azurerm_key_vault"
		p.ResourceName = vaultName
		p.OriginalDocument = aclJSON
		p.OriginalFormat = "json"
		p.NormalizedPolicy = m.normalizeNetworkACLs(networkAcls)

		policies = append(policies, p)
	}

	return policies, nil
}

// normalizeAccessPolicy converts Azure access policy to normalized format.
func (m *KeyVaultMapper) normalizeAccessPolicy(ap map[string]interface{}) *policy.NormalizedPolicy {
	normalized := &policy.NormalizedPolicy{
		Statements: make([]policy.Statement, 0),
	}

	objectID := ""
	if oid, ok := ap["object_id"].(string); ok {
		objectID = oid
	}

	var actions []string

	// Extract key permissions
	if keyPerms := ap["key_permissions"]; keyPerms != nil {
		for _, perm := range m.toStringSlice(keyPerms) {
			actions = append(actions, "key:"+perm)
		}
	}

	// Extract secret permissions
	if secretPerms := ap["secret_permissions"]; secretPerms != nil {
		for _, perm := range m.toStringSlice(secretPerms) {
			actions = append(actions, "secret:"+perm)
		}
	}

	// Extract certificate permissions
	if certPerms := ap["certificate_permissions"]; certPerms != nil {
		for _, perm := range m.toStringSlice(certPerms) {
			actions = append(actions, "certificate:"+perm)
		}
	}

	if len(actions) > 0 {
		stmt := policy.Statement{
			Effect: policy.EffectAllow,
			Principals: []policy.Principal{
				{Type: "AzureAD", ID: objectID},
			},
			Actions:   actions,
			Resources: []string{"*"},
		}
		normalized.Statements = append(normalized.Statements, stmt)
	}

	return normalized
}

// normalizeNetworkACLs converts Azure network ACLs to normalized format.
func (m *KeyVaultMapper) normalizeNetworkACLs(acls interface{}) *policy.NormalizedPolicy {
	normalized := &policy.NormalizedPolicy{
		NetworkRules: make([]policy.NetworkRule, 0),
	}

	if aclMap, ok := acls.(map[string]interface{}); ok {
		defaultAction := "Deny"
		if da, ok := aclMap["default_action"].(string); ok {
			defaultAction = da
		}

		// IP rules
		if ipRules := aclMap["ip_rules"]; ipRules != nil {
			for _, ip := range m.toStringSlice(ipRules) {
				rule := policy.NetworkRule{
					Direction:  "ingress",
					Protocol:   "tcp",
					FromPort:   443,
					ToPort:     443,
					CIDRBlocks: []string{ip},
					Action:     "allow",
				}
				normalized.NetworkRules = append(normalized.NetworkRules, rule)
			}
		}

		// Default deny rule
		if defaultAction == "Deny" {
			rule := policy.NetworkRule{
				Direction:   "ingress",
				Protocol:    "-1",
				CIDRBlocks:  []string{"0.0.0.0/0"},
				Action:      "deny",
				Description: "Default deny rule",
				Priority:    1000,
			}
			normalized.NetworkRules = append(normalized.NetworkRules, rule)
		}
	}

	return normalized
}

// hasSecretPermissions checks if the access policy has specified secret permissions.
func (m *KeyVaultMapper) hasSecretPermissions(ap map[string]interface{}, perms ...string) bool {
	if secretPerms := ap["secret_permissions"]; secretPerms != nil {
		permSet := make(map[string]bool)
		for _, p := range m.toStringSlice(secretPerms) {
			permSet[strings.ToLower(p)] = true
		}
		for _, perm := range perms {
			if permSet[strings.ToLower(perm)] {
				return true
			}
		}
	}
	return false
}

// hasKeyPermissions checks if the access policy has specified key permissions.
func (m *KeyVaultMapper) hasKeyPermissions(ap map[string]interface{}, perms ...string) bool {
	if keyPerms := ap["key_permissions"]; keyPerms != nil {
		permSet := make(map[string]bool)
		for _, p := range m.toStringSlice(keyPerms) {
			permSet[strings.ToLower(p)] = true
		}
		for _, perm := range perms {
			if permSet[strings.ToLower(perm)] {
				return true
			}
		}
	}
	return false
}

// toSlice converts interface to slice.
func (m *KeyVaultMapper) toSlice(v interface{}) []interface{} {
	if v == nil {
		return nil
	}
	if slice, ok := v.([]interface{}); ok {
		return slice
	}
	return []interface{}{v}
}

// toStringSlice converts interface to string slice.
func (m *KeyVaultMapper) toStringSlice(v interface{}) []string {
	var result []string
	for _, item := range m.toSlice(v) {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}
