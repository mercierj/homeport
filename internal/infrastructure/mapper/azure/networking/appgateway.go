// Package networking provides mappers for Azure networking services.
package networking

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cloudexit/cloudexit/internal/domain/mapper"
	"github.com/cloudexit/cloudexit/internal/domain/resource"
)

// AppGatewayMapper converts Azure Application Gateway to Traefik.
type AppGatewayMapper struct {
	*mapper.BaseMapper
}

// NewAppGatewayMapper creates a new Azure Application Gateway to Traefik mapper.
func NewAppGatewayMapper() *AppGatewayMapper {
	return &AppGatewayMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeAppGateway, nil),
	}
}

// Map converts an Azure Application Gateway to a Traefik service.
func (m *AppGatewayMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	gwName := res.GetConfigString("name")
	if gwName == "" {
		gwName = res.Name
	}

	result := mapper.NewMappingResult(m.sanitizeName(gwName))
	svc := result.DockerService

	// Use Traefik as the application gateway replacement
	svc.Image = "traefik:v2.10"
	svc.Command = []string{
		"--api.insecure=true",
		"--providers.docker=true",
		"--providers.docker.exposedbydefault=false",
		"--providers.file.directory=/etc/traefik/dynamic",
		"--entrypoints.web.address=:80",
		"--entrypoints.websecure.address=:443",
		"--log.level=INFO",
		"--accesslog=true",
		"--metrics.prometheus=true",
	}

	svc.Ports = []string{
		"80:80",
		"443:443",
		"8080:8080", // Traefik dashboard
	}

	svc.Volumes = []string{
		"/var/run/docker.sock:/var/run/docker.sock:ro",
		"./config/traefik:/etc/traefik/dynamic:ro",
		"./certs:/certs:ro",
	}

	svc.Networks = []string{"cloudexit"}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"cloudexit.source":      "azurerm_application_gateway",
		"cloudexit.gateway_name": gwName,
	}

	// Health check for Traefik
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "traefik", "healthcheck", "--ping"},
		Interval: 10 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  3,
	}

	// Extract SKU information
	if sku := res.Config["sku"]; sku != nil {
		if skuMap, ok := sku.(map[string]interface{}); ok {
			name, _ := skuMap["name"].(string)
			tier, _ := skuMap["tier"].(string)
			capacity, _ := skuMap["capacity"].(float64)

			result.AddWarning(fmt.Sprintf("Application Gateway SKU: %s (%s) with capacity %d", name, tier, int(capacity)))
			if strings.Contains(strings.ToLower(tier), "waf") {
				result.AddWarning("WAF tier detected. Consider adding ModSecurity or similar WAF solution.")
			}
		}
	}

	// Handle frontend IP configurations
	if frontendIPs := res.Config["frontend_ip_configuration"]; frontendIPs != nil {
		m.handleFrontendIPs(frontendIPs, result)
	}

	// Handle backend address pools
	var backendPools []interface{}
	if pools := res.Config["backend_address_pool"]; pools != nil {
		if poolSlice, ok := pools.([]interface{}); ok {
			backendPools = poolSlice
		}
	}

	// Handle backend HTTP settings
	var httpSettings []interface{}
	if settings := res.Config["backend_http_settings"]; settings != nil {
		if settingsSlice, ok := settings.([]interface{}); ok {
			httpSettings = settingsSlice
		}
	}

	// Handle HTTP listeners
	var listeners []interface{}
	if lsnrs := res.Config["http_listener"]; lsnrs != nil {
		if lsnrSlice, ok := lsnrs.([]interface{}); ok {
			listeners = lsnrSlice
		}
	}

	// Handle request routing rules
	var routingRules []interface{}
	if rules := res.Config["request_routing_rule"]; rules != nil {
		if ruleSlice, ok := rules.([]interface{}); ok {
			routingRules = ruleSlice
		}
	}

	// Handle URL path maps
	var urlPathMaps []interface{}
	if pathMaps := res.Config["url_path_map"]; pathMaps != nil {
		if mapSlice, ok := pathMaps.([]interface{}); ok {
			urlPathMaps = mapSlice
		}
	}

	// Handle WAF configuration
	if wafConfig := res.Config["waf_configuration"]; wafConfig != nil {
		m.handleWAFConfig(wafConfig, result)
	}

	// Generate Traefik dynamic configuration
	traefikConfig := m.generateTraefikConfig(gwName, backendPools, httpSettings, listeners, routingRules, urlPathMaps)
	result.AddConfig("config/traefik/appgateway-config.yml", []byte(traefikConfig))

	// Generate middleware configuration
	middlewareConfig := m.generateMiddlewareConfig(gwName)
	result.AddConfig("config/traefik/middleware.yml", []byte(middlewareConfig))

	// Generate setup script
	setupScript := m.generateSetupScript(gwName)
	result.AddScript("setup_appgateway.sh", []byte(setupScript))

	result.AddWarning("Azure Application Gateway converted to Traefik. Review routing rules carefully.")
	result.AddManualStep("Configure backend services in Traefik dynamic configuration")
	result.AddManualStep("Place SSL certificates in ./certs directory")
	result.AddManualStep("Update routing rules and path-based routing as needed")
	result.AddManualStep("Configure custom error pages if required")

	return result, nil
}

// handleFrontendIPs processes frontend IP configurations.
func (m *AppGatewayMapper) handleFrontendIPs(frontendIPs interface{}, result *mapper.MappingResult) {
	if ipSlice, ok := frontendIPs.([]interface{}); ok {
		for _, ip := range ipSlice {
			if ipMap, ok := ip.(map[string]interface{}); ok {
				name, _ := ipMap["name"].(string)
				publicIP, _ := ipMap["public_ip_address_id"].(string)
				privateIP, _ := ipMap["private_ip_address"].(string)

				if publicIP != "" {
					result.AddWarning(fmt.Sprintf("Frontend IP '%s' uses public IP. Update DNS to point to your Docker host.", name))
				}
				if privateIP != "" {
					result.AddWarning(fmt.Sprintf("Frontend IP '%s' has private IP %s. Ensure Docker host is accessible on this network.", name, privateIP))
				}
			}
		}
	}
}

// handleWAFConfig processes WAF configuration.
func (m *AppGatewayMapper) handleWAFConfig(wafConfig interface{}, result *mapper.MappingResult) {
	if wafMap, ok := wafConfig.(map[string]interface{}); ok {
		enabled, _ := wafMap["enabled"].(bool)
		mode, _ := wafMap["firewall_mode"].(string)
		ruleSetType, _ := wafMap["rule_set_type"].(string)
		ruleSetVersion, _ := wafMap["rule_set_version"].(string)

		if enabled {
			result.AddWarning(fmt.Sprintf("WAF is enabled in %s mode with rule set %s v%s", mode, ruleSetType, ruleSetVersion))
			result.AddWarning("Consider deploying ModSecurity with OWASP Core Rule Set as a WAF replacement")
			result.AddManualStep("Set up ModSecurity WAF container if web application firewall is required")
		}
	}
}

// generateTraefikConfig generates Traefik dynamic configuration.
func (m *AppGatewayMapper) generateTraefikConfig(gwName string, backends []interface{}, httpSettings []interface{}, listeners []interface{}, rules []interface{}, pathMaps []interface{}) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Traefik dynamic configuration for Azure Application Gateway: %s\n\n", gwName))
	sb.WriteString("http:\n")
	sb.WriteString("  routers:\n")

	// Generate routers based on routing rules
	if len(rules) > 0 {
		for i, rule := range rules {
			if ruleMap, ok := rule.(map[string]interface{}); ok {
				name, _ := ruleMap["name"].(string)
				ruleType, _ := ruleMap["rule_type"].(string)
				priority, _ := ruleMap["priority"].(float64)

				routerName := name
				if routerName == "" {
					routerName = fmt.Sprintf("router-%d", i)
				}

				sb.WriteString(fmt.Sprintf("    %s:\n", m.sanitizeName(routerName)))

				if ruleType == "PathBasedRouting" {
					sb.WriteString("      rule: \"PathPrefix(`/api`) || PathPrefix(`/app`)\"\n")
				} else {
					sb.WriteString("      rule: \"PathPrefix(`/`)\"\n")
				}

				sb.WriteString(fmt.Sprintf("      service: service-%d\n", i))
				if priority > 0 {
					sb.WriteString(fmt.Sprintf("      priority: %d\n", int(priority)))
				}
				sb.WriteString("      entryPoints:\n")
				sb.WriteString("        - web\n")
				sb.WriteString("        - websecure\n")
				sb.WriteString("      middlewares:\n")
				sb.WriteString("        - compress\n")
				sb.WriteString("        - secure-headers\n")
				sb.WriteString("\n")
			}
		}
	} else {
		// Default router
		sb.WriteString("    default-router:\n")
		sb.WriteString("      rule: \"PathPrefix(`/`)\"\n")
		sb.WriteString("      service: default-service\n")
		sb.WriteString("      entryPoints:\n")
		sb.WriteString("        - web\n")
		sb.WriteString("        - websecure\n\n")
	}

	sb.WriteString("  services:\n")

	// Generate services based on backend pools
	if len(backends) > 0 {
		for i, backend := range backends {
			if backendMap, ok := backend.(map[string]interface{}); ok {
				name, _ := backendMap["name"].(string)
				serviceName := name
				if serviceName == "" {
					serviceName = fmt.Sprintf("service-%d", i)
				}

				sb.WriteString(fmt.Sprintf("    %s:\n", m.sanitizeName(serviceName)))
				sb.WriteString("      loadBalancer:\n")
				sb.WriteString("        servers:\n")
				sb.WriteString("          # Configure your backend servers here\n")
				sb.WriteString("          - url: \"http://backend:8080\"\n")
				sb.WriteString("        healthCheck:\n")
				sb.WriteString("          path: \"/health\"\n")
				sb.WriteString("          interval: \"10s\"\n")
				sb.WriteString("          timeout: \"5s\"\n")
				sb.WriteString("\n")
			}
		}
	} else {
		sb.WriteString("    default-service:\n")
		sb.WriteString("      loadBalancer:\n")
		sb.WriteString("        servers:\n")
		sb.WriteString("          - url: \"http://backend:8080\"\n\n")
	}

	return sb.String()
}

// generateMiddlewareConfig generates Traefik middleware configuration.
func (m *AppGatewayMapper) generateMiddlewareConfig(gwName string) string {
	return fmt.Sprintf(`# Traefik middleware configuration for Application Gateway: %s

http:
  middlewares:
    compress:
      compress: {}

    secure-headers:
      headers:
        sslRedirect: true
        stsSeconds: 31536000
        stsIncludeSubdomains: true
        stsPreload: true
        forceSTSHeader: true
        frameDeny: true
        contentTypeNosniff: true
        browserXssFilter: true
        referrerPolicy: "same-origin"

    rate-limit:
      rateLimit:
        average: 100
        burst: 50

    retry:
      retry:
        attempts: 3
        initialInterval: 100ms
`, gwName)
}

// generateSetupScript generates a setup script.
func (m *AppGatewayMapper) generateSetupScript(gwName string) string {
	return fmt.Sprintf(`#!/bin/bash
# Setup script for Azure Application Gateway: %s

set -e

echo "Creating Traefik configuration directory..."
mkdir -p ./config/traefik
mkdir -p ./certs

echo "Starting Traefik application gateway..."
docker-compose up -d %s

echo "Waiting for Traefik to be ready..."
sleep 10

echo ""
echo "Traefik dashboard available at: http://localhost:8080"
echo "Application Gateway listening on ports 80 (HTTP) and 443 (HTTPS)"
echo ""
echo "Next steps:"
echo "1. Configure backend services in config/traefik/appgateway-config.yml"
echo "2. Place SSL certificates in ./certs directory"
echo "3. Update routing rules in the dynamic configuration"
echo "4. Start your backend services"
echo "5. Test routing: curl http://localhost/your-path"
`, gwName, m.sanitizeName(gwName))
}

// sanitizeName creates a valid Docker service name.
func (m *AppGatewayMapper) sanitizeName(name string) string {
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
		validName = "azure-appgw"
	}
	return validName
}
