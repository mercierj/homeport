package gcp

import (
	"testing"

	"github.com/homeport/homeport/internal/domain/resource"
)

func TestDocumentAITerraformTypeMapsToResource(t *testing.T) {
	if got := mapGCPTerraformType("google_document_ai_processor"); got != resource.TypeDocumentAIProcessor {
		t.Fatalf("google_document_ai_processor maps to %s, want %s", got, resource.TypeDocumentAIProcessor)
	}
}

func TestDocumentAIDeploymentManagerTypeMapsToResource(t *testing.T) {
	if got := mapDMTypeToResourceType("documentai.v1.processor"); got != resource.TypeDocumentAIProcessor {
		t.Fatalf("documentai.v1.processor maps to %s, want %s", got, resource.TypeDocumentAIProcessor)
	}
}
