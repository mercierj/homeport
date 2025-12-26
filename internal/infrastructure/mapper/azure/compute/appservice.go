// Package compute provides mappers for Azure compute services.
package compute

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

// AppServiceMapper converts Azure App Service to Docker containers.
type AppServiceMapper struct {
	*mapper.BaseMapper
}

// NewAppServiceMapper creates a new Azure App Service to Docker mapper.
func NewAppServiceMapper() *AppServiceMapper {
	return &AppServiceMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeAppService, nil),
	}
}

// Map converts an Azure App Service to a Docker service.
func (m *AppServiceMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	appName := res.GetConfigString("name")
	if appName == "" {
		appName = res.Name
	}

	result := mapper.NewMappingResult(m.sanitizeName(appName))
	svc := result.DockerService

	// Get runtime stack info
	runtime := m.getRuntime(res)
	runtimeVersion := m.getRuntimeVersion(res, runtime)

	// Set Docker image based on runtime
	svc.Image = m.getDockerImage(runtime, runtimeVersion)

	// Configure environment
	svc.Environment = map[string]string{
		"WEBSITES_PORT":           "80",
		"WEBSITE_HOSTNAME":        "localhost",
		"ASPNETCORE_URLS":         "http://+:80",
		"ASPNETCORE_ENVIRONMENT":  "Production",
	}

	// Add app settings
	if appSettings := res.Config["app_settings"]; appSettings != nil {
		if settingsMap, ok := appSettings.(map[string]interface{}); ok {
			for k, v := range settingsMap {
				if strVal, ok := v.(string); ok {
					svc.Environment[k] = strVal
				}
			}
		}
	}

	// Handle site config
	if siteConfig := res.Config["site_config"]; siteConfig != nil {
		m.applySiteConfig(siteConfig, svc, result)
	}

	// Set ports based on HTTPS configuration
	svc.Ports = []string{"80:80"}
	if m.hasHTTPS(res) {
		svc.Ports = append(svc.Ports, "443:443")
	}

	svc.Networks = []string{"cloudexit"}
	svc.Restart = "unless-stopped"

	svc.Labels = map[string]string{
		"cloudexit.source":   "azurerm_app_service",
		"cloudexit.app_name": appName,
		"cloudexit.runtime":  runtime,
		"traefik.enable":     "true",
		"traefik.http.routers." + m.sanitizeName(appName) + ".rule": fmt.Sprintf("Host(`%s.localhost`)", m.sanitizeName(appName)),
	}

	// Health check
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD-SHELL", "curl -f http://localhost/health || curl -f http://localhost/ || exit 1"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  3,
	}

	// Handle connection strings
	if connStrings := res.Config["connection_string"]; connStrings != nil {
		m.handleConnectionStrings(connStrings, svc, result)
	}

	// Handle storage accounts
	if storageAccount := res.Config["storage_account"]; storageAccount != nil {
		result.AddWarning("Storage account mount configured. Configure equivalent Docker volume.")
		result.AddManualStep("Set up MinIO or local storage for mounted storage accounts")
	}

	// Handle identity
	if identity := res.Config["identity"]; identity != nil {
		result.AddWarning("Managed identity is configured. Configure equivalent service credentials.")
	}

	// Apply service plan sizing
	if servicePlanID := res.GetConfigString("app_service_plan_id"); servicePlanID != "" {
		m.applyServicePlanSizing(svc, servicePlanID)
	}

	// Generate Dockerfile
	dockerfile := m.generateDockerfile(runtime, runtimeVersion, appName)
	result.AddConfig(fmt.Sprintf("apps/%s/Dockerfile", appName), []byte(dockerfile))

	// Generate docker-compose override for the app
	composeOverride := m.generateComposeOverride(appName, runtime)
	result.AddConfig(fmt.Sprintf("apps/%s/docker-compose.override.yml", appName), []byte(composeOverride))

	result.AddManualStep("Build app: docker build -t " + m.sanitizeName(appName) + " ./apps/" + appName)
	result.AddManualStep("Access at: http://" + m.sanitizeName(appName) + ".localhost")

	return result, nil
}

func (m *AppServiceMapper) getRuntime(res *resource.AWSResource) string {
	if siteConfig := res.Config["site_config"]; siteConfig != nil {
		if configMap, ok := siteConfig.(map[string]interface{}); ok {
			// Check application_stack for newer Terraform versions
			if appStack, ok := configMap["application_stack"].(map[string]interface{}); ok {
				if _, ok := appStack["dotnet_version"]; ok {
					return "dotnet"
				}
				if _, ok := appStack["node_version"]; ok {
					return "node"
				}
				if _, ok := appStack["python_version"]; ok {
					return "python"
				}
				if _, ok := appStack["java_version"]; ok {
					return "java"
				}
				if _, ok := appStack["php_version"]; ok {
					return "php"
				}
				if _, ok := appStack["ruby_version"]; ok {
					return "ruby"
				}
				if _, ok := appStack["docker_image"]; ok {
					return "docker"
				}
			}

			// Check legacy configs
			if _, ok := configMap["dotnet_framework_version"]; ok {
				return "dotnet"
			}
			if _, ok := configMap["java_version"]; ok {
				return "java"
			}
			if _, ok := configMap["python_version"]; ok {
				return "python"
			}
			if linuxFxVersion, ok := configMap["linux_fx_version"].(string); ok {
				linuxFxVersion = strings.ToLower(linuxFxVersion)
				switch {
				case strings.HasPrefix(linuxFxVersion, "node"):
					return "node"
				case strings.HasPrefix(linuxFxVersion, "python"):
					return "python"
				case strings.HasPrefix(linuxFxVersion, "dotnet"):
					return "dotnet"
				case strings.HasPrefix(linuxFxVersion, "java"):
					return "java"
				case strings.HasPrefix(linuxFxVersion, "php"):
					return "php"
				case strings.HasPrefix(linuxFxVersion, "ruby"):
					return "ruby"
				case strings.HasPrefix(linuxFxVersion, "docker"):
					return "docker"
				}
			}
		}
	}
	return "node"
}

func (m *AppServiceMapper) getRuntimeVersion(res *resource.AWSResource, runtime string) string {
	if siteConfig := res.Config["site_config"]; siteConfig != nil {
		if configMap, ok := siteConfig.(map[string]interface{}); ok {
			if appStack, ok := configMap["application_stack"].(map[string]interface{}); ok {
				switch runtime {
				case "node":
					if v, ok := appStack["node_version"].(string); ok {
						return m.normalizeVersion(v)
					}
				case "python":
					if v, ok := appStack["python_version"].(string); ok {
						return m.normalizeVersion(v)
					}
				case "dotnet":
					if v, ok := appStack["dotnet_version"].(string); ok {
						return m.normalizeVersion(v)
					}
				case "java":
					if v, ok := appStack["java_version"].(string); ok {
						return m.normalizeVersion(v)
					}
				case "php":
					if v, ok := appStack["php_version"].(string); ok {
						return m.normalizeVersion(v)
					}
				case "ruby":
					if v, ok := appStack["ruby_version"].(string); ok {
						return m.normalizeVersion(v)
					}
				}
			}

			// Check linux_fx_version
			if linuxFxVersion, ok := configMap["linux_fx_version"].(string); ok {
				parts := strings.SplitN(linuxFxVersion, "|", 2)
				if len(parts) == 2 {
					return m.normalizeVersion(parts[1])
				}
			}
		}
	}

	// Default versions
	switch runtime {
	case "node":
		return "20"
	case "python":
		return "3.11"
	case "dotnet":
		return "8.0"
	case "java":
		return "17"
	case "php":
		return "8.2"
	case "ruby":
		return "3.2"
	default:
		return "20"
	}
}

func (m *AppServiceMapper) normalizeVersion(version string) string {
	// Remove leading "v" if present
	version = strings.TrimPrefix(version, "v")
	// Remove "LTS" suffix
	version = strings.TrimSuffix(version, "-lts")
	version = strings.TrimSuffix(version, "-LTS")
	return version
}

func (m *AppServiceMapper) getDockerImage(runtime, version string) string {
	switch runtime {
	case "node":
		return fmt.Sprintf("node:%s-alpine", version)
	case "python":
		return fmt.Sprintf("python:%s-slim", version)
	case "dotnet":
		return fmt.Sprintf("mcr.microsoft.com/dotnet/aspnet:%s", version)
	case "java":
		return fmt.Sprintf("eclipse-temurin:%s-jre", version)
	case "php":
		return fmt.Sprintf("php:%s-apache", version)
	case "ruby":
		return fmt.Sprintf("ruby:%s-slim", version)
	case "docker":
		return "nginx:alpine"
	default:
		return "node:20-alpine"
	}
}

func (m *AppServiceMapper) applySiteConfig(siteConfig interface{}, svc *mapper.DockerService, result *mapper.MappingResult) {
	if configMap, ok := siteConfig.(map[string]interface{}); ok {
		// Handle always on
		if alwaysOn, ok := configMap["always_on"].(bool); ok && alwaysOn {
			result.AddWarning("Always On was enabled. Consider using Docker restart policy and health checks.")
		}

		// Handle websockets
		if websockets, ok := configMap["websockets_enabled"].(bool); ok && websockets {
			svc.Labels["cloudexit.websockets"] = "true"
		}

		// Handle remote debugging
		if remoteDebug, ok := configMap["remote_debugging_enabled"].(bool); ok && remoteDebug {
			result.AddWarning("Remote debugging was enabled. Configure equivalent debugging for containers.")
		}

		// Handle CORS
		if cors := configMap["cors"]; cors != nil {
			result.AddWarning("CORS configuration detected. Configure CORS in your application or reverse proxy.")
		}

		// Handle IP restrictions
		if ipRestriction := configMap["ip_restriction"]; ipRestriction != nil {
			result.AddWarning("IP restrictions detected. Configure equivalent firewall rules or reverse proxy settings.")
		}

		// Handle virtual applications
		if vDir := configMap["virtual_application"]; vDir != nil {
			result.AddWarning("Virtual applications/directories detected. Configure equivalent paths in your web server.")
		}
	}
}

func (m *AppServiceMapper) hasHTTPS(res *resource.AWSResource) bool {
	if httpsOnly, ok := res.Config["https_only"].(bool); ok && httpsOnly {
		return true
	}
	return false
}

func (m *AppServiceMapper) handleConnectionStrings(connStrings interface{}, svc *mapper.DockerService, result *mapper.MappingResult) {
	if connSlice, ok := connStrings.([]interface{}); ok {
		for _, conn := range connSlice {
			if connMap, ok := conn.(map[string]interface{}); ok {
				name, _ := connMap["name"].(string)
				connType, _ := connMap["type"].(string)

				if name != "" {
					envVarName := fmt.Sprintf("CONNECTIONSTRINGS_%s", strings.ToUpper(strings.ReplaceAll(name, "-", "_")))
					svc.Environment[envVarName] = fmt.Sprintf("${%s}", envVarName)
				}

				result.AddWarning(fmt.Sprintf("Connection string '%s' (%s) detected. Configure database connection.", name, connType))
			}
		}
	}
	result.AddManualStep("Set up database services and update connection strings in environment variables")
}

func (m *AppServiceMapper) applyServicePlanSizing(svc *mapper.DockerService, servicePlanID string) {
	svc.Deploy = &mapper.DeployConfig{
		Resources: &mapper.Resources{
			Limits: &mapper.ResourceLimits{},
		},
	}

	servicePlanID = strings.ToLower(servicePlanID)

	switch {
	case strings.Contains(servicePlanID, "free") || strings.Contains(servicePlanID, "f1"):
		svc.Deploy.Resources.Limits.CPUs = "0.5"
		svc.Deploy.Resources.Limits.Memory = "1G"
	case strings.Contains(servicePlanID, "shared") || strings.Contains(servicePlanID, "d1"):
		svc.Deploy.Resources.Limits.CPUs = "0.5"
		svc.Deploy.Resources.Limits.Memory = "1G"
	case strings.Contains(servicePlanID, "basic") || strings.Contains(servicePlanID, "b1"):
		svc.Deploy.Resources.Limits.CPUs = "1"
		svc.Deploy.Resources.Limits.Memory = "1.75G"
	case strings.Contains(servicePlanID, "b2"):
		svc.Deploy.Resources.Limits.CPUs = "2"
		svc.Deploy.Resources.Limits.Memory = "3.5G"
	case strings.Contains(servicePlanID, "b3"):
		svc.Deploy.Resources.Limits.CPUs = "4"
		svc.Deploy.Resources.Limits.Memory = "7G"
	case strings.Contains(servicePlanID, "standard") || strings.Contains(servicePlanID, "s1"):
		svc.Deploy.Resources.Limits.CPUs = "1"
		svc.Deploy.Resources.Limits.Memory = "1.75G"
	case strings.Contains(servicePlanID, "s2"):
		svc.Deploy.Resources.Limits.CPUs = "2"
		svc.Deploy.Resources.Limits.Memory = "3.5G"
	case strings.Contains(servicePlanID, "s3"):
		svc.Deploy.Resources.Limits.CPUs = "4"
		svc.Deploy.Resources.Limits.Memory = "7G"
	case strings.Contains(servicePlanID, "premium") || strings.Contains(servicePlanID, "p1"):
		svc.Deploy.Resources.Limits.CPUs = "1"
		svc.Deploy.Resources.Limits.Memory = "3.5G"
	case strings.Contains(servicePlanID, "p2"):
		svc.Deploy.Resources.Limits.CPUs = "2"
		svc.Deploy.Resources.Limits.Memory = "7G"
	case strings.Contains(servicePlanID, "p3"):
		svc.Deploy.Resources.Limits.CPUs = "4"
		svc.Deploy.Resources.Limits.Memory = "14G"
	case strings.Contains(servicePlanID, "p1v2"):
		svc.Deploy.Resources.Limits.CPUs = "1"
		svc.Deploy.Resources.Limits.Memory = "3.5G"
	case strings.Contains(servicePlanID, "p2v2"):
		svc.Deploy.Resources.Limits.CPUs = "2"
		svc.Deploy.Resources.Limits.Memory = "7G"
	case strings.Contains(servicePlanID, "p3v2"):
		svc.Deploy.Resources.Limits.CPUs = "4"
		svc.Deploy.Resources.Limits.Memory = "14G"
	case strings.Contains(servicePlanID, "p1v3"):
		svc.Deploy.Resources.Limits.CPUs = "2"
		svc.Deploy.Resources.Limits.Memory = "8G"
	case strings.Contains(servicePlanID, "p2v3"):
		svc.Deploy.Resources.Limits.CPUs = "4"
		svc.Deploy.Resources.Limits.Memory = "16G"
	case strings.Contains(servicePlanID, "p3v3"):
		svc.Deploy.Resources.Limits.CPUs = "8"
		svc.Deploy.Resources.Limits.Memory = "32G"
	default:
		svc.Deploy.Resources.Limits.CPUs = "1"
		svc.Deploy.Resources.Limits.Memory = "2G"
	}
}

func (m *AppServiceMapper) generateDockerfile(runtime, version, appName string) string {
	switch runtime {
	case "node":
		return fmt.Sprintf(`FROM node:%s-alpine

# Azure App Service: %s

WORKDIR /app

COPY package*.json ./
RUN npm ci --only=production

COPY . .

EXPOSE 80
ENV PORT=80

CMD ["node", "server.js"]
`, version, appName)

	case "python":
		return fmt.Sprintf(`FROM python:%s-slim

# Azure App Service: %s

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY . .

EXPOSE 80
ENV PORT=80

CMD ["gunicorn", "--bind", "0.0.0.0:80", "app:app"]
`, version, appName)

	case "dotnet":
		return fmt.Sprintf(`FROM mcr.microsoft.com/dotnet/aspnet:%s AS runtime

# Azure App Service: %s

WORKDIR /app

COPY ./publish .

EXPOSE 80
ENV ASPNETCORE_URLS=http://+:80

ENTRYPOINT ["dotnet", "App.dll"]
`, version, appName)

	case "java":
		return fmt.Sprintf(`FROM eclipse-temurin:%s-jre

# Azure App Service: %s

WORKDIR /app

COPY target/*.jar app.jar

EXPOSE 80
ENV SERVER_PORT=80

ENTRYPOINT ["java", "-jar", "app.jar"]
`, version, appName)

	case "php":
		return fmt.Sprintf(`FROM php:%s-apache

# Azure App Service: %s

WORKDIR /var/www/html

COPY . .

RUN chown -R www-data:www-data /var/www/html

EXPOSE 80
`, version, appName)

	case "ruby":
		return fmt.Sprintf(`FROM ruby:%s-slim

# Azure App Service: %s

WORKDIR /app

RUN apt-get update && apt-get install -y build-essential && rm -rf /var/lib/apt/lists/*

COPY Gemfile* ./
RUN bundle install --without development test

COPY . .

EXPOSE 80
ENV PORT=80

CMD ["bundle", "exec", "rails", "server", "-b", "0.0.0.0", "-p", "80"]
`, version, appName)

	default:
		return fmt.Sprintf(`FROM node:%s-alpine

# Azure App Service: %s

WORKDIR /app

COPY . .

EXPOSE 80
ENV PORT=80

CMD ["node", "server.js"]
`, version, appName)
	}
}

func (m *AppServiceMapper) generateComposeOverride(appName, runtime string) string {
	return fmt.Sprintf(`# Docker Compose override for %s (%s)
services:
  %s:
    build:
      context: .
      dockerfile: Dockerfile
    environment:
      - NODE_ENV=production
    volumes:
      - ./data:/app/data
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"
`, appName, runtime, m.sanitizeName(appName))
}

func (m *AppServiceMapper) sanitizeName(name string) string {
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
		validName = "webapp"
	}
	return validName
}
