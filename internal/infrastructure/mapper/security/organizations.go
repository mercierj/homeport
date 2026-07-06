package security

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type OrganizationsMapper struct {
	*mapper.BaseMapper
}

func NewOrganizationsMapper() *OrganizationsMapper {
	return &OrganizationsMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeOrganizationsOrganization, nil)}
}

func (m *OrganizationsMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	orgID := res.GetConfigString("id")
	if orgID == "" {
		orgID = res.ID
	}
	name := res.Name
	if name == "" {
		name = orgID
	}

	result := mapper.NewMappingResult("opentofu-opa-landing-zone")
	svc := result.DockerService
	svc.Image = "openpolicyagent/opa:0.70.0"
	svc.Command = []string{"run", "--server", "--addr=0.0.0.0:8181", "/policies"}
	svc.Ports = []string{"8181:8181"}
	svc.Volumes = []string{"./config/organizations:/workspace", "./config/organizations:/policies"}
	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Labels = map[string]string{
		"homeport.source":       "aws_organizations_organization",
		"homeport.organization": orgID,
		"homeport.target":       "opentofu-opa",
	}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "wget", "-qO-", "http://localhost:8181/health"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  5,
	}

	result.AddConfig("config/organizations/workspaces.tf", []byte(m.workspacesTF(orgID, name)))
	result.AddConfig("config/organizations/accounts.auto.tfvars.json", []byte(m.accountsVars(orgID)))
	result.AddConfig("config/organizations/service-control-policies.rego", []byte(m.scpPolicy(orgID)))
	result.AddConfig("config/organizations/app-change.env", []byte(m.appChange(orgID)))
	result.AddConfig("config/organizations/generated-landing-zone.patch", []byte(m.generatedPatch(orgID)))
	result.AddScript("export_organizations_hierarchy.sh", []byte(m.exportScript(orgID)))
	result.AddScript("provision_opentofu_workspaces.sh", []byte(m.provisionScript(orgID)))
	result.AddScript("migrate_organization_accounts.sh", []byte(m.migrateScript(orgID)))
	result.AddScript("validate_organization_policy.sh", []byte(m.validateScript(orgID)))
	result.AddScript("backup_organization_config.sh", []byte(m.backupScript(orgID)))
	result.AddScript("cutover_organization_landing_zone.sh", []byte(m.cutoverScript(orgID)))
	for _, step := range organizationsRunbook(orgID) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *OrganizationsMapper) workspacesTF(orgID, name string) string {
	return fmt.Sprintf(`terraform {
  required_version = ">= 1.8.0"
}

variable "organization_id" {
  type    = string
  default = %q
}

variable "accounts" {
  type = list(object({
    id    = string
    name  = string
    email = string
    ou    = string
  }))
}

locals {
  landing_zone_name = %q
}
`, orgID, name)
}

func (m *OrganizationsMapper) accountsVars(orgID string) string {
	return fmt.Sprintf(`{
  "organization_id": %q,
  "accounts": []
}
`, orgID)
}

func (m *OrganizationsMapper) scpPolicy(orgID string) string {
	return fmt.Sprintf(`package homeport.organizations

default allow := true

organization_id := %q

deny[msg] if {
  input.organization_id == organization_id
  input.account.status != "ACTIVE"
  msg := sprintf("organization account %%s is %%s", [input.account.id, input.account.status])
}
`, orgID)
}

func (m *OrganizationsMapper) appChange(orgID string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=generated_patch
SOURCE_ORGANIZATION_ID=%s
TARGET_LANDING_ZONE=opentofu
TARGET_POLICY_ENGINE=opa
TARGET_POLICY_ENDPOINT=http://opa:8181
GENERATED_PATCH=config/organizations/generated-landing-zone.patch
`, orgID)
}

func (m *OrganizationsMapper) generatedPatch(orgID string) string {
	return fmt.Sprintf(`--- a/landing-zone/org.env
+++ b/landing-zone/org.env
@@
-AWS_ORGANIZATION_ID=%s
+LANDING_ZONE_BACKEND=opentofu
+POLICY_ENGINE=opa
+POLICY_ENDPOINT=http://opa:8181
`, orgID)
}

func (m *OrganizationsMapper) exportScript(orgID string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
ORG_ID=%q
OUTPUT_DIR="./organizations-export"
mkdir -p "$OUTPUT_DIR"
aws organizations describe-organization --output json > "$OUTPUT_DIR/organization.json"
aws organizations list-roots --output json > "$OUTPUT_DIR/roots.json"
aws organizations list-accounts --output json > "$OUTPUT_DIR/accounts.json"
aws organizations list-policies --filter SERVICE_CONTROL_POLICY --output json > "$OUTPUT_DIR/service-control-policies.json"
grep -q "$ORG_ID" "$OUTPUT_DIR/organization.json"
`, orgID)
}

func (m *OrganizationsMapper) provisionScript(orgID string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/organizations/workspaces.tf\ntest -s config/organizations/service-control-policies.rego\necho \"OpenTofu landing-zone workspace ready for %s\"\n", orgID)
}

func (m *OrganizationsMapper) migrateScript(orgID string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s organizations-export/accounts.json\ntest -s config/organizations/accounts.auto.tfvars.json\ngrep -q %q config/organizations/accounts.auto.tfvars.json\necho \"AWS Organizations hierarchy %s mapped to OpenTofu inputs\"\n", orgID, orgID)
}

func (m *OrganizationsMapper) validateScript(orgID string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
test -s config/organizations/app-change.env
test -s config/organizations/generated-landing-zone.patch
opa eval -d config/organizations/service-control-policies.rego -i /dev/stdin 'data.homeport.organizations.allow' <<'JSON'
{"organization_id":%q,"account":{"id":"111111111111","status":"ACTIVE"}}
JSON
`, orgID)
}

func (m *OrganizationsMapper) backupScript(orgID string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-organizations-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/organizations export_organizations_hierarchy.sh provision_opentofu_workspaces.sh migrate_organization_accounts.sh validate_organization_policy.sh cutover_organization_landing_zone.sh
echo "$archive"
`, orgID)
}

func (m *OrganizationsMapper) cutoverScript(orgID string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
. config/organizations/app-change.env
test "$SOURCE_ORGANIZATION_ID" = %q
test "$APP_CHANGE_MODE" = "generated_patch"
test -s "$GENERATED_PATCH"
echo "Apply $GENERATED_PATCH and route landing-zone changes through $TARGET_LANDING_ZONE with $TARGET_POLICY_ENGINE"
`, orgID)
}

func organizationsRunbook(orgID string) []domainrunbook.Step {
	metadata := map[string]string{
		"kind":                "landing-zone",
		"source":              "aws_organizations_organization",
		"organization":        orgID,
		"HOMEPORT_TARGET":     "opentofu-opa",
		"HOMEPORT_APP_CHANGE": "generated_patch",
	}
	return []domainrunbook.Step{
		organizationsStep("export-organizations-hierarchy", "Export Organizations hierarchy", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_organizations_hierarchy.sh"}, "Organizations hierarchy and SCPs are exported", metadata),
		organizationsStep("provision-opentofu-workspaces", "Provision OpenTofu workspaces", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "provision_opentofu_workspaces.sh"}, "OpenTofu workspace files are rendered", metadata),
		organizationsStep("migrate-organization-accounts", "Migrate organization accounts", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_organization_accounts.sh"}, "Organizations accounts map to landing-zone inputs", metadata),
		organizationsStep("validate-organization-policy", "Validate organization policy", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_organization_policy.sh"}, "OPA policy evaluates the generated hierarchy", metadata),
		organizationsStep("backup-organization-config", "Backup organization config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_organization_config.sh"}, "Organizations migration artifacts are archived", metadata),
		organizationsStep("cutover-organization-landing-zone", "Cut over landing-zone config", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_organization_landing_zone.sh"}, "landing-zone consumers use OpenTofu and OPA", metadata),
		organizationsStep("rollback-organizations-source", "Keep AWS Organizations source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "AWS Organizations remains authoritative until landing-zone validation passes", metadata),
	}
}

func organizationsStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
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
