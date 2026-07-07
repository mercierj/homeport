package gcp

import (
	"testing"

	"github.com/homeport/homeport/internal/domain/resource"
)

func TestCloudDeployTerraformTypesMapToResources(t *testing.T) {
	tests := map[string]resource.Type{
		"google_clouddeploy_delivery_pipeline": resource.TypeCloudDeployDeliveryPipeline,
		"google_clouddeploy_target":            resource.TypeCloudDeployTarget,
	}
	for input, want := range tests {
		if got := mapGCPTerraformType(input); got != want {
			t.Fatalf("%s maps to %s, want %s", input, got, want)
		}
	}
}

func TestCloudDeployDeploymentManagerTypesMapToResources(t *testing.T) {
	tests := map[string]resource.Type{
		"clouddeploy.v1.deliveryPipeline": resource.TypeCloudDeployDeliveryPipeline,
		"clouddeploy.v1.target":           resource.TypeCloudDeployTarget,
	}
	for input, want := range tests {
		if got := mapDMTypeToResourceType(input); got != want {
			t.Fatalf("%s maps to %s, want %s", input, got, want)
		}
	}
}
