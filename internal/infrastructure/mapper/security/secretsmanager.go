// Package security provides mappers for AWS security services.
package security

import (
	"context"
	"fmt"
	"strings"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

// SecretsManagerMapper converts AWS Secrets Manager to HashiCorp Vault.
type SecretsManagerMapper struct {
	*mapper.BaseMapper
}

// NewSecretsManagerMapper creates a new Secrets Manager to Vault mapper.
func NewSecretsManagerMapper() *SecretsManagerMapper {
	return &SecretsManagerMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeSecretsManager, nil),
	}
}

// Map converts an AWS Secrets Manager secret to a Vault service.
func (m *SecretsManagerMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	secretName := res.GetConfigString("name")
	if secretName == "" {
		secretName = res.Name
	}

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
		"homeport.source":      "aws_secretsmanager_secret",
		"homeport.secret_name": secretName,
		"traefik.enable":        "false",
	}
	svc.Restart = "unless-stopped"

	vaultConfig := m.generateVaultConfig()
	result.AddConfig("config/vault/vault.hcl", []byte(vaultConfig))

	migrationScript := m.generateMigrationScript(secretName)
	result.AddScript("migrate_secrets.sh", []byte(migrationScript))

	initScript := m.generateInitScript()
	result.AddScript("init_vault.sh", []byte(initScript))

	if m.hasRotationEnabled(res) {
		result.AddWarning("Secret rotation enabled in AWS. Configure Vault rotation or cron job.")
		result.AddManualStep("Set up secret rotation using Vault's dynamic secrets or cron")
	}

	if kmsKeyID := res.GetConfigString("kms_key_id"); kmsKeyID != "" {
		result.AddWarning(fmt.Sprintf("Secret encrypted with KMS key %s. Vault uses its own encryption.", kmsKeyID))
	}

	result.AddManualStep("Access Vault UI at http://localhost:8200")
	result.AddManualStep("Default root token: root (change in production)")
	result.AddManualStep("Initialize Vault using init_vault.sh")
	result.AddManualStep("Migrate secrets using migrate_secrets.sh")

	result.AddWarning("Secret values must be exported from AWS manually")
	result.AddWarning("Use Vault server mode (not dev) in production with proper TLS")

	return result, nil
}

func (m *SecretsManagerMapper) generateVaultConfig() string {
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

func (m *SecretsManagerMapper) generateInitScript() string {
	return `#!/bin/bash
set -e

VAULT_ADDR="http://localhost:8200"
VAULT_TOKEN="root"

echo "Waiting for Vault..."
until curl -sf "$VAULT_ADDR/v1/sys/health" > /dev/null 2>&1; do
  sleep 2
done

export VAULT_ADDR VAULT_TOKEN

vault secrets enable -version=2 -path=secret kv || echo "KV already enabled"

cat > /tmp/readonly.hcl <<EOF
path "secret/data/*" {
  capabilities = ["read", "list"]
}
EOF
vault policy write readonly /tmp/readonly.hcl

echo "Vault initialized!"
`
}

func (m *SecretsManagerMapper) generateMigrationScript(secretName string) string {
	vaultPath := strings.ToLower(strings.ReplaceAll(secretName, "/", "-"))
	return fmt.Sprintf(`#!/bin/bash
set -e

SECRET_NAME="%s"
VAULT_PATH="secret/data/%s"
VAULT_ADDR="${VAULT_ADDR:-http://localhost:8200}"
VAULT_TOKEN="${VAULT_TOKEN:-root}"

export VAULT_ADDR VAULT_TOKEN

echo "Fetching secret from AWS..."
SECRET_VALUE=$(aws secretsmanager get-secret-value \
  --secret-id "$SECRET_NAME" \
  --query SecretString \
  --output text)

echo "Storing in Vault..."
vault kv put "$VAULT_PATH" value="$SECRET_VALUE"

echo "Secret migrated to $VAULT_PATH"
`, secretName, vaultPath)
}

func (m *SecretsManagerMapper) hasRotationEnabled(res *resource.AWSResource) bool {
	if rotation := res.Config["rotation_configuration"]; rotation != nil {
		if rotationMap, ok := rotation.(map[string]interface{}); ok {
			return len(rotationMap) > 0
		}
	}
	return false
}
