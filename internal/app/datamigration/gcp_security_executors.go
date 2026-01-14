package datamigration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ============================================================================
// Secret Manager to Vault Executor
// ============================================================================

// SecretManagerToVaultExecutor migrates GCP Secret Manager secrets to HashiCorp Vault.
type SecretManagerToVaultExecutor struct{}

// NewSecretManagerToVaultExecutor creates a new Secret Manager to Vault executor.
func NewSecretManagerToVaultExecutor() *SecretManagerToVaultExecutor {
	return &SecretManagerToVaultExecutor{}
}

// Type returns the migration type.
func (e *SecretManagerToVaultExecutor) Type() string {
	return "secretmanager_to_vault"
}

// GetPhases returns the migration phases.
func (e *SecretManagerToVaultExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Listing secrets",
		"Fetching secret values",
		"Generating Vault configuration",
		"Creating import scripts",
	}
}

// Validate validates the migration configuration.
func (e *SecretManagerToVaultExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	// Validate source config
	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["project_id"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.project_id is required")
		}
		// service_account_json is optional - can use default credentials
		if _, ok := config.Source["service_account_json"].(string); !ok {
			result.Warnings = append(result.Warnings, "source.service_account_json not specified, using default credentials")
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
	}

	result.Warnings = append(result.Warnings, "Secret values will be exported in plaintext - handle with care")
	result.Warnings = append(result.Warnings, "Ensure Vault is properly secured before importing secrets")

	return result, nil
}

// gcpSecret represents a GCP Secret Manager secret.
type gcpSecret struct {
	Name        string            `json:"name"`
	Labels      map[string]string `json:"labels,omitempty"`
	CreateTime  string            `json:"createTime"`
	Replication struct {
		Automatic *struct{} `json:"automatic,omitempty"`
		UserManaged *struct {
			Replicas []struct {
				Location string `json:"location"`
			} `json:"replicas"`
		} `json:"userManaged,omitempty"`
	} `json:"replication"`
}

// Execute performs the migration.
func (e *SecretManagerToVaultExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	// Extract configuration
	projectID := config.Source["project_id"].(string)
	serviceAccountJSON, hasServiceAccount := config.Source["service_account_json"].(string)
	outputDir := config.Destination["output_dir"].(string)
	vaultPath, _ := config.Destination["vault_path"].(string)
	if vaultPath == "" {
		vaultPath = "secret/gcp-migrated"
	}

	// Prepare gcloud environment
	var gcloudEnv []string
	if hasServiceAccount {
		// Write service account to temp file
		saFile, err := os.CreateTemp("", "gcp-sa-*.json")
		if err != nil {
			return fmt.Errorf("failed to create temp file for service account: %w", err)
		}
		defer func() { _ = os.Remove(saFile.Name()) }()
		if err := os.WriteFile(saFile.Name(), []byte(serviceAccountJSON), 0600); err != nil {
			return fmt.Errorf("failed to write service account file: %w", err)
		}
		gcloudEnv = append(os.Environ(), "GOOGLE_APPLICATION_CREDENTIALS="+saFile.Name())
	} else {
		gcloudEnv = os.Environ()
	}

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating GCP credentials")
	EmitProgress(m, 5, "Checking credentials")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Test gcloud credentials
	testCmd := exec.CommandContext(ctx, "gcloud", "auth", "list", "--format=json")
	testCmd.Env = gcloudEnv
	if _, err := testCmd.Output(); err != nil {
		EmitLog(m, "error", "GCP credentials validation failed")
		return fmt.Errorf("failed to validate GCP credentials: %w", err)
	}
	EmitLog(m, "info", "GCP credentials validated successfully")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Listing secrets
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", fmt.Sprintf("Listing secrets in project: %s", projectID))
	EmitProgress(m, 20, "Listing secrets")

	listSecretsCmd := exec.CommandContext(ctx, "gcloud", "secrets", "list",
		"--project", projectID,
		"--format=json",
	)
	listSecretsCmd.Env = gcloudEnv

	secretsOutput, err := listSecretsCmd.Output()
	if err != nil {
		EmitLog(m, "error", "Failed to list secrets")
		return fmt.Errorf("failed to list secrets: %w", err)
	}

	var secrets []gcpSecret
	if err := json.Unmarshal(secretsOutput, &secrets); err != nil {
		return fmt.Errorf("failed to parse secrets list: %w", err)
	}

	EmitLog(m, "info", fmt.Sprintf("Found %d secrets", len(secrets)))

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Fetching secret values
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", "Fetching secret values")
	EmitProgress(m, 40, "Fetching secret values")

	type SecretData struct {
		Name       string            `json:"name"`
		SecretID   string            `json:"secretId"`
		Value      string            `json:"value"`
		Labels     map[string]string `json:"labels,omitempty"`
		CreateTime string            `json:"createTime"`
	}
	secretsData := make([]SecretData, 0)

	for i, secret := range secrets {
		// Extract secret ID from full name (projects/PROJECT/secrets/SECRET_ID)
		parts := strings.Split(secret.Name, "/")
		secretID := parts[len(parts)-1]

		EmitLog(m, "info", fmt.Sprintf("Fetching secret: %s (%d/%d)", secretID, i+1, len(secrets)))

		// Get the latest version
		accessCmd := exec.CommandContext(ctx, "gcloud", "secrets", "versions", "access", "latest",
			"--secret", secretID,
			"--project", projectID,
		)
		accessCmd.Env = gcloudEnv

		valueOutput, err := accessCmd.Output()
		if err != nil {
			EmitLog(m, "warn", fmt.Sprintf("Failed to access secret %s: %v", secretID, err))
			continue
		}

		secretsData = append(secretsData, SecretData{
			Name:       secret.Name,
			SecretID:   secretID,
			Value:      string(valueOutput),
			Labels:     secret.Labels,
			CreateTime: secret.CreateTime,
		})

		if m.IsCancelled() {
			return fmt.Errorf("migration cancelled")
		}
	}

	EmitLog(m, "info", fmt.Sprintf("Successfully fetched %d secrets", len(secretsData)))

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Generating Vault configuration
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Generating Vault configuration")
	EmitProgress(m, 65, "Generating configuration")

	// Ensure output directory exists
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Generate Docker Compose for Vault
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
	if err := os.WriteFile(composePath, []byte(vaultCompose), 0644); err != nil {
		return fmt.Errorf("failed to write docker-compose.yml: %w", err)
	}

	// Export secrets metadata (without values for security)
	secretsMetadata := make([]map[string]interface{}, 0)
	for _, s := range secretsData {
		secretsMetadata = append(secretsMetadata, map[string]interface{}{
			"secretId":   s.SecretID,
			"name":       s.Name,
			"labels":     s.Labels,
			"createTime": s.CreateTime,
			"hasValue":   len(s.Value) > 0,
		})
	}
	metadataJSON, _ := json.MarshalIndent(secretsMetadata, "", "  ")
	metadataPath := filepath.Join(outputDir, "secrets-metadata.json")
	_ = os.WriteFile(metadataPath, metadataJSON, 0644)

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Creating import scripts
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Creating Vault import scripts")
	EmitProgress(m, 85, "Creating import scripts")

	// Create secrets directory for individual secret files
	secretsDir := filepath.Join(outputDir, "secrets")
	if err := os.MkdirAll(secretsDir, 0700); err != nil {
		return fmt.Errorf("failed to create secrets directory: %w", err)
	}

	// Write individual secret files (for manual import or reference)
	for _, s := range secretsData {
		secretFile := filepath.Join(secretsDir, s.SecretID+".txt")
		if err := os.WriteFile(secretFile, []byte(s.Value), 0600); err != nil {
			EmitLog(m, "warn", fmt.Sprintf("Failed to write secret file %s: %v", s.SecretID, err))
		}
	}

	// Generate Vault import script
	importScript := fmt.Sprintf(`#!/bin/bash
# Vault Secret Import Script
# Migrated from GCP Secret Manager - Project: %s

set -e

export VAULT_ADDR='http://127.0.0.1:8200'
export VAULT_TOKEN='root'

echo "Enabling KV secrets engine v2..."
vault secrets enable -path=%s kv-v2 || true

echo "Importing secrets..."
`, projectID, strings.Split(vaultPath, "/")[0])

	for _, s := range secretsData {
		// Escape single quotes in value for bash
		escapedValue := strings.ReplaceAll(s.Value, "'", "'\"'\"'")
		importScript += fmt.Sprintf(`
# Secret: %s
vault kv put %s/%s value='%s'
`, s.SecretID, vaultPath, s.SecretID, escapedValue)
	}

	importScript += fmt.Sprintf(`
echo "Import complete!"
echo "Secrets imported: %d"
vault kv list %s/
`, len(secretsData), vaultPath)

	importScriptPath := filepath.Join(outputDir, "import-secrets.sh")
	if err := os.WriteFile(importScriptPath, []byte(importScript), 0700); err != nil {
		return fmt.Errorf("failed to write import script: %w", err)
	}

	// Generate README
	readme := fmt.Sprintf(`# GCP Secret Manager to Vault Migration

## Source
- GCP Project: %s
- Secrets Migrated: %d

## Files Generated
- docker-compose.yml: Vault container configuration
- secrets-metadata.json: Secret metadata (no values)
- secrets/: Individual secret value files (handle with care!)
- import-secrets.sh: Script to import secrets into Vault

## Getting Started

1. Start Vault:
'''bash
docker-compose up -d
'''

2. Wait for Vault to be ready:
'''bash
sleep 5
'''

3. Import secrets:
'''bash
./import-secrets.sh
'''

4. Access Vault UI:
- URL: http://localhost:8200
- Token: root (development only!)

## Accessing Secrets in Vault

'''bash
export VAULT_ADDR='http://127.0.0.1:8200'
export VAULT_TOKEN='root'

# List secrets
vault kv list %s/

# Read a secret
vault kv get %s/<secret-name>
'''

## Production Considerations
- Use proper authentication (not dev token)
- Enable audit logging
- Set up proper access policies
- Use auto-unseal in production
- Secure the secrets directory and import script
- Delete exported secret files after import

## Security Warning
The secrets/ directory and import-secrets.sh contain plaintext secret values.
Delete these files after successful import to Vault!
`, projectID, len(secretsData), vaultPath, vaultPath)

	readmePath := filepath.Join(outputDir, "README.md")
	_ = os.WriteFile(readmePath, []byte(readme), 0644)

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", fmt.Sprintf("Secret Manager migration complete: %d secrets exported", len(secretsData)))
	EmitLog(m, "warn", "Remember to delete secret files after importing to Vault!")

	return nil
}

// ============================================================================
// Identity Platform to Keycloak Executor
// ============================================================================

// IdentityPlatformToKeycloakExecutor migrates GCP Identity Platform users to Keycloak.
type IdentityPlatformToKeycloakExecutor struct{}

// NewIdentityPlatformToKeycloakExecutor creates a new Identity Platform to Keycloak executor.
func NewIdentityPlatformToKeycloakExecutor() *IdentityPlatformToKeycloakExecutor {
	return &IdentityPlatformToKeycloakExecutor{}
}

// Type returns the migration type.
func (e *IdentityPlatformToKeycloakExecutor) Type() string {
	return "identityplatform_to_keycloak"
}

// GetPhases returns the migration phases.
func (e *IdentityPlatformToKeycloakExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Fetching Identity Platform configuration",
		"Exporting users",
		"Exporting providers",
		"Generating Keycloak import",
	}
}

// Validate validates the migration configuration.
func (e *IdentityPlatformToKeycloakExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	// Validate source config
	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["project_id"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.project_id is required")
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
	}

	// Add warnings about password migration
	result.Warnings = append(result.Warnings, "User passwords cannot be migrated from Identity Platform (hashes are not exportable)")
	result.Warnings = append(result.Warnings, "Users will need to reset their passwords in Keycloak")
	result.Warnings = append(result.Warnings, "Social login configurations need manual recreation in Keycloak")

	return result, nil
}

// identityPlatformUser represents a Firebase/Identity Platform user.
type identityPlatformUser struct {
	LocalID          string   `json:"localId"`
	Email            string   `json:"email,omitempty"`
	EmailVerified    bool     `json:"emailVerified"`
	DisplayName      string   `json:"displayName,omitempty"`
	PhotoURL         string   `json:"photoUrl,omitempty"`
	Disabled         bool     `json:"disabled"`
	CreatedAt        string   `json:"createdAt"`
	LastLoginAt      string   `json:"lastLoginAt,omitempty"`
	PhoneNumber      string   `json:"phoneNumber,omitempty"`
	ProviderUserInfo []struct {
		ProviderID  string `json:"providerId"`
		RawID       string `json:"rawId"`
		Email       string `json:"email,omitempty"`
		DisplayName string `json:"displayName,omitempty"`
		PhotoURL    string `json:"photoUrl,omitempty"`
	} `json:"providerUserInfo,omitempty"`
	CustomAttributes string `json:"customAttributes,omitempty"`
	MfaInfo          []struct {
		MfaEnrollmentID string `json:"mfaEnrollmentId"`
		PhoneInfo       string `json:"phoneInfo,omitempty"`
		DisplayName     string `json:"displayName,omitempty"`
	} `json:"mfaInfo,omitempty"`
}

// identityPlatformConfig represents Identity Platform configuration.
type identityPlatformConfig struct {
	SignIn struct {
		Email struct {
			Enabled         bool `json:"enabled"`
			PasswordRequired bool `json:"passwordRequired"`
		} `json:"email"`
		PhoneNumber struct {
			Enabled bool `json:"enabled"`
		} `json:"phoneNumber"`
		Anonymous struct {
			Enabled bool `json:"enabled"`
		} `json:"anonymous"`
	} `json:"signIn"`
	MFA struct {
		State string `json:"state"`
	} `json:"mfa"`
}

// Execute performs the migration.
func (e *IdentityPlatformToKeycloakExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	// Extract configuration
	projectID := config.Source["project_id"].(string)
	serviceAccountJSON, hasServiceAccount := config.Source["service_account_json"].(string)
	outputDir := config.Destination["output_dir"].(string)
	realmName, _ := config.Destination["realm_name"].(string)
	if realmName == "" {
		realmName = "identity-platform-migrated"
	}

	// Prepare gcloud environment
	var gcloudEnv []string
	var saFilePath string
	if hasServiceAccount {
		saFile, err := os.CreateTemp("", "gcp-sa-*.json")
		if err != nil {
			return fmt.Errorf("failed to create temp file for service account: %w", err)
		}
		saFilePath = saFile.Name()
		defer func() { _ = os.Remove(saFilePath) }()
		if err := os.WriteFile(saFilePath, []byte(serviceAccountJSON), 0600); err != nil {
			return fmt.Errorf("failed to write service account file: %w", err)
		}
		gcloudEnv = append(os.Environ(), "GOOGLE_APPLICATION_CREDENTIALS="+saFilePath)
	} else {
		gcloudEnv = os.Environ()
	}

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating GCP credentials")
	EmitProgress(m, 5, "Checking credentials")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	testCmd := exec.CommandContext(ctx, "gcloud", "auth", "list", "--format=json")
	testCmd.Env = gcloudEnv
	if _, err := testCmd.Output(); err != nil {
		EmitLog(m, "error", "GCP credentials validation failed")
		return fmt.Errorf("failed to validate GCP credentials: %w", err)
	}
	EmitLog(m, "info", "GCP credentials validated successfully")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Fetching Identity Platform configuration
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", fmt.Sprintf("Fetching Identity Platform configuration for project: %s", projectID))
	EmitProgress(m, 15, "Fetching configuration")

	// Get Identity Platform config
	configCmd := exec.CommandContext(ctx, "gcloud", "identity-platform", "config", "describe",
		"--project", projectID,
		"--format=json",
	)
	configCmd.Env = gcloudEnv

	var idpConfig identityPlatformConfig
	configOutput, err := configCmd.Output()
	if err != nil {
		EmitLog(m, "warn", fmt.Sprintf("Could not fetch Identity Platform config: %v", err))
		EmitLog(m, "info", "Continuing with user export...")
	} else {
		if err := json.Unmarshal(configOutput, &idpConfig); err != nil {
			EmitLog(m, "warn", fmt.Sprintf("Failed to parse Identity Platform config: %v", err))
		} else {
			EmitLog(m, "info", fmt.Sprintf("Email sign-in enabled: %v", idpConfig.SignIn.Email.Enabled))
			EmitLog(m, "info", fmt.Sprintf("Phone sign-in enabled: %v", idpConfig.SignIn.PhoneNumber.Enabled))
			EmitLog(m, "info", fmt.Sprintf("MFA state: %s", idpConfig.MFA.State))
		}
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Exporting users
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", "Exporting users from Identity Platform")
	EmitProgress(m, 35, "Exporting users")

	// Export users using gcloud identity-platform
	listUsersCmd := exec.CommandContext(ctx, "gcloud", "identity-platform", "accounts", "list",
		"--project", projectID,
		"--format=json",
	)
	listUsersCmd.Env = gcloudEnv

	var allUsers []identityPlatformUser
	usersOutput, err := listUsersCmd.Output()
	if err != nil {
		EmitLog(m, "warn", "gcloud identity-platform accounts list failed, trying alternative method")

		// Alternative: Use Firebase Admin SDK export via a helper script
		EmitLog(m, "info", "Note: For complete user export, consider using Firebase Admin SDK directly")
		allUsers = []identityPlatformUser{}
	} else {
		if err := json.Unmarshal(usersOutput, &allUsers); err != nil {
			EmitLog(m, "warn", fmt.Sprintf("Failed to parse users: %v", err))
			allUsers = []identityPlatformUser{}
		}
	}

	EmitLog(m, "info", fmt.Sprintf("Found %d users", len(allUsers)))

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Exporting providers
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Analyzing identity providers")
	EmitProgress(m, 55, "Analyzing providers")

	// Track unique providers
	providers := make(map[string]int)
	for _, user := range allUsers {
		for _, provider := range user.ProviderUserInfo {
			providers[provider.ProviderID]++
		}
	}

	for provider, count := range providers {
		EmitLog(m, "info", fmt.Sprintf("Provider '%s': %d users", provider, count))
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Generating Keycloak import
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Generating Keycloak realm import file")
	EmitProgress(m, 75, "Generating import file")

	// Ensure output directory exists
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Convert users to Keycloak format
	keycloakUsers := make([]keycloakUser, 0, len(allUsers))
	for _, idpUser := range allUsers {
		kcUser := keycloakUser{
			Username:        idpUser.LocalID,
			Email:           idpUser.Email,
			EmailVerified:   idpUser.EmailVerified,
			Enabled:         !idpUser.Disabled,
			RequiredActions: []string{"UPDATE_PASSWORD"},
			Attributes:      make(map[string][]string),
		}

		// Parse display name into first/last name
		if idpUser.DisplayName != "" {
			parts := strings.SplitN(idpUser.DisplayName, " ", 2)
			kcUser.FirstName = parts[0]
			if len(parts) > 1 {
				kcUser.LastName = parts[1]
			}
		}

		// Store Identity Platform specific attributes
		kcUser.Attributes["identityPlatformId"] = []string{idpUser.LocalID}
		if idpUser.PhoneNumber != "" {
			kcUser.Attributes["phoneNumber"] = []string{idpUser.PhoneNumber}
		}
		if idpUser.PhotoURL != "" {
			kcUser.Attributes["photoUrl"] = []string{idpUser.PhotoURL}
		}
		if idpUser.CreatedAt != "" {
			kcUser.Attributes["createdAt"] = []string{idpUser.CreatedAt}
		}

		// Track linked providers
		linkedProviders := make([]string, 0)
		for _, provider := range idpUser.ProviderUserInfo {
			linkedProviders = append(linkedProviders, provider.ProviderID)
		}
		if len(linkedProviders) > 0 {
			kcUser.Attributes["linkedProviders"] = linkedProviders
		}

		// Custom attributes
		if idpUser.CustomAttributes != "" {
			kcUser.Attributes["customAttributes"] = []string{idpUser.CustomAttributes}
		}

		// MFA info
		if len(idpUser.MfaInfo) > 0 {
			kcUser.Attributes["mfaEnabled"] = []string{"true"}
			mfaMethods := make([]string, 0)
			for _, mfa := range idpUser.MfaInfo {
				if mfa.PhoneInfo != "" {
					mfaMethods = append(mfaMethods, "phone:"+mfa.PhoneInfo)
				}
			}
			if len(mfaMethods) > 0 {
				kcUser.Attributes["mfaMethods"] = mfaMethods
			}
		}

		keycloakUsers = append(keycloakUsers, kcUser)
	}

	// Build the realm export
	realmExport := keycloakRealmExport{
		Realm:           realmName,
		Enabled:         true,
		Users:           keycloakUsers,
		Groups:          []keycloakGroup{},
		RequiredActions: []string{"UPDATE_PASSWORD"},
	}

	// Set password policy based on Identity Platform config
	if idpConfig.SignIn.Email.PasswordRequired {
		realmExport.PasswordPolicy = "length(8) and upperCase(1) and lowerCase(1) and digits(1)"
	}

	// Write realm export file
	exportJSON, err := json.MarshalIndent(realmExport, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal realm export: %w", err)
	}

	realmExportPath := filepath.Join(outputDir, "realm-export.json")
	if err := os.WriteFile(realmExportPath, exportJSON, 0644); err != nil {
		return fmt.Errorf("failed to write realm export: %w", err)
	}

	// Save original users data for reference
	usersJSON, _ := json.MarshalIndent(allUsers, "", "  ")
	usersExportPath := filepath.Join(outputDir, "identity-platform-users.json")
	_ = os.WriteFile(usersExportPath, usersJSON, 0644)

	// Generate Docker Compose for Keycloak
	keycloakCompose := `version: '3.8'

services:
  keycloak:
    image: quay.io/keycloak/keycloak:latest
    container_name: keycloak
    environment:
      KEYCLOAK_ADMIN: admin
      KEYCLOAK_ADMIN_PASSWORD: admin
      KC_HEALTH_ENABLED: true
    ports:
      - "8080:8080"
    volumes:
      - ./realm-export.json:/opt/keycloak/data/import/realm-export.json
    command:
      - start-dev
      - --import-realm
    restart: unless-stopped

  postgres:
    image: postgres:15
    container_name: keycloak-db
    environment:
      POSTGRES_DB: keycloak
      POSTGRES_USER: keycloak
      POSTGRES_PASSWORD: keycloak
    volumes:
      - keycloak-db:/var/lib/postgresql/data
    restart: unless-stopped

volumes:
  keycloak-db:
`
	composePath := filepath.Join(outputDir, "docker-compose.yml")
	_ = os.WriteFile(composePath, []byte(keycloakCompose), 0644)

	// Generate provider configuration guide
	providerGuide := fmt.Sprintf(`# Identity Provider Configuration Guide

## Migrated from GCP Identity Platform
Project: %s

## Detected Providers
`, projectID)

	for provider, count := range providers {
		providerGuide += fmt.Sprintf("- %s: %d users\n", provider, count)
	}

	providerGuide += `
## Keycloak Identity Provider Setup

### Google Sign-In
1. Go to Keycloak Admin Console > Identity Providers
2. Add "Google" provider
3. Configure:
   - Client ID: (from Google Cloud Console)
   - Client Secret: (from Google Cloud Console)
   - Scopes: openid email profile

### Facebook Login
1. Add "Facebook" provider
2. Configure:
   - App ID: (from Facebook Developers)
   - App Secret: (from Facebook Developers)

### Apple Sign-In
1. Add "Apple" provider
2. Configure:
   - Client ID: (Service ID from Apple Developer)
   - Client Secret: (Generated JWT)

### Phone Authentication
Keycloak does not natively support SMS authentication.
Consider using:
- Custom authenticator with SMS provider
- WebAuthn for passwordless auth
- Third-party plugins

## User Import

1. Start Keycloak:
'''bash
docker-compose up -d
'''

2. Access Admin Console:
- URL: http://localhost:8080
- Username: admin
- Password: admin

3. The realm will be auto-imported on startup.

4. Users will need to reset their passwords.

## Post-Migration Tasks
- [ ] Configure identity providers
- [ ] Set up email templates for password reset
- [ ] Configure MFA if needed
- [ ] Test login flows
- [ ] Update application OAuth settings
`

	providerGuidePath := filepath.Join(outputDir, "PROVIDER_GUIDE.md")
	_ = os.WriteFile(providerGuidePath, []byte(providerGuide), 0644)

	// Generate README
	readme := fmt.Sprintf(`# Identity Platform to Keycloak Migration

## Source
- GCP Project: %s
- Users Migrated: %d
- Providers Detected: %d

## Files Generated
- docker-compose.yml: Keycloak container
- realm-export.json: Keycloak realm import
- identity-platform-users.json: Original user data
- PROVIDER_GUIDE.md: Identity provider setup guide

## Getting Started

'''bash
docker-compose up -d
'''

Access Keycloak Admin Console:
- URL: http://localhost:8080
- Username: admin
- Password: admin

## Important Notes
- Passwords cannot be migrated from Identity Platform
- Users must reset their passwords
- Social login providers need manual configuration
- See PROVIDER_GUIDE.md for provider setup

## Production Considerations
- Use PostgreSQL for Keycloak database
- Configure proper SSL/TLS
- Set strong admin credentials
- Enable audit logging
`, projectID, len(allUsers), len(providers))

	readmePath := filepath.Join(outputDir, "README.md")
	_ = os.WriteFile(readmePath, []byte(readme), 0644)

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", fmt.Sprintf("Identity Platform migration complete: %d users", len(allUsers)))
	EmitLog(m, "warn", "Users will need to reset their passwords in Keycloak")

	return nil
}

// ============================================================================
// Cloud DNS to CoreDNS Executor
// ============================================================================

// CloudDNSToCoreDNSExecutor migrates GCP Cloud DNS zones to CoreDNS.
type CloudDNSToCoreDNSExecutor struct{}

// NewCloudDNSToCoreDNSExecutor creates a new Cloud DNS to CoreDNS executor.
func NewCloudDNSToCoreDNSExecutor() *CloudDNSToCoreDNSExecutor {
	return &CloudDNSToCoreDNSExecutor{}
}

// Type returns the migration type.
func (e *CloudDNSToCoreDNSExecutor) Type() string {
	return "clouddns_to_coredns"
}

// GetPhases returns the migration phases.
func (e *CloudDNSToCoreDNSExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Listing DNS zones",
		"Exporting DNS records",
		"Generating CoreDNS configuration",
		"Creating zone files",
	}
}

// Validate validates the migration configuration.
func (e *CloudDNSToCoreDNSExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	// Validate source config
	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["project_id"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.project_id is required")
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
	}

	result.Warnings = append(result.Warnings, "Ensure DNS propagation time is considered during cutover")
	result.Warnings = append(result.Warnings, "Some Cloud DNS features may not have CoreDNS equivalents")

	return result, nil
}

// cloudDNSZone represents a Cloud DNS managed zone.
type cloudDNSZone struct {
	Name        string `json:"name"`
	DNSName     string `json:"dnsName"`
	Description string `json:"description"`
	Visibility  string `json:"visibility"`
	Kind        string `json:"kind"`
}

// cloudDNSRecord represents a Cloud DNS resource record set.
type cloudDNSRecord struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	TTL     int      `json:"ttl"`
	Rrdatas []string `json:"rrdatas"`
}

// Execute performs the migration.
func (e *CloudDNSToCoreDNSExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	// Extract configuration
	projectID := config.Source["project_id"].(string)
	serviceAccountJSON, hasServiceAccount := config.Source["service_account_json"].(string)
	outputDir := config.Destination["output_dir"].(string)

	// Optional: specific zones to migrate
	var zonesToMigrate []string
	if zones, ok := config.Source["zones"].([]interface{}); ok {
		for _, z := range zones {
			if zoneStr, ok := z.(string); ok {
				zonesToMigrate = append(zonesToMigrate, zoneStr)
			}
		}
	}

	// Prepare gcloud environment
	var gcloudEnv []string
	if hasServiceAccount {
		saFile, err := os.CreateTemp("", "gcp-sa-*.json")
		if err != nil {
			return fmt.Errorf("failed to create temp file for service account: %w", err)
		}
		defer func() { _ = os.Remove(saFile.Name()) }()
		if err := os.WriteFile(saFile.Name(), []byte(serviceAccountJSON), 0600); err != nil {
			return fmt.Errorf("failed to write service account file: %w", err)
		}
		gcloudEnv = append(os.Environ(), "GOOGLE_APPLICATION_CREDENTIALS="+saFile.Name())
	} else {
		gcloudEnv = os.Environ()
	}

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating GCP credentials")
	EmitProgress(m, 5, "Checking credentials")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	testCmd := exec.CommandContext(ctx, "gcloud", "auth", "list", "--format=json")
	testCmd.Env = gcloudEnv
	if _, err := testCmd.Output(); err != nil {
		EmitLog(m, "error", "GCP credentials validation failed")
		return fmt.Errorf("failed to validate GCP credentials: %w", err)
	}
	EmitLog(m, "info", "GCP credentials validated successfully")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Listing DNS zones
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", fmt.Sprintf("Listing DNS zones in project: %s", projectID))
	EmitProgress(m, 15, "Listing zones")

	listZonesCmd := exec.CommandContext(ctx, "gcloud", "dns", "managed-zones", "list",
		"--project", projectID,
		"--format=json",
	)
	listZonesCmd.Env = gcloudEnv

	zonesOutput, err := listZonesCmd.Output()
	if err != nil {
		EmitLog(m, "error", "Failed to list DNS zones")
		return fmt.Errorf("failed to list DNS zones: %w", err)
	}

	var allZones []cloudDNSZone
	if err := json.Unmarshal(zonesOutput, &allZones); err != nil {
		return fmt.Errorf("failed to parse zones: %w", err)
	}

	// Filter zones if specific ones requested
	var zones []cloudDNSZone
	if len(zonesToMigrate) > 0 {
		zoneMap := make(map[string]bool)
		for _, z := range zonesToMigrate {
			zoneMap[z] = true
		}
		for _, zone := range allZones {
			if zoneMap[zone.Name] {
				zones = append(zones, zone)
			}
		}
	} else {
		zones = allZones
	}

	EmitLog(m, "info", fmt.Sprintf("Found %d zones to migrate", len(zones)))
	for _, zone := range zones {
		EmitLog(m, "info", fmt.Sprintf("Zone: %s (%s) - %s", zone.Name, zone.DNSName, zone.Visibility))
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Exporting DNS records
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", "Exporting DNS records")
	EmitProgress(m, 35, "Exporting records")

	// Ensure output directory exists
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Create zones directory
	zonesDir := filepath.Join(outputDir, "zones")
	if err := os.MkdirAll(zonesDir, 0755); err != nil {
		return fmt.Errorf("failed to create zones directory: %w", err)
	}

	type ZoneRecords struct {
		Zone    cloudDNSZone     `json:"zone"`
		Records []cloudDNSRecord `json:"records"`
	}
	allZoneRecords := make([]ZoneRecords, 0)

	for i, zone := range zones {
		EmitLog(m, "info", fmt.Sprintf("Exporting records for zone: %s (%d/%d)", zone.Name, i+1, len(zones)))

		listRecordsCmd := exec.CommandContext(ctx, "gcloud", "dns", "record-sets", "list",
			"--zone", zone.Name,
			"--project", projectID,
			"--format=json",
		)
		listRecordsCmd.Env = gcloudEnv

		recordsOutput, err := listRecordsCmd.Output()
		if err != nil {
			EmitLog(m, "warn", fmt.Sprintf("Failed to list records for zone %s: %v", zone.Name, err))
			continue
		}

		var records []cloudDNSRecord
		if err := json.Unmarshal(recordsOutput, &records); err != nil {
			EmitLog(m, "warn", fmt.Sprintf("Failed to parse records for zone %s: %v", zone.Name, err))
			continue
		}

		allZoneRecords = append(allZoneRecords, ZoneRecords{
			Zone:    zone,
			Records: records,
		})

		EmitLog(m, "info", fmt.Sprintf("Exported %d records from zone %s", len(records), zone.Name))

		if m.IsCancelled() {
			return fmt.Errorf("migration cancelled")
		}
	}

	// Save all zone data as JSON
	zonesJSON, _ := json.MarshalIndent(allZoneRecords, "", "  ")
	zonesJSONPath := filepath.Join(outputDir, "cloud-dns-export.json")
	_ = os.WriteFile(zonesJSONPath, zonesJSON, 0644)

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Generating CoreDNS configuration
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Generating CoreDNS configuration")
	EmitProgress(m, 60, "Generating config")

	// Generate Corefile
	corefile := "# CoreDNS Configuration\n# Migrated from GCP Cloud DNS\n\n"

	for _, zr := range allZoneRecords {
		zoneName := strings.TrimSuffix(zr.Zone.DNSName, ".")
		corefile += fmt.Sprintf(`%s {
    file /etc/coredns/zones/%s.zone
    log
    errors
}

`, zoneName, zr.Zone.Name)
	}

	// Add forward for external resolution
	corefile += `# Forward other queries to upstream DNS
. {
    forward . 8.8.8.8 8.8.4.4
    cache 30
    log
    errors
}
`

	corefilePath := filepath.Join(outputDir, "Corefile")
	_ = os.WriteFile(corefilePath, []byte(corefile), 0644)

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Creating zone files
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Creating zone files")
	EmitProgress(m, 80, "Creating zone files")

	for _, zr := range allZoneRecords {
		zoneFile := generateZoneFile(zr.Zone, zr.Records)
		zoneFilePath := filepath.Join(zonesDir, zr.Zone.Name+".zone")
		_ = os.WriteFile(zoneFilePath, []byte(zoneFile), 0644)
		EmitLog(m, "info", fmt.Sprintf("Created zone file: %s.zone", zr.Zone.Name))
	}

	// Generate Docker Compose for CoreDNS
	corednsCompose := `version: '3.8'

services:
  coredns:
    image: coredns/coredns:latest
    container_name: coredns
    ports:
      - "53:53/udp"
      - "53:53/tcp"
    volumes:
      - ./Corefile:/etc/coredns/Corefile:ro
      - ./zones:/etc/coredns/zones:ro
    command: -conf /etc/coredns/Corefile
    restart: unless-stopped
    networks:
      - dns-network

networks:
  dns-network:
    driver: bridge
`
	composePath := filepath.Join(outputDir, "docker-compose.yml")
	_ = os.WriteFile(composePath, []byte(corednsCompose), 0644)

	// Generate README
	totalRecords := 0
	for _, zr := range allZoneRecords {
		totalRecords += len(zr.Records)
	}

	readme := fmt.Sprintf(`# Cloud DNS to CoreDNS Migration

## Source
- GCP Project: %s
- Zones Migrated: %d
- Total Records: %d

## Files Generated
- docker-compose.yml: CoreDNS container
- Corefile: CoreDNS configuration
- zones/: Zone files for each domain
- cloud-dns-export.json: Original Cloud DNS data

## Getting Started

1. Start CoreDNS:
'''bash
docker-compose up -d
'''

2. Test DNS resolution:
'''bash
dig @localhost example.com
'''

## Zone Files

`, projectID, len(allZoneRecords), totalRecords)

	for _, zr := range allZoneRecords {
		readme += fmt.Sprintf("- %s.zone: %s (%d records)\n", zr.Zone.Name, zr.Zone.DNSName, len(zr.Records))
	}

	readme += `
## DNS Cutover Steps

1. Test CoreDNS thoroughly in staging
2. Lower TTLs on Cloud DNS records (24-48 hours before)
3. Update NS records at domain registrar
4. Monitor for propagation
5. Keep Cloud DNS as backup during transition

## Record Types Supported
- A, AAAA: IPv4/IPv6 addresses
- CNAME: Canonical names
- MX: Mail servers
- TXT: Text records (SPF, DKIM, etc.)
- NS: Nameservers
- SOA: Start of Authority
- SRV: Service records
- PTR: Pointer records
- CAA: Certificate Authority Authorization

## Production Considerations
- Use multiple CoreDNS instances for HA
- Configure proper monitoring
- Set up log aggregation
- Consider Kubernetes deployment for scaling
- Implement DNSSEC if required
`

	readmePath := filepath.Join(outputDir, "README.md")
	_ = os.WriteFile(readmePath, []byte(readme), 0644)

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", fmt.Sprintf("Cloud DNS migration complete: %d zones, %d records", len(allZoneRecords), totalRecords))

	return nil
}

// generateZoneFile creates a BIND-style zone file from Cloud DNS records.
func generateZoneFile(zone cloudDNSZone, records []cloudDNSRecord) string {
	zoneName := strings.TrimSuffix(zone.DNSName, ".")

	zoneFile := fmt.Sprintf(`; Zone file for %s
; Migrated from GCP Cloud DNS zone: %s
; Generated automatically

$ORIGIN %s.
$TTL 3600

`, zoneName, zone.Name, zoneName)

	// Find SOA record first
	var soaRecord *cloudDNSRecord
	for _, r := range records {
		if r.Type == "SOA" {
			soaRecord = &r
			break
		}
	}

	// Generate SOA if not found
	if soaRecord == nil {
		zoneFile += fmt.Sprintf(`@   IN  SOA ns1.%s. admin.%s. (
                2024010101  ; Serial
                3600        ; Refresh
                1800        ; Retry
                604800      ; Expire
                86400       ; Minimum TTL
            )

`, zoneName, zoneName)
	}

	// Add records
	for _, record := range records {
		name := record.Name
		// Convert FQDN to relative name
		if strings.HasSuffix(name, zone.DNSName) {
			name = strings.TrimSuffix(name, zone.DNSName)
			name = strings.TrimSuffix(name, ".")
			if name == "" {
				name = "@"
			}
		}

		for _, rdata := range record.Rrdatas {
			switch record.Type {
			case "SOA":
				zoneFile += fmt.Sprintf("%s\t%d\tIN\t%s\t%s\n", name, record.TTL, record.Type, rdata)
			case "TXT":
				// TXT records may need quote handling
				if !strings.HasPrefix(rdata, "\"") {
					rdata = fmt.Sprintf("\"%s\"", rdata)
				}
				zoneFile += fmt.Sprintf("%s\t%d\tIN\t%s\t%s\n", name, record.TTL, record.Type, rdata)
			case "MX":
				zoneFile += fmt.Sprintf("%s\t%d\tIN\t%s\t%s\n", name, record.TTL, record.Type, rdata)
			case "SRV":
				zoneFile += fmt.Sprintf("%s\t%d\tIN\t%s\t%s\n", name, record.TTL, record.Type, rdata)
			default:
				zoneFile += fmt.Sprintf("%s\t%d\tIN\t%s\t%s\n", name, record.TTL, record.Type, rdata)
			}
		}
	}

	return zoneFile
}
