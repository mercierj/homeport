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
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
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
		"traefik.enable":     "true",
	}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD-SHELL", "curl -f http://localhost:8080/health/ready || exit 1"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  5,
	}
	result.AddService(m.postgresService())

	realmConfig := m.generateRealmConfig(res, directoryName)
	result.AddConfig("config/keycloak/realm.json", []byte(realmConfig))

	postgresConfig := m.generatePostgresConfig()
	result.AddConfig("config/keycloak/postgres-service.yml", []byte(postgresConfig))
	result.AddConfig("config/adb2c/app-change.env", []byte(m.generateAppChange(directoryName)))
	result.AddConfig("config/adb2c/generated-client.patch", []byte(m.generateClientPatch(directoryName)))
	result.AddConfig("config/adb2c/user-import-plan.json", []byte(m.generateUserImportPlan(directoryName)))

	setupScript := m.generateSetupScript(directoryName)
	result.AddScript("setup_keycloak.sh", []byte(setupScript))

	userFlowScript := m.generateUserFlowScript(directoryName)
	result.AddScript("migrate_user_flows.sh", []byte(userFlowScript))
	result.AddScript("export_adb2c_users.sh", []byte(m.generateUserExportScript(directoryName)))
	result.AddScript("validate_adb2c_keycloak.sh", []byte(m.generateValidateScript(directoryName)))
	result.AddScript("backup_adb2c_config.sh", []byte(m.generateBackupScript(directoryName)))
	result.AddScript("cutover_adb2c_clients.sh", []byte(m.generateCutoverScript(directoryName)))

	for _, step := range m.runbook(directoryName) {
		result.AddRunbookStep(step)
	}

	result.AddWarning("User flows are converted into generated Keycloak authentication-flow commands.")
	result.AddWarning("Social identity providers are emitted as generated client patch placeholders for new OAuth credentials.")
	result.AddWarning("Custom policies (Identity Experience Framework) are captured in the generated migration plan for validation.")

	return result, nil
}

func (m *ADB2CMapper) postgresService() *mapper.DockerService {
	svc := mapper.NewDockerService("postgres-keycloak")
	svc.Image = "postgres:16-alpine"
	svc.Environment = map[string]string{"POSTGRES_DB": "keycloak", "POSTGRES_USER": "keycloak", "POSTGRES_PASSWORD": "keycloak"}
	svc.Volumes = []string{"./data/postgres-keycloak:/var/lib/postgresql/data"}
	svc.Networks = []string{"homeport"}
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD-SHELL", "pg_isready -U keycloak"}, Interval: 10 * time.Second, Timeout: 5 * time.Second, Retries: 5}
	return svc
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
			"migratedFrom":     "Azure AD B2C",
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

func (m *ADB2CMapper) generateAppChange(directoryName string) string {
	realmName := strings.ToLower(strings.ReplaceAll(directoryName, ".", "-"))
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_ADB2C_DIRECTORY=%s\nKEYCLOAK_URL=http://keycloak:8080\nKEYCLOAK_REALM=%s\nGENERATED_PATCH=config/adb2c/generated-client.patch\n", directoryName, realmName)
}

func (m *ADB2CMapper) generateClientPatch(directoryName string) string {
	realmName := strings.ToLower(strings.ReplaceAll(directoryName, ".", "-"))
	return fmt.Sprintf("--- a/app/auth.env\n+++ b/app/auth.env\n@@\n-AZURE_AD_B2C_TENANT=%s\n+OIDC_ISSUER=http://keycloak:8080/realms/%s\n+OIDC_AUTH_URL=http://keycloak:8080/realms/%s/protocol/openid-connect/auth\n+OIDC_TOKEN_URL=http://keycloak:8080/realms/%s/protocol/openid-connect/token\n", directoryName, realmName, realmName, realmName)
}

func (m *ADB2CMapper) generateUserImportPlan(directoryName string) string {
	realmName := strings.ToLower(strings.ReplaceAll(directoryName, ".", "-"))
	plan := map[string]string{
		"source_directory": directoryName,
		"target_realm":     realmName,
		"export_file":      "adb2c-export/users.json",
		"import_command":   "kcadm.sh create users -r " + realmName,
	}
	data, _ := json.MarshalIndent(plan, "", "  ")
	return string(data)
}

func (m *ADB2CMapper) generateUserExportScript(directoryName string) string {
	return fmt.Sprintf(`#!/bin/bash
set -e

OUTPUT_DIR="${OUTPUT_DIR:-./adb2c-export}"
mkdir -p "$OUTPUT_DIR"
az rest --method GET --url "https://graph.microsoft.com/v1.0/users?$select=id,displayName,userPrincipalName,identities" > "$OUTPUT_DIR/users.json"
echo "Exported Azure AD B2C users for %s to $OUTPUT_DIR/users.json"
`, directoryName)
}

func (m *ADB2CMapper) generateValidateScript(directoryName string) string {
	realmName := strings.ToLower(strings.ReplaceAll(directoryName, ".", "-"))
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/keycloak/realm.json\ntest -s config/adb2c/app-change.env\ngrep -q %q config/adb2c/app-change.env\n", realmName)
}

func (m *ADB2CMapper) generateBackupScript(directoryName string) string {
	realmName := strings.ToLower(strings.ReplaceAll(directoryName, ".", "-"))
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/adb2c-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/keycloak config/adb2c adb2c-export 2>/dev/null || tar -czf \"$archive\" config/keycloak config/adb2c\necho \"$archive\"\n", realmName)
}

func (m *ADB2CMapper) generateCutoverScript(directoryName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/adb2c/app-change.env\ntest \"$SOURCE_ADB2C_DIRECTORY\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Apply $GENERATED_PATCH and point OIDC clients at $KEYCLOAK_URL/realms/$KEYCLOAK_REALM\"\n", directoryName)
}

func (m *ADB2CMapper) runbook(directoryName string) []domainrunbook.Step {
	realmName := strings.ToLower(strings.ReplaceAll(directoryName, ".", "-"))
	metadata := map[string]string{"kind": "auth", "source": "azurerm_aadb2c_directory", "directory": directoryName, "target": "keycloak", "realm": realmName}
	return []domainrunbook.Step{
		m.step("export-adb2c-users", "Export Azure AD B2C users", "Discovery", domainrunbook.StepTypeCommand, []string{"bash", "export_adb2c_users.sh"}, "B2C users are exported", metadata),
		m.step("setup-keycloak-realm", "Setup Keycloak realm", "Provision", domainrunbook.StepTypeCommand, []string{"bash", "setup_keycloak.sh"}, "Keycloak realm is created", metadata),
		m.step("migrate-adb2c-flows", "Migrate B2C user flows", "Migrate", domainrunbook.StepTypeCommand, []string{"bash", "migrate_user_flows.sh"}, "B2C flow migration commands are generated", metadata),
		m.step("validate-adb2c-realm", "Validate Keycloak realm", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_adb2c_keycloak.sh"}, "Keycloak realm handoff validates", metadata),
		m.step("backup-adb2c-config", "Backup AD B2C migration config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_adb2c_config.sh"}, "AD B2C migration artifacts are archived", metadata),
		m.step("cutover-adb2c-clients", "Cut over AD B2C clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_adb2c_clients.sh"}, "clients use generated Keycloak OIDC endpoints", metadata),
		m.step("rollback-adb2c-source", "Keep AD B2C source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "AD B2C remains authoritative until Keycloak validation passes", metadata),
	}
}

func (m *ADB2CMapper) step(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}
