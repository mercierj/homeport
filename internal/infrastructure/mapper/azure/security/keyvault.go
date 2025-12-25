// Package security provides mappers for Azure security services.
package security

import (
	"context"
	"fmt"
	"strings"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
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
	svc.Networks = []string{"cloudexit"}
	svc.Labels = map[string]string{
		"cloudexit.source":         "azurerm_key_vault",
		"cloudexit.vault_name":     vaultName,
		"cloudexit.resource_group": resourceGroup,
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
