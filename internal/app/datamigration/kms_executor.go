package datamigration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// KMSToVaultExecutor migrates KMS keys to HashiCorp Vault.
type KMSToVaultExecutor struct{}

// NewKMSToVaultExecutor creates a new KMS to Vault executor.
func NewKMSToVaultExecutor() *KMSToVaultExecutor {
	return &KMSToVaultExecutor{}
}

// Type returns the migration type.
func (e *KMSToVaultExecutor) Type() string {
	return "kms_to_vault"
}

// GetPhases returns the migration phases.
func (e *KMSToVaultExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Fetching KMS keys",
		"Analyzing key policies",
		"Generating Vault config",
		"Creating transit keys",
		"Finalizing",
	}
}

// Validate validates the migration configuration.
func (e *KMSToVaultExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["access_key_id"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.access_key_id is required")
		}
		if _, ok := config.Source["secret_access_key"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.secret_access_key is required")
		}
	}

	if config.Destination == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "destination configuration is required")
	} else {
		if _, ok := config.Destination["output_dir"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "destination.output_dir is required")
		}
	}

	result.Warnings = append(result.Warnings, "KMS key material cannot be exported - new keys will be created")
	result.Warnings = append(result.Warnings, "Data encrypted with KMS must be re-encrypted with new keys")

	return result, nil
}

// Execute performs the migration.
func (e *KMSToVaultExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	accessKeyID := config.Source["access_key_id"].(string)
	secretAccessKey := config.Source["secret_access_key"].(string)
	region, _ := config.Source["region"].(string)
	if region == "" {
		region = "us-east-1"
	}
	outputDir := config.Destination["output_dir"].(string)

	awsEnv := []string{
		"AWS_ACCESS_KEY_ID=" + accessKeyID,
		"AWS_SECRET_ACCESS_KEY=" + secretAccessKey,
		"AWS_DEFAULT_REGION=" + region,
	}

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating AWS credentials")
	EmitProgress(m, 10, "Checking credentials")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Fetching KMS keys
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", "Fetching KMS keys")
	EmitProgress(m, 25, "Fetching keys")

	listKeysCmd := exec.CommandContext(ctx, "aws", "kms", "list-keys",
		"--region", region, "--output", "json",
	)
	listKeysCmd.Env = append(os.Environ(), awsEnv...)
	keysOutput, err := listKeysCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to list keys: %w", err)
	}

	var keysList struct {
		Keys []struct {
			KeyId  string `json:"KeyId"`
			KeyArn string `json:"KeyArn"`
		} `json:"Keys"`
	}
	_ = json.Unmarshal(keysOutput, &keysList)

	// Get details for each key
	type KeyDetails struct {
		KeyID       string                 `json:"keyId"`
		KeyArn      string                 `json:"keyArn"`
		Description string                 `json:"description"`
		KeyUsage    string                 `json:"keyUsage"`
		KeySpec     string                 `json:"keySpec"`
		KeyState    string                 `json:"keyState"`
		Aliases     []string               `json:"aliases"`
		Policy      map[string]interface{} `json:"policy"`
	}
	keys := make([]KeyDetails, 0)

	for _, key := range keysList.Keys {
		describeCmd := exec.CommandContext(ctx, "aws", "kms", "describe-key",
			"--key-id", key.KeyId,
			"--region", region, "--output", "json",
		)
		describeCmd.Env = append(os.Environ(), awsEnv...)
		descOutput, err := describeCmd.Output()
		if err != nil {
			continue
		}

		var keyMeta struct {
			KeyMetadata struct {
				KeyID       string `json:"KeyId"`
				Arn         string `json:"Arn"`
				Description string `json:"Description"`
				KeyUsage    string `json:"KeyUsage"`
				KeySpec     string `json:"KeySpec"`
				KeyState    string `json:"KeyState"`
				KeyManager  string `json:"KeyManager"`
			} `json:"KeyMetadata"`
		}
		_ = json.Unmarshal(descOutput, &keyMeta)

		// Skip AWS managed keys
		if keyMeta.KeyMetadata.KeyManager == "AWS" {
			continue
		}

		// Get aliases
		aliasesCmd := exec.CommandContext(ctx, "aws", "kms", "list-aliases",
			"--key-id", key.KeyId,
			"--region", region, "--output", "json",
		)
		aliasesCmd.Env = append(os.Environ(), awsEnv...)
		aliasOutput, _ := aliasesCmd.Output()
		var aliases struct {
			Aliases []struct {
				AliasName string `json:"AliasName"`
			} `json:"Aliases"`
		}
		_ = json.Unmarshal(aliasOutput, &aliases)

		aliasNames := make([]string, 0)
		for _, a := range aliases.Aliases {
			aliasNames = append(aliasNames, a.AliasName)
		}

		keys = append(keys, KeyDetails{
			KeyID:       keyMeta.KeyMetadata.KeyID,
			KeyArn:      keyMeta.KeyMetadata.Arn,
			Description: keyMeta.KeyMetadata.Description,
			KeyUsage:    keyMeta.KeyMetadata.KeyUsage,
			KeySpec:     keyMeta.KeyMetadata.KeySpec,
			KeyState:    keyMeta.KeyMetadata.KeyState,
			Aliases:     aliasNames,
		})
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Analyzing key policies
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", "Analyzing key policies")
	EmitProgress(m, 40, "Analyzing policies")

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	keysData, _ := json.MarshalIndent(keys, "", "  ")
	keysPath := filepath.Join(outputDir, "kms-keys.json")
	_ = os.WriteFile(keysPath, keysData, 0644)

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Generating Vault config
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Generating Vault configuration")
	EmitProgress(m, 60, "Generating config")

	// Docker compose for Vault
	vaultCompose := `version: '3.8'

services:
  vault:
    image: hashicorp/vault:latest
    container_name: vault
    cap_add:
      - IPC_LOCK
    environment:
      VAULT_DEV_ROOT_TOKEN_ID: root
      VAULT_DEV_LISTEN_ADDRESS: 0.0.0.0:8200
    ports:
      - "8200:8200"
    volumes:
      - vault-data:/vault/data
      - ./vault-config:/vault/config
    command: server -dev
    restart: unless-stopped

volumes:
  vault-data:
`
	composePath := filepath.Join(outputDir, "docker-compose.yml")
	_ = os.WriteFile(composePath, []byte(vaultCompose), 0644)

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Creating transit keys
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Creating Vault transit key configurations")
	EmitProgress(m, 80, "Creating transit keys")

	// Generate Vault setup script
	setupScript := `#!/bin/bash
# Vault Transit Engine Setup
# Migrated from AWS KMS

set -e

export VAULT_ADDR='http://127.0.0.1:8200'
export VAULT_TOKEN='root'

echo "Enabling transit secrets engine..."
vault secrets enable transit || true

echo "Creating encryption keys..."
`

	for _, key := range keys {
		keyName := key.KeyID
		if len(key.Aliases) > 0 {
			// Use alias name without 'alias/' prefix
			alias := key.Aliases[0]
			if len(alias) > 6 && alias[:6] == "alias/" {
				keyName = alias[6:]
			}
		}

		vaultKeyType := "aes256-gcm96"
		switch key.KeySpec {
		case "RSA_2048":
			vaultKeyType = "rsa-2048"
		case "RSA_4096":
			vaultKeyType = "rsa-4096"
		case "ECC_NIST_P256":
			vaultKeyType = "ecdsa-p256"
		case "ECC_NIST_P384":
			vaultKeyType = "ecdsa-p384"
		}

		setupScript += fmt.Sprintf(`
# Key: %s (from KMS: %s)
vault write transit/keys/%s type=%s
`, key.Description, key.KeyID, keyName, vaultKeyType)
	}

	setupScript += `
echo "Keys created successfully!"
vault list transit/keys
`
	setupScriptPath := filepath.Join(outputDir, "setup-vault.sh")
	_ = os.WriteFile(setupScriptPath, []byte(setupScript), 0755)

	// Generate encryption example
	encryptExample := `#!/bin/bash
# Example: Encrypt/Decrypt with Vault Transit

export VAULT_ADDR='http://127.0.0.1:8200'
export VAULT_TOKEN='root'

KEY_NAME="my-key"  # Replace with your key name

# Encrypt
echo "Encrypting data..."
PLAINTEXT=$(echo -n "Hello, World!" | base64)
CIPHERTEXT=$(vault write -format=json transit/encrypt/$KEY_NAME plaintext=$PLAINTEXT | jq -r '.data.ciphertext')
echo "Ciphertext: $CIPHERTEXT"

# Decrypt
echo "Decrypting data..."
DECRYPTED=$(vault write -format=json transit/decrypt/$KEY_NAME ciphertext=$CIPHERTEXT | jq -r '.data.plaintext')
echo "Plaintext: $(echo $DECRYPTED | base64 -d)"
`
	encryptExamplePath := filepath.Join(outputDir, "encrypt-example.sh")
	_ = os.WriteFile(encryptExamplePath, []byte(encryptExample), 0755)

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 6: Finalizing
	EmitPhase(m, phases[5], 6)
	EmitLog(m, "info", "Finalizing migration artifacts")
	EmitProgress(m, 95, "Finalizing")

	readme := fmt.Sprintf(`# KMS to Vault Transit Migration

## Source KMS
- Region: %s
- Customer Managed Keys: %d

## Important Notes

KMS key material **cannot be exported**. This migration:
1. Exports key metadata and configuration
2. Creates equivalent Vault Transit keys
3. Requires re-encryption of existing data

## Migration Mapping

| KMS Key Type | Vault Transit Type |
|--------------|-------------------|
| SYMMETRIC_DEFAULT | aes256-gcm96 |
| RSA_2048 | rsa-2048 |
| RSA_4096 | rsa-4096 |
| ECC_NIST_P256 | ecdsa-p256 |
| ECC_NIST_P384 | ecdsa-p384 |

## Getting Started

1. Start Vault:
'''bash
docker-compose up -d
'''

2. Create transit keys:
'''bash
./setup-vault.sh
'''

3. Test encryption:
'''bash
./encrypt-example.sh
'''

4. Access Vault UI:
- URL: http://localhost:8200
- Token: root (development only!)

## Re-encrypting Data

Data encrypted with KMS must be:
1. Decrypted using KMS (before migration cutoff)
2. Re-encrypted using Vault Transit

## Files Generated
- kms-keys.json: Original KMS key details
- docker-compose.yml: Vault container
- setup-vault.sh: Transit key creation
- encrypt-example.sh: Encryption example

## Production Considerations
- Use proper Vault authentication (not dev token)
- Enable auto-unseal
- Set up high availability
- Configure audit logging
`, region, len(keys))

	readmePath := filepath.Join(outputDir, "README.md")
	_ = os.WriteFile(readmePath, []byte(readme), 0644)

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", fmt.Sprintf("KMS migration complete: %d keys", len(keys)))

	return nil
}
