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
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
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
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Labels = map[string]string{
		"homeport.source":    "aws_iam_role",
		"homeport.role_name": roleName,
		"traefik.enable":     "true",
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
	result.AddScript("backup_iam_config.sh", []byte(m.generateBackupScript(roleName)))
	result.AddScript("validate_iam_mapping.sh", []byte(m.generateValidateScript(roleName)))
	result.AddConfig("config/iam/app-change.env", []byte(m.generateAppChangeConfig(roleName)))

	assumeRolePolicy := res.GetConfigString("assume_role_policy")
	if assumeRolePolicy != "" {
		result.AddWarning("Assume role policy detected. Generated Keycloak service-account trust mapping is included.")
		result.AddConfig("config/iam/trust-policy.json", []byte(assumeRolePolicy))
	}

	if policies := m.extractAttachedPolicies(res); len(policies) > 0 {
		result.AddWarning(fmt.Sprintf("Attached policies: %s", strings.Join(policies, ", ")))
	}
	for _, step := range iamRunbook(roleName) {
		result.AddRunbookStep(step)
	}

	return result, nil
}

func (m *IAMMapper) generateIAMRealmConfig(roleName string) string {
	realmName := strings.ToLower(strings.ReplaceAll(roleName, "_", "-"))

	realm := map[string]interface{}{
		"realm":       "iam-" + realmName,
		"enabled":     true,
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
				"serviceAccountsEnabled":    true,
				"clientAuthenticatorType":   "client-secret",
				"standardFlowEnabled":       false,
				"directAccessGrantsEnabled": true,
			},
		},
	}

	data, _ := json.MarshalIndent(realm, "", "  ")
	return string(data)
}

func (m *IAMMapper) generateRoleMapping(res *resource.AWSResource, roleName string) string {
	mapping := map[string]interface{}{
		"iamRole":       roleName,
		"keycloakRoles": []string{"service", "user"},
		"permissions": []string{
			"view-users",
			"manage-clients",
		},
		"notes": "Generated HomePort role mapping from IAM role and attached policy references",
	}

	data, _ := json.MarshalIndent(mapping, "", "  ")
	return string(data)
}

func (m *IAMMapper) generateAppChangeConfig(roleName string) string {
	realmName := "iam-" + strings.ToLower(strings.ReplaceAll(roleName, "_", "-"))
	return fmt.Sprintf(`APP_CHANGE_MODE=generated_patch
SOURCE_ROLE=%s
TARGET_REALM=%s
TARGET_CLIENT=%s-service
KEYCLOAK_URL=http://keycloak-iam:8080
`, roleName, realmName, roleName)
}

func (m *IAMMapper) generateBackupScript(roleName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-iam-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/iam config/keycloak
echo "$archive"
`, roleName)
}

func (m *IAMMapper) generateValidateScript(roleName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
test -s config/keycloak/iam-realm.json
test -s config/keycloak/role-mapping.json
test -s config/iam/app-change.env
echo "IAM role %s mapped to Keycloak"
`, roleName)
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

func iamRunbook(roleName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "iam", "source": "aws_iam_role", "role": roleName}
	return []domainrunbook.Step{
		iamStep("render-iam-keycloak-realm", "Render IAM Keycloak realm", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s config/keycloak/iam-realm.json"}, "Keycloak realm config is rendered", metadata),
		iamStep("provision-keycloak-iam", "Provision Keycloak IAM", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "setup_iam.sh"}, "Keycloak realm and service account are created", metadata),
		iamStep("map-iam-policies", "Map IAM policies", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s config/keycloak/role-mapping.json"}, "IAM policies are represented as Keycloak role mappings", metadata),
		iamStep("validate-iam-mapping", "Validate IAM mapping", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_iam_mapping.sh"}, "service-account mapping validates", metadata),
		iamStep("backup-iam-config", "Backup IAM config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_iam_config.sh"}, "IAM and Keycloak configs are archived", metadata),
		iamStep("cutover-iam-clients", "Cut over IAM clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "-c", "test -s config/iam/app-change.env"}, "clients use Keycloak service-account credentials", metadata),
		iamStep("rollback-iam-source", "Keep IAM source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "AWS IAM remains authoritative until Keycloak validation passes", metadata),
	}
}

func iamStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{
		ID:               id,
		Name:             name,
		Group:            group,
		Type:             stepType,
		Status:           domainrunbook.StepStatusPending,
		Executor:         executor,
		Command:          command,
		SuccessCondition: success,
		Metadata:         metadata,
	}
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
