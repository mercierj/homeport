package compute

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestContainerAppConformanceManagedAToZ(t *testing.T) {
	result, err := NewContainerAppMapper().Map(context.Background(), managedContainerAppFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Container Apps migration", result.ManualSteps)
	}
	if result.DockerService.Image != "ghcr.io/example/checkout:1.2.3" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA container app target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/container-apps/app-change.env", "config/container-apps/generated-client.patch", "config/container-apps/knative-service.yaml"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/container-apps/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_AZURE_CONTAINER_APP=checkout-api", "TARGET_SERVICE=checkout-api", "CONTAINER_APP_URL=http://checkout-api.localhost"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"deploy_container_app.sh", "validate_container_app.sh", "backup_container_app.sh", "cutover_container_app.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"resolve-app-image":                 domainrunbook.StepTypeCommand,
		"deploy-compose-app":                domainrunbook.StepTypeCommand,
		"validate-app-health":               domainrunbook.StepTypeCommand,
		"backup-container-app-config":       domainrunbook.StepTypeCommand,
		"cutover-container-app-clients":     domainrunbook.StepTypeAPICall,
		"rollback-compute-source-authority": domainrunbook.StepTypeRollback,
	} {
		if !hasContainerAppRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func managedContainerAppFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "/subscriptions/demo/resourceGroups/rg/providers/Microsoft.App/containerApps/checkout-api",
		Type: resource.TypeAzureContainerApp,
		Name: "checkout-api",
		Config: map[string]interface{}{
			"name": "checkout-api",
			"template": map[string]interface{}{
				"container": []interface{}{
					map[string]interface{}{
						"name":  "checkout-api",
						"image": "ghcr.io/example/checkout:1.2.3",
						"env": []interface{}{
							map[string]interface{}{"name": "APP_ENV", "value": "prod"},
						},
					},
				},
				"min_replicas": float64(2),
			},
			"ingress": map[string]interface{}{
				"external_enabled": true,
				"target_port":      float64(8080),
			},
		},
	}
}

func hasContainerAppRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
