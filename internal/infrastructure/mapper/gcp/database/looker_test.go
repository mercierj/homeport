package database

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestLookerConformanceManagedAToZ(t *testing.T) {
	result, err := NewLookerMapper().Map(context.Background(), managedLookerFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Looker migration", result.ManualSteps)
	}
	if result.DockerService.Image != "apache/superset:4.0.2" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Superset target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/superset/looker-dashboard.yaml", "config/looker/app-change.env", "config/looker/generated-dashboard.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/looker/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_LOOKER_INSTANCE=analytics-looker", "TARGET_BI=apache-superset", "SUPERSET_URL=http://superset:8088"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_looker_assets.sh", "provision_looker_superset.sh", "migrate_looker_dashboards.sh", "validate_looker_superset.sh", "backup_looker_assets.sh", "cutover_looker_users.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-looker-assets":      domainrunbook.StepTypeCommand,
		"provision-looker-superset": domainrunbook.StepTypeCommand,
		"migrate-looker-dashboards": domainrunbook.StepTypeCommand,
		"validate-looker-superset":  domainrunbook.StepTypeCommand,
		"backup-looker-assets":      domainrunbook.StepTypeCommand,
		"cutover-looker-users":      domainrunbook.StepTypeAPICall,
		"rollback-looker-source":    domainrunbook.StepTypeRollback,
	} {
		if !hasLookerRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewLookerMapper(t *testing.T) {
	m := NewLookerMapper()
	if m == nil {
		t.Fatal("NewLookerMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeLookerInstance {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeLookerInstance)
	}
}

func managedLookerFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "projects/demo/locations/europe-west1/instances/analytics-looker",
		Type: resource.TypeLookerInstance,
		Name: "analytics-looker",
		Config: map[string]interface{}{
			"name":     "analytics-looker",
			"platform": "LOOKER_CORE_STANDARD",
			"region":   "europe-west1",
		},
	}
}

func hasLookerRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
