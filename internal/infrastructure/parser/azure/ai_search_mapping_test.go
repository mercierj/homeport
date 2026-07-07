package azure

import (
	"testing"

	"github.com/homeport/homeport/internal/domain/resource"
)

func TestAISearchTerraformTypeMapsToResource(t *testing.T) {
	if got := mapAzureTerraformType("azurerm_search_service"); got != resource.TypeAzureAISearch {
		t.Fatalf("azurerm_search_service maps to %s, want %s", got, resource.TypeAzureAISearch)
	}
}
