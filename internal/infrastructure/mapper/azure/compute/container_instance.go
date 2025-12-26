// Package compute provides mappers for Azure compute services.
package compute

import (
	"context"
	"fmt"
	"strings"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
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
	svc.Networks = []string{"cloudexit"}

	// Set labels
	svc.Labels = map[string]string{
		"cloudexit.source":     "azurerm_container_group",
		"cloudexit.group_name": groupName,
	}

	// Handle OS type
	osType := res.GetConfigString("os_type")
	if osType != "" {
		svc.Labels["cloudexit.os_type"] = osType
	}
	if osType == "Windows" {
		result.AddWarning("Windows containers may require Windows Docker host.")
	}

	// Handle DNS name label
	dnsNameLabel := res.GetConfigString("dns_name_label")
	if dnsNameLabel != "" {
		svc.Labels["cloudexit.dns_label"] = dnsNameLabel
		result.AddWarning(fmt.Sprintf("Azure DNS label '%s' was configured. Configure DNS or /etc/hosts manually.", dnsNameLabel))
	}

	// Handle managed identity
	if identity := res.Config["identity"]; identity != nil {
		result.AddWarning("Managed identity was configured. Configure local secrets management.")
	}

	// Handle image registry credentials
	if registryCreds := res.Config["image_registry_credential"]; registryCreds != nil {
		result.AddWarning("Private registry credentials configured. Run 'docker login' with appropriate credentials.")
		result.AddManualStep("Configure Docker registry authentication: docker login <registry-url>")
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

	// Add manual steps
	result.AddManualStep(fmt.Sprintf("Start container: docker-compose up -d %s", serviceName))
	result.AddManualStep(fmt.Sprintf("View logs: docker-compose logs -f %s", serviceName))

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
	sb.WriteString("    - cloudexit\n")
	sb.WriteString("  restart: unless-stopped\n")

	return sb.String()
}

func (m *ContainerInstanceMapper) sanitizeServiceName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "_", "-")
	name = strings.ReplaceAll(name, " ", "-")
	return name
}
