package datamigration

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SecretsToVaultExecutor migrates AWS Secrets Manager secrets to HashiCorp Vault format.
type SecretsToVaultExecutor struct{}

// NewSecretsToVaultExecutor creates a new Secrets Manager to Vault executor.
func NewSecretsToVaultExecutor() *SecretsToVaultExecutor {
	return &SecretsToVaultExecutor{}
}

// Type returns the migration type.
func (e *SecretsToVaultExecutor) Type() string {
	return "secrets_to_vault"
}

// GetPhases returns the migration phases.
func (e *SecretsToVaultExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Listing secrets",
		"Fetching secret values",
		"Generating Vault import",
		"Writing output files",
	}
}

// Validate validates the migration configuration.
func (e *SecretsToVaultExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	// Validate source config
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
		// secret_prefix is optional - for filtering secrets by prefix
		if prefix, ok := config.Source["secret_prefix"].(string); ok && prefix != "" {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Will only migrate secrets with prefix: %s", prefix))
		}
		if _, ok := config.Source["region"].(string); !ok {
			result.Warnings = append(result.Warnings, "source.region not specified, using default us-east-1")
		}
	}

	// Validate destination config
	if config.Destination == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "destination configuration is required")
	} else {
		if _, ok := config.Destination["output_dir"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "destination.output_dir is required")
		}
		// vault_path is optional, defaults to "secret/migrated"
	}

	// Security warnings
	result.Warnings = append(result.Warnings, "WARNING: Secret values will be written to disk temporarily")
	result.Warnings = append(result.Warnings, "WARNING: Avoid logging sensitive data during migration")
	result.Warnings = append(result.Warnings, "Ensure the output directory has restricted permissions")

	if encrypt, ok := config.Destination["encrypt"].(bool); ok && encrypt {
		if _, ok := config.Destination["encryption_key"].(string); !ok {
			result.Warnings = append(result.Warnings, "destination.encrypt is true but no encryption_key provided, will generate one")
		}
	}

	return result, nil
}

// secretEntry represents a secret from AWS Secrets Manager.
type secretEntry struct {
	ARN         string `json:"ARN"`
	Name        string `json:"Name"`
	Description string `json:"Description,omitempty"`
}

// secretValue represents the value of a secret.
type secretValue struct {
	Name         string                 `json:"name"`
	ARN          string                 `json:"arn"`
	SecretString string                 `json:"secret_string,omitempty"`
	SecretJSON   map[string]interface{} `json:"secret_json,omitempty"`
	IsJSON       bool                   `json:"is_json"`
}

// Execute performs the migration.
func (e *SecretsToVaultExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	// Extract source configuration
	accessKeyID := config.Source["access_key_id"].(string)
	secretAccessKey := config.Source["secret_access_key"].(string)
	region, _ := config.Source["region"].(string)
	if region == "" {
		region = "us-east-1"
	}
	secretPrefix, _ := config.Source["secret_prefix"].(string)

	// Extract destination configuration
	outputDir := config.Destination["output_dir"].(string)
	vaultPath, _ := config.Destination["vault_path"].(string)
	if vaultPath == "" {
		vaultPath = "secret/migrated"
	}
	shouldEncrypt, _ := config.Destination["encrypt"].(bool)
	encryptionKey, _ := config.Destination["encryption_key"].(string)

	// =========================================================================
	// Phase 1: Validating credentials
	// =========================================================================
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating AWS credentials")
	EmitProgress(m, 5, "Checking AWS credentials")

	testCmd := exec.CommandContext(ctx, "aws", "sts", "get-caller-identity", "--region", region)
	testCmd.Env = append(os.Environ(),
		"AWS_ACCESS_KEY_ID="+accessKeyID,
		"AWS_SECRET_ACCESS_KEY="+secretAccessKey,
		"AWS_DEFAULT_REGION="+region,
	)

	if output, err := testCmd.CombinedOutput(); err != nil {
		EmitLog(m, "error", fmt.Sprintf("AWS credential validation failed: %s", string(output)))
		return fmt.Errorf("failed to validate AWS credentials: %w", err)
	}
	EmitLog(m, "info", "AWS credentials validated successfully")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// =========================================================================
	// Phase 2: Listing secrets
	// =========================================================================
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", "Listing secrets from AWS Secrets Manager")
	EmitProgress(m, 15, "Fetching secret list")

	var allSecrets []secretEntry
	var nextToken string

	for {
		if m.IsCancelled() {
			return fmt.Errorf("migration cancelled")
		}

		args := []string{"secretsmanager", "list-secrets", "--region", region, "--output", "json"}
		if nextToken != "" {
			args = append(args, "--next-token", nextToken)
		}

		listCmd := exec.CommandContext(ctx, "aws", args...)
		listCmd.Env = append(os.Environ(),
			"AWS_ACCESS_KEY_ID="+accessKeyID,
			"AWS_SECRET_ACCESS_KEY="+secretAccessKey,
			"AWS_DEFAULT_REGION="+region,
		)

		output, err := listCmd.Output()
		if err != nil {
			EmitLog(m, "error", "Failed to list secrets")
			return fmt.Errorf("failed to list secrets: %w", err)
		}

		var listResult struct {
			SecretList []secretEntry `json:"SecretList"`
			NextToken  string        `json:"NextToken"`
		}

		if err := json.Unmarshal(output, &listResult); err != nil {
			return fmt.Errorf("failed to parse secrets list: %w", err)
		}

		// Filter by prefix if specified
		for _, secret := range listResult.SecretList {
			if secretPrefix == "" || strings.HasPrefix(secret.Name, secretPrefix) {
				allSecrets = append(allSecrets, secret)
			}
		}

		if listResult.NextToken == "" {
			break
		}
		nextToken = listResult.NextToken
	}

	if len(allSecrets) == 0 {
		EmitLog(m, "warn", "No secrets found matching the criteria")
		EmitProgress(m, 100, "No secrets to migrate")
		return nil
	}

	EmitLog(m, "info", fmt.Sprintf("Found %d secrets to migrate", len(allSecrets)))

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// =========================================================================
	// Phase 3: Fetching secret values
	// =========================================================================
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", "Fetching secret values")
	EmitLog(m, "warn", "SECURITY: Sensitive data is being processed - avoid verbose logging")
	EmitProgress(m, 30, "Retrieving secret values")

	var secretValues []secretValue
	for i, secret := range allSecrets {
		if m.IsCancelled() {
			return fmt.Errorf("migration cancelled")
		}

		EmitLog(m, "info", fmt.Sprintf("Fetching secret %d/%d: %s", i+1, len(allSecrets), secret.Name))

		getCmd := exec.CommandContext(ctx, "aws", "secretsmanager", "get-secret-value",
			"--secret-id", secret.ARN,
			"--region", region,
			"--output", "json",
		)
		getCmd.Env = append(os.Environ(),
			"AWS_ACCESS_KEY_ID="+accessKeyID,
			"AWS_SECRET_ACCESS_KEY="+secretAccessKey,
			"AWS_DEFAULT_REGION="+region,
		)

		output, err := getCmd.Output()
		if err != nil {
			EmitLog(m, "warn", fmt.Sprintf("Failed to get secret value for %s, skipping", secret.Name))
			continue
		}

		var getResult struct {
			ARN          string `json:"ARN"`
			Name         string `json:"Name"`
			SecretString string `json:"SecretString"`
			SecretBinary string `json:"SecretBinary"`
		}

		if err := json.Unmarshal(output, &getResult); err != nil {
			EmitLog(m, "warn", fmt.Sprintf("Failed to parse secret value for %s, skipping", secret.Name))
			continue
		}

		sv := secretValue{
			Name: secret.Name,
			ARN:  secret.ARN,
		}

		// Try to parse as JSON
		if getResult.SecretString != "" {
			var jsonValue map[string]interface{}
			if err := json.Unmarshal([]byte(getResult.SecretString), &jsonValue); err == nil {
				sv.IsJSON = true
				sv.SecretJSON = jsonValue
			} else {
				sv.IsJSON = false
				sv.SecretString = getResult.SecretString
			}
		} else if getResult.SecretBinary != "" {
			// Binary secret - store as base64
			sv.IsJSON = false
			sv.SecretString = getResult.SecretBinary
			EmitLog(m, "info", fmt.Sprintf("Secret %s contains binary data (stored as base64)", secret.Name))
		}

		secretValues = append(secretValues, sv)

		progress := 30 + (30 * (i + 1) / len(allSecrets))
		EmitProgress(m, progress, fmt.Sprintf("Retrieved %d/%d secrets", i+1, len(allSecrets)))
	}

	EmitLog(m, "info", fmt.Sprintf("Successfully retrieved %d secret values", len(secretValues)))

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// =========================================================================
	// Phase 4: Generating Vault import
	// =========================================================================
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Generating Vault import scripts")
	EmitProgress(m, 65, "Creating import scripts")

	// Generate vault-import.sh script
	var vaultScript strings.Builder
	vaultScript.WriteString("#!/bin/bash\n")
	vaultScript.WriteString("# Vault import script generated from AWS Secrets Manager migration\n")
	vaultScript.WriteString("# Generated by Homeport Data Migration\n")
	vaultScript.WriteString("#\n")
	vaultScript.WriteString("# SECURITY WARNING: This script contains sensitive data!\n")
	vaultScript.WriteString("# - Review before running\n")
	vaultScript.WriteString("# - Delete after use\n")
	vaultScript.WriteString("# - Ensure Vault is properly configured\n")
	vaultScript.WriteString("#\n")
	vaultScript.WriteString("# Prerequisites:\n")
	vaultScript.WriteString("#   - vault CLI installed and in PATH\n")
	vaultScript.WriteString("#   - VAULT_ADDR environment variable set\n")
	vaultScript.WriteString("#   - VAULT_TOKEN environment variable set or vault login completed\n")
	vaultScript.WriteString("#   - KV v2 secrets engine enabled at the configured path\n")
	vaultScript.WriteString("#\n")
	vaultScript.WriteString("# Usage:\n")
	vaultScript.WriteString("#   chmod +x vault-import.sh\n")
	vaultScript.WriteString("#   ./vault-import.sh\n")
	vaultScript.WriteString("#\n\n")
	vaultScript.WriteString("set -e\n\n")
	vaultScript.WriteString("# Check prerequisites\n")
	vaultScript.WriteString("if ! command -v vault &> /dev/null; then\n")
	vaultScript.WriteString("    echo \"Error: vault CLI not found in PATH\"\n")
	vaultScript.WriteString("    exit 1\n")
	vaultScript.WriteString("fi\n\n")
	vaultScript.WriteString("if [ -z \"$VAULT_ADDR\" ]; then\n")
	vaultScript.WriteString("    echo \"Error: VAULT_ADDR environment variable not set\"\n")
	vaultScript.WriteString("    exit 1\n")
	vaultScript.WriteString("fi\n\n")
	vaultScript.WriteString("echo \"Starting Vault import...\"\n")
	vaultScript.WriteString(fmt.Sprintf("echo \"Target path: %s\"\n\n", vaultPath))

	for _, sv := range secretValues {
		// Sanitize secret name for Vault path (replace / and other special chars)
		vaultSecretName := strings.ReplaceAll(sv.Name, "/", "-")
		vaultSecretName = strings.ReplaceAll(vaultSecretName, " ", "_")
		fullVaultPath := fmt.Sprintf("%s/%s", vaultPath, vaultSecretName)

		vaultScript.WriteString(fmt.Sprintf("echo \"Importing: %s\"\n", vaultSecretName))

		if sv.IsJSON && sv.SecretJSON != nil {
			// Write JSON secrets as individual key-value pairs
			var kvPairs []string
			for key, value := range sv.SecretJSON {
				// Escape special characters in the value
				valueStr := fmt.Sprintf("%v", value)
				valueStr = strings.ReplaceAll(valueStr, "'", "'\"'\"'")
				kvPairs = append(kvPairs, fmt.Sprintf("%s='%s'", key, valueStr))
			}
			vaultScript.WriteString(fmt.Sprintf("vault kv put %s %s\n", fullVaultPath, strings.Join(kvPairs, " ")))
		} else {
			// Write string secrets as a single "value" key
			escapedValue := strings.ReplaceAll(sv.SecretString, "'", "'\"'\"'")
			vaultScript.WriteString(fmt.Sprintf("vault kv put %s value='%s'\n", fullVaultPath, escapedValue))
		}
		vaultScript.WriteString("\n")
	}

	vaultScript.WriteString("echo \"\"\n")
	vaultScript.WriteString(fmt.Sprintf("echo \"Import complete! %d secrets imported to %s\"\n", len(secretValues), vaultPath))
	vaultScript.WriteString("echo \"SECURITY: Please delete this script after verifying the import.\"\n")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// =========================================================================
	// Phase 5: Writing output files
	// =========================================================================
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Writing output files")
	EmitProgress(m, 85, "Writing files to disk")

	// Create output directory
	if err := os.MkdirAll(outputDir, 0700); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}
	EmitLog(m, "info", fmt.Sprintf("Output directory: %s", outputDir))

	// Write vault-import.sh
	vaultScriptPath := filepath.Join(outputDir, "vault-import.sh")
	if err := os.WriteFile(vaultScriptPath, []byte(vaultScript.String()), 0700); err != nil {
		return fmt.Errorf("failed to write vault-import.sh: %w", err)
	}
	EmitLog(m, "info", fmt.Sprintf("Created: %s", vaultScriptPath))

	// Prepare secrets JSON
	secretsData := map[string]interface{}{
		"metadata": map[string]interface{}{
			"source":      "aws_secrets_manager",
			"region":      region,
			"vault_path":  vaultPath,
			"secret_count": len(secretValues),
			"encrypted":   shouldEncrypt,
		},
		"secrets": secretValues,
	}

	secretsJSON, err := json.MarshalIndent(secretsData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal secrets JSON: %w", err)
	}

	secretsPath := filepath.Join(outputDir, "secrets.json")

	if shouldEncrypt {
		EmitLog(m, "info", "Encrypting secrets.json")

		// Generate encryption key if not provided
		if encryptionKey == "" {
			keyBytes := make([]byte, 32)
			if _, err := rand.Read(keyBytes); err != nil {
				return fmt.Errorf("failed to generate encryption key: %w", err)
			}
			encryptionKey = base64.StdEncoding.EncodeToString(keyBytes)
			EmitLog(m, "warn", "Generated encryption key - save this key to decrypt the file!")
		}

		encryptedData, err := encryptAES(secretsJSON, encryptionKey)
		if err != nil {
			return fmt.Errorf("failed to encrypt secrets: %w", err)
		}

		// Write encrypted secrets
		encryptedPath := filepath.Join(outputDir, "secrets.json.enc")
		if err := os.WriteFile(encryptedPath, []byte(encryptedData), 0600); err != nil {
			return fmt.Errorf("failed to write encrypted secrets: %w", err)
		}
		EmitLog(m, "info", fmt.Sprintf("Created encrypted file: %s", encryptedPath))

		// Write encryption key to separate file (user should move this to secure location)
		keyPath := filepath.Join(outputDir, "encryption-key.txt")
		keyContent := fmt.Sprintf("# Encryption key for secrets.json.enc\n# SECURITY: Move this file to a secure location!\n# Delete after decrypting and importing secrets.\n\n%s\n", encryptionKey)
		if err := os.WriteFile(keyPath, []byte(keyContent), 0600); err != nil {
			return fmt.Errorf("failed to write encryption key: %w", err)
		}
		EmitLog(m, "warn", fmt.Sprintf("Encryption key saved to: %s - MOVE TO SECURE LOCATION!", keyPath))

		// Write decryption helper script
		decryptScriptPath := filepath.Join(outputDir, "decrypt-secrets.sh")
		decryptScript := `#!/bin/bash
# Decryption helper for secrets.json.enc
# Usage: ./decrypt-secrets.sh <encryption-key>

if [ -z "$1" ]; then
    echo "Usage: $0 <encryption-key>"
    echo "The encryption key should be the base64-encoded key from encryption-key.txt"
    exit 1
fi

KEY="$1"
ENCRYPTED_FILE="secrets.json.enc"
OUTPUT_FILE="secrets.json"

if [ ! -f "$ENCRYPTED_FILE" ]; then
    echo "Error: $ENCRYPTED_FILE not found"
    exit 1
fi

# Decrypt using openssl (AES-256-GCM)
# The encrypted file format is: base64(nonce + ciphertext + tag)
echo "Decrypting $ENCRYPTED_FILE..."

# For Go's AES-GCM encryption, use a custom decryption or use the Go tool
echo "Note: Use the Homeport CLI or a compatible tool to decrypt this file."
echo "The file is encrypted with AES-256-GCM."
`
		if err := os.WriteFile(decryptScriptPath, []byte(decryptScript), 0700); err != nil {
			EmitLog(m, "warn", "Failed to write decryption helper script")
		}
	} else {
		// Write unencrypted secrets
		if err := os.WriteFile(secretsPath, secretsJSON, 0600); err != nil {
			return fmt.Errorf("failed to write secrets.json: %w", err)
		}
		EmitLog(m, "info", fmt.Sprintf("Created: %s", secretsPath))
		EmitLog(m, "warn", "SECURITY: secrets.json contains sensitive data - restrict access and delete after import!")
	}

	// Write a summary/README file
	readmePath := filepath.Join(outputDir, "README.txt")
	readmeContent := fmt.Sprintf(`AWS Secrets Manager to Vault Migration
======================================

Generated by Homeport Data Migration

Source: AWS Secrets Manager (Region: %s)
Target: HashiCorp Vault (Path: %s)
Secrets Migrated: %d

Files:
------
- vault-import.sh    : Bash script to import secrets into Vault
- secrets.json       : JSON file containing all secret values%s
- README.txt         : This file

Usage:
------
1. Ensure Vault is running and accessible
2. Set VAULT_ADDR environment variable
3. Authenticate to Vault (vault login)
4. Run: ./vault-import.sh

Security Notes:
---------------
- These files contain sensitive data
- Restrict file permissions (chmod 600)
- Delete files after successful import
- Do not commit to version control
- Review vault-import.sh before executing

`, region, vaultPath, len(secretValues),
		func() string {
			if shouldEncrypt {
				return " (encrypted as secrets.json.enc)"
			}
			return ""
		}())

	if err := os.WriteFile(readmePath, []byte(readmeContent), 0600); err != nil {
		EmitLog(m, "warn", "Failed to write README.txt")
	}

	// Set restrictive permissions on output directory
	if err := os.Chmod(outputDir, 0700); err != nil {
		EmitLog(m, "warn", "Failed to set restrictive permissions on output directory")
	}

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", "Secrets Manager to Vault migration completed successfully")
	EmitLog(m, "info", fmt.Sprintf("Output directory: %s", outputDir))
	EmitLog(m, "info", fmt.Sprintf("Total secrets migrated: %d", len(secretValues)))
	EmitLog(m, "warn", "REMINDER: Delete output files after importing to Vault!")

	return nil
}

// encryptAES encrypts data using AES-256-GCM.
func encryptAES(plaintext []byte, keyBase64 string) (string, error) {
	key, err := base64.StdEncoding.DecodeString(keyBase64)
	if err != nil {
		return "", fmt.Errorf("invalid base64 key: %w", err)
	}

	if len(key) != 32 {
		return "", fmt.Errorf("key must be 32 bytes for AES-256")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}
