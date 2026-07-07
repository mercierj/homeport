package compute

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestContainerRegistryConformanceManagedAToZ(t *testing.T) {
	result, err := NewContainerRegistryMapper().Map(context.Background(), managedContainerRegistryFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Container Registry migration", result.ManualSteps)
	}
	if result.DockerService.Image != "registry:2" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA OCI registry: %#v", result.DockerService)
	}
	for _, file := range []string{"config/container-registry/registry.yml", "config/container-registry/app-change.env"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/container-registry/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_AZURE_CONTAINER_REGISTRY=ordersacr", "TARGET_REGISTRY=registry:5000"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"sync_container_registry.sh", "backup_container_registry.sh", "validate_container_registry.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"discover-container-registry-images": domainrunbook.StepTypeCommand,
		"provision-container-oci-registry":   domainrunbook.StepTypeCommand,
		"sync-container-registry-images":     domainrunbook.StepTypeCommand,
		"validate-container-oci-registry":    domainrunbook.StepTypeCommand,
		"backup-container-registry":          domainrunbook.StepTypeCommand,
		"cutover-container-image-refs":       domainrunbook.StepTypeAPICall,
		"rollback-container-source-images":   domainrunbook.StepTypeRollback,
	} {
		if !hasContainerRegistryRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func managedContainerRegistryFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:     "/subscriptions/demo/resourceGroups/rg/providers/Microsoft.ContainerRegistry/registries/ordersacr",
		Type:   resource.TypeAzureContainerRegistry,
		Name:   "ordersacr",
		Region: "westeurope",
		Config: map[string]interface{}{"name": "ordersacr", "login_server": "ordersacr.azurecr.io"},
	}
}

func hasContainerRegistryRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
