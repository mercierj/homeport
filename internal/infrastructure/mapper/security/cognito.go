// Package security provides mappers for AWS security services.
package security

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cloudexit/cloudexit/internal/domain/mapper"
	"github.com/cloudexit/cloudexit/internal/domain/resource"
)

// CognitoMapper converts AWS Cognito User Pools to Keycloak.
type CognitoMapper struct {
	*mapper.BaseMapper
}

// NewCognitoMapper creates a new Cognito to Keycloak mapper.
func NewCognitoMapper() *CognitoMapper {
	return &CognitoMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeCognitoPool, nil),
	}
}

// Map converts a Cognito User Pool to a Keycloak service.
func (m *CognitoMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	poolName := res.GetConfigString("name")
	if poolName == "" {
		poolName = res.Name
	}

	// Create Keycloak service using new API
	result := mapper.NewMappingResult("keycloak")
	keycloakSvc := result.DockerService

	// Configure Keycloak service
	keycloakSvc.Image = "quay.io/keycloak/keycloak:23.0"
	keycloakSvc.Environment = map[string]string{
		"KEYCLOAK_ADMIN":          "admin",
		"KEYCLOAK_ADMIN_PASSWORD": "admin",
		"KC_DB":                   "postgres",
		"KC_DB_URL":               "jdbc:postgresql://postgres-keycloak:5432/keycloak",
		"KC_DB_USERNAME":          "keycloak",
		"KC_DB_PASSWORD":          "keycloak",
		"KC_HOSTNAME":             "localhost",
		"KC_HTTP_ENABLED":         "true",
		"KC_HEALTH_ENABLED":       "true",
	}
	keycloakSvc.Ports = []string{
		"8080:8080",
	}
	keycloakSvc.Command = []string{
		"start-dev",
	}
	keycloakSvc.DependsOn = []string{"postgres-keycloak"}
	keycloakSvc.Volumes = []string{
		"./config/keycloak:/opt/keycloak/data/import",
	}
	keycloakSvc.Networks = []string{"cloudexit"}
	keycloakSvc.Labels = map[string]string{
		"cloudexit.source":    "aws_cognito_user_pool",
		"cloudexit.pool_name": poolName,
		"traefik.enable":      "true",
		"traefik.http.routers.keycloak.rule":                      "Host(`keycloak.localhost`)",
		"traefik.http.services.keycloak.loadbalancer.server.port": "8080",
	}
	keycloakSvc.Restart = "unless-stopped"

	// Add health check
	keycloakSvc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD-SHELL", "exec 3<>/dev/tcp/localhost/8080 && echo -e 'GET /health/ready HTTP/1.1\\r\\nHost: localhost\\r\\n\\r\\n' >&3 && cat <&3 | grep -q '200 OK'"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  5,
	}

	// Note: We need to handle PostgreSQL service separately since we can only have one DockerService
	// We'll add it as a manual step
	result.AddManualStep("Add PostgreSQL service for Keycloak to docker-compose.yml (see generated postgres service config)")

	// Generate PostgreSQL service config as a separate file
	postgresConfig := m.generatePostgresServiceConfig(poolName)
	result.AddConfig("config/keycloak/postgres-service.yml", []byte(postgresConfig))

	// Generate Keycloak realm configuration
	realmConfig := m.generateRealmConfig(res, poolName)
	result.AddConfig("config/keycloak/realm.json", []byte(realmConfig))

	// Generate password policy configuration
	passwordPolicy := m.extractPasswordPolicy(res)
	if passwordPolicy != "" {
		result.AddWarning(fmt.Sprintf("Password policy: %s", passwordPolicy))
	}

	// Handle MFA configuration
	if m.hasMFAEnabled(res) {
		result.AddWarning("MFA is enabled in Cognito. Configure OTP in Keycloak realm settings.")
		result.AddManualStep("Enable and configure OTP in Keycloak: Realm Settings > Authentication > Required Actions")
	}

	// Handle email configuration
	if emailConfig := res.Config["email_configuration"]; emailConfig != nil {
		if emailMap, ok := emailConfig.(map[string]interface{}); ok {
			m.handleEmailConfiguration(emailMap, result)
		}
	}

	// Handle SMS configuration (for MFA)
	if smsConfig := res.Config["sms_configuration"]; smsConfig != nil {
		result.AddWarning("SMS configuration detected. Keycloak requires custom SPI for SMS OTP.")
		result.AddManualStep("Configure SMS provider in Keycloak using a custom SPI or third-party integration")
	}

	// Handle user pool clients
	if clients := m.extractUserPoolClients(res); len(clients) > 0 {
		clientsConfig := m.generateClientsConfig(clients, poolName)
		result.AddConfig("config/keycloak/clients.json", []byte(clientsConfig))
	}

	// Generate setup script
	setupScript := m.generateSetupScript(poolName)
	result.AddScript("setup_keycloak.sh", []byte(setupScript))

	// Generate user migration script
	migrationScript := m.generateUserMigrationScript(poolName)
	result.AddScript("migrate_users.sh", []byte(migrationScript))

	result.AddManualStep("Access Keycloak admin console at http://localhost:8080")
	result.AddManualStep("Default admin credentials: admin/admin (change immediately)")
	result.AddManualStep("Import realm configuration from config/keycloak/realm.json")
	result.AddManualStep("Configure email server settings in Keycloak")
	result.AddManualStep("Migrate users from Cognito using the provided script")

	result.AddWarning("Cognito user migration requires exporting users from AWS and importing to Keycloak")
	result.AddWarning("Password hashes cannot be migrated - users will need to reset passwords")

	return result, nil
}

// generatePostgresServiceConfig creates a PostgreSQL service configuration.
func (m *CognitoMapper) generatePostgresServiceConfig(poolName string) string {
	return `# PostgreSQL service for Keycloak
# Add this to your docker-compose.yml services section:

postgres-keycloak:
  image: postgres:16-alpine
  environment:
    POSTGRES_DB: keycloak
    POSTGRES_USER: keycloak
    POSTGRES_PASSWORD: keycloak
  volumes:
    - ./data/postgres-keycloak:/var/lib/postgresql/data
  networks:
    - cloudexit
  healthcheck:
    test: ["CMD-SHELL", "pg_isready -U keycloak"]
    interval: 10s
    timeout: 5s
    retries: 5
  restart: unless-stopped
  labels:
    cloudexit.service: keycloak-database
`
}

// generateRealmConfig generates a Keycloak realm configuration.
func (m *CognitoMapper) generateRealmConfig(res *resource.AWSResource, poolName string) string {
	realmName := m.sanitizeRealmName(poolName)

	realm := map[string]interface{}{
		"realm":   realmName,
		"enabled": true,
		"displayName": poolName,
		"registrationAllowed": true,
		"registrationEmailAsUsername": true,
		"rememberMe": true,
		"verifyEmail": res.GetConfigBool("auto_verified_attributes.email") || m.hasAutoVerifiedAttribute(res, "email"),
		"loginWithEmailAllowed": true,
		"duplicateEmailsAllowed": false,
		"resetPasswordAllowed": true,
		"editUsernameAllowed": false,
		"bruteForceProtected": true,
		"permanentLockout": false,
		"maxFailureWaitSeconds": 900,
		"minimumQuickLoginWaitSeconds": 60,
		"waitIncrementSeconds": 60,
		"quickLoginCheckMilliSeconds": 1000,
		"maxDeltaTimeSeconds": 43200,
		"failureFactor": 30,
	}

	// Extract user attributes
	if attrs := res.Config["schema"]; attrs != nil {
		if attrSlice, ok := attrs.([]interface{}); ok {
			userAttributes := []map[string]interface{}{}
			for _, attr := range attrSlice {
				if attrMap, ok := attr.(map[string]interface{}); ok {
					name := ""
					if n, ok := attrMap["name"].(string); ok {
						name = n
					}

					required := false
					if r, ok := attrMap["required"].(bool); ok {
						required = r
					}

					userAttributes = append(userAttributes, map[string]interface{}{
						"name":     name,
						"required": required,
					})
				}
			}
			realm["attributes"] = userAttributes
		}
	}

	// Add password policy
	if passwordPolicy := m.extractPasswordPolicy(res); passwordPolicy != "" {
		realm["passwordPolicy"] = passwordPolicy
	}

	// Convert to JSON
	realmJSON, _ := json.MarshalIndent(realm, "", "  ")

	return string(realmJSON)
}

// generateClientsConfig generates client configurations.
func (m *CognitoMapper) generateClientsConfig(clients []UserPoolClient, poolName string) string {
	clientsJSON := []map[string]interface{}{}

	for _, client := range clients {
		clientConfig := map[string]interface{}{
			"clientId":                   client.ClientID,
			"name":                       client.ClientName,
			"enabled":                    true,
			"publicClient":               !client.GenerateSecret,
			"standardFlowEnabled":        true,
			"implicitFlowEnabled":        client.AllowedOAuthFlows["implicit"],
			"directAccessGrantsEnabled": client.AllowedOAuthFlows["password"],
			"redirectUris":               client.CallbackURLs,
			"webOrigins":                 client.CallbackURLs,
		}

		if len(client.AllowedOAuthScopes) > 0 {
			clientConfig["defaultClientScopes"] = client.AllowedOAuthScopes
		}

		clientsJSON = append(clientsJSON, clientConfig)
	}

	content, _ := json.MarshalIndent(clientsJSON, "", "  ")

	return string(content)
}

// generateSetupScript creates a Keycloak setup script.
func (m *CognitoMapper) generateSetupScript(poolName string) string {
	realmName := m.sanitizeRealmName(poolName)

	script := fmt.Sprintf(`#!/bin/bash
# Keycloak Setup Script
# Sets up Keycloak realm from Cognito User Pool configuration

set -e

KEYCLOAK_URL="http://localhost:8080"
ADMIN_USER="admin"
ADMIN_PASS="admin"
REALM_NAME="%s"

echo "Waiting for Keycloak to be ready..."
until curl -sf "$KEYCLOAK_URL/health/ready" > /dev/null; do
  echo "Waiting..."
  sleep 5
done

echo "Keycloak is ready!"

# Get admin token
echo "Getting admin token..."
TOKEN=$(curl -X POST "$KEYCLOAK_URL/realms/master/protocol/openid-connect/token" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "username=$ADMIN_USER" \
  -d "password=$ADMIN_PASS" \
  -d "grant_type=password" \
  -d "client_id=admin-cli" \
  | jq -r '.access_token')

# Create realm
echo "Creating realm: $REALM_NAME"
curl -X POST "$KEYCLOAK_URL/admin/realms" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d @config/keycloak/realm.json

# Import clients if they exist
if [ -f config/keycloak/clients.json ]; then
  echo "Importing clients..."
  jq -c '.[]' config/keycloak/clients.json | while read client; do
    curl -X POST "$KEYCLOAK_URL/admin/realms/$REALM_NAME/clients" \
      -H "Authorization: Bearer $TOKEN" \
      -H "Content-Type: application/json" \
      -d "$client"
  done
fi

echo "Keycloak setup complete!"
echo "Access the admin console at: $KEYCLOAK_URL"
echo "Realm: $REALM_NAME"
`, realmName)

	return script
}

// generateUserMigrationScript creates a user migration script.
func (m *CognitoMapper) generateUserMigrationScript(poolName string) string {
	realmName := m.sanitizeRealmName(poolName)

	script := fmt.Sprintf(`#!/bin/bash
# User Migration Script
# Migrates users from AWS Cognito to Keycloak

set -e

COGNITO_POOL_ID="${COGNITO_POOL_ID:-your-pool-id}"
KEYCLOAK_URL="http://localhost:8080"
REALM_NAME="%s"

echo "User Migration from Cognito to Keycloak"
echo "========================================"

echo "Step 1: Export users from Cognito"
echo "Run: aws cognito-idp list-users --user-pool-id $COGNITO_POOL_ID > cognito_users.json"
echo ""

echo "Step 2: Transform user data"
echo "The following script transforms Cognito users to Keycloak format:"
echo ""

cat > transform_users.py <<'PYTHON'
import json
import sys

# Read Cognito users
with open('cognito_users.json', 'r') as f:
    cognito_data = json.load(f)

keycloak_users = []

for user in cognito_data.get('Users', []):
    username = user['Username']

    # Extract attributes
    attributes = {}
    for attr in user.get('Attributes', []):
        attributes[attr['Name']] = attr['Value']

    keycloak_user = {
        'username': username,
        'email': attributes.get('email', ''),
        'emailVerified': user.get('UserStatus') == 'CONFIRMED',
        'enabled': user.get('Enabled', True),
        'attributes': {
            'phone_number': [attributes.get('phone_number', '')],
            'cognito_sub': [attributes.get('sub', '')]
        },
        'requiredActions': ['UPDATE_PASSWORD']  # Users must reset password
    }

    keycloak_users.append(keycloak_user)

print(json.dumps(keycloak_users, indent=2))
PYTHON

echo "Run: python transform_users.py > keycloak_users.json"
echo ""

echo "Step 3: Import users to Keycloak"
echo "Get admin token..."
TOKEN=$(curl -X POST "$KEYCLOAK_URL/realms/master/protocol/openid-connect/token" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "username=admin" \
  -d "password=admin" \
  -d "grant_type=password" \
  -d "client_id=admin-cli" \
  | jq -r '.access_token')

echo "Importing users..."
jq -c '.[]' keycloak_users.json | while read user; do
  curl -X POST "$KEYCLOAK_URL/admin/realms/$REALM_NAME/users" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d "$user"
done

echo "Migration complete!"
echo "Note: Users will need to reset their passwords on first login"
`, realmName)

	return script
}

// extractPasswordPolicy extracts password policy from Cognito configuration.
func (m *CognitoMapper) extractPasswordPolicy(res *resource.AWSResource) string {
	policies := []string{}

	if pwdPolicy := res.Config["password_policy"]; pwdPolicy != nil {
		if pwdMap, ok := pwdPolicy.(map[string]interface{}); ok {
			if minLen, ok := pwdMap["minimum_length"].(float64); ok {
				policies = append(policies, fmt.Sprintf("length(%d)", int(minLen)))
			}

			if requireLower, ok := pwdMap["require_lowercase"].(bool); ok && requireLower {
				policies = append(policies, "lowerCase(1)")
			}

			if requireUpper, ok := pwdMap["require_uppercase"].(bool); ok && requireUpper {
				policies = append(policies, "upperCase(1)")
			}

			if requireNum, ok := pwdMap["require_numbers"].(bool); ok && requireNum {
				policies = append(policies, "digits(1)")
			}

			if requireSymbol, ok := pwdMap["require_symbols"].(bool); ok && requireSymbol {
				policies = append(policies, "specialChars(1)")
			}
		}
	}

	return strings.Join(policies, " and ")
}

// hasMFAEnabled checks if MFA is enabled.
func (m *CognitoMapper) hasMFAEnabled(res *resource.AWSResource) bool {
	mfaConfig := res.GetConfigString("mfa_configuration")
	return mfaConfig == "ON" || mfaConfig == "OPTIONAL"
}

// hasAutoVerifiedAttribute checks if an attribute is auto-verified.
func (m *CognitoMapper) hasAutoVerifiedAttribute(res *resource.AWSResource, attrName string) bool {
	if attrs := res.Config["auto_verified_attributes"]; attrs != nil {
		if attrSlice, ok := attrs.([]interface{}); ok {
			for _, attr := range attrSlice {
				if str, ok := attr.(string); ok && str == attrName {
					return true
				}
			}
		}
	}
	return false
}

// handleEmailConfiguration processes email configuration.
func (m *CognitoMapper) handleEmailConfiguration(emailConfig map[string]interface{}, result *mapper.MappingResult) {
	if sourceArn, ok := emailConfig["source_arn"].(string); ok {
		result.AddWarning(fmt.Sprintf("SES configuration detected: %s. Configure SMTP in Keycloak.", sourceArn))
	}

	result.AddManualStep("Configure email server in Keycloak: Realm Settings > Email")
}

// UserPoolClient represents a Cognito user pool client.
type UserPoolClient struct {
	ClientID            string
	ClientName          string
	GenerateSecret      bool
	CallbackURLs        []string
	LogoutURLs          []string
	AllowedOAuthFlows   map[string]bool
	AllowedOAuthScopes  []string
}

// extractUserPoolClients extracts user pool client information.
func (m *CognitoMapper) extractUserPoolClients(res *resource.AWSResource) []UserPoolClient {
	var clients []UserPoolClient

	// Note: In a real implementation, you would fetch user pool client resources
	// separately as they are typically separate Terraform resources

	return clients
}

// sanitizeRealmName sanitizes the realm name.
func (m *CognitoMapper) sanitizeRealmName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "_", "-")
	name = strings.ReplaceAll(name, " ", "-")

	validName := ""
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' {
			validName += string(ch)
		}
	}

	if validName == "" {
		validName = "app-realm"
	}

	return validName
}
