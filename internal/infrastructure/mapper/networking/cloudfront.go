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
	svc.Networks = []string{"cloudexit"}
	svc.Labels = map[string]string{
		"cloudexit.source":          "aws_cloudfront",
		"cloudexit.distribution_id": distributionID,
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

	// Add Caddy volumes
	result.AddVolume(mapper.Volume{
		Name:   "caddy-data",
		Driver: "local",
	})
	result.AddVolume(mapper.Volume{
		Name:   "caddy-config",
		Driver: "local",
	})

	// Generate Caddyfile configuration
	caddyfile := m.generateCaddyfile(res)
	result.AddConfig("config/caddy/Caddyfile", []byte(caddyfile))

	// Generate nginx alternative configuration
	nginxConfig := m.generateNginxConfig(res)
	result.AddConfig("config/nginx/nginx.conf", []byte(nginxConfig))
	result.AddConfig("config/nginx/README.md", []byte("# Alternative nginx configuration\nIf you prefer nginx over Caddy, use this configuration instead.\n"))

	// Handle origins
	if m.hasOrigins(res) {
		origins := m.extractOrigins(res)
		result.AddManualStep(fmt.Sprintf("Configure backend origins: %s", strings.Join(origins, ", ")))
		result.AddWarning("CloudFront origins detected. Ensure backend services are accessible from Caddy/nginx.")
	}

	// Handle cache behaviors
	if m.hasCacheBehaviors(res) {
		result.AddWarning("CloudFront cache behaviors detected. Configure Caddy cache directives accordingly.")
		result.AddManualStep("Review CloudFront cache behaviors and implement equivalent Caddy caching rules")

		cacheConfig := m.generateCacheConfig(res)
		result.AddConfig("config/caddy/cache-config.txt", []byte(cacheConfig))
	}

	// Handle custom domains
	if m.hasCustomDomains(res) {
		domains := m.extractCustomDomains(res)
		result.AddWarning(fmt.Sprintf("Custom domains detected: %s", strings.Join(domains, ", ")))
		result.AddManualStep("Configure DNS records to point to your Caddy instance")
		result.AddManualStep("Caddy will automatically obtain SSL certificates via Let's Encrypt")
	}

	// Handle SSL/TLS certificates
	if m.hasSSLCertificate(res) {
		certARN := res.GetConfigString("viewer_certificate.acm_certificate_arn")
		result.AddWarning(fmt.Sprintf("ACM certificate detected: %s. Caddy will manage SSL automatically with Let's Encrypt.", certARN))
		result.AddManualStep("If you need to use custom certificates, place them in ./config/caddy/certs/")
	}

	// Handle geo-restrictions
	if m.hasGeoRestrictions(res) {
		result.AddWarning("CloudFront geo-restrictions detected. Consider using Caddy GeoIP module or configure at firewall level.")
		result.AddManualStep("Review geo-restriction settings and implement equivalent controls")
	}

	// Handle WAF integration
	if wafARN := res.GetConfigString("web_acl_id"); wafARN != "" {
		result.AddWarning(fmt.Sprintf("AWS WAF integration detected: %s. Configure Caddy security plugins or external WAF.", wafARN))
		result.AddManualStep("Review WAF rules and implement security controls in Caddy or reverse proxy")
	}

	// Handle Lambda@Edge functions
	if m.hasLambdaEdge(res) {
		result.AddWarning("Lambda@Edge functions detected. These need to be migrated to backend services or Caddy modules.")
		result.AddManualStep("Migrate Lambda@Edge functions to backend API endpoints or Caddy plugins")
	}

	// Handle logging
	if m.hasLogging(res) {
		result.AddWarning("CloudFront logging detected. Configure Caddy access logs.")
		logConfig := m.generateLogConfig(res)
		result.AddConfig("config/caddy/log-config.txt", []byte(logConfig))
	}

	result.AddManualStep("Review and test all cache policies and behaviors")
	result.AddManualStep("Configure monitoring and alerting for Caddy")
	result.AddManualStep("Test SSL/TLS configuration and HTTP/3 support")
	result.AddManualStep("Benchmark performance and adjust cache settings as needed")

	// Add warning about CloudFront edge locations
	result.AddWarning("CloudFront edge locations provide global CDN. Consider using external CDN (Cloudflare, Fastly) or multi-region Caddy deployment for global reach.")

	return result, nil
}

// generateCaddyfile creates the Caddy configuration file.
func (m *CloudFrontMapper) generateCaddyfile(res *resource.AWSResource) string {
	config := `# Caddyfile - Generated from AWS CloudFront Distribution
# See: https://caddyserver.com/docs/caddyfile

{
	# Global options
	admin 0.0.0.0:2019
	auto_https on

	# Enable HTTP/3
	servers {
		protocol {
			experimental_http3
		}
	}
}

# Default site configuration
# Replace 'localhost' with your actual domain
:80, :443 {
	# Enable compression
	encode gzip zstd

	# Security headers
	header {
		# Enable HSTS
		Strict-Transport-Security "max-age=31536000; includeSubDomains; preload"
		# Prevent clickjacking
		X-Frame-Options "SAMEORIGIN"
		# Prevent MIME sniffing
		X-Content-Type-Options "nosniff"
		# Enable XSS protection
		X-XSS-Protection "1; mode=block"
		# Remove server header
		-Server
	}

	# Health check endpoint
	handle /health {
		respond "OK" 200
	}

	# Static file serving
	handle /static/* {
		root * /srv
		file_server

		# Cache static assets
		header Cache-Control "public, max-age=31536000, immutable"
	}

	# Reverse proxy to backend origin
	# TODO: Configure your backend origin
	reverse_proxy /* {
		to http://backend:8080

		# Health checks
		health_uri /health
		health_interval 10s
		health_timeout 5s

		# Load balancing
		lb_policy round_robin

		# Retry failed requests
		lb_try_duration 5s
		lb_try_interval 250ms

		# Headers
		header_up Host {upstream_hostport}
		header_up X-Real-IP {remote_host}
		header_up X-Forwarded-For {remote_host}
		header_up X-Forwarded-Proto {scheme}
	}

	# Logging
	log {
		output file /var/log/caddy/access.log
		format json
		level INFO
	}
}

# Additional domain configurations
# Uncomment and configure as needed
# example.com {
#     reverse_proxy backend:8080
# }
`

	return config
}

// generateNginxConfig creates an alternative nginx configuration.
func (m *CloudFrontMapper) generateNginxConfig(res *resource.AWSResource) string {
	config := `# nginx.conf - Alternative to Caddy
# Generated from AWS CloudFront Distribution

user nginx;
worker_processes auto;
error_log /var/log/nginx/error.log warn;
pid /var/run/nginx.pid;

events {
    worker_connections 1024;
    use epoll;
}

http {
    include /etc/nginx/mime.types;
    default_type application/octet-stream;

    # Logging
    log_format main '$remote_addr - $remote_user [$time_local] "$request" '
                    '$status $body_bytes_sent "$http_referer" '
                    '"$http_user_agent" "$http_x_forwarded_for"';
    access_log /var/log/nginx/access.log main;

    # Performance
    sendfile on;
    tcp_nopush on;
    tcp_nodelay on;
    keepalive_timeout 65;
    types_hash_max_size 2048;

    # Compression
    gzip on;
    gzip_vary on;
    gzip_proxied any;
    gzip_comp_level 6;
    gzip_types text/plain text/css text/xml text/javascript
               application/json application/javascript application/xml+rss
               application/rss+xml font/truetype font/opentype
               application/vnd.ms-fontobject image/svg+xml;

    # Cache configuration
    proxy_cache_path /var/cache/nginx levels=1:2 keys_zone=cdn_cache:10m
                     max_size=1g inactive=60m use_temp_path=off;

    # Rate limiting
    limit_req_zone $binary_remote_addr zone=api_limit:10m rate=10r/s;

    # Default server
    server {
        listen 80 default_server;
        listen [::]:80 default_server;
        server_name _;

        # Security headers
        add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;
        add_header X-Frame-Options "SAMEORIGIN" always;
        add_header X-Content-Type-Options "nosniff" always;
        add_header X-XSS-Protection "1; mode=block" always;

        # Health check
        location /health {
            access_log off;
            return 200 "OK\n";
            add_header Content-Type text/plain;
        }

        # Static files
        location /static/ {
            alias /usr/share/nginx/html/;
            expires 1y;
            add_header Cache-Control "public, immutable";
        }

        # Reverse proxy to backend
        location / {
            proxy_pass http://backend:8080;
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Proto $scheme;

            # Caching
            proxy_cache cdn_cache;
            proxy_cache_valid 200 60m;
            proxy_cache_use_stale error timeout updating http_500 http_502 http_503 http_504;
            add_header X-Cache-Status $upstream_cache_status;
        }
    }
}
`

	return config
}

// generateCacheConfig creates cache configuration documentation.
func (m *CloudFrontMapper) generateCacheConfig(res *resource.AWSResource) string {
	config := `# CloudFront Cache Behavior Mapping

CloudFront cache behaviors need to be mapped to Caddy cache directives.

## Caddy Cache Plugin
Install the cache plugin: https://github.com/caddyserver/cache-handler

Example configuration:
{
    order cache before rewrite
    cache {
        ttl 1h
        default_max_age 3600
    }
}

## Cache Policies
1. Review CloudFront cache policies (TTL, headers, query strings)
2. Configure equivalent Caddy cache directives
3. Test cache hit rates and adjust as needed

## Cache Invalidation
- CloudFront: CreateInvalidation API
- Caddy: Use cache purge plugin or restart service

## TODO:
- [ ] Map CloudFront cache behaviors to Caddy cache rules
- [ ] Configure cache TTLs
- [ ] Set up cache headers
- [ ] Implement cache invalidation strategy
`

	return config
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

// hasOrigins checks if the CloudFront distribution has origins configured.
func (m *CloudFrontMapper) hasOrigins(res *resource.AWSResource) bool {
	if origins := res.Config["origin"]; origins != nil {
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
