// Package compute provides mappers for GCP compute services.
package compute

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

// CloudRunMapper converts GCP Cloud Run services to Docker containers.
type CloudRunMapper struct {
	*mapper.BaseMapper
}

// NewCloudRunMapper creates a new Cloud Run to Docker mapper.
func NewCloudRunMapper() *CloudRunMapper {
	return &CloudRunMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeCloudRun, nil),
	}
}

// Map converts a Cloud Run service to a Docker service.
func (m *CloudRunMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	serviceName := res.GetConfigString("name")
	if serviceName == "" {
		serviceName = res.Name
	}

	result := mapper.NewMappingResult(m.sanitizeName(serviceName))
	svc := result.DockerService

	// Extract container image
	image := m.extractImage(res)
	svc.Image = image

	// Extract port
	containerPort := m.extractPort(res)
	svc.Ports = []string{fmt.Sprintf("%d:%d", containerPort, containerPort)}

	// Environment variables
	svc.Environment = m.extractEnvironment(res)
	svc.Environment["PORT"] = fmt.Sprintf("%d", containerPort)

	// Resource limits
	m.applyResourceLimits(res, svc)

	// Configure for Traefik
	svc.Labels = map[string]string{
		"cloudexit.source":       "google_cloud_run_service",
		"cloudexit.service_name": serviceName,
		"traefik.enable":         "true",
		"traefik.http.routers." + m.sanitizeName(serviceName) + ".rule": fmt.Sprintf("Host(`%s.localhost`)", m.sanitizeName(serviceName)),
		"traefik.http.services." + m.sanitizeName(serviceName) + ".loadbalancer.server.port": fmt.Sprintf("%d", containerPort),
	}

	svc.Networks = []string{"cloudexit"}
	svc.Restart = "unless-stopped"

	// Health check
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD-SHELL", fmt.Sprintf("curl -f http://localhost:%d/ || exit 1", containerPort)},
		Interval: 30 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  3,
	}

	// Handle autoscaling
	if autoscaling := res.Config["autoscaling"]; autoscaling != nil {
		m.handleAutoscaling(autoscaling, result)
	}

	// Handle traffic
	if traffic := res.Config["traffic"]; traffic != nil {
		result.AddWarning("Cloud Run traffic splitting is configured. Docker doesn't support traffic splitting natively.")
		result.AddManualStep("Configure Traefik weighted routing for traffic splitting if needed")
	}

	// Handle VPC connector
	if vpcAccess := res.Config["vpc_access"]; vpcAccess != nil {
		result.AddWarning("VPC Access Connector is configured. Configure Docker networks accordingly.")
	}

	result.AddManualStep(fmt.Sprintf("Access service at: http://%s.localhost (requires Traefik)", m.sanitizeName(serviceName)))
	result.AddManualStep("Configure environment variables with actual values")

	return result, nil
}

// extractImage extracts the container image from Cloud Run config.
func (m *CloudRunMapper) extractImage(res *resource.AWSResource) string {
	// Check template -> spec -> containers
	if template := res.Config["template"]; template != nil {
		if tmplMap, ok := template.(map[string]interface{}); ok {
			if spec, ok := tmplMap["spec"].(map[string]interface{}); ok {
				if containers, ok := spec["containers"].([]interface{}); ok && len(containers) > 0 {
					if container, ok := containers[0].(map[string]interface{}); ok {
						if image, ok := container["image"].(string); ok {
							return image
						}
					}
				}
			}
		}
	}

	return "gcr.io/cloudrun/placeholder"
}

// extractPort extracts the container port from Cloud Run config.
func (m *CloudRunMapper) extractPort(res *resource.AWSResource) int {
	if template := res.Config["template"]; template != nil {
		if tmplMap, ok := template.(map[string]interface{}); ok {
			if spec, ok := tmplMap["spec"].(map[string]interface{}); ok {
				if containers, ok := spec["containers"].([]interface{}); ok && len(containers) > 0 {
					if container, ok := containers[0].(map[string]interface{}); ok {
						if ports, ok := container["ports"].([]interface{}); ok && len(ports) > 0 {
							if port, ok := ports[0].(map[string]interface{}); ok {
								if containerPort, ok := port["container_port"].(float64); ok {
									return int(containerPort)
								}
							}
						}
					}
				}
			}
		}
	}

	return 8080 // Cloud Run default
}

// extractEnvironment extracts environment variables.
func (m *CloudRunMapper) extractEnvironment(res *resource.AWSResource) map[string]string {
	env := make(map[string]string)

	if template := res.Config["template"]; template != nil {
		if tmplMap, ok := template.(map[string]interface{}); ok {
			if spec, ok := tmplMap["spec"].(map[string]interface{}); ok {
				if containers, ok := spec["containers"].([]interface{}); ok && len(containers) > 0 {
					if container, ok := containers[0].(map[string]interface{}); ok {
						if envVars, ok := container["env"].([]interface{}); ok {
							for _, e := range envVars {
								if envMap, ok := e.(map[string]interface{}); ok {
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
			}
		}
	}

	return env
}

// applyResourceLimits applies CPU and memory limits.
func (m *CloudRunMapper) applyResourceLimits(res *resource.AWSResource, svc *mapper.DockerService) {
	svc.Deploy = &mapper.DeployConfig{
		Resources: &mapper.Resources{
			Limits: &mapper.ResourceLimits{},
		},
	}

	if template := res.Config["template"]; template != nil {
		if tmplMap, ok := template.(map[string]interface{}); ok {
			if spec, ok := tmplMap["spec"].(map[string]interface{}); ok {
				if containers, ok := spec["containers"].([]interface{}); ok && len(containers) > 0 {
					if container, ok := containers[0].(map[string]interface{}); ok {
						if resources, ok := container["resources"].(map[string]interface{}); ok {
							if limits, ok := resources["limits"].(map[string]interface{}); ok {
								if cpu, ok := limits["cpu"].(string); ok {
									svc.Deploy.Resources.Limits.CPUs = cpu
								}
								if memory, ok := limits["memory"].(string); ok {
									svc.Deploy.Resources.Limits.Memory = memory
								}
							}
						}
					}
				}
			}
		}
	}

	// Defaults if not set
	if svc.Deploy.Resources.Limits.CPUs == "" {
		svc.Deploy.Resources.Limits.CPUs = "1"
	}
	if svc.Deploy.Resources.Limits.Memory == "" {
		svc.Deploy.Resources.Limits.Memory = "512Mi"
	}
}

// handleAutoscaling processes autoscaling configuration.
func (m *CloudRunMapper) handleAutoscaling(autoscaling interface{}, result *mapper.MappingResult) {
	if asMap, ok := autoscaling.(map[string]interface{}); ok {
		minInstances := 0
		maxInstances := 100

		if min, ok := asMap["min_instance_count"].(float64); ok {
			minInstances = int(min)
		}
		if max, ok := asMap["max_instance_count"].(float64); ok {
			maxInstances = int(max)
		}

		result.AddWarning(fmt.Sprintf("Cloud Run autoscaling is configured (min: %d, max: %d). Consider using Docker Swarm or Kubernetes for autoscaling.", minInstances, maxInstances))
	}
}

// sanitizeName sanitizes the name for Docker.
func (m *CloudRunMapper) sanitizeName(name string) string {
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
