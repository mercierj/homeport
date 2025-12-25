// Package networking provides mappers for Azure networking services.
package networking

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

// LBMapper converts Azure Load Balancer to Traefik or HAProxy.
type LBMapper struct {
	*mapper.BaseMapper
}

// NewLBMapper creates a new Azure Load Balancer to Traefik/HAProxy mapper.
func NewLBMapper() *LBMapper {
	return &LBMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeAzureLB, nil),
	}
}

// Map converts an Azure Load Balancer to a Traefik service.
func (m *LBMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	lbName := res.GetConfigString("name")
	if lbName == "" {
		lbName = res.Name
	}

	result := mapper.NewMappingResult(m.sanitizeName(lbName))
	svc := result.DockerService

	// Use Traefik as the default load balancer
	svc.Image = "traefik:v2.10"
	svc.Command = []string{
		"--api.insecure=true",
		"--providers.docker=true",
		"--providers.docker.exposedbydefault=false",
		"--providers.file.directory=/etc/traefik/dynamic",
		"--entrypoints.web.address=:80",
		"--entrypoints.websecure.address=:443",
		"--log.level=INFO",
	}

	svc.Ports = []string{
		"80:80",
		"443:443",
		"8080:8080", // Traefik dashboard
	}

	svc.Volumes = []string{
		"/var/run/docker.sock:/var/run/docker.sock:ro",
		"./config/traefik:/etc/traefik/dynamic:ro",
	}

	svc.Networks = []string{"cloudexit"}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"cloudexit.source":  "azurerm_lb",
		"cloudexit.lb_name": lbName,
	}

	// Health check for Traefik
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "traefik", "healthcheck", "--ping"},
		Interval: 10 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  3,
	}

	// Handle frontend IP configuration
	if frontendIPConfigs := res.Config["frontend_ip_configuration"]; frontendIPConfigs != nil {
		m.handleFrontendIPs(frontendIPConfigs, result)
	}

	// Handle backend address pools
	if backendPools := res.Config["backend_address_pool"]; backendPools != nil {
		m.handleBackendPools(backendPools, result)
	}

	// Handle load balancing rules
	var lbRules []interface{}
	if rules := res.Config["lb_rule"]; rules != nil {
		if ruleSlice, ok := rules.([]interface{}); ok {
			lbRules = ruleSlice
		}
	}

	// Handle health probes
	var healthProbes []interface{}
	if probes := res.Config["probe"]; probes != nil {
		if probeSlice, ok := probes.([]interface{}); ok {
			healthProbes = probeSlice
		}
	}

	// Generate Traefik dynamic configuration
	traefikConfig := m.generateTraefikConfig(lbName, lbRules, healthProbes)
	result.AddConfig("config/traefik/lb-config.yml", []byte(traefikConfig))

	// Generate HAProxy alternative configuration
	haproxyConfig := m.generateHAProxyConfig(lbName, lbRules, healthProbes)
	result.AddConfig("config/haproxy/haproxy.cfg", []byte(haproxyConfig))

	// Generate setup script
	setupScript := m.generateSetupScript(lbName)
	result.AddScript("setup_lb.sh", []byte(setupScript))

	result.AddWarning("Azure Load Balancer converted to Traefik. Review backend configurations.")
	result.AddWarning("HAProxy configuration also provided as an alternative in config/haproxy/")
	result.AddManualStep("Configure backend service endpoints in Traefik dynamic configuration")
	result.AddManualStep("Update health probe settings to match your services")
	result.AddManualStep("Configure SSL certificates if using HTTPS")

	return result, nil
}

// handleFrontendIPs processes frontend IP configurations.
func (m *LBMapper) handleFrontendIPs(frontendIPs interface{}, result *mapper.MappingResult) {
	if ipSlice, ok := frontendIPs.([]interface{}); ok {
		for _, ip := range ipSlice {
			if ipMap, ok := ip.(map[string]interface{}); ok {
				name, _ := ipMap["name"].(string)
				privateIP, _ := ipMap["private_ip_address"].(string)
				publicIP, _ := ipMap["public_ip_address_id"].(string)

				if publicIP != "" {
					result.AddWarning(fmt.Sprintf("Frontend IP '%s' uses public IP. Configure DNS to point to your Docker host.", name))
				}
				if privateIP != "" {
					result.AddWarning(fmt.Sprintf("Frontend IP '%s' has private IP %s. Update to Docker host IP.", name, privateIP))
				}
			}
		}
	}
}

// handleBackendPools processes backend address pools.
func (m *LBMapper) handleBackendPools(pools interface{}, result *mapper.MappingResult) {
	if poolSlice, ok := pools.([]interface{}); ok {
		for _, pool := range poolSlice {
			if poolMap, ok := pool.(map[string]interface{}); ok {
				name, _ := poolMap["name"].(string)
				result.AddWarning(fmt.Sprintf("Backend pool '%s' detected. Configure corresponding Docker services.", name))
			}
		}
	}
}

// generateTraefikConfig generates Traefik dynamic configuration.
func (m *LBMapper) generateTraefikConfig(lbName string, rules []interface{}, probes []interface{}) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Traefik dynamic configuration for Azure LB: %s\n\n", lbName))
	sb.WriteString("http:\n")
	sb.WriteString("  routers:\n")
	sb.WriteString("    lb-router:\n")
	sb.WriteString("      rule: \"PathPrefix(`/`)\"\n")
	sb.WriteString("      service: lb-service\n")
	sb.WriteString("      entryPoints:\n")
	sb.WriteString("        - web\n")
	sb.WriteString("        - websecure\n\n")

	sb.WriteString("  services:\n")
	sb.WriteString("    lb-service:\n")
	sb.WriteString("      loadBalancer:\n")
	sb.WriteString("        servers:\n")
	sb.WriteString("          # Configure your backend servers here\n")
	sb.WriteString("          - url: \"http://backend1:8080\"\n")
	sb.WriteString("          - url: \"http://backend2:8080\"\n")

	// Add health check configuration if probes exist
	if len(probes) > 0 {
		for _, probe := range probes {
			if probeMap, ok := probe.(map[string]interface{}); ok {
				protocol, _ := probeMap["protocol"].(string)
				port, _ := probeMap["port"].(float64)
				path, _ := probeMap["request_path"].(string)

				if protocol == "Http" || protocol == "Https" {
					sb.WriteString("        healthCheck:\n")
					if path != "" {
						sb.WriteString(fmt.Sprintf("          path: \"%s\"\n", path))
					} else {
						sb.WriteString("          path: \"/\"\n")
					}
					if port > 0 {
						sb.WriteString(fmt.Sprintf("          port: %d\n", int(port)))
					}
					sb.WriteString("          interval: \"10s\"\n")
					sb.WriteString("          timeout: \"5s\"\n")
					break
				}
			}
		}
	}

	return sb.String()
}

// generateHAProxyConfig generates HAProxy configuration as an alternative.
func (m *LBMapper) generateHAProxyConfig(lbName string, rules []interface{}, probes []interface{}) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# HAProxy configuration for Azure LB: %s\n\n", lbName))
	sb.WriteString("global\n")
	sb.WriteString("    log stdout format raw local0\n")
	sb.WriteString("    maxconn 4096\n\n")

	sb.WriteString("defaults\n")
	sb.WriteString("    mode http\n")
	sb.WriteString("    log global\n")
	sb.WriteString("    option httplog\n")
	sb.WriteString("    option dontlognull\n")
	sb.WriteString("    timeout connect 5000ms\n")
	sb.WriteString("    timeout client 50000ms\n")
	sb.WriteString("    timeout server 50000ms\n\n")

	sb.WriteString("frontend http-in\n")
	sb.WriteString("    bind *:80\n")
	sb.WriteString("    bind *:443\n")
	sb.WriteString("    default_backend servers\n\n")

	sb.WriteString("backend servers\n")
	sb.WriteString("    balance roundrobin\n")

	// Add health check options if probes exist
	healthCheckOpts := "check"
	if len(probes) > 0 {
		for _, probe := range probes {
			if probeMap, ok := probe.(map[string]interface{}); ok {
				interval, _ := probeMap["interval_in_seconds"].(float64)
				if interval > 0 {
					healthCheckOpts = fmt.Sprintf("check inter %ds", int(interval))
					break
				}
			}
		}
	}

	sb.WriteString(fmt.Sprintf("    server backend1 backend1:8080 %s\n", healthCheckOpts))
	sb.WriteString(fmt.Sprintf("    server backend2 backend2:8080 %s\n", healthCheckOpts))

	return sb.String()
}

// generateSetupScript generates a setup script for the load balancer.
func (m *LBMapper) generateSetupScript(lbName string) string {
	return fmt.Sprintf(`#!/bin/bash
# Setup script for Azure Load Balancer: %s

set -e

echo "Creating Traefik configuration directory..."
mkdir -p ./config/traefik
mkdir -p ./config/haproxy

echo "Starting Traefik load balancer..."
docker-compose up -d %s

echo "Waiting for Traefik to be ready..."
sleep 10

echo ""
echo "Traefik dashboard available at: http://localhost:8080"
echo "Load balancer listening on ports 80 (HTTP) and 443 (HTTPS)"
echo ""
echo "Next steps:"
echo "1. Configure backend services in config/traefik/lb-config.yml"
echo "2. Start your backend services"
echo "3. Verify load balancing: curl http://localhost"
`, lbName, m.sanitizeName(lbName))
}

// sanitizeName creates a valid Docker service name.
func (m *LBMapper) sanitizeName(name string) string {
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
		validName = "azure-lb"
	}
	return validName
}
