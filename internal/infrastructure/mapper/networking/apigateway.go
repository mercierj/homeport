// Package networking provides mappers for AWS networking services.
package networking

import (
	"context"
	"fmt"
	"time"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

// APIGatewayMapper converts AWS API Gateway to Kong or Traefik.
type APIGatewayMapper struct {
	*mapper.BaseMapper
}

// NewAPIGatewayMapper creates a new API Gateway to Kong/Traefik mapper.
func NewAPIGatewayMapper() *APIGatewayMapper {
	return &APIGatewayMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeAPIGateway, nil),
	}
}

// Map converts an API Gateway to a Kong service with Traefik integration.
func (m *APIGatewayMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	apiName := res.Config["name"]
	if apiName == nil {
		apiName = res.Name
	}
	apiNameStr := fmt.Sprintf("%v", apiName)

	// Create result using Kong as the API Gateway
	result := mapper.NewMappingResult("kong")
	svc := result.DockerService

	// Configure Kong service
	svc.Image = "kong:3.5-alpine"
	svc.Ports = []string{
		"8000:8000", // Proxy HTTP
		"8443:8443", // Proxy HTTPS
		"8001:8001", // Admin API HTTP
		"8444:8444", // Admin API HTTPS
	}
	svc.Environment = map[string]string{
		"KONG_DATABASE":         "postgres",
		"KONG_PG_HOST":          "kong-db",
		"KONG_PG_USER":          "kong",
		"KONG_PG_PASSWORD":      "kong",
		"KONG_PROXY_ACCESS_LOG": "/dev/stdout",
		"KONG_ADMIN_ACCESS_LOG": "/dev/stdout",
		"KONG_PROXY_ERROR_LOG":  "/dev/stderr",
		"KONG_ADMIN_ERROR_LOG":  "/dev/stderr",
		"KONG_ADMIN_LISTEN":     "0.0.0.0:8001, 0.0.0.0:8444 ssl",
	}
	svc.Volumes = []string{
		"./config/kong:/etc/kong",
	}
	svc.DependsOn = []string{"kong-db"}
	svc.Networks = []string{"cloudexit"}
	svc.Labels = map[string]string{
		"cloudexit.source":   "aws_api_gateway",
		"cloudexit.api_name": apiNameStr,
		// Traefik integration for routing
		"traefik.enable":                                "true",
		"traefik.http.routers.kong-api.rule":            "Host(`api.localhost`)",
		"traefik.http.routers.kong-api.entrypoints":     "web,websecure",
		"traefik.http.services.kong-api.loadbalancer.server.port": "8000",
	}
	svc.Restart = "unless-stopped"

	// Add health check
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "kong", "health"},
		Interval: 10 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  5,
	}

	// Add PostgreSQL database for Kong
	dbResult := m.createKongDatabase()
	result.AddConfig("kong-db-compose.yml", []byte(dbResult))

	// Generate Kong declarative configuration
	kongConfig := m.generateKongConfig(res, apiNameStr)
	result.AddConfig("config/kong/kong.yml", []byte(kongConfig))

	// Generate Kong initialization script
	initScript := m.generateInitScript(res)
	result.AddScript("scripts/kong-init.sh", []byte(initScript))

	// Handle API Gateway stages
	if stages := res.Config["stage"]; stages != nil {
		result.AddWarning("API Gateway stages detected. Configure Kong workspaces or route prefixes for different environments.")
		result.AddManualStep("Review API Gateway stages and configure Kong routes accordingly")
	}

	// Handle authorizers
	if m.hasAuthorizers(res) {
		result.AddWarning("API Gateway authorizers detected. Configure Kong authentication plugins (JWT, OAuth2, API Key).")
		result.AddManualStep("Migrate API Gateway authorizers to Kong authentication plugins")

		authConfig := m.generateAuthPluginConfig(res, apiNameStr)
		result.AddConfig("config/kong/auth-plugins.yml", []byte(authConfig))
	}

	// Handle request validators
	if m.hasRequestValidators(res) {
		result.AddWarning("API Gateway request validators detected. Configure Kong request-validator plugin.")
		result.AddManualStep("Review request/response models and configure Kong validation plugins")
	}

	// Handle API keys
	if m.hasAPIKeys(res) {
		result.AddWarning("API Gateway API keys detected. Configure Kong key-auth plugin.")
		result.AddManualStep("Migrate API keys to Kong consumers and credentials")
	}

	// Handle throttling
	if m.hasThrottling(res) {
		result.AddWarning("API Gateway throttling detected. Configure Kong rate-limiting plugin.")
		result.AddManualStep("Review throttling settings and configure Kong rate limiting")
	}

	// Handle custom domain names
	if customDomain := res.GetConfigString("domain_name"); customDomain != "" {
		result.AddWarning(fmt.Sprintf("Custom domain name detected: %s. Configure DNS and SSL certificates.", customDomain))
		result.AddManualStep("Configure custom domain name in Kong and Traefik")
		result.AddManualStep("Set up SSL certificates for custom domain")
	}

	result.AddManualStep("Run Kong database migrations: docker exec kong kong migrations bootstrap")
	result.AddManualStep("Apply Kong declarative configuration: deck sync -s config/kong/kong.yml")
	result.AddManualStep("Review and update backend service URLs in Kong configuration")
	result.AddManualStep("Test API endpoints and authentication flows")

	return result, nil
}

// createKongDatabase generates a PostgreSQL service for Kong.
func (m *APIGatewayMapper) createKongDatabase() string {
	return `# Kong Database Service
# Add this to your docker-compose.yml

services:
  kong-db:
    image: postgres:15-alpine
    container_name: kong-db
    environment:
      POSTGRES_DB: kong
      POSTGRES_USER: kong
      POSTGRES_PASSWORD: kong
    volumes:
      - kong-db-data:/var/lib/postgresql/data
    networks:
      - cloudexit
    restart: unless-stopped
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U kong"]
      interval: 10s
      timeout: 5s
      retries: 5

volumes:
  kong-db-data:
    driver: local
`
}

// generateKongConfig creates the Kong declarative configuration.
func (m *APIGatewayMapper) generateKongConfig(res *resource.AWSResource, apiName string) string {
	config := `# Kong Declarative Configuration
# Generated from AWS API Gateway
# Apply with: deck sync -s kong.yml

_format_version: "3.0"
_transform: true

services:
  # TODO: Add services based on API Gateway integrations
  # Example:
  # - name: backend-service
  #   url: http://backend:8080
  #   routes:
  #     - name: api-route
  #       paths:
  #         - /api
  #       methods:
  #         - GET
  #         - POST
  #       strip_path: true
  #   plugins:
  #     - name: rate-limiting
  #       config:
  #         minute: 100
  #         policy: local

plugins:
  # Global plugins
  - name: correlation-id
    config:
      header_name: X-Correlation-ID
      generator: uuid
      echo_downstream: true

  - name: request-id
    config:
      header_name: X-Request-ID
      echo_downstream: true

consumers:
  # TODO: Add consumers for API keys, JWT, OAuth2
  # Example:
  # - username: api-user
  #   custom_id: api-user-1
  #   keyauth_credentials:
  #     - key: your-api-key-here

# Upstreams for load balancing
upstreams: []

# Certificates for SSL/TLS
certificates: []
`

	return config
}

// generateAuthPluginConfig creates authentication plugin configuration.
func (m *APIGatewayMapper) generateAuthPluginConfig(res *resource.AWSResource, apiName string) string {
	config := `# Kong Authentication Plugins
# Configure based on API Gateway authorizers

# JWT Authentication Example
# - name: jwt
#   service: your-service
#   config:
#     uri_param_names:
#       - jwt
#     cookie_names:
#       - jwt
#     key_claim_name: kid
#     secret_is_base64: false

# OAuth 2.0 Example
# - name: oauth2
#   service: your-service
#   config:
#     scopes:
#       - read
#       - write
#     mandatory_scope: true
#     enable_authorization_code: true

# API Key Authentication Example
# - name: key-auth
#   service: your-service
#   config:
#     key_names:
#       - apikey
#       - x-api-key
#     hide_credentials: true

# Basic Auth Example
# - name: basic-auth
#   service: your-service
#   config:
#     hide_credentials: true

# AWS IAM to Kong HMAC Mapping
# - name: hmac-auth
#   service: your-service
#   config:
#     hide_credentials: true
#     clock_skew: 300
`

	return config
}

// generateInitScript creates an initialization script for Kong.
func (m *APIGatewayMapper) generateInitScript(res *resource.AWSResource) string {
	script := `#!/bin/bash
# Kong Initialization Script

set -e

echo "Waiting for Kong database to be ready..."
until docker exec kong-db pg_isready -U kong; do
  echo "Waiting for database..."
  sleep 2
done

echo "Running Kong migrations..."
docker exec kong kong migrations bootstrap

echo "Installing deck (Kong declarative config tool)..."
# Install deck if not already installed
if ! command -v deck &> /dev/null; then
    echo "Please install deck: https://docs.konghq.com/deck/latest/installation/"
fi

echo "Kong initialization complete!"
echo "Apply configuration with: deck sync -s config/kong/kong.yml"
echo "Access Kong Admin API: http://localhost:8001"
echo "Access Kong Proxy: http://localhost:8000"
`

	return script
}

// hasAuthorizers checks if the API Gateway has authorizers configured.
func (m *APIGatewayMapper) hasAuthorizers(res *resource.AWSResource) bool {
	// Check for Lambda authorizers or Cognito authorizers
	if authorizers := res.Config["authorizer"]; authorizers != nil {
		return true
	}
	if authorizerID := res.GetConfigString("authorizer_id"); authorizerID != "" {
		return true
	}
	return false
}

// hasRequestValidators checks if the API Gateway has request validators.
func (m *APIGatewayMapper) hasRequestValidators(res *resource.AWSResource) bool {
	if validator := res.Config["request_validator"]; validator != nil {
		return true
	}
	if validatorID := res.GetConfigString("request_validator_id"); validatorID != "" {
		return true
	}
	return false
}

// hasAPIKeys checks if the API Gateway uses API keys.
func (m *APIGatewayMapper) hasAPIKeys(res *resource.AWSResource) bool {
	if apiKeyRequired := res.GetConfigBool("api_key_required"); apiKeyRequired {
		return true
	}
	if apiKeys := res.Config["api_key"]; apiKeys != nil {
		return true
	}
	return false
}

// hasThrottling checks if the API Gateway has throttling configured.
func (m *APIGatewayMapper) hasThrottling(res *resource.AWSResource) bool {
	if throttle := res.Config["throttle_settings"]; throttle != nil {
		return true
	}
	if quota := res.Config["quota_settings"]; quota != nil {
		return true
	}
	return false
}
