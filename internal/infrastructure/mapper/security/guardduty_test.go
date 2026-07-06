package security

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestGuardDutyConformanceManagedAToZ(t *testing.T) {
	if !resource.TypeGuardDutyDetector.IsValid() {
		t.Fatalf("%s should be a valid AWS resource type", resource.TypeGuardDutyDetector)
	}
	result, err := NewGuardDutyMapper().Map(context.Background(), managedGuardDutyFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated GuardDuty migration", result.ManualSteps)
	}
	if result.DockerService.Image != "wazuh/wazuh-manager:4.9.0" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Wazuh target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/guardduty/findings-map.yaml", "config/guardduty/app-change.env", "config/wazuh/rules/guardduty_rules.xml"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/guardduty/app-change.env"])
	for _, want := range []string{"SOURCE_DETECTOR=det-123", "TARGET_SECURITY_ENGINE=wazuh", "APP_CHANGE_MODE=generated_patch"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_guardduty_findings.sh", "load_wazuh_rules.sh", "backup_guardduty_config.sh", "validate_guardduty_detection.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-guardduty-findings":    domainrunbook.StepTypeCommand,
		"provision-wazuh-manager":      domainrunbook.StepTypeCommand,
		"load-guardduty-rules":         domainrunbook.StepTypeCommand,
		"validate-guardduty-detection": domainrunbook.StepTypeCommand,
		"backup-guardduty-config":      domainrunbook.StepTypeCommand,
		"cutover-guardduty-alerts":     domainrunbook.StepTypeAPICall,
		"rollback-guardduty-source":    domainrunbook.StepTypeRollback,
	} {
		if !hasGuardDutyRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func managedGuardDutyFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "det-123",
		Type: resource.TypeGuardDutyDetector,
		Name: "det-123",
		Config: map[string]interface{}{
			"detector_id":                  "det-123",
			"enable":                       true,
			"finding_publishing_frequency": "FIFTEEN_MINUTES",
		},
	}
}

func hasGuardDutyRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
