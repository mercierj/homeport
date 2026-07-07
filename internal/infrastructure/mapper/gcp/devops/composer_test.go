package devops

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestComposerConformanceManagedAToZ(t *testing.T) {
	result, err := NewComposerMapper().Map(context.Background(), managedComposerFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Composer migration", result.ManualSteps)
	}
	if result.DockerService.Image != "apache/airflow:2.9.3" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Airflow target: %#v", result.DockerService)
	}
	for _, file := range []string{"docker-compose.airflow.yml", "config/airflow/airflow.cfg", "config/composer/app-change.env", "config/composer/environment-report.yaml"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/composer/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_COMPOSER_ENVIRONMENT=prod-composer", "TARGET_AIRFLOW_URL=http://airflow:8080"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_composer_environment.sh", "migrate_composer_dags.sh", "validate_composer_airflow.sh", "backup_composer_config.sh", "cutover_composer_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-composer-environment": domainrunbook.StepTypeCommand,
		"provision-airflow":           domainrunbook.StepTypeCommand,
		"migrate-composer-dags":       domainrunbook.StepTypeCommand,
		"validate-airflow":            domainrunbook.StepTypeCommand,
		"backup-composer-config":      domainrunbook.StepTypeCommand,
		"cutover-composer-clients":    domainrunbook.StepTypeAPICall,
		"rollback-composer-source":    domainrunbook.StepTypeRollback,
	} {
		if !hasComposerRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewComposerMapper(t *testing.T) {
	m := NewComposerMapper()
	if m == nil {
		t.Fatal("NewComposerMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeComposerEnvironment {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeComposerEnvironment)
	}
}

func managedComposerFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "projects/demo/locations/europe-west1/environments/prod-composer",
		Type: resource.TypeComposerEnvironment,
		Name: "prod-composer",
		Config: map[string]interface{}{
			"name":   "prod-composer",
			"region": "europe-west1",
		},
	}
}

func hasComposerRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
