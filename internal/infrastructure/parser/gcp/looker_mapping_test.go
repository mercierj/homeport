package gcp

import (
	"testing"

	"github.com/homeport/homeport/internal/domain/resource"
)

func TestLookerTerraformTypeMapsToResource(t *testing.T) {
	if got := mapGCPTerraformType("google_looker_instance"); got != resource.TypeLookerInstance {
		t.Fatalf("google_looker_instance maps to %s, want %s", got, resource.TypeLookerInstance)
	}
}

func TestLookerDeploymentManagerTypeMapsToResource(t *testing.T) {
	if got := mapDMTypeToResourceType("looker.v1.instance"); got != resource.TypeLookerInstance {
		t.Fatalf("looker.v1.instance maps to %s, want %s", got, resource.TypeLookerInstance)
	}
}
