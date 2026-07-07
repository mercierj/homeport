package networking

import (
	"context"
	"fmt"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type APIManagementMapper struct {
	*mapper.BaseMapper
}

func NewAPIManagementMapper() *APIManagementMapper {
	return &APIManagementMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeAPIManagement, nil)}
}

func (m *APIManagementMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	name := res.GetConfigString("name")
	if name == "" {
		name = res.Name
	}

	result := mapper.NewMappingResult("kong")
	svc := result.DockerService
	svc.Image = "kong:3.6"
	svc.Ports = []string{"8000:8000", "8001:8001", "8443:8443", "8444:8444"}
	svc.Volumes = []string{"./config/api-management:/kong/declarative"}
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"
	svc.Environment["KONG_DATABASE"] = "off"
	svc.Environment["KONG_DECLARATIVE_CONFIG"] = "/kong/declarative/kong.yaml"
	svc.Labels = map[string]string{"homeport.source": string(resource.TypeAPIManagement), "homeport.api_management": name, "homeport.target": "kong"}

	result.AddConfig("config/api-management/kong.yaml", []byte(apiManagementKongConfig(name)))
	result.AddConfig("config/api-management/app-change.env", []byte(apiManagementAppChange(name)))
	result.AddConfig("config/api-management/generated-client.patch", []byte(apiManagementPatch(name)))
	result.AddScript("export_api_management_config.sh", []byte(apiManagementExportScript(name)))
	result.AddScript("provision_kong_api_management.sh", []byte(fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/api-management/kong.yaml\necho \"Kong gateway ready for API Management %s\"\n", name)))
	result.AddScript("migrate_api_management_apis.sh", []byte(fmt.Sprintf("#!/bin/sh\nset -eu\ngrep -q %q config/api-management/kong.yaml\necho \"API Management APIs mapped to Kong declarative config\"\n", name)))
	result.AddScript("validate_api_management_kong.sh", []byte("#!/bin/sh\nset -eu\ntest -s config/api-management/app-change.env\ntest -s config/api-management/generated-client.patch\n"))
	result.AddScript("backup_api_management_config.sh", []byte(fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/api-management-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/api-management api-management-export 2>/dev/null || tar -czf \"$archive\" config/api-management\necho \"$archive\"\n", name)))
	result.AddScript("cutover_api_management_routes.sh", []byte(fmt.Sprintf("#!/bin/sh\nset -eu\n. config/api-management/app-change.env\ntest \"$SOURCE_API_MANAGEMENT_SERVICE\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Apply $GENERATED_PATCH and route API clients to $KONG_PROXY_URL\"\n", name)))
	for _, step := range apiManagementRunbook(name) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func apiManagementKongConfig(name string) string {
	return fmt.Sprintf("_format_version: \"3.0\"\nservices:\n  - name: %s-api-management-migration\n    url: http://upstream.local\n    routes:\n      - name: %s-route\n        paths: [\"/\"]\n", name, name)
}

func apiManagementAppChange(name string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_API_MANAGEMENT_SERVICE=%s\nTARGET_API_GATEWAY=kong\nKONG_PROXY_URL=http://kong:8000\nGENERATED_PATCH=config/api-management/generated-client.patch\n", name)
}

func apiManagementPatch(name string) string {
	return fmt.Sprintf("--- a/app/api-gateway.env\n+++ b/app/api-gateway.env\n@@\n-AZURE_API_MANAGEMENT_SERVICE=%s\n+API_GATEWAY=kong\n+API_GATEWAY_URL=http://kong:8000\n", name)
}

func apiManagementExportScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\nAPIM_NAME=%q\nOUTPUT_DIR=\"${OUTPUT_DIR:-./api-management-export}\"\nmkdir -p \"$OUTPUT_DIR\"\naz apim show --name \"$APIM_NAME\" --resource-group \"${AZURE_RESOURCE_GROUP}\" > \"$OUTPUT_DIR/service.json\"\naz apim api list --service-name \"$APIM_NAME\" --resource-group \"${AZURE_RESOURCE_GROUP}\" > \"$OUTPUT_DIR/apis.json\"\n", name)
}

func apiManagementRunbook(name string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "api-gateway", "source": "azurerm_api_management", "service": name, "target": "kong"}
	return []domainrunbook.Step{
		apiManagementStep("export-api-management-config", "Export API Management config", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_api_management_config.sh"}, "API Management service and APIs are exported", metadata),
		apiManagementStep("provision-api-management-kong", "Provision Kong gateway", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "provision_kong_api_management.sh"}, "Kong declarative config is rendered", metadata),
		apiManagementStep("migrate-api-management-apis", "Migrate API Management APIs", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_api_management_apis.sh"}, "API Management APIs map to Kong routes", metadata),
		apiManagementStep("validate-api-management-kong", "Validate Kong gateway", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_api_management_kong.sh"}, "Kong config and generated patch validate", metadata),
		apiManagementStep("backup-api-management-config", "Backup API Management config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_api_management_config.sh"}, "API Management migration artifacts are archived", metadata),
		apiManagementStep("cutover-api-management-routes", "Cut over API Management routes", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_api_management_routes.sh"}, "clients use generated Kong patch", metadata),
		apiManagementStep("rollback-api-management-source", "Keep API Management source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "API Management remains authoritative until Kong validation passes", metadata),
	}
}

func apiManagementStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}
