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
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
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
		"traefik.enable":      "true",
	}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD-SHELL", "curl -f http://localhost:8080/health/ready || exit 1"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  5,
	}

	realmConfig := m.generateRealmConfig(res, projectID)
	result.AddConfig("config/keycloak/realm.json", []byte(realmConfig))
	result.AddConfig("config/identity-platform/app-change.env", []byte(m.generateAppChangeConfig(projectID)))
	result.AddConfig("config/identity-platform/migration.env", []byte(m.generateMigrationConfig(projectID)))

	postgresConfig := m.generatePostgresConfig()
	result.AddConfig("config/keycloak/postgres-service.yml", []byte(postgresConfig))

	setupScript := m.generateSetupScript(projectID)
	result.AddScript("setup_keycloak.sh", []byte(setupScript))

	migrationScript := m.generateMigrationScript(projectID)
	result.AddScript("migrate_users.sh", []byte(migrationScript))
	result.AddScript("export_identity_platform_users.sh", []byte(m.generateExportScript(projectID)))
	result.AddScript("import_identity_platform_keycloak.sh", []byte(m.generateImportScript(projectID)))
	result.AddScript("validate_identity_platform_keycloak.sh", []byte(m.generateValidateScript(projectID)))
	result.AddScript("backup_identity_platform_config.sh", []byte(m.generateBackupScript(projectID)))
	result.AddScript("cutover_identity_platform_clients.sh", []byte(m.generateCutoverScript(projectID)))
	for _, step := range identityPlatformRunbook(projectID) {
		result.AddRunbookStep(step)
	}

	if signIn := res.Config["sign_in"]; signIn != nil {
		m.handleSignInConfig(signIn, result)
	}

	if mfa := res.Config["mfa"]; mfa != nil {
		m.handleMFAConfig(mfa, result)
	}

	result.AddWarning("Password hashes cannot be migrated - users must reset passwords")
	result.AddWarning("Social identity providers require new OAuth credentials")

	return result, nil
}

func (m *IdentityPlatformMapper) generateAppChangeConfig(projectID string) string {
	realmName := strings.ToLower(strings.ReplaceAll(projectID, "_", "-"))
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_IDENTITY_PLATFORM_PROJECT=%s\nTARGET_AUTH_PROVIDER=keycloak\nTARGET_KEYCLOAK_URL=http://keycloak:8080\nTARGET_KEYCLOAK_REALM=%s\n", projectID, realmName)
}

func (m *IdentityPlatformMapper) generateMigrationConfig(projectID string) string {
	return fmt.Sprintf("SOURCE_IDENTITY_PLATFORM_PROJECT=%s\nTARGET_PROVIDER=keycloak\nTARGET_REALM=%s\nUSER_EXPORT_PATH=./identity-platform-export/users.json\n", projectID, strings.ToLower(strings.ReplaceAll(projectID, "_", "-")))
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

func (m *IdentityPlatformMapper) generateExportScript(projectID string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\nmkdir -p identity-platform-export\nfirebase auth:export identity-platform-export/users.json --project %q --format=json\n", projectID)
}

func (m *IdentityPlatformMapper) generateImportScript(projectID string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s identity-platform-export/users.json\ntest -s config/keycloak/realm.json\necho \"Transform and import Identity Platform users for %s into Keycloak\"\n", projectID)
}

func (m *IdentityPlatformMapper) generateValidateScript(projectID string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/identity-platform/app-change.env\ntest -s config/keycloak/realm.json\ncurl -fsS \"${KEYCLOAK_URL:-http://localhost:8080}/health/ready\" >/dev/null\necho \"Identity Platform project %s validated on Keycloak\"\n", projectID)
}

func (m *IdentityPlatformMapper) generateBackupScript(projectID string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/identity-platform-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/identity-platform config/keycloak identity-platform-export\necho \"$archive\"\n", strings.ReplaceAll(projectID, "/", "-"))
}

func (m *IdentityPlatformMapper) generateCutoverScript(projectID string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/identity-platform/app-change.env\ntest \"$SOURCE_IDENTITY_PLATFORM_PROJECT\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Patch auth clients to $TARGET_KEYCLOAK_URL realm $TARGET_KEYCLOAK_REALM\"\n", projectID)
}

func identityPlatformRunbook(projectID string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "auth", "source": "google_identity_platform_config", "project": projectID, "target": "keycloak"}
	return []domainrunbook.Step{
		identityPlatformStep("export-identity-platform-users", "Export Identity Platform users", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_identity_platform_users.sh"}, "Identity Platform users are exported", metadata),
		identityPlatformStep("provision-keycloak-identity", "Provision Keycloak identity realm", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "setup_keycloak.sh"}, "Keycloak realm is configured", metadata),
		identityPlatformStep("migrate-identity-platform-users", "Migrate users to Keycloak", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "import_identity_platform_keycloak.sh"}, "users are transformed and imported", metadata),
		identityPlatformStep("validate-identity-platform-keycloak", "Validate Keycloak identity target", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_identity_platform_keycloak.sh"}, "Keycloak health and config validate", metadata),
		identityPlatformStep("backup-identity-platform-config", "Backup identity config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_identity_platform_config.sh"}, "identity migration artifacts are archived", metadata),
		identityPlatformStep("cutover-identity-platform-clients", "Cut over identity clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_identity_platform_clients.sh"}, "auth clients use Keycloak", metadata),
		identityPlatformStep("rollback-identity-platform-source", "Keep Identity Platform source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "Identity Platform remains authoritative until Keycloak validation passes", metadata),
	}
}

func identityPlatformStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
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
				result.AddConfig("config/keycloak/sms-authenticator.env", []byte("KEYCLOAK_SMS_AUTHENTICATOR=required\n"))
			}
		}
	}
}

func (m *IdentityPlatformMapper) handleMFAConfig(mfa interface{}, result *mapper.MappingResult) {
	if mfaMap, ok := mfa.(map[string]interface{}); ok {
		if state, ok := mfaMap["state"].(string); ok && (state == "ENABLED" || state == "MANDATORY") {
			result.AddWarning("MFA is " + strings.ToLower(state) + ". Configure OTP in Keycloak.")
			result.AddConfig("config/keycloak/otp-required-action.env", []byte("KEYCLOAK_OTP_REQUIRED_ACTION="+state+"\n"))
		}
	}
}
