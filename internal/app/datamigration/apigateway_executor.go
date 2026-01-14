package datamigration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// APIGatewayToTraefikExecutor exports API Gateway configurations to Traefik format.
type APIGatewayToTraefikExecutor struct{}

// NewAPIGatewayToTraefikExecutor creates a new API Gateway to Traefik executor.
func NewAPIGatewayToTraefikExecutor() *APIGatewayToTraefikExecutor {
	return &APIGatewayToTraefikExecutor{}
}

// Type returns the migration type.
func (e *APIGatewayToTraefikExecutor) Type() string {
	return "apigateway_to_traefik"
}

// GetPhases returns the migration phases.
func (e *APIGatewayToTraefikExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Fetching API configuration",
		"Extracting routes",
		"Generating Traefik config",
		"Writing output files",
	}
}

// Validate validates the migration configuration.
func (e *APIGatewayToTraefikExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	// Validate source config
	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["api_id"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.api_id is required")
		}
		if _, ok := config.Source["access_key_id"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.access_key_id is required")
		}
		if _, ok := config.Source["secret_access_key"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.secret_access_key is required")
		}
		if _, ok := config.Source["region"].(string); !ok {
			result.Warnings = append(result.Warnings, "source.region not specified, using default us-east-1")
		}
	}

	// Validate destination config
	if config.Destination == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "destination configuration is required")
	} else {
		if _, ok := config.Destination["output_dir"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "destination.output_dir is required")
		}
	}

	return result, nil
}

// apiGatewayRestAPI represents the REST API details from AWS CLI.
type apiGatewayRestAPI struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
}

// apiGatewayResource represents an API Gateway resource.
type apiGatewayResource struct {
	ID              string                        `json:"id"`
	ParentID        string                        `json:"parentId,omitempty"`
	PathPart        string                        `json:"pathPart,omitempty"`
	Path            string                        `json:"path"`
	ResourceMethods map[string]*apiGatewayMethod  `json:"resourceMethods,omitempty"`
}

// apiGatewayMethod represents an API Gateway method.
type apiGatewayMethod struct {
	HTTPMethod        string                    `json:"httpMethod"`
	AuthorizationType string                    `json:"authorizationType"`
	APIKeyRequired    bool                      `json:"apiKeyRequired"`
	MethodIntegration *apiGatewayIntegration    `json:"methodIntegration,omitempty"`
}

// apiGatewayIntegration represents an API Gateway integration.
type apiGatewayIntegration struct {
	Type                  string            `json:"type"`
	HTTPMethod            string            `json:"httpMethod"`
	URI                   string            `json:"uri"`
	ConnectionType        string            `json:"connectionType,omitempty"`
	IntegrationHTTPMethod string            `json:"integrationHttpMethod,omitempty"`
	RequestParameters     map[string]string `json:"requestParameters,omitempty"`
}

// apiGatewayResourcesResponse represents the response from get-resources.
type apiGatewayResourcesResponse struct {
	Items []apiGatewayResource `json:"items"`
}

// traefikRoute represents a Traefik route configuration.
type traefikRoute struct {
	Path       string
	Methods    []string
	ServiceURL string
	Name       string
}

// Execute performs the migration.
func (e *APIGatewayToTraefikExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	// Extract source configuration
	apiID := config.Source["api_id"].(string)
	accessKeyID := config.Source["access_key_id"].(string)
	secretAccessKey := config.Source["secret_access_key"].(string)
	region, _ := config.Source["region"].(string)
	if region == "" {
		region = "us-east-1"
	}

	// Extract destination configuration
	outputDir := config.Destination["output_dir"].(string)

	// Extract optional backend URL override
	defaultBackendURL, _ := config.Destination["default_backend_url"].(string)
	if defaultBackendURL == "" {
		defaultBackendURL = "http://backend:8080"
	}

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating AWS credentials")
	EmitProgress(m, 5, "Checking credentials")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Test AWS credentials by checking caller identity
	testCmd := exec.CommandContext(ctx, "aws", "sts", "get-caller-identity", "--region", region)
	testCmd.Env = append(os.Environ(),
		"AWS_ACCESS_KEY_ID="+accessKeyID,
		"AWS_SECRET_ACCESS_KEY="+secretAccessKey,
		"AWS_DEFAULT_REGION="+region,
	)
	if output, err := testCmd.CombinedOutput(); err != nil {
		EmitLog(m, "error", fmt.Sprintf("AWS credentials validation failed: %s", string(output)))
		return fmt.Errorf("failed to validate AWS credentials: %w", err)
	}
	EmitLog(m, "info", "AWS credentials validated successfully")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Fetching API configuration
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", fmt.Sprintf("Fetching API Gateway configuration for: %s", apiID))
	EmitProgress(m, 20, "Getting API details")

	// Get REST API details
	getAPICmd := exec.CommandContext(ctx, "aws", "apigateway", "get-rest-api",
		"--rest-api-id", apiID,
		"--region", region,
		"--output", "json",
	)
	getAPICmd.Env = append(os.Environ(),
		"AWS_ACCESS_KEY_ID="+accessKeyID,
		"AWS_SECRET_ACCESS_KEY="+secretAccessKey,
		"AWS_DEFAULT_REGION="+region,
	)

	apiOutput, err := getAPICmd.Output()
	if err != nil {
		EmitLog(m, "error", "Failed to get REST API details")
		return fmt.Errorf("failed to get REST API: %w", err)
	}

	var restAPI apiGatewayRestAPI
	if err := json.Unmarshal(apiOutput, &restAPI); err != nil {
		return fmt.Errorf("failed to parse REST API response: %w", err)
	}

	EmitLog(m, "info", fmt.Sprintf("API Name: %s, ID: %s", restAPI.Name, restAPI.ID))
	if restAPI.Description != "" {
		EmitLog(m, "info", fmt.Sprintf("Description: %s", restAPI.Description))
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Extracting routes
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", "Fetching API resources and methods")
	EmitProgress(m, 40, "Extracting routes")

	// Get resources
	getResourcesCmd := exec.CommandContext(ctx, "aws", "apigateway", "get-resources",
		"--rest-api-id", apiID,
		"--region", region,
		"--output", "json",
	)
	getResourcesCmd.Env = append(os.Environ(),
		"AWS_ACCESS_KEY_ID="+accessKeyID,
		"AWS_SECRET_ACCESS_KEY="+secretAccessKey,
		"AWS_DEFAULT_REGION="+region,
	)

	resourcesOutput, err := getResourcesCmd.Output()
	if err != nil {
		EmitLog(m, "error", "Failed to get API resources")
		return fmt.Errorf("failed to get API resources: %w", err)
	}

	var resourcesResp apiGatewayResourcesResponse
	if err := json.Unmarshal(resourcesOutput, &resourcesResp); err != nil {
		return fmt.Errorf("failed to parse resources response: %w", err)
	}

	EmitLog(m, "info", fmt.Sprintf("Found %d resource(s)", len(resourcesResp.Items)))

	// Collect routes with their methods
	var routes []traefikRoute
	routeCounter := 0

	for _, resource := range resourcesResp.Items {
		if m.IsCancelled() {
			return fmt.Errorf("migration cancelled")
		}

		// Skip root resource without methods
		if resource.Path == "/" && len(resource.ResourceMethods) == 0 {
			continue
		}

		// Get detailed method information for each resource
		for methodName := range resource.ResourceMethods {
			if methodName == "OPTIONS" {
				// Skip OPTIONS as it's typically for CORS
				continue
			}

			routeCounter++
			routeName := e.generateRouteName(restAPI.Name, resource.Path, methodName, routeCounter)

			// Get method details including integration
			getMethodCmd := exec.CommandContext(ctx, "aws", "apigateway", "get-method",
				"--rest-api-id", apiID,
				"--resource-id", resource.ID,
				"--http-method", methodName,
				"--region", region,
				"--output", "json",
			)
			getMethodCmd.Env = append(os.Environ(),
				"AWS_ACCESS_KEY_ID="+accessKeyID,
				"AWS_SECRET_ACCESS_KEY="+secretAccessKey,
				"AWS_DEFAULT_REGION="+region,
			)

			methodOutput, err := getMethodCmd.Output()
			if err != nil {
				EmitLog(m, "warn", fmt.Sprintf("Failed to get method details for %s %s: %v", methodName, resource.Path, err))
				// Create route with default backend
				routes = append(routes, traefikRoute{
					Path:       resource.Path,
					Methods:    []string{methodName},
					ServiceURL: defaultBackendURL,
					Name:       routeName,
				})
				continue
			}

			var method apiGatewayMethod
			if err := json.Unmarshal(methodOutput, &method); err != nil {
				EmitLog(m, "warn", fmt.Sprintf("Failed to parse method details: %v", err))
				routes = append(routes, traefikRoute{
					Path:       resource.Path,
					Methods:    []string{methodName},
					ServiceURL: defaultBackendURL,
					Name:       routeName,
				})
				continue
			}

			// Determine backend URL from integration
			backendURL := defaultBackendURL
			if method.MethodIntegration != nil && method.MethodIntegration.URI != "" {
				backendURL = e.extractBackendURL(method.MethodIntegration)
			}

			routes = append(routes, traefikRoute{
				Path:       resource.Path,
				Methods:    []string{methodName},
				ServiceURL: backendURL,
				Name:       routeName,
			})

			EmitLog(m, "info", fmt.Sprintf("Route: %s %s -> %s", methodName, resource.Path, backendURL))
		}
	}

	if len(routes) == 0 {
		EmitLog(m, "warn", "No routes found in API Gateway")
		return fmt.Errorf("no routes found to convert")
	}

	EmitLog(m, "info", fmt.Sprintf("Extracted %d route(s)", len(routes)))

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Generating Traefik config
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Generating Traefik dynamic configuration")
	EmitProgress(m, 70, "Generating config")

	traefikConfig := e.generateTraefikConfig(routes, restAPI.Name)

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Writing output files
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Writing Traefik configuration files")
	EmitProgress(m, 90, "Writing files")

	// Create output directory
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Write traefik.yml
	traefikPath := filepath.Join(outputDir, "traefik.yml")
	if err := os.WriteFile(traefikPath, []byte(traefikConfig), 0644); err != nil {
		return fmt.Errorf("failed to write traefik.yml: %w", err)
	}
	EmitLog(m, "info", fmt.Sprintf("Created traefik.yml at %s", traefikPath))

	// Generate README with migration notes
	readmeContent := e.generateReadme(restAPI, routes, outputDir)
	readmePath := filepath.Join(outputDir, "README.md")
	if err := os.WriteFile(readmePath, []byte(readmeContent), 0644); err != nil {
		EmitLog(m, "warn", fmt.Sprintf("Failed to write README.md: %v", err))
	} else {
		EmitLog(m, "info", fmt.Sprintf("Created README.md with migration notes at %s", readmePath))
	}

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", "API Gateway to Traefik migration completed successfully")

	return nil
}

// generateRouteName creates a valid Traefik route name.
func (e *APIGatewayToTraefikExecutor) generateRouteName(apiName, path, method string, counter int) string {
	// Sanitize the API name
	name := strings.ToLower(apiName)
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.ReplaceAll(name, "_", "-")

	// Sanitize the path
	pathPart := strings.ToLower(path)
	pathPart = strings.ReplaceAll(pathPart, "/", "-")
	pathPart = strings.ReplaceAll(pathPart, "{", "")
	pathPart = strings.ReplaceAll(pathPart, "}", "")
	pathPart = strings.Trim(pathPart, "-")

	if pathPart == "" {
		pathPart = "root"
	}

	return fmt.Sprintf("%s-%s-%s-%d", name, pathPart, strings.ToLower(method), counter)
}

// extractBackendURL extracts the backend URL from an API Gateway integration.
func (e *APIGatewayToTraefikExecutor) extractBackendURL(integration *apiGatewayIntegration) string {
	if integration == nil {
		return "http://backend:8080"
	}

	uri := integration.URI

	// Handle different integration types
	switch integration.Type {
	case "HTTP", "HTTP_PROXY":
		// Direct HTTP integration - use the URI directly
		return uri
	case "AWS_PROXY", "AWS":
		// Lambda or other AWS service integration
		// Extract function name if it's a Lambda ARN
		if strings.Contains(uri, "lambda") {
			// arn:aws:apigateway:{region}:lambda:path/2015-03-31/functions/arn:aws:lambda:{region}:{account}:function:{function-name}/invocations
			parts := strings.Split(uri, "function:")
			if len(parts) > 1 {
				funcPart := parts[1]
				funcPart = strings.TrimSuffix(funcPart, "/invocations")
				// Return a placeholder for Lambda functions
				return fmt.Sprintf("http://%s:8080", funcPart)
			}
		}
		return "http://lambda-backend:8080"
	case "MOCK":
		return "http://mock-backend:8080"
	default:
		return "http://backend:8080"
	}
}

// generateTraefikConfig generates the Traefik dynamic configuration YAML.
func (e *APIGatewayToTraefikExecutor) generateTraefikConfig(routes []traefikRoute, apiName string) string {
	var sb strings.Builder

	sb.WriteString("# Traefik dynamic configuration\n")
	sb.WriteString(fmt.Sprintf("# Migrated from AWS API Gateway: %s\n", apiName))
	sb.WriteString("# Generated by Agnostech\n\n")

	sb.WriteString("http:\n")

	// Generate routers
	sb.WriteString("  routers:\n")
	for _, route := range routes {
		sb.WriteString(fmt.Sprintf("    %s:\n", route.Name))

		// Build the rule
		rule := e.buildTraefikRule(route.Path, route.Methods)
		sb.WriteString(fmt.Sprintf("      rule: \"%s\"\n", rule))

		// Reference the service
		serviceName := e.getServiceName(route.Name)
		sb.WriteString(fmt.Sprintf("      service: %s\n", serviceName))

		// Add entrypoints (default to web)
		sb.WriteString("      entryPoints:\n")
		sb.WriteString("        - web\n")

		sb.WriteString("\n")
	}

	// Generate services
	sb.WriteString("  services:\n")

	// Group routes by service URL to avoid duplicates
	serviceURLs := make(map[string]string)
	for _, route := range routes {
		serviceName := e.getServiceName(route.Name)
		serviceURLs[serviceName] = route.ServiceURL
	}

	for serviceName, serviceURL := range serviceURLs {
		sb.WriteString(fmt.Sprintf("    %s:\n", serviceName))
		sb.WriteString("      loadBalancer:\n")
		sb.WriteString("        servers:\n")
		sb.WriteString(fmt.Sprintf("          - url: \"%s\"\n", serviceURL))
		sb.WriteString("\n")
	}

	return sb.String()
}

// buildTraefikRule builds a Traefik rule string from path and methods.
func (e *APIGatewayToTraefikExecutor) buildTraefikRule(path string, methods []string) string {
	var parts []string

	// Convert API Gateway path parameters to Traefik path matchers
	// {param} in API Gateway -> Traefik regex or PathPrefix
	traefikPath := path
	if strings.Contains(path, "{") {
		// Path has parameters - use PathPrefix for the base and regex for params
		// For simplicity, we use PathPrefix and note that regex may be needed
		basePath := strings.Split(path, "{")[0]
		basePath = strings.TrimSuffix(basePath, "/")
		if basePath == "" {
			basePath = "/"
		}
		traefikPath = basePath
	}

	// Add path matching
	if traefikPath == "/" {
		parts = append(parts, "PathPrefix(`/`)")
	} else {
		parts = append(parts, fmt.Sprintf("PathPrefix(`%s`)", traefikPath))
	}

	// Add method matching
	if len(methods) > 0 && methods[0] != "ANY" {
		methodList := strings.Join(methods, "`, `")
		parts = append(parts, fmt.Sprintf("Method(`%s`)", methodList))
	}

	return strings.Join(parts, " && ")
}

// getServiceName generates a service name from a route name.
func (e *APIGatewayToTraefikExecutor) getServiceName(routeName string) string {
	return routeName + "-service"
}

// generateReadme generates a README with migration notes.
func (e *APIGatewayToTraefikExecutor) generateReadme(api apiGatewayRestAPI, routes []traefikRoute, outputDir string) string {
	var sb strings.Builder

	sb.WriteString("# API Gateway to Traefik Migration\n\n")
	sb.WriteString(fmt.Sprintf("Migrated from AWS API Gateway: **%s** (`%s`)\n\n", api.Name, api.ID))

	if api.Description != "" {
		sb.WriteString(fmt.Sprintf("Description: %s\n\n", api.Description))
	}

	sb.WriteString("## Files Generated\n\n")
	sb.WriteString("- `traefik.yml` - Traefik dynamic configuration with routes and services\n\n")

	sb.WriteString("## Routes Migrated\n\n")
	sb.WriteString("| Method | Path | Backend Service |\n")
	sb.WriteString("|--------|------|----------------|\n")
	for _, route := range routes {
		for _, method := range route.Methods {
			sb.WriteString(fmt.Sprintf("| %s | %s | %s |\n", method, route.Path, route.ServiceURL))
		}
	}
	sb.WriteString("\n")

	sb.WriteString("## Usage\n\n")
	sb.WriteString("1. Review and update the backend service URLs in `traefik.yml`\n")
	sb.WriteString("2. If you have path parameters (e.g., `/users/{id}`), update the Traefik rules to use regex matchers\n")
	sb.WriteString("3. Add the configuration to your Traefik deployment:\n\n")
	sb.WriteString("```yaml\n")
	sb.WriteString("# In your traefik static config or docker-compose.yml\n")
	sb.WriteString("providers:\n")
	sb.WriteString("  file:\n")
	sb.WriteString(fmt.Sprintf("    filename: %s/traefik.yml\n", outputDir))
	sb.WriteString("```\n\n")

	sb.WriteString("## Notes\n\n")
	sb.WriteString("- Lambda integrations are converted to HTTP backends - update the URLs to point to your containerized functions\n")
	sb.WriteString("- Authentication/authorization rules need to be reconfigured in Traefik middleware\n")
	sb.WriteString("- CORS handling should be configured via Traefik middleware if needed\n")
	sb.WriteString("- API key validation needs to be implemented separately\n\n")

	sb.WriteString("---\n")
	sb.WriteString("Generated by Agnostech\n")

	return sb.String()
}
