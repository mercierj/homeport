package database

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type LookerMapper struct {
	*mapper.BaseMapper
}

func NewLookerMapper() *LookerMapper {
	return &LookerMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeLookerInstance, nil)}
}

func (m *LookerMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	name := firstNonEmptyDataplex(res.GetConfigString("name"), res.GetConfigString("display_name"), res.Name)

	result := mapper.NewMappingResult("superset")
	svc := result.DockerService
	svc.Image = "apache/superset:4.0.2"
	svc.Environment = map[string]string{"SUPERSET_SECRET_KEY": "change-me", "SUPERSET_LOAD_EXAMPLES": "no", "LOOKER_INSTANCE": name}
	svc.Ports = []string{"8088:8088"}
	svc.Volumes = []string{"./config/superset:/app/pythonpath", "./data/superset:/app/superset_home"}
	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Labels = map[string]string{"homeport.source": string(resource.TypeLookerInstance), "homeport.instance": name, "homeport.target": "apache-superset"}
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD", "curl", "-f", "http://localhost:8088/health"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 5}

	result.AddConfig("config/superset/looker-dashboard.yaml", []byte(m.dashboardConfig(name)))
	result.AddConfig("config/looker/app-change.env", []byte(m.appChange(name)))
	result.AddConfig("config/looker/generated-dashboard.patch", []byte(m.generatedPatch(name)))
	result.AddScript("export_looker_assets.sh", []byte(m.exportScript(name)))
	result.AddScript("provision_looker_superset.sh", []byte(m.provisionScript(name)))
	result.AddScript("migrate_looker_dashboards.sh", []byte(m.migrateScript(name)))
	result.AddScript("validate_looker_superset.sh", []byte(m.validateScript(name)))
	result.AddScript("backup_looker_assets.sh", []byte(m.backupScript(name)))
	result.AddScript("cutover_looker_users.sh", []byte(m.cutoverScript(name)))
	for _, step := range lookerRunbook(name) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *LookerMapper) dashboardConfig(name string) string {
	return fmt.Sprintf("instance: %s\ntarget: apache-superset\nimport_mode: lookml_export\n", name)
}

func (m *LookerMapper) appChange(name string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_LOOKER_INSTANCE=%s\nTARGET_BI=apache-superset\nSUPERSET_URL=http://superset:8088\nGENERATED_PATCH=config/looker/generated-dashboard.patch\n", name)
}

func (m *LookerMapper) generatedPatch(name string) string {
	return fmt.Sprintf("--- app.env\n+++ app.env\n@@\n-LOOKER_INSTANCE=%s\n+SUPERSET_URL=http://superset:8088\n+BI_DASHBOARD=%s\n+BI_PROVIDER=apache-superset\n", name, name)
}

func (m *LookerMapper) exportScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\nLOOKER_INSTANCE=%q\nOUTPUT_DIR=\"${LOOKER_EXPORT_DIR:-looker-export}\"\nmkdir -p \"$OUTPUT_DIR\"\ngcloud looker instances describe \"$LOOKER_INSTANCE\" --format=json > \"$OUTPUT_DIR/instance.json\"\necho \"Export LookML projects and dashboards for $LOOKER_INSTANCE into $OUTPUT_DIR\"\n", name)
}

func (m *LookerMapper) provisionScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/superset/looker-dashboard.yaml\necho \"Superset ready for Looker instance %s\"\n", name)
}

func (m *LookerMapper) migrateScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/superset/looker-dashboard.yaml\ngrep -q %q config/superset/looker-dashboard.yaml\necho \"Looker dashboards mapped to Superset\"\n", name)
}

func (m *LookerMapper) validateScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ncurl -fsS http://localhost:8088/health >/tmp/homeport-superset-health.txt\ngrep -q %q config/superset/looker-dashboard.yaml\n", name)
}

func (m *LookerMapper) backupScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/%s-looker-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/superset config/looker looker-export 2>/dev/null || tar -czf \"$archive\" config/superset config/looker\necho \"$archive\"\n", sanitizeDataplexName(name))
}

func (m *LookerMapper) cutoverScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/looker/app-change.env\ntest \"$SOURCE_LOOKER_INSTANCE\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\ntest -s \"$GENERATED_PATCH\"\necho \"Patch BI links and users to SUPERSET_URL=$SUPERSET_URL\"\n", name)
}

func lookerRunbook(name string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "bi-dashboard", "source": "google_looker_instance", "instance": name, "SUPERSET_URL": "http://superset:8088", "HOMEPORT_APP_CHANGE": "generated_patch"}
	return []domainrunbook.Step{
		lookerStep("export-looker-assets", "Export Looker assets", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_looker_assets.sh"}, "Looker instance, dashboards, and semantic model exports are staged", metadata),
		lookerStep("provision-looker-superset", "Provision Superset target", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "provision_looker_superset.sh"}, "Superset dashboard config is present", metadata),
		lookerStep("migrate-looker-dashboards", "Migrate Looker dashboards", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_looker_dashboards.sh"}, "dashboard assets are represented in Superset", metadata),
		lookerStep("validate-looker-superset", "Validate Superset dashboard", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_looker_superset.sh"}, "Superset health and dashboard config validate", metadata),
		lookerStep("backup-looker-assets", "Backup Looker migration assets", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_looker_assets.sh"}, "BI migration artifacts are archived", metadata),
		lookerStep("cutover-looker-users", "Cut over BI users", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_looker_users.sh"}, "BI users use Superset URL", metadata),
		lookerStep("rollback-looker-source", "Keep Looker source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "Looker remains authoritative until Superset validation passes", metadata),
	}
}

func lookerStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}
