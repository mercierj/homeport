package compute

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestCloudRunMapperProducesAppUnitAndRunbook(t *testing.T) {
	result, err := NewCloudRunMapper().Map(context.Background(), &resource.AWSResource{
		ID:   "run-1",
		Type: resource.TypeCloudRun,
		Name: "api",
		Config: map[string]interface{}{
			"name": "api",
			"template": map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": []interface{}{
						map[string]interface{}{
							"image": "gcr.io/project/api:1",
							"env": []interface{}{
								map[string]interface{}{"name": "APP_ENV", "value": "prod"},
							},
							"ports": []interface{}{
								map[string]interface{}{"container_port": float64(9090)},
							},
						},
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
	if unit.Image != "gcr.io/project/api:1" {
		t.Fatalf("AppUnit image = %q", unit.Image)
	}
	if unit.Environment["PORT"] != "9090" || unit.Environment["APP_ENV"] != "prod" {
		t.Fatalf("AppUnit env = %#v", unit.Environment)
	}
	if !hasComputeRunbookKind(result, "compute-app") {
		t.Fatalf("missing compute-app runbook steps: %#v", result.RunbookSteps)
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
