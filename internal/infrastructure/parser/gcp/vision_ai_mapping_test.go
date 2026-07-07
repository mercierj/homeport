package gcp

import (
	"testing"

	"github.com/homeport/homeport/internal/domain/resource"
)

func TestVisionAITerraformServiceMapsToResource(t *testing.T) {
	parser := NewTFStateParser()
	res := parser.convertResource(StateResource{Type: "google_project_service", Name: "vision"}, ResourceInstance{
		Attributes: map[string]interface{}{"service": "vision.googleapis.com"},
	})
	if res.Type != resource.TypeVisionAIService {
		t.Fatalf("google_project_service with vision service maps to %s, want %s", res.Type, resource.TypeVisionAIService)
	}
}

func TestVisionAIDeploymentManagerServiceMapsToResource(t *testing.T) {
	parser := NewDeploymentManagerParser()
	res := parser.convertResource(DMResource{
		Name:       "vision",
		Type:       "serviceusage.v1.service",
		Properties: map[string]interface{}{"name": "vision.googleapis.com"},
	}, "deployment.yaml")
	if res.Type != resource.TypeVisionAIService {
		t.Fatalf("serviceusage.v1.service with vision name maps to %s, want %s", res.Type, resource.TypeVisionAIService)
	}
}
