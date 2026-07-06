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
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Labels = map[string]string{
		"homeport.source":   "aws_api_gateway",
		"homeport.api_name": apiNameStr,
		// Traefik integration for routing
		"traefik.enable":                                          "true",
		"traefik.http.routers.kong-api.rule":                      "Host(`api.localhost`)",
		"traefik.http.routers.kong-api.entrypoints":               "web,websecure",
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
	kongConfig, managedRoutes := m.generateKongConfig(res, apiNameStr)
	result.AddConfig("config/kong/kong.yml", []byte(kongConfig))

	// Generate Kong initialization script
	initScript := m.generateInitScript(res)
	result.AddScript("scripts/kong-init.sh", []byte(initScript))
	result.AddScript("backup_apigateway_config.sh", []byte(m.generateBackupScript(apiNameStr)))

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
		result.AddWarning(fmt.Sprintf("Custom domain name detected: %s. Generated Kong routes are ready for DNS cutover.", customDomain))
	}

	if !managedRoutes {
		result.AddManualStep("Run Kong database migrations: docker exec kong kong migrations bootstrap")
		result.AddManualStep("Apply Kong declarative configuration: deck sync -s config/kong/kong.yml")
		result.AddManualStep("Review and update backend service URLs in Kong configuration")
		result.AddManualStep("Test API endpoints and authentication flows")
	}
	for _, step := range netrunbook.Routing(apiNameStr, "aws_api_gateway_rest_api") {
		result.AddRunbookStep(step)
	}
	for _, step := range apiGatewayRunbook(apiNameStr) {
		result.AddRunbookStep(step)
	}

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
      - homeport
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
func (m *APIGatewayMapper) generateKongConfig(res *resource.AWSResource, apiName string) (string, bool) {
	routes := apiGatewayRoutes(res, apiName)
	var b strings.Builder
	b.WriteString(`# Kong Declarative Configuration
# Generated from AWS API Gateway
# Apply with: deck sync -s kong.yml

_format_version: "3.0"
_transform: true

services:
`)
	if len(routes) == 0 {
		b.WriteString("  # Add integrations after API Gateway backend URIs are imported.\n")
	} else {
		for _, route := range routes {
			b.WriteString(fmt.Sprintf("  - name: %s\n", route.name))
			b.WriteString(fmt.Sprintf("    url: %s\n", route.uri))
			b.WriteString("    routes:\n")
			b.WriteString(fmt.Sprintf("      - name: %s-route\n", route.name))
			b.WriteString("        paths:\n")
			b.WriteString(fmt.Sprintf("          - %s\n", route.path))
			b.WriteString("        methods:\n")
			b.WriteString(fmt.Sprintf("          - %s\n", route.method))
			b.WriteString("        strip_path: false\n")
		}
	}
	b.WriteString(`
plugins:
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
  []

upstreams: []

certificates: []
`)

	return b.String(), len(routes) > 0
}

type apiGatewayRoute struct {
	name   string
	path   string
	method string
	uri    string
}

func apiGatewayRoutes(res *resource.AWSResource, apiName string) []apiGatewayRoute {
	integrations := configSlice(res.Config["integration"])
	if len(integrations) == 0 {
		integrations = configSlice(res.Config["integrations"])
	}
	if len(integrations) == 0 {
		for _, apiResource := range configSlice(res.Config["resources"]) {
			path := configString(apiResource["path"])
			for _, integration := range configSlice(apiResource["integration"]) {
				if path != "" && configString(integration["path"]) == "" {
					integration["path"] = path
				}
				integrations = append(integrations, integration)
			}
		}
	}
	routes := []apiGatewayRoute{}
	for i, integration := range integrations {
		uri := configString(integration["uri"])
		if uri == "" {
			continue
		}
		method := configString(integration["http_method"])
		if method == "" {
			method = "ANY"
		}
		path := configString(integration["path"])
		if path == "" {
			path = configString(integration["resource_path"])
		}
		if path == "" {
			path = "/"
		}
		name := configString(integration["name"])
		if name == "" {
			name = fmt.Sprintf("%s-%s-%d", strings.ToLower(method), strings.Trim(path, "/"), i)
		}
		routes = append(routes, apiGatewayRoute{
			name:   sanitizeTraefikName(apiName + "-" + name),
			path:   path,
			method: strings.ToUpper(method),
			uri:    uri,
		})
	}
	return routes
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

func (m *APIGatewayMapper) generateBackupScript(apiName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-kong-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/kong kong-db-compose.yml
echo "$archive"
`, sanitizeTraefikName(apiName))
}

func apiGatewayRunbook(apiName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "api_gateway", "name": apiName, "source": "aws_api_gateway_rest_api"}
	return []domainrunbook.Step{
		{
			ID:               "backup-kong-config",
			Name:             "Backup Kong config",
			Group:            "Backup",
			Type:             domainrunbook.StepTypeCommand,
			Status:           domainrunbook.StepStatusPending,
			Executor:         "shell",
			Command:          []string{"sh", "backup_apigateway_config.sh"},
			SuccessCondition: "Kong declarative config and database compose file are archived before cutover",
			Metadata:         metadata,
		},
		{
			ID:               "cutover-api-gateway-to-kong",
			Name:             "Cut over API Gateway DNS to Kong",
			Group:            "Cutover",
			Type:             domainrunbook.StepTypeDNSCheck,
			Status:           domainrunbook.StepStatusPending,
			Executor:         "dns",
			SuccessCondition: "API hostname resolves to Kong and generated route probes pass",
			Metadata:         metadata,
		},
	}
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
