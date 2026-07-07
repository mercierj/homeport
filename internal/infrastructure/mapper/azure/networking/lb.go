// Package networking provides mappers for Azure networking services.
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

	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Labels = map[string]string{
		"homeport.source":  "azurerm_lb",
		"homeport.lb_name": lbName,
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
	result.AddConfig("config/lb/app-change.env", []byte(m.generateAppChange(lbName)))
	result.AddConfig("config/lb/generated-client.patch", []byte(m.generateClientPatch(lbName)))

	// Generate setup script
	setupScript := m.generateSetupScript(lbName)
	result.AddScript("setup_lb.sh", []byte(setupScript))
	result.AddScript("validate_lb.sh", []byte(m.generateValidateScript(lbName)))
	result.AddScript("backup_lb_config.sh", []byte(m.generateBackupScript(lbName)))
	result.AddScript("cutover_lb_routes.sh", []byte(m.generateCutoverScript(lbName)))

	result.AddWarning("Azure Load Balancer converted to Traefik. Review backend configurations.")
	result.AddWarning("HAProxy configuration also provided as an alternative in config/haproxy/")
	for _, step := range netrunbook.Routing(lbName, "azurerm_lb") {
		result.AddRunbookStep(step)
	}
	for _, step := range m.runbook(lbName) {
		result.AddRunbookStep(step)
	}

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

func (m *LBMapper) generateAppChange(lbName string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_AZURE_LB=%s\nTRAEFIK_ENTRYPOINT=websecure\nTRAEFIK_CONFIG=config/traefik/lb-config.yml\nHAPROXY_CONFIG=config/haproxy/haproxy.cfg\nGENERATED_PATCH=config/lb/generated-client.patch\n", lbName)
}

func (m *LBMapper) generateClientPatch(lbName string) string {
	return fmt.Sprintf("--- a/app/load-balancer.env\n+++ b/app/load-balancer.env\n@@\n-AZURE_LOAD_BALANCER=%s\n+LOAD_BALANCER_BACKEND=traefik\n+TRAEFIK_CONFIG=config/traefik/lb-config.yml\n", lbName)
}

func (m *LBMapper) generateValidateScript(lbName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/traefik/lb-config.yml\ntest -s config/haproxy/haproxy.cfg\ntest -s config/lb/app-change.env\ngrep -q %q config/lb/app-change.env\n", lbName)
}

func (m *LBMapper) generateBackupScript(lbName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/lb-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/traefik config/haproxy config/lb setup_lb.sh validate_lb.sh\necho \"$archive\"\n", m.sanitizeName(lbName))
}

func (m *LBMapper) generateCutoverScript(lbName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/lb/app-change.env\ntest \"$SOURCE_AZURE_LB\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Apply $GENERATED_PATCH and route traffic through Traefik $TRAEFIK_ENTRYPOINT\"\n", lbName)
}

func (m *LBMapper) runbook(lbName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "routing", "source": "azurerm_lb", "lb": lbName, "target": "traefik-haproxy"}
	return []domainrunbook.Step{
		m.step("backup-lb-config", "Backup load balancer config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_lb_config.sh"}, "load balancer migration artifacts are archived", metadata),
		m.step("cutover-lb-routes", "Cut over load balancer routes", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_lb_routes.sh"}, "traffic routes through generated Traefik endpoint", metadata),
	}
}

func (m *LBMapper) step(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: "shell", Command: command, SuccessCondition: success, Metadata: metadata}
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
