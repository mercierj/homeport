package devops

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestCloudFormationConformanceManagedAToZ(t *testing.T) {
	result, err := NewCloudFormationMapper().Map(context.Background(), managedCloudFormationFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated CloudFormation import", result.ManualSteps)
	}
	if result.DockerService.Image != "ghcr.io/opentofu/opentofu:1.8.3" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA OpenTofu import target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/opentofu/imports.tf", "config/cloudformation/stack-map.yaml", "config/cloudformation/app-change.env", "config/cloudformation/generated-import.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/cloudformation/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_CLOUDFORMATION_STACK=shop-stack", "TARGET_IAC=opentofu", "TOFU_WORKDIR=config/opentofu"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_cloudformation_stack.sh", "provision_opentofu_import.sh", "migrate_cloudformation_stack.sh", "validate_opentofu_state.sh", "backup_cloudformation_import.sh", "cutover_cloudformation_iac.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-cloudformation-stack":  domainrunbook.StepTypeCommand,
		"provision-opentofu-import":    domainrunbook.StepTypeCommand,
		"migrate-cloudformation-stack": domainrunbook.StepTypeCommand,
		"validate-opentofu-state":      domainrunbook.StepTypeCommand,
		"backup-cloudformation-import": domainrunbook.StepTypeCommand,
		"cutover-cloudformation-iac":   domainrunbook.StepTypeAPICall,
		"rollback-cloudformation":      domainrunbook.StepTypeRollback,
	} {
		if !hasCloudFormationRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewCloudFormationMapper(t *testing.T) {
	m := NewCloudFormationMapper()
	if m == nil {
		t.Fatal("NewCloudFormationMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeCloudFormationStack {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeCloudFormationStack)
	}
}

func managedCloudFormationFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:     "shop-stack",
		Type:   resource.TypeCloudFormationStack,
		Name:   "shop-stack",
		Region: "eu-west-1",
		Config: map[string]interface{}{
			"name": "shop-stack",
		},
	}
}

func hasCloudFormationRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
