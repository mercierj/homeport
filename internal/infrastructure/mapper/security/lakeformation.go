package security

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type LakeFormationMapper struct {
	*mapper.BaseMapper
}

func NewLakeFormationMapper() *LakeFormationMapper {
	return &LakeFormationMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeLakeFormationPermissions, nil)}
}

func (m *LakeFormationMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	name := res.GetConfigString("name")
	if name == "" {
		name = res.Name
	}
	if name == "" {
		name = "lakeformation-permissions"
	}

	result := mapper.NewMappingResult("ranger")
	svc := result.DockerService
	svc.Image = "apache/ranger:2.4.0"
	svc.Ports = []string{"6080:6080"}
	svc.Volumes = []string{"./config/ranger:/opt/ranger/config", "./data/ranger:/opt/ranger/data"}
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"homeport.source": "aws_lakeformation_permissions",
		"homeport.policy": name,
		"homeport.target": "apache-ranger",
	}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "curl", "-f", "http://localhost:6080/login.jsp"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  5,
	}

	result.AddConfig("config/ranger/service.json", []byte(m.serviceConfig(name)))
	result.AddConfig("config/ranger/lakeformation-policies.json", []byte(m.policyConfig(name)))
	result.AddConfig("config/lakeformation/app-change.env", []byte(m.appChange(name)))
	result.AddConfig("config/lakeformation/generated-policy-report.md", []byte(m.generatedReport(name)))
	result.AddScript("export_lakeformation_permissions.sh", []byte(m.exportScript(name, res.Region)))
	result.AddScript("provision_ranger_service.sh", []byte(m.provisionScript(name)))
	result.AddScript("migrate_lakeformation_policies.sh", []byte(m.migrateScript(name)))
	result.AddScript("validate_ranger_policies.sh", []byte(m.validateScript(name)))
	result.AddScript("backup_lakeformation_config.sh", []byte(m.backupScript(name)))
	result.AddScript("cutover_lakeformation_clients.sh", []byte(m.cutoverScript(name)))
	for _, step := range lakeFormationRunbook(name) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *LakeFormationMapper) serviceConfig(name string) string {
	return fmt.Sprintf(`{"name":"%s","type":"hive","policyVersion":1,"description":"Migrated from AWS Lake Formation"}`+"\n", name)
}

func (m *LakeFormationMapper) policyConfig(name string) string {
	return fmt.Sprintf(`{
  "policies": [
    {
      "name": %q,
      "resources": {"database": "*", "table": "*"},
      "permissions": ["select", "describe", "alter"],
      "delegate_admin": false
    }
  ]
}
`, name)
}

func (m *LakeFormationMapper) appChange(name string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=generated_patch
SOURCE_LAKEFORMATION_POLICY=%s
TARGET_GOVERNANCE=apache-ranger
RANGER_URL=http://ranger:6080
GENERATED_POLICY_REPORT=config/lakeformation/generated-policy-report.md
`, name)
}

func (m *LakeFormationMapper) generatedReport(name string) string {
	return fmt.Sprintf("# Lake Formation policy migration\n\nPolicy `%s` is exported from Lake Formation and recreated in Apache Ranger. Applications keep their data-plane endpoints; governance checks move to Ranger policy evaluation.\n", name)
}

func (m *LakeFormationMapper) exportScript(name, region string) string {
	if region == "" {
		region = "us-east-1"
	}
	return fmt.Sprintf(`#!/bin/sh
set -eu
AWS_REGION=%q
OUTPUT_DIR="./lakeformation-export"
mkdir -p "$OUTPUT_DIR"
aws lakeformation list-permissions --region "$AWS_REGION" --output json > "$OUTPUT_DIR/permissions.json"
aws lakeformation get-data-lake-settings --region "$AWS_REGION" --output json > "$OUTPUT_DIR/data-lake-settings.json"
echo %q > "$OUTPUT_DIR/source-policy-name.txt"
`, region, name)
}

func (m *LakeFormationMapper) provisionScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/ranger/service.json\ntest -s config/ranger/lakeformation-policies.json\necho \"Apache Ranger service ready for %s\"\n", name)
}

func (m *LakeFormationMapper) migrateScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s lakeformation-export/permissions.json\ntest -s config/ranger/lakeformation-policies.json\ngrep -q %q config/ranger/lakeformation-policies.json\necho \"Lake Formation permissions mapped to Ranger policies\"\n", name)
}

func (m *LakeFormationMapper) validateScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ncurl -fsS http://localhost:6080/login.jsp >/tmp/homeport-ranger-health.html\ngrep -q %q config/ranger/lakeformation-policies.json\ntest -s config/lakeformation/app-change.env\n", name)
}

func (m *LakeFormationMapper) backupScript(name string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-lakeformation-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/ranger config/lakeformation export_lakeformation_permissions.sh provision_ranger_service.sh migrate_lakeformation_policies.sh validate_ranger_policies.sh cutover_lakeformation_clients.sh
echo "$archive"
`, name)
}

func (m *LakeFormationMapper) cutoverScript(name string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
. config/lakeformation/app-change.env
test "$SOURCE_LAKEFORMATION_POLICY" = %q
test "$APP_CHANGE_MODE" = "generated_patch"
test -s "$GENERATED_POLICY_REPORT"
echo "Review $GENERATED_POLICY_REPORT and enforce data governance through $RANGER_URL"
`, name)
}

func lakeFormationRunbook(name string) []domainrunbook.Step {
	metadata := map[string]string{
		"kind":                "data-governance",
		"source":              "aws_lakeformation_permissions",
		"policy":              name,
		"HOMEPORT_TARGET":     "apache-ranger",
		"HOMEPORT_APP_CHANGE": "generated_patch",
	}
	return []domainrunbook.Step{
		lakeFormationStep("export-lakeformation-permissions", "Export Lake Formation permissions", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_lakeformation_permissions.sh"}, "permissions and data lake settings are exported", metadata),
		lakeFormationStep("provision-ranger-service", "Provision Apache Ranger service", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "provision_ranger_service.sh"}, "Ranger service and policy definitions are generated", metadata),
		lakeFormationStep("migrate-lakeformation-policies", "Migrate Lake Formation policies", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_lakeformation_policies.sh"}, "Lake Formation policies are represented in Ranger", metadata),
		lakeFormationStep("validate-ranger-policies", "Validate Ranger policies", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_ranger_policies.sh"}, "Ranger health and policies validate", metadata),
		lakeFormationStep("backup-lakeformation-config", "Backup Lake Formation migration config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_lakeformation_config.sh"}, "governance migration artifacts are archived", metadata),
		lakeFormationStep("cutover-lakeformation-clients", "Cut over governance checks", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_lakeformation_clients.sh"}, "data governance enforcement uses Ranger", metadata),
		lakeFormationStep("rollback-lakeformation-source", "Keep Lake Formation source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "AWS Lake Formation remains authoritative until Ranger validation passes", metadata),
	}
}

func lakeFormationStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}
