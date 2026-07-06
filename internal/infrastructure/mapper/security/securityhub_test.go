package security

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestSecurityHubConformanceManagedAToZ(t *testing.T) {
	result, err := NewSecurityHubMapper().Map(context.Background(), managedSecurityHubFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Security Hub migration", result.ManualSteps)
	}
	if result.DockerService.Image != "wazuh/wazuh-manager:4.9.0" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Wazuh target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/securityhub/findings-map.yaml", "config/securityhub/standards-map.yaml", "config/securityhub/app-change.env", "config/securityhub/generated-findings.patch", "config/wazuh/rules/securityhub_rules.xml"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/securityhub/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_SECURITYHUB_ACCOUNT=123456789012", "TARGET_SECURITY_ENGINE=wazuh", "TARGET_FINDINGS_ENDPOINT=http://wazuh-manager:55000"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_securityhub_findings.sh", "provision_wazuh_securityhub.sh", "migrate_securityhub_findings.sh", "validate_securityhub_findings.sh", "backup_securityhub_config.sh", "cutover_securityhub_alerts.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-securityhub-findings":   domainrunbook.StepTypeCommand,
		"provision-wazuh-securityhub":   domainrunbook.StepTypeCommand,
		"migrate-securityhub-findings":  domainrunbook.StepTypeCommand,
		"validate-securityhub-findings": domainrunbook.StepTypeCommand,
		"backup-securityhub-config":     domainrunbook.StepTypeCommand,
		"cutover-securityhub-alerts":    domainrunbook.StepTypeAPICall,
		"rollback-securityhub-source":   domainrunbook.StepTypeRollback,
	} {
		if !hasSecurityHubRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewSecurityHubMapper(t *testing.T) {
	m := NewSecurityHubMapper()
	if m == nil {
		t.Fatal("NewSecurityHubMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeSecurityHubAccount {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeSecurityHubAccount)
	}
}

func managedSecurityHubFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:     "123456789012",
		Type:   resource.TypeSecurityHubAccount,
		Name:   "securityhub-main",
		Region: "eu-west-1",
		Config: map[string]interface{}{
			"account_id":               "123456789012",
			"standards_arn":            "arn:aws:securityhub:eu-west-1::standards/aws-foundational-security-best-practices/v/1.0.0",
			"enable_default_standards": true,
		},
	}
}

func hasSecurityHubRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
