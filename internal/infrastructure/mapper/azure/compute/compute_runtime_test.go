package compute

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestAppServiceMapperProducesAppUnitAndRunbook(t *testing.T) {
	result, err := NewAppServiceMapper().Map(context.Background(), &resource.AWSResource{
		ID:   "app-1",
		Type: resource.TypeAppService,
		Name: "api",
		Config: map[string]interface{}{
			"name": "api",
			"site_config": map[string]interface{}{
				"application_stack": map[string]interface{}{
					"node_version": "20",
				},
			},
			"app_settings": map[string]interface{}{
				"APP_ENV": "prod",
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
	if unit.Runtime != "node" {
		t.Fatalf("AppUnit runtime = %q, want node", unit.Runtime)
	}
	if unit.SourcePath != "./apps/api" {
		t.Fatalf("AppUnit source path = %q", unit.SourcePath)
	}
	if unit.Environment["APP_ENV"] != "prod" {
		t.Fatalf("AppUnit env = %#v", unit.Environment)
	}
	if !hasComputeRunbookKind(result, "compute-app") {
		t.Fatalf("missing compute-app runbook steps: %#v", result.RunbookSteps)
	}
	for _, step := range result.ManualSteps {
		if strings.Contains(strings.ToLower(step), "build app") {
			t.Fatalf("manual build note should be a runbook step, got %q", step)
		}
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
