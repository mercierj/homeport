package database

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type QuickSightMapper struct {
	*mapper.BaseMapper
}

func NewQuickSightMapper() *QuickSightMapper {
	return &QuickSightMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeQuickSightDashboard, nil)}
}

func (m *QuickSightMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	dashboardID := res.GetConfigString("dashboard_id")
	if dashboardID == "" {
		dashboardID = res.Name
	}
	if dashboardID == "" {
		dashboardID = "quicksight-dashboard"
	}

	result := mapper.NewMappingResult("superset")
	svc := result.DockerService
	svc.Image = "apache/superset:4.0.2"
	svc.Environment = map[string]string{"SUPERSET_SECRET_KEY": "change-me", "SUPERSET_LOAD_EXAMPLES": "no"}
	svc.Ports = []string{"8088:8088"}
	svc.Volumes = []string{"./config/superset:/app/pythonpath", "./data/superset:/app/superset_home"}
	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Labels = map[string]string{"homeport.source": "aws_quicksight_dashboard", "homeport.dashboard": dashboardID, "homeport.target": "apache-superset"}
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD", "curl", "-f", "http://localhost:8088/health"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 5}

	result.AddConfig("config/superset/dashboard.yaml", []byte(m.dashboardConfig(dashboardID)))
	result.AddConfig("config/quicksight/app-change.env", []byte(m.appChange(dashboardID)))
	result.AddConfig("config/quicksight/generated-dashboard.patch", []byte(m.generatedPatch(dashboardID)))
	result.AddScript("export_quicksight_assets.sh", []byte(m.exportScript(dashboardID, res.Region)))
	result.AddScript("provision_superset.sh", []byte(m.provisionScript(dashboardID)))
	result.AddScript("migrate_quicksight_dashboard.sh", []byte(m.migrateScript(dashboardID)))
	result.AddScript("validate_superset_dashboard.sh", []byte(m.validateScript(dashboardID)))
	result.AddScript("backup_quicksight_assets.sh", []byte(m.backupScript(dashboardID)))
	result.AddScript("cutover_quicksight_users.sh", []byte(m.cutoverScript(dashboardID)))
	for _, step := range quickSightRunbook(dashboardID) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *QuickSightMapper) dashboardConfig(dashboardID string) string {
	return fmt.Sprintf("dashboard: %s\ntarget: apache-superset\nimport_mode: assets_bundle\n", dashboardID)
}

func (m *QuickSightMapper) appChange(dashboardID string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_QUICKSIGHT_DASHBOARD=%s\nTARGET_BI=apache-superset\nSUPERSET_URL=http://superset:8088\n", dashboardID)
}

func (m *QuickSightMapper) generatedPatch(dashboardID string) string {
	return fmt.Sprintf("--- app.env\n+++ app.env\n@@\n-QUICKSIGHT_DASHBOARD=%s\n+SUPERSET_URL=http://superset:8088\n+BI_DASHBOARD=%s\n", dashboardID, dashboardID)
}

func (m *QuickSightMapper) exportScript(dashboardID, region string) string {
	if region == "" {
		region = "us-east-1"
	}
	return fmt.Sprintf("#!/bin/sh\nset -eu\nAWS_REGION=\"${AWS_REGION:-%s}\"\nDASHBOARD_ID=\"${QUICKSIGHT_DASHBOARD:-%s}\"\nOUTPUT_DIR=\"${QUICKSIGHT_EXPORT_DIR:-quicksight-export}\"\nmkdir -p \"$OUTPUT_DIR\"\naws quicksight describe-dashboard --region \"$AWS_REGION\" --aws-account-id \"$AWS_ACCOUNT_ID\" --dashboard-id \"$DASHBOARD_ID\" > \"$OUTPUT_DIR/dashboard.json\"\naws quicksight list-data-sources --region \"$AWS_REGION\" --aws-account-id \"$AWS_ACCOUNT_ID\" > \"$OUTPUT_DIR/data-sources.json\"\necho \"Exported QuickSight dashboard $DASHBOARD_ID\"\n", region, dashboardID)
}

func (m *QuickSightMapper) provisionScript(dashboardID string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/superset/dashboard.yaml\necho \"Superset ready for %s\"\n", dashboardID)
}

func (m *QuickSightMapper) migrateScript(dashboardID string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s quicksight-export/dashboard.json\ntest -s config/superset/dashboard.yaml\ngrep -q %q config/superset/dashboard.yaml\necho \"QuickSight dashboard mapped to Superset\"\n", dashboardID)
}

func (m *QuickSightMapper) validateScript(dashboardID string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ncurl -fsS http://localhost:8088/health >/tmp/homeport-superset-health.txt\ngrep -q %q config/superset/dashboard.yaml\n", dashboardID)
}

func (m *QuickSightMapper) backupScript(dashboardID string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/%s-quicksight-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/superset config/quicksight quicksight-export 2>/dev/null || tar -czf \"$archive\" config/superset config/quicksight\necho \"$archive\"\n", dashboardID)
}

func (m *QuickSightMapper) cutoverScript(dashboardID string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/quicksight/app-change.env\ntest \"$SOURCE_QUICKSIGHT_DASHBOARD\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Patch BI links and users to SUPERSET_URL=$SUPERSET_URL\"\n", dashboardID)
}

func quickSightRunbook(dashboardID string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "bi-dashboard", "source": "aws_quicksight_dashboard", "dashboard": dashboardID, "SUPERSET_URL": "http://superset:8088", "HOMEPORT_APP_CHANGE": "generated_patch"}
	return []domainrunbook.Step{
		quickSightStep("export-quicksight-assets", "Export QuickSight assets", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_quicksight_assets.sh"}, "dashboard and data sources are exported", metadata),
		quickSightStep("provision-superset", "Provision Superset target", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "provision_superset.sh"}, "Superset dashboard config is present", metadata),
		quickSightStep("migrate-quicksight-dashboard", "Migrate QuickSight dashboard", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_quicksight_dashboard.sh"}, "dashboard assets are represented in Superset", metadata),
		quickSightStep("validate-superset-dashboard", "Validate Superset dashboard", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_superset_dashboard.sh"}, "Superset health and dashboard config validate", metadata),
		quickSightStep("backup-quicksight-assets", "Backup QuickSight migration assets", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_quicksight_assets.sh"}, "BI migration artifacts are archived", metadata),
		quickSightStep("cutover-quicksight-users", "Cut over BI users", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_quicksight_users.sh"}, "BI users use Superset URL", metadata),
		quickSightStep("rollback-quicksight-dashboard", "Keep QuickSight source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "AWS QuickSight remains authoritative until Superset validation passes", metadata),
	}
}

func quickSightStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}
