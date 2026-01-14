// Package compute provides mappers for GCP compute services.
package compute

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

// AppEngineMapper converts GCP App Engine applications to Docker services.
type AppEngineMapper struct {
	*mapper.BaseMapper
}

// NewAppEngineMapper creates a new App Engine mapper.
func NewAppEngineMapper() *AppEngineMapper {
	return &AppEngineMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeAppEngine, nil),
	}
}

// Map converts an App Engine application to a Docker service.
func (m *AppEngineMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	appID := res.GetConfigString("project")
	if appID == "" {
		appID = res.GetConfigString("id")
	}
	if appID == "" {
		appID = res.Name
	}

	result := mapper.NewMappingResult(m.sanitizeName(appID))
	svc := result.DockerService

	locationID := res.GetConfigString("location_id")
	servingStatus := res.GetConfigString("serving_status")
	authDomain := res.GetConfigString("auth_domain")
	databaseType := res.GetConfigString("database_type")

	// Default to a basic web server image - actual image depends on runtime
	svc.Image = fmt.Sprintf("%s:latest", m.sanitizeName(appID))

	// Configure service
	svc.Environment = map[string]string{
		"PORT":              "8080",
		"GAE_APPLICATION":   appID,
		"GAE_ENV":           "standard",
		"GAE_RUNTIME":       "custom",
		"GAE_DEPLOYMENT_ID": "local",
		"GAE_INSTANCE":      "local-instance",
	}

	if locationID != "" {
		svc.Environment["GAE_REGION"] = locationID
	}

	svc.Ports = []string{"8080:8080"}
	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"

	// Labels
	svc.Labels = map[string]string{
		"homeport.source":         "google_app_engine_application",
		"homeport.app_id":         appID,
		"homeport.location":       locationID,
		"homeport.serving_status": servingStatus,
		"traefik.enable":           "true",
		"traefik.http.routers." + m.sanitizeName(appID) + ".rule": fmt.Sprintf("Host(`%s.localhost`)", m.sanitizeName(appID)),
	}

	// Health check
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD-SHELL", "curl -f http://localhost:8080/_ah/health || curl -f http://localhost:8080/ || exit 1"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  3,
	}

	// Handle feature settings
	if featureSettings := res.Config["feature_settings"]; featureSettings != nil {
		m.handleFeatureSettings(featureSettings, result)
	}

	// Handle IAP (Identity-Aware Proxy)
	if iap := res.Config["iap"]; iap != nil {
		m.handleIAP(iap, result)
	}

	// Handle default hostname
	if defaultHostname := res.GetConfigString("default_hostname"); defaultHostname != "" {
		result.AddWarning(fmt.Sprintf("Default hostname: %s - Update DNS to point to your self-hosted service", defaultHostname))
	}

	// Handle code bucket
	if codeBucket := res.GetConfigString("code_bucket"); codeBucket != "" {
		result.AddWarning(fmt.Sprintf("Code stored in bucket: %s - Migrate code to local storage", codeBucket))
		result.AddManualStep("Download application code from GCS bucket: " + codeBucket)
	}

	// Handle auth domain
	if authDomain != "" {
		result.AddWarning(fmt.Sprintf("Auth domain: %s - Configure authentication provider", authDomain))
		result.AddManualStep("Set up authentication to replace Google auth domain")
	}

	// Handle database type
	if databaseType != "" {
		m.handleDatabaseType(databaseType, result)
	}

	// Generate Dockerfile template
	dockerfile := m.generateDockerfile(appID)
	result.AddConfig(fmt.Sprintf("appengine/%s/Dockerfile", appID), []byte(dockerfile))

	// Generate app.yaml equivalent
	appYaml := m.generateAppYaml(appID, res)
	result.AddConfig(fmt.Sprintf("appengine/%s/app.yaml", appID), []byte(appYaml))

	result.AddManualStep("Build application Docker image: docker build -t " + m.sanitizeName(appID) + " ./appengine/" + appID)
	result.AddManualStep("Copy your application code to ./appengine/" + appID + "/")
	result.AddManualStep("Configure app.yaml runtime and handlers for your application")
	result.AddManualStep("Set up environment variables for any GCP service dependencies")

	return result, nil
}

// handleFeatureSettings processes App Engine feature settings.
func (m *AppEngineMapper) handleFeatureSettings(settings interface{}, result *mapper.MappingResult) {
	if settingsMap, ok := settings.(map[string]interface{}); ok {
		if splitHealthChecks, ok := settingsMap["split_health_checks"].(bool); ok && splitHealthChecks {
			result.AddWarning("Split health checks enabled - Configure separate liveness and readiness probes")
		}
	}
}

// handleIAP processes Identity-Aware Proxy settings.
func (m *AppEngineMapper) handleIAP(iap interface{}, result *mapper.MappingResult) {
	if iapMap, ok := iap.(map[string]interface{}); ok {
		if enabled, ok := iapMap["enabled"].(bool); ok && enabled {
			result.AddWarning("IAP (Identity-Aware Proxy) enabled - Configure alternative authentication")
			result.AddManualStep("Set up OAuth2 proxy or similar authentication proxy to replace IAP")
		}
		if oauth2ClientID, ok := iapMap["oauth2_client_id"].(string); ok && oauth2ClientID != "" {
			result.AddWarning(fmt.Sprintf("OAuth2 client configured: %s", oauth2ClientID))
		}
	}
}

// handleDatabaseType adds warnings based on database type.
func (m *AppEngineMapper) handleDatabaseType(dbType string, result *mapper.MappingResult) {
	switch dbType {
	case "CLOUD_DATASTORE_COMPATIBILITY":
		result.AddWarning("Using Cloud Datastore compatibility mode - Migrate to self-hosted document database")
		result.AddManualStep("Consider migrating to MongoDB or another document database")
	case "CLOUD_FIRESTORE":
		result.AddWarning("Using Cloud Firestore - Migrate to self-hosted document database")
		result.AddManualStep("Consider migrating to MongoDB or another NoSQL database")
	case "CLOUD_DATASTORE":
		result.AddWarning("Using Cloud Datastore - Migrate to self-hosted key-value store")
		result.AddManualStep("Consider migrating to MongoDB, Redis, or another suitable database")
	}
}

// generateDockerfile creates a template Dockerfile for App Engine.
func (m *AppEngineMapper) generateDockerfile(appID string) string {
	return fmt.Sprintf(`# Dockerfile for App Engine application: %s
# Customize this based on your application runtime

# Example for Python
FROM python:3.11-slim

WORKDIR /app

# Install dependencies
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Copy application code
COPY . .

# App Engine expects port 8080
ENV PORT=8080
EXPOSE 8080

# Health check endpoint
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/_ah/health || curl -f http://localhost:8080/ || exit 1

# Start the application
# Adjust this based on your application
CMD ["python", "main.py"]

# Alternative runtimes:
#
# Node.js:
# FROM node:18-alpine
# WORKDIR /app
# COPY package*.json ./
# RUN npm ci --production
# COPY . .
# CMD ["npm", "start"]
#
# Go:
# FROM golang:1.21-alpine AS builder
# WORKDIR /app
# COPY . .
# RUN CGO_ENABLED=0 go build -o server .
# FROM alpine:latest
# COPY --from=builder /app/server /server
# CMD ["/server"]
#
# Java:
# FROM eclipse-temurin:17-jre-alpine
# COPY target/*.jar app.jar
# CMD ["java", "-jar", "app.jar"]
`, appID)
}

// generateAppYaml creates a template app.yaml configuration.
func (m *AppEngineMapper) generateAppYaml(appID string, res *resource.AWSResource) string {
	locationID := res.GetConfigString("location_id")

	return fmt.Sprintf(`# App Engine configuration for: %s
# Original location: %s
#
# This file documents the original App Engine configuration.
# Use it as a reference when configuring your self-hosted application.

runtime: custom
env: flex

# Automatic scaling settings
automatic_scaling:
  min_instances: 1
  max_instances: 10
  target_cpu_utilization: 0.65
  target_throughput_utilization: 0.65
  min_pending_latency: automatic
  max_pending_latency: automatic

# Resource settings
resources:
  cpu: 1
  memory_gb: 0.5
  disk_size_gb: 10

# Network settings
network:
  name: homeport

# Environment variables
env_variables:
  GAE_APPLICATION: "%s"
  GAE_ENV: "standard"

# Health check settings
liveness_check:
  path: "/_ah/health"
  check_interval_sec: 30
  timeout_sec: 10
  failure_threshold: 3
  success_threshold: 2

readiness_check:
  path: "/_ah/ready"
  check_interval_sec: 5
  timeout_sec: 10
  failure_threshold: 2
  success_threshold: 2

# URL handlers - customize based on your application
handlers:
- url: /static
  static_dir: static

- url: /.*
  script: auto
`, appID, locationID, appID)
}

func (m *AppEngineMapper) sanitizeName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "_", "-")
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.ReplaceAll(name, ".", "-")
	validName := ""
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' {
			validName += string(ch)
		}
	}
	validName = strings.TrimLeft(validName, "-0123456789")
	if validName == "" {
		validName = "appengine"
	}
	return validName
}
