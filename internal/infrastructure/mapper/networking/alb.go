// Package networking provides mappers for AWS networking services.
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
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Labels = map[string]string{
		"homeport.source":  "aws_lb",
		"homeport.lb_name": lbNameStr,
		"homeport.lb_type": "application",
		// Traefik dashboard
		"traefik.enable":                             "true",
		"traefik.http.routers.dashboard.rule":        "Host(`traefik.localhost`)",
		"traefik.http.routers.dashboard.service":     "api@internal",
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
	dynamicConfig, managedRoutes := m.generateDynamicConfig(res, lbNameStr)
	result.AddConfig("config/traefik/dynamic/config.yml", []byte(dynamicConfig))
	result.AddScript("backup_alb_config.sh", []byte(m.generateBackupScript(lbNameStr)))

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

	if !managedRoutes {
		result.AddManualStep("Review and update target group backends in dynamic configuration")
		result.AddManualStep("Configure health check endpoints for all services")
		result.AddManualStep("Test load balancing behavior")
	}
	for _, step := range netrunbook.Routing(lbNameStr, "aws_lb") {
		result.AddRunbookStep(step)
	}
	for _, step := range albRunbook(lbNameStr) {
		result.AddRunbookStep(step)
	}

	return result, nil
}

func (m *ALBMapper) generateBackupScript(lbName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-traefik-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/traefik
echo "$archive"
`, sanitizeTraefikName(lbName))
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
func (m *ALBMapper) generateDynamicConfig(res *resource.AWSResource, lbName string) (string, bool) {
	routes := albRoutes(res)
	var b strings.Builder
	b.WriteString("# Traefik Dynamic Configuration\n")
	b.WriteString("# Generated from AWS Application Load Balancer listeners and rules\n\n")
	b.WriteString("http:\n")
	b.WriteString("  routers:\n")
	if len(routes) == 0 {
		b.WriteString("    # No listener rules with concrete target hosts were found.\n")
	} else {
		for _, route := range routes {
			b.WriteString(fmt.Sprintf("    %s-router:\n", route.name))
			b.WriteString(fmt.Sprintf("      rule: \"%s\"\n", route.rule))
			b.WriteString(fmt.Sprintf("      service: %s-service\n", route.name))
			b.WriteString("      entryPoints:\n")
			b.WriteString(fmt.Sprintf("        - %s\n", route.entryPoint))
			if route.entryPoint == "websecure" {
				b.WriteString("      tls: {}\n")
			}
			b.WriteString("      middlewares:\n")
			b.WriteString("        - security-headers\n")
			b.WriteString("        - rate-limit\n")
		}
	}

	b.WriteString("\n  services:\n")
	if len(routes) == 0 {
		b.WriteString("    # Add services after importing ALB target group hosts.\n")
	} else {
		for _, route := range routes {
			b.WriteString(fmt.Sprintf("    %s-service:\n", route.name))
			b.WriteString("      loadBalancer:\n")
			b.WriteString("        servers:\n")
			for _, host := range route.hosts {
				b.WriteString(fmt.Sprintf("          - url: \"%s://%s:%d\"\n", strings.ToLower(route.protocol), host, route.port))
			}
			if route.healthPath != "" {
				b.WriteString("        healthCheck:\n")
				b.WriteString(fmt.Sprintf("          path: \"%s\"\n", route.healthPath))
				b.WriteString("          interval: \"10s\"\n")
				b.WriteString("          timeout: \"3s\"\n")
			}
		}
	}

	b.WriteString(`
  middlewares:
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
`)

	return b.String(), len(routes) > 0
}

type albRoute struct {
	name       string
	rule       string
	entryPoint string
	protocol   string
	port       int
	hosts      []string
	healthPath string
}

func albRoutes(res *resource.AWSResource) []albRoute {
	listeners := configSlice(res.Config["listeners"])
	if len(listeners) == 0 {
		listeners = configSlice(res.Config["listener"])
	}
	routes := []albRoute{}
	for listenerIndex, listener := range listeners {
		listenerPort := configInt(listener["port"], 80)
		entryPoint := "web"
		if listenerPort == 443 || strings.EqualFold(configString(listener["protocol"]), "HTTPS") {
			entryPoint = "websecure"
		}
		for ruleIndex, rule := range configSlice(listener["rules"]) {
			hosts := configStrings(rule["target_group_hosts"])
			if len(hosts) == 0 {
				continue
			}
			targetName := configString(rule["target_group_name"])
			if targetName == "" {
				targetName = fmt.Sprintf("target-%d-%d", listenerIndex, ruleIndex)
			}
			backendPort := configInt(rule["target_group_port"], listenerPort)
			backendProtocol := configString(rule["target_group_protocol"])
			if backendProtocol == "" {
				backendProtocol = "HTTP"
			}
			routes = append(routes, albRoute{
				name:       sanitizeTraefikName(res.Name + "-" + targetName),
				rule:       traefikRule(rule),
				entryPoint: entryPoint,
				protocol:   backendProtocol,
				port:       backendPort,
				hosts:      hosts,
				healthPath: configString(rule["health_check_path"]),
			})
		}
	}
	return routes
}

func traefikRule(rule map[string]interface{}) string {
	parts := []string{}
	if host := configString(rule["host"]); host != "" {
		parts = append(parts, fmt.Sprintf("Host(`%s`)", host))
	}
	if path := configString(rule["path"]); path != "" {
		parts = append(parts, fmt.Sprintf("PathPrefix(`%s`)", path))
	}
	if len(parts) == 0 {
		return "PathPrefix(`/`)"
	}
	return strings.Join(parts, " && ")
}

func configSlice(value interface{}) []map[string]interface{} {
	switch typed := value.(type) {
	case []map[string]interface{}:
		return typed
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(typed))
		for _, item := range typed {
			if itemMap, ok := item.(map[string]interface{}); ok {
				out = append(out, itemMap)
			}
		}
		return out
	}
	return nil
}

func configStrings(value interface{}) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []interface{}:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if str := fmt.Sprintf("%v", item); str != "" {
				out = append(out, str)
			}
		}
		return out
	case string:
		if typed != "" {
			return []string{typed}
		}
	}
	return nil
}

func configString(value interface{}) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%v", value)
}

func configInt(value interface{}, fallback int) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	}
	return fallback
}

func sanitizeTraefikName(value string) string {
	value = strings.ToLower(value)
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func albRunbook(lbName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "routing", "name": lbName, "source": "aws_lb"}
	return []domainrunbook.Step{
		{
			ID:               "backup-traefik-config",
			Name:             "Backup Traefik routing config",
			Group:            "Backup",
			Type:             domainrunbook.StepTypeCommand,
			Status:           domainrunbook.StepStatusPending,
			Executor:         "shell",
			Command:          []string{"sh", "backup_alb_config.sh"},
			SuccessCondition: "Traefik static and dynamic config archive is created before cutover",
			Metadata:         metadata,
		},
		{
			ID:               "cutover-dns-to-traefik",
			Name:             "Cut over DNS to Traefik",
			Group:            "Cutover",
			Type:             domainrunbook.StepTypeDNSCheck,
			Status:           domainrunbook.StepStatusPending,
			Executor:         "dns",
			SuccessCondition: "ALB hostnames resolve to the HomePort Traefik endpoint and route probes pass",
			Metadata:         metadata,
		},
	}
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
	Type           string
	TargetGroupArn string
	RedirectConfig map[string]interface{}
	FixedResponse  map[string]interface{}
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
