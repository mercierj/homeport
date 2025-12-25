// Package networking provides mappers for GCP networking services.
package networking

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

// CloudCDNMapper converts GCP Cloud CDN (Backend Bucket) to Varnish or nginx.
type CloudCDNMapper struct {
	*mapper.BaseMapper
}

// NewCloudCDNMapper creates a new Cloud CDN to Varnish/nginx mapper.
func NewCloudCDNMapper() *CloudCDNMapper {
	return &CloudCDNMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeCloudCDN, nil),
	}
}

// Map converts a GCP Cloud CDN backend bucket to a Varnish caching service.
func (m *CloudCDNMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	bucketName := res.GetConfigString("name")
	if bucketName == "" {
		bucketName = res.Name
	}

	result := mapper.NewMappingResult(m.sanitizeName(bucketName))
	svc := result.DockerService

	// Use Varnish as the CDN cache
	svc.Image = "varnish:7.4"

	// Configure ports
	svc.Ports = []string{
		"80:80",   // HTTP cache
		"6081:81", // Varnish admin
	}

	// Extract bucket configuration
	bucketConfig := m.extractBucketConfig(res)
	originURL := bucketConfig["origin_url"]

	// Environment variables
	svc.Environment = map[string]string{
		"VARNISH_SIZE":   "256M",
		"VARNISH_BACKEND": originURL,
	}

	// Labels
	svc.Labels = map[string]string{
		"cloudexit.source":       "google_compute_backend_bucket",
		"cloudexit.service_name": bucketName,
		"cloudexit.origin":       originURL,
	}

	svc.Networks = []string{"cloudexit"}
	svc.Restart = "unless-stopped"

	// Volume for Varnish configuration
	svc.Volumes = []string{
		"./config/varnish:/etc/varnish:ro",
		"varnish-cache:/var/lib/varnish",
	}

	// Add Varnish cache volume
	result.AddVolume(mapper.Volume{
		Name:   "varnish-cache",
		Driver: "local",
		Labels: map[string]string{
			"cloudexit.service": bucketName,
		},
	})

	// Extract CDN configuration
	cdnPolicy := m.extractCDNPolicy(res)
	cacheTTL := m.extractCacheTTL(cdnPolicy)
	cacheMode := m.extractCacheMode(cdnPolicy)
	negativeCaching := m.extractNegativeCaching(cdnPolicy)

	// Generate VCL configuration
	vclConfig := m.generateVCLConfig(bucketName, originURL, cacheTTL, cacheMode, negativeCaching, cdnPolicy)
	result.AddConfig("varnish/default.vcl", []byte(vclConfig))

	// Handle custom cache keys
	if cacheKeyPolicy := cdnPolicy["cache_key_policy"]; cacheKeyPolicy != nil {
		result.AddWarning("Custom cache key policy detected. Review VCL configuration for cache key handling.")
	}

	// Handle signed URLs
	if signedUrlCacheMaxAge := res.GetConfigInt("signed_url_cache_max_age_sec"); signedUrlCacheMaxAge > 0 {
		result.AddWarning(fmt.Sprintf("Signed URL caching configured (%d seconds). Implement signed URL validation.", signedUrlCacheMaxAge))
		result.AddManualStep("Configure signed URL validation in Varnish or use nginx with secure_link module")
	}

	// Handle compression
	if compression := cdnPolicy["compression"]; compression != nil {
		if compressionSlice, ok := compression.([]interface{}); ok && len(compressionSlice) > 0 {
			result.AddWarning("Compression is enabled. Varnish will handle gzip compression automatically.")
		}
	}

	// Health check
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD-SHELL", "varnishadm status || exit 1"},
		Interval: 30 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  3,
	}

	// Generate nginx alternative configuration
	nginxConfig := m.generateNginxConfig(bucketName, originURL, cacheTTL)
	result.AddConfig("nginx-alternative/nginx.conf", []byte(nginxConfig))

	// Generate management script
	managementScript := m.generateManagementScript(bucketName)
	result.AddScript("varnish-manage.sh", []byte(managementScript))

	result.AddManualStep(fmt.Sprintf("Update origin URL in VCL: %s", originURL))
	result.AddManualStep("Configure origin authentication if required")
	result.AddManualStep("Purge cache: docker exec <container> varnishadm 'ban req.url ~ .'")
	result.AddManualStep("Monitor cache statistics: docker exec <container> varnishstat")
	result.AddWarning("Consider using nginx with proxy_cache for simpler setup (see nginx-alternative/nginx.conf)")
	result.AddWarning("Configure SSL/TLS termination with Traefik or nginx reverse proxy")

	return result, nil
}

// extractBucketConfig extracts backend bucket configuration.
func (m *CloudCDNMapper) extractBucketConfig(res *resource.AWSResource) map[string]string {
	config := map[string]string{
		"origin_url": "http://storage.googleapis.com/example-bucket",
	}

	if bucketName := res.GetConfigString("bucket_name"); bucketName != "" {
		config["bucket_name"] = bucketName
		config["origin_url"] = fmt.Sprintf("https://storage.googleapis.com/%s", bucketName)
	}

	if enableCDN := res.GetConfigBool("enable_cdn"); enableCDN {
		config["cdn_enabled"] = "true"
	}

	return config
}

// extractCDNPolicy extracts CDN policy configuration.
func (m *CloudCDNMapper) extractCDNPolicy(res *resource.AWSResource) map[string]interface{} {
	if cdnPolicy := res.Config["cdn_policy"]; cdnPolicy != nil {
		if policyMap, ok := cdnPolicy.(map[string]interface{}); ok {
			return policyMap
		}
	}
	return make(map[string]interface{})
}

// extractCacheTTL extracts cache TTL settings.
func (m *CloudCDNMapper) extractCacheTTL(policy map[string]interface{}) int {
	if defaultTTL, ok := policy["default_ttl"].(float64); ok {
		return int(defaultTTL)
	}
	if clientTTL, ok := policy["client_ttl"].(float64); ok {
		return int(clientTTL)
	}
	return 3600 // Default 1 hour
}

// extractCacheMode extracts cache mode.
func (m *CloudCDNMapper) extractCacheMode(policy map[string]interface{}) string {
	if mode, ok := policy["cache_mode"].(string); ok {
		return mode
	}
	return "CACHE_ALL_STATIC"
}

// extractNegativeCaching checks for negative caching.
func (m *CloudCDNMapper) extractNegativeCaching(policy map[string]interface{}) bool {
	if negCaching, ok := policy["negative_caching"].(bool); ok {
		return negCaching
	}
	return false
}

// generateVCLConfig generates Varnish VCL configuration.
func (m *CloudCDNMapper) generateVCLConfig(bucketName, originURL string, cacheTTL int, cacheMode string, negativeCaching bool, policy map[string]interface{}) string {
	// Extract origin host from URL
	originHost := strings.TrimPrefix(originURL, "https://")
	originHost = strings.TrimPrefix(originHost, "http://")
	parts := strings.SplitN(originHost, "/", 2)
	backendHost := parts[0]

	negativeCachingNote := ""
	if negativeCaching {
		negativeCachingNote = `
    # Negative caching enabled
    if (beresp.status >= 400 && beresp.status <= 599) {
        set beresp.ttl = 60s;  # Cache errors for 60 seconds
    }`
	}

	cacheModeNote := fmt.Sprintf("# Cache mode: %s", cacheMode)

	return fmt.Sprintf(`vcl 4.1;

# Varnish configuration for Cloud CDN: %s
# Origin: %s
%s

backend default {
    .host = "%s";
    .port = "80";
    .connect_timeout = 5s;
    .first_byte_timeout = 60s;
    .between_bytes_timeout = 10s;
}

# Access Control List for purge requests
acl purge {
    "localhost";
    "127.0.0.1";
    "::1";
    # Add your admin IPs here
}

sub vcl_recv {
    # Allow PURGE requests from ACL
    if (req.method == "PURGE") {
        if (!client.ip ~ purge) {
            return (synth(403, "Forbidden"));
        }
        return (purge);
    }

    # Only cache GET and HEAD requests
    if (req.method != "GET" && req.method != "HEAD") {
        return (pass);
    }

    # Remove cookies for static content
    if (req.url ~ "\.(jpg|jpeg|png|gif|ico|svg|css|js|woff|woff2|ttf|eot)$") {
        unset req.http.Cookie;
    }

    # Normalize Accept-Encoding header
    if (req.http.Accept-Encoding) {
        if (req.url ~ "\.(jpg|jpeg|png|gif|ico|svg)$") {
            unset req.http.Accept-Encoding;
        } elsif (req.http.Accept-Encoding ~ "gzip") {
            set req.http.Accept-Encoding = "gzip";
        } elsif (req.http.Accept-Encoding ~ "deflate") {
            set req.http.Accept-Encoding = "deflate";
        } else {
            unset req.http.Accept-Encoding;
        }
    }
}

sub vcl_backend_response {
    # Set default TTL
    set beresp.ttl = %ds;

    # Cache static content
    if (bereq.url ~ "\.(jpg|jpeg|png|gif|ico|svg|css|js|woff|woff2|ttf|eot|pdf|zip)$") {
        set beresp.ttl = 24h;
        unset beresp.http.Set-Cookie;
    }
%s

    # Enable ESI processing if needed
    # set beresp.do_esi = true;

    # Set cache markers
    set beresp.http.X-Cache-TTL = beresp.ttl;
}

sub vcl_deliver {
    # Add cache status header
    if (obj.hits > 0) {
        set resp.http.X-Cache = "HIT";
        set resp.http.X-Cache-Hits = obj.hits;
    } else {
        set resp.http.X-Cache = "MISS";
    }

    # Remove internal headers
    unset resp.http.X-Varnish;
    unset resp.http.Via;
    unset resp.http.X-Cache-TTL;
}

sub vcl_hit {
    # Handle purge requests
    if (req.method == "PURGE") {
        return (synth(200, "Purged"));
    }
}

sub vcl_miss {
    # Handle purge requests
    if (req.method == "PURGE") {
        return (synth(404, "Not in cache"));
    }
}
`, bucketName, originURL, cacheModeNote, backendHost, cacheTTL, negativeCachingNote)
}

// generateNginxConfig generates nginx alternative configuration.
func (m *CloudCDNMapper) generateNginxConfig(bucketName, originURL string, cacheTTL int) string {
	return fmt.Sprintf(`# nginx CDN configuration for %s
# Alternative to Varnish

proxy_cache_path /var/cache/nginx levels=1:2 keys_zone=cdn_cache:10m max_size=1g inactive=60m use_temp_path=off;

upstream backend {
    server %s;
}

server {
    listen 80;
    server_name _;

    # Cache configuration
    proxy_cache cdn_cache;
    proxy_cache_valid 200 %dm;
    proxy_cache_valid 404 1m;
    proxy_cache_use_stale error timeout updating http_500 http_502 http_503 http_504;
    proxy_cache_background_update on;
    proxy_cache_lock on;

    # Add cache status header
    add_header X-Cache-Status $upstream_cache_status;

    location / {
        proxy_pass %s;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # Cache settings
        proxy_cache_key "$scheme$request_method$host$request_uri";
        proxy_ignore_headers Cache-Control;
        proxy_hide_header Set-Cookie;
    }

    # Cache purge endpoint
    location ~ /purge(/.*) {
        allow 127.0.0.1;
        deny all;
        proxy_cache_purge cdn_cache "$scheme$request_method$host$1";
    }

    # Health check endpoint
    location /health {
        access_log off;
        return 200 "healthy\n";
        add_header Content-Type text/plain;
    }
}
`, bucketName, originURL, cacheTTL/60, originURL)
}

// generateManagementScript generates a management script for Varnish.
func (m *CloudCDNMapper) generateManagementScript(bucketName string) string {
	return fmt.Sprintf(`#!/bin/bash
# Varnish CDN Management Script for %s

set -e

CONTAINER_NAME="varnish-cdn-%s"

# Function to display usage
usage() {
    echo "Usage: $0 {stats|purge|restart|logs}"
    echo ""
    echo "Commands:"
    echo "  stats   - Show cache statistics"
    echo "  purge   - Purge entire cache"
    echo "  restart - Restart Varnish"
    echo "  logs    - Show Varnish logs"
    exit 1
}

# Check if container exists
check_container() {
    if ! docker ps -a --format '{{.Names}}' | grep -q "^${CONTAINER_NAME}$"; then
        echo "Error: Container $CONTAINER_NAME not found"
        exit 1
    fi
}

# Show statistics
show_stats() {
    echo "Varnish Cache Statistics:"
    docker exec "$CONTAINER_NAME" varnishstat -1
}

# Purge cache
purge_cache() {
    echo "Purging Varnish cache..."
    docker exec "$CONTAINER_NAME" varnishadm "ban req.url ~ ."
    echo "Cache purged successfully"
}

# Restart Varnish
restart_varnish() {
    echo "Restarting Varnish..."
    docker restart "$CONTAINER_NAME"
    echo "Varnish restarted successfully"
}

# Show logs
show_logs() {
    docker logs -f "$CONTAINER_NAME"
}

# Main
check_container

case "${1:-}" in
    stats)
        show_stats
        ;;
    purge)
        purge_cache
        ;;
    restart)
        restart_varnish
        ;;
    logs)
        show_logs
        ;;
    *)
        usage
        ;;
esac
`, bucketName, bucketName)
}

// sanitizeName sanitizes the name for Docker.
func (m *CloudCDNMapper) sanitizeName(name string) string {
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
		validName = "cdn"
	}

	return validName
}
