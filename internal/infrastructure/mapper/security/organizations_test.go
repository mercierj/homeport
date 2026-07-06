package security

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestOrganizationsConformanceManagedAToZ(t *testing.T) {
	result, err := NewOrganizationsMapper().Map(context.Background(), managedOrganizationsFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Organizations migration", result.ManualSteps)
	}
	if result.DockerService.Image != "openpolicyagent/opa:0.70.0" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA policy target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/organizations/workspaces.tf", "config/organizations/accounts.auto.tfvars.json", "config/organizations/service-control-policies.rego", "config/organizations/app-change.env", "config/organizations/generated-landing-zone.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/organizations/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_ORGANIZATION_ID=o-example", "TARGET_LANDING_ZONE=opentofu", "TARGET_POLICY_ENGINE=opa"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_organizations_hierarchy.sh", "provision_opentofu_workspaces.sh", "migrate_organization_accounts.sh", "validate_organization_policy.sh", "backup_organization_config.sh", "cutover_organization_landing_zone.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-organizations-hierarchy":    domainrunbook.StepTypeCommand,
		"provision-opentofu-workspaces":     domainrunbook.StepTypeCommand,
		"migrate-organization-accounts":     domainrunbook.StepTypeCommand,
		"validate-organization-policy":      domainrunbook.StepTypeCommand,
		"backup-organization-config":        domainrunbook.StepTypeCommand,
		"cutover-organization-landing-zone": domainrunbook.StepTypeAPICall,
		"rollback-organizations-source":     domainrunbook.StepTypeRollback,
	} {
		if !hasOrganizationsRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewOrganizationsMapper(t *testing.T) {
	m := NewOrganizationsMapper()
	if m == nil {
		t.Fatal("NewOrganizationsMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeOrganizationsOrganization {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeOrganizationsOrganization)
	}
}

func managedOrganizationsFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "o-example",
		Type: resource.TypeOrganizationsOrganization,
		Name: "prod-org",
		Config: map[string]interface{}{
			"id":          "o-example",
			"feature_set": "ALL",
		},
	}
}

func hasOrganizationsRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
