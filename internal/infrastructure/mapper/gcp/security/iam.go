// Package security provides mappers for GCP security services.
package security

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/policy"
	"github.com/homeport/homeport/internal/domain/resource"
)

// IAMMapper converts GCP IAM bindings and policies to Keycloak roles.
type IAMMapper struct {
	*mapper.BaseMapper
}

// NewIAMMapper creates a new GCP IAM to Keycloak mapper.
func NewIAMMapper() *IAMMapper {
	return &IAMMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeGCPIAM, nil),
	}
}

// Map converts a GCP IAM binding to Keycloak configuration.
func (m *IAMMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	projectID := res.GetConfigString("project")
	if projectID == "" {
		projectID = res.Name
	}

	member := res.GetConfigString("member")
	role := res.GetConfigString("role")

	result := mapper.NewMappingResult("keycloak-iam")
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
		"homeport.source":     "google_project_iam_member",
		"homeport.project_id": projectID,
		"homeport.role":       role,
		"traefik.enable":       "true",
	}
	svc.Restart = "unless-stopped"
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD-SHELL", "curl -f http://localhost:8080/health/ready || exit 1"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  5,
	}

	// Generate realm configuration
	realmConfig := m.generateRealmConfig(projectID, role)
	result.AddConfig("config/keycloak/gcp-iam-realm.json", []byte(realmConfig))

	// Generate role mapping
	roleMapping := m.generateRoleMapping(res, role, member)
	result.AddConfig("config/keycloak/gcp-role-mapping.json", []byte(roleMapping))

	// Generate Postgres service config
	postgresConfig := m.generatePostgresConfig()
	result.AddConfig("config/keycloak/postgres-service.yml", []byte(postgresConfig))

	// Generate setup script
	setupScript := m.generateSetupScript(projectID)
	result.AddScript("setup_gcp_iam.sh", []byte(setupScript))

	// Generate migration script
	migrationScript := m.generateMigrationScript(projectID, role, member)
	result.AddScript("migrate_gcp_iam.sh", []byte(migrationScript))

	// Handle conditions if present
	if condition := res.Config["condition"]; condition != nil {
		m.handleCondition(condition, result)
	}

	// Add manual steps and warnings
	result.AddManualStep("Access Keycloak at http://localhost:8080")
	result.AddManualStep("Import realm from config/keycloak/gcp-iam-realm.json")
	result.AddManualStep("Map GCP IAM roles to Keycloak roles based on your application needs")
	result.AddManualStep("Update application code to use Keycloak for authorization")
	result.AddWarning("GCP IAM conditions require manual translation to Keycloak policies")
	result.AddWarning("Service account bindings need to be recreated as Keycloak service accounts")

	return result, nil
}

func (m *IAMMapper) generateRealmConfig(projectID, gcpRole string) string {
	realmName := strings.ToLower(strings.ReplaceAll(projectID, "_", "-"))

	// Map GCP role to Keycloak roles
	keycloakRoles := m.mapGCPRoleToKeycloakRoles(gcpRole)

	realmRoles := make([]map[string]string, 0, len(keycloakRoles))
	for _, r := range keycloakRoles {
		realmRoles = append(realmRoles, map[string]string{
			"name":        r,
			"description": fmt.Sprintf("Mapped from GCP role: %s", gcpRole),
		})
	}

	realm := map[string]interface{}{
		"realm":                       "gcp-" + realmName,
		"enabled":                     true,
		"displayName":                 "GCP IAM: " + projectID,
		"registrationAllowed":         false,
		"registrationEmailAsUsername": true,
		"resetPasswordAllowed":        true,
		"loginWithEmailAllowed":       true,
		"verifyEmail":                 true,
		"bruteForceProtected":         true,
		"roles": map[string]interface{}{
			"realm": realmRoles,
		},
		"clients": []map[string]interface{}{
			{
				"clientId":                  projectID + "-service",
				"enabled":                   true,
				"serviceAccountsEnabled":    true,
				"clientAuthenticatorType":   "client-secret",
				"standardFlowEnabled":       false,
				"directAccessGrantsEnabled": true,
			},
		},
		"attributes": map[string]string{
			"migratedFrom":     "GCP IAM",
			"projectId":        projectID,
			"originalGCPRole":  gcpRole,
		},
	}

	data, _ := json.MarshalIndent(realm, "", "  ")
	return string(data)
}

func (m *IAMMapper) generateRoleMapping(res *resource.AWSResource, gcpRole, member string) string {
	keycloakRoles := m.mapGCPRoleToKeycloakRoles(gcpRole)
	permissions := m.extractPermissionsFromGCPRole(gcpRole)

	mapping := map[string]interface{}{
		"gcpRole":       gcpRole,
		"gcpMember":     member,
		"keycloakRoles": keycloakRoles,
		"permissions":   permissions,
		"roleMapping":   m.generateDetailedRoleMapping(gcpRole),
		"notes":         "Review and adjust Keycloak roles based on your application needs",
	}

	data, _ := json.MarshalIndent(mapping, "", "  ")
	return string(data)
}

func (m *IAMMapper) mapGCPRoleToKeycloakRoles(gcpRole string) []string {
	roleSet := make(map[string]bool)

	// Normalize role name
	roleLower := strings.ToLower(gcpRole)

	// Map predefined GCP roles to Keycloak roles
	// Check service-specific patterns first before generic owner/admin
	switch {
	case strings.Contains(roleLower, "storage"):
		roleSet["storage-access"] = true
	case strings.Contains(roleLower, "compute"):
		roleSet["compute-access"] = true
	case strings.Contains(roleLower, "bigquery") || strings.Contains(roleLower, "datastore") || strings.Contains(roleLower, "sql"):
		roleSet["database-access"] = true
	case strings.Contains(roleLower, "pubsub") || strings.Contains(roleLower, "cloudtasks"):
		roleSet["messaging-access"] = true
	case strings.Contains(roleLower, "secretmanager") || strings.Contains(roleLower, "kms"):
		roleSet["secrets-access"] = true
	case strings.Contains(roleLower, "logging") || strings.Contains(roleLower, "monitoring"):
		roleSet["monitoring-access"] = true
	case strings.Contains(roleLower, "cloudfunctions") || strings.Contains(roleLower, "run"):
		roleSet["compute-access"] = true
		roleSet["service"] = true
	case strings.Contains(roleLower, "serviceaccount"):
		roleSet["service"] = true
	case strings.Contains(roleLower, "owner"):
		roleSet["admin"] = true
		roleSet["manage-users"] = true
		roleSet["manage-clients"] = true
		roleSet["manage-realm"] = true
	case strings.Contains(roleLower, "editor"):
		roleSet["user"] = true
		roleSet["manage-clients"] = true
		roleSet["view-users"] = true
	case strings.Contains(roleLower, "viewer") || strings.Contains(roleLower, "readonly"):
		roleSet["view-users"] = true
		roleSet["view-clients"] = true
	default:
		roleSet["user"] = true
	}

	// Convert map to slice
	var roles []string
	for role := range roleSet {
		roles = append(roles, role)
	}

	if len(roles) == 0 {
		roles = []string{"user"}
	}

	return roles
}

func (m *IAMMapper) extractPermissionsFromGCPRole(gcpRole string) []string {
	permSet := make(map[string]bool)

	roleLower := strings.ToLower(gcpRole)

	// Map GCP role patterns to permissions
	permissionMappings := map[string][]string{
		"storage.admin":               {"storage:admin", "storage:read", "storage:write", "storage:delete"},
		"storage.objectviewer":        {"storage:read"},
		"storage.objectcreator":       {"storage:write"},
		"compute.admin":               {"compute:admin", "compute:read", "compute:write"},
		"compute.viewer":              {"compute:read"},
		"cloudsql.admin":              {"database:admin", "database:read", "database:write"},
		"cloudsql.client":             {"database:read", "database:write"},
		"cloudsql.viewer":             {"database:read"},
		"pubsub.admin":                {"messaging:admin", "messaging:publish", "messaging:subscribe"},
		"pubsub.publisher":            {"messaging:publish"},
		"pubsub.subscriber":           {"messaging:subscribe"},
		"secretmanager.admin":         {"secrets:admin", "secrets:read", "secrets:write"},
		"secretmanager.secretaccessor": {"secrets:read"},
		"logging.admin":               {"monitoring:admin", "monitoring:read", "monitoring:write"},
		"logging.viewer":              {"monitoring:read"},
		"cloudfunctions.admin":        {"compute:admin", "compute:invoke"},
		"cloudfunctions.invoker":      {"compute:invoke"},
		"run.admin":                   {"compute:admin", "compute:invoke"},
		"run.invoker":                 {"compute:invoke"},
		"owner":                       {"admin:all"},
		"editor":                      {"write:all", "read:all"},
		"viewer":                      {"read:all"},
	}

	for pattern, perms := range permissionMappings {
		if strings.Contains(roleLower, pattern) {
			for _, p := range perms {
				permSet[p] = true
			}
		}
	}

	var permissions []string
	for perm := range permSet {
		permissions = append(permissions, perm)
	}

	if len(permissions) == 0 {
		permissions = []string{"view-profile"}
	}

	return permissions
}

func (m *IAMMapper) generateDetailedRoleMapping(gcpRole string) []map[string]interface{} {
	var mappings []map[string]interface{}

	// Extract service and role from GCP role format: roles/service.role
	parts := strings.Split(gcpRole, "/")
	rolePart := gcpRole
	if len(parts) > 1 {
		rolePart = parts[1]
	}

	serviceParts := strings.Split(rolePart, ".")
	service := "general"
	roleType := rolePart
	if len(serviceParts) >= 2 {
		service = serviceParts[0]
		roleType = strings.Join(serviceParts[1:], ".")
	}

	mapping := map[string]interface{}{
		"gcpService":  service,
		"gcpRoleType": roleType,
		"keycloakMapping": map[string]interface{}{
			"realm":        "gcp-project",
			"clientScopes": m.mapServiceToClientScopes(service),
			"permissions":  m.extractPermissionsFromGCPRole(gcpRole),
		},
	}

	mappings = append(mappings, mapping)
	return mappings
}

func (m *IAMMapper) mapServiceToClientScopes(service string) []string {
	scopeMap := map[string][]string{
		"storage":        {"storage:read", "storage:write"},
		"compute":        {"compute:manage"},
		"cloudsql":       {"database:access"},
		"pubsub":         {"messaging:access"},
		"secretmanager":  {"secrets:access"},
		"cloudfunctions": {"functions:invoke"},
		"run":            {"cloudrun:invoke"},
		"logging":        {"logs:read"},
		"monitoring":     {"metrics:read"},
	}

	if scopes, ok := scopeMap[service]; ok {
		return scopes
	}
	return []string{"default"}
}

func (m *IAMMapper) generatePostgresConfig() string {
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

func (m *IAMMapper) generateSetupScript(projectID string) string {
	realmName := "gcp-" + strings.ToLower(strings.ReplaceAll(projectID, "_", "-"))
	return fmt.Sprintf(`#!/bin/bash
set -e

KEYCLOAK_URL="http://localhost:8080"
REALM="%s"

echo "GCP IAM to Keycloak Setup"
echo "========================="

echo "Waiting for Keycloak..."
until curl -sf "$KEYCLOAK_URL/health/ready" > /dev/null; do
  sleep 5
done

TOKEN=$(curl -s -X POST "$KEYCLOAK_URL/realms/master/protocol/openid-connect/token" \
  -d "username=admin&password=admin&grant_type=password&client_id=admin-cli" | jq -r '.access_token')

echo "Creating realm: $REALM"
curl -X POST "$KEYCLOAK_URL/admin/realms" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d @config/keycloak/gcp-iam-realm.json

echo "Realm $REALM created!"
echo ""
echo "Next steps:"
echo "1. Import role mappings from config/keycloak/gcp-role-mapping.json"
echo "2. Create service accounts for your applications"
echo "3. Update application code to use Keycloak for authorization"
`, realmName)
}

func (m *IAMMapper) generateMigrationScript(projectID, role, member string) string {
	return fmt.Sprintf(`#!/bin/bash
set -e

PROJECT_ID="%s"
GCP_ROLE="%s"
GCP_MEMBER="%s"

echo "GCP IAM Migration Guide"
echo "======================="
echo ""
echo "Source Configuration:"
echo "  Project: $PROJECT_ID"
echo "  Role: $GCP_ROLE"
echo "  Member: $GCP_MEMBER"
echo ""
echo "Migration Steps:"
echo ""
echo "1. Export GCP IAM bindings:"
echo "   gcloud projects get-iam-policy $PROJECT_ID --format=json > iam-policy.json"
echo ""
echo "2. Review the exported policy and map roles to Keycloak:"
echo "   - roles/owner -> admin, manage-users, manage-clients"
echo "   - roles/editor -> user, manage-clients"
echo "   - roles/viewer -> view-users, view-clients"
echo "   - Custom roles need manual mapping"
echo ""
echo "3. Create corresponding users/groups in Keycloak"
echo ""
echo "4. Update application authorization:"
echo "   - Replace GCP IAM checks with Keycloak role checks"
echo "   - Update OAuth2/OIDC configuration to use Keycloak"
echo ""
echo "5. For service accounts:"
echo "   - Create Keycloak service account clients"
echo "   - Use client credentials flow for authentication"
echo ""
echo "Note: GCP IAM conditions need to be reimplemented as Keycloak policies"
`, projectID, role, member)
}

func (m *IAMMapper) handleCondition(condition interface{}, result *mapper.MappingResult) {
	if condMap, ok := condition.(map[string]interface{}); ok {
		title := ""
		expression := ""
		if t, ok := condMap["title"].(string); ok {
			title = t
		}
		if e, ok := condMap["expression"].(string); ok {
			expression = e
		}

		if title != "" || expression != "" {
			result.AddWarning(fmt.Sprintf("IAM condition detected: %s", title))
			result.AddManualStep(fmt.Sprintf("Translate IAM condition to Keycloak policy: %s", expression))
		}
	}
}

// ExtractPolicies extracts IAM bindings as policies.
func (m *IAMMapper) ExtractPolicies(ctx context.Context, res *resource.AWSResource) ([]*policy.Policy, error) {
	var policies []*policy.Policy

	projectID := res.GetConfigString("project")
	if projectID == "" {
		projectID = res.Name
	}

	member := res.GetConfigString("member")
	role := res.GetConfigString("role")

	// Create IAM binding policy
	bindingJSON, _ := json.Marshal(map[string]interface{}{
		"project": projectID,
		"role":    role,
		"member":  member,
	})

	p := policy.NewPolicy(
		res.ID+"-iam-binding",
		fmt.Sprintf("GCP IAM: %s for %s", role, member),
		policy.PolicyTypeIAM,
		policy.ProviderGCP,
	)
	p.ResourceID = res.ID
	p.ResourceType = "google_project_iam_member"
	p.ResourceName = projectID
	p.OriginalDocument = bindingJSON
	p.OriginalFormat = "json"
	p.NormalizedPolicy = m.normalizeIAMBinding(role, member)

	// Add warnings for special roles
	if strings.Contains(role, "owner") {
		p.AddWarning("Owner role grants full access to the project")
	}
	if strings.Contains(role, "editor") {
		p.AddWarning("Editor role grants broad write access")
	}
	if strings.HasPrefix(member, "allUsers") {
		p.AddWarning("Binding grants access to all users (public)")
	}
	if strings.HasPrefix(member, "allAuthenticatedUsers") {
		p.AddWarning("Binding grants access to all authenticated users")
	}

	// Handle conditions
	if condition := res.Config["condition"]; condition != nil {
		if condMap, ok := condition.(map[string]interface{}); ok {
			if title, ok := condMap["title"].(string); ok {
				p.AddWarning(fmt.Sprintf("IAM condition: %s", title))
			}
		}
	}

	policies = append(policies, p)

	return policies, nil
}

// normalizeIAMBinding converts a GCP IAM binding to normalized format.
func (m *IAMMapper) normalizeIAMBinding(role, member string) *policy.NormalizedPolicy {
	normalized := &policy.NormalizedPolicy{
		Statements: make([]policy.Statement, 0),
	}

	// Parse member into principal
	principalType := "user"
	principalID := member
	if strings.HasPrefix(member, "serviceAccount:") {
		principalType = "serviceAccount"
		principalID = strings.TrimPrefix(member, "serviceAccount:")
	} else if strings.HasPrefix(member, "group:") {
		principalType = "group"
		principalID = strings.TrimPrefix(member, "group:")
	} else if strings.HasPrefix(member, "user:") {
		principalID = strings.TrimPrefix(member, "user:")
	} else if member == "allUsers" {
		principalType = "*"
		principalID = "*"
	}

	// Extract actions from role
	actions := m.extractPermissionsFromGCPRole(role)

	stmt := policy.Statement{
		Effect: policy.EffectAllow,
		Principals: []policy.Principal{
			{Type: principalType, ID: principalID},
		},
		Actions:   actions,
		Resources: []string{"*"}, // GCP IAM bindings apply to the project
	}

	normalized.Statements = append(normalized.Statements, stmt)

	return normalized
}
