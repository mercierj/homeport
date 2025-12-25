// Package storage provides mappers for AWS storage services.
package storage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cloudexit/cloudexit/internal/domain/mapper"
	"github.com/cloudexit/cloudexit/internal/domain/resource"
)

// S3Mapper converts AWS S3 buckets to MinIO.
type S3Mapper struct {
	*mapper.BaseMapper
}

// NewS3Mapper creates a new S3 to MinIO mapper.
func NewS3Mapper() *S3Mapper {
	return &S3Mapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeS3Bucket, nil),
	}
}

// Map converts an S3 bucket to a MinIO service.
func (m *S3Mapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	bucketName := res.GetConfigString("bucket")
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
		"cloudexit.source": "aws_s3_bucket",
		"cloudexit.bucket": bucketName,
		"traefik.enable":   "true",
		"traefik.http.routers.minio.rule":                      "Host(`minio.localhost`)",
		"traefik.http.services.minio.loadbalancer.server.port": "9001",
	}

	// Generate MinIO client (mc) setup script
	mcScript := m.generateMCScript(res, bucketName)
	result.AddScript("setup_minio.sh", []byte(mcScript))

	// Handle versioning
	if res.GetConfigBool("versioning.enabled") || m.hasVersioningBlock(res) {
		result.AddManualStep(
			fmt.Sprintf("Enable versioning on bucket '%s' using: mc version enable local/%s", bucketName, bucketName))
	}

	// Handle lifecycle rules
	if lifecycleRules := m.getLifecycleRules(res); len(lifecycleRules) > 0 {
		lifecycleScript := m.generateLifecycleScript(bucketName, lifecycleRules)
		result.AddScript("configure_lifecycle.sh", []byte(lifecycleScript))
		result.AddWarning("S3 lifecycle rules have been partially mapped. Review the generated lifecycle script for accuracy.")
	}

	// Handle CORS configuration
	if corsRules := m.getCORSRules(res); len(corsRules) > 0 {
		corsConfig := m.generateCORSConfig(bucketName, corsRules)
		result.AddConfig(fmt.Sprintf("config/minio/%s-cors.json", bucketName), []byte(corsConfig))
		result.AddManualStep(
			fmt.Sprintf("Apply CORS configuration: mc anonymous set-json config/minio/%s-cors.json local/%s", bucketName, bucketName))
	}

	// Handle public access settings
	if m.isPublicBucket(res) {
		result.AddManualStep(fmt.Sprintf("Set public read access: mc anonymous set download local/%s", bucketName))
		result.AddWarning("Bucket has public access enabled. Ensure this is intentional in your self-hosted environment.")
	}

	// Handle encryption
	if res.GetConfigString("server_side_encryption_configuration") != "" {
		result.AddWarning("S3 server-side encryption is configured. MinIO supports encryption at rest - configure manually if needed.")
		result.AddManualStep("Configure MinIO encryption: https://min.io/docs/minio/linux/operations/server-side-encryption.html")
	}

	// Handle replication
	if res.GetConfigString("replication_configuration") != "" {
		result.AddWarning("S3 replication is configured. MinIO supports bucket replication - configure manually.")
	}

	return result, nil
}

// generateMCScript creates a MinIO client setup script.
func (m *S3Mapper) generateMCScript(res *resource.AWSResource, bucketName string) string {
	region := res.Region
	if region == "" {
		region = "us-east-1"
	}

	return fmt.Sprintf(`#!/bin/bash
# MinIO Client (mc) Setup Script
# This script configures the MinIO client and creates the bucket

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
echo "To use with AWS SDK, configure endpoint: http://localhost:9000"
`, bucketName, bucketName, region, bucketName)
}

// generateLifecycleScript creates a script to configure lifecycle policies.
func (m *S3Mapper) generateLifecycleScript(bucketName string, rules []map[string]interface{}) string {
	script := fmt.Sprintf(`#!/bin/bash
# MinIO Lifecycle Configuration Script
# Configures lifecycle policies for bucket: %s

set -e

echo "Configuring lifecycle policies for %s..."

# Note: MinIO lifecycle rules use similar syntax to S3
# You may need to adjust these rules based on your MinIO version

`, bucketName, bucketName)

	for i, rule := range rules {
		ruleID := fmt.Sprintf("rule-%d", i)
		if id, ok := rule["id"].(string); ok {
			ruleID = id
		}
		script += fmt.Sprintf("# Rule: %s\n", ruleID)
		script += "# Review and apply manually using mc ilm add command\n\n"
	}

	script += `
echo "Lifecycle configuration complete!"
echo "Note: Review the rules above and apply them using 'mc ilm add' command"
`
	return script
}

// generateCORSConfig creates a CORS configuration file.
func (m *S3Mapper) generateCORSConfig(bucketName string, rules []map[string]interface{}) string {
	content := "{\n  \"CORSRules\": [\n"

	for i, rule := range rules {
		if i > 0 {
			content += ",\n"
		}
		content += "    {\n"

		if allowedOrigins, ok := rule["allowed_origins"].([]interface{}); ok {
			content += "      \"AllowedOrigins\": ["
			for j, origin := range allowedOrigins {
				if j > 0 {
					content += ", "
				}
				content += fmt.Sprintf("\"%v\"", origin)
			}
			content += "],\n"
		}

		if allowedMethods, ok := rule["allowed_methods"].([]interface{}); ok {
			content += "      \"AllowedMethods\": ["
			for j, method := range allowedMethods {
				if j > 0 {
					content += ", "
				}
				content += fmt.Sprintf("\"%v\"", method)
			}
			content += "]\n"
		}

		content += "    }"
	}

	content += "\n  ]\n}\n"
	return content
}

// hasVersioningBlock checks if the resource has versioning configuration.
func (m *S3Mapper) hasVersioningBlock(res *resource.AWSResource) bool {
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
func (m *S3Mapper) getLifecycleRules(res *resource.AWSResource) []map[string]interface{} {
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
func (m *S3Mapper) getCORSRules(res *resource.AWSResource) []map[string]interface{} {
	var rules []map[string]interface{}

	if corsSlice, ok := res.Config["cors_rule"].([]interface{}); ok {
		for _, rule := range corsSlice {
			if ruleMap, ok := rule.(map[string]interface{}); ok {
				rules = append(rules, ruleMap)
			}
		}
	}

	return rules
}

// isPublicBucket determines if the bucket has public access.
func (m *S3Mapper) isPublicBucket(res *resource.AWSResource) bool {
	// Check ACL
	acl := res.GetConfigString("acl")
	if acl == "public-read" || acl == "public-read-write" {
		return true
	}

	// Check public access block configuration
	if pab, ok := res.Config["public_access_block_configuration"].(map[string]interface{}); ok {
		blockPublicAcls := true
		if val, ok := pab["block_public_acls"].(bool); ok {
			blockPublicAcls = val
		}
		return !blockPublicAcls
	}

	// Check bucket policy for public statements
	if policy := res.GetConfigString("policy"); policy != "" {
		return strings.Contains(strings.ToLower(policy), "\"effect\":\"allow\"") &&
			strings.Contains(strings.ToLower(policy), "\"principal\":\"*\"")
	}

	return false
}
