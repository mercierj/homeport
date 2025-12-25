// Package compute provides mappers for AWS compute services.
package compute

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

// ECSMapper converts AWS ECS services and task definitions to Docker Compose.
type ECSMapper struct {
	*mapper.BaseMapper
}

// NewECSMapper creates a new ECS to Docker Compose mapper.
func NewECSMapper() *ECSMapper {
	return &ECSMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeECSService, nil),
	}
}

// Map converts an ECS service to a Docker Compose service.
func (m *ECSMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	serviceName := res.GetConfigString("name")
	if serviceName == "" {
		serviceName = res.Name
	}

	result := mapper.NewMappingResult(m.sanitizeServiceName(serviceName))
	svc := result.DockerService

	// Extract task definition info
	taskDef := res.GetConfigString("task_definition")
	desiredCount := res.GetConfigInt("desired_count")
	if desiredCount == 0 {
		desiredCount = 1
	}

	// Configure Docker service
	svc.Image = m.extractImageFromTaskDef(res)
	if svc.Image == "" {
		svc.Image = "nginx:alpine" // Default placeholder
		result.AddWarning("Could not extract container image from task definition. Using placeholder.")
		result.AddManualStep("Update the image in docker-compose.yml with your actual container image")
	}

	svc.Environment = m.extractEnvironmentVariables(res)
	svc.Ports = m.extractPortMappings(res)
	svc.Volumes = m.extractVolumeMounts(res)
	svc.Networks = []string{"cloudexit"}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"cloudexit.source":       "aws_ecs_service",
		"cloudexit.service_name": serviceName,
		"cloudexit.task_def":     taskDef,
	}

	// Add health check if defined
	if healthCheck := m.extractHealthCheck(res); healthCheck != nil {
		svc.HealthCheck = healthCheck
	}

	// Configure deployment settings
	svc.Deploy = &mapper.DeployConfig{
		Replicas: desiredCount,
	}

	// Extract resource limits from task definition
	cpu := res.GetConfigInt("cpu")
	memory := res.GetConfigInt("memory")
	if cpu > 0 || memory > 0 {
		svc.Deploy.Resources = &mapper.Resources{
			Limits: &mapper.ResourceLimits{},
		}
		if cpu > 0 {
			svc.Deploy.Resources.Limits.CPUs = fmt.Sprintf("%.2f", float64(cpu)/1024)
		}
		if memory > 0 {
			svc.Deploy.Resources.Limits.Memory = fmt.Sprintf("%dM", memory)
		}
	}

	// Handle load balancer configuration
	if lbConfig := res.Config["load_balancer"]; lbConfig != nil {
		m.handleLoadBalancer(lbConfig, svc, result)
	}

	// Handle service discovery
	if serviceRegistries := res.Config["service_registries"]; serviceRegistries != nil {
		result.AddWarning("ECS Service Discovery is configured. Consider using Docker DNS or Traefik for service discovery.")
	}

	// Handle deployment circuit breaker
	if deployConfig := res.Config["deployment_circuit_breaker"]; deployConfig != nil {
		result.AddWarning("Deployment circuit breaker is enabled. Docker Compose has limited deployment rollback support.")
	}

	// Generate setup script
	setupScript := m.generateSetupScript(serviceName, desiredCount)
	result.AddScript("setup_ecs_service.sh", []byte(setupScript))

	result.AddManualStep("Review container image and update if needed")
	result.AddManualStep("Configure environment variables with actual values")
	result.AddManualStep("Set up volume mounts for persistent data")

	return result, nil
}

// extractImageFromTaskDef extracts the container image from task definition.
func (m *ECSMapper) extractImageFromTaskDef(res *resource.AWSResource) string {
	// Try to extract from container_definitions
	if containerDefs, ok := res.Config["container_definitions"].([]interface{}); ok && len(containerDefs) > 0 {
		if firstContainer, ok := containerDefs[0].(map[string]interface{}); ok {
			if image, ok := firstContainer["image"].(string); ok {
				return image
			}
		}
	}
	return ""
}

// extractEnvironmentVariables extracts environment variables from container definitions.
func (m *ECSMapper) extractEnvironmentVariables(res *resource.AWSResource) map[string]string {
	env := make(map[string]string)

	if containerDefs, ok := res.Config["container_definitions"].([]interface{}); ok && len(containerDefs) > 0 {
		if firstContainer, ok := containerDefs[0].(map[string]interface{}); ok {
			if envVars, ok := firstContainer["environment"].([]interface{}); ok {
				for _, envVar := range envVars {
					if envMap, ok := envVar.(map[string]interface{}); ok {
						name, _ := envMap["name"].(string)
						value, _ := envMap["value"].(string)
						if name != "" {
							env[name] = value
						}
					}
				}
			}
		}
	}

	return env
}

// extractPortMappings extracts port mappings from container definitions.
func (m *ECSMapper) extractPortMappings(res *resource.AWSResource) []string {
	var ports []string

	if containerDefs, ok := res.Config["container_definitions"].([]interface{}); ok && len(containerDefs) > 0 {
		if firstContainer, ok := containerDefs[0].(map[string]interface{}); ok {
			if portMappings, ok := firstContainer["portMappings"].([]interface{}); ok {
				for _, pm := range portMappings {
					if portMap, ok := pm.(map[string]interface{}); ok {
						containerPort := 0
						hostPort := 0

						if cp, ok := portMap["containerPort"].(float64); ok {
							containerPort = int(cp)
						}
						if hp, ok := portMap["hostPort"].(float64); ok {
							hostPort = int(hp)
						}

						if hostPort == 0 {
							hostPort = containerPort
						}

						if containerPort > 0 {
							ports = append(ports, fmt.Sprintf("%d:%d", hostPort, containerPort))
						}
					}
				}
			}
		}
	}

	return ports
}

// extractVolumeMounts extracts volume mounts from container definitions.
func (m *ECSMapper) extractVolumeMounts(res *resource.AWSResource) []string {
	var volumes []string

	if containerDefs, ok := res.Config["container_definitions"].([]interface{}); ok && len(containerDefs) > 0 {
		if firstContainer, ok := containerDefs[0].(map[string]interface{}); ok {
			if mountPoints, ok := firstContainer["mountPoints"].([]interface{}); ok {
				for _, mp := range mountPoints {
					if mount, ok := mp.(map[string]interface{}); ok {
						sourceVolume, _ := mount["sourceVolume"].(string)
						containerPath, _ := mount["containerPath"].(string)
						if sourceVolume != "" && containerPath != "" {
							volumes = append(volumes, fmt.Sprintf("./data/%s:%s", sourceVolume, containerPath))
						}
					}
				}
			}
		}
	}

	return volumes
}

// extractHealthCheck extracts health check configuration.
func (m *ECSMapper) extractHealthCheck(res *resource.AWSResource) *mapper.HealthCheck {
	if containerDefs, ok := res.Config["container_definitions"].([]interface{}); ok && len(containerDefs) > 0 {
		if firstContainer, ok := containerDefs[0].(map[string]interface{}); ok {
			if hc, ok := firstContainer["healthCheck"].(map[string]interface{}); ok {
				healthCheck := &mapper.HealthCheck{
					Retries: 3,
				}

				if command, ok := hc["command"].([]interface{}); ok {
					for _, c := range command {
						if cmd, ok := c.(string); ok {
							healthCheck.Test = append(healthCheck.Test, cmd)
						}
					}
				}

				if interval, ok := hc["interval"].(float64); ok {
					healthCheck.Interval = time.Duration(interval) * time.Second
				} else {
					healthCheck.Interval = 30 * time.Second
				}

				if timeout, ok := hc["timeout"].(float64); ok {
					healthCheck.Timeout = time.Duration(timeout) * time.Second
				} else {
					healthCheck.Timeout = 5 * time.Second
				}

				if retries, ok := hc["retries"].(float64); ok {
					healthCheck.Retries = int(retries)
				}

				if len(healthCheck.Test) > 0 {
					return healthCheck
				}
			}
		}
	}

	return nil
}

// handleLoadBalancer configures Traefik labels for load balancing.
func (m *ECSMapper) handleLoadBalancer(lbConfig interface{}, svc *mapper.DockerService, result *mapper.MappingResult) {
	if lbSlice, ok := lbConfig.([]interface{}); ok && len(lbSlice) > 0 {
		if lb, ok := lbSlice[0].(map[string]interface{}); ok {
			containerPort := 0
			if cp, ok := lb["container_port"].(float64); ok {
				containerPort = int(cp)
			}

			if containerPort > 0 {
				svc.Labels["traefik.enable"] = "true"
				svc.Labels["traefik.http.routers."+svc.Name+".rule"] = fmt.Sprintf("Host(`%s.localhost`)", svc.Name)
				svc.Labels["traefik.http.services."+svc.Name+".loadbalancer.server.port"] = fmt.Sprintf("%d", containerPort)
			}
		}
	}

	result.AddWarning("ECS service uses load balancer. Traefik labels have been configured for routing.")
}

// generateSetupScript creates a setup script for the ECS service.
func (m *ECSMapper) generateSetupScript(serviceName string, replicas int) string {
	return fmt.Sprintf(`#!/bin/bash
# Setup script for ECS Service: %s

set -e

echo "Setting up ECS Service: %s"

# Create data directories
mkdir -p ./data

# Scale the service
echo "Scaling service to %d replicas..."
docker-compose up -d --scale %s=%d

echo "Service '%s' is ready!"
echo "Access at: http://%s.localhost (if Traefik is configured)"
`, serviceName, serviceName, replicas, serviceName, replicas, serviceName, serviceName)
}

// sanitizeServiceName sanitizes the service name for Docker Compose.
func (m *ECSMapper) sanitizeServiceName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "_", "-")
	name = strings.ReplaceAll(name, " ", "-")

	validName := ""
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' {
			validName += string(ch)
		}
	}

	validName = strings.TrimLeft(validName, "-0123456789")

	if validName == "" {
		validName = "service"
	}

	return validName
}

// ECSTaskDefMapper converts AWS ECS Task Definitions to Docker Compose.
type ECSTaskDefMapper struct {
	*mapper.BaseMapper
}

// NewECSTaskDefMapper creates a new ECS Task Definition mapper.
func NewECSTaskDefMapper() *ECSTaskDefMapper {
	return &ECSTaskDefMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeECSTaskDef, nil),
	}
}

// Map converts an ECS task definition to Docker Compose services.
func (m *ECSTaskDefMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	taskFamily := res.GetConfigString("family")
	if taskFamily == "" {
		taskFamily = res.Name
	}

	result := mapper.NewMappingResult(taskFamily)
	svc := result.DockerService

	// Extract container definitions
	containerDefs, ok := res.Config["container_definitions"].([]interface{})
	if !ok || len(containerDefs) == 0 {
		return nil, fmt.Errorf("no container definitions found in task definition")
	}

	// Use the first (or primary) container
	firstContainer, ok := containerDefs[0].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid container definition format")
	}

	// Configure service from container definition
	if image, ok := firstContainer["image"].(string); ok {
		svc.Image = image
	}

	if name, ok := firstContainer["name"].(string); ok {
		svc.Name = name
	}

	// Environment variables
	if envVars, ok := firstContainer["environment"].([]interface{}); ok {
		for _, envVar := range envVars {
			if envMap, ok := envVar.(map[string]interface{}); ok {
				name, _ := envMap["name"].(string)
				value, _ := envMap["value"].(string)
				if name != "" {
					svc.Environment[name] = value
				}
			}
		}
	}

	// Port mappings
	if portMappings, ok := firstContainer["portMappings"].([]interface{}); ok {
		for _, pm := range portMappings {
			if portMap, ok := pm.(map[string]interface{}); ok {
				containerPort := 0
				hostPort := 0

				if cp, ok := portMap["containerPort"].(float64); ok {
					containerPort = int(cp)
				}
				if hp, ok := portMap["hostPort"].(float64); ok {
					hostPort = int(hp)
				}

				if hostPort == 0 {
					hostPort = containerPort
				}

				if containerPort > 0 {
					svc.Ports = append(svc.Ports, fmt.Sprintf("%d:%d", hostPort, containerPort))
				}
			}
		}
	}

	// Resource requirements
	cpu := res.GetConfigInt("cpu")
	memory := res.GetConfigInt("memory")
	if cpu > 0 || memory > 0 {
		svc.Deploy = &mapper.DeployConfig{
			Resources: &mapper.Resources{
				Limits: &mapper.ResourceLimits{},
			},
		}
		if cpu > 0 {
			svc.Deploy.Resources.Limits.CPUs = fmt.Sprintf("%.2f", float64(cpu)/1024)
		}
		if memory > 0 {
			svc.Deploy.Resources.Limits.Memory = fmt.Sprintf("%dM", memory)
		}
	}

	svc.Networks = []string{"cloudexit"}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"cloudexit.source":      "aws_ecs_task_definition",
		"cloudexit.task_family": taskFamily,
	}

	// Handle multiple containers
	if len(containerDefs) > 1 {
		result.AddWarning(fmt.Sprintf("Task definition has %d containers. Only the primary container is mapped. Additional containers need manual configuration.", len(containerDefs)))
		result.AddManualStep("Review task definition and add sidecar containers to docker-compose.yml")
	}

	// Handle Fargate requirements
	requiresCompatibilities := res.GetConfigString("requires_compatibilities")
	if strings.Contains(requiresCompatibilities, "FARGATE") {
		result.AddWarning("Task uses Fargate compatibility. Resource limits have been configured for Docker.")
	}

	return result, nil
}
