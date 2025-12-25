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

// CDNMapper converts Azure CDN to Varnish or nginx.
type CDNMapper struct {
	*mapper.BaseMapper
}

// NewCDNMapper creates a new Azure CDN to Varnish/nginx mapper.
func NewCDNMapper() *CDNMapper {
	return &CDNMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeAzureCDN, nil),
	}
}

// Map converts an Azure CDN profile to a Varnish caching service.
func (m *CDNMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	cdnName := res.GetConfigString("name")
	if cdnName == "" {
		cdnName = res.Name
	}

	result := mapper.NewMappingResult(m.sanitizeName(cdnName))
	svc := result.DockerService

	// Use Varnish as the primary CDN replacement
	svc.Image = "varnish:7.4"
	svc.Command = []string{
		"varnishd",
		"-F",
		"-f", "/etc/varnish/default.vcl",
		"-s", "malloc,256m",
		"-a", ":80",
	}

	svc.Ports = []string{
		"6081:80",   // Varnish HTTP
		"6082:6082", // Varnish admin
	}

	svc.Volumes = []string{
		"./config/varnish:/etc/varnish:ro",
	}

	svc.Networks = []string{"cloudexit"}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"cloudexit.source":   "azurerm_cdn_profile",
		"cloudexit.cdn_name": cdnName,
	}

	// Health check for Varnish
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD-SHELL", "varnishadm ping || exit 1"},
		Interval: 30 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  3,
	}

	// Get CDN SKU
	sku := res.GetConfigString("sku")
	if sku != "" {
		result.AddWarning(fmt.Sprintf("Azure CDN SKU: %s. Varnish configuration may need tuning for similar performance.", sku))
	}

	// Handle CDN endpoints
	if endpoints := res.Config["endpoint"]; endpoints != nil {
		m.handleEndpoints(endpoints, result)
	}

	// Generate Varnish VCL configuration
	vclConfig := m.generateVarnishVCL(cdnName, res)
	result.AddConfig("config/varnish/default.vcl", []byte(vclConfig))

	// Generate nginx alternative configuration
	nginxConfig := m.generateNginxConfig(cdnName, res)
	result.AddConfig("config/nginx/nginx.conf", []byte(nginxConfig))

	// Generate nginx cache configuration
	nginxCacheConfig := m.generateNginxCacheConfig(cdnName)
	result.AddConfig("config/nginx/cache.conf", []byte(nginxCacheConfig))

	// Generate setup script
	setupScript := m.generateSetupScript(cdnName)
	result.AddScript("setup_cdn.sh", []byte(setupScript))

	result.AddWarning("Azure CDN converted to Varnish. Review cache policies and TTL settings.")
	result.AddWarning("nginx with caching configuration also provided as an alternative")
	result.AddManualStep("Configure origin servers in Varnish VCL")
	result.AddManualStep("Adjust cache storage size based on your needs")
	result.AddManualStep("Configure custom domains and SSL certificates")
	result.AddManualStep("Set up cache purging mechanisms")

	return result, nil
}

// handleEndpoints processes CDN endpoints.
func (m *CDNMapper) handleEndpoints(endpoints interface{}, result *mapper.MappingResult) {
	if endpointSlice, ok := endpoints.([]interface{}); ok {
		for _, endpoint := range endpointSlice {
			if endpointMap, ok := endpoint.(map[string]interface{}); ok {
				name, _ := endpointMap["name"].(string)
				originHost, _ := endpointMap["origin_host_name"].(string)
				originPath, _ := endpointMap["origin_path"].(string)

				result.AddWarning(fmt.Sprintf("CDN endpoint '%s' with origin %s%s", name, originHost, originPath))

				// Check for custom domains
				if customDomain := endpointMap["custom_domain"]; customDomain != nil {
					result.AddManualStep("Configure custom domain and SSL for CDN endpoint")
				}

				// Check for compression
				if compression := endpointMap["is_compression_enabled"]; compression != nil {
					if enabled, ok := compression.(bool); ok && enabled {
						result.AddWarning("Compression is enabled. Varnish VCL includes compression support.")
					}
				}
			}
		}
	}
}

// generateVarnishVCL generates Varnish VCL configuration.
func (m *CDNMapper) generateVarnishVCL(cdnName string, res *resource.AWSResource) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Varnish VCL configuration for Azure CDN: %s\n\n", cdnName))
	sb.WriteString("vcl 4.1;\n\n")

	sb.WriteString("import std;\n\n")

	sb.WriteString("# Backend configuration\n")
	sb.WriteString("backend default {\n")
	sb.WriteString("    .host = \"origin-server\";\n")
	sb.WriteString("    .port = \"80\";\n")
	sb.WriteString("    .probe = {\n")
	sb.WriteString("        .url = \"/health\";\n")
	sb.WriteString("        .interval = 5s;\n")
	sb.WriteString("        .timeout = 2s;\n")
	sb.WriteString("        .window = 5;\n")
	sb.WriteString("        .threshold = 3;\n")
	sb.WriteString("    }\n")
	sb.WriteString("}\n\n")

	sb.WriteString("# Receive from client\n")
	sb.WriteString("sub vcl_recv {\n")
	sb.WriteString("    # Remove cookies for static content\n")
	sb.WriteString("    if (req.url ~ \"\\.(jpg|jpeg|png|gif|ico|css|js|svg|woff|woff2)$\") {\n")
	sb.WriteString("        unset req.http.Cookie;\n")
	sb.WriteString("    }\n\n")

	sb.WriteString("    # Normalize Accept-Encoding header\n")
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
	sb.WriteString("    }\n\n")

	sb.WriteString("    # Cache purge support\n")
	sb.WriteString("    if (req.method == \"PURGE\") {\n")
	sb.WriteString("        return (purge);\n")
	sb.WriteString("    }\n\n")

	sb.WriteString("    # Allow only GET and HEAD methods\n")
	sb.WriteString("    if (req.method != \"GET\" && req.method != \"HEAD\") {\n")
	sb.WriteString("        return (pass);\n")
	sb.WriteString("    }\n")
	sb.WriteString("}\n\n")

	sb.WriteString("# Backend response\n")
	sb.WriteString("sub vcl_backend_response {\n")
	sb.WriteString("    # Set default TTL for cached objects\n")
	sb.WriteString("    if (beresp.status == 200) {\n")
	sb.WriteString("        set beresp.ttl = 1h;\n")
	sb.WriteString("    }\n\n")

	sb.WriteString("    # Long TTL for static content\n")
	sb.WriteString("    if (bereq.url ~ \"\\.(jpg|jpeg|png|gif|ico|css|js|svg|woff|woff2)$\") {\n")
	sb.WriteString("        set beresp.ttl = 24h;\n")
	sb.WriteString("        set beresp.http.Cache-Control = \"public, max-age=86400\";\n")
	sb.WriteString("    }\n\n")

	sb.WriteString("    # Enable gzip compression\n")
	sb.WriteString("    if (beresp.http.content-type ~ \"text|javascript|json|xml\") {\n")
	sb.WriteString("        set beresp.do_gzip = true;\n")
	sb.WriteString("    }\n")
	sb.WriteString("}\n\n")

	sb.WriteString("# Deliver to client\n")
	sb.WriteString("sub vcl_deliver {\n")
	sb.WriteString("    # Add cache hit/miss header\n")
	sb.WriteString("    if (obj.hits > 0) {\n")
	sb.WriteString("        set resp.http.X-Cache = \"HIT\";\n")
	sb.WriteString("        set resp.http.X-Cache-Hits = obj.hits;\n")
	sb.WriteString("    } else {\n")
	sb.WriteString("        set resp.http.X-Cache = \"MISS\";\n")
	sb.WriteString("    }\n")
	sb.WriteString("}\n")

	return sb.String()
}

// generateNginxConfig generates nginx configuration with caching.
func (m *CDNMapper) generateNginxConfig(cdnName string, res *resource.AWSResource) string {
	return fmt.Sprintf(`# nginx CDN configuration for Azure CDN: %s

user nginx;
worker_processes auto;
error_log /var/log/nginx/error.log warn;
pid /var/run/nginx.pid;

events {
    worker_connections 1024;
}

http {
    include /etc/nginx/mime.types;
    default_type application/octet-stream;

    log_format main '$remote_addr - $remote_user [$time_local] "$request" '
                    '$status $body_bytes_sent "$http_referer" '
                    '"$http_user_agent" "$http_x_forwarded_for"';

    access_log /var/log/nginx/access.log main;

    sendfile on;
    tcp_nopush on;
    tcp_nodelay on;
    keepalive_timeout 65;
    types_hash_max_size 2048;

    # Gzip compression
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

    include /etc/nginx/conf.d/*.conf;
}
`, cdnName)
}

// generateNginxCacheConfig generates nginx cache server configuration.
func (m *CDNMapper) generateNginxCacheConfig(cdnName string) string {
	return fmt.Sprintf(`# nginx cache server configuration for: %s

upstream origin {
    server origin-server:80;
}

server {
    listen 80;
    server_name _;

    # Cache status header
    add_header X-Cache-Status $upstream_cache_status;

    location / {
        proxy_cache cdn_cache;
        proxy_cache_valid 200 1h;
        proxy_cache_valid 404 1m;
        proxy_cache_use_stale error timeout updating http_500 http_502 http_503 http_504;
        proxy_cache_background_update on;
        proxy_cache_lock on;

        proxy_pass http://origin;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # Static content - long cache
    location ~* \.(jpg|jpeg|png|gif|ico|css|js|svg|woff|woff2)$ {
        proxy_cache cdn_cache;
        proxy_cache_valid 200 24h;
        proxy_cache_use_stale error timeout updating http_500 http_502 http_503 http_504;

        proxy_pass http://origin;
        proxy_set_header Host $host;

        expires 1d;
        add_header Cache-Control "public, immutable";
        add_header X-Cache-Status $upstream_cache_status;
    }

    # Cache purge endpoint
    location ~ /purge(/.*) {
        allow 127.0.0.1;
        deny all;
        proxy_cache_purge cdn_cache $1$is_args$args;
    }
}
`, cdnName)
}

// generateSetupScript generates a setup script.
func (m *CDNMapper) generateSetupScript(cdnName string) string {
	return fmt.Sprintf(`#!/bin/bash
# Setup script for Azure CDN: %s

set -e

echo "Creating Varnish configuration directory..."
mkdir -p ./config/varnish
mkdir -p ./config/nginx

echo "Starting Varnish CDN cache..."
docker-compose up -d %s

echo "Waiting for Varnish to be ready..."
sleep 5

echo ""
echo "Testing Varnish..."
curl -I http://localhost:6081/

echo ""
echo "Varnish CDN is running!"
echo "HTTP cache available at: http://localhost:6081"
echo "Admin interface at: http://localhost:6082"
echo ""
echo "Next steps:"
echo "1. Configure origin server in config/varnish/default.vcl"
echo "2. Adjust cache TTL settings as needed"
echo "3. Test caching: curl -I http://localhost:6081/"
echo "4. Monitor cache hits: varnishstat"
echo ""
echo "Alternative: To use nginx instead, see config/nginx/"
`, cdnName, m.sanitizeName(cdnName))
}

// sanitizeName creates a valid Docker service name.
func (m *CDNMapper) sanitizeName(name string) string {
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
		validName = "varnish-cdn"
	}
	return validName
}
