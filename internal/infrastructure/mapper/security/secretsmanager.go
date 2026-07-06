// Package security provides mappers for AWS security services.
package security

import (
	"context"
	"fmt"
	"strings"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
	"github.com/homeport/homeport/internal/infrastructure/mapper/shared/securityrunbook"
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
		"homeport.target":      "vault",
		"traefik.enable":       "false",
	}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"

	vaultConfig := m.generateVaultConfig()
	result.AddConfig("config/vault/vault.hcl", []byte(vaultConfig))
	result.AddConfig("config/vault/app-change.env", []byte(m.generateAppChangeConfig(secretName)))

	migrationScript := m.generateMigrationScript(secretName)
	result.AddScript("migrate_secrets.sh", []byte(migrationScript))

	initScript := m.generateInitScript()
	result.AddScript("init_vault.sh", []byte(initScript))
	result.AddScript("validate_secretsmanager_adapter.sh", []byte(m.generateValidateScript(secretName)))
	result.AddScript("backup_secretsmanager_config.sh", []byte(m.generateBackupScript(secretName)))
	result.AddScript("cutover_secretsmanager_adapter.sh", []byte(m.generateCutoverScript(secretName)))
	for _, step := range securityrunbook.SecretsManager(secretName) {
		result.AddRunbookStep(step)
	}
	for _, step := range secretsManagerExtraRunbook(secretName) {
		result.AddRunbookStep(step)
	}

	if m.hasRotationEnabled(res) {
		result.AddWarning("Secret rotation enabled in AWS. Generated Vault rotation policy scaffold.")
		result.AddConfig("config/vault/rotation-policy.hcl", []byte(m.generateRotationPolicy(secretName)))
	}

	if kmsKeyID := res.GetConfigString("kms_key_id"); kmsKeyID != "" {
		result.AddWarning(fmt.Sprintf("Secret encrypted with KMS key %s. Vault uses its own encryption.", kmsKeyID))
	}

	result.AddWarning("Secret values are imported with aws secretsmanager get-secret-value when credentials allow; use encrypted runbook input for unreadable values.")
	result.AddWarning("Use Vault server mode (not dev) in production with proper TLS")

	return result, nil
}

func (m *SecretsManagerMapper) generateAppChangeConfig(secretName string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=adapter
SOURCE_SECRET=%s
TARGET_SECRET_PATH=secret/data/%s
AWS_ENDPOINT_URL_SECRETSMANAGER=http://homeport:8080/api/v1/compat/aws/secretsmanager
HOMEPORT_COMPAT_BACKEND=vault
HOMEPORT_COMPAT_PROTOCOL=secretsmanager
VAULT_ADDR=http://vault:8200
`, secretName, vaultPath(secretName))
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
`, secretName, vaultPath(secretName))
}

func (m *SecretsManagerMapper) generateValidateScript(secretName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
. config/vault/app-change.env
test "$SOURCE_SECRET" = %q
vault kv get "$TARGET_SECRET_PATH" >/tmp/homeport-vault-secret.json
test "$AWS_ENDPOINT_URL_SECRETSMANAGER" = "http://homeport:8080/api/v1/compat/aws/secretsmanager"
`, secretName)
}

func (m *SecretsManagerMapper) generateBackupScript(secretName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-vault-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/vault init_vault.sh migrate_secrets.sh validate_secretsmanager_adapter.sh cutover_secretsmanager_adapter.sh
echo "$archive"
`, vaultPath(secretName))
}

func (m *SecretsManagerMapper) generateCutoverScript(secretName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
. config/vault/app-change.env
test "$SOURCE_SECRET" = %q
test "$APP_CHANGE_MODE" = "adapter"
echo "Use AWS_ENDPOINT_URL_SECRETSMANAGER=$AWS_ENDPOINT_URL_SECRETSMANAGER for Secrets Manager SDK clients"
`, secretName)
}

func (m *SecretsManagerMapper) generateRotationPolicy(secretName string) string {
	return fmt.Sprintf(`path "secret/data/%s" {
  capabilities = ["create", "update", "read"]
}

path "secret/metadata/%s" {
  capabilities = ["read", "update"]
}
`, vaultPath(secretName), vaultPath(secretName))
}

func secretsManagerExtraRunbook(secretName string) []domainrunbook.Step {
	metadata := map[string]string{
		"kind":                            "secrets",
		"secret":                          secretName,
		"AWS_ENDPOINT_URL_SECRETSMANAGER": "http://homeport:8080/api/v1/compat/aws/secretsmanager",
		"HOMEPORT_TARGET":                 "vault",
		"HOMEPORT_APP_CHANGE":             "adapter",
	}
	return []domainrunbook.Step{
		secretsManagerStep("initialize-vault-secrets", "Initialize Vault secrets engine", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "init_vault.sh"}, "Vault KV v2 secrets engine is enabled", metadata),
		secretsManagerStep("backup-secretsmanager-config", "Backup Secrets Manager migration config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_secretsmanager_config.sh"}, "Vault and migration artifacts are archived", metadata),
		secretsManagerStep("cutover-secretsmanager-adapter", "Cut over Secrets Manager clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_secretsmanager_adapter.sh"}, "SDK clients use the HomePort Secrets Manager endpoint", metadata),
	}
}

func secretsManagerStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	return domainrunbook.Step{
		ID:               id,
		Name:             name,
		Group:            group,
		Type:             stepType,
		Status:           domainrunbook.StepStatusPending,
		Executor:         "shell",
		Command:          command,
		SuccessCondition: success,
		Metadata:         metadata,
	}
}

func vaultPath(secretName string) string {
	return strings.ToLower(strings.ReplaceAll(secretName, "/", "-"))
}

func (m *SecretsManagerMapper) hasRotationEnabled(res *resource.AWSResource) bool {
	if rotation := res.Config["rotation_configuration"]; rotation != nil {
		if rotationMap, ok := rotation.(map[string]interface{}); ok {
			return len(rotationMap) > 0
		}
	}
	return false
}
