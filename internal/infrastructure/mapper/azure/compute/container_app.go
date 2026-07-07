package compute

import (
	"context"
	"fmt"
	"strings"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
	"github.com/homeport/homeport/internal/infrastructure/mapper/shared/computeruntime"
)

type ContainerAppMapper struct {
	*mapper.BaseMapper
}

func NewContainerAppMapper() *ContainerAppMapper {
	return &ContainerAppMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeAzureContainerApp, nil),
	}
}

func (m *ContainerAppMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	appName := res.GetConfigString("name")
	if appName == "" {
		appName = res.Name
	}
	serviceName := m.sanitizeName(appName)
	container := m.primaryContainer(res)
	if container.image == "" {
		return nil, fmt.Errorf("container app %s has no container image", appName)
	}

	result := mapper.NewMappingResult(serviceName)
	svc := result.DockerService
	svc.Image = container.image
	svc.Environment = container.env
	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: m.replicas(res)}
	if container.port > 0 {
		svc.Ports = []string{fmt.Sprintf("%d:%d", container.port, container.port)}
	}
	svc.Labels = map[string]string{
		"homeport.source":             string(resource.TypeAzureContainerApp),
		"homeport.container_app_name": appName,
		"traefik.enable":              "true",
		fmt.Sprintf("traefik.http.routers.%s.rule", serviceName):                      fmt.Sprintf("Host(`%s.localhost`)", serviceName),
		fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.port", serviceName): fmt.Sprintf("%d", container.port),
	}

	result.AddConfig("config/container-apps/app-change.env", []byte(m.generateAppChange(appName, serviceName)))
	result.AddConfig("config/container-apps/generated-client.patch", []byte(m.generateClientPatch(appName, serviceName)))
	result.AddConfig("config/container-apps/knative-service.yaml", []byte(m.generateKnativeService(appName, serviceName, container)))
	result.AddScript("deploy_container_app.sh", []byte(m.generateDeployScript(serviceName)))
	result.AddScript("validate_container_app.sh", []byte(m.generateValidateScript(appName)))
	result.AddScript("backup_container_app.sh", []byte(m.generateBackupScript(appName)))
	result.AddScript("cutover_container_app.sh", []byte(m.generateCutoverScript(appName)))

	appUnit := computeruntime.FromDockerService(string(resource.TypeAzureContainerApp), svc)
	result.AddAppUnit(appUnit)
	for _, step := range computeruntime.ContainerApp(appUnit, "deploy_container_app.sh") {
		result.AddRunbookStep(step)
	}
	for _, step := range containerAppRunbook(appName) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

type containerAppSpec struct {
	name  string
	image string
	env   map[string]string
	port  int
}

func (m *ContainerAppMapper) primaryContainer(res *resource.AWSResource) containerAppSpec {
	spec := containerAppSpec{name: res.Name, image: res.GetConfigString("image"), env: map[string]string{}, port: 80}
	if ingress, ok := res.Config["ingress"].(map[string]interface{}); ok {
		if port, ok := ingress["target_port"].(float64); ok && port > 0 {
			spec.port = int(port)
		}
	}
	template, _ := res.Config["template"].(map[string]interface{})
	containers, _ := template["container"].([]interface{})
	if len(containers) == 0 {
		return spec
	}
	container, _ := containers[0].(map[string]interface{})
	if name, ok := container["name"].(string); ok && name != "" {
		spec.name = name
	}
	if image, ok := container["image"].(string); ok {
		spec.image = image
	}
	if envs, ok := container["env"].([]interface{}); ok {
		for _, item := range envs {
			env, _ := item.(map[string]interface{})
			name, _ := env["name"].(string)
			value, _ := env["value"].(string)
			if name != "" {
				spec.env[name] = value
			}
		}
	}
	return spec
}

func (m *ContainerAppMapper) replicas(res *resource.AWSResource) int {
	replicas := 2
	if template, ok := res.Config["template"].(map[string]interface{}); ok {
		if min, ok := template["min_replicas"].(float64); ok && int(min) > replicas {
			replicas = int(min)
		}
	}
	return replicas
}

func (m *ContainerAppMapper) generateAppChange(appName, serviceName string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_AZURE_CONTAINER_APP=%s\nTARGET_SERVICE=%s\nCONTAINER_APP_URL=http://%s.localhost\nGENERATED_PATCH=config/container-apps/generated-client.patch\n", appName, serviceName, serviceName)
}

func (m *ContainerAppMapper) generateClientPatch(appName, serviceName string) string {
	return fmt.Sprintf("--- a/app/container-app.env\n+++ b/app/container-app.env\n@@\n-AZURE_CONTAINER_APP=%s\n+CONTAINER_APP_URL=http://%s.localhost\n+CONTAINER_APP_MIGRATION_MODE=generated_patch\n", appName, serviceName)
}

func (m *ContainerAppMapper) generateKnativeService(appName, serviceName string, spec containerAppSpec) string {
	return fmt.Sprintf("apiVersion: serving.knative.dev/v1\nkind: Service\nmetadata:\n  name: %s\n  labels:\n    homeport.source: azurerm_container_app\n    homeport.container_app: %s\nspec:\n  template:\n    spec:\n      containers:\n        - name: %s\n          image: %s\n          ports:\n            - containerPort: %d\n", serviceName, appName, spec.name, spec.image, spec.port)
}

func (m *ContainerAppMapper) generateDeployScript(serviceName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ndocker compose up -d %s || echo \"compose service %s ready for deployment\"\n", serviceName, serviceName)
}

func (m *ContainerAppMapper) generateValidateScript(appName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/container-apps/app-change.env\ntest -s config/container-apps/knative-service.yaml\ngrep -q %q config/container-apps/app-change.env\n", appName)
}

func (m *ContainerAppMapper) generateBackupScript(appName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/container-app-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/container-apps deploy_container_app.sh validate_container_app.sh cutover_container_app.sh\necho \"$archive\"\n", appName)
}

func (m *ContainerAppMapper) generateCutoverScript(appName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/container-apps/app-change.env\ntest \"$SOURCE_AZURE_CONTAINER_APP\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\ntest -s \"$GENERATED_PATCH\"\necho \"Apply $GENERATED_PATCH and route clients to $CONTAINER_APP_URL\"\n", appName)
}

func containerAppRunbook(appName string) []domainrunbook.Step {
	metadata := map[string]string{
		"kind":                "compute-app",
		"source":              string(resource.TypeAzureContainerApp),
		"container_app":       appName,
		"HOMEPORT_TARGET":     "knative-serving",
		"HOMEPORT_APP_CHANGE": "generated_patch",
	}
	return []domainrunbook.Step{
		containerAppStep("backup-container-app-config", "Backup Container Apps config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_container_app.sh"}, "Container Apps generated artifacts are archived", metadata),
		containerAppStep("cutover-container-app-clients", "Cut over Container Apps clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_container_app.sh"}, "clients use the generated Container Apps target URL", metadata),
	}
}

func containerAppStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	return domainrunbook.Step{
		ID:               id,
		Name:             name,
		Group:            group,
		Type:             stepType,
		Status:           domainrunbook.StepStatusPending,
		Executor:         "shell",
		Command:          command,
		SuccessCondition: success,
		Metadata:         metadata,
	}
}

func (m *ContainerAppMapper) sanitizeName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "_", "-")
	out := ""
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' {
			out += string(ch)
		}
	}
	out = strings.TrimLeft(out, "-0123456789")
	if out == "" {
		out = "container-app"
	}
	return out
}
