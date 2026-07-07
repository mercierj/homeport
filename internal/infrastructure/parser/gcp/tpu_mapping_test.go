package gcp

import (
	"testing"

	"github.com/homeport/homeport/internal/domain/resource"
)

func TestTPUTerraformTypesMapToResources(t *testing.T) {
	tests := map[string]resource.Type{
		"google_tpu_node":  resource.TypeTPUNode,
		"google_tpu_v2_vm": resource.TypeTPUV2VM,
	}
	for input, want := range tests {
		if got := mapGCPTerraformType(input); got != want {
			t.Fatalf("%s maps to %s, want %s", input, got, want)
		}
	}
}

func TestTPUDeploymentManagerTypesMapToResources(t *testing.T) {
	tests := map[string]resource.Type{
		"tpu.v1.node": resource.TypeTPUNode,
		"tpu.v2.vm":   resource.TypeTPUV2VM,
	}
	for input, want := range tests {
		if got := mapDMTypeToResourceType(input); got != want {
			t.Fatalf("%s maps to %s, want %s", input, got, want)
		}
	}
}
