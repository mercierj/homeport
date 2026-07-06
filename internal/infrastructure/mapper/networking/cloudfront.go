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

// CloudFrontMapper converts AWS CloudFront distributions to Caddy or nginx as CDN.
type CloudFrontMapper struct {
	*mapper.BaseMapper
}

// NewCloudFrontMapper creates a new CloudFront to Caddy/nginx mapper.
func NewCloudFrontMapper() *CloudFrontMapper {
	return &CloudFrontMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeCloudFront, nil),
	}
}

// Map converts a CloudFront distribution to a Caddy service (with nginx as alternative).
func (m *CloudFrontMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	distributionID := res.ID
	if distributionID == "" {
		distributionID = res.Name
	}

	// Use Caddy as the primary CDN/reverse proxy (easier configuration than nginx)
	result := mapper.NewMappingResult("caddy")
	svc := result.DockerService

	// Configure Caddy service
	svc.Image = "caddy:2.7-alpine"
	svc.Ports = []string{
		"80:80",
		"443:443",
		"443:443/udp", // HTTP/3
	}
	svc.Volumes = []string{
		"./config/caddy/Caddyfile:/etc/caddy/Caddyfile",
		"./config/caddy/site:/srv",
		"caddy-data:/data",
		"caddy-config:/config",
	}
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Labels = map[string]string{
		"homeport.source":          "aws_cloudfront",
		"homeport.distribution_id": distributionID,
		// Traefik integration if used alongside
		"traefik.enable": "false", // Caddy handles its own routing
	}
	svc.Restart = "unless-stopped"

	// Add health check
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:80/health"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  3,
	}

	result.AddService(&mapper.DockerService{
		Name:    "varnish",
		Image:   "varnish:7.5-alpine",
		Command: []string{"varnishd", "-F", "-f", "/etc/varnish/default.vcl", "-s", "malloc,512m"},
		Volumes: []string{
			"./config/varnish/default.vcl:/etc/varnish/default.vcl:ro",
			"varnish-cache:/var/lib/varnish",
		},
		Networks: []string{"homeport"},
		HealthCheck: &mapper.HealthCheck{
			Test:     []string{"CMD", "varnishadm", "ping"},
			Interval: 30 * time.Second,
			Timeout:  10 * time.Second,
			Retries:  3,
		},
		Deploy:  &mapper.DeployConfig{Replicas: 2},
		Restart: "unless-stopped",
	})

	// Add Caddy volumes
	result.AddVolume(mapper.Volume{
		Name:   "caddy-data",
		Driver: "local",
	})
	result.AddVolume(mapper.Volume{
		Name:   "caddy-config",
		Driver: "local",
	})
	result.AddVolume(mapper.Volume{
		Name:   "varnish-cache",
		Driver: "local",
	})

	// Generate Caddyfile configuration
	caddyfile := m.generateCaddyfile(res)
	result.AddConfig("config/caddy/Caddyfile", []byte(caddyfile))
	result.AddConfig("config/varnish/default.vcl", []byte(m.generateVarnishConfig(res)))
	result.AddConfig("config/cloudfront/dns-cutover.env", []byte(m.generateDNSCutoverEnv(res, distributionID)))
	result.AddConfig("config/cloudfront/validation.sh", []byte(m.generateValidationScript(res)))
	result.AddConfig("config/cloudfront/cache-behaviors.md", []byte(m.generateCacheConfig(res)))
	result.AddConfig("config/caddy/log-config.txt", []byte(m.generateLogConfig(res)))
	result.AddScript("backup_cloudfront_config.sh", []byte(m.generateBackupScript(distributionID)))

	// Handle origins
	if m.hasOrigins(res) {
		origins := m.extractOrigins(res)
		result.AddWarning(fmt.Sprintf("CloudFront origins mapped to Varnish backends: %s", strings.Join(origins, ", ")))
	}

	// Handle cache behaviors
	if m.hasCacheBehaviors(res) {
		result.AddWarning("CloudFront cache behaviors mapped to Varnish TTL rules.")
	}

	// Handle custom domains
	if m.hasCustomDomains(res) {
		domains := m.extractCustomDomains(res)
		result.AddWarning(fmt.Sprintf("Custom domains mapped to Caddy hosts and DNS cutover env: %s", strings.Join(domains, ", ")))
	}

	// Handle SSL/TLS certificates
	if m.hasSSLCertificate(res) {
		certARN := res.GetConfigString("viewer_certificate.acm_certificate_arn")
		if certARN != "" {
			result.AddWarning(fmt.Sprintf("ACM certificate detected: %s. Caddy manages replacement certificates for mapped hostnames.", certARN))
		}
	}

	// Handle geo-restrictions
	if m.hasGeoRestrictions(res) {
		result.AddWarning("CloudFront geo-restrictions detected. Generated CDN keeps routing explicit; enforce country controls at upstream auth or firewall.")
	}

	// Handle WAF integration
	if wafARN := res.GetConfigString("web_acl_id"); wafARN != "" {
		result.AddWarning(fmt.Sprintf("AWS WAF integration detected: %s. Preserve policy in generated app-change report or upstream WAF.", wafARN))
	}

	// Handle Lambda@Edge functions
	if m.hasLambdaEdge(res) {
		result.AddWarning("Lambda@Edge associations detected. Generated CDN preserves routing and emits an app-change report for edge code.")
	}

	// Handle logging
	if m.hasLogging(res) {
		result.AddWarning("CloudFront logging mapped to Caddy JSON access logs.")
	}

	// Add warning about CloudFront edge locations
	result.AddWarning("CloudFront edge locations provide global CDN. Consider using external CDN (Cloudflare, Fastly) or multi-region Caddy deployment for global reach.")
	for _, step := range cloudFrontRunbook(distributionID) {
		result.AddRunbookStep(step)
	}
	for _, step := range netrunbook.Edge(distributionID, "aws_cloudfront_distribution") {
		result.AddRunbookStep(step)
	}

	return result, nil
}

// generateCaddyfile creates the Caddy configuration file.
func (m *CloudFrontMapper) generateCaddyfile(res *resource.AWSResource) string {
	hosts := m.extractCustomDomains(res)
	site := ":80, :443"
	if len(hosts) > 0 {
		site = strings.Join(hosts, ", ")
	}

	return fmt.Sprintf(`# Caddyfile - Generated from AWS CloudFront Distribution
# See: https://caddyserver.com/docs/caddyfile

{
	admin 0.0.0.0:2019
	auto_https on

	servers {
		protocol {
			experimental_http3
		}
	}
}

%s {
	encode gzip zstd

	header {
		Strict-Transport-Security "max-age=31536000; includeSubDomains; preload"
		X-Frame-Options "SAMEORIGIN"
		X-Content-Type-Options "nosniff"
		X-XSS-Protection "1; mode=block"
		-Server
	}

	handle /health {
		respond "OK" 200
	}

	handle /static/* {
		root * /srv
		file_server
		header Cache-Control "public, max-age=31536000, immutable"
	}

	reverse_proxy varnish:6081 {
		health_uri /health
		health_interval 10s
		health_timeout 5s
		lb_try_duration 5s
		lb_try_interval 250ms
		header_up X-Real-IP {remote_host}
		header_up X-Forwarded-For {remote_host}
		header_up X-Forwarded-Proto {scheme}
	}

	log {
		output file /var/log/caddy/access.log
		format json
		level INFO
	}
}
`, site)
}

// generateCacheConfig creates cache configuration documentation.
func (m *CloudFrontMapper) generateCacheConfig(res *resource.AWSResource) string {
	var b strings.Builder
	b.WriteString("# CloudFront Cache Behavior Mapping\n\n")
	b.WriteString("HomePort maps CloudFront cache behavior to Varnish VCL in `config/varnish/default.vcl`.\n\n")
	b.WriteString("| Path | Origin | TTL seconds |\n")
	b.WriteString("| --- | --- | --- |\n")
	for _, behavior := range m.cacheBehaviors(res) {
		b.WriteString(fmt.Sprintf("| `%s` | `%s` | `%d` |\n", behavior.pathPattern, behavior.targetOriginID, behavior.ttlSeconds))
	}
	return b.String()
}

// generateLogConfig creates logging configuration documentation.
func (m *CloudFrontMapper) generateLogConfig(res *resource.AWSResource) string {
	config := `# Logging Configuration

Caddy supports JSON structured logging.

Example configuration:
log {
    output file /var/log/caddy/access.log {
        roll_size 100mb
        roll_keep 10
        roll_keep_for 720h
    }
    format json
    level INFO
}

## Log Fields
Caddy automatically includes:
- Timestamp
- Client IP
- HTTP method and path
- Status code
- Response size
- User agent
- Referer

## CloudFront Standard Logs
Map CloudFront log fields to Caddy equivalents:
- c-ip -> remote_host
- cs-method -> method
- cs-uri-stem -> uri
- sc-status -> status
- time-taken -> duration

## Integration
Consider shipping logs to:
- Elasticsearch/OpenSearch
- Loki
- CloudWatch Logs (if keeping AWS for logging)
`

	return config
}

func (m *CloudFrontMapper) generateVarnishConfig(res *resource.AWSResource) string {
	origins := m.originConfigs(res)
	if len(origins) == 0 {
		origins = []cloudFrontOrigin{{id: "default", domain: "backend", port: 8080}}
	}

	var b strings.Builder
	b.WriteString("vcl 4.1;\n\n")
	for _, origin := range origins {
		b.WriteString(fmt.Sprintf("backend %s_origin {\n", sanitizeCloudFrontName(origin.id)))
		b.WriteString(fmt.Sprintf("    .host = \"%s\";\n", origin.domain))
		b.WriteString(fmt.Sprintf("    .port = \"%d\";\n", origin.port))
		b.WriteString("    .connect_timeout = 5s;\n")
		b.WriteString("    .first_byte_timeout = 30s;\n")
		b.WriteString("    .between_bytes_timeout = 10s;\n")
		b.WriteString("}\n\n")
	}

	defaultOrigin := m.defaultOriginID(res, origins[0].id)
	b.WriteString("sub vcl_recv {\n")
	b.WriteString("    if (req.url == \"/health\") {\n")
	b.WriteString("        return (synth(200, \"OK\"));\n")
	b.WriteString("    }\n")
	for _, behavior := range m.cacheBehaviors(res) {
		if behavior.pathPattern == "*" || behavior.pathPattern == "/*" {
			continue
		}
		prefix := strings.TrimSuffix(strings.TrimSuffix(behavior.pathPattern, "*"), "/")
		if prefix == "" {
			continue
		}
		b.WriteString(fmt.Sprintf("    if (req.url ~ \"^%s\") {\n", prefix))
		b.WriteString(fmt.Sprintf("        set req.backend_hint = %s_origin;\n", sanitizeCloudFrontName(behavior.targetOriginID)))
		b.WriteString("        return (hash);\n")
		b.WriteString("    }\n")
	}
	b.WriteString(fmt.Sprintf("    set req.backend_hint = %s_origin;\n", sanitizeCloudFrontName(defaultOrigin)))
	b.WriteString("    return (hash);\n")
	b.WriteString("}\n\n")

	b.WriteString("sub vcl_backend_response {\n")
	for _, behavior := range m.cacheBehaviors(res) {
		if behavior.pathPattern == "*" || behavior.pathPattern == "/*" {
			continue
		}
		prefix := strings.TrimSuffix(strings.TrimSuffix(behavior.pathPattern, "*"), "/")
		if prefix == "" {
			continue
		}
		b.WriteString(fmt.Sprintf("    if (bereq.url ~ \"^%s\") {\n", prefix))
		b.WriteString(fmt.Sprintf("        set beresp.ttl = %ds;\n", behavior.ttlSeconds))
		b.WriteString("        return (deliver);\n")
		b.WriteString("    }\n")
	}
	defaultTTL := 300
	if behaviors := m.cacheBehaviors(res); len(behaviors) > 0 {
		defaultTTL = behaviors[0].ttlSeconds
	}
	b.WriteString(fmt.Sprintf("    set beresp.ttl = %ds;\n", defaultTTL))
	b.WriteString("    return (deliver);\n")
	b.WriteString("}\n\n")
	b.WriteString("sub vcl_synth {\n")
	b.WriteString("    set resp.http.Content-Type = \"text/plain\";\n")
	b.WriteString("    return (deliver);\n")
	b.WriteString("}\n")
	return b.String()
}

func (m *CloudFrontMapper) generateDNSCutoverEnv(res *resource.AWSResource, distributionID string) string {
	domains := m.extractCustomDomains(res)
	if len(domains) == 0 {
		domains = []string{res.Name}
	}
	return fmt.Sprintf("SOURCE_DISTRIBUTION_ID=%s\nSOURCE_DISTRIBUTION_DOMAIN=%s\nTARGET_CDN_ENDPOINT=${HOMEPORT_CDN_ENDPOINT:-127.0.0.1}\nCUTOVER_HOSTS=%s\nROLLBACK_HOST=%s\n", distributionID, res.GetConfigString("domain_name"), strings.Join(domains, ","), res.GetConfigString("domain_name"))
}

func (m *CloudFrontMapper) generateValidationScript(res *resource.AWSResource) string {
	domains := m.extractCustomDomains(res)
	if len(domains) == 0 {
		domains = []string{"localhost"}
	}
	var b strings.Builder
	b.WriteString("#!/bin/sh\nset -eu\n")
	b.WriteString("endpoint=\"${HOMEPORT_CDN_ENDPOINT:-127.0.0.1}\"\n")
	for _, domain := range domains {
		b.WriteString(fmt.Sprintf("curl -fsS -H 'Host: %s' \"http://$endpoint/health\" >/dev/null\n", domain))
	}
	b.WriteString("echo cloudfront-cdn-validation-ok\n")
	return b.String()
}

func (m *CloudFrontMapper) generateBackupScript(distributionID string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-cdn-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/caddy config/varnish config/cloudfront
echo "$archive"
`, sanitizeCloudFrontName(distributionID))
}

// hasOrigins checks if the CloudFront distribution has origins configured.
func (m *CloudFrontMapper) hasOrigins(res *resource.AWSResource) bool {
	if origins := res.Config["origin"]; origins != nil {
		return true
	}
	if origins := res.Config["origins"]; origins != nil {
		return true
	}
	return false
}

// extractOrigins extracts origin domain names from the distribution.
func (m *CloudFrontMapper) extractOrigins(res *resource.AWSResource) []string {
	var origins []string

	if originConfig := res.Config["origin"]; originConfig != nil {
		if originSlice, ok := originConfig.([]interface{}); ok {
			for _, o := range originSlice {
				if oMap, ok := o.(map[string]interface{}); ok {
					if domain, ok := oMap["domain_name"].(string); ok {
						origins = append(origins, domain)
					}
				}
			}
		}
	}
	for _, origin := range m.originConfigs(res) {
		if origin.domain != "" && !containsString(origins, origin.domain) {
			origins = append(origins, origin.domain)
		}
	}

	return origins
}

// hasCacheBehaviors checks if the CloudFront distribution has cache behaviors.
func (m *CloudFrontMapper) hasCacheBehaviors(res *resource.AWSResource) bool {
	if behaviors := res.Config["default_cache_behavior"]; behaviors != nil {
		return true
	}
	if ordered := res.Config["ordered_cache_behavior"]; ordered != nil {
		return true
	}
	return false
}

// hasCustomDomains checks if the CloudFront distribution has custom domains.
func (m *CloudFrontMapper) hasCustomDomains(res *resource.AWSResource) bool {
	if aliases := res.Config["aliases"]; aliases != nil {
		return true
	}
	return false
}

// extractCustomDomains extracts custom domain names from the distribution.
func (m *CloudFrontMapper) extractCustomDomains(res *resource.AWSResource) []string {
	var domains []string

	if aliases := res.Config["aliases"]; aliases != nil {
		if aliasSlice, ok := aliases.([]interface{}); ok {
			for _, a := range aliasSlice {
				if domain, ok := a.(string); ok {
					domains = append(domains, domain)
				}
			}
		}
	}

	return domains
}

// hasSSLCertificate checks if the CloudFront distribution has SSL certificate configured.
func (m *CloudFrontMapper) hasSSLCertificate(res *resource.AWSResource) bool {
	if cert := res.Config["viewer_certificate"]; cert != nil {
		return true
	}
	return false
}

// hasGeoRestrictions checks if the CloudFront distribution has geo-restrictions.
func (m *CloudFrontMapper) hasGeoRestrictions(res *resource.AWSResource) bool {
	if restrictions := res.Config["restrictions"]; restrictions != nil {
		if resMap, ok := restrictions.(map[string]interface{}); ok {
			if geoRestriction := resMap["geo_restriction"]; geoRestriction != nil {
				return true
			}
		}
	}
	return false
}

// hasLambdaEdge checks if the CloudFront distribution has Lambda@Edge functions.
func (m *CloudFrontMapper) hasLambdaEdge(res *resource.AWSResource) bool {
	if behavior := res.Config["default_cache_behavior"]; behavior != nil {
		if behaviorMap, ok := behavior.(map[string]interface{}); ok {
			if lambdaAssoc := behaviorMap["lambda_function_association"]; lambdaAssoc != nil {
				return true
			}
		}
	}
	return false
}

// hasLogging checks if the CloudFront distribution has logging enabled.
func (m *CloudFrontMapper) hasLogging(res *resource.AWSResource) bool {
	if logging := res.Config["logging_config"]; logging != nil {
		return true
	}
	return false
}

type cloudFrontOrigin struct {
	id     string
	domain string
	port   int
}

type cloudFrontBehavior struct {
	pathPattern    string
	targetOriginID string
	ttlSeconds     int
}

func (m *CloudFrontMapper) originConfigs(res *resource.AWSResource) []cloudFrontOrigin {
	var origins []cloudFrontOrigin
	add := func(raw map[string]interface{}) {
		id := stringFromAny(raw["id"])
		if id == "" {
			id = stringFromAny(raw["origin_id"])
		}
		domain := stringFromAny(raw["domain_name"])
		if domain == "" {
			return
		}
		if id == "" {
			id = domain
		}
		port := intFromAny(raw["http_port"], 8080)
		if port == 8080 && stringFromAny(raw["origin_protocol_policy"]) == "https-only" {
			port = intFromAny(raw["https_port"], 443)
		}
		origins = append(origins, cloudFrontOrigin{id: id, domain: domain, port: port})
	}
	for _, raw := range mapSlice(res.Config["origins"]) {
		add(raw)
	}
	for _, raw := range mapSlice(res.Config["origin"]) {
		add(raw)
	}
	return origins
}

func (m *CloudFrontMapper) cacheBehaviors(res *resource.AWSResource) []cloudFrontBehavior {
	defaultOrigin := m.defaultOriginID(res, "default")
	behaviors := []cloudFrontBehavior{{
		pathPattern:    "*",
		targetOriginID: defaultOrigin,
		ttlSeconds:     ttlFromMap(asMap(res.Config["default_cache_behavior"]), 300),
	}}
	for _, raw := range mapSlice(res.Config["ordered_cache_behavior"]) {
		target := stringFromAny(raw["target_origin_id"])
		if target == "" {
			target = defaultOrigin
		}
		behaviors = append(behaviors, cloudFrontBehavior{
			pathPattern:    defaultString(stringFromAny(raw["path_pattern"]), "*"),
			targetOriginID: target,
			ttlSeconds:     ttlFromMap(raw, 300),
		})
	}
	return behaviors
}

func (m *CloudFrontMapper) defaultOriginID(res *resource.AWSResource, fallback string) string {
	if behavior := asMap(res.Config["default_cache_behavior"]); behavior != nil {
		if target := stringFromAny(behavior["target_origin_id"]); target != "" {
			return target
		}
	}
	return fallback
}

func cloudFrontRunbook(distributionID string) []domainrunbook.Step {
	return []domainrunbook.Step{
		{
			ID:               "render-cloudfront-cdn-config",
			Name:             "Render CloudFront CDN configuration",
			Type:             domainrunbook.StepTypeCommand,
			Status:           domainrunbook.StepStatusPending,
			Executor:         "docker compose config",
			Command:          []string{"docker", "compose", "config"},
			SuccessCondition: "Compose renders Caddy and Varnish services",
			Metadata:         map[string]string{"source": distributionID},
		},
		{
			ID:               "provision-caddy-varnish-cdn",
			Name:             "Provision Caddy and Varnish CDN",
			Type:             domainrunbook.StepTypeCommand,
			Status:           domainrunbook.StepStatusPending,
			Executor:         "docker compose up",
			Command:          []string{"docker", "compose", "up", "-d", "caddy", "varnish"},
			SuccessCondition: "Caddy and Varnish containers are healthy",
			Metadata:         map[string]string{"target": "caddy-varnish"},
		},
		{
			ID:               "validate-cdn-cache-routing",
			Name:             "Validate CDN cache routing",
			Type:             domainrunbook.StepTypeHealth,
			Status:           domainrunbook.StepStatusPending,
			Executor:         "sh",
			Command:          []string{"sh", "config/cloudfront/validation.sh"},
			SuccessCondition: "Validation script returns cloudfront-cdn-validation-ok",
		},
		{
			ID:               "backup-cloudfront-config",
			Name:             "Backup generated CDN configuration",
			Type:             domainrunbook.StepTypeCommand,
			Status:           domainrunbook.StepStatusPending,
			Executor:         "sh",
			Command:          []string{"sh", "backup_cloudfront_config.sh"},
			SuccessCondition: "Backup archive path is printed",
		},
		{
			ID:               "cutover-cloudfront-dns",
			Name:             "Cut over DNS to HomePort CDN endpoint",
			Type:             domainrunbook.StepTypeDNSCheck,
			Status:           domainrunbook.StepStatusPending,
			Executor:         "env",
			Command:          []string{"sh", "-c", ". config/cloudfront/dns-cutover.env && printf '%s\n' \"$CUTOVER_HOSTS\""},
			SuccessCondition: "CUTOVER_HOSTS resolves to TARGET_CDN_ENDPOINT",
		},
		{
			ID:               "rollback-cloudfront-dns",
			Name:             "Rollback DNS to CloudFront",
			Type:             domainrunbook.StepTypeRollback,
			Status:           domainrunbook.StepStatusPending,
			Executor:         "env",
			Command:          []string{"sh", "-c", ". config/cloudfront/dns-cutover.env && printf '%s\n' \"$ROLLBACK_HOST\""},
			SuccessCondition: "Aliases point back to the CloudFront distribution domain",
		},
	}
}

func mapSlice(value interface{}) []map[string]interface{} {
	switch typed := value.(type) {
	case []interface{}:
		var out []map[string]interface{}
		for _, item := range typed {
			if m, ok := item.(map[string]interface{}); ok {
				out = append(out, m)
			}
		}
		return out
	case []map[string]interface{}:
		return typed
	case map[string]interface{}:
		return []map[string]interface{}{typed}
	default:
		return nil
	}
}

func asMap(value interface{}) map[string]interface{} {
	if typed, ok := value.(map[string]interface{}); ok {
		return typed
	}
	return nil
}

func ttlFromMap(values map[string]interface{}, fallback int) int {
	for _, key := range []string{"default_ttl", "max_ttl", "min_ttl"} {
		if ttl := intFromAny(values[key], 0); ttl > 0 {
			return ttl
		}
	}
	return fallback
}

func stringFromAny(value interface{}) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", value)
}

func intFromAny(value interface{}, fallback int) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return fallback
	}
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func sanitizeCloudFrontName(value string) string {
	value = strings.ToLower(value)
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "default"
	}
	return out
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
