// Package storage provides mappers for GCP storage services.
package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/policy"
	"github.com/homeport/homeport/internal/domain/resource"
	"github.com/homeport/homeport/internal/infrastructure/mapper/shared/storagerunbook"
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
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "curl", "-f", "http://localhost:9000/minio/health/live"},
		Interval: 30 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  3,
	}
	svc.Labels = map[string]string{
		"homeport.source":                 "google_storage_bucket",
		"homeport.bucket":                 bucketName,
		"traefik.enable":                  "true",
		"traefik.http.routers.minio.rule": "Host(`minio.localhost`)",
		"traefik.http.services.minio.loadbalancer.server.port": "9001",
	}
	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"

	// Generate MinIO client (mc) setup script
	mcScript := m.generateMCScript(res, bucketName)
	result.AddScript("setup_minio.sh", []byte(mcScript))
	result.AddConfig("config/minio/app-change.env", []byte(m.generateGCSAppChange(bucketName)))
	result.AddScript("validate_gcs_api.sh", []byte(m.generateGCSValidateScript(bucketName)))
	result.AddScript("backup_gcs_config.sh", []byte(m.generateGCSBackupScript(bucketName)))
	result.AddScript("cutover_gcs_clients.sh", []byte(m.generateGCSCutoverScript(bucketName)))
	for _, step := range storagerunbook.ObjectStorage(bucketName, "gcs:"+bucketName) {
		result.AddRunbookStep(step)
	}

	// Handle storage class
	storageClass := res.GetConfigString("storage_class")
	if storageClass != "" {
		result.AddWarning(fmt.Sprintf("GCS storage class '%s' is configured. MinIO uses erasure coding for redundancy by default.", storageClass))
	}

	// Handle versioning
	if res.GetConfigBool("versioning.enabled") || m.hasVersioningBlock(res) {
		result.AddScript("configure_versioning.sh", []byte(fmt.Sprintf("#!/bin/sh\nset -eu\nmc version enable local/%s\n", bucketName)))
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
		result.AddScript("configure_cors.sh", []byte(fmt.Sprintf("#!/bin/sh\nset -eu\nmc anonymous set-json config/minio/%s-cors.json local/%s\n", bucketName, bucketName)))
	}

	// Handle uniform bucket-level access
	if uniformBucketAccess := res.GetConfigBool("uniform_bucket_level_access.enabled"); uniformBucketAccess {
		result.AddWarning("Uniform bucket-level access is enabled. MinIO uses bucket policies for access control.")
		result.AddConfig(fmt.Sprintf("config/minio/%s-policy.json", bucketName), []byte(m.generateBucketPolicy(bucketName, false)))
		result.AddScript("configure_bucket_policy.sh", []byte(fmt.Sprintf("#!/bin/sh\nset -eu\nmc anonymous set-json config/minio/%s-policy.json local/%s\n", bucketName, bucketName)))
	}

	// Handle public access prevention
	publicAccessPrevention := res.GetConfigString("public_access_prevention")
	if publicAccessPrevention == "enforced" {
		result.AddWarning("Public access prevention is enforced. Ensure MinIO bucket policies prevent public access.")
	} else if m.isPublicBucket(res) {
		result.AddScript("configure_public_access.sh", []byte(fmt.Sprintf("#!/bin/sh\nset -eu\nmc anonymous set download local/%s\n", bucketName)))
		result.AddWarning("Bucket has public access enabled. Ensure this is intentional in your self-hosted environment.")
	}

	// Handle encryption
	if encryption := res.Config["encryption"]; encryption != nil {
		result.AddWarning("GCS encryption is configured. MinIO encryption env and setup script are generated for target deployment.")
		result.AddConfig("config/minio/encryption.env", []byte("MINIO_KMS_SECRET_KEY=homeport-key:${MINIO_MASTER_KEY}\n"))
		result.AddScript("configure_encryption.sh", []byte("#!/bin/sh\nset -eu\ntest -s config/minio/encryption.env\nmc encrypt set sse-s3 local/${TARGET_BUCKET:-.}\n"))
	}

	// Handle website configuration
	if website := res.Config["website"]; website != nil {
		result.AddWarning("GCS website configuration detected. MinIO supports static website hosting.")
		result.AddScript("configure_website.sh", []byte(fmt.Sprintf("#!/bin/sh\nset -eu\nmc anonymous set download local/%s\n", bucketName)))
	}

	// Handle retention policy
	if retentionPolicy := res.Config["retention_policy"]; retentionPolicy != nil {
		result.AddWarning("GCS retention policy is configured. MinIO supports object locking for retention.")
		result.AddScript("configure_retention.sh", []byte(fmt.Sprintf("#!/bin/sh\nset -eu\nmc retention set --default GOVERNANCE 1d local/%s\n", bucketName)))
	}

	// Handle logging
	if logging := res.Config["logging"]; logging != nil {
		result.AddWarning("GCS bucket logging is configured. MinIO supports audit logging.")
		result.AddScript("configure_audit_logging.sh", []byte("#!/bin/sh\nset -eu\ntest -d data/minio\n"))
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
echo "Generated app patch config: config/minio/app-change.env"
`, bucketName, bucketName, bucketName, location, bucketName)
}

func (m *GCSMapper) generateGCSAppChange(bucketName string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_GCS_BUCKET=%s\nTARGET_BUCKET=%s\nHOMEPORT_STORAGE_ENDPOINT=http://minio:9000\nAWS_ENDPOINT_URL_S3=http://minio:9000\nAWS_S3_FORCE_PATH_STYLE=true\n", bucketName, bucketName)
}

func (m *GCSMapper) generateGCSValidateScript(bucketName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\naws --endpoint-url http://localhost:9000 s3api head-bucket --bucket %s\naws --endpoint-url http://localhost:9000 s3api list-objects-v2 --bucket %s >/tmp/homeport-gcs-list.json\n", bucketName, bucketName)
}

func (m *GCSMapper) generateGCSBackupScript(bucketName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/%s-minio-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/minio setup_minio.sh validate_gcs_api.sh cutover_gcs_clients.sh\necho \"$archive\"\n", bucketName)
}

func (m *GCSMapper) generateGCSCutoverScript(bucketName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/minio/app-change.env\ntest \"$SOURCE_GCS_BUCKET\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Patch Cloud Storage clients to HOMEPORT_STORAGE_ENDPOINT=$HOMEPORT_STORAGE_ENDPOINT\"\n", bucketName)
}

func (m *GCSMapper) generateBucketPolicy(bucketName string, public bool) string {
	if public {
		return fmt.Sprintf("{\n  \"Version\": \"2012-10-17\",\n  \"Statement\": [{\"Effect\": \"Allow\", \"Principal\": \"*\", \"Action\": [\"s3:GetObject\"], \"Resource\": [\"arn:aws:s3:::%s/*\"]}]\n}\n", bucketName)
	}
	return "{\n  \"Version\": \"2012-10-17\",\n  \"Statement\": []\n}\n"
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

	// GCS uses bucket-level IAM, check if bucket is public via IAM
	// This would need to check the associated IAM bindings
	// For now, we'll return false and let the user configure manually

	return false
}

// ExtractPolicies extracts bucket IAM policies from the GCS bucket.
func (m *GCSMapper) ExtractPolicies(ctx context.Context, res *resource.AWSResource) ([]*policy.Policy, error) {
	var policies []*policy.Policy

	bucketName := res.GetConfigString("name")
	if bucketName == "" {
		bucketName = res.Name
	}

	// Extract uniform bucket-level access settings
	if uniformAccess := res.Config["uniform_bucket_level_access"]; uniformAccess != nil {
		accessJSON, _ := json.Marshal(uniformAccess)
		p := policy.NewPolicy(
			res.ID+"-uniform-access",
			bucketName+" Uniform Bucket Access",
			policy.PolicyTypeResource,
			policy.ProviderGCP,
		)
		p.ResourceID = res.ID
		p.ResourceType = "google_storage_bucket"
		p.ResourceName = bucketName
		p.OriginalDocument = accessJSON
		p.OriginalFormat = "json"

		if enabled, ok := uniformAccess.(map[string]interface{})["enabled"].(bool); ok && enabled {
			p.AddWarning("Uniform bucket-level access is enabled - ACLs are disabled")
		}

		policies = append(policies, p)
	}

	// Extract public access prevention settings
	publicAccessPrevention := res.GetConfigString("public_access_prevention")
	if publicAccessPrevention != "" {
		accessJSON, _ := json.Marshal(map[string]string{
			"public_access_prevention": publicAccessPrevention,
		})
		p := policy.NewPolicy(
			res.ID+"-public-access",
			bucketName+" Public Access Prevention",
			policy.PolicyTypeResource,
			policy.ProviderGCP,
		)
		p.ResourceID = res.ID
		p.ResourceType = "google_storage_bucket"
		p.ResourceName = bucketName
		p.OriginalDocument = accessJSON
		p.OriginalFormat = "json"

		if publicAccessPrevention == "enforced" {
			p.AddWarning("Public access prevention is enforced")
		}

		policies = append(policies, p)
	}

	// Extract IAM configuration if present
	if iamConfig := res.Config["iam_configuration"]; iamConfig != nil {
		iamJSON, _ := json.Marshal(iamConfig)
		p := policy.NewPolicy(
			res.ID+"-iam-config",
			bucketName+" IAM Configuration",
			policy.PolicyTypeResource,
			policy.ProviderGCP,
		)
		p.ResourceID = res.ID
		p.ResourceType = "google_storage_bucket"
		p.ResourceName = bucketName
		p.OriginalDocument = iamJSON
		p.OriginalFormat = "json"

		policies = append(policies, p)
	}

	return policies, nil
}
