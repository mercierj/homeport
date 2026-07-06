// Package networking provides mappers for GCP networking services.
package networking

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
	"github.com/homeport/homeport/internal/infrastructure/mapper/shared/netrunbook"
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
		"homeport.source":                            "google_compute_backend_service",
		"homeport.service_name":                      serviceName,
		"traefik.enable":                             "true",
		"traefik.http.routers.dashboard.rule":        "Host(`traefik.localhost`)",
		"traefik.http.routers.dashboard.service":     "api@internal",
		"traefik.http.routers.dashboard.entrypoints": "web",
		"traefik.http.services." + m.sanitizeName(serviceName) + ".loadbalancer.sticky": fmt.Sprintf("%t", sessionAffinity),
	}

	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"

	// Generate Traefik configuration
	traefikConfig := m.generateTraefikConfig(serviceName, lbScheme, sessionAffinity, res)
	result.AddConfig("traefik.yml", []byte(traefikConfig))
	result.AddConfig("config/cloud-lb/app-change.env", []byte(m.generateAppChangeConfig(serviceName)))

	// Handle backends
	backends := m.extractBackends(res)
	if len(backends) > 0 {
		result.AddWarning(fmt.Sprintf("Backend service has %d backend(s). Configure backend services separately.", len(backends)))
	}
	result.AddConfig("config/cloud-lb/backend-report.yaml", []byte(m.generateBackendReport(serviceName, lbScheme, backends)))

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

	result.AddScript("backup_cloud_lb.sh", []byte(m.generateBackupScript(serviceName)))
	result.AddScript("validate_cloud_lb.sh", []byte(m.generateValidateScript(serviceName)))
	result.AddWarning("Consider using HAProxy for more advanced load balancing features")
	for _, step := range netrunbook.Routing(serviceName, "google_compute_backend_service") {
		result.AddRunbookStep(step)
	}
	for _, step := range cloudLBRunbook(serviceName) {
		result.AddRunbookStep(step)
	}

	return result, nil
}

func (m *CloudLBMapper) generateAppChangeConfig(serviceName string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=generated_patch
SOURCE_CLOUD_LB_SERVICE=%s
TARGET_LB_ENDPOINT=http://%s:80
TARGET_LB_DASHBOARD=http://traefik.localhost:8080
TARGET_LB_CONFIG=traefik.yml
`, serviceName, m.sanitizeName(serviceName))
}

func (m *CloudLBMapper) generateBackendReport(serviceName, lbScheme string, backends []string) string {
	var b strings.Builder
	b.WriteString("source: google_compute_backend_service\n")
	b.WriteString("service: " + serviceName + "\n")
	b.WriteString("target: traefik\n")
	b.WriteString("load_balancing_policy: " + lbScheme + "\n")
	b.WriteString("backends:\n")
	for _, backend := range backends {
		b.WriteString("  - " + backend + "\n")
	}
	return b.String()
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
      rule: "PathPrefix(`+"`/`)"+`"
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

func (m *CloudLBMapper) generateBackupScript(serviceName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/cloud-lb-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" traefik.yml dynamic-config.yml health-check.yml config/cloud-lb
echo "$archive"
`, m.sanitizeName(serviceName))
}

func (m *CloudLBMapper) generateValidateScript(serviceName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
test -s traefik.yml
test -s config/cloud-lb/app-change.env
test -s config/cloud-lb/backend-report.yaml
traefik check --configFile=traefik.yml
echo "Cloud Load Balancing service %s validated on Traefik"
`, serviceName)
}

func cloudLBRunbook(serviceName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "load-balancer", "source": "google_compute_backend_service", "service": serviceName}
	return []domainrunbook.Step{
		cloudLBStep("discover-cloud-lb-service", "Discover Cloud Load Balancing service", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "-c", fmt.Sprintf("gcloud compute backend-services describe %q --global --format=json", serviceName)}, "backend service configuration is exported", metadata),
		cloudLBStep("provision-traefik-lb", "Provision Traefik load balancer", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s traefik.yml"}, "Traefik configuration is rendered", metadata),
		cloudLBStep("migrate-cloud-lb-backends", "Migrate Cloud LB backends", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s config/cloud-lb/backend-report.yaml"}, "backend report and dynamic routes are rendered", metadata),
		cloudLBStep("validate-traefik-lb", "Validate Traefik load balancer", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_cloud_lb.sh"}, "Traefik configuration validates", metadata),
		cloudLBStep("backup-cloud-lb-config", "Backup Traefik load balancer", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_cloud_lb.sh"}, "load balancer config archive is produced", metadata),
		cloudLBStep("cutover-cloud-lb-endpoint", "Cut over load balancer endpoint", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "-c", "test -s config/cloud-lb/app-change.env"}, "generated patch points traffic at Traefik", metadata),
		cloudLBStep("rollback-cloud-lb-service", "Keep Cloud Load Balancing as rollback", "Rollback", domainrunbook.StepTypeRollback, nil, "source backend service remains available until validation passes", metadata),
	}
}

func cloudLBStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
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
