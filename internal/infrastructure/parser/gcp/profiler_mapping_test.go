package gcp

import (
	"testing"

	"github.com/homeport/homeport/internal/domain/resource"
)

func TestProfilerTerraformTypeMapsToResource(t *testing.T) {
	parser := NewTFStateParser()
	res := parser.convertResource(StateResource{Type: "google_project_service", Name: "profiler"}, ResourceInstance{
		Attributes: map[string]interface{}{"service": "cloudprofiler.googleapis.com"},
	})
	if res.Type != resource.TypeProfilerService {
		t.Fatalf("google_project_service with cloudprofiler service maps to %s, want %s", res.Type, resource.TypeProfilerService)
	}
}

func TestProfilerDeploymentManagerTypeMapsToResource(t *testing.T) {
	parser := NewDeploymentManagerParser()
	res := parser.convertResource(DMResource{
		Name:       "profiler",
		Type:       "serviceusage.v1.service",
		Properties: map[string]interface{}{"name": "cloudprofiler.googleapis.com"},
	}, "deployment.yaml")
	if res.Type != resource.TypeProfilerService {
		t.Fatalf("serviceusage.v1.service with cloudprofiler name maps to %s, want %s", res.Type, resource.TypeProfilerService)
	}
}
