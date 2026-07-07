// Package compute provides mappers for Azure compute services.
package compute

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
	"github.com/homeport/homeport/internal/infrastructure/mapper/shared/computeruntime"
)

// FunctionMapper converts Azure Functions to Docker/OpenFaaS.
type FunctionMapper struct {
	*mapper.BaseMapper
}

// NewFunctionMapper creates a new Azure Functions to Docker mapper.
func NewFunctionMapper() *FunctionMapper {
	return &FunctionMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeAzureFunction, nil),
	}
}

// Map converts an Azure Function App to a Docker service.
func (m *FunctionMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	functionName := res.GetConfigString("name")
	if functionName == "" {
		functionName = res.Name
	}

	result := mapper.NewMappingResult(m.sanitizeName(functionName))
	svc := result.DockerService

	// Get runtime info
	runtime := m.getRuntime(res)
	runtimeVersion := m.getRuntimeVersion(res)

	// Set Docker image
	svc.Image = m.getDockerImage(runtime, runtimeVersion)

	// Configure environment
	svc.Environment = map[string]string{
		"FUNCTIONS_WORKER_RUNTIME": runtime,
		"AzureWebJobsStorage":      "UseDevelopmentStorage=true",
		"WEBSITE_HOSTNAME":         "localhost",
		"PORT":                     "80",
	}

	// Add app settings
	if appSettings := res.Config["app_settings"]; appSettings != nil {
		if settingsMap, ok := appSettings.(map[string]interface{}); ok {
			for k, v := range settingsMap {
				if strVal, ok := v.(string); ok {
					svc.Environment[k] = strVal
				}
			}
		}
	}

	svc.Ports = []string{"80:80"}
	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}

	svc.Labels = map[string]string{
		"homeport.source":        "azurerm_function_app",
		"homeport.function_name": functionName,
		"homeport.runtime":       runtime,
		"traefik.enable":         "true",
		"traefik.http.routers." + m.sanitizeName(functionName) + ".rule": fmt.Sprintf("Host(`%s.localhost`)", m.sanitizeName(functionName)),
	}

	// Health check
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD-SHELL", "curl -f http://localhost/api/health || exit 1"},
		Interval: 30 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  3,
	}

	// Handle connection strings
	if connStrings := res.Config["connection_string"]; connStrings != nil {
		result.AddWarning("Connection strings detected. Configure database connections manually.")
	}

	// Handle storage account
	if storageAccount := res.GetConfigString("storage_account_name"); storageAccount != "" {
		result.AddWarning("Storage account configured. Use Azurite or MinIO for local development.")
	}

	// Generate Dockerfile
	dockerfile := m.generateDockerfile(runtime, runtimeVersion, functionName)
	result.AddConfig(fmt.Sprintf("functions/%s/Dockerfile", functionName), []byte(dockerfile))
	svc.Build = &mapper.DockerBuild{Context: fmt.Sprintf("./functions/%s", functionName), Dockerfile: "Dockerfile"}

	// Generate sample function
	sampleFunc := m.generateSampleFunction(runtime, functionName)
	result.AddConfig(fmt.Sprintf("functions/%s/%s", functionName, m.getFunctionFile(runtime)), []byte(sampleFunc))

	// Generate host.json
	hostJson := m.generateHostJson()
	result.AddConfig(fmt.Sprintf("functions/%s/host.json", functionName), []byte(hostJson))
	result.AddConfig("config/functions/app-change.env", []byte(m.generateAppChange(functionName)))
	result.AddConfig("config/functions/generated-client.patch", []byte(m.generateClientPatch(functionName)))
	result.AddScript("deploy_function.sh", []byte(m.generateDeployScript(functionName)))
	result.AddScript("validate_function.sh", []byte(m.generateValidateScript(functionName)))
	result.AddScript("backup_function_config.sh", []byte(m.generateBackupScript(functionName)))
	result.AddScript("cutover_function_clients.sh", []byte(m.generateCutoverScript(functionName)))

	appUnit := computeruntime.FromDockerService("azurerm_function_app", svc)
	result.AddAppUnit(appUnit)
	for _, step := range computeruntime.ServerlessFunction(appUnit, "deploy_function.sh") {
		result.AddRunbookStep(step)
	}
	for _, step := range m.runbook(functionName) {
		result.AddRunbookStep(step)
	}

	return result, nil
}

func (m *FunctionMapper) getRuntime(res *resource.AWSResource) string {
	if siteConfig := res.Config["site_config"]; siteConfig != nil {
		if configMap, ok := siteConfig.(map[string]interface{}); ok {
			if appStack, ok := configMap["application_stack"].(map[string]interface{}); ok {
				if _, ok := appStack["node_version"]; ok {
					return "node"
				}
				if _, ok := appStack["python_version"]; ok {
					return "python"
				}
				if _, ok := appStack["dotnet_version"]; ok {
					return "dotnet"
				}
				if _, ok := appStack["java_version"]; ok {
					return "java"
				}
			}
		}
	}
	return "node"
}

func (m *FunctionMapper) getRuntimeVersion(res *resource.AWSResource) string {
	if siteConfig := res.Config["site_config"]; siteConfig != nil {
		if configMap, ok := siteConfig.(map[string]interface{}); ok {
			if appStack, ok := configMap["application_stack"].(map[string]interface{}); ok {
				if v, ok := appStack["node_version"].(string); ok {
					return v
				}
				if v, ok := appStack["python_version"].(string); ok {
					return v
				}
				if v, ok := appStack["dotnet_version"].(string); ok {
					return v
				}
			}
		}
	}
	return "18"
}

func (m *FunctionMapper) getDockerImage(runtime, version string) string {
	switch runtime {
	case "node":
		return fmt.Sprintf("mcr.microsoft.com/azure-functions/node:%s", version)
	case "python":
		return fmt.Sprintf("mcr.microsoft.com/azure-functions/python:%s", version)
	case "dotnet":
		return "mcr.microsoft.com/azure-functions/dotnet:4"
	case "java":
		return "mcr.microsoft.com/azure-functions/java:4"
	default:
		return "mcr.microsoft.com/azure-functions/node:18"
	}
}

func (m *FunctionMapper) generateDockerfile(runtime, version, functionName string) string {
	baseImage := m.getDockerImage(runtime, version)

	return fmt.Sprintf(`FROM %s

# Azure Function: %s

ENV AzureWebJobsScriptRoot=/home/site/wwwroot
ENV AzureFunctionsJobHost__Logging__Console__IsEnabled=true

COPY . /home/site/wwwroot

EXPOSE 80

CMD ["--urls", "http://0.0.0.0:80"]
`, baseImage, functionName)
}

func (m *FunctionMapper) generateSampleFunction(runtime, functionName string) string {
	switch runtime {
	case "node":
		return `module.exports = async function (context, req) {
    context.log('HTTP trigger function processed a request.');

    const name = (req.query.name || (req.body && req.body.name));
    const responseMessage = name
        ? "Hello, " + name + "!"
        : "Hello from Azure Functions!";

    context.res = {
        body: responseMessage
    };
}
`
	case "python":
		return `import azure.functions as func
import logging

def main(req: func.HttpRequest) -> func.HttpResponse:
    logging.info('Python HTTP trigger function processed a request.')

    name = req.params.get('name')
    if not name:
        try:
            req_body = req.get_json()
        except ValueError:
            pass
        else:
            name = req_body.get('name')

    if name:
        return func.HttpResponse(f"Hello, {name}!")
    else:
        return func.HttpResponse("Hello from Azure Functions!")
`
	default:
		return "// TODO: Implement function"
	}
}

func (m *FunctionMapper) getFunctionFile(runtime string) string {
	switch runtime {
	case "node":
		return "index.js"
	case "python":
		return "__init__.py"
	case "dotnet":
		return "Function.cs"
	default:
		return "index.js"
	}
}

func (m *FunctionMapper) generateHostJson() string {
	return `{
  "version": "2.0",
  "logging": {
    "applicationInsights": {
      "samplingSettings": {
        "isEnabled": true,
        "excludedTypes": "Request"
      }
    }
  },
  "extensionBundle": {
    "id": "Microsoft.Azure.Functions.ExtensionBundle",
    "version": "[3.*, 4.0.0)"
  }
}
`
}

func (m *FunctionMapper) generateAppChange(functionName string) string {
	host := m.sanitizeName(functionName) + ".localhost"
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_FUNCTION_APP=%s\nFUNCTION_URL=http://%s/api\nFUNCTION_HOST=%s\nGENERATED_PATCH=config/functions/generated-client.patch\n", functionName, host, host)
}

func (m *FunctionMapper) generateClientPatch(functionName string) string {
	host := m.sanitizeName(functionName) + ".localhost"
	return fmt.Sprintf("--- a/app/functions.env\n+++ b/app/functions.env\n@@\n-AZURE_FUNCTION_APP=%s\n+FUNCTION_BASE_URL=http://%s/api\n+FUNCTION_PLATFORM=homeport-compose\n", functionName, host)
}

func (m *FunctionMapper) generateDeployScript(functionName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ndocker build -t %s ./functions/%s\ndocker compose up -d %s\n", m.sanitizeName(functionName), functionName, m.sanitizeName(functionName))
}

func (m *FunctionMapper) generateValidateScript(functionName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s functions/%s/Dockerfile\ntest -s functions/%s/host.json\ntest -s config/functions/app-change.env\ngrep -q %q config/functions/app-change.env\n", functionName, functionName, functionName)
}

func (m *FunctionMapper) generateBackupScript(functionName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/function-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" functions/%s config/functions deploy_function.sh validate_function.sh\necho \"$archive\"\n", m.sanitizeName(functionName), functionName)
}

func (m *FunctionMapper) generateCutoverScript(functionName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/functions/app-change.env\ntest \"$SOURCE_FUNCTION_APP\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Apply $GENERATED_PATCH and call functions through $FUNCTION_URL\"\n", functionName)
}

func (m *FunctionMapper) runbook(functionName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "serverless-function", "source": "azurerm_function_app", "function": functionName, "target": "compose-openfaas"}
	return []domainrunbook.Step{
		m.step("backup-function-config", "Backup function config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_function_config.sh"}, "function migration artifacts are archived", metadata),
		m.step("cutover-function-clients", "Cut over function clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_function_clients.sh"}, "clients use generated function endpoint", metadata),
	}
}

func (m *FunctionMapper) step(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: "shell", Command: command, SuccessCondition: success, Metadata: metadata}
}

func (m *FunctionMapper) sanitizeName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "_", "-")
	validName := ""
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' {
			validName += string(ch)
		}
	}
	validName = strings.TrimLeft(validName, "-0123456789")
	if validName == "" {
		validName = "function"
	}
	return validName
}
