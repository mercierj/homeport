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

// FrontDoorMapper converts Azure Front Door to Traefik with caching.
type FrontDoorMapper struct {
	*mapper.BaseMapper
}

// NewFrontDoorMapper creates a new Azure Front Door to Traefik+Varnish mapper.
func NewFrontDoorMapper() *FrontDoorMapper {
	return &FrontDoorMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeFrontDoor, nil),
	}
}

// Map converts an Azure Front Door to a Traefik+Varnish combo service.
func (m *FrontDoorMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	fdName := res.GetConfigString("name")
	if fdName == "" {
		fdName = res.Name
	}

	result := mapper.NewMappingResult(m.sanitizeName(fdName))
	svc := result.DockerService

	// Use Traefik as the main reverse proxy
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
		"cloudexit.source":         "azurerm_frontdoor",
		"cloudexit.frontdoor_name": fdName,
	}

	// Health check for Traefik
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "traefik", "healthcheck", "--ping"},
		Interval: 10 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  3,
	}

	// Add Varnish cache service
	varnishService := m.createVarnishService(fdName)
	result.AddManualStep("Add varnish service to docker-compose (see config/varnish/varnish-compose.yml)")

	// Handle frontend endpoints
	if frontendEndpoints := res.Config["frontend_endpoint"]; frontendEndpoints != nil {
		m.handleFrontendEndpoints(frontendEndpoints, result)
	}

	// Handle backend pools
	var backendPools []interface{}
	if pools := res.Config["backend_pool"]; pools != nil {
		if poolSlice, ok := pools.([]interface{}); ok {
			backendPools = poolSlice
		}
	}

	// Handle routing rules
	var routingRules []interface{}
	if rules := res.Config["routing_rule"]; rules != nil {
		if ruleSlice, ok := rules.([]interface{}); ok {
			routingRules = ruleSlice
		}
	}

	// Handle backend pool settings
	if settings := res.Config["backend_pool_settings"]; settings != nil {
		if settingsSlice, ok := settings.([]interface{}); ok && len(settingsSlice) > 0 {
			result.AddWarning("Backend pool settings detected. Review timeout and connection settings.")
		}
	}

	// Handle backend pool health probes
	var healthProbes []interface{}
	if probes := res.Config["backend_pool_health_probe"]; probes != nil {
		if probeSlice, ok := probes.([]interface{}); ok {
			healthProbes = probeSlice
		}
	}

	// Handle backend pool load balancing
	if lb := res.Config["backend_pool_load_balancing"]; lb != nil {
		if lbSlice, ok := lb.([]interface{}); ok && len(lbSlice) > 0 {
			result.AddWarning("Load balancing settings detected. Configure similar behavior in Traefik.")
		}
	}

	// Generate Traefik dynamic configuration
	traefikConfig := m.generateTraefikConfig(fdName, backendPools, routingRules, healthProbes)
	result.AddConfig("config/traefik/frontdoor-config.yml", []byte(traefikConfig))

	// Generate Varnish VCL configuration
	varnishVCL := m.generateVarnishVCL(fdName, backendPools)
	result.AddConfig("config/varnish/default.vcl", []byte(varnishVCL))

	// Generate Varnish docker-compose
	result.AddConfig("config/varnish/varnish-compose.yml", []byte(varnishService))

	// Generate middleware configuration
	middlewareConfig := m.generateMiddlewareConfig(fdName)
	result.AddConfig("config/traefik/middleware.yml", []byte(middlewareConfig))

	// Generate setup script
	setupScript := m.generateSetupScript(fdName)
	result.AddScript("setup_frontdoor.sh", []byte(setupScript))

	result.AddWarning("Azure Front Door converted to Traefik + Varnish combination")
	result.AddWarning("Front Door's global routing is replaced with local load balancing")
	result.AddManualStep("Configure backend pools in Traefik dynamic configuration")
	result.AddManualStep("Place SSL certificates in ./certs directory")
	result.AddManualStep("Configure custom domains in routing rules")
	result.AddManualStep("Adjust cache policies in Varnish VCL")
	result.AddManualStep("Set up health probes for backend services")

	return result, nil
}

// handleFrontendEndpoints processes frontend endpoints.
func (m *FrontDoorMapper) handleFrontendEndpoints(endpoints interface{}, result *mapper.MappingResult) {
	if endpointSlice, ok := endpoints.([]interface{}); ok {
		for _, endpoint := range endpointSlice {
			if endpointMap, ok := endpoint.(map[string]interface{}); ok {
				name, _ := endpointMap["name"].(string)
				hostName, _ := endpointMap["host_name"].(string)

				result.AddWarning(fmt.Sprintf("Frontend endpoint '%s' with hostname %s. Update DNS to point to your server.", name, hostName))

				// Check for custom HTTPS configuration
				if customHttps := endpointMap["custom_https_configuration"]; customHttps != nil {
					result.AddManualStep("Configure SSL certificates for custom HTTPS")
				}
			}
		}
	}
}

// createVarnishService creates a Varnish service configuration.
func (m *FrontDoorMapper) createVarnishService(fdName string) string {
	return fmt.Sprintf(`# Varnish cache service for Azure Front Door: %s

varnish:
  image: varnish:7.4
  container_name: varnish-cache
  command:
    - varnishd
    - -F
    - -f
    - /etc/varnish/default.vcl
    - -s
    - malloc,512m
    - -a
    - :80
  ports:
    - "6081:80"
    - "6082:6082"
  volumes:
    - ./config/varnish:/etc/varnish:ro
  networks:
    - cloudexit
  restart: unless-stopped
  labels:
    cloudexit.source: azurerm_frontdoor
    cloudexit.component: cache
  healthcheck:
    test: ["CMD-SHELL", "varnishadm ping || exit 1"]
    interval: 30s
    timeout: 5s
    retries: 3
`, fdName)
}

// generateTraefikConfig generates Traefik dynamic configuration.
func (m *FrontDoorMapper) generateTraefikConfig(fdName string, backendPools []interface{}, routingRules []interface{}, healthProbes []interface{}) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Traefik dynamic configuration for Azure Front Door: %s\n\n", fdName))
	sb.WriteString("http:\n")
	sb.WriteString("  routers:\n")

	// Generate routers based on routing rules
	if len(routingRules) > 0 {
		for i, rule := range routingRules {
			if ruleMap, ok := rule.(map[string]interface{}); ok {
				name, _ := ruleMap["name"].(string)
				acceptedProtocols, _ := ruleMap["accepted_protocols"].([]interface{})
				patternsToMatch, _ := ruleMap["patterns_to_match"].([]interface{})

				routerName := name
				if routerName == "" {
					routerName = fmt.Sprintf("router-%d", i)
				}

				sb.WriteString(fmt.Sprintf("    %s:\n", m.sanitizeName(routerName)))

				// Build routing rule
				if len(patternsToMatch) > 0 {
					patterns := make([]string, 0)
					for _, pattern := range patternsToMatch {
						if p, ok := pattern.(string); ok {
							patterns = append(patterns, p)
						}
					}
					if len(patterns) > 0 {
						sb.WriteString(fmt.Sprintf("      rule: \"PathPrefix(`%s`)\"\n", patterns[0]))
					}
				} else {
					sb.WriteString("      rule: \"PathPrefix(`/`)\"\n")
				}

				sb.WriteString("      service: varnish-cache\n")
				sb.WriteString("      entryPoints:\n")

				// Handle protocols
				hasHTTPS := false
				if len(acceptedProtocols) > 0 {
					for _, proto := range acceptedProtocols {
						if p, ok := proto.(string); ok && strings.ToLower(p) == "https" {
							hasHTTPS = true
						}
					}
				}

				sb.WriteString("        - web\n")
				if hasHTTPS {
					sb.WriteString("        - websecure\n")
				}

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
		sb.WriteString("      service: varnish-cache\n")
		sb.WriteString("      entryPoints:\n")
		sb.WriteString("        - web\n")
		sb.WriteString("        - websecure\n\n")
	}

	sb.WriteString("  services:\n")
	sb.WriteString("    varnish-cache:\n")
	sb.WriteString("      loadBalancer:\n")
	sb.WriteString("        servers:\n")
	sb.WriteString("          - url: \"http://varnish:80\"\n")
	sb.WriteString("        healthCheck:\n")
	sb.WriteString("          path: \"/\"\n")
	sb.WriteString("          interval: \"10s\"\n")
	sb.WriteString("          timeout: \"5s\"\n\n")

	// Generate backend services
	if len(backendPools) > 0 {
		for i, pool := range backendPools {
			if poolMap, ok := pool.(map[string]interface{}); ok {
				name, _ := poolMap["name"].(string)
				serviceName := name
				if serviceName == "" {
					serviceName = fmt.Sprintf("backend-%d", i)
				}

				sb.WriteString(fmt.Sprintf("    %s:\n", m.sanitizeName(serviceName)))
				sb.WriteString("      loadBalancer:\n")
				sb.WriteString("        servers:\n")
				sb.WriteString("          # Configure your backend servers here\n")
				sb.WriteString("          - url: \"http://backend:8080\"\n\n")
			}
		}
	}

	return sb.String()
}

// generateVarnishVCL generates Varnish VCL configuration.
func (m *FrontDoorMapper) generateVarnishVCL(fdName string, backendPools []interface{}) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Varnish VCL for Azure Front Door: %s\n\n", fdName))
	sb.WriteString("vcl 4.1;\n\n")

	sb.WriteString("import std;\n\n")

	sb.WriteString("# Backend configuration\n")
	sb.WriteString("backend default {\n")
	sb.WriteString("    .host = \"backend\";\n")
	sb.WriteString("    .port = \"8080\";\n")
	sb.WriteString("    .probe = {\n")
	sb.WriteString("        .url = \"/health\";\n")
	sb.WriteString("        .interval = 5s;\n")
	sb.WriteString("        .timeout = 2s;\n")
	sb.WriteString("        .window = 5;\n")
	sb.WriteString("        .threshold = 3;\n")
	sb.WriteString("    }\n")
	sb.WriteString("}\n\n")

	sb.WriteString("sub vcl_recv {\n")
	sb.WriteString("    # Remove cookies for static content\n")
	sb.WriteString("    if (req.url ~ \"\\.(jpg|jpeg|png|gif|ico|css|js|svg|woff|woff2)$\") {\n")
	sb.WriteString("        unset req.http.Cookie;\n")
	sb.WriteString("    }\n\n")

	sb.WriteString("    # Normalize headers\n")
	sb.WriteString("    if (req.http.Accept-Encoding) {\n")
	sb.WriteString("        if (req.url ~ \"\\.(jpg|jpeg|png|gif|gz|tgz|bz2|tbz|mp3|ogg|swf)$\") {\n")
	sb.WriteString("            unset req.http.Accept-Encoding;\n")
	sb.WriteString("        } elsif (req.http.Accept-Encoding ~ \"gzip\") {\n")
	sb.WriteString("            set req.http.Accept-Encoding = \"gzip\";\n")
	sb.WriteString("        } elsif (req.http.Accept-Encoding ~ \"deflate\") {\n")
	sb.WriteString("            set req.http.Accept-Encoding = \"deflate\";\n")
	sb.WriteString("        } else {\n")
	sb.WriteString("            unset req.http.Accept-Encoding;\n")
	sb.WriteString("        }\n")
	sb.WriteString("    }\n")
	sb.WriteString("}\n\n")

	sb.WriteString("sub vcl_backend_response {\n")
	sb.WriteString("    # Set cache TTL\n")
	sb.WriteString("    if (beresp.status == 200) {\n")
	sb.WriteString("        set beresp.ttl = 1h;\n")
	sb.WriteString("    }\n\n")

	sb.WriteString("    # Long TTL for static content\n")
	sb.WriteString("    if (bereq.url ~ \"\\.(jpg|jpeg|png|gif|ico|css|js|svg|woff|woff2)$\") {\n")
	sb.WriteString("        set beresp.ttl = 24h;\n")
	sb.WriteString("    }\n\n")

	sb.WriteString("    # Enable compression\n")
	sb.WriteString("    if (beresp.http.content-type ~ \"text|javascript|json|xml\") {\n")
	sb.WriteString("        set beresp.do_gzip = true;\n")
	sb.WriteString("    }\n")
	sb.WriteString("}\n\n")

	sb.WriteString("sub vcl_deliver {\n")
	sb.WriteString("    # Add cache status header\n")
	sb.WriteString("    if (obj.hits > 0) {\n")
	sb.WriteString("        set resp.http.X-Cache = \"HIT\";\n")
	sb.WriteString("    } else {\n")
	sb.WriteString("        set resp.http.X-Cache = \"MISS\";\n")
	sb.WriteString("    }\n")
	sb.WriteString("}\n")

	return sb.String()
}

// generateMiddlewareConfig generates Traefik middleware configuration.
func (m *FrontDoorMapper) generateMiddlewareConfig(fdName string) string {
	return fmt.Sprintf(`# Traefik middleware for Azure Front Door: %s

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
`, fdName)
}

// generateSetupScript generates a setup script.
func (m *FrontDoorMapper) generateSetupScript(fdName string) string {
	return fmt.Sprintf(`#!/bin/bash
# Setup script for Azure Front Door: %s

set -e

echo "Creating configuration directories..."
mkdir -p ./config/traefik
mkdir -p ./config/varnish
mkdir -p ./certs

echo "Starting Traefik and Varnish..."
docker-compose up -d %s
docker-compose -f config/varnish/varnish-compose.yml up -d

echo "Waiting for services to be ready..."
sleep 10

echo ""
echo "Front Door replacement is running!"
echo "Traefik dashboard: http://localhost:8080"
echo "HTTP/HTTPS: http://localhost:80 / https://localhost:443"
echo "Varnish cache: http://localhost:6081"
echo ""
echo "Next steps:"
echo "1. Configure backend pools in config/traefik/frontdoor-config.yml"
echo "2. Place SSL certificates in ./certs directory"
echo "3. Configure routing rules and custom domains"
echo "4. Adjust Varnish cache settings in config/varnish/default.vcl"
echo "5. Start your backend services"
echo "6. Test routing and caching"
`, fdName, m.sanitizeName(fdName))
}

// sanitizeName creates a valid Docker service name.
func (m *FrontDoorMapper) sanitizeName(name string) string {
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
		validName = "frontdoor"
	}
	return validName
}
