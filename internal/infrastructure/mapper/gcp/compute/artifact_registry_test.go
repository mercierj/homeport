package compute

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestArtifactRegistryConformanceManagedAToZ(t *testing.T) {
	result, err := NewArtifactRegistryMapper().Map(context.Background(), managedArtifactRegistryFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Artifact Registry migration", result.ManualSteps)
	}
	if result.DockerService.Image != "registry:2" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA OCI registry: %#v", result.DockerService)
	}
	for _, file := range []string{"config/artifact-registry/registry.yml", "config/artifact-registry/app-change.env"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/artifact-registry/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_ARTIFACT_REGISTRY=orders-api", "TARGET_REGISTRY=registry:5000"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"sync_artifact_registry.sh", "backup_artifact_registry.sh", "validate_artifact_registry.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"discover-artifact-registry-images": domainrunbook.StepTypeCommand,
		"provision-artifact-oci-registry":   domainrunbook.StepTypeCommand,
		"sync-artifact-registry-images":     domainrunbook.StepTypeCommand,
		"validate-artifact-oci-registry":    domainrunbook.StepTypeCommand,
		"backup-artifact-registry":          domainrunbook.StepTypeCommand,
		"cutover-artifact-image-refs":       domainrunbook.StepTypeAPICall,
		"rollback-artifact-source-images":   domainrunbook.StepTypeRollback,
	} {
		if !hasArtifactRegistryRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewArtifactRegistryMapper(t *testing.T) {
	m := NewArtifactRegistryMapper()
	if m == nil {
		t.Fatal("NewArtifactRegistryMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeArtifactRegistryRepository {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeArtifactRegistryRepository)
	}
}

func managedArtifactRegistryFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:     "projects/demo/locations/europe-west1/repositories/orders-api",
		Type:   resource.TypeArtifactRegistryRepository,
		Name:   "orders-api",
		Region: "europe-west1",
		Config: map[string]interface{}{
			"name":     "orders-api",
			"location": "europe-west1",
			"format":   "DOCKER",
		},
	}
}

func hasArtifactRegistryRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
