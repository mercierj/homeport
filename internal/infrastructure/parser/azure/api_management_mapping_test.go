package azure

import (
	"testing"

	"github.com/homeport/homeport/internal/domain/resource"
)

func TestAPIManagementTerraformTypeMapsToResource(t *testing.T) {
	if got := mapAzureTerraformType("azurerm_api_management"); got != resource.TypeAPIManagement {
		t.Fatalf("azurerm_api_management maps to %s, want %s", got, resource.TypeAPIManagement)
	}
}
