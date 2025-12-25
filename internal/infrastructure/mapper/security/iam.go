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

// IAMMapper converts AWS IAM roles to Keycloak service accounts.
type IAMMapper struct {
	*mapper.BaseMapper
}

// NewIAMMapper creates a new IAM to Keycloak mapper.
func NewIAMMapper() *IAMMapper {
	return &IAMMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeIAMRole, nil),
	}
}

// Map converts an AWS IAM role to Keycloak configuration.
func (m *IAMMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	roleName := res.GetConfigString("name")
	if roleName == "" {
		roleName = res.Name
	}

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
	}
	svc.Command = []string{"start-dev"}
	svc.Ports = []string{"8080:8080"}
	svc.DependsOn = []string{"postgres-keycloak"}
	svc.Networks = []string{"cloudexit"}
	svc.Labels = map[string]string{
		"cloudexit.source":    "aws_iam_role",
		"cloudexit.role_name": roleName,
		"traefik.enable":      "true",
	}
	svc.Restart = "unless-stopped"
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD-SHELL", "curl -f http://localhost:8080/health/ready || exit 1"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  5,
	}

	realmConfig := m.generateIAMRealmConfig(roleName)
	result.AddConfig("config/keycloak/iam-realm.json", []byte(realmConfig))

	roleMapping := m.generateRoleMapping(res, roleName)
	result.AddConfig("config/keycloak/role-mapping.json", []byte(roleMapping))

	setupScript := m.generateSetupScript(roleName)
	result.AddScript("setup_iam.sh", []byte(setupScript))

	assumeRolePolicy := res.GetConfigString("assume_role_policy")
	if assumeRolePolicy != "" {
		result.AddWarning("Assume role policy detected. Configure trust relationships in Keycloak.")
		result.AddManualStep("Set up service account authentication in Keycloak")
	}

	if policies := m.extractAttachedPolicies(res); len(policies) > 0 {
		result.AddWarning(fmt.Sprintf("Attached policies: %s", strings.Join(policies, ", ")))
		result.AddManualStep("Map IAM policies to Keycloak roles manually")
	}

	result.AddManualStep("Import realm: Admin Console > Create Realm > Import")
	result.AddManualStep("Create service account clients for applications")
	result.AddManualStep("Map IAM policies to Keycloak authorization policies")
	result.AddWarning("IAM policy to Keycloak mapping requires manual review")

	return result, nil
}

func (m *IAMMapper) generateIAMRealmConfig(roleName string) string {
	realmName := strings.ToLower(strings.ReplaceAll(roleName, "_", "-"))

	realm := map[string]interface{}{
		"realm":   "iam-" + realmName,
		"enabled": true,
		"displayName": "IAM: " + roleName,
		"roles": map[string]interface{}{
			"realm": []map[string]string{
				{"name": "admin", "description": "Administrator role"},
				{"name": "user", "description": "Standard user role"},
				{"name": "service", "description": "Service account role"},
			},
		},
		"clients": []map[string]interface{}{
			{
				"clientId":                  roleName + "-service",
				"enabled":                   true,
				"serviceAccountsEnabled":   true,
				"clientAuthenticatorType":  "client-secret",
				"standardFlowEnabled":      false,
				"directAccessGrantsEnabled": true,
			},
		},
	}

	data, _ := json.MarshalIndent(realm, "", "  ")
	return string(data)
}

func (m *IAMMapper) generateRoleMapping(res *resource.AWSResource, roleName string) string {
	mapping := map[string]interface{}{
		"iamRole": roleName,
		"keycloakRoles": []string{"service", "user"},
		"permissions": []string{
			"view-users",
			"manage-clients",
		},
		"notes": "Manual mapping required - review IAM policies",
	}

	data, _ := json.MarshalIndent(mapping, "", "  ")
	return string(data)
}

func (m *IAMMapper) generateSetupScript(roleName string) string {
	realmName := "iam-" + strings.ToLower(strings.ReplaceAll(roleName, "_", "-"))
	return fmt.Sprintf(`#!/bin/bash
set -e

KEYCLOAK_URL="http://localhost:8080"
REALM="%s"

echo "IAM to Keycloak Setup"
echo "===================="

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
  -d @config/keycloak/iam-realm.json

echo "Setup complete!"
`, realmName)
}

func (m *IAMMapper) extractAttachedPolicies(res *resource.AWSResource) []string {
	var policies []string
	if managed := res.Config["managed_policy_arns"]; managed != nil {
		if policySlice, ok := managed.([]interface{}); ok {
			for _, p := range policySlice {
				if pStr, ok := p.(string); ok {
					policies = append(policies, pStr)
				}
			}
		}
	}
	return policies
}
