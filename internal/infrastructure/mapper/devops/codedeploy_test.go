package devops

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestCodeDeployConformanceManagedAToZ(t *testing.T) {
	result, err := NewCodeDeployMapper().Map(context.Background(), managedCodeDeployFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated CodeDeploy migration", result.ManualSteps)
	}
	if result.DockerService.Image != "quay.io/argoproj/argo-rollouts:v1.7.2" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Argo Rollouts target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/argo-rollouts/rollout.yaml", "config/codedeploy/app-change.env", "config/codedeploy/generated-rollout.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/codedeploy/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_CODEDEPLOY_APP=shop-api", "TARGET_ROLLOUTS=argo-rollouts", "ROLLOUT_NAMESPACE=default"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_codedeploy_app.sh", "provision_argo_rollouts.sh", "migrate_codedeploy_group.sh", "validate_argo_rollout.sh", "backup_codedeploy_config.sh", "cutover_codedeploy_traffic.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-codedeploy-app":      domainrunbook.StepTypeCommand,
		"provision-argo-rollouts":    domainrunbook.StepTypeCommand,
		"migrate-codedeploy-group":   domainrunbook.StepTypeCommand,
		"validate-argo-rollout":      domainrunbook.StepTypeCommand,
		"backup-codedeploy-config":   domainrunbook.StepTypeCommand,
		"cutover-codedeploy-traffic": domainrunbook.StepTypeAPICall,
		"rollback-codedeploy-source": domainrunbook.StepTypeRollback,
	} {
		if !hasCodeDeployRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewCodeDeployMapper(t *testing.T) {
	m := NewCodeDeployMapper()
	if m == nil {
		t.Fatal("NewCodeDeployMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeCodeDeployApp {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeCodeDeployApp)
	}
}

func managedCodeDeployFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:     "shop-api",
		Type:   resource.TypeCodeDeployApp,
		Name:   "shop-api",
		Region: "eu-west-1",
		Config: map[string]interface{}{
			"app_name":              "shop-api",
			"deployment_group_name": "shop-api-prod",
		},
	}
}

func hasCodeDeployRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
