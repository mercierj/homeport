package azure

import (
	"testing"

	"github.com/homeport/homeport/internal/domain/resource"
)

func TestAppInsightsTerraformTypeMapsToResource(t *testing.T) {
	if got := mapAzureTerraformType("azurerm_application_insights"); got != resource.TypeAppInsights {
		t.Fatalf("azurerm_application_insights maps to %s, want %s", got, resource.TypeAppInsights)
	}
}
