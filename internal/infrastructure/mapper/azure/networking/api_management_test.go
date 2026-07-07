package networking

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestAPIManagementConformanceManagedAToZ(t *testing.T) {
	result, err := NewAPIManagementMapper().Map(context.Background(), managedAPIManagementFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated API Management migration", result.ManualSteps)
	}
	if result.DockerService.Image != "kong:3.6" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Kong target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/api-management/kong.yaml", "config/api-management/app-change.env", "config/api-management/generated-client.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/api-management/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_API_MANAGEMENT_SERVICE=payments-apim", "TARGET_API_GATEWAY=kong"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_api_management_config.sh", "provision_kong_api_management.sh", "migrate_api_management_apis.sh", "validate_api_management_kong.sh", "backup_api_management_config.sh", "cutover_api_management_routes.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-api-management-config":   domainrunbook.StepTypeCommand,
		"provision-api-management-kong":  domainrunbook.StepTypeCommand,
		"migrate-api-management-apis":    domainrunbook.StepTypeCommand,
		"validate-api-management-kong":   domainrunbook.StepTypeCommand,
		"backup-api-management-config":   domainrunbook.StepTypeCommand,
		"cutover-api-management-routes":  domainrunbook.StepTypeAPICall,
		"rollback-api-management-source": domainrunbook.StepTypeRollback,
	} {
		if !hasAPIManagementRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewAPIManagementMapper(t *testing.T) {
	if NewAPIManagementMapper().ResourceType() != resource.TypeAPIManagement {
		t.Fatalf("API Management mapper type = %s, want %s", NewAPIManagementMapper().ResourceType(), resource.TypeAPIManagement)
	}
}

func managedAPIManagementFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "/subscriptions/demo/resourceGroups/rg/providers/Microsoft.ApiManagement/service/payments-apim",
		Type: resource.TypeAPIManagement,
		Name: "payments-apim",
		Config: map[string]interface{}{
			"name":     "payments-apim",
			"location": "westeurope",
		},
	}
}

func hasAPIManagementRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
