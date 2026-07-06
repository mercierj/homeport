package compute

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestECSMapperProducesAppUnitAndRunbook(t *testing.T) {
	result, err := NewECSMapper().Map(context.Background(), &resource.AWSResource{
		ID:   "svc-1",
		Type: resource.TypeECSService,
		Name: "api",
		Config: map[string]interface{}{
			"name":          "api",
			"desired_count": 2,
			"container_definitions": []interface{}{
				map[string]interface{}{
					"name":  "api",
					"image": "registry.example.com/api:1",
					"environment": []interface{}{
						map[string]interface{}{"name": "APP_ENV", "value": "prod"},
					},
					"portMappings": []interface{}{
						map[string]interface{}{"containerPort": float64(8080), "hostPort": float64(80)},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Map() error = %v", err)
	}
	if len(result.AppUnits) != 1 {
		t.Fatalf("AppUnits len = %d, want 1", len(result.AppUnits))
	}
	unit := result.AppUnits[0]
	if unit.Image != "registry.example.com/api:1" {
		t.Fatalf("AppUnit image = %q", unit.Image)
	}
	if unit.Environment["APP_ENV"] != "prod" {
		t.Fatalf("AppUnit env = %#v", unit.Environment)
	}
	if unit.Replicas != 2 {
		t.Fatalf("AppUnit replicas = %d, want 2", unit.Replicas)
	}
	if !hasComputeRunbookKind(result, "compute-app") {
		t.Fatalf("missing compute-app runbook steps: %#v", result.RunbookSteps)
	}
}

func TestLambdaMapperProducesServerlessRunbook(t *testing.T) {
	result, err := NewLambdaMapper().Map(context.Background(), &resource.AWSResource{
		ID:   "fn-1",
		Type: resource.TypeLambdaFunction,
		Name: "resize",
		Config: map[string]interface{}{
			"function_name": "resize",
			"runtime":       "nodejs20.x",
			"handler":       "index.handler",
		},
	})
	if err != nil {
		t.Fatalf("Map() error = %v", err)
	}
	if len(result.AppUnits) != 1 {
		t.Fatalf("AppUnits len = %d, want 1", len(result.AppUnits))
	}
	if result.AppUnits[0].Runtime != "nodejs20.x" {
		t.Fatalf("AppUnit runtime = %q", result.AppUnits[0].Runtime)
	}
	if !hasComputeRunbookKind(result, "serverless-function") {
		t.Fatalf("missing serverless-function runbook steps: %#v", result.RunbookSteps)
	}
}

func TestEKSMapperProducesKubernetesRunbook(t *testing.T) {
	result, err := NewEKSMapper().Map(context.Background(), &resource.AWSResource{
		ID:   "eks-1",
		Type: resource.TypeEKSCluster,
		Name: "prod",
		Config: map[string]interface{}{
			"name":    "prod",
			"version": "1.29",
		},
	})
	if err != nil {
		t.Fatalf("Map() error = %v", err)
	}
	if !hasComputeRunbookKind(result, "kubernetes") {
		t.Fatalf("missing kubernetes runbook steps: %#v", result.RunbookSteps)
	}
}

func hasComputeRunbookKind(result *mapper.MappingResult, kind string) bool {
	for _, step := range result.RunbookSteps {
		if step.Metadata["kind"] == kind {
			return true
		}
	}
	return false
}
