package security

import (
	"context"
	"fmt"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type GuardDutyMapper struct {
	*mapper.BaseMapper
}

func NewGuardDutyMapper() *GuardDutyMapper {
	return &GuardDutyMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeGuardDutyDetector, nil)}
}

func (m *GuardDutyMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	detectorID := res.GetConfigString("detector_id")
	if detectorID == "" {
		detectorID = res.Name
	}
	result := mapper.NewMappingResult("wazuh-manager")
	svc := result.DockerService
	svc.Image = "wazuh/wazuh-manager:4.9.0"
	svc.Ports = []string{"1514:1514/udp", "55000:55000"}
	svc.Volumes = []string{"./config/wazuh:/var/ossec/etc/shared", "./data/wazuh:/var/ossec/data"}
	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Environment = map[string]string{"HOMEPORT_SOURCE_GUARDDUTY_DETECTOR": detectorID}
	svc.Labels = map[string]string{
		"homeport.source":   "aws_guardduty_detector",
		"homeport.detector": detectorID,
		"homeport.target":   "wazuh",
	}

	result.AddConfig("config/guardduty/findings-map.yaml", []byte(m.findingsMap(detectorID)))
	result.AddConfig("config/guardduty/app-change.env", []byte(m.appChangeConfig(detectorID)))
	result.AddConfig("config/wazuh/rules/guardduty_rules.xml", []byte(m.wazuhRules()))
	result.AddScript("export_guardduty_findings.sh", []byte(m.exportScript(detectorID)))
	result.AddScript("load_wazuh_rules.sh", []byte("#!/bin/sh\nset -eu\ntest -s config/wazuh/rules/guardduty_rules.xml\n"))
	result.AddScript("backup_guardduty_config.sh", []byte(m.backupScript(detectorID)))
	result.AddScript("validate_guardduty_detection.sh", []byte(m.validateScript(detectorID)))
	for _, step := range guardDutyRunbook(detectorID) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *GuardDutyMapper) findingsMap(detectorID string) string {
	return fmt.Sprintf("detector: %s\nmappings:\n  - source: GuardDutyFinding\n    target_rule_group: wazuh-guardduty\n", detectorID)
}

func (m *GuardDutyMapper) appChangeConfig(detectorID string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=generated_patch
SOURCE_DETECTOR=%s
TARGET_SECURITY_ENGINE=wazuh
TARGET_FINDINGS_ENDPOINT=http://wazuh-manager:55000
`, detectorID)
}

func (m *GuardDutyMapper) wazuhRules() string {
	return `<group name="guardduty,aws,">
  <rule id="110001" level="10">
    <decoded_as>json</decoded_as>
    <field name="service.serviceName">guardduty</field>
    <description>AWS GuardDuty finding mapped by HomePort</description>
  </rule>
</group>
`
}

func (m *GuardDutyMapper) exportScript(detectorID string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\naws guardduty list-findings --detector-id %q > config/guardduty/source-findings.json\n", detectorID)
}

func (m *GuardDutyMapper) backupScript(detectorID string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-guardduty-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/guardduty config/wazuh
echo "$archive"
`, detectorID)
}

func (m *GuardDutyMapper) validateScript(detectorID string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/guardduty/app-change.env\ntest -s config/wazuh/rules/guardduty_rules.xml\necho GuardDuty detector %s mapped to Wazuh\n", detectorID)
}

func guardDutyRunbook(detectorID string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "security-detection", "source": "aws_guardduty_detector", "detector": detectorID}
	return []domainrunbook.Step{
		guardDutyStep("export-guardduty-findings", "Export GuardDuty findings", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_guardduty_findings.sh"}, "GuardDuty findings export is captured", metadata),
		guardDutyStep("provision-wazuh-manager", "Provision Wazuh manager", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s config/wazuh/rules/guardduty_rules.xml"}, "Wazuh rules are rendered", metadata),
		guardDutyStep("load-guardduty-rules", "Load GuardDuty rules", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "load_wazuh_rules.sh"}, "GuardDuty rules are loaded into Wazuh", metadata),
		guardDutyStep("validate-guardduty-detection", "Validate GuardDuty detection", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_guardduty_detection.sh"}, "sample finding maps to a Wazuh alert rule", metadata),
		guardDutyStep("backup-guardduty-config", "Backup GuardDuty config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_guardduty_config.sh"}, "GuardDuty and Wazuh config are archived", metadata),
		guardDutyStep("cutover-guardduty-alerts", "Cut over GuardDuty alerts", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "-c", "test -s config/guardduty/app-change.env"}, "alert consumers use the Wazuh target", metadata),
		guardDutyStep("rollback-guardduty-source", "Keep GuardDuty source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "AWS GuardDuty remains authoritative until Wazuh validation passes", metadata),
	}
}

func guardDutyStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
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
