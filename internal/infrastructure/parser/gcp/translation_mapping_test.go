package gcp

import (
	"testing"

	"github.com/homeport/homeport/internal/domain/resource"
)

func TestTranslationTerraformServiceMapsToResource(t *testing.T) {
	parser := NewTFStateParser()
	res := parser.convertResource(StateResource{Type: "google_project_service", Name: "translate"}, ResourceInstance{
		Attributes: map[string]interface{}{"service": "translate.googleapis.com"},
	})
	if res.Type != resource.TypeTranslationService {
		t.Fatalf("google_project_service with translate service maps to %s, want %s", res.Type, resource.TypeTranslationService)
	}
}

func TestTranslationDeploymentManagerServiceMapsToResource(t *testing.T) {
	parser := NewDeploymentManagerParser()
	res := parser.convertResource(DMResource{
		Name:       "translate",
		Type:       "serviceusage.v1.service",
		Properties: map[string]interface{}{"name": "translate.googleapis.com"},
	}, "deployment.yaml")
	if res.Type != resource.TypeTranslationService {
		t.Fatalf("serviceusage.v1.service with translate name maps to %s, want %s", res.Type, resource.TypeTranslationService)
	}
}
