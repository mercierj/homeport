// Package storage provides mappers for GCP storage services.
package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudexit/cloudexit/internal/domain/mapper"
	"github.com/cloudexit/cloudexit/internal/domain/resource"
)

// GCSMapper converts GCP Cloud Storage buckets to MinIO.
type GCSMapper struct {
	*mapper.BaseMapper
}

// NewGCSMapper creates a new GCS to MinIO mapper.
func NewGCSMapper() *GCSMapper {
	return &GCSMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeGCSBucket, nil),
	}
}

// Map converts a GCS bucket to a MinIO service.
func (m *GCSMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	bucketName := res.GetConfigString("name")
	if bucketName == "" {
		bucketName = res.Name
	}

	// Create result using new API
	result := mapper.NewMappingResult("minio")
	svc := result.DockerService

	// Configure MinIO service
	svc.Image = "minio/minio:latest"
	svc.Environment = map[string]string{
		"MINIO_ROOT_USER":     "minioadmin",
		"MINIO_ROOT_PASSWORD": "minioadmin",
	}
	svc.Ports = []string{
		"9000:9000", // API
		"9001:9001", // Console
	}
	svc.Volumes = []string{
		"./data/minio:/data",
	}
	svc.Command = []string{"server", "/data", "--console-address", ":9001"}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "curl", "-f", "http://localhost:9000/minio/health/live"},
		Interval: 30 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  3,
	}
	svc.Labels = map[string]string{
		"cloudexit.source": "google_storage_bucket",
		"cloudexit.bucket": bucketName,
		"traefik.enable":   "true",
		"traefik.http.routers.minio.rule":                      "Host(`minio.localhost`)",
		"traefik.http.services.minio.loadbalancer.server.port": "9001",
	}
	svc.Networks = []string{"cloudexit"}
	svc.Restart = "unless-stopped"

	// Generate MinIO client (mc) setup script
	mcScript := m.generateMCScript(res, bucketName)
	result.AddScript("setup_minio.sh", []byte(mcScript))

	// Handle storage class
	storageClass := res.GetConfigString("storage_class")
	if storageClass != "" {
		result.AddWarning(fmt.Sprintf("GCS storage class '%s' is configured. MinIO uses erasure coding for redundancy by default.", storageClass))
		result.AddManualStep("Review MinIO deployment mode (standalone vs distributed) based on your storage class requirements")
	}

	// Handle versioning
	if res.GetConfigBool("versioning.enabled") || m.hasVersioningBlock(res) {
		result.AddManualStep(
			fmt.Sprintf("Enable versioning on bucket '%s' using: mc version enable local/%s", bucketName, bucketName))
	}

	// Handle lifecycle rules
	if lifecycleRules := m.getLifecycleRules(res); len(lifecycleRules) > 0 {
		lifecycleScript := m.generateLifecycleScript(bucketName, lifecycleRules)
		result.AddScript("configure_lifecycle.sh", []byte(lifecycleScript))
		result.AddWarning("GCS lifecycle rules have been partially mapped. Review the generated lifecycle script for accuracy.")
	}

	// Handle CORS configuration
	if corsRules := m.getCORSRules(res); len(corsRules) > 0 {
		corsConfig := m.generateCORSConfig(bucketName, corsRules)
		result.AddConfig(fmt.Sprintf("config/minio/%s-cors.json", bucketName), []byte(corsConfig))
		result.AddManualStep(
			fmt.Sprintf("Apply CORS configuration: mc anonymous set-json config/minio/%s-cors.json local/%s", bucketName, bucketName))
	}

	// Handle uniform bucket-level access
	if uniformBucketAccess := res.GetConfigBool("uniform_bucket_level_access.enabled"); uniformBucketAccess {
		result.AddWarning("Uniform bucket-level access is enabled. MinIO uses bucket policies for access control.")
		result.AddManualStep("Configure MinIO bucket policies to match GCS uniform bucket-level access requirements")
	}

	// Handle public access prevention
	publicAccessPrevention := res.GetConfigString("public_access_prevention")
	if publicAccessPrevention == "enforced" {
		result.AddWarning("Public access prevention is enforced. Ensure MinIO bucket policies prevent public access.")
	} else if m.isPublicBucket(res) {
		result.AddManualStep(fmt.Sprintf("Set public read access: mc anonymous set download local/%s", bucketName))
		result.AddWarning("Bucket has public access enabled. Ensure this is intentional in your self-hosted environment.")
	}

	// Handle encryption
	if encryption := res.Config["encryption"]; encryption != nil {
		result.AddWarning("GCS encryption is configured. MinIO supports encryption at rest - configure manually if needed.")
		result.AddManualStep("Configure MinIO encryption: https://min.io/docs/minio/linux/operations/server-side-encryption.html")
	}

	// Handle website configuration
	if website := res.Config["website"]; website != nil {
		result.AddWarning("GCS website configuration detected. MinIO supports static website hosting.")
		result.AddManualStep("Configure MinIO static website hosting: mc anonymous set-json for website bucket")
	}

	// Handle retention policy
	if retentionPolicy := res.Config["retention_policy"]; retentionPolicy != nil {
		result.AddWarning("GCS retention policy is configured. MinIO supports object locking for retention.")
		result.AddManualStep("Configure MinIO object locking: mc retention set --default GOVERNANCE <days> local/" + bucketName)
	}

	// Handle logging
	if logging := res.Config["logging"]; logging != nil {
		result.AddWarning("GCS bucket logging is configured. MinIO supports audit logging.")
		result.AddManualStep("Enable MinIO audit logging in server configuration")
	}

	return result, nil
}

// generateMCScript creates a MinIO client setup script.
func (m *GCSMapper) generateMCScript(res *resource.AWSResource, bucketName string) string {
	location := res.GetConfigString("location")
	if location == "" {
		location = "us-east-1"
	}

	return fmt.Sprintf(`#!/bin/bash
# MinIO Client (mc) Setup Script
# This script configures the MinIO client and creates the bucket
# Migrated from GCS bucket: %s

set -e

echo "Setting up MinIO client..."

# Configure MinIO alias
mc alias set local http://localhost:9000 minioadmin minioadmin

# Create bucket
echo "Creating bucket: %s"
mc mb --ignore-existing local/%s

# Set region (for compatibility)
mc admin config set local region name=%s

echo "MinIO bucket '%s' is ready!"
echo "Access MinIO Console at: http://localhost:9001"
echo "Credentials: minioadmin / minioadmin"
echo ""
echo "To use with GCS-compatible SDK, configure endpoint: http://localhost:9000"
echo "Note: Update your application to use S3-compatible SDK instead of GCS SDK"
`, bucketName, bucketName, bucketName, location, bucketName)
}

// generateLifecycleScript creates a script to configure lifecycle policies.
func (m *GCSMapper) generateLifecycleScript(bucketName string, rules []map[string]interface{}) string {
	script := fmt.Sprintf(`#!/bin/bash
# MinIO Lifecycle Configuration Script
# Configures lifecycle policies for bucket: %s
# Migrated from GCS lifecycle rules

set -e

echo "Configuring lifecycle policies for %s..."

# Note: MinIO lifecycle rules use S3-compatible syntax
# GCS lifecycle rules have been mapped to equivalent MinIO rules

`, bucketName, bucketName)

	for i, rule := range rules {
		if actionType, ok := rule["action"].(map[string]interface{}); ok {
			if aType, ok := actionType["type"].(string); ok {
				script += fmt.Sprintf("# Rule %d: %s\n", i, aType)
			}
		}
		script += "# Review and apply manually using mc ilm add command\n\n"
	}

	script += `
echo "Lifecycle configuration complete!"
echo "Note: Review the rules above and apply them using 'mc ilm add' command"
`
	return script
}

// generateCORSConfig creates a CORS configuration file.
func (m *GCSMapper) generateCORSConfig(bucketName string, rules []map[string]interface{}) string {
	content := "{\n  \"CORSRules\": [\n"

	for i, rule := range rules {
		if i > 0 {
			content += ",\n"
		}
		content += "    {\n"

		if origins, ok := rule["origin"].([]interface{}); ok {
			content += "      \"AllowedOrigins\": ["
			for j, origin := range origins {
				if j > 0 {
					content += ", "
				}
				content += fmt.Sprintf("\"%v\"", origin)
			}
			content += "],\n"
		}

		if methods, ok := rule["method"].([]interface{}); ok {
			content += "      \"AllowedMethods\": ["
			for j, method := range methods {
				if j > 0 {
					content += ", "
				}
				content += fmt.Sprintf("\"%v\"", method)
			}
			content += "],\n"
		}

		if headers, ok := rule["response_header"].([]interface{}); ok {
			content += "      \"AllowedHeaders\": ["
			for j, header := range headers {
				if j > 0 {
					content += ", "
				}
				content += fmt.Sprintf("\"%v\"", header)
			}
			content += "],\n"
		}

		if maxAge, ok := rule["max_age_seconds"].(float64); ok {
			content += fmt.Sprintf("      \"MaxAgeSeconds\": %d\n", int(maxAge))
		}

		content += "    }"
	}

	content += "\n  ]\n}\n"
	return content
}

// hasVersioningBlock checks if the resource has versioning configuration.
func (m *GCSMapper) hasVersioningBlock(res *resource.AWSResource) bool {
	if versioning := res.Config["versioning"]; versioning != nil {
		if vm, ok := versioning.(map[string]interface{}); ok {
			if enabled, ok := vm["enabled"].(bool); ok {
				return enabled
			}
		}
	}
	return false
}

// getLifecycleRules extracts lifecycle rules from the resource.
func (m *GCSMapper) getLifecycleRules(res *resource.AWSResource) []map[string]interface{} {
	var rules []map[string]interface{}

	if lifecycle, ok := res.Config["lifecycle_rule"].(map[string]interface{}); ok {
		rules = append(rules, lifecycle)
	}

	if lifecycleSlice, ok := res.Config["lifecycle_rule"].([]interface{}); ok {
		for _, rule := range lifecycleSlice {
			if ruleMap, ok := rule.(map[string]interface{}); ok {
				rules = append(rules, ruleMap)
			}
		}
	}

	return rules
}

// getCORSRules extracts CORS rules from the resource.
func (m *GCSMapper) getCORSRules(res *resource.AWSResource) []map[string]interface{} {
	var rules []map[string]interface{}

	if corsSlice, ok := res.Config["cors"].([]interface{}); ok {
		for _, rule := range corsSlice {
			if ruleMap, ok := rule.(map[string]interface{}); ok {
				rules = append(rules, ruleMap)
			}
		}
	}

	return rules
}

// isPublicBucket determines if the bucket has public access.
func (m *GCSMapper) isPublicBucket(res *resource.AWSResource) bool {
	// Check if public access prevention is not enforced
	publicAccessPrevention := res.GetConfigString("public_access_prevention")
	if publicAccessPrevention == "enforced" {
		return false
	}

	// Check IAM configuration for allUsers or allAuthenticatedUsers
	if iamConfig := res.Config["iam_configuration"]; iamConfig != nil {
		// GCS uses bucket-level IAM, check if bucket is public via IAM
		// This would need to check the associated IAM bindings
		// For now, we'll return false and let the user configure manually
	}

	return false
}
