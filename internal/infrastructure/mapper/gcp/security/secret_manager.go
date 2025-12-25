// Package security provides mappers for GCP security services.
package security

import (
	"context"
	"fmt"
	"strings"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
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
	svc.Volumes = []string{
		"./data/vault:/vault/data",
		"./config/vault:/vault/config",
	}
	svc.Networks = []string{"cloudexit"}
	svc.Labels = map[string]string{
		"cloudexit.source":     "google_secret_manager_secret",
		"cloudexit.secret_id":  secretID,
		"cloudexit.project_id": projectID,
		"traefik.enable":       "false",
	}
	svc.Restart = "unless-stopped"

	vaultConfig := m.generateVaultConfig()
	result.AddConfig("config/vault/config.hcl", []byte(vaultConfig))

	initScript := m.generateInitScript()
	result.AddScript("init_vault.sh", []byte(initScript))

	migrationScript := m.generateMigrationScript(projectID, secretID)
	result.AddScript("migrate_secret.sh", []byte(migrationScript))

	if replication := res.Config["replication"]; replication != nil {
		result.AddWarning("Secret replication configured. Consider Vault Enterprise for replication.")
		result.AddManualStep("Review Vault replication options for high availability")
	}

	if labels := m.extractLabels(res); len(labels) > 0 {
		labelStr := ""
		for k, v := range labels {
			labelStr += fmt.Sprintf("%s=%s, ", k, v)
		}
		result.AddWarning("Secret has labels: " + strings.TrimSuffix(labelStr, ", "))
	}

	result.AddManualStep("Access Vault UI at http://localhost:8200")
	result.AddManualStep("Default root token: root (change in production)")
	result.AddManualStep("Migrate secrets using migrate_secret.sh")
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
