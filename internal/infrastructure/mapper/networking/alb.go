// Package networking provides mappers for AWS networking services.
package networking

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
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
	svc.Networks = []string{"cloudexit"}
	svc.Labels = map[string]string{
		"cloudexit.source":  "aws_lb",
		"cloudexit.lb_name": lbNameStr,
		"cloudexit.lb_type": "application",
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
    network: cloudexit
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

// ListenerRule represents an ALB listener rule.
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

// parseListenerRules parses listener rules from the ALB resource.
func (m *ALBMapper) parseListenerRules(res *resource.AWSResource) []ListenerRule {
	var rules []ListenerRule

	// Note: In a real implementation, you would fetch listener rule resources
	// separately as they are typically separate Terraform resources

	return rules
}

// generateTraefikRouterFromRule converts an ALB listener rule to a Traefik router.
func (m *ALBMapper) generateTraefikRouterFromRule(rule ListenerRule, index int) string {
	routerName := fmt.Sprintf("rule-%d", index)
	routerConfig := fmt.Sprintf("    %s:\n", routerName)
	routerConfig += "      rule: \""

	// Convert conditions to Traefik rule syntax
	ruleExprs := []string{}
	for _, cond := range rule.Conditions {
		switch cond.Field {
		case "host-header":
			for _, value := range cond.Values {
				ruleExprs = append(ruleExprs, fmt.Sprintf("Host(`%s`)", value))
			}
		case "path-pattern":
			for _, value := range cond.Values {
				ruleExprs = append(ruleExprs, fmt.Sprintf("PathPrefix(`%s`)", value))
			}
		case "http-header":
			// More complex, would need header name and value
		case "http-request-method":
			for _, value := range cond.Values {
				ruleExprs = append(ruleExprs, fmt.Sprintf("Method(`%s`)", value))
			}
		}
	}

	routerConfig += strings.Join(ruleExprs, " && ")
	routerConfig += "\"\n"
	routerConfig += "      entryPoints:\n"
	routerConfig += "        - websecure\n"
	routerConfig += "      service: " + routerName + "-service\n"

	return routerConfig
}

// TargetGroup represents an ALB target group.
type TargetGroup struct {
	Name             string
	Port             int
	Protocol         string
	HealthCheckPath  string
	HealthCheckPort  int
	Targets          []Target
}

// Target represents a target in a target group.
type Target struct {
	ID   string
	Port int
}

// parseTargetGroups parses target groups from the ALB.
func (m *ALBMapper) parseTargetGroups(res *resource.AWSResource) []TargetGroup {
	var groups []TargetGroup

	// Note: In a real implementation, you would fetch target group resources
	// separately as they are typically separate Terraform resources

	return groups
}

// generateTraefikServiceFromTargetGroup converts a target group to a Traefik service.
func (m *ALBMapper) generateTraefikServiceFromTargetGroup(tg TargetGroup) string {
	serviceName := m.sanitizeName(tg.Name)
	serviceConfig := fmt.Sprintf("    %s:\n", serviceName)
	serviceConfig += "      loadBalancer:\n"
	serviceConfig += "        servers:\n"

	for _, target := range tg.Targets {
		serviceConfig += fmt.Sprintf("          - url: \"http://%s:%d\"\n", target.ID, target.Port)
	}

	if tg.HealthCheckPath != "" {
		serviceConfig += "        healthCheck:\n"
		serviceConfig += fmt.Sprintf("          path: \"%s\"\n", tg.HealthCheckPath)
		serviceConfig += "          interval: \"10s\"\n"
		serviceConfig += "          timeout: \"3s\"\n"
	}

	return serviceConfig
}

// sanitizeName sanitizes a name for use in Traefik configuration.
func (m *ALBMapper) sanitizeName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "_", "-")
	name = strings.ReplaceAll(name, " ", "-")

	validName := ""
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' {
			validName += string(ch)
		}
	}

	return validName
}
