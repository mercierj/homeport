package datamigration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// AppServiceToDockerExecutor migrates Azure App Service to Docker containers.
type AppServiceToDockerExecutor struct{}

// NewAppServiceToDockerExecutor creates a new App Service to Docker executor.
func NewAppServiceToDockerExecutor() *AppServiceToDockerExecutor {
	return &AppServiceToDockerExecutor{}
}

// Type returns the migration type.
func (e *AppServiceToDockerExecutor) Type() string {
	return "appservice_to_docker"
}

// GetPhases returns the migration phases.
func (e *AppServiceToDockerExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Fetching app configuration",
		"Analyzing app settings",
		"Generating Dockerfile",
		"Creating Docker Compose",
		"Finalizing",
	}
}

// Validate validates the migration configuration.
func (e *AppServiceToDockerExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["app_name"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.app_name is required")
		}
		if _, ok := config.Source["resource_group"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.resource_group is required")
		}
	}

	if config.Destination == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "destination configuration is required")
	} else {
		if _, ok := config.Destination["output_dir"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "destination.output_dir is required")
		}
	}

	result.Warnings = append(result.Warnings, "Application code needs to be downloaded separately from deployment source")
	result.Warnings = append(result.Warnings, "Managed identity connections may need reconfiguration")

	return result, nil
}

// Execute performs the migration.
func (e *AppServiceToDockerExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	appName := config.Source["app_name"].(string)
	resourceGroup := config.Source["resource_group"].(string)
	subscription, _ := config.Source["subscription"].(string)

	outputDir := config.Destination["output_dir"].(string)

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating Azure credentials")
	EmitProgress(m, 10, "Checking credentials")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Fetching app configuration
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", fmt.Sprintf("Fetching App Service configuration for %s", appName))
	EmitProgress(m, 25, "Fetching configuration")

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Get app details
	args := []string{"webapp", "show",
		"--name", appName,
		"--resource-group", resourceGroup,
		"--output", "json",
	}
	if subscription != "" {
		args = append(args, "--subscription", subscription)
	}

	showCmd := exec.CommandContext(ctx, "az", args...)
	appOutput, err := showCmd.Output()
	if err != nil {
		EmitLog(m, "warning", "Could not fetch app info (may require authentication)")
	}

	var appInfo struct {
		Name     string `json:"name"`
		State    string `json:"state"`
		Kind     string `json:"kind"`
		Location string `json:"location"`
		SiteConfig struct {
			LinuxFxVersion   string `json:"linuxFxVersion"`
			WindowsFxVersion string `json:"windowsFxVersion"`
			AppCommandLine   string `json:"appCommandLine"`
			AlwaysOn         bool   `json:"alwaysOn"`
			HttpVersion      string `json:"http20Enabled"`
		} `json:"siteConfig"`
		DefaultHostName string `json:"defaultHostName"`
	}
	if len(appOutput) > 0 {
		json.Unmarshal(appOutput, &appInfo)
	}

	// Save app info
	appInfoPath := filepath.Join(outputDir, "app-info.json")
	if len(appOutput) > 0 {
		if err := os.WriteFile(appInfoPath, appOutput, 0644); err != nil {
			return fmt.Errorf("failed to write app info: %w", err)
		}
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Analyzing app settings
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", "Fetching app settings")
	EmitProgress(m, 40, "Analyzing settings")

	// Get app settings
	settingsArgs := []string{"webapp", "config", "appsettings", "list",
		"--name", appName,
		"--resource-group", resourceGroup,
		"--output", "json",
	}
	if subscription != "" {
		settingsArgs = append(settingsArgs, "--subscription", subscription)
	}

	settingsCmd := exec.CommandContext(ctx, "az", settingsArgs...)
	settingsOutput, _ := settingsCmd.Output()

	var settings []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if len(settingsOutput) > 0 {
		json.Unmarshal(settingsOutput, &settings)
	}

	// Get connection strings
	connStrArgs := []string{"webapp", "config", "connection-string", "list",
		"--name", appName,
		"--resource-group", resourceGroup,
		"--output", "json",
	}
	if subscription != "" {
		connStrArgs = append(connStrArgs, "--subscription", subscription)
	}

	connStrCmd := exec.CommandContext(ctx, "az", connStrArgs...)
	connStrOutput, _ := connStrCmd.Output()

	// Save settings
	settingsPath := filepath.Join(outputDir, "app-settings.json")
	if len(settingsOutput) > 0 {
		if err := os.WriteFile(settingsPath, settingsOutput, 0644); err != nil {
			return fmt.Errorf("failed to write settings: %w", err)
		}
	}

	connStrPath := filepath.Join(outputDir, "connection-strings.json")
	if len(connStrOutput) > 0 {
		if err := os.WriteFile(connStrPath, connStrOutput, 0644); err != nil {
			return fmt.Errorf("failed to write connection strings: %w", err)
		}
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Generating Dockerfile
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Generating Dockerfile")
	EmitProgress(m, 55, "Generating Dockerfile")

	// Detect runtime from LinuxFxVersion
	runtime := appInfo.SiteConfig.LinuxFxVersion
	if runtime == "" {
		runtime = "NODE|18-lts"
	}

	var dockerfile string
	switch {
	case contains(runtime, "NODE"):
		dockerfile = `# Node.js Application
FROM node:18-alpine

WORKDIR /app

# Copy package files
COPY package*.json ./

# Install dependencies
RUN npm ci --only=production

# Copy application code
COPY . .

# Expose port
EXPOSE 3000

# Start command
CMD ["npm", "start"]
`
	case contains(runtime, "PYTHON"):
		dockerfile = `# Python Application
FROM python:3.11-slim

WORKDIR /app

# Copy requirements
COPY requirements.txt ./

# Install dependencies
RUN pip install --no-cache-dir -r requirements.txt

# Copy application code
COPY . .

# Expose port
EXPOSE 8000

# Start command (adjust for your framework)
CMD ["gunicorn", "--bind", "0.0.0.0:8000", "app:app"]
`
	case contains(runtime, "DOTNET"):
		dockerfile = `# .NET Application
FROM mcr.microsoft.com/dotnet/aspnet:7.0 AS base
WORKDIR /app
EXPOSE 80

FROM mcr.microsoft.com/dotnet/sdk:7.0 AS build
WORKDIR /src
COPY . .
RUN dotnet restore
RUN dotnet build -c Release -o /app/build

FROM build AS publish
RUN dotnet publish -c Release -o /app/publish

FROM base AS final
WORKDIR /app
COPY --from=publish /app/publish .
ENTRYPOINT ["dotnet", "YourApp.dll"]
`
	case contains(runtime, "JAVA"):
		dockerfile = `# Java Application
FROM eclipse-temurin:17-jre-alpine

WORKDIR /app

# Copy JAR file
COPY target/*.jar app.jar

# Expose port
EXPOSE 8080

# Start command
CMD ["java", "-jar", "app.jar"]
`
	case contains(runtime, "PHP"):
		dockerfile = `# PHP Application
FROM php:8.2-apache

# Enable Apache modules
RUN a2enmod rewrite

# Install PHP extensions
RUN docker-php-ext-install pdo pdo_mysql

# Copy application code
COPY . /var/www/html/

# Set permissions
RUN chown -R www-data:www-data /var/www/html

EXPOSE 80
`
	default:
		dockerfile = `# Generic Web Application
FROM nginx:alpine

# Copy static files
COPY . /usr/share/nginx/html/

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
`
	}

	dockerfilePath := filepath.Join(outputDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0644); err != nil {
		return fmt.Errorf("failed to write Dockerfile: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Creating Docker Compose
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Creating Docker Compose configuration")
	EmitProgress(m, 75, "Creating Docker Compose")

	// Generate environment variables from settings
	var envVars string
	for _, s := range settings {
		envVars += fmt.Sprintf("      %s: \"%s\"\n", s.Name, s.Value)
	}

	dockerCompose := fmt.Sprintf(`version: '3.8'

services:
  app:
    build: .
    container_name: %s
    environment:
%s    ports:
      - "8080:80"
    restart: unless-stopped

  # Uncomment if you need a reverse proxy
  # traefik:
  #   image: traefik:v2.9
  #   container_name: traefik
  #   command:
  #     - "--api.insecure=true"
  #     - "--providers.docker=true"
  #   ports:
  #     - "80:80"
  #     - "8081:8080"
  #   volumes:
  #     - /var/run/docker.sock:/var/run/docker.sock
`, appName, envVars)

	composePath := filepath.Join(outputDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(dockerCompose), 0644); err != nil {
		return fmt.Errorf("failed to write docker-compose.yml: %w", err)
	}

	// Generate .env file
	var envContent string
	for _, s := range settings {
		envContent += fmt.Sprintf("%s=%s\n", s.Name, s.Value)
	}
	envPath := filepath.Join(outputDir, ".env.example")
	if err := os.WriteFile(envPath, []byte(envContent), 0644); err != nil {
		return fmt.Errorf("failed to write .env.example: %w", err)
	}

	// Generate download script
	downloadScript := fmt.Sprintf(`#!/bin/bash
# Download App Service Code
# App: %s

set -e

echo "Downloading App Service code..."

# Option 1: Download deployment content
az webapp deployment source download \
    --name %s \
    --resource-group %s \
    --output-folder ./app-code

# Option 2: If using Git deployment, clone from source
# git clone <your-repo-url> ./app-code

echo ""
echo "Code downloaded to ./app-code"
echo "Copy the contents to this directory and build with:"
echo "  docker-compose build"
echo "  docker-compose up -d"
`, appName, appName, resourceGroup)

	downloadPath := filepath.Join(outputDir, "download-code.sh")
	if err := os.WriteFile(downloadPath, []byte(downloadScript), 0755); err != nil {
		return fmt.Errorf("failed to write download script: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 6: Finalizing
	EmitPhase(m, phases[5], 6)
	EmitLog(m, "info", "Finalizing migration artifacts")
	EmitProgress(m, 90, "Finalizing")

	readme := fmt.Sprintf(`# App Service to Docker Migration

## Source App Service
- App Name: %s
- Resource Group: %s
- Runtime: %s
- Location: %s

## Migration Steps

1. Download your application code:
'''bash
./download-code.sh
# Or clone from your source repository
'''

2. Copy application code to this directory

3. Review and adjust Dockerfile for your specific needs

4. Copy .env.example to .env and update values:
'''bash
cp .env.example .env
'''

5. Build and run:
'''bash
docker-compose build
docker-compose up -d
'''

## Files Generated
- app-info.json: App Service configuration
- app-settings.json: Application settings
- connection-strings.json: Connection strings
- Dockerfile: Container configuration
- docker-compose.yml: Container orchestration
- .env.example: Environment variables template
- download-code.sh: Code download script

## Notes
- Review Dockerfile and adjust for your specific runtime
- Update connection strings for local services
- Managed identity connections need to be replaced
- Review any Azure-specific SDKs in your application
`, appName, resourceGroup, runtime, appInfo.Location)

	readmePath := filepath.Join(outputDir, "README.md")
	if err := os.WriteFile(readmePath, []byte(readme), 0644); err != nil {
		return fmt.Errorf("failed to write README: %w", err)
	}

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", fmt.Sprintf("App Service %s migration prepared at %s", appName, outputDir))

	return nil
}

// FunctionsToOpenFaaSExecutor migrates Azure Functions to OpenFaaS.
type FunctionsToOpenFaaSExecutor struct{}

// NewFunctionsToOpenFaaSExecutor creates a new Functions to OpenFaaS executor.
func NewFunctionsToOpenFaaSExecutor() *FunctionsToOpenFaaSExecutor {
	return &FunctionsToOpenFaaSExecutor{}
}

// Type returns the migration type.
func (e *FunctionsToOpenFaaSExecutor) Type() string {
	return "functions_to_openfaas"
}

// GetPhases returns the migration phases.
func (e *FunctionsToOpenFaaSExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Fetching function app info",
		"Analyzing functions",
		"Generating OpenFaaS templates",
		"Creating deployment configuration",
		"Finalizing",
	}
}

// Validate validates the migration configuration.
func (e *FunctionsToOpenFaaSExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["function_app"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.function_app is required")
		}
		if _, ok := config.Source["resource_group"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.resource_group is required")
		}
	}

	if config.Destination == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "destination configuration is required")
	} else {
		if _, ok := config.Destination["output_dir"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "destination.output_dir is required")
		}
	}

	result.Warnings = append(result.Warnings, "Azure bindings need to be replaced with HTTP triggers")
	result.Warnings = append(result.Warnings, "Durable Functions require alternative orchestration")

	return result, nil
}

// Execute performs the migration.
func (e *FunctionsToOpenFaaSExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	functionApp := config.Source["function_app"].(string)
	resourceGroup := config.Source["resource_group"].(string)
	subscription, _ := config.Source["subscription"].(string)

	outputDir := config.Destination["output_dir"].(string)

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating Azure credentials")
	EmitProgress(m, 10, "Checking credentials")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Fetching function app info
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", fmt.Sprintf("Fetching Function App info for %s", functionApp))
	EmitProgress(m, 25, "Fetching function info")

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Get function app details
	args := []string{"functionapp", "show",
		"--name", functionApp,
		"--resource-group", resourceGroup,
		"--output", "json",
	}
	if subscription != "" {
		args = append(args, "--subscription", subscription)
	}

	showCmd := exec.CommandContext(ctx, "az", args...)
	funcOutput, _ := showCmd.Output()

	var funcInfo struct {
		Name     string `json:"name"`
		State    string `json:"state"`
		Kind     string `json:"kind"`
		Location string `json:"location"`
		SiteConfig struct {
			LinuxFxVersion string `json:"linuxFxVersion"`
		} `json:"siteConfig"`
	}
	if len(funcOutput) > 0 {
		json.Unmarshal(funcOutput, &funcInfo)
	}

	// Save function info
	funcInfoPath := filepath.Join(outputDir, "function-info.json")
	if len(funcOutput) > 0 {
		if err := os.WriteFile(funcInfoPath, funcOutput, 0644); err != nil {
			return fmt.Errorf("failed to write function info: %w", err)
		}
	}

	// Get list of functions
	funcListArgs := []string{"functionapp", "function", "list",
		"--name", functionApp,
		"--resource-group", resourceGroup,
		"--output", "json",
	}
	if subscription != "" {
		funcListArgs = append(funcListArgs, "--subscription", subscription)
	}

	funcListCmd := exec.CommandContext(ctx, "az", funcListArgs...)
	funcListOutput, _ := funcListCmd.Output()

	var functions []struct {
		Name      string `json:"name"`
		Language  string `json:"language"`
		IsDisabled bool  `json:"isDisabled"`
	}
	if len(funcListOutput) > 0 {
		json.Unmarshal(funcListOutput, &functions)
	}

	// Save functions list
	funcListPath := filepath.Join(outputDir, "functions-list.json")
	if len(funcListOutput) > 0 {
		if err := os.WriteFile(funcListPath, funcListOutput, 0644); err != nil {
			return fmt.Errorf("failed to write functions list: %w", err)
		}
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Analyzing functions
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", "Analyzing function configurations")
	EmitProgress(m, 40, "Analyzing functions")

	// Get app settings
	settingsArgs := []string{"functionapp", "config", "appsettings", "list",
		"--name", functionApp,
		"--resource-group", resourceGroup,
		"--output", "json",
	}
	if subscription != "" {
		settingsArgs = append(settingsArgs, "--subscription", subscription)
	}

	settingsCmd := exec.CommandContext(ctx, "az", settingsArgs...)
	settingsOutput, _ := settingsCmd.Output()

	settingsPath := filepath.Join(outputDir, "app-settings.json")
	if len(settingsOutput) > 0 {
		if err := os.WriteFile(settingsPath, settingsOutput, 0644); err != nil {
			return fmt.Errorf("failed to write settings: %w", err)
		}
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Generating OpenFaaS templates
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Generating OpenFaaS templates")
	EmitProgress(m, 55, "Generating templates")

	// Create functions directory
	functionsDir := filepath.Join(outputDir, "functions")
	if err := os.MkdirAll(functionsDir, 0755); err != nil {
		return fmt.Errorf("failed to create functions directory: %w", err)
	}

	// Detect runtime
	runtime := funcInfo.SiteConfig.LinuxFxVersion
	if runtime == "" {
		runtime = "NODE|18"
	}

	var templateLang, handler, dockerfile string
	switch {
	case contains(runtime, "NODE"):
		templateLang = "node"
		handler = "handler.js"
		dockerfile = `FROM ghcr.io/openfaas/of-watchdog:0.9.10 as watchdog
FROM node:18-alpine

COPY --from=watchdog /fwatchdog /usr/bin/fwatchdog
RUN chmod +x /usr/bin/fwatchdog

WORKDIR /home/app

COPY package*.json ./
RUN npm ci --only=production

COPY . .

ENV fprocess="node handler.js"
ENV mode="http"
ENV upstream_url="http://127.0.0.1:3000"

HEALTHCHECK --interval=3s CMD [ -e /tmp/.lock ] || exit 1

CMD ["fwatchdog"]
`
	case contains(runtime, "PYTHON"):
		templateLang = "python"
		handler = "handler.py"
		dockerfile = `FROM ghcr.io/openfaas/of-watchdog:0.9.10 as watchdog
FROM python:3.11-slim

COPY --from=watchdog /fwatchdog /usr/bin/fwatchdog
RUN chmod +x /usr/bin/fwatchdog

WORKDIR /home/app

COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt

COPY . .

ENV fprocess="python handler.py"
ENV mode="http"
ENV upstream_url="http://127.0.0.1:5000"

HEALTHCHECK --interval=3s CMD [ -e /tmp/.lock ] || exit 1

CMD ["fwatchdog"]
`
	default:
		templateLang = "node"
		handler = "handler.js"
		dockerfile = `FROM ghcr.io/openfaas/of-watchdog:0.9.10 as watchdog
FROM node:18-alpine

COPY --from=watchdog /fwatchdog /usr/bin/fwatchdog
RUN chmod +x /usr/bin/fwatchdog

WORKDIR /home/app

COPY . .
RUN npm ci --only=production 2>/dev/null || true

ENV fprocess="node handler.js"
ENV mode="http"
ENV upstream_url="http://127.0.0.1:3000"

HEALTHCHECK --interval=3s CMD [ -e /tmp/.lock ] || exit 1

CMD ["fwatchdog"]
`
	}

	// Generate sample function for each Azure function
	for _, f := range functions {
		if f.IsDisabled {
			continue
		}
		funcDir := filepath.Join(functionsDir, f.Name)
		if err := os.MkdirAll(funcDir, 0755); err != nil {
			continue
		}

		// Write Dockerfile
		dfPath := filepath.Join(funcDir, "Dockerfile")
		os.WriteFile(dfPath, []byte(dockerfile), 0644)

		// Write handler template
		var handlerContent string
		if templateLang == "node" {
			handlerContent = fmt.Sprintf(`// OpenFaaS handler for %s
// Migrated from Azure Functions

const http = require('http');

const server = http.createServer((req, res) => {
    let body = '';

    req.on('data', chunk => {
        body += chunk.toString();
    });

    req.on('end', () => {
        // Your function logic here
        const result = handle(body, req.headers);

        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify(result));
    });
});

function handle(body, headers) {
    // TODO: Migrate your Azure Function logic here
    console.log('Received:', body);

    return {
        message: 'Hello from %s',
        timestamp: new Date().toISOString()
    };
}

server.listen(3000, () => {
    console.log('Function listening on port 3000');
});
`, f.Name, f.Name)
		} else {
			handlerContent = fmt.Sprintf(`# OpenFaaS handler for %s
# Migrated from Azure Functions

from flask import Flask, request, jsonify

app = Flask(__name__)

@app.route('/', methods=['GET', 'POST'])
def handle():
    # Your function logic here
    body = request.get_data(as_text=True)
    headers = dict(request.headers)

    # TODO: Migrate your Azure Function logic here
    print(f'Received: {body}')

    return jsonify({
        'message': 'Hello from %s',
        'timestamp': __import__('datetime').datetime.now().isoformat()
    })

if __name__ == '__main__':
    app.run(host='0.0.0.0', port=5000)
`, f.Name, f.Name)
		}
		handlerPath := filepath.Join(funcDir, handler)
		os.WriteFile(handlerPath, []byte(handlerContent), 0644)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Creating deployment configuration
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Creating deployment configuration")
	EmitProgress(m, 75, "Creating deployment config")

	// Generate stack.yml for OpenFaaS
	var functionsYaml string
	for _, f := range functions {
		if f.IsDisabled {
			continue
		}
		functionsYaml += fmt.Sprintf(`  %s:
    lang: dockerfile
    handler: ./functions/%s
    image: ${REGISTRY}/%s:latest
`, f.Name, f.Name, f.Name)
	}

	stackYaml := fmt.Sprintf(`version: 1.0
provider:
  name: openfaas
  gateway: http://127.0.0.1:8080

functions:
%s`, functionsYaml)

	stackPath := filepath.Join(outputDir, "stack.yml")
	if err := os.WriteFile(stackPath, []byte(stackYaml), 0644); err != nil {
		return fmt.Errorf("failed to write stack.yml: %w", err)
	}

	// Generate Docker Compose for OpenFaaS
	dockerCompose := `version: '3.8'

services:
  gateway:
    image: ghcr.io/openfaas/gateway:latest
    container_name: gateway
    environment:
      functions_provider_url: "http://faas-swarm:8080/"
      read_timeout: "5m"
      write_timeout: "5m"
      upstream_timeout: "5m5s"
      basic_auth: "true"
      secret_mount_path: "/run/secrets/"
    volumes:
      - ./credentials:/run/secrets
    ports:
      - "8080:8080"
    depends_on:
      - faas-swarm
    restart: unless-stopped

  faas-swarm:
    image: ghcr.io/openfaas/faas-swarm:latest
    container_name: faas-swarm
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    environment:
      read_timeout: "5m"
      write_timeout: "5m"
    restart: unless-stopped

  nats:
    image: nats-streaming:0.24.5
    container_name: nats
    command: "--store memory --cluster_id faas-cluster"
    ports:
      - "4222:4222"
    restart: unless-stopped

  queue-worker:
    image: ghcr.io/openfaas/queue-worker:latest
    container_name: queue-worker
    environment:
      faas_nats_address: "nats"
      faas_nats_channel: "faas-request"
      faas_gateway_address: "gateway"
      max_inflight: "1"
    depends_on:
      - nats
      - gateway
    restart: unless-stopped

  prometheus:
    image: prom/prometheus:v2.40.0
    container_name: prometheus
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml
    ports:
      - "9090:9090"
    restart: unless-stopped
`
	composeOpenfaasPath := filepath.Join(outputDir, "docker-compose.yml")
	if err := os.WriteFile(composeOpenfaasPath, []byte(dockerCompose), 0644); err != nil {
		return fmt.Errorf("failed to write docker-compose.yml: %w", err)
	}

	// Generate Prometheus config
	prometheusYml := `global:
  scrape_interval: 15s
  evaluation_interval: 15s

scrape_configs:
  - job_name: 'openfaas'
    static_configs:
      - targets: ['gateway:8080']
`
	prometheusPath := filepath.Join(outputDir, "prometheus.yml")
	if err := os.WriteFile(prometheusPath, []byte(prometheusYml), 0644); err != nil {
		return fmt.Errorf("failed to write prometheus.yml: %w", err)
	}

	// Create credentials directory
	credentialsDir := filepath.Join(outputDir, "credentials")
	os.MkdirAll(credentialsDir, 0755)
	os.WriteFile(filepath.Join(credentialsDir, "basic-auth-user"), []byte("admin"), 0600)
	os.WriteFile(filepath.Join(credentialsDir, "basic-auth-password"), []byte("changeme"), 0600)

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 6: Finalizing
	EmitPhase(m, phases[5], 6)
	EmitLog(m, "info", "Finalizing migration artifacts")
	EmitProgress(m, 90, "Finalizing")

	readme := fmt.Sprintf(`# Azure Functions to OpenFaaS Migration

## Source Function App
- Function App: %s
- Resource Group: %s
- Runtime: %s
- Functions: %d

## Migration Steps

1. Start OpenFaaS:
'''bash
docker-compose up -d
'''

2. Install faas-cli:
'''bash
curl -sL https://cli.openfaas.com | sudo sh
'''

3. Login to OpenFaaS:
'''bash
faas-cli login --password changeme
'''

4. Build and deploy functions:
'''bash
export REGISTRY=your-registry
faas-cli build -f stack.yml
faas-cli push -f stack.yml
faas-cli deploy -f stack.yml
'''

## Files Generated
- function-info.json: Function app configuration
- functions-list.json: List of functions
- app-settings.json: Application settings
- functions/: Function templates (one per Azure function)
- stack.yml: OpenFaaS stack configuration
- docker-compose.yml: OpenFaaS deployment
- prometheus.yml: Metrics configuration

## Access
- OpenFaaS Gateway: http://localhost:8080
- Prometheus: http://localhost:9090
- Default credentials: admin / changeme

## Notes
- Azure bindings (Blob, Queue, etc.) need HTTP adapters
- Review each function handler and migrate logic
- Durable Functions need alternative orchestration (Temporal, etc.)
- Update credentials before production use
`, functionApp, resourceGroup, runtime, len(functions))

	readmePath := filepath.Join(outputDir, "README.md")
	if err := os.WriteFile(readmePath, []byte(readme), 0644); err != nil {
		return fmt.Errorf("failed to write README: %w", err)
	}

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", fmt.Sprintf("Azure Functions %s migration prepared at %s", functionApp, outputDir))

	return nil
}

// AKSToK3sExecutor migrates Azure Kubernetes Service to K3s.
type AKSToK3sExecutor struct{}

// NewAKSToK3sExecutor creates a new AKS to K3s executor.
func NewAKSToK3sExecutor() *AKSToK3sExecutor {
	return &AKSToK3sExecutor{}
}

// Type returns the migration type.
func (e *AKSToK3sExecutor) Type() string {
	return "aks_to_k3s"
}

// GetPhases returns the migration phases.
func (e *AKSToK3sExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Fetching AKS configuration",
		"Exporting Kubernetes resources",
		"Generating K3s configuration",
		"Creating migration scripts",
		"Finalizing",
	}
}

// Validate validates the migration configuration.
func (e *AKSToK3sExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["cluster_name"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.cluster_name is required")
		}
		if _, ok := config.Source["resource_group"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.resource_group is required")
		}
	}

	if config.Destination == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "destination configuration is required")
	} else {
		if _, ok := config.Destination["output_dir"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "destination.output_dir is required")
		}
	}

	result.Warnings = append(result.Warnings, "Azure-specific resources (Azure Disk, Azure File) need alternatives")
	result.Warnings = append(result.Warnings, "AAD integration requires replacement")
	result.Warnings = append(result.Warnings, "Ingress controllers may need reconfiguration")

	return result, nil
}

// Execute performs the migration.
func (e *AKSToK3sExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	clusterName := config.Source["cluster_name"].(string)
	resourceGroup := config.Source["resource_group"].(string)
	subscription, _ := config.Source["subscription"].(string)
	namespaces, _ := config.Source["namespaces"].([]interface{})

	outputDir := config.Destination["output_dir"].(string)

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating Azure credentials")
	EmitProgress(m, 10, "Checking credentials")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Fetching AKS configuration
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", fmt.Sprintf("Fetching AKS configuration for %s", clusterName))
	EmitProgress(m, 25, "Fetching configuration")

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Get AKS credentials
	credArgs := []string{"aks", "get-credentials",
		"--name", clusterName,
		"--resource-group", resourceGroup,
		"--overwrite-existing",
	}
	if subscription != "" {
		credArgs = append(credArgs, "--subscription", subscription)
	}

	credCmd := exec.CommandContext(ctx, "az", credArgs...)
	credCmd.Run() // Get credentials for kubectl

	// Get cluster info
	showArgs := []string{"aks", "show",
		"--name", clusterName,
		"--resource-group", resourceGroup,
		"--output", "json",
	}
	if subscription != "" {
		showArgs = append(showArgs, "--subscription", subscription)
	}

	showCmd := exec.CommandContext(ctx, "az", showArgs...)
	clusterOutput, _ := showCmd.Output()

	var clusterInfo struct {
		Name              string `json:"name"`
		Location          string `json:"location"`
		KubernetesVersion string `json:"kubernetesVersion"`
		AgentPoolProfiles []struct {
			Name   string `json:"name"`
			Count  int    `json:"count"`
			VMSize string `json:"vmSize"`
		} `json:"agentPoolProfiles"`
	}
	if len(clusterOutput) > 0 {
		json.Unmarshal(clusterOutput, &clusterInfo)
	}

	// Save cluster info
	clusterInfoPath := filepath.Join(outputDir, "cluster-info.json")
	if len(clusterOutput) > 0 {
		if err := os.WriteFile(clusterInfoPath, clusterOutput, 0644); err != nil {
			return fmt.Errorf("failed to write cluster info: %w", err)
		}
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Exporting Kubernetes resources
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", "Exporting Kubernetes resources")
	EmitProgress(m, 45, "Exporting resources")

	manifestsDir := filepath.Join(outputDir, "manifests")
	if err := os.MkdirAll(manifestsDir, 0755); err != nil {
		return fmt.Errorf("failed to create manifests directory: %w", err)
	}

	// Determine namespaces to export
	var nsToExport []string
	if len(namespaces) > 0 {
		for _, ns := range namespaces {
			if s, ok := ns.(string); ok {
				nsToExport = append(nsToExport, s)
			}
		}
	} else {
		// Get all namespaces except system ones
		nsCmd := exec.CommandContext(ctx, "kubectl", "get", "namespaces", "-o", "jsonpath={.items[*].metadata.name}")
		nsOutput, _ := nsCmd.Output()
		for _, ns := range splitString(string(nsOutput)) {
			if ns != "" && ns != "kube-system" && ns != "kube-public" && ns != "kube-node-lease" {
				nsToExport = append(nsToExport, ns)
			}
		}
	}

	// Export resources from each namespace
	resourceTypes := []string{"deployments", "services", "configmaps", "secrets", "ingresses", "persistentvolumeclaims"}
	for _, ns := range nsToExport {
		nsDir := filepath.Join(manifestsDir, ns)
		os.MkdirAll(nsDir, 0755)

		for _, rt := range resourceTypes {
			exportCmd := exec.CommandContext(ctx, "kubectl", "get", rt, "-n", ns, "-o", "yaml")
			output, err := exportCmd.Output()
			if err == nil && len(output) > 0 {
				filePath := filepath.Join(nsDir, fmt.Sprintf("%s.yaml", rt))
				os.WriteFile(filePath, output, 0644)
			}
		}
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Generating K3s configuration
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Generating K3s configuration")
	EmitProgress(m, 65, "Generating K3s config")

	// Generate K3s installation script
	k3sInstall := `#!/bin/bash
# K3s Installation Script

set -e

echo "Installing K3s..."

# Install K3s server
curl -sfL https://get.k3s.io | sh -s - \
    --disable traefik \
    --write-kubeconfig-mode 644

# Wait for K3s to be ready
echo "Waiting for K3s to be ready..."
until kubectl get nodes 2>/dev/null | grep -q "Ready"; do
    sleep 2
done

echo "K3s installed successfully!"
echo ""
echo "To add worker nodes, run on each worker:"
echo "  curl -sfL https://get.k3s.io | K3S_URL=https://<server-ip>:6443 K3S_TOKEN=<token> sh -"
echo ""
echo "Get the token with: sudo cat /var/lib/rancher/k3s/server/node-token"
`
	k3sInstallPath := filepath.Join(outputDir, "install-k3s.sh")
	if err := os.WriteFile(k3sInstallPath, []byte(k3sInstall), 0755); err != nil {
		return fmt.Errorf("failed to write install script: %w", err)
	}

	// Generate Docker Compose for K3d (local K3s)
	k3dCompose := `version: '3.8'

# For local development, use k3d (K3s in Docker)
# Install k3d: curl -s https://raw.githubusercontent.com/k3d-io/k3d/main/install.sh | bash

# Create cluster:
# k3d cluster create mycluster --servers 1 --agents 2 -p "8080:80@loadbalancer" -p "8443:443@loadbalancer"

# This docker-compose is for supporting services

services:
  registry:
    image: registry:2
    container_name: registry
    ports:
      - "5000:5000"
    volumes:
      - registry-data:/var/lib/registry
    restart: unless-stopped

volumes:
  registry-data:
`
	k3dComposePath := filepath.Join(outputDir, "docker-compose.yml")
	if err := os.WriteFile(k3dComposePath, []byte(k3dCompose), 0644); err != nil {
		return fmt.Errorf("failed to write docker-compose.yml: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Creating migration scripts
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Creating migration scripts")
	EmitProgress(m, 80, "Creating scripts")

	// Generate apply script
	applyScript := fmt.Sprintf(`#!/bin/bash
# Apply Kubernetes manifests to K3s

set -e

echo "Applying Kubernetes manifests to K3s..."

# Ensure kubectl is pointing to K3s
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml

# Create namespaces first
%s

# Apply manifests
for ns_dir in ./manifests/*/; do
    ns=$(basename "$ns_dir")
    echo "Applying resources in namespace: $ns"

    # Create namespace if not exists
    kubectl create namespace "$ns" --dry-run=client -o yaml | kubectl apply -f -

    # Apply all resources
    for manifest in "$ns_dir"*.yaml; do
        if [ -f "$manifest" ]; then
            echo "  Applying: $manifest"
            kubectl apply -f "$manifest" -n "$ns" || true
        fi
    done
done

echo ""
echo "Migration complete!"
echo "Check status: kubectl get all --all-namespaces"
`, generateNamespaceCreation(nsToExport))

	applyScriptPath := filepath.Join(outputDir, "apply-manifests.sh")
	if err := os.WriteFile(applyScriptPath, []byte(applyScript), 0755); err != nil {
		return fmt.Errorf("failed to write apply script: %w", err)
	}

	// Generate storage migration guide
	storageGuide := `# Storage Migration Guide

## Azure Disk to Local Storage

Azure Disk PVCs need to be migrated to local storage or a different storage provider.

### Option 1: Local Path Provisioner (Default in K3s)
K3s includes local-path-provisioner by default. Update your PVCs:

'''yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: my-pvc
spec:
  accessModes:
    - ReadWriteOnce
  storageClassName: local-path
  resources:
    requests:
      storage: 10Gi
'''

### Option 2: Longhorn (Distributed Storage)
Install Longhorn for replicated storage:
'''bash
kubectl apply -f https://raw.githubusercontent.com/longhorn/longhorn/v1.4.0/deploy/longhorn.yaml
'''

### Option 3: NFS
Deploy NFS provisioner:
'''bash
helm install nfs-subdir-external-provisioner nfs-subdir-external-provisioner/nfs-subdir-external-provisioner \
    --set nfs.server=<nfs-server-ip> \
    --set nfs.path=/exported/path
'''

## Azure File to NFS/SMB

Replace Azure File shares with NFS or SMB mounts.

## Data Migration

1. Scale down workloads using the storage
2. Copy data from Azure to new storage
3. Update PVC definitions
4. Apply new PVCs
5. Scale up workloads
`
	storageGuidePath := filepath.Join(outputDir, "STORAGE_MIGRATION.md")
	if err := os.WriteFile(storageGuidePath, []byte(storageGuide), 0644); err != nil {
		return fmt.Errorf("failed to write storage guide: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 6: Finalizing
	EmitPhase(m, phases[5], 6)
	EmitLog(m, "info", "Finalizing migration artifacts")
	EmitProgress(m, 90, "Finalizing")

	nodeCount := 0
	if len(clusterInfo.AgentPoolProfiles) > 0 {
		for _, ap := range clusterInfo.AgentPoolProfiles {
			nodeCount += ap.Count
		}
	}

	readme := fmt.Sprintf(`# AKS to K3s Migration

## Source Cluster
- Cluster Name: %s
- Resource Group: %s
- Kubernetes Version: %s
- Node Count: %d
- Location: %s

## Migration Steps

### Option A: Production (Bare Metal/VM)

1. Install K3s:
'''bash
./install-k3s.sh
'''

2. Apply manifests:
'''bash
./apply-manifests.sh
'''

### Option B: Local Development (k3d)

1. Install k3d:
'''bash
curl -s https://raw.githubusercontent.com/k3d-io/k3d/main/install.sh | bash
'''

2. Create cluster:
'''bash
k3d cluster create mycluster --servers 1 --agents 2 \
    -p "8080:80@loadbalancer" \
    -p "8443:443@loadbalancer"
'''

3. Apply manifests:
'''bash
./apply-manifests.sh
'''

## Files Generated
- cluster-info.json: AKS cluster configuration
- manifests/: Exported Kubernetes resources by namespace
- install-k3s.sh: K3s installation script
- apply-manifests.sh: Manifest application script
- docker-compose.yml: Supporting services
- STORAGE_MIGRATION.md: Storage migration guide

## Important Considerations
- Review STORAGE_MIGRATION.md for storage changes
- Update ingress configurations
- Replace Azure-specific annotations
- Update image references if using ACR
- Review service types (LoadBalancer may need NodePort)

## Exported Namespaces
%s
`, clusterName, resourceGroup, clusterInfo.KubernetesVersion, nodeCount, clusterInfo.Location, formatNamespaceList(nsToExport))

	readmePath := filepath.Join(outputDir, "README.md")
	if err := os.WriteFile(readmePath, []byte(readme), 0644); err != nil {
		return fmt.Errorf("failed to write README: %w", err)
	}

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", fmt.Sprintf("AKS %s migration prepared at %s", clusterName, outputDir))

	return nil
}

// ACIToDockerExecutor migrates Azure Container Instances to Docker.
type ACIToDockerExecutor struct{}

// NewACIToDockerExecutor creates a new ACI to Docker executor.
func NewACIToDockerExecutor() *ACIToDockerExecutor {
	return &ACIToDockerExecutor{}
}

// Type returns the migration type.
func (e *ACIToDockerExecutor) Type() string {
	return "aci_to_docker"
}

// GetPhases returns the migration phases.
func (e *ACIToDockerExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Fetching container group info",
		"Analyzing container configuration",
		"Generating Docker Compose",
		"Finalizing",
	}
}

// Validate validates the migration configuration.
func (e *ACIToDockerExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["container_group"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.container_group is required")
		}
		if _, ok := config.Source["resource_group"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.resource_group is required")
		}
	}

	if config.Destination == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "destination configuration is required")
	} else {
		if _, ok := config.Destination["output_dir"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "destination.output_dir is required")
		}
	}

	result.Warnings = append(result.Warnings, "ACR image references may need updating")
	result.Warnings = append(result.Warnings, "Azure File mounts need local alternatives")

	return result, nil
}

// Execute performs the migration.
func (e *ACIToDockerExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	containerGroup := config.Source["container_group"].(string)
	resourceGroup := config.Source["resource_group"].(string)
	subscription, _ := config.Source["subscription"].(string)

	outputDir := config.Destination["output_dir"].(string)

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating Azure credentials")
	EmitProgress(m, 10, "Checking credentials")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Fetching container group info
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", fmt.Sprintf("Fetching container group info for %s", containerGroup))
	EmitProgress(m, 30, "Fetching container info")

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Get container group details
	args := []string{"container", "show",
		"--name", containerGroup,
		"--resource-group", resourceGroup,
		"--output", "json",
	}
	if subscription != "" {
		args = append(args, "--subscription", subscription)
	}

	showCmd := exec.CommandContext(ctx, "az", args...)
	aciOutput, _ := showCmd.Output()

	var aciInfo struct {
		Name       string `json:"name"`
		Location   string `json:"location"`
		OsType     string `json:"osType"`
		IPAddress  struct {
			IP    string `json:"ip"`
			Ports []struct {
				Port     int    `json:"port"`
				Protocol string `json:"protocol"`
			} `json:"ports"`
		} `json:"ipAddress"`
		Containers []struct {
			Name       string `json:"name"`
			Image      string `json:"image"`
			Resources  struct {
				Requests struct {
					CPU        float64 `json:"cpu"`
					MemoryInGB float64 `json:"memoryInGb"`
				} `json:"requests"`
			} `json:"resources"`
			Ports []struct {
				Port int `json:"port"`
			} `json:"ports"`
			EnvironmentVariables []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"environmentVariables"`
			Command []string `json:"command"`
		} `json:"containers"`
		Volumes []struct {
			Name      string `json:"name"`
			AzureFile struct {
				ShareName          string `json:"shareName"`
				StorageAccountName string `json:"storageAccountName"`
			} `json:"azureFile"`
		} `json:"volumes"`
	}
	if len(aciOutput) > 0 {
		json.Unmarshal(aciOutput, &aciInfo)
	}

	// Save container group info
	aciInfoPath := filepath.Join(outputDir, "container-group-info.json")
	if len(aciOutput) > 0 {
		if err := os.WriteFile(aciInfoPath, aciOutput, 0644); err != nil {
			return fmt.Errorf("failed to write container group info: %w", err)
		}
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Analyzing container configuration
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", "Analyzing container configuration")
	EmitProgress(m, 50, "Analyzing configuration")

	// Generate environment files for each container
	for _, container := range aciInfo.Containers {
		envContent := ""
		for _, env := range container.EnvironmentVariables {
			envContent += fmt.Sprintf("%s=%s\n", env.Name, env.Value)
		}
		if envContent != "" {
			envPath := filepath.Join(outputDir, fmt.Sprintf("%s.env", container.Name))
			os.WriteFile(envPath, []byte(envContent), 0644)
		}
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Generating Docker Compose
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Generating Docker Compose")
	EmitProgress(m, 75, "Generating Docker Compose")

	// Build Docker Compose services
	var services string
	var volumes string

	for _, container := range aciInfo.Containers {
		// Build ports
		var ports string
		for _, port := range container.Ports {
			ports += fmt.Sprintf("      - \"%d:%d\"\n", port.Port, port.Port)
		}

		// Build environment
		var envVars string
		for _, env := range container.EnvironmentVariables {
			envVars += fmt.Sprintf("      %s: \"%s\"\n", env.Name, env.Value)
		}

		// Build command
		var command string
		if len(container.Command) > 0 {
			command = fmt.Sprintf("    command: %v\n", container.Command)
		}

		// Calculate resources
		cpuLimit := container.Resources.Requests.CPU
		memLimit := container.Resources.Requests.MemoryInGB

		services += fmt.Sprintf(`  %s:
    image: %s
    container_name: %s
%s    ports:
%s    environment:
%s    deploy:
      resources:
        limits:
          cpus: "%.1f"
          memory: %.0fG
    restart: unless-stopped

`, container.Name, container.Image, container.Name, command, ports, envVars, cpuLimit, memLimit)
	}

	// Build volumes
	for _, vol := range aciInfo.Volumes {
		volumes += fmt.Sprintf(`  %s:
    driver: local
`, vol.Name)
	}

	dockerCompose := fmt.Sprintf(`version: '3.8'

services:
%s
volumes:
%s`, services, volumes)

	composePath := filepath.Join(outputDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(dockerCompose), 0644); err != nil {
		return fmt.Errorf("failed to write docker-compose.yml: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Finalizing
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Finalizing migration artifacts")
	EmitProgress(m, 90, "Finalizing")

	containerNames := make([]string, 0)
	for _, c := range aciInfo.Containers {
		containerNames = append(containerNames, c.Name)
	}

	readme := fmt.Sprintf(`# ACI to Docker Migration

## Source Container Group
- Name: %s
- Resource Group: %s
- OS Type: %s
- Location: %s
- Containers: %v

## Migration Steps

1. Pull required images (if using ACR, authenticate first):
'''bash
# If using Azure Container Registry:
# docker login <your-acr>.azurecr.io
docker-compose pull
'''

2. Start containers:
'''bash
docker-compose up -d
'''

3. Check status:
'''bash
docker-compose ps
docker-compose logs
'''

## Files Generated
- container-group-info.json: ACI configuration
- docker-compose.yml: Docker Compose configuration
- *.env: Environment files for each container

## ACR Authentication

If images are in Azure Container Registry:
'''bash
# Option 1: Use Azure CLI
az acr login --name <your-acr>

# Option 2: Docker login with token
TOKEN=$(az acr login --name <your-acr> --expose-token --output tsv --query accessToken)
docker login <your-acr>.azurecr.io -u 00000000-0000-0000-0000-000000000000 -p $TOKEN

# Option 3: Pull and retag images
docker pull <your-acr>.azurecr.io/image:tag
docker tag <your-acr>.azurecr.io/image:tag image:tag
'''

## Notes
- Update image references if migrating away from ACR
- Azure File mounts need local volume alternatives
- Review resource limits and adjust for your host
`, containerGroup, resourceGroup, aciInfo.OsType, aciInfo.Location, containerNames)

	readmePath := filepath.Join(outputDir, "README.md")
	if err := os.WriteFile(readmePath, []byte(readme), 0644); err != nil {
		return fmt.Errorf("failed to write README: %w", err)
	}

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", fmt.Sprintf("ACI %s migration prepared at %s", containerGroup, outputDir))

	return nil
}

// Helper functions

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func splitString(s string) []string {
	var result []string
	current := ""
	for _, c := range s {
		if c == ' ' || c == '\n' || c == '\t' {
			if current != "" {
				result = append(result, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

func generateNamespaceCreation(namespaces []string) string {
	var cmds string
	for _, ns := range namespaces {
		cmds += fmt.Sprintf("kubectl create namespace %s --dry-run=client -o yaml | kubectl apply -f -\n", ns)
	}
	return cmds
}

func formatNamespaceList(namespaces []string) string {
	var list string
	for _, ns := range namespaces {
		list += fmt.Sprintf("- %s\n", ns)
	}
	return list
}
