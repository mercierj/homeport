package gcp

import (
	"testing"

	"github.com/homeport/homeport/internal/domain/resource"
)

func TestDataplexTerraformTypesMapToResources(t *testing.T) {
	tests := map[string]resource.Type{
		"google_dataplex_lake": resource.TypeDataplexLake,
		"google_dataplex_zone": resource.TypeDataplexZone,
	}
	for input, want := range tests {
		if got := mapGCPTerraformType(input); got != want {
			t.Fatalf("%s maps to %s, want %s", input, got, want)
		}
	}
}

func TestDataplexDeploymentManagerTypesMapToResources(t *testing.T) {
	tests := map[string]resource.Type{
		"dataplex.v1.lake": resource.TypeDataplexLake,
		"dataplex.v1.zone": resource.TypeDataplexZone,
	}
	for input, want := range tests {
		if got := mapDMTypeToResourceType(input); got != want {
			t.Fatalf("%s maps to %s, want %s", input, got, want)
		}
	}
}
