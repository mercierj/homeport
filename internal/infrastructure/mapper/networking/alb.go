// Package networking provides mappers for AWS networking services.
package networking

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

// ALBMapper converts AWS Application Load Balancers to Traefik.
type ALBMapper struct {
	*mapper.BaseMapper
}

// NewALBMapper creates a new ALB to Traefik mapper.
func NewALBMapper() *ALBMapper {
	return &ALBMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeALB, nil),
	}
}

// Map converts an Application Load Balancer to a Traefik service.
func (m *ALBMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	lbName := res.Config["name"]
	if lbName == nil {
		lbName = res.Name
	}
	lbNameStr := fmt.Sprintf("%v", lbName)

	// Create result using new API
	result := mapper.NewMappingResult("traefik")
	svc := result.DockerService

	// Configure Traefik service
	svc.Image = "traefik:v3.0"
	svc.Ports = []string{
		"80:80",     // HTTP
		"443:443",   // HTTPS
		"8080:8080", // Dashboard
	}
	svc.Volumes = []string{
		"/var/run/docker.sock:/var/run/docker.sock:ro",
		"./config/traefik:/etc/traefik",
		"./config/traefik/certs:/certs",
	}
	svc.Command = []string{
		"--api.dashboard=true",
		"--providers.docker=true",
		"--providers.docker.exposedbydefault=false",
		"--providers.file.directory=/etc/traefik/dynamic",
		"--entrypoints.web.address=:80",
		"--entrypoints.websecure.address=:443",
		"--log.level=INFO",
		"--accesslog=true",
	}
	svc.Networks = []string{"homeport"}
	svc.Labels = map[string]string{
		"homeport.source":  "aws_lb",
		"homeport.lb_name": lbNameStr,
		"homeport.lb_type": "application",
		// Traefik dashboard
		"traefik.enable":                            "true",
		"traefik.http.routers.dashboard.rule":       "Host(`traefik.localhost`)",
		"traefik.http.routers.dashboard.service":    "api@internal",
		"traefik.http.routers.dashboard.entrypoints": "web",
	}
	svc.Restart = "unless-stopped"

	// Add health check
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "traefik", "healthcheck", "--ping"},
		Interval: 10 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  3,
	}

	// Generate Traefik configuration
	traefikConfig := m.generateTraefikConfig(res, lbNameStr)
	result.AddConfig("config/traefik/traefik.yml", []byte(traefikConfig))

	// Generate dynamic configuration for routes
	dynamicConfig := m.generateDynamicConfig(res, lbNameStr)
	result.AddConfig("config/traefik/dynamic/config.yml", []byte(dynamicConfig))

	// Handle SSL/TLS certificates
	if m.hasHTTPSListener(res) {
		result.AddManualStep("Configure SSL certificates in Traefik")
		result.AddManualStep("Place certificates in ./config/traefik/certs/ directory")
		result.AddManualStep("Update certificate paths in traefik.yml")

		certConfig := m.generateCertificateConfig(res, lbNameStr)
		result.AddConfig("config/traefik/dynamic/certs.yml", []byte(certConfig))

		result.AddWarning("SSL certificates from ACM need to be exported and configured in Traefik")
	}

	// Handle access logs
	if res.GetConfigBool("enable_http2") {
		result.AddWarning("HTTP/2 is enabled. Traefik supports HTTP/2 by default.")
	}

	// Handle WAF
	if wafArn := res.GetConfigString("web_acl_arn"); wafArn != "" {
		result.AddWarning("AWS WAF is configured. Consider using Traefik middleware or external WAF for equivalent protection.")
		result.AddManualStep("Review WAF rules and implement equivalent protection using Traefik middleware")
	}

	result.AddManualStep("Review and update target group backends in dynamic configuration")
	result.AddManualStep("Configure health check endpoints for all services")
	result.AddManualStep("Test load balancing behavior")

	return result, nil
}

// generateTraefikConfig creates the main Traefik configuration file.
func (m *ALBMapper) generateTraefikConfig(res *resource.AWSResource, lbName string) string {
	config := `# Traefik Static Configuration
# Generated from AWS Application Load Balancer

api:
  dashboard: true
  insecure: true  # Change to false in production

log:
  level: INFO
  format: json

accessLog:
  filePath: "/var/log/traefik/access.log"
  format: json

providers:
  docker:
    endpoint: "unix:///var/run/docker.sock"
    exposedByDefault: false
    network: homeport
  file:
    directory: "/etc/traefik/dynamic"
    watch: true

entryPoints:
  web:
    address: ":80"
`

	if m.hasHTTPSListener(res) {
		config += `    http:
      redirections:
        entryPoint:
          to: websecure
          scheme: https

  websecure:
    address: ":443"
    http:
      tls:
        certResolver: letsencrypt
`
	}

	// Add health check
	config += `
ping:
  entryPoint: "web"
`

	return config
}

// generateDynamicConfig creates the dynamic Traefik configuration with routes.
func (m *ALBMapper) generateDynamicConfig(res *resource.AWSResource, lbName string) string {
	config := `# Traefik Dynamic Configuration
# Generated from AWS Application Load Balancer listeners and rules

http:
  routers:
`

	// Parse listener rules
	// Note: In a real implementation, you would fetch listener and target group
	// resources separately and parse their rules
	config += `    # TODO: Add routers based on ALB listener rules
    # Example:
    # app-router:
    #   rule: "Host(` + "`app.example.com`)" + `"
    #   service: app-service
    #   entryPoints:
    #     - websecure
    #   tls: {}

  services:
    # TODO: Add services based on ALB target groups
    # Example:
    # app-service:
    #   loadBalancer:
    #     servers:
    #       - url: "http://app:8080"
    #     healthCheck:
    #       path: "/health"
    #       interval: "10s"
    #       timeout: "3s"

  middlewares:
    # Common middlewares
    security-headers:
      headers:
        browserXssFilter: true
        contentTypeNosniff: true
        frameDeny: true
        sslRedirect: true
        stsIncludeSubdomains: true
        stsPreload: true
        stsSeconds: 31536000

    rate-limit:
      rateLimit:
        average: 100
        burst: 50
`

	return config
}

// generateCertificateConfig creates a certificate configuration template.
func (m *ALBMapper) generateCertificateConfig(res *resource.AWSResource, lbName string) string {
	config := `# TLS Certificate Configuration
# Place your SSL certificates in ./config/traefik/certs/

tls:
  certificates:
    # Example certificate configuration
    # - certFile: /certs/example.com.crt
    #   keyFile: /certs/example.com.key
    #   stores:
    #     - default

  stores:
    default:
      defaultCertificate:
        certFile: /certs/default.crt
        keyFile: /certs/default.key

  options:
    default:
      minVersion: VersionTLS12
      cipherSuites:
        - TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256
        - TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384
        - TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305
`

	return config
}

// hasHTTPSListener checks if the ALB has HTTPS listeners.
func (m *ALBMapper) hasHTTPSListener(res *resource.AWSResource) bool {
	// Check if any listener uses HTTPS (port 443)
	// In a real implementation, you would parse the listener resources

	// For now, check common attributes
	if listeners := res.Config["listener"]; listeners != nil {
		if listenerSlice, ok := listeners.([]interface{}); ok {
			for _, l := range listenerSlice {
				if lMap, ok := l.(map[string]interface{}); ok {
					if port, ok := lMap["port"].(float64); ok && port == 443 {
						return true
					}
					if port, ok := lMap["port"].(int); ok && port == 443 {
						return true
					}
					if protocol, ok := lMap["protocol"].(string); ok {
						if protocol == "HTTPS" || protocol == "TLS" {
							return true
						}
					}
				}
			}
		}
	}

	return false
}

// ListenerRule represents an ALB listener rule (for future use).
type ListenerRule struct {
	Priority   int
	Conditions []RuleCondition
	Actions    []RuleAction
}

// RuleCondition represents a rule condition.
type RuleCondition struct {
	Field  string
	Values []string
}

// RuleAction represents a rule action.
type RuleAction struct {
	Type            string
	TargetGroupArn  string
	RedirectConfig  map[string]interface{}
	FixedResponse   map[string]interface{}
}

// TargetGroup represents an ALB target group (for future use).
type TargetGroup struct {
	Name            string
	Port            int
	Protocol        string
	HealthCheckPath string
	HealthCheckPort int
	Targets         []Target
}

// Target represents a target in a target group.
type Target struct {
	ID   string
	Port int
}
