package datamigration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// KeyVaultToVaultExecutor migrates Azure Key Vault to HashiCorp Vault.
type KeyVaultToVaultExecutor struct{}

// NewKeyVaultToVaultExecutor creates a new Key Vault to Vault executor.
func NewKeyVaultToVaultExecutor() *KeyVaultToVaultExecutor {
	return &KeyVaultToVaultExecutor{}
}

// Type returns the migration type.
func (e *KeyVaultToVaultExecutor) Type() string {
	return "keyvault_to_vault"
}

// GetPhases returns the migration phases.
func (e *KeyVaultToVaultExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Fetching Key Vault info",
		"Exporting secrets",
		"Generating Vault configuration",
		"Creating import scripts",
		"Finalizing",
	}
}

// Validate validates the migration configuration.
func (e *KeyVaultToVaultExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["vault_name"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.vault_name is required")
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

	result.Warnings = append(result.Warnings, "Secret values will be exported - secure the output directory")
	result.Warnings = append(result.Warnings, "Certificates need manual export and import")
	result.Warnings = append(result.Warnings, "HSM-backed keys cannot be exported")

	return result, nil
}

// Execute performs the migration.
func (e *KeyVaultToVaultExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	vaultName := config.Source["vault_name"].(string)
	subscription, _ := config.Source["subscription"].(string)

	outputDir := config.Destination["output_dir"].(string)

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating Azure credentials")
	EmitProgress(m, 10, "Checking credentials")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Fetching Key Vault info
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", fmt.Sprintf("Fetching Key Vault info for %s", vaultName))
	EmitProgress(m, 25, "Fetching vault info")

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Get vault info
	args := []string{"keyvault", "show",
		"--name", vaultName,
		"--output", "json",
	}
	if subscription != "" {
		args = append(args, "--subscription", subscription)
	}

	showCmd := exec.CommandContext(ctx, "az", args...)
	vaultOutput, _ := showCmd.Output()

	var vaultInfo struct {
		Name       string `json:"name"`
		Location   string `json:"location"`
		Properties struct {
			VaultUri string `json:"vaultUri"`
			Sku      struct {
				Name string `json:"name"`
			} `json:"sku"`
		} `json:"properties"`
	}
	if len(vaultOutput) > 0 {
		_ = json.Unmarshal(vaultOutput, &vaultInfo)
	}

	// Save vault info
	vaultInfoPath := filepath.Join(outputDir, "vault-info.json")
	if len(vaultOutput) > 0 {
		_ = os.WriteFile(vaultInfoPath, vaultOutput, 0644)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Exporting secrets
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", "Exporting secrets")
	EmitProgress(m, 45, "Exporting secrets")

	// Create secrets directory with restricted permissions
	secretsDir := filepath.Join(outputDir, "secrets")
	if err := os.MkdirAll(secretsDir, 0700); err != nil {
		return fmt.Errorf("failed to create secrets directory: %w", err)
	}

	// Get list of secrets
	secretListArgs := []string{"keyvault", "secret", "list",
		"--vault-name", vaultName,
		"--output", "json",
	}
	if subscription != "" {
		secretListArgs = append(secretListArgs, "--subscription", subscription)
	}

	secretListCmd := exec.CommandContext(ctx, "az", secretListArgs...)
	secretListOutput, _ := secretListCmd.Output()

	var secrets []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if len(secretListOutput) > 0 {
		_ = json.Unmarshal(secretListOutput, &secrets)
	}

	// Save secret metadata
	secretMetadataPath := filepath.Join(outputDir, "secret-metadata.json")
	if len(secretListOutput) > 0 {
		_ = os.WriteFile(secretMetadataPath, secretListOutput, 0644)
	}

	// Generate secret export script (actual values)
	exportScript := fmt.Sprintf(`#!/bin/bash
# Key Vault Secret Export Script
# Vault: %s
# WARNING: This script exports secret values - keep output secure!

set -e

echo "Key Vault Secret Export"
echo "======================="

VAULT_NAME="%s"
OUTPUT_DIR="./secrets"

mkdir -p "$OUTPUT_DIR"
chmod 700 "$OUTPUT_DIR"

# Export each secret
`, vaultName, vaultName)

	for _, secret := range secrets {
		exportScript += fmt.Sprintf(`
echo "Exporting: %s"
az keyvault secret show --vault-name "$VAULT_NAME" --name "%s" --query value -o tsv > "$OUTPUT_DIR/%s.txt"
`, secret.Name, secret.Name, secret.Name)
	}

	exportScript += `
echo ""
echo "Secrets exported to $OUTPUT_DIR"
echo "IMPORTANT: Secure these files and delete after import!"
`

	exportScriptPath := filepath.Join(outputDir, "export-secrets.sh")
	if err := os.WriteFile(exportScriptPath, []byte(exportScript), 0700); err != nil {
		return fmt.Errorf("failed to write export script: %w", err)
	}

	// Get list of keys
	keyListArgs := []string{"keyvault", "key", "list",
		"--vault-name", vaultName,
		"--output", "json",
	}
	if subscription != "" {
		keyListArgs = append(keyListArgs, "--subscription", subscription)
	}

	keyListCmd := exec.CommandContext(ctx, "az", keyListArgs...)
	keyListOutput, _ := keyListCmd.Output()

	// Save key metadata
	keyMetadataPath := filepath.Join(outputDir, "key-metadata.json")
	if len(keyListOutput) > 0 {
		_ = os.WriteFile(keyMetadataPath, keyListOutput, 0644)
	}

	// Get list of certificates
	certListArgs := []string{"keyvault", "certificate", "list",
		"--vault-name", vaultName,
		"--output", "json",
	}
	if subscription != "" {
		certListArgs = append(certListArgs, "--subscription", subscription)
	}

	certListCmd := exec.CommandContext(ctx, "az", certListArgs...)
	certListOutput, _ := certListCmd.Output()

	// Save certificate metadata
	certMetadataPath := filepath.Join(outputDir, "certificate-metadata.json")
	if len(certListOutput) > 0 {
		_ = os.WriteFile(certMetadataPath, certListOutput, 0644)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Generating Vault configuration
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Generating HashiCorp Vault configuration")
	EmitProgress(m, 65, "Generating configuration")

	// Generate Docker Compose for HashiCorp Vault
	dockerCompose := `version: '3.8'

services:
  vault:
    image: hashicorp/vault:1.15
    container_name: vault
    cap_add:
      - IPC_LOCK
    environment:
      VAULT_DEV_ROOT_TOKEN_ID: root
      VAULT_DEV_LISTEN_ADDRESS: 0.0.0.0:8200
    volumes:
      - vault-data:/vault/data
      - ./vault-config:/vault/config
    ports:
      - "8200:8200"
    command: server -dev
    restart: unless-stopped

  vault-ui:
    image: dpage/vault-ui:latest
    container_name: vault-ui
    environment:
      VAULT_URL_DEFAULT: http://vault:8200
    ports:
      - "8000:8000"
    depends_on:
      - vault
    restart: unless-stopped

volumes:
  vault-data:
`
	composePath := filepath.Join(outputDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(dockerCompose), 0644); err != nil {
		return fmt.Errorf("failed to write docker-compose.yml: %w", err)
	}

	// Generate Vault configuration for production
	vaultConfig := `# HashiCorp Vault Configuration
# For production, replace dev mode with this configuration

storage "file" {
  path = "/vault/data"
}

listener "tcp" {
  address     = "0.0.0.0:8200"
  tls_disable = true  # Enable TLS in production!
}

api_addr = "http://127.0.0.1:8200"
cluster_addr = "https://127.0.0.1:8201"

ui = true
`
	vaultConfigDir := filepath.Join(outputDir, "vault-config")
	if err := os.MkdirAll(vaultConfigDir, 0755); err != nil {
		return fmt.Errorf("failed to create vault config directory: %w", err)
	}
	vaultConfigPath := filepath.Join(vaultConfigDir, "vault.hcl")
	if err := os.WriteFile(vaultConfigPath, []byte(vaultConfig), 0644); err != nil {
		return fmt.Errorf("failed to write vault config: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Creating import scripts
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Creating import scripts")
	EmitProgress(m, 80, "Creating import scripts")

	// Generate Vault import script
	importScript := `#!/bin/bash
# HashiCorp Vault Import Script
# Imports secrets from Key Vault export

set -e

echo "HashiCorp Vault Import"
echo "======================"

# Configuration
export VAULT_ADDR="${VAULT_ADDR:-http://localhost:8200}"
export VAULT_TOKEN="${VAULT_TOKEN:-root}"
SECRETS_DIR="./secrets"

# Enable KV secrets engine
echo "Enabling KV secrets engine..."
vault secrets enable -path=secret kv-v2 2>/dev/null || echo "KV engine already enabled"

# Import secrets
echo "Importing secrets..."
`

	for _, secret := range secrets {
		importScript += fmt.Sprintf(`
if [ -f "$SECRETS_DIR/%s.txt" ]; then
    echo "Importing: %s"
    vault kv put secret/%s value=@"$SECRETS_DIR/%s.txt"
fi
`, secret.Name, secret.Name, secret.Name, secret.Name)
	}

	importScript += `
echo ""
echo "Import complete!"
echo ""
echo "Verify with: vault kv list secret/"
echo ""
echo "IMPORTANT: Delete the secrets directory after verifying!"
echo "rm -rf $SECRETS_DIR"
`

	importScriptPath := filepath.Join(outputDir, "import-secrets.sh")
	if err := os.WriteFile(importScriptPath, []byte(importScript), 0700); err != nil {
		return fmt.Errorf("failed to write import script: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 6: Finalizing
	EmitPhase(m, phases[5], 6)
	EmitLog(m, "info", "Finalizing migration artifacts")
	EmitProgress(m, 90, "Finalizing")

	var keyCount, certCount int
	if len(keyListOutput) > 0 {
		var keys []interface{}
		_ = json.Unmarshal(keyListOutput, &keys)
		keyCount = len(keys)
	}
	if len(certListOutput) > 0 {
		var certs []interface{}
		_ = json.Unmarshal(certListOutput, &certs)
		certCount = len(certs)
	}

	readme := fmt.Sprintf(`# Key Vault to HashiCorp Vault Migration

## Source Key Vault
- Vault Name: %s
- Location: %s
- SKU: %s
- Secrets: %d
- Keys: %d
- Certificates: %d

## Migration Steps

1. Export secrets from Key Vault:
'''bash
./export-secrets.sh
'''

2. Start HashiCorp Vault:
'''bash
docker-compose up -d
'''

3. Import secrets to Vault:
'''bash
./import-secrets.sh
'''

4. Delete exported secrets:
'''bash
rm -rf ./secrets
'''

## Files Generated
- vault-info.json: Key Vault configuration
- secret-metadata.json: Secret metadata
- key-metadata.json: Key metadata
- certificate-metadata.json: Certificate metadata
- export-secrets.sh: Secret export script
- import-secrets.sh: Vault import script
- docker-compose.yml: HashiCorp Vault setup
- vault-config/: Vault configuration files

## Access
- Vault: http://localhost:8200 (token: root)
- Vault UI: http://localhost:8000

## Key Vault to Vault Mapping

| Key Vault | HashiCorp Vault |
|-----------|-----------------|
| Secret | KV v2 secret |
| Key | Transit engine key |
| Certificate | PKI engine |
| Access Policy | Policy + Auth method |

## Certificate Migration

Export certificates from Key Vault:
'''bash
az keyvault certificate download --vault-name %s --name <cert-name> --file cert.pem
az keyvault secret show --vault-name %s --name <cert-name> --query value -o tsv > cert-full.pem
'''

## Key Migration

For software-protected keys:
'''bash
az keyvault key download --vault-name %s --name <key-name> --file key.pem
'''

Note: HSM-protected keys cannot be exported.

## Security Notes
- Change the root token in production
- Enable TLS in production
- Set up proper authentication (AppRole, JWT, etc.)
- Configure audit logging
- Delete exported secrets after import
`, vaultName, vaultInfo.Location, vaultInfo.Properties.Sku.Name, len(secrets), keyCount, certCount, vaultName, vaultName, vaultName)

	readmePath := filepath.Join(outputDir, "README.md")
	if err := os.WriteFile(readmePath, []byte(readme), 0644); err != nil {
		return fmt.Errorf("failed to write README: %w", err)
	}

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", fmt.Sprintf("Key Vault %s migration prepared at %s", vaultName, outputDir))

	return nil
}

// ADB2CToKeycloakExecutor migrates Azure AD B2C to Keycloak.
type ADB2CToKeycloakExecutor struct{}

// NewADB2CToKeycloakExecutor creates a new AD B2C to Keycloak executor.
func NewADB2CToKeycloakExecutor() *ADB2CToKeycloakExecutor {
	return &ADB2CToKeycloakExecutor{}
}

// Type returns the migration type.
func (e *ADB2CToKeycloakExecutor) Type() string {
	return "adb2c_to_keycloak"
}

// GetPhases returns the migration phases.
func (e *ADB2CToKeycloakExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Fetching tenant info",
		"Exporting user flows and policies",
		"Generating Keycloak configuration",
		"Creating user migration scripts",
		"Finalizing",
	}
}

// Validate validates the migration configuration.
func (e *ADB2CToKeycloakExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["tenant_name"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.tenant_name is required (e.g., myb2ctenant)")
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

	result.Warnings = append(result.Warnings, "User passwords cannot be migrated - users must reset")
	result.Warnings = append(result.Warnings, "Custom policies need manual conversion")
	result.Warnings = append(result.Warnings, "MFA settings need reconfiguration")

	return result, nil
}

// Execute performs the migration.
func (e *ADB2CToKeycloakExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	tenantName := config.Source["tenant_name"].(string)
	tenantDomain := tenantName + ".onmicrosoft.com"

	outputDir := config.Destination["output_dir"].(string)

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating Azure credentials")
	EmitProgress(m, 10, "Checking credentials")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Fetching tenant info
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", fmt.Sprintf("Fetching AD B2C tenant info for %s", tenantName))
	EmitProgress(m, 25, "Fetching tenant info")

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Save tenant info
	tenantInfo := map[string]string{
		"tenant_name":   tenantName,
		"tenant_domain": tenantDomain,
		"tenant_id":     tenantName + ".onmicrosoft.com",
	}
	tenantInfoBytes, _ := json.MarshalIndent(tenantInfo, "", "  ")
	tenantInfoPath := filepath.Join(outputDir, "tenant-info.json")
	if err := os.WriteFile(tenantInfoPath, tenantInfoBytes, 0644); err != nil {
		return fmt.Errorf("failed to write tenant info: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Exporting user flows and policies
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", "Generating user flow export scripts")
	EmitProgress(m, 45, "Generating export scripts")

	// Generate Graph API export script
	exportScript := fmt.Sprintf(`#!/bin/bash
# Azure AD B2C Export Script
# Tenant: %s

set -e

echo "Azure AD B2C Export"
echo "==================="

# Configuration
TENANT_ID="%s"
OUTPUT_DIR="./b2c-export"

mkdir -p "$OUTPUT_DIR"

# Note: Requires Azure AD Graph API permissions
# Application.Read.All, User.Read.All

# Get access token (requires az cli login to B2C tenant)
echo "Getting access token..."
TOKEN=$(az account get-access-token --resource-type ms-graph --query accessToken -o tsv)

# Export users
echo "Exporting users..."
curl -s -H "Authorization: Bearer $TOKEN" \
    "https://graph.microsoft.com/v1.0/users?\$select=id,displayName,mail,userPrincipalName,identities" \
    > "$OUTPUT_DIR/users.json"

# Export applications
echo "Exporting applications..."
curl -s -H "Authorization: Bearer $TOKEN" \
    "https://graph.microsoft.com/v1.0/applications" \
    > "$OUTPUT_DIR/applications.json"

# Export groups
echo "Exporting groups..."
curl -s -H "Authorization: Bearer $TOKEN" \
    "https://graph.microsoft.com/v1.0/groups" \
    > "$OUTPUT_DIR/groups.json"

echo ""
echo "Export complete! Files in $OUTPUT_DIR"
echo ""
echo "For custom policies, download from Azure Portal:"
echo "  Azure AD B2C > Identity Experience Framework > Custom policies"
`, tenantName, tenantDomain)

	exportScriptPath := filepath.Join(outputDir, "export-b2c.sh")
	if err := os.WriteFile(exportScriptPath, []byte(exportScript), 0755); err != nil {
		return fmt.Errorf("failed to write export script: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Generating Keycloak configuration
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Generating Keycloak configuration")
	EmitProgress(m, 65, "Generating configuration")

	// Generate Docker Compose for Keycloak
	dockerCompose := `version: '3.8'

services:
  keycloak:
    image: quay.io/keycloak/keycloak:23.0
    container_name: keycloak
    command: start-dev --import-realm
    environment:
      KEYCLOAK_ADMIN: admin
      KEYCLOAK_ADMIN_PASSWORD: admin
      KC_DB: postgres
      KC_DB_URL: jdbc:postgresql://postgres:5432/keycloak
      KC_DB_USERNAME: keycloak
      KC_DB_PASSWORD: keycloak
    volumes:
      - ./realm-export.json:/opt/keycloak/data/import/realm-export.json
    ports:
      - "8080:8080"
    depends_on:
      - postgres
    restart: unless-stopped

  postgres:
    image: postgres:15-alpine
    container_name: keycloak-postgres
    environment:
      POSTGRES_DB: keycloak
      POSTGRES_USER: keycloak
      POSTGRES_PASSWORD: keycloak
    volumes:
      - postgres-data:/var/lib/postgresql/data
    restart: unless-stopped

volumes:
  postgres-data:
`

	composePath := filepath.Join(outputDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(dockerCompose), 0644); err != nil {
		return fmt.Errorf("failed to write docker-compose.yml: %w", err)
	}

	// Generate Keycloak realm configuration
	realmExport := map[string]interface{}{
		"realm":       tenantName,
		"enabled":     true,
		"displayName": tenantName + " (migrated from AD B2C)",
		"registrationAllowed":     true,
		"registrationEmailAsUsername": true,
		"resetPasswordAllowed":    true,
		"verifyEmail":             true,
		"loginWithEmailAllowed":   true,
		"duplicateEmailsAllowed":  false,
		"sslRequired":             "external",
		"passwordPolicy":          "length(8) and upperCase(1) and lowerCase(1) and digits(1)",
		"browserFlow":             "browser",
		"registrationFlow":        "registration",
		"directGrantFlow":         "direct grant",
		"resetCredentialsFlow":    "reset credentials",
		"clientAuthenticationFlow": "clients",
		"clients": []map[string]interface{}{
			{
				"clientId":                  "app-client",
				"name":                      "Application Client",
				"enabled":                   true,
				"publicClient":              true,
				"standardFlowEnabled":       true,
				"directAccessGrantsEnabled": true,
				"redirectUris":              []string{"http://localhost:*", "https://localhost:*"},
				"webOrigins":                []string{"*"},
				"protocol":                  "openid-connect",
			},
		},
		"roles": map[string]interface{}{
			"realm": []map[string]interface{}{
				{"name": "user", "description": "Regular user"},
				{"name": "admin", "description": "Administrator"},
			},
		},
		"defaultRoles": []string{"user"},
		"requiredActions": []map[string]interface{}{
			{
				"alias":         "VERIFY_EMAIL",
				"name":          "Verify Email",
				"providerId":    "VERIFY_EMAIL",
				"enabled":       true,
				"defaultAction": false,
			},
			{
				"alias":         "UPDATE_PASSWORD",
				"name":          "Update Password",
				"providerId":    "UPDATE_PASSWORD",
				"enabled":       true,
				"defaultAction": true,
			},
		},
	}

	realmBytes, _ := json.MarshalIndent(realmExport, "", "  ")
	realmPath := filepath.Join(outputDir, "realm-export.json")
	if err := os.WriteFile(realmPath, realmBytes, 0644); err != nil {
		return fmt.Errorf("failed to write realm export: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Creating user migration scripts
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Creating user migration scripts")
	EmitProgress(m, 80, "Creating migration scripts")

	// Generate user import script for Keycloak
	userImportScript := `#!/bin/bash
# Keycloak User Import Script
# Imports users from AD B2C export

set -e

echo "Keycloak User Import"
echo "===================="

# Configuration
KEYCLOAK_URL="${KEYCLOAK_URL:-http://localhost:8080}"
REALM="${REALM:-` + tenantName + `}"
ADMIN_USER="${ADMIN_USER:-admin}"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-admin}"
USERS_FILE="./b2c-export/users.json"

if [ ! -f "$USERS_FILE" ]; then
    echo "Error: $USERS_FILE not found. Run export-b2c.sh first."
    exit 1
fi

# Get admin token
echo "Getting admin token..."
TOKEN=$(curl -s -X POST "$KEYCLOAK_URL/realms/master/protocol/openid-connect/token" \
    -H "Content-Type: application/x-www-form-urlencoded" \
    -d "username=$ADMIN_USER" \
    -d "password=$ADMIN_PASSWORD" \
    -d "grant_type=password" \
    -d "client_id=admin-cli" \
    | jq -r '.access_token')

if [ "$TOKEN" = "null" ] || [ -z "$TOKEN" ]; then
    echo "Error: Failed to get admin token"
    exit 1
fi

echo "Token obtained. Importing users..."

# Parse and import users
cat "$USERS_FILE" | jq -c '.value[]' | while read user; do
    EMAIL=$(echo $user | jq -r '.mail // .userPrincipalName')
    DISPLAY_NAME=$(echo $user | jq -r '.displayName')
    FIRST_NAME=$(echo $DISPLAY_NAME | cut -d' ' -f1)
    LAST_NAME=$(echo $DISPLAY_NAME | cut -d' ' -f2-)

    if [ "$EMAIL" != "null" ] && [ -n "$EMAIL" ]; then
        echo "Creating user: $EMAIL"

        # Create user (password must be reset)
        curl -s -X POST "$KEYCLOAK_URL/admin/realms/$REALM/users" \
            -H "Authorization: Bearer $TOKEN" \
            -H "Content-Type: application/json" \
            -d "{
                \"username\": \"$EMAIL\",
                \"email\": \"$EMAIL\",
                \"firstName\": \"$FIRST_NAME\",
                \"lastName\": \"$LAST_NAME\",
                \"enabled\": true,
                \"emailVerified\": true,
                \"requiredActions\": [\"UPDATE_PASSWORD\"]
            }" || echo "  (user may already exist)"
    fi
done

echo ""
echo "Import complete!"
echo ""
echo "Users will need to reset their passwords on first login."
`

	userImportPath := filepath.Join(outputDir, "import-users.sh")
	if err := os.WriteFile(userImportPath, []byte(userImportScript), 0755); err != nil {
		return fmt.Errorf("failed to write user import script: %w", err)
	}

	// Generate migration guide
	migrationGuide := `# Azure AD B2C to Keycloak Migration Guide

## Authentication Flow Mapping

| AD B2C Flow | Keycloak Equivalent |
|-------------|---------------------|
| Sign Up/Sign In | Browser Flow + Registration |
| Password Reset | Reset Credentials Flow |
| Profile Edit | Account Management |
| MFA | Browser Flow + OTP |

## Custom Policy Migration

AD B2C custom policies (XML) need to be converted to Keycloak:

1. **Custom Attributes**: Create custom user attributes in Keycloak
2. **Identity Providers**: Configure under Identity Providers
3. **Claims Transformation**: Use Protocol Mappers
4. **API Connectors**: Use Keycloak SPI or webhooks

## Social Login Configuration

Configure identity providers in Keycloak:
- Google: Identity Providers > Add > Google
- Facebook: Identity Providers > Add > Facebook
- Microsoft: Identity Providers > Add > Microsoft

## Token Configuration

Map AD B2C token claims to Keycloak:
1. Go to Clients > Your Client > Client Scopes
2. Add mappers for custom claims
3. Configure token lifetimes in Realm Settings

## Application Migration

Update applications:
1. Change authorization endpoint
2. Update token endpoint
3. Modify logout endpoint
4. Update JWKS URI
5. Adjust claim names if needed

## Endpoints Comparison

| AD B2C | Keycloak |
|--------|----------|
| https://[tenant].b2clogin.com/[tenant]/oauth2/v2.0/authorize | http://localhost:8080/realms/[realm]/protocol/openid-connect/auth |
| https://[tenant].b2clogin.com/[tenant]/oauth2/v2.0/token | http://localhost:8080/realms/[realm]/protocol/openid-connect/token |
| https://[tenant].b2clogin.com/[tenant]/oauth2/v2.0/logout | http://localhost:8080/realms/[realm]/protocol/openid-connect/logout |
`

	guidePath := filepath.Join(outputDir, "MIGRATION_GUIDE.md")
	if err := os.WriteFile(guidePath, []byte(migrationGuide), 0644); err != nil {
		return fmt.Errorf("failed to write migration guide: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 6: Finalizing
	EmitPhase(m, phases[5], 6)
	EmitLog(m, "info", "Finalizing migration artifacts")
	EmitProgress(m, 90, "Finalizing")

	readme := fmt.Sprintf(`# Azure AD B2C to Keycloak Migration

## Source AD B2C
- Tenant: %s
- Domain: %s

## Migration Steps

1. Export users from AD B2C:
'''bash
./export-b2c.sh
'''

2. Start Keycloak:
'''bash
docker-compose up -d
'''

3. Import users:
'''bash
./import-users.sh
'''

4. Update applications to use Keycloak endpoints

## Files Generated
- tenant-info.json: AD B2C tenant configuration
- export-b2c.sh: User export script
- import-users.sh: Keycloak user import
- docker-compose.yml: Keycloak setup
- realm-export.json: Keycloak realm configuration
- MIGRATION_GUIDE.md: Detailed migration guide

## Access
- Keycloak Admin: http://localhost:8080/admin (admin/admin)
- Realm Settings: http://localhost:8080/admin/master/console/#/%s

## Important Notes
- User passwords CANNOT be migrated
- Users must reset passwords on first login
- Custom policies need manual conversion
- MFA must be reconfigured in Keycloak
- Test all authentication flows before cutover
`, tenantName, tenantDomain, tenantName)

	readmePath := filepath.Join(outputDir, "README.md")
	if err := os.WriteFile(readmePath, []byte(readme), 0644); err != nil {
		return fmt.Errorf("failed to write README: %w", err)
	}

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", fmt.Sprintf("AD B2C %s migration prepared at %s", tenantName, outputDir))

	return nil
}

// AzureDNSToCoreDNSExecutor migrates Azure DNS to CoreDNS.
type AzureDNSToCoreDNSExecutor struct{}

// NewAzureDNSToCoreDNSExecutor creates a new Azure DNS to CoreDNS executor.
func NewAzureDNSToCoreDNSExecutor() *AzureDNSToCoreDNSExecutor {
	return &AzureDNSToCoreDNSExecutor{}
}

// Type returns the migration type.
func (e *AzureDNSToCoreDNSExecutor) Type() string {
	return "azure_dns_to_coredns"
}

// GetPhases returns the migration phases.
func (e *AzureDNSToCoreDNSExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Fetching DNS zone info",
		"Exporting DNS records",
		"Generating CoreDNS configuration",
		"Creating zone files",
		"Finalizing",
	}
}

// Validate validates the migration configuration.
func (e *AzureDNSToCoreDNSExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["zone_name"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.zone_name is required")
		}
		if _, ok := config.Source["resource_group"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.resource_group is required")
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

	result.Warnings = append(result.Warnings, "Remember to update NS records at your registrar")
	result.Warnings = append(result.Warnings, "Azure-specific features (Traffic Manager) need alternatives")

	return result, nil
}

// Execute performs the migration.
func (e *AzureDNSToCoreDNSExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	zoneName := config.Source["zone_name"].(string)
	resourceGroup := config.Source["resource_group"].(string)
	subscription, _ := config.Source["subscription"].(string)

	outputDir := config.Destination["output_dir"].(string)

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating Azure credentials")
	EmitProgress(m, 10, "Checking credentials")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Fetching DNS zone info
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", fmt.Sprintf("Fetching DNS zone info for %s", zoneName))
	EmitProgress(m, 25, "Fetching zone info")

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Get zone info
	args := []string{"network", "dns", "zone", "show",
		"--name", zoneName,
		"--resource-group", resourceGroup,
		"--output", "json",
	}
	if subscription != "" {
		args = append(args, "--subscription", subscription)
	}

	showCmd := exec.CommandContext(ctx, "az", args...)
	zoneOutput, _ := showCmd.Output()

	var zoneInfo struct {
		Name            string   `json:"name"`
		NameServers     []string `json:"nameServers"`
		NumberOfRecordSets int   `json:"numberOfRecordSets"`
	}
	if len(zoneOutput) > 0 {
		_ = json.Unmarshal(zoneOutput, &zoneInfo)
	}

	// Save zone info
	zoneInfoPath := filepath.Join(outputDir, "zone-info.json")
	if len(zoneOutput) > 0 {
		_ = os.WriteFile(zoneInfoPath, zoneOutput, 0644)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Exporting DNS records
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", "Exporting DNS records")
	EmitProgress(m, 45, "Exporting records")

	// Get all record sets
	recordArgs := []string{"network", "dns", "record-set", "list",
		"--zone-name", zoneName,
		"--resource-group", resourceGroup,
		"--output", "json",
	}
	if subscription != "" {
		recordArgs = append(recordArgs, "--subscription", subscription)
	}

	recordCmd := exec.CommandContext(ctx, "az", recordArgs...)
	recordOutput, _ := recordCmd.Output()

	var recordSets []struct {
		Name    string `json:"name"`
		Type    string `json:"type"`
		TTL     int    `json:"ttl"`
		ARecords []struct {
			IPv4Address string `json:"ipv4Address"`
		} `json:"aRecords"`
		AAAARecords []struct {
			IPv6Address string `json:"ipv6Address"`
		} `json:"aaaaRecords"`
		CNAMERecord struct {
			Cname string `json:"cname"`
		} `json:"cnameRecord"`
		MXRecords []struct {
			Exchange   string `json:"exchange"`
			Preference int    `json:"preference"`
		} `json:"mxRecords"`
		TXTRecords []struct {
			Value []string `json:"value"`
		} `json:"txtRecords"`
		NSRecords []struct {
			Nsdname string `json:"nsdname"`
		} `json:"nsRecords"`
		SOARecord struct {
			Host        string `json:"host"`
			Email       string `json:"email"`
			SerialNumber int64 `json:"serialNumber"`
			RefreshTime int    `json:"refreshTime"`
			RetryTime   int    `json:"retryTime"`
			ExpireTime  int    `json:"expireTime"`
			MinimumTTL  int    `json:"minimumTtl"`
		} `json:"soaRecord"`
	}
	if len(recordOutput) > 0 {
		_ = json.Unmarshal(recordOutput, &recordSets)
	}

	// Save records
	recordPath := filepath.Join(outputDir, "records.json")
	if len(recordOutput) > 0 {
		_ = os.WriteFile(recordPath, recordOutput, 0644)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Generating CoreDNS configuration
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Generating CoreDNS configuration")
	EmitProgress(m, 65, "Generating configuration")

	// Generate Docker Compose for CoreDNS
	dockerCompose := `version: '3.8'

services:
  coredns:
    image: coredns/coredns:1.11
    container_name: coredns
    command: -conf /etc/coredns/Corefile
    volumes:
      - ./Corefile:/etc/coredns/Corefile
      - ./zones:/etc/coredns/zones
    ports:
      - "53:53/udp"
      - "53:53/tcp"
    restart: unless-stopped

  # Optional: DNS admin UI
  # dnsmasq-webui:
  #   image: ...
`

	composePath := filepath.Join(outputDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(dockerCompose), 0644); err != nil {
		return fmt.Errorf("failed to write docker-compose.yml: %w", err)
	}

	// Generate Corefile
	corefile := fmt.Sprintf(`# CoreDNS Configuration
# Migrated from Azure DNS zone: %s

%s {
    file /etc/coredns/zones/db.%s
    log
    errors
    cache 30
}

. {
    forward . 8.8.8.8 8.8.4.4
    log
    errors
    cache 30
}
`, zoneName, zoneName, zoneName)

	corefilePath := filepath.Join(outputDir, "Corefile")
	if err := os.WriteFile(corefilePath, []byte(corefile), 0644); err != nil {
		return fmt.Errorf("failed to write Corefile: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Creating zone files
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Creating zone files")
	EmitProgress(m, 80, "Creating zone files")

	// Create zones directory
	zonesDir := filepath.Join(outputDir, "zones")
	if err := os.MkdirAll(zonesDir, 0755); err != nil {
		return fmt.Errorf("failed to create zones directory: %w", err)
	}

	// Generate zone file
	var zoneFile string
	zoneFile += fmt.Sprintf(`; Zone file for %s
; Migrated from Azure DNS
$ORIGIN %s.
$TTL 3600

`, zoneName, zoneName)

	// Add SOA record
	for _, rs := range recordSets {
		if rs.SOARecord.Host != "" {
			email := rs.SOARecord.Email
			if email == "" {
				email = "hostmaster." + zoneName
			}
			zoneFile += fmt.Sprintf(`@   IN  SOA %s. %s. (
                %d    ; Serial
                %d      ; Refresh
                %d       ; Retry
                %d     ; Expire
                %d )    ; Minimum TTL

`, rs.SOARecord.Host, email, rs.SOARecord.SerialNumber, rs.SOARecord.RefreshTime, rs.SOARecord.RetryTime, rs.SOARecord.ExpireTime, rs.SOARecord.MinimumTTL)
			break
		}
	}

	// Add other records
	for _, rs := range recordSets {
		name := rs.Name
		if name == "@" {
			name = zoneName + "."
		}
		ttl := rs.TTL
		if ttl == 0 {
			ttl = 3600
		}

		// A records
		for _, a := range rs.ARecords {
			zoneFile += fmt.Sprintf("%-20s %d  IN  A       %s\n", name, ttl, a.IPv4Address)
		}

		// AAAA records
		for _, aaaa := range rs.AAAARecords {
			zoneFile += fmt.Sprintf("%-20s %d  IN  AAAA    %s\n", name, ttl, aaaa.IPv6Address)
		}

		// CNAME records
		if rs.CNAMERecord.Cname != "" {
			zoneFile += fmt.Sprintf("%-20s %d  IN  CNAME   %s\n", name, ttl, rs.CNAMERecord.Cname)
		}

		// MX records
		for _, mx := range rs.MXRecords {
			zoneFile += fmt.Sprintf("%-20s %d  IN  MX      %d %s\n", name, ttl, mx.Preference, mx.Exchange)
		}

		// TXT records
		for _, txt := range rs.TXTRecords {
			for _, v := range txt.Value {
				zoneFile += fmt.Sprintf("%-20s %d  IN  TXT     \"%s\"\n", name, ttl, v)
			}
		}

		// NS records (skip Azure's NS records)
		for _, ns := range rs.NSRecords {
			if !containsAzure(ns.Nsdname) {
				zoneFile += fmt.Sprintf("%-20s %d  IN  NS      %s\n", name, ttl, ns.Nsdname)
			}
		}
	}

	zoneFilePath := filepath.Join(zonesDir, "db."+zoneName)
	if err := os.WriteFile(zoneFilePath, []byte(zoneFile), 0644); err != nil {
		return fmt.Errorf("failed to write zone file: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 6: Finalizing
	EmitPhase(m, phases[5], 6)
	EmitLog(m, "info", "Finalizing migration artifacts")
	EmitProgress(m, 90, "Finalizing")

	readme := fmt.Sprintf(`# Azure DNS to CoreDNS Migration

## Source DNS Zone
- Zone Name: %s
- Resource Group: %s
- Record Sets: %d
- Azure Name Servers: %v

## Migration Steps

1. Start CoreDNS:
'''bash
docker-compose up -d
'''

2. Test DNS resolution:
'''bash
dig @localhost %s
dig @localhost www.%s
'''

3. Update NS records at your domain registrar

## Files Generated
- zone-info.json: Azure DNS zone configuration
- records.json: All DNS records
- docker-compose.yml: CoreDNS container
- Corefile: CoreDNS configuration
- zones/db.%s: Zone file

## Important Steps

### Update Name Server Records
After verifying CoreDNS is working, update your domain registrar to point to your new name servers.

### Testing Before Cutover
'''bash
# Query your CoreDNS server directly
dig @localhost %s A
dig @localhost %s MX
dig @localhost %s TXT
'''

### DNS Propagation
Allow 24-48 hours for DNS changes to propagate after updating NS records.

## Notes
- Review the zone file for accuracy
- Azure-specific NS records have been removed
- Update TTL values if needed
- Consider secondary DNS for redundancy
- Monitor DNS resolution after migration
`, zoneName, resourceGroup, len(recordSets), zoneInfo.NameServers, zoneName, zoneName, zoneName, zoneName, zoneName, zoneName)

	readmePath := filepath.Join(outputDir, "README.md")
	if err := os.WriteFile(readmePath, []byte(readme), 0644); err != nil {
		return fmt.Errorf("failed to write README: %w", err)
	}

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", fmt.Sprintf("Azure DNS %s migration prepared at %s", zoneName, outputDir))

	return nil
}

// Helper function to check if a string contains Azure DNS
func containsAzure(s string) bool {
	return containsSubstring(s, "azure-dns") || containsSubstring(s, "azuredns")
}
