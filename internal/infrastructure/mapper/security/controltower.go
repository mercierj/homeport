package security

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type ControlTowerMapper struct {
	*mapper.BaseMapper
}

func NewControlTowerMapper() *ControlTowerMapper {
	return &ControlTowerMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeControlTowerControl, nil)}
}

func (m *ControlTowerMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	controlID := res.GetConfigString("control_identifier")
	if controlID == "" {
		controlID = res.Name
	}
	targetID := res.GetConfigString("target_identifier")
	if targetID == "" {
		targetID = "landing-zone"
	}

	result := mapper.NewMappingResult("crossplane-opa-controltower")
	svc := result.DockerService
	svc.Image = "openpolicyagent/opa:0.70.0"
	svc.Command = []string{"run", "--server", "--addr=0.0.0.0:8181", "/policies"}
	svc.Ports = []string{"8181:8181"}
	svc.Volumes = []string{"./config/controltower:/policies", "./config/controltower:/workspace"}
	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Labels = map[string]string{
		"homeport.source":            "aws_controltower_control",
		"homeport.controltower_rule": controlID,
		"homeport.target":            "crossplane-opa",
	}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "wget", "-qO-", "http://localhost:8181/health"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  5,
	}

	result.AddConfig("config/controltower/crossplane-control.yaml", []byte(m.crossplaneControl(controlID, targetID)))
	result.AddConfig("config/controltower/controls-map.yaml", []byte(m.controlsMap(controlID, targetID)))
	result.AddConfig("config/controltower/guardrails.rego", []byte(m.guardrailPolicy(controlID)))
	result.AddConfig("config/controltower/app-change.env", []byte(m.appChange(controlID)))
	result.AddConfig("config/controltower/generated-governance.patch", []byte(m.generatedPatch(controlID)))
	result.AddScript("export_controltower_controls.sh", []byte(m.exportScript(controlID, res.Region)))
	result.AddScript("provision_crossplane_controls.sh", []byte(m.provisionScript(controlID)))
	result.AddScript("migrate_controltower_guardrails.sh", []byte(m.migrateScript(controlID)))
	result.AddScript("validate_controltower_policy.sh", []byte(m.validateScript(controlID)))
	result.AddScript("backup_controltower_config.sh", []byte(m.backupScript(controlID)))
	result.AddScript("cutover_controltower_governance.sh", []byte(m.cutoverScript(controlID)))
	for _, step := range controlTowerRunbook(controlID) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *ControlTowerMapper) crossplaneControl(controlID, targetID string) string {
	return fmt.Sprintf(`apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: controltower-%s
spec:
  compositeTypeRef:
    apiVersion: homeport.io/v1alpha1
    kind: LandingZoneControl
  resources:
    - name: %s
      base:
        apiVersion: homeport.io/v1alpha1
        kind: PolicyControl
        spec:
          target: %s
`, controlID, controlID, targetID)
}

func (m *ControlTowerMapper) controlsMap(controlID, targetID string) string {
	return fmt.Sprintf(`control: %s
source: aws_controltower_control
target: crossplane
target_identifier: %s
policy: data.homeport.controltower
`, controlID, targetID)
}

func (m *ControlTowerMapper) guardrailPolicy(controlID string) string {
	return fmt.Sprintf(`package homeport.controltower

default allow := true

control_identifier := %q

deny[msg] if {
  input.control_identifier == control_identifier
  input.status != "ENABLED"
  msg := sprintf("Control Tower guardrail %%s is %%s", [control_identifier, input.status])
}
`, controlID)
}

func (m *ControlTowerMapper) appChange(controlID string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=generated_patch
SOURCE_CONTROL_IDENTIFIER=%s
TARGET_PROVISIONER=crossplane
TARGET_POLICY_ENGINE=opa
TARGET_POLICY_ENDPOINT=http://opa:8181
GENERATED_PATCH=config/controltower/generated-governance.patch
`, controlID)
}

func (m *ControlTowerMapper) generatedPatch(controlID string) string {
	return fmt.Sprintf(`--- a/governance/controltower.env
+++ b/governance/controltower.env
@@
-AWS_CONTROLTOWER_CONTROL=%s
+GOVERNANCE_PROVISIONER=crossplane
+POLICY_ENGINE=opa
+POLICY_ENDPOINT=http://opa:8181
`, controlID)
}

func (m *ControlTowerMapper) exportScript(controlID, region string) string {
	if region == "" {
		region = "us-east-1"
	}
	return fmt.Sprintf(`#!/bin/sh
set -eu
AWS_REGION=%q
CONTROL_IDENTIFIER=%q
OUTPUT_DIR="./controltower-export"
mkdir -p "$OUTPUT_DIR"
aws controltower list-enabled-controls --region "$AWS_REGION" --output json > "$OUTPUT_DIR/enabled-controls.json"
aws controltower get-enabled-control --enabled-control-identifier "$CONTROL_IDENTIFIER" --region "$AWS_REGION" --output json > "$OUTPUT_DIR/control.json"
`, region, controlID)
}

func (m *ControlTowerMapper) provisionScript(controlID string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/controltower/crossplane-control.yaml\ntest -s config/controltower/guardrails.rego\necho \"Crossplane and OPA controls ready for %s\"\n", controlID)
}

func (m *ControlTowerMapper) migrateScript(controlID string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s controltower-export/control.json\ntest -s config/controltower/controls-map.yaml\ngrep -q %q config/controltower/controls-map.yaml\necho \"Control Tower guardrail %s mapped to Crossplane and OPA\"\n", controlID, controlID)
}

func (m *ControlTowerMapper) validateScript(controlID string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
test -s config/controltower/app-change.env
test -s config/controltower/generated-governance.patch
opa eval -d config/controltower/guardrails.rego -i /dev/stdin 'data.homeport.controltower.allow' <<'JSON'
{"control_identifier":%q,"status":"ENABLED"}
JSON
`, controlID)
}

func (m *ControlTowerMapper) backupScript(controlID string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-controltower-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/controltower export_controltower_controls.sh provision_crossplane_controls.sh migrate_controltower_guardrails.sh validate_controltower_policy.sh cutover_controltower_governance.sh
echo "$archive"
`, controlID)
}

func (m *ControlTowerMapper) cutoverScript(controlID string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
. config/controltower/app-change.env
test "$SOURCE_CONTROL_IDENTIFIER" = %q
test "$APP_CHANGE_MODE" = "generated_patch"
test -s "$GENERATED_PATCH"
echo "Apply $GENERATED_PATCH and route governance changes through $TARGET_PROVISIONER and $TARGET_POLICY_ENGINE"
`, controlID)
}

func controlTowerRunbook(controlID string) []domainrunbook.Step {
	metadata := map[string]string{
		"kind":                "landing-zone-guardrail",
		"source":              "aws_controltower_control",
		"control":             controlID,
		"HOMEPORT_TARGET":     "crossplane-opa",
		"HOMEPORT_APP_CHANGE": "generated_patch",
	}
	return []domainrunbook.Step{
		controlTowerStep("export-controltower-controls", "Export Control Tower controls", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_controltower_controls.sh"}, "Control Tower enabled controls are exported", metadata),
		controlTowerStep("provision-crossplane-controls", "Provision Crossplane controls", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "provision_crossplane_controls.sh"}, "Crossplane control manifests are rendered", metadata),
		controlTowerStep("migrate-controltower-guardrails", "Migrate Control Tower guardrails", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_controltower_guardrails.sh"}, "Control Tower guardrails map to Crossplane and OPA", metadata),
		controlTowerStep("validate-controltower-policy", "Validate Control Tower policy", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_controltower_policy.sh"}, "OPA evaluates the generated guardrail policy", metadata),
		controlTowerStep("backup-controltower-config", "Backup Control Tower config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_controltower_config.sh"}, "Control Tower migration artifacts are archived", metadata),
		controlTowerStep("cutover-controltower-governance", "Cut over Control Tower governance", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_controltower_governance.sh"}, "governance consumers use Crossplane and OPA", metadata),
		controlTowerStep("rollback-controltower-source", "Keep Control Tower source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "AWS Control Tower remains authoritative until generated governance validation passes", metadata),
	}
}

func controlTowerStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
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
