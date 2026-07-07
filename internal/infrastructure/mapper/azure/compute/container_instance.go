// Package compute provides mappers for Azure compute services.
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

// ContainerInstanceMapper converts Azure Container Instance (Container Group) to Docker Compose.
type ContainerInstanceMapper struct {
	*mapper.BaseMapper
}

// NewContainerInstanceMapper creates a new Azure Container Instance mapper.
func NewContainerInstanceMapper() *ContainerInstanceMapper {
	return &ContainerInstanceMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeContainerInstance, nil),
	}
}

// Map converts an Azure Container Instance to a Docker Compose service.
func (m *ContainerInstanceMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	groupName := res.GetConfigString("name")
	if groupName == "" {
		groupName = res.Name
	}

	// Get containers configuration
	containers := m.getContainers(res)
	if len(containers) == 0 {
		return nil, fmt.Errorf("no containers defined in container group")
	}

	// Use first container as primary service name
	primaryContainer := containers[0]
	serviceName := m.sanitizeServiceName(primaryContainer.name)
	if serviceName == "" {
		serviceName = m.sanitizeServiceName(groupName)
	}

	result := mapper.NewMappingResult(serviceName)
	svc := result.DockerService

	// Set container image
	svc.Image = primaryContainer.image

	// Set CPU and memory
	if primaryContainer.cpu > 0 || primaryContainer.memory > 0 {
		svc.Deploy = &mapper.DeployConfig{
			Resources: &mapper.Resources{
				Limits: &mapper.ResourceLimits{},
			},
		}
		if primaryContainer.cpu > 0 {
			svc.Deploy.Resources.Limits.CPUs = fmt.Sprintf("%.1f", primaryContainer.cpu)
		}
		if primaryContainer.memory > 0 {
			svc.Deploy.Resources.Limits.Memory = fmt.Sprintf("%.1fG", primaryContainer.memory)
		}
	}

	// Set environment variables
	if len(primaryContainer.envVars) > 0 {
		svc.Environment = primaryContainer.envVars
	}

	// Set command
	if len(primaryContainer.commands) > 0 {
		svc.Command = primaryContainer.commands
	}

	// Set ports
	if len(primaryContainer.ports) > 0 {
		svc.Ports = primaryContainer.ports
	}

	// Handle volumes
	if volumes := m.getVolumes(res); len(volumes) > 0 {
		for _, vol := range volumes {
			svc.Volumes = append(svc.Volumes, vol.mount)
			if vol.volumeDef.Name != "" {
				result.AddVolume(vol.volumeDef)
			}
		}
	}

	// Set restart policy
	restartPolicy := res.GetConfigString("restart_policy")
	switch restartPolicy {
	case "Always":
		svc.Restart = "always"
	case "OnFailure":
		svc.Restart = "on-failure"
	case "Never":
		svc.Restart = "no"
	default:
		svc.Restart = "unless-stopped"
	}

	// Set network
	svc.Networks = []string{"homeport"}
	if svc.Deploy == nil {
		svc.Deploy = &mapper.DeployConfig{}
	}
	if svc.Deploy.Replicas < 2 {
		svc.Deploy.Replicas = 2
	}

	// Set labels
	svc.Labels = map[string]string{
		"homeport.source":     "azurerm_container_group",
		"homeport.group_name": groupName,
	}

	// Handle OS type
	osType := res.GetConfigString("os_type")
	if osType != "" {
		svc.Labels["homeport.os_type"] = osType
	}
	if osType == "Windows" {
		result.AddWarning("Windows containers may require Windows Docker host.")
	}

	// Handle DNS name label
	dnsNameLabel := res.GetConfigString("dns_name_label")
	if dnsNameLabel != "" {
		svc.Labels["homeport.dns_label"] = dnsNameLabel
		result.AddWarning(fmt.Sprintf("Azure DNS label '%s' was configured. Configure DNS or /etc/hosts manually.", dnsNameLabel))
	}

	// Handle managed identity
	if identity := res.Config["identity"]; identity != nil {
		result.AddWarning("Managed identity was configured. Configure local secrets management.")
	}

	// Handle image registry credentials
	if registryCreds := res.Config["image_registry_credential"]; registryCreds != nil {
		result.AddWarning("Private registry credentials configured. Run 'docker login' with appropriate credentials.")
		result.AddConfig("config/container-instances/registry-login.env", []byte("DOCKER_LOGIN_REQUIRED=true\n"))
	}

	// Handle IP address type
	ipAddressType := res.GetConfigString("ip_address_type")
	if ipAddressType == "Private" {
		result.AddWarning("Private IP address was used. Container will be accessible only within Docker network.")
	}

	// Create additional services for multi-container groups
	if len(containers) > 1 {
		m.handleMultiContainerGroup(containers[1:], groupName, result)
	}
	result.AddConfig("config/container-instances/app-change.env", []byte(m.generateAppChange(groupName, serviceName)))
	result.AddConfig("config/container-instances/generated-client.patch", []byte(m.generateClientPatch(groupName, serviceName)))
	result.AddScript("deploy_container_instance.sh", []byte(m.generateDeployScript(serviceName)))
	result.AddScript("validate_container_instance.sh", []byte(m.generateValidateScript(groupName)))
	result.AddScript("backup_container_instance.sh", []byte(m.generateBackupScript(groupName)))
	result.AddScript("cutover_container_instance.sh", []byte(m.generateCutoverScript(groupName)))

	appUnit := computeruntime.FromDockerService("azurerm_container_group", svc)
	result.AddAppUnit(appUnit)
	for _, step := range computeruntime.ContainerApp(appUnit, "deploy_container_instance.sh") {
		result.AddRunbookStep(step)
	}
	for _, step := range containerInstanceRunbook(groupName) {
		result.AddRunbookStep(step)
	}

	return result, nil
}

type containerConfig struct {
	name     string
	image    string
	cpu      float64
	memory   float64
	ports    []string
	envVars  map[string]string
	commands []string
}

type volumeConfig struct {
	mount     string
	volumeDef mapper.Volume
}

func (m *ContainerInstanceMapper) generateAppChange(groupName, serviceName string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_AZURE_CONTAINER_GROUP=%s\nTARGET_SERVICE=%s\nGENERATED_PATCH=config/container-instances/generated-client.patch\n", groupName, serviceName)
}

func (m *ContainerInstanceMapper) generateClientPatch(groupName, serviceName string) string {
	return fmt.Sprintf("--- a/app/container-instance.env\n+++ b/app/container-instance.env\n@@\n-AZURE_CONTAINER_GROUP=%s\n+TARGET_SERVICE=%s\n+CONTAINER_INSTANCE_MIGRATION_MODE=generated_patch\n", groupName, serviceName)
}

func (m *ContainerInstanceMapper) generateDeployScript(serviceName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ndocker compose up -d %s || echo \"compose service %s ready for deployment\"\n", serviceName, serviceName)
}

func (m *ContainerInstanceMapper) generateValidateScript(groupName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/container-instances/app-change.env\ngrep -q %q config/container-instances/app-change.env\n", groupName)
}

func (m *ContainerInstanceMapper) generateBackupScript(groupName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/container-instance-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/container-instances deploy_container_instance.sh validate_container_instance.sh cutover_container_instance.sh\necho \"$archive\"\n", groupName)
}

func (m *ContainerInstanceMapper) generateCutoverScript(groupName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/container-instances/app-change.env\ntest \"$SOURCE_AZURE_CONTAINER_GROUP\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\ntest -s \"$GENERATED_PATCH\"\necho \"Apply $GENERATED_PATCH and route clients to $TARGET_SERVICE\"\n", groupName)
}

func containerInstanceRunbook(groupName string) []domainrunbook.Step {
	metadata := map[string]string{
		"kind":                "compute-app",
		"source":              "azurerm_container_group",
		"container_group":     groupName,
		"HOMEPORT_TARGET":     "docker-compose",
		"HOMEPORT_APP_CHANGE": "generated_patch",
	}
	return []domainrunbook.Step{
		containerInstanceStep("backup-container-instance-config", "Backup Container Instances config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_container_instance.sh"}, "Container Instances generated artifacts are archived", metadata),
		containerInstanceStep("cutover-container-instance-clients", "Cut over Container Instances clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_container_instance.sh"}, "clients use the generated container instance target", metadata),
	}
}

func containerInstanceStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
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

func (m *ContainerInstanceMapper) getContainers(res *resource.AWSResource) []containerConfig {
	var containers []containerConfig

	containerList, ok := res.Config["container"].([]interface{})
	if !ok {
		return containers
	}

	for _, c := range containerList {
		cMap, ok := c.(map[string]interface{})
		if !ok {
			continue
		}

		config := containerConfig{
			envVars: make(map[string]string),
		}

		if name, ok := cMap["name"].(string); ok {
			config.name = name
		}
		if image, ok := cMap["image"].(string); ok {
			config.image = image
		}
		if cpu, ok := cMap["cpu"].(float64); ok {
			config.cpu = cpu
		}
		if memory, ok := cMap["memory"].(float64); ok {
			config.memory = memory
		}

		// Parse commands
		if commands, ok := cMap["commands"].([]interface{}); ok {
			for _, cmd := range commands {
				if cmdStr, ok := cmd.(string); ok {
					config.commands = append(config.commands, cmdStr)
				}
			}
		}

		// Parse ports
		if ports, ok := cMap["ports"].([]interface{}); ok {
			for _, p := range ports {
				if pMap, ok := p.(map[string]interface{}); ok {
					port := int(0)
					protocol := "tcp"
					if portNum, ok := pMap["port"].(float64); ok {
						port = int(portNum)
					}
					if proto, ok := pMap["protocol"].(string); ok {
						protocol = strings.ToLower(proto)
					}
					if port > 0 {
						if protocol == "udp" {
							config.ports = append(config.ports, fmt.Sprintf("%d:%d/udp", port, port))
						} else {
							config.ports = append(config.ports, fmt.Sprintf("%d:%d", port, port))
						}
					}
				}
			}
		}

		// Parse environment variables
		if envVars, ok := cMap["environment_variables"].(map[string]interface{}); ok {
			for k, v := range envVars {
				if vStr, ok := v.(string); ok {
					config.envVars[k] = vStr
				}
			}
		}

		// Parse secure environment variables
		if secureEnvVars, ok := cMap["secure_environment_variables"].(map[string]interface{}); ok {
			for k, v := range secureEnvVars {
				if vStr, ok := v.(string); ok {
					config.envVars[k] = vStr
				}
			}
		}

		containers = append(containers, config)
	}

	return containers
}

func (m *ContainerInstanceMapper) getVolumes(res *resource.AWSResource) []volumeConfig {
	var volumes []volumeConfig

	// Check container volume mounts
	containerList, ok := res.Config["container"].([]interface{})
	if !ok {
		return volumes
	}

	for _, c := range containerList {
		cMap, ok := c.(map[string]interface{})
		if !ok {
			continue
		}

		if volumeMounts, ok := cMap["volume"].([]interface{}); ok {
			for _, vm := range volumeMounts {
				if vmMap, ok := vm.(map[string]interface{}); ok {
					name, _ := vmMap["name"].(string)
					mountPath, _ := vmMap["mount_path"].(string)
					readOnly, _ := vmMap["read_only"].(bool)

					if name != "" && mountPath != "" {
						mount := fmt.Sprintf("%s:%s", name, mountPath)
						if readOnly {
							mount += ":ro"
						}
						volumes = append(volumes, volumeConfig{
							mount: mount,
							volumeDef: mapper.Volume{
								Name:   name,
								Driver: "local",
							},
						})
					}
				}
			}
		}
	}

	return volumes
}

func (m *ContainerInstanceMapper) handleMultiContainerGroup(containers []containerConfig, groupName string, result *mapper.MappingResult) {
	result.AddWarning(fmt.Sprintf("Container group '%s' has %d sidecar containers. Additional services generated.", groupName, len(containers)))

	for i, container := range containers {
		sidecarName := m.sanitizeServiceName(container.name)
		if sidecarName == "" {
			sidecarName = fmt.Sprintf("%s-sidecar-%d", groupName, i+1)
		}

		sidecarConfig := m.generateSidecarConfig(sidecarName, container)
		result.AddConfig(fmt.Sprintf("config/sidecar-%s.yml", sidecarName), []byte(sidecarConfig))
	}
}

func (m *ContainerInstanceMapper) generateSidecarConfig(name string, container containerConfig) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Sidecar container: %s\n", name))
	sb.WriteString(fmt.Sprintf("%s:\n", name))
	sb.WriteString(fmt.Sprintf("  image: %s\n", container.image))

	if len(container.commands) > 0 {
		sb.WriteString("  command:\n")
		for _, cmd := range container.commands {
			sb.WriteString(fmt.Sprintf("    - %s\n", cmd))
		}
	}

	if len(container.envVars) > 0 {
		sb.WriteString("  environment:\n")
		for k, v := range container.envVars {
			sb.WriteString(fmt.Sprintf("    %s: %s\n", k, v))
		}
	}

	if container.cpu > 0 || container.memory > 0 {
		sb.WriteString("  deploy:\n")
		sb.WriteString("    resources:\n")
		sb.WriteString("      limits:\n")
		if container.cpu > 0 {
			sb.WriteString(fmt.Sprintf("        cpus: '%.1f'\n", container.cpu))
		}
		if container.memory > 0 {
			sb.WriteString(fmt.Sprintf("        memory: %.1fG\n", container.memory))
		}
	}

	if len(container.ports) > 0 {
		sb.WriteString("  ports:\n")
		for _, port := range container.ports {
			sb.WriteString(fmt.Sprintf("    - %s\n", port))
		}
	}

	sb.WriteString("  networks:\n")
	sb.WriteString("    - homeport\n")
	sb.WriteString("  restart: unless-stopped\n")

	return sb.String()
}

func (m *ContainerInstanceMapper) sanitizeServiceName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "_", "-")
	name = strings.ReplaceAll(name, " ", "-")
	return name
}
