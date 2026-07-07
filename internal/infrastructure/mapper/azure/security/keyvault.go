// Package security provides mappers for Azure security services.
package security

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/policy"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
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
		"traefik.enable":          "false",
	}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD-SHELL", "vault status -address=http://127.0.0.1:8200 >/dev/null || exit 1"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 5}

	vaultConfig := m.generateVaultConfig()
	result.AddConfig("config/vault/config.hcl", []byte(vaultConfig))
	result.AddConfig("config/vault/policies.hcl", []byte(m.generatePolicyConfig(vaultName)))
	result.AddConfig("config/keyvault/app-change.env", []byte(m.generateAppChange(vaultName)))
	result.AddConfig("config/keyvault/generated-client.patch", []byte(m.generateClientPatch(vaultName)))

	initScript := m.generateInitScript()
	result.AddScript("init_vault.sh", []byte(initScript))

	result.AddScript("export_keyvault_metadata.sh", []byte(m.generateExportScript(vaultName, resourceGroup)))
	migrationScript := m.generateMigrationScript(vaultName, resourceGroup)
	result.AddScript("migrate_keyvault.sh", []byte(migrationScript))
	result.AddScript("validate_keyvault_vault.sh", []byte(m.generateValidateScript(vaultName)))
	result.AddScript("backup_keyvault_config.sh", []byte(m.generateBackupScript(vaultName)))
	result.AddScript("cutover_keyvault_clients.sh", []byte(m.generateCutoverScript(vaultName)))

	if skuName := res.GetConfigString("sku_name"); skuName == "premium" {
		result.AddWarning("Premium Key Vault with HSM-backed keys. Vault Enterprise required for HSM support.")
	}

	if softDelete := res.Config["soft_delete_retention_days"]; softDelete != nil {
		result.AddWarning("Soft delete is enabled. Vault has similar deletion protection with policies.")
	}

	if networkAcls := res.Config["network_acls"]; networkAcls != nil {
		result.AddWarning("Network ACLs configured. Set up Vault network policies accordingly.")
	}

	result.AddWarning("Key Vault access policies are rendered to config/vault/policies.hcl")
	result.AddWarning("Certificate management differs between Key Vault and Vault PKI")
	for _, step := range keyVaultRunbook(vaultName) {
		result.AddRunbookStep(step)
	}

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

func (m *KeyVaultMapper) generatePolicyConfig(vaultName string) string {
	path := keyVaultPath(vaultName)
	return fmt.Sprintf(`path "azure-secrets/data/%s/*" {
  capabilities = ["create", "read", "update", "delete", "list"]
}

path "azure-keys/*" {
  capabilities = ["create", "read", "update", "delete", "list"]
}

path "azure-pki/*" {
  capabilities = ["create", "read", "update", "delete", "list"]
}
`, path)
}

func (m *KeyVaultMapper) generateAppChange(vaultName string) string {
	path := keyVaultPath(vaultName)
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_KEY_VAULT=%s\nVAULT_ADDR=http://vault:8200\nVAULT_SECRETS_PATH=azure-secrets/%s\nGENERATED_PATCH=config/keyvault/generated-client.patch\n", vaultName, path)
}

func (m *KeyVaultMapper) generateClientPatch(vaultName string) string {
	path := keyVaultPath(vaultName)
	return fmt.Sprintf("--- a/app/secrets.env\n+++ b/app/secrets.env\n@@\n-AZURE_KEY_VAULT=%s\n+VAULT_ADDR=http://vault:8200\n+VAULT_SECRETS_PATH=azure-secrets/%s\n+SECRETS_PROVIDER=vault\n", vaultName, path)
}

func (m *KeyVaultMapper) generateExportScript(vaultName, resourceGroup string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\nKEYVAULT_NAME=%q\nRESOURCE_GROUP=%q\nOUTPUT_DIR=\"${KEYVAULT_EXPORT_DIR:-keyvault-export}\"\nmkdir -p \"$OUTPUT_DIR\"\naz keyvault show --name \"$KEYVAULT_NAME\" --resource-group \"$RESOURCE_GROUP\" > \"$OUTPUT_DIR/vault.json\"\naz keyvault secret list --vault-name \"$KEYVAULT_NAME\" > \"$OUTPUT_DIR/secrets.json\"\naz keyvault key list --vault-name \"$KEYVAULT_NAME\" > \"$OUTPUT_DIR/keys.json\"\naz keyvault certificate list --vault-name \"$KEYVAULT_NAME\" > \"$OUTPUT_DIR/certificates.json\"\n", vaultName, resourceGroup)
}

func (m *KeyVaultMapper) generateMigrationScript(vaultName, resourceGroup string) string {
	vaultPath := keyVaultPath(vaultName)
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
echo "Keys staged for transit import planning:"
az keyvault key list --vault-name "$KEYVAULT_NAME" --query "[].name" -o tsv

# List Certificates (manual migration required)
echo ""
echo "Certificates staged for Vault PKI import planning:"
az keyvault certificate list --vault-name "$KEYVAULT_NAME" --query "[].name" -o tsv

echo ""
echo "Migration complete!"
echo "Generated config/keyvault/app-change.env points clients at Vault"
`, vaultName, resourceGroup, vaultPath)
}

func (m *KeyVaultMapper) generateValidateScript(vaultName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/vault/config.hcl\ntest -s config/vault/policies.hcl\ntest -s config/keyvault/app-change.env\ngrep -q %q config/keyvault/app-change.env\n", vaultName)
}

func (m *KeyVaultMapper) generateBackupScript(vaultName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/keyvault-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/vault config/keyvault keyvault-export 2>/dev/null || tar -czf \"$archive\" config/vault config/keyvault\necho \"$archive\"\n", keyVaultPath(vaultName))
}

func (m *KeyVaultMapper) generateCutoverScript(vaultName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/keyvault/app-change.env\ntest \"$SOURCE_KEY_VAULT\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\ntest -s \"$GENERATED_PATCH\"\necho \"Apply $GENERATED_PATCH and read secrets from $VAULT_ADDR/$VAULT_SECRETS_PATH\"\n", vaultName)
}

func keyVaultRunbook(vaultName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "secrets", "source": "azurerm_key_vault", "vault": vaultName, "target": "vault"}
	return []domainrunbook.Step{
		keyVaultStep("export-keyvault-metadata", "Export Key Vault metadata", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_keyvault_metadata.sh"}, "Key Vault secrets, keys, and certificates are enumerated", metadata),
		keyVaultStep("init-vault-keyvault", "Initialize Vault for Key Vault", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "init_vault.sh"}, "Vault engines for secrets, keys, and PKI are enabled", metadata),
		keyVaultStep("migrate-keyvault-secrets", "Migrate Key Vault secrets", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_keyvault.sh"}, "Key Vault secrets are written to Vault", metadata),
		keyVaultStep("validate-keyvault-vault", "Validate Key Vault target", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_keyvault_vault.sh"}, "Vault config and generated app change validate", metadata),
		keyVaultStep("backup-keyvault-config", "Backup Key Vault migration config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_keyvault_config.sh"}, "Key Vault migration artifacts are archived", metadata),
		keyVaultStep("cutover-keyvault-clients", "Cut over Key Vault clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_keyvault_clients.sh"}, "clients use generated Vault settings", metadata),
		keyVaultStep("rollback-keyvault-source", "Keep Key Vault source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "Azure Key Vault remains authoritative until Vault validation passes", metadata),
	}
}

func keyVaultStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}

func keyVaultPath(vaultName string) string {
	return strings.ToLower(strings.ReplaceAll(vaultName, "-", "_"))
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
