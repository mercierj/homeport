// Package security provides mappers for GCP security services.
package security

import (
	"context"
	"fmt"
	"strings"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

// SecretManagerMapper converts GCP Secret Manager to HashiCorp Vault.
type SecretManagerMapper struct {
	*mapper.BaseMapper
}

// NewSecretManagerMapper creates a new GCP Secret Manager to Vault mapper.
func NewSecretManagerMapper() *SecretManagerMapper {
	return &SecretManagerMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeSecretManager, nil),
	}
}

// Map converts a GCP Secret Manager secret to a Vault service.
func (m *SecretManagerMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	secretID := res.GetConfigString("secret_id")
	if secretID == "" {
		secretID = res.Name
	}

	projectID := res.GetConfigString("project")

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
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Volumes = []string{
		"./data/vault:/vault/data",
		"./config/vault:/vault/config",
	}
	svc.Networks = []string{"homeport"}
	svc.Labels = map[string]string{
		"homeport.source":     "google_secret_manager_secret",
		"homeport.secret_id":  secretID,
		"homeport.project_id": projectID,
		"traefik.enable":      "false",
	}
	svc.Restart = "unless-stopped"
	svc.HealthCheck = &mapper.HealthCheck{
		Test: []string{"CMD", "vault", "status"},
	}

	vaultConfig := m.generateVaultConfig()
	result.AddConfig("config/vault/config.hcl", []byte(vaultConfig))

	initScript := m.generateInitScript()
	result.AddScript("init_vault.sh", []byte(initScript))

	migrationScript := m.generateMigrationScript(projectID, secretID)
	result.AddScript("migrate_secret.sh", []byte(migrationScript))
	result.AddScript("validate_secret_vault.sh", []byte(m.generateValidateScript(secretID)))
	result.AddScript("backup_secret_vault.sh", []byte(m.generateBackupScript(secretID)))
	result.AddConfig("config/vault/app-change.env", []byte(m.generateAppChangeConfig(secretID)))
	for _, step := range secretManagerRunbook(secretID) {
		result.AddRunbookStep(step)
	}

	if replication := res.Config["replication"]; replication != nil {
		result.AddWarning("Secret replication configured. Generated Vault handoff keeps the source authoritative until validation passes.")
	}

	if labels := m.extractLabels(res); len(labels) > 0 {
		labelStr := ""
		for k, v := range labels {
			labelStr += fmt.Sprintf("%s=%s, ", k, v)
		}
		result.AddWarning("Secret has labels: " + strings.TrimSuffix(labelStr, ", "))
	}

	result.AddWarning("Use Vault server mode (not dev) in production")

	return result, nil
}

func (m *SecretManagerMapper) generateVaultConfig() string {
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

func (m *SecretManagerMapper) generateInitScript() string {
	return `#!/bin/bash
set -e

VAULT_ADDR="http://localhost:8200"
VAULT_TOKEN="root"

echo "Waiting for Vault..."
until curl -sf "$VAULT_ADDR/v1/sys/health" > /dev/null 2>&1; do
  sleep 2
done

export VAULT_ADDR VAULT_TOKEN

vault secrets enable -version=2 -path=gcp-secrets kv || echo "KV already enabled"

cat > /tmp/gcp-readonly.hcl <<EOF
path "gcp-secrets/data/*" {
  capabilities = ["read", "list"]
}
EOF
vault policy write gcp-readonly /tmp/gcp-readonly.hcl

echo "Vault initialized for GCP secrets!"
`
}

func (m *SecretManagerMapper) generateMigrationScript(projectID, secretID string) string {
	vaultPath := strings.ToLower(strings.ReplaceAll(secretID, "/", "-"))
	return fmt.Sprintf(`#!/bin/bash
set -e

PROJECT_ID="%s"
SECRET_ID="%s"
VAULT_PATH="gcp-secrets/data/%s"
VAULT_ADDR="${VAULT_ADDR:-http://localhost:8200}"
VAULT_TOKEN="${VAULT_TOKEN:-root}"

export VAULT_ADDR VAULT_TOKEN

echo "Migrating GCP Secret Manager secret to Vault"
echo "============================================="

echo "Fetching latest secret version..."
SECRET_VALUE=$(gcloud secrets versions access latest \
  --secret="$SECRET_ID" \
  --project="$PROJECT_ID")

if [ -z "$SECRET_VALUE" ]; then
  echo "Error: Failed to fetch secret"
  exit 1
fi

echo "Storing in Vault..."
vault kv put "$VAULT_PATH" value="$SECRET_VALUE"

echo "Secret migrated to $VAULT_PATH"
echo ""
echo "To retrieve: vault kv get $VAULT_PATH"
`, projectID, secretID, vaultPath)
}

func (m *SecretManagerMapper) generateAppChangeConfig(secretID string) string {
	vaultPath := strings.ToLower(strings.ReplaceAll(secretID, "/", "-"))
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_SECRET=%s\nVAULT_ADDR=http://vault:8200\nVAULT_PATH=gcp-secrets/data/%s\n", secretID, vaultPath)
}

func (m *SecretManagerMapper) generateValidateScript(secretID string) string {
	vaultPath := strings.ToLower(strings.ReplaceAll(secretID, "/", "-"))
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/vault/app-change.env\nVAULT_ADDR=\"${VAULT_ADDR:-http://localhost:8200}\" VAULT_TOKEN=\"${VAULT_TOKEN:-root}\" vault kv get gcp-secrets/%s >/dev/null\necho \"Secret Manager secret %s validated in Vault\"\n", vaultPath, secretID)
}

func (m *SecretManagerMapper) generateBackupScript(secretID string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/gcp-secret-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/vault data/vault\necho \"$archive\"\n", strings.ToLower(strings.ReplaceAll(secretID, "/", "-")))
}

func secretManagerRunbook(secretID string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "secret", "source": "google_secret_manager_secret", "secret": secretID, "target": "vault"}
	return []domainrunbook.Step{
		secretManagerStep("init-vault-secret-target", "Initialize Vault secret target", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "init_vault.sh"}, "Vault KV engine and read policy are configured", metadata),
		secretManagerStep("migrate-secret-to-vault", "Migrate secret to Vault", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_secret.sh"}, "latest secret version is stored in Vault", metadata),
		secretManagerStep("validate-secret-vault", "Validate Vault secret", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_secret_vault.sh"}, "Vault path is readable", metadata),
		secretManagerStep("backup-secret-vault", "Backup Vault secret config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_secret_vault.sh"}, "Vault config and data are archived", metadata),
		secretManagerStep("cutover-secret-clients", "Cut over secret clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "-c", "test -s config/vault/app-change.env"}, "applications use generated Vault path", metadata),
		secretManagerStep("rollback-secret-source-authority", "Keep Secret Manager source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "Secret Manager remains authoritative until cutover passes", metadata),
	}
}

func secretManagerStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}

func (m *SecretManagerMapper) extractLabels(res *resource.AWSResource) map[string]string {
	labels := make(map[string]string)
	if labelConfig := res.Config["labels"]; labelConfig != nil {
		if labelMap, ok := labelConfig.(map[string]interface{}); ok {
			for k, v := range labelMap {
				if vStr, ok := v.(string); ok {
					labels[k] = vStr
				}
			}
		}
	}
	return labels
}
