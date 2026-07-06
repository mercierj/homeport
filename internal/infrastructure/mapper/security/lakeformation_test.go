package security

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestLakeFormationConformanceManagedAToZ(t *testing.T) {
	result, err := NewLakeFormationMapper().Map(context.Background(), managedLakeFormationFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Lake Formation migration", result.ManualSteps)
	}
	if result.DockerService.Image != "apache/ranger:2.4.0" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Ranger target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/ranger/service.json", "config/ranger/lakeformation-policies.json", "config/lakeformation/app-change.env", "config/lakeformation/generated-policy-report.md"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/lakeformation/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_LAKEFORMATION_POLICY=orders-governance", "TARGET_GOVERNANCE=apache-ranger", "RANGER_URL=http://ranger:6080"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_lakeformation_permissions.sh", "provision_ranger_service.sh", "migrate_lakeformation_policies.sh", "validate_ranger_policies.sh", "backup_lakeformation_config.sh", "cutover_lakeformation_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-lakeformation-permissions": domainrunbook.StepTypeCommand,
		"provision-ranger-service":         domainrunbook.StepTypeCommand,
		"migrate-lakeformation-policies":   domainrunbook.StepTypeCommand,
		"validate-ranger-policies":         domainrunbook.StepTypeCommand,
		"backup-lakeformation-config":      domainrunbook.StepTypeCommand,
		"cutover-lakeformation-clients":    domainrunbook.StepTypeAPICall,
		"rollback-lakeformation-source":    domainrunbook.StepTypeRollback,
	} {
		if !hasLakeFormationRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewLakeFormationMapper(t *testing.T) {
	m := NewLakeFormationMapper()
	if m == nil {
		t.Fatal("NewLakeFormationMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeLakeFormationPermissions {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeLakeFormationPermissions)
	}
}

func managedLakeFormationFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:     "orders-governance",
		Type:   resource.TypeLakeFormationPermissions,
		Name:   "orders-governance",
		Region: "eu-west-1",
		Config: map[string]interface{}{
			"name": "orders-governance",
		},
	}
}

func hasLakeFormationRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
