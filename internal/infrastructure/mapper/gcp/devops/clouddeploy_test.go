package devops

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestCloudDeployConformanceManagedAToZ(t *testing.T) {
	result, err := NewCloudDeployPipelineMapper().Map(context.Background(), managedCloudDeployFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Cloud Deploy migration", result.ManualSteps)
	}
	if result.DockerService.Image != "quay.io/argoproj/argocd:v2.11.4" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Argo CD target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/argocd/application.yaml", "config/clouddeploy/app-change.env", "config/clouddeploy/generated-argocd.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/clouddeploy/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_CLOUD_DEPLOY_PIPELINE=checkout-release", "TARGET_GITOPS=argocd"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_cloud_deploy_pipeline.sh", "provision_argocd_app.sh", "migrate_cloud_deploy_targets.sh", "validate_argocd_app.sh", "backup_cloud_deploy_config.sh", "cutover_cloud_deploy_releases.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-cloud-deploy-pipeline":  domainrunbook.StepTypeCommand,
		"provision-argocd-app":          domainrunbook.StepTypeCommand,
		"migrate-cloud-deploy-targets":  domainrunbook.StepTypeCommand,
		"validate-argocd-app":           domainrunbook.StepTypeCommand,
		"backup-cloud-deploy-config":    domainrunbook.StepTypeCommand,
		"cutover-cloud-deploy-releases": domainrunbook.StepTypeAPICall,
		"rollback-cloud-deploy-source":  domainrunbook.StepTypeRollback,
	} {
		if !hasCloudDeployRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewCloudDeployMappers(t *testing.T) {
	if NewCloudDeployPipelineMapper().ResourceType() != resource.TypeCloudDeployDeliveryPipeline {
		t.Fatalf("pipeline mapper type = %s, want %s", NewCloudDeployPipelineMapper().ResourceType(), resource.TypeCloudDeployDeliveryPipeline)
	}
	if NewCloudDeployTargetMapper().ResourceType() != resource.TypeCloudDeployTarget {
		t.Fatalf("target mapper type = %s, want %s", NewCloudDeployTargetMapper().ResourceType(), resource.TypeCloudDeployTarget)
	}
}

func managedCloudDeployFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "projects/demo/locations/europe-west1/deliveryPipelines/checkout-release",
		Type: resource.TypeCloudDeployDeliveryPipeline,
		Name: "checkout-release",
		Config: map[string]interface{}{
			"name":   "checkout-release",
			"region": "europe-west1",
		},
	}
}

func hasCloudDeployRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
