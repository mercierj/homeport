package gcp

import (
	"testing"

	"github.com/homeport/homeport/internal/domain/resource"
)

func TestErrorReportingTerraformTypeMapsToResource(t *testing.T) {
	parser := NewTFStateParser()
	res := parser.convertResource(StateResource{Type: "google_project_service", Name: "error_reporting"}, ResourceInstance{
		Attributes: map[string]interface{}{"service": "clouderrorreporting.googleapis.com"},
	})
	if res.Type != resource.TypeErrorReportingService {
		t.Fatalf("google_project_service with clouderrorreporting service maps to %s, want %s", res.Type, resource.TypeErrorReportingService)
	}
}

func TestErrorReportingDeploymentManagerTypeMapsToResource(t *testing.T) {
	parser := NewDeploymentManagerParser()
	res := parser.convertResource(DMResource{
		Name:       "error-reporting",
		Type:       "serviceusage.v1.service",
		Properties: map[string]interface{}{"name": "clouderrorreporting.googleapis.com"},
	}, "deployment.yaml")
	if res.Type != resource.TypeErrorReportingService {
		t.Fatalf("serviceusage.v1.service with clouderrorreporting name maps to %s, want %s", res.Type, resource.TypeErrorReportingService)
	}
}
