// Package networking provides mappers for GCP networking services.
package networking

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cloudexit/cloudexit/internal/domain/mapper"
	"github.com/cloudexit/cloudexit/internal/domain/resource"
)

// CloudLBMapper converts GCP Cloud Load Balancer (Backend Service) to Traefik or HAProxy.
type CloudLBMapper struct {
	*mapper.BaseMapper
}

// NewCloudLBMapper creates a new Cloud Load Balancer to Traefik/HAProxy mapper.
func NewCloudLBMapper() *CloudLBMapper {
	return &CloudLBMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeCloudLB, nil),
	}
}

// Map converts a GCP Cloud Load Balancer to a Traefik service.
func (m *CloudLBMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	serviceName := res.GetConfigString("name")
	if serviceName == "" {
		serviceName = res.Name
	}

	result := mapper.NewMappingResult(m.sanitizeName(serviceName))
	svc := result.DockerService

	// Use Traefik as the load balancer
	svc.Image = "traefik:v2.10"

	// Configure ports
	svc.Ports = []string{
		"80:80",     // HTTP
		"443:443",   // HTTPS
		"8080:8080", // Traefik dashboard
	}

	// Extract load balancing algorithm
	lbScheme := m.extractLoadBalancingScheme(res)
	sessionAffinity := m.extractSessionAffinity(res)

	// Environment variables
	svc.Environment = map[string]string{
		"TRAEFIK_LOG_LEVEL": "INFO",
	}

	// Configure Traefik with labels
	svc.Labels = map[string]string{
		"cloudexit.source":       "google_compute_backend_service",
		"cloudexit.service_name": serviceName,
		"traefik.enable":         "true",
		"traefik.http.routers.dashboard.rule":         "Host(`traefik.localhost`)",
		"traefik.http.routers.dashboard.service":      "api@internal",
		"traefik.http.routers.dashboard.entrypoints":  "web",
		"traefik.http.services." + m.sanitizeName(serviceName) + ".loadbalancer.sticky": fmt.Sprintf("%t", sessionAffinity),
	}

	svc.Networks = []string{"cloudexit"}
	svc.Restart = "unless-stopped"

	// Generate Traefik configuration
	traefikConfig := m.generateTraefikConfig(serviceName, lbScheme, sessionAffinity, res)
	result.AddConfig("traefik.yml", []byte(traefikConfig))

	// Handle backends
	backends := m.extractBackends(res)
	if len(backends) > 0 {
		result.AddWarning(fmt.Sprintf("Backend service has %d backend(s). Configure backend services separately.", len(backends)))
		for i, backend := range backends {
			result.AddManualStep(fmt.Sprintf("Configure backend %d: %s", i+1, backend))
		}
	}

	// Handle health checks
	if healthCheck := m.extractHealthCheck(res); healthCheck != nil {
		healthCheckConfig := m.generateHealthCheckConfig(healthCheck)
		result.AddConfig("health-check.yml", []byte(healthCheckConfig))
		result.AddWarning("Health check configuration generated. Integrate with backend services.")
	}

	// Handle protocol
	protocol := res.GetConfigString("protocol")
	if protocol != "" {
		result.AddWarning(fmt.Sprintf("Load balancer protocol: %s. Ensure Traefik is configured accordingly.", protocol))
	}

	// Handle timeout settings
	if timeoutSec := res.GetConfigInt("timeout_sec"); timeoutSec > 0 {
		result.AddWarning(fmt.Sprintf("Backend timeout: %d seconds. Configure in Traefik middleware.", timeoutSec))
	}

	// Health check
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD-SHELL", "traefik healthcheck --ping || exit 1"},
		Interval: 30 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  3,
	}

	// Generate dynamic configuration for backends
	if len(backends) > 0 {
		dynamicConfig := m.generateDynamicConfig(serviceName, backends, lbScheme)
		result.AddConfig("dynamic-config.yml", []byte(dynamicConfig))
	}

	result.AddManualStep("Access Traefik dashboard at: http://traefik.localhost:8080")
	result.AddManualStep("Configure SSL/TLS certificates for HTTPS support")
	result.AddManualStep("Update backend service URLs in dynamic-config.yml")
	result.AddWarning("Consider using HAProxy for more advanced load balancing features")

	return result, nil
}

// extractLoadBalancingScheme extracts the load balancing algorithm.
func (m *CloudLBMapper) extractLoadBalancingScheme(res *resource.AWSResource) string {
	if locality := res.Config["locality_lb_policy"]; locality != nil {
		if policy, ok := locality.(string); ok {
			return policy
		}
	}
	return "ROUND_ROBIN"
}

// extractSessionAffinity checks for session affinity settings.
func (m *CloudLBMapper) extractSessionAffinity(res *resource.AWSResource) bool {
	if sessionAffinity := res.Config["session_affinity"]; sessionAffinity != nil {
		if affinity, ok := sessionAffinity.(string); ok {
			return affinity != "NONE"
		}
	}
	return false
}

// extractBackends extracts backend configuration.
func (m *CloudLBMapper) extractBackends(res *resource.AWSResource) []string {
	backends := []string{}

	if backend := res.Config["backend"]; backend != nil {
		if backendSlice, ok := backend.([]interface{}); ok {
			for _, b := range backendSlice {
				if backendMap, ok := b.(map[string]interface{}); ok {
					if group, ok := backendMap["group"].(string); ok {
						backends = append(backends, group)
					}
				}
			}
		}
	}

	return backends
}

// extractHealthCheck extracts health check configuration.
func (m *CloudLBMapper) extractHealthCheck(res *resource.AWSResource) map[string]interface{} {
	if healthCheck := res.Config["health_checks"]; healthCheck != nil {
		if hcSlice, ok := healthCheck.([]interface{}); ok && len(hcSlice) > 0 {
			if hcMap, ok := hcSlice[0].(map[string]interface{}); ok {
				return hcMap
			}
		}
	}
	return nil
}

// generateTraefikConfig generates the main Traefik configuration.
func (m *CloudLBMapper) generateTraefikConfig(serviceName, lbScheme string, sessionAffinity bool, res *resource.AWSResource) string {
	protocol := res.GetConfigString("protocol")
	if protocol == "" {
		protocol = "HTTP"
	}

	return fmt.Sprintf(`# Traefik configuration for %s
global:
  checkNewVersion: true
  sendAnonymousUsage: false

api:
  dashboard: true
  insecure: true

entryPoints:
  web:
    address: ":80"
  websecure:
    address: ":443"

providers:
  file:
    filename: /etc/traefik/dynamic-config.yml
    watch: true
  docker:
    exposedByDefault: false

log:
  level: INFO

# Load balancing strategy: %s
# Protocol: %s
# Session affinity: %t
`, serviceName, lbScheme, protocol, sessionAffinity)
}

// generateHealthCheckConfig generates health check configuration.
func (m *CloudLBMapper) generateHealthCheckConfig(healthCheck map[string]interface{}) string {
	path := "/"
	if checkPath, ok := healthCheck["request_path"].(string); ok {
		path = checkPath
	}

	port := 80
	if checkPort, ok := healthCheck["port"].(float64); ok {
		port = int(checkPort)
	}

	interval := 30
	if checkInterval, ok := healthCheck["check_interval_sec"].(float64); ok {
		interval = int(checkInterval)
	}

	timeout := 5
	if checkTimeout, ok := healthCheck["timeout_sec"].(float64); ok {
		timeout = int(checkTimeout)
	}

	return fmt.Sprintf(`# Health check configuration
health_check:
  path: "%s"
  port: %d
  interval: %ds
  timeout: %ds

# Integrate this with your backend services
# Example for Docker health check:
# healthcheck:
#   test: ["CMD", "curl", "-f", "http://localhost:%d%s"]
#   interval: %ds
#   timeout: %ds
#   retries: 3
`, path, port, interval, timeout, port, path, interval, timeout)
}

// generateDynamicConfig generates Traefik dynamic configuration for backends.
func (m *CloudLBMapper) generateDynamicConfig(serviceName string, backends []string, lbScheme string) string {
	servers := ""
	for i := range backends {
		servers += fmt.Sprintf(`      server%d:
        url: "http://backend-%d:80"
`, i+1, i+1)
	}

	lbAlgorithm := "roundrobin"
	if strings.Contains(strings.ToLower(lbScheme), "least") {
		lbAlgorithm = "wrr" // Weighted Round Robin
	}

	return fmt.Sprintf(`# Dynamic Traefik configuration for %s
http:
  routers:
    %s:
      rule: "PathPrefix(` + "`/`)" + `"
      service: "%s"
      entryPoints:
        - web
        - websecure

  services:
    %s:
      loadBalancer:
        servers:
%s
        healthCheck:
          path: /health
          interval: 30s
          timeout: 5s
        passHostHeader: true

# Load balancing algorithm: %s
`, serviceName, serviceName, serviceName, serviceName, servers, lbAlgorithm)
}

// sanitizeName sanitizes the name for Docker.
func (m *CloudLBMapper) sanitizeName(name string) string {
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
		validName = "loadbalancer"
	}

	return validName
}
