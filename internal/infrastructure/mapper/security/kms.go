// Package security provides mappers for AWS security services.
package security

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	"github.com/homeport/homeport/internal/infrastructure/mapper/shared/securityrunbook"
)

// KMSMapper converts AWS KMS keys to HashiCorp Vault Transit secrets engine.
type KMSMapper struct {
	*mapper.BaseMapper
}

// NewKMSMapper creates a new KMS to Vault Transit mapper.
func NewKMSMapper() *KMSMapper {
	return &KMSMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeKMSKey, nil),
	}
}

// Map converts an AWS KMS key to a Vault Transit service.
func (m *KMSMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	keyID := res.GetConfigString("key_id")
	if keyID == "" {
		keyID = res.ID
	}

	keyUsage := res.GetConfigString("key_usage")
	keySpec := res.GetConfigString("key_spec")

	result := mapper.NewMappingResult("vault")
	svc := result.DockerService

	// Configure Vault with Transit secrets engine
	svc.Image = "hashicorp/vault:1.15"
	svc.Environment = map[string]string{
		"VAULT_DEV_ROOT_TOKEN_ID":  "${VAULT_ROOT_TOKEN:-root}",
		"VAULT_DEV_LISTEN_ADDRESS": "0.0.0.0:8200",
		"VAULT_ADDR":               "http://0.0.0.0:8200",
		"VAULT_API_ADDR":           "http://0.0.0.0:8200",
	}
	svc.Ports = []string{
		"8200:8200",
	}
	svc.Command = []string{"server", "-dev"}
	svc.CapAdd = []string{"IPC_LOCK"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Volumes = []string{
		"./data/vault:/vault/data",
		"./config/vault:/vault/config",
		"./scripts/vault:/vault/scripts",
	}
	svc.Networks = []string{"homeport"}
	svc.Labels = map[string]string{
		"homeport.source":                 "aws_kms_key",
		"homeport.key_id":                 keyID,
		"homeport.key_usage":              keyUsage,
		"traefik.enable":                  "true",
		"traefik.http.routers.vault.rule": "Host(`vault.localhost`)",
		"traefik.http.services.vault.loadbalancer.server.port": "8200",
	}
	svc.Restart = "unless-stopped"
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "vault", "status"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  3,
	}

	// Generate Vault configuration
	vaultConfig := m.generateVaultConfig()
	result.AddConfig("config/vault/vault.hcl", []byte(vaultConfig))
	result.AddConfig("config/kms/app-change.env", []byte(m.generateAppChangeConfig(keyID)))
	result.AddConfig("config/kms/reencrypt-plan.json", []byte(m.generateReencryptPlan(res)))

	// Generate Transit engine setup script
	transitSetup := m.generateTransitSetupScript(res)
	result.AddScript("scripts/vault/setup-transit.sh", []byte(transitSetup))

	// Generate key migration script
	migrationScript := m.generateMigrationScript(res)
	result.AddScript("scripts/kms-migrate.sh", []byte(migrationScript))
	result.AddScript("scripts/kms-reencrypt.sh", []byte(m.generateReencryptScript(res)))
	result.AddScript("scripts/kms-backup.sh", []byte(m.generateBackupScript(keyID)))
	result.AddScript("scripts/kms-cutover.sh", []byte(m.generateCutoverScript(keyID)))

	// Generate AWS KMS export script
	exportScript := m.generateExportScript(res)
	result.AddScript("scripts/kms-export.sh", []byte(exportScript))

	// Generate encryption/decryption test script
	testScript := m.generateTestScript(res)
	result.AddScript("scripts/vault/test-transit.sh", []byte(testScript))
	for _, step := range securityrunbook.KMSTransit(keyID) {
		result.AddRunbookStep(step)
	}

	// Add warnings and manual steps based on key configuration
	m.addMigrationWarnings(result, res, keyUsage, keySpec)

	return result, nil
}

type KeyMaterialExportClassification struct {
	Status string
	Reason string
}

func KMSKeyMaterialExportStatus(res *resource.AWSResource) KeyMaterialExportClassification {
	origin := res.GetConfigString("origin")
	if origin == "" || origin == "AWS_KMS" {
		return KeyMaterialExportClassification{
			Status: "impossible",
			Reason: "managed AWS KMS key material is non-exportable; create a new Vault Transit key and re-encrypt data",
		}
	}
	if origin == "EXTERNAL" {
		return KeyMaterialExportClassification{
			Status: "guided",
			Reason: "externally imported key material may exist outside AWS; import only from your original custody source",
		}
	}
	return KeyMaterialExportClassification{
		Status: "guided",
		Reason: "key material export depends on the original key custody model",
	}
}

func (m *KMSMapper) generateVaultConfig() string {
	return `# Vault Server Configuration
# Generated for KMS key migration

ui = true

storage "file" {
  path = "/vault/data"
}

listener "tcp" {
  address     = "0.0.0.0:8200"
  tls_disable = 1
}

# For production, enable TLS:
# listener "tcp" {
#   address       = "0.0.0.0:8200"
#   tls_cert_file = "/vault/config/tls/vault.crt"
#   tls_key_file  = "/vault/config/tls/vault.key"
# }

# Enable audit logging
# audit {
#   type = "file"
#   path = "/vault/logs/audit.log"
# }

api_addr = "http://0.0.0.0:8200"
cluster_addr = "https://0.0.0.0:8201"
`
}

func (m *KMSMapper) generateAppChangeConfig(keyID string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=adapter
SOURCE_KEY_ID=%s
TARGET_TRANSIT_KEY=%s
AWS_ENDPOINT_URL_KMS=http://homeport:8080/api/v1/compat/aws/kms
HOMEPORT_COMPAT_BACKEND=vault-transit
HOMEPORT_COMPAT_PROTOCOL=kms
`, keyID, keyID)
}

func (m *KMSMapper) generateReencryptPlan(res *resource.AWSResource) string {
	keyID := res.GetConfigString("key_id")
	if keyID == "" {
		keyID = res.ID
	}
	region := res.Region
	if region == "" {
		region = "us-east-1"
	}
	return fmt.Sprintf(`{
  "source_provider": "aws",
  "source_service": "kms",
  "source_key_id": %q,
  "source_region": %q,
  "target": "vault-transit",
  "target_key": %q,
  "ciphertexts": []
}
`, keyID, region, keyID)
}

func (m *KMSMapper) generateTransitSetupScript(res *resource.AWSResource) string {
	keyID := res.GetConfigString("key_id")
	if keyID == "" {
		keyID = res.ID
	}

	keySpec := res.GetConfigString("key_spec")
	keyUsage := res.GetConfigString("key_usage")

	// Map KMS key spec to Vault key type
	vaultKeyType := m.mapKeySpecToVaultType(keySpec)

	var aliases []string
	if aliasesRaw := res.Config["aliases"]; aliasesRaw != nil {
		if aliasSlice, ok := aliasesRaw.([]string); ok {
			aliases = aliasSlice
		}
	}

	aliasComment := ""
	if len(aliases) > 0 {
		aliasComment = fmt.Sprintf("# Original KMS aliases: %s\n", strings.Join(aliases, ", "))
	}

	return fmt.Sprintf(`#!/bin/bash
# Vault Transit Engine Setup Script
# Migrated from AWS KMS key: %s
# Key Usage: %s
# Key Spec: %s
%s
set -e

VAULT_ADDR="${VAULT_ADDR:-http://localhost:8200}"
VAULT_TOKEN="${VAULT_TOKEN:-root}"

export VAULT_ADDR
export VAULT_TOKEN

echo "============================================"
echo "Setting up Vault Transit Secrets Engine"
echo "============================================"

# Wait for Vault to be ready
echo "Waiting for Vault..."
until vault status > /dev/null 2>&1; do
  sleep 2
done
echo "Vault is ready."

# Enable Transit secrets engine
echo "Enabling Transit secrets engine..."
vault secrets enable transit 2>/dev/null || echo "Transit engine already enabled"

# Create encryption key equivalent to KMS key
echo "Creating transit key: %s"
vault write -f transit/keys/%s \
  type=%s \
  deletion_allowed=true \
  exportable=false \
  allow_plaintext_backup=false

vault write transit/keys/%s/config \
  min_decryption_version=1 \
  min_encryption_version=0 \
  deletion_allowed=true \
  auto_rotate_period="${VAULT_AUTO_ROTATE_PERIOD:-720h}"

# Configure key rotation (optional, uncomment to enable)
# vault write transit/keys/%s/config \
#   min_decryption_version=1 \
#   min_encryption_version=0 \
#   deletion_allowed=true \
#   auto_rotate_period="720h"

echo ""
echo "============================================"
echo "Transit Engine Setup Complete!"
echo "============================================"
echo ""
echo "Key Name: %s"
echo "Key Type: %s"
echo "Vault Address: $VAULT_ADDR"
echo ""
echo "Usage Examples:"
echo "  Encrypt: vault write transit/encrypt/%s plaintext=\$(echo -n 'secret' | base64)"
echo "  Decrypt: vault write transit/decrypt/%s ciphertext=vault:v1:..."
echo "  Rotate:  vault write -f transit/keys/%s/rotate"
echo ""
`, keyID, keyUsage, keySpec, aliasComment, keyID, keyID, vaultKeyType, keyID, keyID, keyID, vaultKeyType, keyID, keyID, keyID)
}

func (m *KMSMapper) mapKeySpecToVaultType(keySpec string) string {
	switch keySpec {
	case "SYMMETRIC_DEFAULT":
		return "aes256-gcm96"
	case "RSA_2048":
		return "rsa-2048"
	case "RSA_3072":
		return "rsa-3072"
	case "RSA_4096":
		return "rsa-4096"
	case "ECC_NIST_P256", "ECC_SECG_P256K1":
		return "ecdsa-p256"
	case "ECC_NIST_P384":
		return "ecdsa-p384"
	case "ECC_NIST_P521":
		return "ecdsa-p521"
	case "HMAC_224", "HMAC_256", "HMAC_384", "HMAC_512":
		return "aes256-gcm96" // Vault uses HMAC differently
	default:
		return "aes256-gcm96"
	}
}

func (m *KMSMapper) generateMigrationScript(res *resource.AWSResource) string {
	keyID := res.GetConfigString("key_id")
	if keyID == "" {
		keyID = res.ID
	}

	return fmt.Sprintf(`#!/bin/bash
# KMS to Vault Migration Script
# Key ID: %s

set -e

echo "============================================"
echo "KMS to Vault Transit Migration"
echo "============================================"
echo ""
echo "IMPORTANT: AWS KMS keys cannot be exported."
echo "This script helps you migrate your encryption workflow."
echo ""

# Step 1: Set up Vault Transit
echo "Step 1: Setting up Vault Transit engine..."
./scripts/vault/setup-transit.sh

# Step 2: Re-encrypt data (manual process)
echo ""
echo "Step 2: Re-encryption Process"
echo "=============================="
echo ""
echo "You need to re-encrypt your data with the new Vault key."
echo "For each piece of encrypted data:"
echo ""
echo "  1. Decrypt with AWS KMS:"
echo "     aws kms decrypt --ciphertext-blob fileb://encrypted.bin \\"
echo "       --key-id %s --output text --query Plaintext | base64 -d > plaintext.bin"
echo ""
echo "  2. Encrypt with Vault Transit:"
echo "     vault write transit/encrypt/%s \\"
echo "       plaintext=\$(cat plaintext.bin | base64)"
echo ""
echo "  3. Store the new ciphertext (vault:v1:...)"
echo ""

# Step 3: Update application code
echo "Step 3: Update Application Code"
echo "================================"
echo ""
echo "Replace AWS KMS SDK calls with Vault Transit API:"
echo ""
echo "  AWS KMS:   kms.Encrypt({KeyId: '%s', Plaintext: data})"
echo "  Vault:     POST /v1/transit/encrypt/%s {plaintext: base64(data)}"
echo ""
echo "  AWS KMS:   kms.Decrypt({CiphertextBlob: cipher})"
echo "  Vault:     POST /v1/transit/decrypt/%s {ciphertext: vault:v1:...}"
echo ""

echo "Migration guidance complete. See scripts/vault/test-transit.sh for testing."
`, keyID, keyID, keyID, keyID, keyID, keyID)
}

func (m *KMSMapper) generateReencryptScript(res *resource.AWSResource) string {
	keyID := res.GetConfigString("key_id")
	if keyID == "" {
		keyID = res.ID
	}
	region := res.Region
	if region == "" {
		region = "us-east-1"
	}

	return fmt.Sprintf(`#!/bin/bash
# KMS ciphertext re-encryption script
# Source key: %s

set -e

AWS_REGION="${AWS_REGION:-%s}"
KEY_ID="${KMS_KEY_ID:-%s}"
VAULT_ADDR="${VAULT_ADDR:-http://localhost:8200}"
VAULT_TOKEN="${VAULT_TOKEN:-root}"
TARGET_KEY="${TARGET_TRANSIT_KEY:-%s}"
MANIFEST="${REENCRYPT_MANIFEST:-config/kms/reencrypt-plan.json}"
OUTPUT_DIR="${REENCRYPT_OUTPUT_DIR:-kms-reencrypted}"

export VAULT_ADDR
export VAULT_TOKEN

test -s "$MANIFEST"
mkdir -p "$OUTPUT_DIR"

count=$(jq '.ciphertexts | length' "$MANIFEST")
if [ "$count" = "0" ]; then
  echo "No ciphertext entries in $MANIFEST; generated plan is valid."
  exit 0
fi

jq -c '.ciphertexts[]' "$MANIFEST" | while read -r item; do
  id=$(printf '%%s' "$item" | jq -r '.id')
  blob=$(printf '%%s' "$item" | jq -r '.ciphertext_blob')
  plaintext_file="$OUTPUT_DIR/$id.plaintext"
  ciphertext_file="$OUTPUT_DIR/$id.vault-ciphertext"

  aws kms decrypt \
    --region "$AWS_REGION" \
    --key-id "$KEY_ID" \
    --ciphertext-blob "$blob" \
    --output text \
    --query Plaintext | base64 -d > "$plaintext_file"

  vault write -format=json "transit/encrypt/$TARGET_KEY" \
    plaintext="$(base64 < "$plaintext_file")" | jq -r '.data.ciphertext' > "$ciphertext_file"

  rm -f "$plaintext_file"
done

echo "Re-encrypted $count ciphertext entries into $OUTPUT_DIR"
`, keyID, region, keyID, keyID)
}

func (m *KMSMapper) generateExportScript(res *resource.AWSResource) string {
	keyID := res.GetConfigString("key_id")
	region := res.Region
	if region == "" {
		region = "us-east-1"
	}

	return fmt.Sprintf(`#!/bin/bash
# AWS KMS Key Export Script
# Exports key metadata (not the actual key material)

set -e

AWS_REGION="%s"
KEY_ID="%s"
OUTPUT_DIR="./kms-export"

echo "Exporting KMS key metadata: $KEY_ID"
mkdir -p "$OUTPUT_DIR"

# Export key metadata
echo "Exporting key description..."
aws kms describe-key \
  --key-id "$KEY_ID" \
  --region "$AWS_REGION" \
  --output json > "$OUTPUT_DIR/key-metadata.json"

# Export key policy
echo "Exporting key policy..."
aws kms get-key-policy \
  --key-id "$KEY_ID" \
  --policy-name default \
  --region "$AWS_REGION" \
  --output json > "$OUTPUT_DIR/key-policy.json"

# Export key aliases
echo "Exporting key aliases..."
aws kms list-aliases \
  --key-id "$KEY_ID" \
  --region "$AWS_REGION" \
  --output json > "$OUTPUT_DIR/key-aliases.json"

# Export grants
echo "Exporting key grants..."
aws kms list-grants \
  --key-id "$KEY_ID" \
  --region "$AWS_REGION" \
  --output json > "$OUTPUT_DIR/key-grants.json"

# Export key rotation status
echo "Exporting rotation status..."
aws kms get-key-rotation-status \
  --key-id "$KEY_ID" \
  --region "$AWS_REGION" \
  --output json > "$OUTPUT_DIR/key-rotation.json" 2>/dev/null || true

# Export tags
echo "Exporting key tags..."
aws kms list-resource-tags \
  --key-id "$KEY_ID" \
  --region "$AWS_REGION" \
  --output json > "$OUTPUT_DIR/key-tags.json"

echo ""
echo "Export complete! Files saved to: $OUTPUT_DIR"
echo ""
echo "IMPORTANT: AWS KMS keys cannot be exported."
echo "The key material stays in AWS. You must:"
echo "1. Create new keys in Vault"
echo "2. Re-encrypt all data with the new keys"
echo "3. Update applications to use Vault"
`, region, keyID)
}

func (m *KMSMapper) generateBackupScript(keyID string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-kms-vault-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/kms config/vault scripts/kms-export.sh scripts/kms-reencrypt.sh scripts/vault/setup-transit.sh scripts/vault/test-transit.sh
echo "$archive"
`, keyID)
}

func (m *KMSMapper) generateCutoverScript(keyID string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
test -s config/kms/app-change.env
. config/kms/app-change.env
test "$SOURCE_KEY_ID" = %q
test "$TARGET_TRANSIT_KEY" = %q
test "$APP_CHANGE_MODE" = "adapter"
echo "Use AWS_ENDPOINT_URL_KMS=$AWS_ENDPOINT_URL_KMS for KMS clients backed by Vault Transit."
`, keyID, keyID)
}

func (m *KMSMapper) generateTestScript(res *resource.AWSResource) string {
	keyID := res.GetConfigString("key_id")
	if keyID == "" {
		keyID = res.ID
	}

	return fmt.Sprintf(`#!/bin/bash
# Vault Transit Test Script
# Tests encryption/decryption with the migrated key

set -e

VAULT_ADDR="${VAULT_ADDR:-http://localhost:8200}"
VAULT_TOKEN="${VAULT_TOKEN:-root}"
KEY_NAME="%s"

export VAULT_ADDR
export VAULT_TOKEN

echo "============================================"
echo "Testing Vault Transit Key: $KEY_NAME"
echo "============================================"

# Test data
TEST_DATA="Hello, this is a test message for KMS migration!"
echo "Original: $TEST_DATA"

# Encrypt
echo ""
echo "Encrypting..."
ENCRYPTED=$(vault write -format=json transit/encrypt/$KEY_NAME \
  plaintext=$(echo -n "$TEST_DATA" | base64) | jq -r '.data.ciphertext')
echo "Ciphertext: $ENCRYPTED"

# Decrypt
echo ""
echo "Decrypting..."
DECRYPTED=$(vault write -format=json transit/decrypt/$KEY_NAME \
  ciphertext="$ENCRYPTED" | jq -r '.data.plaintext' | base64 -d)
echo "Decrypted: $DECRYPTED"

# Verify
echo ""
if [ "$TEST_DATA" = "$DECRYPTED" ]; then
  echo "✓ SUCCESS: Encryption/decryption working correctly!"
else
  echo "✗ FAILED: Decrypted data does not match original!"
  exit 1
fi

# Test key rotation
echo ""
echo "Testing key rotation..."
vault write -f transit/keys/$KEY_NAME/rotate
echo "Key rotated. New version created."

# Encrypt with new key version
echo ""
echo "Encrypting with new key version..."
ENCRYPTED_V2=$(vault write -format=json transit/encrypt/$KEY_NAME \
  plaintext=$(echo -n "$TEST_DATA" | base64) | jq -r '.data.ciphertext')
echo "New ciphertext: $ENCRYPTED_V2"

# Verify old ciphertext still decrypts
echo ""
echo "Verifying old ciphertext still decrypts..."
DECRYPTED_OLD=$(vault write -format=json transit/decrypt/$KEY_NAME \
  ciphertext="$ENCRYPTED" | jq -r '.data.plaintext' | base64 -d)
if [ "$TEST_DATA" = "$DECRYPTED_OLD" ]; then
  echo "✓ SUCCESS: Old ciphertext still decrypts correctly!"
else
  echo "✗ FAILED: Old ciphertext decryption failed!"
  exit 1
fi

echo ""
echo "============================================"
echo "All tests passed!"
echo "============================================"
`, keyID)
}

func (m *KMSMapper) addMigrationWarnings(result *mapper.MappingResult, res *resource.AWSResource, keyUsage, keySpec string) {
	exportStatus := KMSKeyMaterialExportStatus(res)
	result.AddWarning(fmt.Sprintf("Key material export status: %s - %s", exportStatus.Status, exportStatus.Reason))

	// Key usage warnings
	switch keyUsage {
	case "ENCRYPT_DECRYPT":
		result.AddWarning("KMS key used for encryption/decryption. Vault Transit provides equivalent functionality.")
	case "SIGN_VERIFY":
		result.AddWarning("KMS key used for signing/verification. Use Vault Transit sign/verify endpoints.")
	case "GENERATE_VERIFY_MAC":
		result.AddWarning("KMS key used for HMAC. Vault Transit supports HMAC operations.")
	}

	// Key spec warnings
	if strings.HasPrefix(keySpec, "RSA") {
		result.AddWarning(fmt.Sprintf("RSA key (%s) detected. Vault Transit supports RSA keys for signing.", keySpec))
	}
	if strings.HasPrefix(keySpec, "ECC") {
		result.AddWarning(fmt.Sprintf("ECC key (%s) detected. Vault Transit supports ECDSA keys.", keySpec))
	}

	// Multi-region warning
	if res.GetConfigBool("multi_region") {
		result.AddWarning("Multi-region KMS key detected. Consider Vault Enterprise for cross-datacenter replication.")
	}

	// Critical warning
	result.AddWarning("CRITICAL: KMS key material cannot be exported. You must re-encrypt all data.")

	// Volumes
	result.AddVolume(mapper.Volume{
		Name:   "vault-data",
		Driver: "local",
	})
}
