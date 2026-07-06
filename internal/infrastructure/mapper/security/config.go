package security

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type AWSConfigMapper struct {
	*mapper.BaseMapper
}

func NewAWSConfigMapper() *AWSConfigMapper {
	return &AWSConfigMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeAWSConfigRule, nil)}
}

func (m *AWSConfigMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	name := res.GetConfigString("name")
	if name == "" {
		name = res.Name
	}
	sourceIdentifier := res.GetConfigString("source_identifier")
	if sourceIdentifier == "" {
		sourceIdentifier = "CUSTOM_POLICY"
	}

	result := mapper.NewMappingResult("opa")
	svc := result.DockerService
	svc.Image = "openpolicyagent/opa:0.70.0"
	svc.Command = []string{"run", "--server", "--addr=0.0.0.0:8181", "/policies"}
	svc.Ports = []string{"8181:8181"}
	svc.Volumes = []string{"./config/awsconfig/policies:/policies", "./data/awsconfig:/data"}
	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Labels = map[string]string{
		"homeport.source":      "aws_config_config_rule",
		"homeport.config_rule": name,
		"homeport.target":      "opa",
	}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "wget", "-qO-", "http://localhost:8181/health"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  5,
	}

	result.AddConfig("config/awsconfig/policies/config_rules.rego", []byte(m.regoPolicy(name, sourceIdentifier)))
	result.AddConfig("config/awsconfig/rules-map.yaml", []byte(m.rulesMap(name, sourceIdentifier)))
	result.AddConfig("config/awsconfig/app-change.env", []byte(m.appChange(name)))
	result.AddConfig("config/awsconfig/generated-policy.patch", []byte(m.generatedPatch(name)))
	result.AddConfig("config/opa/config.yaml", []byte(m.opaConfig()))
	result.AddScript("export_aws_config_rules.sh", []byte(m.exportScript(name, res.Region)))
	result.AddScript("provision_opa_config.sh", []byte(m.provisionScript(name)))
	result.AddScript("migrate_config_rules.sh", []byte(m.migrateScript(name)))
	result.AddScript("validate_opa_policies.sh", []byte(m.validateScript(name)))
	result.AddScript("backup_aws_config_policy.sh", []byte(m.backupScript(name)))
	result.AddScript("cutover_config_policy.sh", []byte(m.cutoverScript(name)))
	for _, step := range awsConfigRunbook(name) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *AWSConfigMapper) regoPolicy(name, sourceIdentifier string) string {
	return fmt.Sprintf(`package homeport.aws_config

default allow := false

rule_name := %q
source_identifier := %q

allow if {
	input.configRuleName == rule_name
	input.complianceType == "COMPLIANT"
}

deny[msg] if {
	input.configRuleName == rule_name
	input.complianceType != "COMPLIANT"
	msg := sprintf("AWS Config rule %%s is %%s", [rule_name, input.complianceType])
}
`, name, sourceIdentifier)
}

func (m *AWSConfigMapper) rulesMap(name, sourceIdentifier string) string {
	return fmt.Sprintf(`rule: %s
source: aws_config_config_rule
source_identifier: %s
target: opa
policy: data.homeport.aws_config
inventory_input: aws_config_snapshot
`, name, sourceIdentifier)
}

func (m *AWSConfigMapper) appChange(name string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=generated_patch
SOURCE_CONFIG_RULE=%s
TARGET_POLICY_ENGINE=opa
TARGET_POLICY_ENDPOINT=http://opa:8181
GENERATED_PATCH=config/awsconfig/generated-policy.patch
`, name)
}

func (m *AWSConfigMapper) generatedPatch(name string) string {
	return fmt.Sprintf(`--- a/security/policy.env
+++ b/security/policy.env
@@
-AWS_CONFIG_RULE=%s
+POLICY_ENGINE=opa
+POLICY_ENDPOINT=http://opa:8181
`, name)
}

func (m *AWSConfigMapper) opaConfig() string {
	return "services:\n  homeport:\n    url: http://opa:8181\nbundles:\n  awsconfig:\n    resource: /bundles/awsconfig.tar.gz\n"
}

func (m *AWSConfigMapper) exportScript(name, region string) string {
	if region == "" {
		region = "us-east-1"
	}
	return fmt.Sprintf(`#!/bin/sh
set -eu
AWS_REGION=%q
CONFIG_RULE_NAME=%q
OUTPUT_DIR="./awsconfig-export"
mkdir -p "$OUTPUT_DIR"
aws configservice describe-config-rules --config-rule-names "$CONFIG_RULE_NAME" --region "$AWS_REGION" --output json > "$OUTPUT_DIR/config-rule.json"
aws configservice get-compliance-details-by-config-rule --config-rule-name "$CONFIG_RULE_NAME" --region "$AWS_REGION" --output json > "$OUTPUT_DIR/compliance.json"
aws configservice get-resource-config-history --resource-type AWS::EC2::Instance --limit 1 --region "$AWS_REGION" --output json > "$OUTPUT_DIR/inventory-sample.json"
`, region, name)
}

func (m *AWSConfigMapper) provisionScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/awsconfig/policies/config_rules.rego\ntest -s config/opa/config.yaml\necho \"OPA policy target ready for AWS Config rule %s\"\n", name)
}

func (m *AWSConfigMapper) migrateScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s awsconfig-export/config-rule.json\ntest -s config/awsconfig/rules-map.yaml\ngrep -q %q config/awsconfig/rules-map.yaml\necho \"AWS Config rule %s mapped to OPA\"\n", name, name)
}

func (m *AWSConfigMapper) validateScript(name string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
test -s config/awsconfig/app-change.env
test -s config/awsconfig/generated-policy.patch
opa eval -d config/awsconfig/policies/config_rules.rego -i /dev/stdin 'data.homeport.aws_config.allow' <<'JSON'
{"configRuleName":%q,"complianceType":"COMPLIANT"}
JSON
`, name)
}

func (m *AWSConfigMapper) backupScript(name string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-aws-config-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/awsconfig config/opa export_aws_config_rules.sh provision_opa_config.sh migrate_config_rules.sh validate_opa_policies.sh cutover_config_policy.sh
echo "$archive"
`, name)
}

func (m *AWSConfigMapper) cutoverScript(name string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
. config/awsconfig/app-change.env
test "$SOURCE_CONFIG_RULE" = %q
test "$APP_CHANGE_MODE" = "generated_patch"
test -s "$GENERATED_PATCH"
echo "Apply $GENERATED_PATCH and route compliance checks to $TARGET_POLICY_ENDPOINT"
`, name)
}

func awsConfigRunbook(name string) []domainrunbook.Step {
	metadata := map[string]string{
		"kind":                "policy-compliance",
		"source":              "aws_config_config_rule",
		"rule":                name,
		"HOMEPORT_TARGET":     "opa",
		"HOMEPORT_APP_CHANGE": "generated_patch",
	}
	return []domainrunbook.Step{
		awsConfigStep("export-aws-config-rules", "Export AWS Config rules", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_aws_config_rules.sh"}, "AWS Config rule and compliance snapshot are exported", metadata),
		awsConfigStep("provision-opa-config", "Provision OPA policy engine", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "provision_opa_config.sh"}, "OPA policy bundle is rendered", metadata),
		awsConfigStep("migrate-config-rules", "Migrate Config rules", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_config_rules.sh"}, "AWS Config rule maps to Rego policy", metadata),
		awsConfigStep("validate-opa-policies", "Validate OPA policies", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_opa_policies.sh"}, "OPA evaluates the generated Config policy", metadata),
		awsConfigStep("backup-aws-config-policy", "Backup AWS Config policy", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_aws_config_policy.sh"}, "AWS Config and OPA artifacts are archived", metadata),
		awsConfigStep("cutover-config-policy", "Cut over Config policy checks", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_config_policy.sh"}, "policy consumers use the OPA target", metadata),
		awsConfigStep("rollback-aws-config-source", "Keep AWS Config source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "AWS Config remains authoritative until OPA validation passes", metadata),
	}
}

func awsConfigStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
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
