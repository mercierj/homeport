package security

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestAWSConfigConformanceManagedAToZ(t *testing.T) {
	result, err := NewAWSConfigMapper().Map(context.Background(), managedAWSConfigFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated AWS Config migration", result.ManualSteps)
	}
	if result.DockerService.Image != "openpolicyagent/opa:0.70.0" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA OPA target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/awsconfig/policies/config_rules.rego", "config/awsconfig/rules-map.yaml", "config/awsconfig/app-change.env", "config/awsconfig/generated-policy.patch", "config/opa/config.yaml"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/awsconfig/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_CONFIG_RULE=required-tags", "TARGET_POLICY_ENGINE=opa", "TARGET_POLICY_ENDPOINT=http://opa:8181"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_aws_config_rules.sh", "provision_opa_config.sh", "migrate_config_rules.sh", "validate_opa_policies.sh", "backup_aws_config_policy.sh", "cutover_config_policy.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-aws-config-rules":    domainrunbook.StepTypeCommand,
		"provision-opa-config":       domainrunbook.StepTypeCommand,
		"migrate-config-rules":       domainrunbook.StepTypeCommand,
		"validate-opa-policies":      domainrunbook.StepTypeCommand,
		"backup-aws-config-policy":   domainrunbook.StepTypeCommand,
		"cutover-config-policy":      domainrunbook.StepTypeAPICall,
		"rollback-aws-config-source": domainrunbook.StepTypeRollback,
	} {
		if !hasAWSConfigRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewAWSConfigMapper(t *testing.T) {
	m := NewAWSConfigMapper()
	if m == nil {
		t.Fatal("NewAWSConfigMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeAWSConfigRule {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeAWSConfigRule)
	}
}

func managedAWSConfigFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:     "required-tags",
		Type:   resource.TypeAWSConfigRule,
		Name:   "required-tags",
		Region: "eu-west-1",
		Config: map[string]interface{}{
			"name":              "required-tags",
			"source_owner":      "AWS",
			"source_identifier": "REQUIRED_TAGS",
		},
	}
}

func hasAWSConfigRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
