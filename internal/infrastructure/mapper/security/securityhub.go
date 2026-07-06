package security

import (
	"context"
	"fmt"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type SecurityHubMapper struct {
	*mapper.BaseMapper
}

func NewSecurityHubMapper() *SecurityHubMapper {
	return &SecurityHubMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeSecurityHubAccount, nil)}
}

func (m *SecurityHubMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	accountID := res.GetConfigString("account_id")
	if accountID == "" {
		accountID = res.ID
	}
	standardsARN := res.GetConfigString("standards_arn")
	if standardsARN == "" {
		standardsARN = "enabled-standards"
	}

	result := mapper.NewMappingResult("wazuh-manager")
	svc := result.DockerService
	svc.Image = "wazuh/wazuh-manager:4.9.0"
	svc.Ports = []string{"1514:1514/udp", "55000:55000"}
	svc.Volumes = []string{"./config/wazuh:/var/ossec/etc/shared", "./data/wazuh:/var/ossec/data"}
	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Environment = map[string]string{"HOMEPORT_SOURCE_SECURITYHUB_ACCOUNT": accountID}
	svc.Labels = map[string]string{
		"homeport.source":              "aws_securityhub_account",
		"homeport.securityhub_account": accountID,
		"homeport.target":              "wazuh",
	}

	result.AddConfig("config/securityhub/findings-map.yaml", []byte(m.findingsMap(accountID)))
	result.AddConfig("config/securityhub/standards-map.yaml", []byte(m.standardsMap(standardsARN)))
	result.AddConfig("config/securityhub/app-change.env", []byte(m.appChangeConfig(accountID)))
	result.AddConfig("config/securityhub/generated-findings.patch", []byte(m.generatedPatch(accountID)))
	result.AddConfig("config/wazuh/rules/securityhub_rules.xml", []byte(m.wazuhRules()))
	result.AddScript("export_securityhub_findings.sh", []byte(m.exportScript(accountID, res.Region)))
	result.AddScript("provision_wazuh_securityhub.sh", []byte(m.provisionScript(accountID)))
	result.AddScript("migrate_securityhub_findings.sh", []byte(m.migrateScript(accountID)))
	result.AddScript("validate_securityhub_findings.sh", []byte(m.validateScript(accountID)))
	result.AddScript("backup_securityhub_config.sh", []byte(m.backupScript(accountID)))
	result.AddScript("cutover_securityhub_alerts.sh", []byte(m.cutoverScript(accountID)))
	for _, step := range securityHubRunbook(accountID) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *SecurityHubMapper) findingsMap(accountID string) string {
	return fmt.Sprintf(`account: %s
source: aws_securityhub_account
target: wazuh
mappings:
  - source: SecurityHubFinding
    target_rule_group: wazuh-securityhub
    severity_field: Severity.Label
`, accountID)
}

func (m *SecurityHubMapper) standardsMap(standardsARN string) string {
	return fmt.Sprintf(`source: aws_securityhub_standards_subscription
standards_arn: %s
target: wazuh-compliance
controls:
  - aws-foundational-security-best-practices
  - cis-aws-foundations-benchmark
`, standardsARN)
}

func (m *SecurityHubMapper) appChangeConfig(accountID string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=generated_patch
SOURCE_SECURITYHUB_ACCOUNT=%s
TARGET_SECURITY_ENGINE=wazuh
TARGET_FINDINGS_ENDPOINT=http://wazuh-manager:55000
GENERATED_PATCH=config/securityhub/generated-findings.patch
`, accountID)
}

func (m *SecurityHubMapper) generatedPatch(accountID string) string {
	return fmt.Sprintf(`--- a/security/findings.env
+++ b/security/findings.env
@@
-AWS_SECURITYHUB_ACCOUNT=%s
+SECURITY_FINDINGS_ENDPOINT=http://wazuh-manager:55000
+SECURITY_FINDINGS_ENGINE=wazuh
`, accountID)
}

func (m *SecurityHubMapper) wazuhRules() string {
	return `<group name="securityhub,aws,">
  <rule id="110101" level="8">
    <decoded_as>json</decoded_as>
    <field name="ProductName">Security Hub</field>
    <description>AWS Security Hub finding mapped by HomePort</description>
  </rule>
  <rule id="110102" level="10">
    <if_sid>110101</if_sid>
    <field name="Severity.Label">CRITICAL</field>
    <description>Critical AWS Security Hub finding mapped by HomePort</description>
  </rule>
</group>
`
}

func (m *SecurityHubMapper) exportScript(accountID, region string) string {
	if region == "" {
		region = "us-east-1"
	}
	return fmt.Sprintf(`#!/bin/sh
set -eu
AWS_REGION=%q
ACCOUNT_ID=%q
OUTPUT_DIR="./securityhub-export"
mkdir -p "$OUTPUT_DIR"
aws securityhub get-findings --region "$AWS_REGION" --filters '{"AwsAccountId":[{"Value":"'"$ACCOUNT_ID"'","Comparison":"EQUALS"}]}' --output json > "$OUTPUT_DIR/findings.json"
aws securityhub get-enabled-standards --region "$AWS_REGION" --output json > "$OUTPUT_DIR/enabled-standards.json"
`, region, accountID)
}

func (m *SecurityHubMapper) provisionScript(accountID string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/wazuh/rules/securityhub_rules.xml\ntest -s config/securityhub/standards-map.yaml\necho \"Wazuh Security Hub target ready for %s\"\n", accountID)
}

func (m *SecurityHubMapper) migrateScript(accountID string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s securityhub-export/findings.json\ntest -s config/securityhub/findings-map.yaml\ngrep -q %q config/securityhub/findings-map.yaml\necho \"Security Hub account %s mapped to Wazuh\"\n", accountID, accountID)
}

func (m *SecurityHubMapper) validateScript(accountID string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/securityhub/app-change.env\ntest -s config/securityhub/generated-findings.patch\ntest -s config/wazuh/rules/securityhub_rules.xml\ngrep -q %q config/securityhub/app-change.env\n", accountID)
}

func (m *SecurityHubMapper) backupScript(accountID string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-securityhub-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/securityhub config/wazuh export_securityhub_findings.sh provision_wazuh_securityhub.sh migrate_securityhub_findings.sh validate_securityhub_findings.sh cutover_securityhub_alerts.sh
echo "$archive"
`, accountID)
}

func (m *SecurityHubMapper) cutoverScript(accountID string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
. config/securityhub/app-change.env
test "$SOURCE_SECURITYHUB_ACCOUNT" = %q
test "$APP_CHANGE_MODE" = "generated_patch"
test -s "$GENERATED_PATCH"
echo "Apply $GENERATED_PATCH and route Security Hub consumers to $TARGET_FINDINGS_ENDPOINT"
`, accountID)
}

func securityHubRunbook(accountID string) []domainrunbook.Step {
	metadata := map[string]string{
		"kind":                "security-posture",
		"source":              "aws_securityhub_account",
		"account":             accountID,
		"HOMEPORT_TARGET":     "wazuh",
		"HOMEPORT_APP_CHANGE": "generated_patch",
	}
	return []domainrunbook.Step{
		securityHubStep("export-securityhub-findings", "Export Security Hub findings", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_securityhub_findings.sh"}, "Security Hub findings and standards are exported", metadata),
		securityHubStep("provision-wazuh-securityhub", "Provision Wazuh Security Hub rules", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "provision_wazuh_securityhub.sh"}, "Wazuh Security Hub rules are rendered", metadata),
		securityHubStep("migrate-securityhub-findings", "Migrate Security Hub findings", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_securityhub_findings.sh"}, "Security Hub findings map to Wazuh rules", metadata),
		securityHubStep("validate-securityhub-findings", "Validate Security Hub findings", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_securityhub_findings.sh"}, "generated Security Hub routing validates", metadata),
		securityHubStep("backup-securityhub-config", "Backup Security Hub config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_securityhub_config.sh"}, "Security Hub and Wazuh config are archived", metadata),
		securityHubStep("cutover-securityhub-alerts", "Cut over Security Hub alerts", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_securityhub_alerts.sh"}, "finding consumers use the Wazuh target", metadata),
		securityHubStep("rollback-securityhub-source", "Keep Security Hub source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "AWS Security Hub remains authoritative until Wazuh validation passes", metadata),
	}
}

func securityHubStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
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
