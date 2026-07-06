package database

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestQuickSightConformanceManagedAToZ(t *testing.T) {
	result, err := NewQuickSightMapper().Map(context.Background(), managedQuickSightFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated QuickSight migration", result.ManualSteps)
	}
	if result.DockerService.Image != "apache/superset:4.0.2" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Superset target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/superset/dashboard.yaml", "config/quicksight/app-change.env", "config/quicksight/generated-dashboard.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/quicksight/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_QUICKSIGHT_DASHBOARD=exec-kpis", "TARGET_BI=apache-superset", "SUPERSET_URL=http://superset:8088"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_quicksight_assets.sh", "provision_superset.sh", "migrate_quicksight_dashboard.sh", "validate_superset_dashboard.sh", "backup_quicksight_assets.sh", "cutover_quicksight_users.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-quicksight-assets":      domainrunbook.StepTypeCommand,
		"provision-superset":            domainrunbook.StepTypeCommand,
		"migrate-quicksight-dashboard":  domainrunbook.StepTypeCommand,
		"validate-superset-dashboard":   domainrunbook.StepTypeCommand,
		"backup-quicksight-assets":      domainrunbook.StepTypeCommand,
		"cutover-quicksight-users":      domainrunbook.StepTypeAPICall,
		"rollback-quicksight-dashboard": domainrunbook.StepTypeRollback,
	} {
		if !hasQuickSightRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewQuickSightMapper(t *testing.T) {
	m := NewQuickSightMapper()
	if m == nil {
		t.Fatal("NewQuickSightMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeQuickSightDashboard {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeQuickSightDashboard)
	}
}

func managedQuickSightFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:     "exec-kpis",
		Type:   resource.TypeQuickSightDashboard,
		Name:   "exec-kpis",
		Region: "eu-west-1",
		Config: map[string]interface{}{
			"dashboard_id": "exec-kpis",
		},
	}
}

func hasQuickSightRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
