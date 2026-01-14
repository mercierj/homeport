// Package security provides mappers for Azure security services.
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

// ADB2CMapper converts Azure AD B2C to Keycloak.
type ADB2CMapper struct {
	*mapper.BaseMapper
}

// NewADB2CMapper creates a new Azure AD B2C to Keycloak mapper.
func NewADB2CMapper() *ADB2CMapper {
	return &ADB2CMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeAzureADB2C, nil),
	}
}

// Map converts an Azure AD B2C directory to a Keycloak service.
func (m *ADB2CMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	directoryName := res.GetConfigString("domain_name")
	if directoryName == "" {
		directoryName = res.Name
	}

	result := mapper.NewMappingResult("keycloak")
	svc := result.DockerService

	svc.Image = "quay.io/keycloak/keycloak:23.0"
	svc.Environment = map[string]string{
		"KEYCLOAK_ADMIN":          "admin",
		"KEYCLOAK_ADMIN_PASSWORD": "changeme",
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
	svc.Volumes = []string{"./data/keycloak:/opt/keycloak/data"}
	svc.Networks = []string{"homeport"}
	svc.Labels = map[string]string{
		"homeport.source":    "azurerm_aadb2c_directory",
		"homeport.directory": directoryName,
		"traefik.enable":      "true",
	}
	svc.Restart = "unless-stopped"
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD-SHELL", "curl -f http://localhost:8080/health/ready || exit 1"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  5,
	}

	realmConfig := m.generateRealmConfig(res, directoryName)
	result.AddConfig("config/keycloak/realm.json", []byte(realmConfig))

	postgresConfig := m.generatePostgresConfig()
	result.AddConfig("config/keycloak/postgres-service.yml", []byte(postgresConfig))

	setupScript := m.generateSetupScript(directoryName)
	result.AddScript("setup_keycloak.sh", []byte(setupScript))

	userFlowScript := m.generateUserFlowScript(directoryName)
	result.AddScript("migrate_user_flows.sh", []byte(userFlowScript))

	result.AddManualStep("Import realm: Admin Console > Add Realm > Import")
	result.AddManualStep("Recreate user flows as Keycloak authentication flows")
	result.AddManualStep("Configure external identity providers with new credentials")
	result.AddManualStep("Export users from Azure AD B2C and import to Keycloak")

	result.AddWarning("User flows must be recreated manually in Keycloak")
	result.AddWarning("Social identity providers require new OAuth credentials")
	result.AddWarning("Custom policies (Identity Experience Framework) need manual conversion")

	return result, nil
}

func (m *ADB2CMapper) generateRealmConfig(res *resource.AWSResource, directoryName string) string {
	realmName := strings.ToLower(strings.ReplaceAll(directoryName, ".", "-"))
	tenantID := res.GetConfigString("tenant_id")
	if tenantID == "" {
		tenantID = "your-tenant-id"
	}

	realm := map[string]interface{}{
		"realm":                       realmName,
		"enabled":                     true,
		"displayName":                 "Azure AD B2C: " + directoryName,
		"registrationAllowed":         true,
		"registrationEmailAsUsername": true,
		"rememberMe":                  true,
		"verifyEmail":                 true,
		"loginWithEmailAllowed":       true,
		"resetPasswordAllowed":        true,
		"bruteForceProtected":         true,
		"attributes": map[string]string{
			"migratedFrom":      "Azure AD B2C",
			"azureB2CTenantId": tenantID,
		},
		"roles": map[string]interface{}{
			"realm": []map[string]string{
				{"name": "user", "description": "Standard user"},
				{"name": "admin", "description": "Administrator"},
			},
		},
	}

	data, _ := json.MarshalIndent(realm, "", "  ")
	return string(data)
}

func (m *ADB2CMapper) generatePostgresConfig() string {
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

func (m *ADB2CMapper) generateSetupScript(directoryName string) string {
	realmName := strings.ToLower(strings.ReplaceAll(directoryName, ".", "-"))
	return fmt.Sprintf(`#!/bin/bash
set -e

KEYCLOAK_URL="http://localhost:8080"
REALM="%s"

echo "Azure AD B2C to Keycloak Setup"
echo "=============================="

until curl -sf "$KEYCLOAK_URL/health/ready" > /dev/null; do
  sleep 5
done

TOKEN=$(curl -s -X POST "$KEYCLOAK_URL/realms/master/protocol/openid-connect/token" \
  -d "username=admin&password=changeme&grant_type=password&client_id=admin-cli" | jq -r '.access_token')

curl -X POST "$KEYCLOAK_URL/admin/realms" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d @config/keycloak/realm.json

echo "Realm $REALM created!"
`, realmName)
}

func (m *ADB2CMapper) generateUserFlowScript(directoryName string) string {
	realmName := strings.ToLower(strings.ReplaceAll(directoryName, ".", "-"))
	return fmt.Sprintf(`#!/bin/bash
set -e

KEYCLOAK_URL="http://localhost:8080"
REALM="%s"

echo "User Flow Migration Guide"
echo "========================="
echo ""
echo "Azure AD B2C user flows map to Keycloak authentication flows:"
echo ""
echo "  B2C_1_signupsignin -> Browser flow + Registration"
echo "  B2C_1_password_reset -> Reset credentials flow"
echo "  B2C_1_profile_edit -> Update profile required action"
echo ""
echo "To configure in Keycloak:"
echo "1. Go to Authentication > Flows"
echo "2. Copy 'browser' flow and customize"
echo "3. Bind to 'Browser flow' in realm settings"
`, realmName)
}
