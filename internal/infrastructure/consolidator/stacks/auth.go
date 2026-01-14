// Package stacks provides stack-specific merger implementations for consolidation.
package stacks

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	"github.com/homeport/homeport/internal/domain/stack"
	"github.com/homeport/homeport/internal/infrastructure/consolidator"
)

// AuthMerger consolidates identity/auth resources into a single Keycloak stack.
// It handles Cognito (AWS), Identity Platform (GCP), and Azure AD B2C resources,
// converting them into a unified Keycloak deployment.
type AuthMerger struct {
	*consolidator.BaseMerger
}

// NewAuthMerger creates a new AuthMerger.
func NewAuthMerger() *AuthMerger {
	return &AuthMerger{
		BaseMerger: consolidator.NewBaseMerger(stack.StackTypeAuth),
	}
}

// StackType returns the stack type this merger handles.
func (m *AuthMerger) StackType() stack.StackType {
	return stack.StackTypeAuth
}

// CanMerge checks if this merger can handle the given results.
// Returns true if there are any authentication-related resources.
func (m *AuthMerger) CanMerge(results []*mapper.MappingResult) bool {
	if len(results) == 0 {
		return false
	}

	for _, result := range results {
		if result == nil {
			continue
		}
		if isAuthResource(result.SourceResourceType) {
			return true
		}
	}
	return false
}

// isAuthResource checks if a resource type is an authentication resource.
func isAuthResource(resourceType string) bool {
	authTypes := []string{
		// AWS Cognito
		string(resource.TypeCognitoPool),
		"aws_cognito_user_pool_client",
		"aws_cognito_identity_pool",
		// GCP Identity Platform
		string(resource.TypeIdentityPlatform),
		"google_identity_platform_tenant",
		"google_identity_platform_oauth_idp_config",
		// Azure AD B2C
		string(resource.TypeAzureADB2C),
		"azurerm_aadb2c_directory",
	}

	for _, t := range authTypes {
		if resourceType == t {
			return true
		}
	}
	return false
}

// Merge creates a consolidated auth stack from identity resources.
// It generates:
// - Keycloak server as the primary identity provider
// - Realm configurations for each user pool/directory
// - Migration scripts for user data
func (m *AuthMerger) Merge(ctx context.Context, results []*mapper.MappingResult, opts *consolidator.MergeOptions) (*stack.Stack, error) {
	if opts == nil {
		opts = consolidator.DefaultOptions()
	}

	// Create the auth stack
	name := "auth"
	if opts.NamePrefix != "" {
		name = opts.NamePrefix + "-" + name
	}

	stk := stack.NewStack(stack.StackTypeAuth, name)
	stk.Description = "Identity and authentication stack (Keycloak)"

	// Keycloak depends on a database for persistence
	stk.AddDependency(stack.StackTypeDatabase)

	// Convert each identity provider to a Keycloak realm
	var realms []*KeycloakRealm
	for _, result := range results {
		if result == nil {
			continue
		}

		var realm *KeycloakRealm
		switch {
		case strings.HasPrefix(result.SourceResourceType, "aws_cognito"):
			realm = m.convertCognitoToRealm(result)
		case strings.HasPrefix(result.SourceResourceType, "google_identity_platform"):
			realm = m.convertIdentityPlatformToRealm(result)
		case strings.HasPrefix(result.SourceResourceType, "azurerm_aadb2c"):
			realm = m.convertADB2CToRealm(result)
		}

		if realm != nil {
			realms = append(realms, realm)
		}

		// Add source resource
		res := &resource.Resource{
			Type: resource.Type(result.SourceResourceType),
			Name: result.SourceResourceName,
		}
		stk.AddSourceResource(res)
	}

	// Generate realm import JSON
	if len(realms) > 0 {
		realmJSON := m.generateRealmImportJSON(realms)
		stk.AddConfig("keycloak/realms/import-realms.json", realmJSON)
	}

	// Generate user migration script
	migrationScript := m.generateUserMigrationScript()
	stk.AddScript("keycloak/migrate-users.sh", migrationScript)

	// Create Keycloak service
	keycloakService := stack.NewService("keycloak", "quay.io/keycloak/keycloak:latest")
	keycloakService.Ports = []string{"8080:8080", "8443:8443"}
	keycloakService.Command = []string{"start"}
	keycloakService.Environment = map[string]string{
		"KC_HOSTNAME":           "${KEYCLOAK_HOSTNAME:-localhost}",
		"KC_HOSTNAME_STRICT":    "false",
		"KC_HTTP_ENABLED":       "true",
		"KC_HEALTH_ENABLED":     "true",
		"KC_METRICS_ENABLED":    "true",
		"KEYCLOAK_ADMIN":        "${KEYCLOAK_ADMIN:-admin}",
		"KEYCLOAK_ADMIN_PASSWORD": "${KEYCLOAK_ADMIN_PASSWORD:-admin}",
		// Database connection (PostgreSQL by default)
		"KC_DB":          "postgres",
		"KC_DB_URL_HOST": "${KC_DB_HOST:-database}",
		"KC_DB_URL_PORT": "${KC_DB_PORT:-5432}",
		"KC_DB_URL_DATABASE": "${KC_DB_NAME:-keycloak}",
		"KC_DB_USERNAME": "${KC_DB_USERNAME:-keycloak}",
		"KC_DB_PASSWORD": "${KC_DB_PASSWORD:-keycloak}",
	}
	keycloakService.Volumes = []string{
		"keycloak-data:/opt/keycloak/data",
		"./keycloak/realms:/opt/keycloak/data/import:ro",
	}
	keycloakService.DependsOn = []string{"database"}
	keycloakService.HealthCheck = &stack.HealthCheck{
		Test:        []string{"CMD", "curl", "-f", "http://localhost:8080/health/ready"},
		Interval:    "30s",
		Timeout:     "10s",
		Retries:     5,
		StartPeriod: "60s",
	}
	keycloakService.Networks = []string{"auth", "backend"}
	keycloakService.Labels = map[string]string{
		"traefik.enable": "true",
		"traefik.http.routers.keycloak.rule": "Host(`${KEYCLOAK_HOSTNAME:-auth.localhost}`)",
		"traefik.http.services.keycloak.loadbalancer.server.port": "8080",
	}
	stk.AddService(keycloakService)

	// Add volumes
	stk.AddVolume(stack.Volume{Name: "keycloak-data", Driver: "local"})

	// Add networks
	stk.AddNetwork(stack.Network{Name: "auth", Driver: "bridge"})
	stk.AddNetwork(stack.Network{Name: "backend", Driver: "bridge"})

	// Add manual steps for migration
	stk.Metadata["manual_step_1"] = "Export users from source identity provider (Cognito/Identity Platform/Azure AD B2C)"
	stk.Metadata["manual_step_2"] = "Import users into Keycloak using the Admin API or migration script"
	stk.Metadata["manual_step_3"] = "Update application OAuth2/OIDC configuration to point to Keycloak"
	stk.Metadata["manual_step_4"] = "Configure email templates and branding in Keycloak Admin Console"
	stk.Metadata["manual_step_5"] = "Set up social login providers (Google, Facebook, etc.) if previously configured"
	stk.Metadata["manual_step_6"] = "Review and configure password policies and MFA settings"

	return stk, nil
}

// KeycloakRealm represents a Keycloak realm configuration.
type KeycloakRealm struct {
	Realm               string            `json:"realm"`
	Enabled             bool              `json:"enabled"`
	DisplayName         string            `json:"displayName,omitempty"`
	Clients             []Client          `json:"clients,omitempty"`
	Users               []User            `json:"users,omitempty"`
	DefaultRoles        []string          `json:"defaultRoles,omitempty"`
	PasswordPolicy      string            `json:"passwordPolicy,omitempty"`
	BruteForceProtected bool              `json:"bruteForceProtected"`
	LoginTheme          string            `json:"loginTheme,omitempty"`
	AccountTheme        string            `json:"accountTheme,omitempty"`
	SMTPServer          map[string]string `json:"smtpServer,omitempty"`
	Attributes          map[string]string `json:"attributes,omitempty"`
}

// Client represents a Keycloak client configuration.
type Client struct {
	ClientID                  string   `json:"clientId"`
	Name                      string   `json:"name,omitempty"`
	Enabled                   bool     `json:"enabled"`
	Protocol                  string   `json:"protocol"`
	PublicClient              bool     `json:"publicClient"`
	StandardFlowEnabled       bool     `json:"standardFlowEnabled"`
	DirectAccessGrantsEnabled bool     `json:"directAccessGrantsEnabled"`
	ServiceAccountsEnabled    bool     `json:"serviceAccountsEnabled"`
	RedirectUris              []string `json:"redirectUris"`
	WebOrigins                []string `json:"webOrigins"`
	Attributes                map[string]string `json:"attributes,omitempty"`
}

// User represents a Keycloak user configuration.
type User struct {
	Username      string           `json:"username"`
	Email         string           `json:"email,omitempty"`
	EmailVerified bool             `json:"emailVerified"`
	Enabled       bool             `json:"enabled"`
	FirstName     string           `json:"firstName,omitempty"`
	LastName      string           `json:"lastName,omitempty"`
	Attributes    map[string][]string `json:"attributes,omitempty"`
	Credentials   []Credential     `json:"credentials,omitempty"`
}

// Credential represents a Keycloak user credential.
type Credential struct {
	Type      string `json:"type"`
	Value     string `json:"value,omitempty"`
	Temporary bool   `json:"temporary"`
}

// convertCognitoToRealm converts an AWS Cognito user pool to a Keycloak realm.
func (m *AuthMerger) convertCognitoToRealm(result *mapper.MappingResult) *KeycloakRealm {
	if result == nil {
		return nil
	}

	realmName := consolidator.NormalizeName(result.SourceResourceName)
	if realmName == "" {
		realmName = "cognito-migration"
	}

	realm := &KeycloakRealm{
		Realm:               realmName,
		Enabled:             true,
		DisplayName:         fmt.Sprintf("Migrated from Cognito: %s", result.SourceResourceName),
		BruteForceProtected: true,
		DefaultRoles:        []string{"user"},
		Attributes: map[string]string{
			"source":          "aws_cognito",
			"original_name":   result.SourceResourceName,
			"migration_notes": "Realm migrated from AWS Cognito User Pool",
		},
	}

	// Try to extract configuration from result
	if result.Configs != nil {
		if configData, ok := result.Configs["user_pool.json"]; ok {
			var poolConfig map[string]interface{}
			if err := json.Unmarshal(configData, &poolConfig); err == nil {
				// Extract password policy
				if policies, ok := poolConfig["password_policy"].(map[string]interface{}); ok {
					realm.PasswordPolicy = convertCognitoPasswordPolicy(policies)
				}
			}
		}

		// Extract app clients
		if clientData, ok := result.Configs["app_clients.json"]; ok {
			var clients []map[string]interface{}
			if err := json.Unmarshal(clientData, &clients); err == nil {
				for _, clientConfig := range clients {
					client := convertCognitoAppClient(clientConfig)
					if client != nil {
						realm.Clients = append(realm.Clients, *client)
					}
				}
			}
		}
	}

	// Add a default client if none were found
	if len(realm.Clients) == 0 {
		realm.Clients = []Client{
			{
				ClientID:                  realmName + "-app",
				Name:                      "Migrated Application",
				Enabled:                   true,
				Protocol:                  "openid-connect",
				PublicClient:              true,
				StandardFlowEnabled:       true,
				DirectAccessGrantsEnabled: true,
				RedirectUris:              []string{"http://localhost:*", "https://localhost:*"},
				WebOrigins:                []string{"*"},
			},
		}
	}

	return realm
}

// convertCognitoPasswordPolicy converts Cognito password policy to Keycloak format.
func convertCognitoPasswordPolicy(policies map[string]interface{}) string {
	var parts []string

	if minLength, ok := policies["minimum_length"].(float64); ok {
		parts = append(parts, fmt.Sprintf("length(%d)", int(minLength)))
	}
	if requireLowercase, ok := policies["require_lowercase"].(bool); ok && requireLowercase {
		parts = append(parts, "lowerCase(1)")
	}
	if requireUppercase, ok := policies["require_uppercase"].(bool); ok && requireUppercase {
		parts = append(parts, "upperCase(1)")
	}
	if requireNumbers, ok := policies["require_numbers"].(bool); ok && requireNumbers {
		parts = append(parts, "digits(1)")
	}
	if requireSymbols, ok := policies["require_symbols"].(bool); ok && requireSymbols {
		parts = append(parts, "specialChars(1)")
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " and ")
}

// convertCognitoAppClient converts a Cognito app client to a Keycloak client.
func convertCognitoAppClient(clientConfig map[string]interface{}) *Client {
	clientID, _ := clientConfig["client_id"].(string)
	if clientID == "" {
		clientID, _ = clientConfig["name"].(string)
	}
	if clientID == "" {
		return nil
	}

	client := &Client{
		ClientID:                  consolidator.NormalizeName(clientID),
		Name:                      clientID,
		Enabled:                   true,
		Protocol:                  "openid-connect",
		PublicClient:              true,
		StandardFlowEnabled:       true,
		DirectAccessGrantsEnabled: true,
		RedirectUris:              []string{"*"},
		WebOrigins:                []string{"*"},
	}

	// Extract callback URLs
	if callbackURLs, ok := clientConfig["callback_urls"].([]interface{}); ok {
		urls := make([]string, 0, len(callbackURLs))
		for _, url := range callbackURLs {
			if u, ok := url.(string); ok {
				urls = append(urls, u)
			}
		}
		if len(urls) > 0 {
			client.RedirectUris = urls
		}
	}

	// Extract allowed OAuth scopes
	if scopes, ok := clientConfig["allowed_oauth_scopes"].([]interface{}); ok {
		client.Attributes = make(map[string]string)
		scopeStrings := make([]string, 0, len(scopes))
		for _, scope := range scopes {
			if s, ok := scope.(string); ok {
				scopeStrings = append(scopeStrings, s)
			}
		}
		if len(scopeStrings) > 0 {
			client.Attributes["oauth2.scopes"] = strings.Join(scopeStrings, " ")
		}
	}

	return client
}

// convertIdentityPlatformToRealm converts a GCP Identity Platform config to a Keycloak realm.
func (m *AuthMerger) convertIdentityPlatformToRealm(result *mapper.MappingResult) *KeycloakRealm {
	if result == nil {
		return nil
	}

	realmName := consolidator.NormalizeName(result.SourceResourceName)
	if realmName == "" {
		realmName = "identity-platform-migration"
	}

	realm := &KeycloakRealm{
		Realm:               realmName,
		Enabled:             true,
		DisplayName:         fmt.Sprintf("Migrated from GCP Identity Platform: %s", result.SourceResourceName),
		BruteForceProtected: true,
		DefaultRoles:        []string{"user"},
		Attributes: map[string]string{
			"source":          "google_identity_platform",
			"original_name":   result.SourceResourceName,
			"migration_notes": "Realm migrated from GCP Identity Platform",
		},
	}

	// Try to extract configuration from result
	if result.Configs != nil {
		if configData, ok := result.Configs["identity_platform.json"]; ok {
			var ipConfig map[string]interface{}
			if err := json.Unmarshal(configData, &ipConfig); err == nil {
				// Extract authorized domains for redirect URIs
				if domains, ok := ipConfig["authorized_domains"].([]interface{}); ok {
					for _, domain := range domains {
						if d, ok := domain.(string); ok {
							realm.Clients = append(realm.Clients, Client{
								ClientID:            consolidator.NormalizeName(d) + "-client",
								Name:                d,
								Enabled:             true,
								Protocol:            "openid-connect",
								PublicClient:        true,
								StandardFlowEnabled: true,
								RedirectUris:        []string{fmt.Sprintf("https://%s/*", d)},
								WebOrigins:          []string{fmt.Sprintf("https://%s", d)},
							})
						}
					}
				}
			}
		}
	}

	// Add a default client if none were found
	if len(realm.Clients) == 0 {
		realm.Clients = []Client{
			{
				ClientID:                  realmName + "-app",
				Name:                      "Migrated Application",
				Enabled:                   true,
				Protocol:                  "openid-connect",
				PublicClient:              true,
				StandardFlowEnabled:       true,
				DirectAccessGrantsEnabled: true,
				RedirectUris:              []string{"http://localhost:*", "https://localhost:*"},
				WebOrigins:                []string{"*"},
			},
		}
	}

	return realm
}

// convertADB2CToRealm converts an Azure AD B2C directory to a Keycloak realm.
func (m *AuthMerger) convertADB2CToRealm(result *mapper.MappingResult) *KeycloakRealm {
	if result == nil {
		return nil
	}

	realmName := consolidator.NormalizeName(result.SourceResourceName)
	if realmName == "" {
		realmName = "adb2c-migration"
	}

	realm := &KeycloakRealm{
		Realm:               realmName,
		Enabled:             true,
		DisplayName:         fmt.Sprintf("Migrated from Azure AD B2C: %s", result.SourceResourceName),
		BruteForceProtected: true,
		DefaultRoles:        []string{"user"},
		Attributes: map[string]string{
			"source":          "azure_adb2c",
			"original_name":   result.SourceResourceName,
			"migration_notes": "Realm migrated from Azure AD B2C",
		},
	}

	// Try to extract configuration from result
	if result.Configs != nil {
		if configData, ok := result.Configs["adb2c.json"]; ok {
			var b2cConfig map[string]interface{}
			if err := json.Unmarshal(configData, &b2cConfig); err == nil {
				// Extract domain name for display
				if domainName, ok := b2cConfig["domain_name"].(string); ok {
					realm.DisplayName = fmt.Sprintf("Azure AD B2C: %s", domainName)
				}
			}
		}

		// Extract app registrations
		if appData, ok := result.Configs["app_registrations.json"]; ok {
			var apps []map[string]interface{}
			if err := json.Unmarshal(appData, &apps); err == nil {
				for _, appConfig := range apps {
					client := convertADB2CAppRegistration(appConfig)
					if client != nil {
						realm.Clients = append(realm.Clients, *client)
					}
				}
			}
		}
	}

	// Add a default client if none were found
	if len(realm.Clients) == 0 {
		realm.Clients = []Client{
			{
				ClientID:                  realmName + "-app",
				Name:                      "Migrated Application",
				Enabled:                   true,
				Protocol:                  "openid-connect",
				PublicClient:              true,
				StandardFlowEnabled:       true,
				DirectAccessGrantsEnabled: true,
				RedirectUris:              []string{"http://localhost:*", "https://localhost:*"},
				WebOrigins:                []string{"*"},
			},
		}
	}

	return realm
}

// convertADB2CAppRegistration converts an Azure AD B2C app registration to a Keycloak client.
func convertADB2CAppRegistration(appConfig map[string]interface{}) *Client {
	appID, _ := appConfig["application_id"].(string)
	displayName, _ := appConfig["display_name"].(string)
	if appID == "" && displayName == "" {
		return nil
	}

	clientID := appID
	if clientID == "" {
		clientID = consolidator.NormalizeName(displayName)
	}

	client := &Client{
		ClientID:                  clientID,
		Name:                      displayName,
		Enabled:                   true,
		Protocol:                  "openid-connect",
		StandardFlowEnabled:       true,
		DirectAccessGrantsEnabled: true,
		RedirectUris:              []string{"*"},
		WebOrigins:                []string{"*"},
	}

	// Check if public client
	if isPublic, ok := appConfig["is_public_client"].(bool); ok {
		client.PublicClient = isPublic
	}

	// Extract reply URLs (redirect URIs)
	if replyURLs, ok := appConfig["reply_urls"].([]interface{}); ok {
		urls := make([]string, 0, len(replyURLs))
		for _, url := range replyURLs {
			if u, ok := url.(string); ok {
				urls = append(urls, u)
			}
		}
		if len(urls) > 0 {
			client.RedirectUris = urls
		}
	}

	return client
}

// generateRealmImportJSON generates the JSON for importing realms into Keycloak.
func (m *AuthMerger) generateRealmImportJSON(realms []*KeycloakRealm) []byte {
	// Keycloak imports realms as an array
	realmArray := make([]KeycloakRealm, 0, len(realms))
	for _, realm := range realms {
		if realm != nil {
			realmArray = append(realmArray, *realm)
		}
	}

	jsonData, err := json.MarshalIndent(realmArray, "", "  ")
	if err != nil {
		// Return an empty array on error
		return []byte("[]")
	}
	return jsonData
}

// generateUserMigrationScript generates a shell script for migrating users to Keycloak.
func (m *AuthMerger) generateUserMigrationScript() []byte {
	script := `#!/bin/bash
# User Migration Script for Keycloak
# This script helps migrate users from cloud identity providers to Keycloak
#
# Prerequisites:
# - jq installed
# - curl installed
# - Access to Keycloak Admin API
# - User export file from source provider
#
# Usage:
#   ./migrate-users.sh <realm> <users-file.json>

set -euo pipefail

KEYCLOAK_URL="${KEYCLOAK_URL:-http://localhost:8080}"
KEYCLOAK_ADMIN="${KEYCLOAK_ADMIN:-admin}"
KEYCLOAK_ADMIN_PASSWORD="${KEYCLOAK_ADMIN_PASSWORD:-admin}"

REALM="${1:-}"
USERS_FILE="${2:-}"

if [ -z "$REALM" ] || [ -z "$USERS_FILE" ]; then
    echo "Usage: $0 <realm> <users-file.json>"
    echo ""
    echo "Environment variables:"
    echo "  KEYCLOAK_URL              - Keycloak base URL (default: http://localhost:8080)"
    echo "  KEYCLOAK_ADMIN            - Admin username (default: admin)"
    echo "  KEYCLOAK_ADMIN_PASSWORD   - Admin password (default: admin)"
    exit 1
fi

if [ ! -f "$USERS_FILE" ]; then
    echo "Error: Users file '$USERS_FILE' not found"
    exit 1
fi

echo "=== Keycloak User Migration ==="
echo "Keycloak URL: $KEYCLOAK_URL"
echo "Realm: $REALM"
echo "Users file: $USERS_FILE"
echo ""

# Get admin access token
echo "Obtaining admin access token..."
TOKEN=$(curl -s -X POST "$KEYCLOAK_URL/realms/master/protocol/openid-connect/token" \
    -H "Content-Type: application/x-www-form-urlencoded" \
    -d "username=$KEYCLOAK_ADMIN" \
    -d "password=$KEYCLOAK_ADMIN_PASSWORD" \
    -d "grant_type=password" \
    -d "client_id=admin-cli" | jq -r '.access_token')

if [ "$TOKEN" == "null" ] || [ -z "$TOKEN" ]; then
    echo "Error: Failed to obtain access token. Check credentials and Keycloak availability."
    exit 1
fi

echo "Token obtained successfully."
echo ""

# Count users to import
USER_COUNT=$(jq length "$USERS_FILE")
echo "Found $USER_COUNT users to import"
echo ""

# Import each user
IMPORTED=0
FAILED=0

for i in $(seq 0 $((USER_COUNT - 1))); do
    USER=$(jq ".[$i]" "$USERS_FILE")
    USERNAME=$(echo "$USER" | jq -r '.username // .email // "unknown"')

    echo -n "Importing user $((i + 1))/$USER_COUNT: $USERNAME ... "

    RESPONSE=$(curl -s -w "%{http_code}" -o /tmp/keycloak_response.json \
        -X POST "$KEYCLOAK_URL/admin/realms/$REALM/users" \
        -H "Authorization: Bearer $TOKEN" \
        -H "Content-Type: application/json" \
        -d "$USER")

    if [ "$RESPONSE" == "201" ]; then
        echo "OK"
        IMPORTED=$((IMPORTED + 1))
    elif [ "$RESPONSE" == "409" ]; then
        echo "SKIPPED (already exists)"
    else
        echo "FAILED (HTTP $RESPONSE)"
        cat /tmp/keycloak_response.json
        echo ""
        FAILED=$((FAILED + 1))
    fi
done

echo ""
echo "=== Migration Complete ==="
echo "Imported: $IMPORTED"
echo "Failed: $FAILED"
echo "Skipped: $((USER_COUNT - IMPORTED - FAILED))"
echo ""
echo "Note: Imported users will need to reset their passwords on first login."
echo "Consider enabling the 'Update Password' required action for migrated users."
`
	return []byte(script)
}
