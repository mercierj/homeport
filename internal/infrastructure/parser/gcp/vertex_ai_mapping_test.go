package gcp

import (
	"testing"

	"github.com/homeport/homeport/internal/domain/resource"
)

func TestVertexAITerraformTypeMapsToResource(t *testing.T) {
	if got := mapGCPTerraformType("google_vertex_ai_endpoint"); got != resource.TypeVertexAIEndpoint {
		t.Fatalf("google_vertex_ai_endpoint maps to %s, want %s", got, resource.TypeVertexAIEndpoint)
	}
}

func TestVertexAIDeploymentManagerTypeMapsToResource(t *testing.T) {
	if got := mapDMTypeToResourceType("aiplatform.v1.endpoint"); got != resource.TypeVertexAIEndpoint {
		t.Fatalf("aiplatform.v1.endpoint maps to %s, want %s", got, resource.TypeVertexAIEndpoint)
	}
}
