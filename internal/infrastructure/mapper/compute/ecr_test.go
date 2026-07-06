package compute

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestECRConformanceManagedAToZ(t *testing.T) {
	if !resource.TypeECRRepository.IsValid() {
		t.Fatalf("%s should be a valid AWS resource type", resource.TypeECRRepository)
	}
	result, err := NewECRMapper().Map(context.Background(), managedECRFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated ECR registry migration", result.ManualSteps)
	}
	if result.DockerService.Image != "registry:2" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA OCI registry: %#v", result.DockerService)
	}
	for _, file := range []string{"config/ecr/registry.yml", "config/ecr/app-change.env"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/ecr/app-change.env"])
	for _, want := range []string{"SOURCE_REPOSITORY=orders-api", "TARGET_REGISTRY=registry:5000", "APP_CHANGE_MODE=generated_patch"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"sync_ecr_repository.sh", "backup_ecr_registry.sh", "validate_ecr_registry.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"discover-ecr-images":        domainrunbook.StepTypeCommand,
		"provision-oci-registry":     domainrunbook.StepTypeCommand,
		"sync-ecr-images":            domainrunbook.StepTypeCommand,
		"validate-oci-registry":      domainrunbook.StepTypeCommand,
		"backup-ecr-registry":        domainrunbook.StepTypeCommand,
		"cutover-ecr-image-refs":     domainrunbook.StepTypeAPICall,
		"rollback-ecr-source-images": domainrunbook.StepTypeRollback,
	} {
		if !hasECRRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func managedECRFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "orders-api",
		Type: resource.TypeECRRepository,
		Name: "orders-api",
		Config: map[string]interface{}{
			"name":                 "orders-api",
			"repository_url":       "123456789012.dkr.ecr.eu-west-1.amazonaws.com/orders-api",
			"image_tag_mutability": "IMMUTABLE",
			"scan_on_push":         true,
			"encryption_type":      "AES256",
		},
	}
}

func hasECRRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
