// Package security provides mappers for GCP security services.
package security

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

// IdentityPlatformMapper converts GCP Identity Platform to Keycloak.
type IdentityPlatformMapper struct {
	*mapper.BaseMapper
}

// NewIdentityPlatformMapper creates a new Identity Platform to Keycloak mapper.
func NewIdentityPlatformMapper() *IdentityPlatformMapper {
	return &IdentityPlatformMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeIdentityPlatform, nil),
	}
}

// Map converts a GCP Identity Platform configuration to a Keycloak service.
func (m *IdentityPlatformMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	projectID := res.GetConfigString("project")
	if projectID == "" {
		projectID = res.Name
	}

	result := mapper.NewMappingResult("keycloak")
	svc := result.DockerService

	svc.Image = "quay.io/keycloak/keycloak:23.0"
	svc.Environment = map[string]string{
		"KEYCLOAK_ADMIN":          "admin",
		"KEYCLOAK_ADMIN_PASSWORD": "admin",
		"KC_DB":                   "postgres",
		"KC_DB_URL":               "jdbc:postgresql://postgres-keycloak:5432/keycloak",
		"KC_DB_USERNAME":          "keycloak",
		"KC_DB_PASSWORD":          "keycloak",
		"KC_HTTP_ENABLED":         "true",
		"KC_HEALTH_ENABLED":       "true",
	}
	svc.Command = []string{"start-dev"}
	svc.Ports = []string{"8080:8080"}
	svc.DependsOn = []string{"postgres-keycloak"}
	svc.Volumes = []string{"./config/keycloak:/opt/keycloak/data/import"}
	svc.Networks = []string{"homeport"}
	svc.Labels = map[string]string{
		"homeport.source":     "google_identity_platform_config",
		"homeport.project_id": projectID,
		"traefik.enable":       "true",
	}
	svc.Restart = "unless-stopped"
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD-SHELL", "curl -f http://localhost:8080/health/ready || exit 1"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  5,
	}

	realmConfig := m.generateRealmConfig(res, projectID)
	result.AddConfig("config/keycloak/realm.json", []byte(realmConfig))

	postgresConfig := m.generatePostgresConfig()
	result.AddConfig("config/keycloak/postgres-service.yml", []byte(postgresConfig))

	setupScript := m.generateSetupScript(projectID)
	result.AddScript("setup_keycloak.sh", []byte(setupScript))

	migrationScript := m.generateMigrationScript(projectID)
	result.AddScript("migrate_users.sh", []byte(migrationScript))

	if signIn := res.Config["sign_in"]; signIn != nil {
		m.handleSignInConfig(signIn, result)
	}

	if mfa := res.Config["mfa"]; mfa != nil {
		m.handleMFAConfig(mfa, result)
	}

	result.AddManualStep("Access Keycloak at http://localhost:8080")
	result.AddManualStep("Import realm from config/keycloak/realm.json")
	result.AddManualStep("Export users from Firebase and import to Keycloak")
	result.AddWarning("Password hashes cannot be migrated - users must reset passwords")
	result.AddWarning("Social identity providers require new OAuth credentials")

	return result, nil
}

func (m *IdentityPlatformMapper) generateRealmConfig(res *resource.AWSResource, projectID string) string {
	realmName := strings.ToLower(strings.ReplaceAll(projectID, "_", "-"))

	realm := map[string]interface{}{
		"realm":                       realmName,
		"enabled":                     true,
		"displayName":                 "Identity Platform: " + projectID,
		"registrationAllowed":         true,
		"registrationEmailAsUsername": true,
		"resetPasswordAllowed":        true,
		"loginWithEmailAllowed":       true,
		"verifyEmail":                 true,
		"bruteForceProtected":         true,
		"attributes": map[string]string{
			"migratedFrom": "GCP Identity Platform",
			"projectId":    projectID,
		},
	}

	data, _ := json.MarshalIndent(realm, "", "  ")
	return string(data)
}

func (m *IdentityPlatformMapper) generatePostgresConfig() string {
	return `postgres-keycloak:
  image: postgres:16-alpine
  environment:
    POSTGRES_DB: keycloak
    POSTGRES_USER: keycloak
    POSTGRES_PASSWORD: keycloak
  volumes:
    - ./data/postgres-keycloak:/var/lib/postgresql/data
  networks:
    - homeport
  healthcheck:
    test: ["CMD-SHELL", "pg_isready -U keycloak"]
    interval: 10s
    timeout: 5s
    retries: 5
  restart: unless-stopped
`
}

func (m *IdentityPlatformMapper) generateSetupScript(projectID string) string {
	realmName := strings.ToLower(strings.ReplaceAll(projectID, "_", "-"))
	return fmt.Sprintf(`#!/bin/bash
set -e

KEYCLOAK_URL="http://localhost:8080"
REALM="%s"

echo "Identity Platform to Keycloak Setup"
echo "===================================="

until curl -sf "$KEYCLOAK_URL/health/ready" > /dev/null; do
  sleep 5
done

TOKEN=$(curl -s -X POST "$KEYCLOAK_URL/realms/master/protocol/openid-connect/token" \
  -d "username=admin&password=admin&grant_type=password&client_id=admin-cli" | jq -r '.access_token')

curl -X POST "$KEYCLOAK_URL/admin/realms" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d @config/keycloak/realm.json

echo "Realm $REALM created!"
`, realmName)
}

func (m *IdentityPlatformMapper) generateMigrationScript(projectID string) string {
	return fmt.Sprintf(`#!/bin/bash
set -e

PROJECT_ID="%s"

echo "User Migration from Identity Platform"
echo "======================================"
echo ""
echo "Step 1: Export users from Firebase"
echo "  firebase auth:export users.json --project $PROJECT_ID --format=json"
echo ""
echo "Step 2: Transform user data for Keycloak"
echo "  See https://www.keycloak.org/docs/latest/server_admin/#user-storage-federation"
echo ""
echo "Step 3: Import users to Keycloak"
echo "  Use Keycloak Admin API or CLI"
echo ""
echo "Note: Password hashes cannot be migrated. Users must reset passwords."
`, projectID)
}

func (m *IdentityPlatformMapper) handleSignInConfig(signIn interface{}, result *mapper.MappingResult) {
	if signInMap, ok := signIn.(map[string]interface{}); ok {
		if email, ok := signInMap["email"].(map[string]interface{}); ok {
			if enabled, ok := email["enabled"].(bool); ok && enabled {
				result.AddWarning("Email sign-in enabled. Configure email verification in Keycloak.")
			}
		}
		if phone, ok := signInMap["phone_number"].(map[string]interface{}); ok {
			if enabled, ok := phone["enabled"].(bool); ok && enabled {
				result.AddWarning("Phone sign-in enabled. Keycloak requires SMS provider extension.")
				result.AddManualStep("Install Keycloak SMS authenticator SPI")
			}
		}
	}
}

func (m *IdentityPlatformMapper) handleMFAConfig(mfa interface{}, result *mapper.MappingResult) {
	if mfaMap, ok := mfa.(map[string]interface{}); ok {
		if state, ok := mfaMap["state"].(string); ok && (state == "ENABLED" || state == "MANDATORY") {
			result.AddWarning("MFA is " + strings.ToLower(state) + ". Configure OTP in Keycloak.")
			result.AddManualStep("Enable OTP in Keycloak: Realm Settings > Authentication > Required Actions")
		}
	}
}
