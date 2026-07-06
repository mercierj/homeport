package security

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestControlTowerConformanceManagedAToZ(t *testing.T) {
	result, err := NewControlTowerMapper().Map(context.Background(), managedControlTowerFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Control Tower migration", result.ManualSteps)
	}
	if result.DockerService.Image != "openpolicyagent/opa:0.70.0" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA policy target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/controltower/crossplane-control.yaml", "config/controltower/controls-map.yaml", "config/controltower/guardrails.rego", "config/controltower/app-change.env", "config/controltower/generated-governance.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/controltower/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_CONTROL_IDENTIFIER=guardrail-1", "TARGET_PROVISIONER=crossplane", "TARGET_POLICY_ENGINE=opa"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_controltower_controls.sh", "provision_crossplane_controls.sh", "migrate_controltower_guardrails.sh", "validate_controltower_policy.sh", "backup_controltower_config.sh", "cutover_controltower_governance.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-controltower-controls":    domainrunbook.StepTypeCommand,
		"provision-crossplane-controls":   domainrunbook.StepTypeCommand,
		"migrate-controltower-guardrails": domainrunbook.StepTypeCommand,
		"validate-controltower-policy":    domainrunbook.StepTypeCommand,
		"backup-controltower-config":      domainrunbook.StepTypeCommand,
		"cutover-controltower-governance": domainrunbook.StepTypeAPICall,
		"rollback-controltower-source":    domainrunbook.StepTypeRollback,
	} {
		if !hasControlTowerRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewControlTowerMapper(t *testing.T) {
	m := NewControlTowerMapper()
	if m == nil {
		t.Fatal("NewControlTowerMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeControlTowerControl {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeControlTowerControl)
	}
}

func managedControlTowerFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:     "guardrail-1",
		Type:   resource.TypeControlTowerControl,
		Name:   "guardrail-1",
		Region: "eu-west-1",
		Config: map[string]interface{}{
			"control_identifier": "guardrail-1",
			"target_identifier":  "ou-prod",
		},
	}
}

func hasControlTowerRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
