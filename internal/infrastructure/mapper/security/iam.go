// Package security provides mappers for AWS security services.
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
	svc.Networks = []string{"homeport"}
	svc.Labels = map[string]string{
		"homeport.source":    "aws_iam_role",
		"homeport.role_name": roleName,
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

// ExtractPolicies extracts IAM policies from the role.
func (m *IAMMapper) ExtractPolicies(ctx context.Context, res *resource.AWSResource) ([]*policy.Policy, error) {
	var policies []*policy.Policy

	roleName := res.GetConfigString("name")
	if roleName == "" {
		roleName = res.Name
	}

	// Extract assume role policy (trust policy)
	if assumeRolePolicy := res.GetConfigString("assume_role_policy"); assumeRolePolicy != "" {
		p := policy.NewPolicy(
			res.ID+"-trust-policy",
			roleName+" Trust Policy",
			policy.PolicyTypeIAM,
			policy.ProviderAWS,
		)
		p.ResourceID = res.ID
		p.ResourceType = "aws_iam_role"
		p.ResourceName = roleName
		p.OriginalDocument = json.RawMessage(assumeRolePolicy)
		p.OriginalFormat = "json"
		p.NormalizedPolicy = m.normalizeIAMPolicy(assumeRolePolicy)

		// Add warning for cross-account trust
		if strings.Contains(assumeRolePolicy, "arn:aws:iam::") {
			p.AddWarning("Trust policy allows cross-account access")
		}
		// Add warning for service principal
		if strings.Contains(assumeRolePolicy, "\"Service\"") {
			p.AddWarning("Trust policy allows AWS service principals")
		}

		policies = append(policies, p)
	}

	// Extract inline policies
	if inlinePolicies := res.Config["inline_policy"]; inlinePolicies != nil {
		if policyList, ok := inlinePolicies.([]interface{}); ok {
			for i, ip := range policyList {
				if policyMap, ok := ip.(map[string]interface{}); ok {
					policyName := ""
					if name, ok := policyMap["name"].(string); ok {
						policyName = name
					}
					if policyDoc, ok := policyMap["policy"].(string); ok {
						p := policy.NewPolicy(
							fmt.Sprintf("%s-inline-%d", res.ID, i),
							fmt.Sprintf("%s Inline Policy: %s", roleName, policyName),
							policy.PolicyTypeIAM,
							policy.ProviderAWS,
						)
						p.ResourceID = res.ID
						p.ResourceType = "aws_iam_role_policy"
						p.ResourceName = roleName
						p.OriginalDocument = json.RawMessage(policyDoc)
						p.OriginalFormat = "json"
						p.NormalizedPolicy = m.normalizeIAMPolicy(policyDoc)

						// Check for dangerous permissions
						if strings.Contains(policyDoc, "\"*\"") {
							p.AddWarning("Policy contains wildcard permissions")
						}

						policies = append(policies, p)
					}
				}
			}
		}
	}

	// Extract managed policy attachments as references
	managedPolicies := m.extractAttachedPolicies(res)
	if len(managedPolicies) > 0 {
		managedJSON, _ := json.Marshal(map[string]interface{}{
			"managed_policy_arns": managedPolicies,
		})

		p := policy.NewPolicy(
			res.ID+"-managed-policies",
			roleName+" Managed Policy Attachments",
			policy.PolicyTypeIAM,
			policy.ProviderAWS,
		)
		p.ResourceID = res.ID
		p.ResourceType = "aws_iam_role_policy_attachment"
		p.ResourceName = roleName
		p.OriginalDocument = managedJSON
		p.OriginalFormat = "json"
		p.AddWarning("Managed policy ARNs referenced - actual policy content needs to be fetched from AWS")

		policies = append(policies, p)
	}

	return policies, nil
}

// normalizeIAMPolicy converts an AWS IAM policy document to normalized format.
func (m *IAMMapper) normalizeIAMPolicy(policyDoc string) *policy.NormalizedPolicy {
	normalized := &policy.NormalizedPolicy{
		Statements: make([]policy.Statement, 0),
	}

	var awsPolicy struct {
		Version   string `json:"Version"`
		Statement []struct {
			Sid       string      `json:"Sid"`
			Effect    string      `json:"Effect"`
			Principal interface{} `json:"Principal"`
			Action    interface{} `json:"Action"`
			Resource  interface{} `json:"Resource"`
			Condition interface{} `json:"Condition"`
		} `json:"Statement"`
	}

	if err := json.Unmarshal([]byte(policyDoc), &awsPolicy); err != nil {
		return normalized
	}

	normalized.Version = awsPolicy.Version

	for _, stmt := range awsPolicy.Statement {
		normalizedStmt := policy.Statement{
			SID:    stmt.Sid,
			Effect: policy.Effect(stmt.Effect),
		}

		// Parse principals
		normalizedStmt.Principals = m.parsePrincipals(stmt.Principal)

		// Parse actions
		normalizedStmt.Actions = m.parseStringOrSlice(stmt.Action)

		// Parse resources
		normalizedStmt.Resources = m.parseStringOrSlice(stmt.Resource)

		// Parse conditions
		normalizedStmt.Conditions = m.parseConditions(stmt.Condition)

		normalized.Statements = append(normalized.Statements, normalizedStmt)
	}

	return normalized
}

// parsePrincipals converts AWS principal format to normalized principals.
func (m *IAMMapper) parsePrincipals(principal interface{}) []policy.Principal {
	var principals []policy.Principal

	if principal == nil {
		return principals
	}

	switch p := principal.(type) {
	case string:
		if p == "*" {
			principals = append(principals, policy.Principal{Type: "*", ID: "*"})
		} else {
			principals = append(principals, policy.Principal{Type: "AWS", ID: p})
		}
	case map[string]interface{}:
		for pType, pValue := range p {
			ids := m.parseStringOrSlice(pValue)
			for _, id := range ids {
				principals = append(principals, policy.Principal{Type: pType, ID: id})
			}
		}
	}

	return principals
}

// parseStringOrSlice handles AWS policy fields that can be string or array.
func (m *IAMMapper) parseStringOrSlice(value interface{}) []string {
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case string:
		return []string{v}
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}

	return nil
}

// parseConditions converts AWS conditions to normalized format.
func (m *IAMMapper) parseConditions(condition interface{}) []policy.Condition {
	var conditions []policy.Condition

	if condition == nil {
		return conditions
	}

	condMap, ok := condition.(map[string]interface{})
	if !ok {
		return conditions
	}

	for operator, keys := range condMap {
		keyMap, ok := keys.(map[string]interface{})
		if !ok {
			continue
		}

		for key, values := range keyMap {
			cond := policy.Condition{
				Operator: operator,
				Key:      key,
				Values:   m.parseStringOrSlice(values),
			}
			conditions = append(conditions, cond)
		}
	}

	return conditions
}
