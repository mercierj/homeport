package stacks

import (
	"context"
	"fmt"
	"strings"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	"github.com/homeport/homeport/internal/domain/stack"
	"github.com/homeport/homeport/internal/infrastructure/consolidator"
)

// StorageMerger consolidates object storage resources into a single MinIO stack.
// It handles S3, GCS, Azure Blob Storage, and similar object storage resources.
type StorageMerger struct {
	*consolidator.BaseMerger
}

// NewStorageMerger creates a new StorageMerger instance.
func NewStorageMerger() *StorageMerger {
	return &StorageMerger{
		BaseMerger: consolidator.NewBaseMerger(stack.StackTypeStorage),
	}
}

// StackType returns the stack type this merger handles.
func (m *StorageMerger) StackType() stack.StackType {
	return stack.StackTypeStorage
}

// CanMerge checks if this merger can handle the given results.
// Returns true if there are storage resources to consolidate.
func (m *StorageMerger) CanMerge(results []*mapper.MappingResult) bool {
	if len(results) == 0 {
		return false
	}

	// Check if any result is a storage resource
	for _, result := range results {
		if result == nil {
			continue
		}
		if isStorageResource(result.SourceResourceType) {
			return true
		}
	}

	return false
}

// Merge creates a consolidated storage stack with MinIO.
// It creates a bucket for each S3/GCS/Blob container from the source resources.
func (m *StorageMerger) Merge(ctx context.Context, results []*mapper.MappingResult, opts *consolidator.MergeOptions) (*stack.Stack, error) {
	if opts == nil {
		opts = consolidator.DefaultOptions()
	}

	// Create the stack
	name := "storage"
	if opts.NamePrefix != "" {
		name = opts.NamePrefix + "-" + name
	}

	stk := stack.NewStack(stack.StackTypeStorage, name)
	stk.Description = "Object storage (MinIO - S3-compatible)"

	// Create the MinIO service
	minio := stack.NewService("minio", "minio/minio:latest")
	minio.Command = []string{"server", "/data", "--console-address", ":9001"}
	minio.Ports = []string{"9000:9000", "9001:9001"}
	minio.Environment = map[string]string{
		"MINIO_ROOT_USER":     "${MINIO_ROOT_USER:-admin}",
		"MINIO_ROOT_PASSWORD": "${MINIO_ROOT_PASSWORD:-changeme123}",
		"MINIO_BROWSER":       "on",
	}
	minio.Volumes = []string{
		"minio_data:/data",
	}
	minio.HealthCheck = &stack.HealthCheck{
		Test:        []string{"CMD", "mc", "ready", "local"},
		Interval:    "30s",
		Timeout:     "10s",
		Retries:     3,
		StartPeriod: "10s",
	}
	minio.Labels = map[string]string{
		"homeport.stack":   "storage",
		"homeport.service": "minio",
		"homeport.role":    "primary",
	}

	stk.AddService(minio)

	// Add MinIO client (mc) init service to create buckets
	bucketNames := m.extractBucketNames(results)
	if len(bucketNames) > 0 {
		mcInit := stack.NewService("minio-init", "minio/mc:latest")
		mcInit.DependsOn = []string{"minio"}
		mcInit.Restart = "on-failure"
		mcInit.Labels = map[string]string{
			"homeport.stack":   "storage",
			"homeport.service": "minio-init",
			"homeport.role":    "init",
		}

		// Build entrypoint command to create buckets
		initScript := m.generateMCSetupScript(bucketNames)
		mcInit.Command = []string{
			"/bin/sh", "-c",
			string(initScript),
		}
		mcInit.Environment = map[string]string{
			"MC_HOST_myminio": "http://${MINIO_ROOT_USER:-admin}:${MINIO_ROOT_PASSWORD:-changeme123}@minio:9000",
		}

		stk.AddService(mcInit)
	}

	// Add volume for MinIO data
	stk.AddVolume(stack.Volume{
		Name:   "minio_data",
		Driver: "local",
		Labels: map[string]string{
			"homeport.stack": "storage",
		},
	})

	// Generate bucket setup script for reference
	setupScript := m.generateMCSetupScript(bucketNames)
	stk.AddScript("setup-buckets.sh", setupScript)

	// Generate migration script
	migrationScript := m.generateMigrationScript(results)
	stk.AddScript("migrate-data.sh", migrationScript)

	// Optionally add nginx proxy config for custom domain support
	if opts.IncludeSupportServices {
		nginxConfig := m.generateNginxConfig(bucketNames)
		stk.AddConfig("nginx/minio-proxy.conf", nginxConfig)
	}

	// Add source resources and collect metadata
	originalBuckets := make(map[string]string) // Maps normalized name to original name

	for _, result := range results {
		if result == nil {
			continue
		}

		// Track source resource
		res := &resource.Resource{
			Type: resource.Type(result.SourceResourceType),
			Name: result.SourceResourceName,
		}
		stk.AddSourceResource(res)

		// Track original bucket names
		normalizedName := consolidator.NormalizeName(result.SourceResourceName)
		originalBuckets[normalizedName] = result.SourceResourceName
	}

	// Store bucket mapping in metadata
	for normalized, original := range originalBuckets {
		stk.Metadata["bucket_"+normalized] = original
	}
	stk.Metadata["total_buckets"] = fmt.Sprintf("%d", len(bucketNames))

	// Add storage-specific manual steps
	manualSteps := []string{
		"Update application configuration to use MinIO endpoint (http://localhost:9000)",
		"Update AWS SDK configuration to use path-style URLs (required for MinIO)",
		"Migrate existing data using rclone, aws s3 sync, or mc mirror",
		"Review and apply bucket policies for access control",
		"Configure lifecycle rules for object expiration if needed",
		"Set up bucket versioning if required",
		"Configure event notifications if using S3 event triggers",
	}

	// Add provider-specific migration steps
	providerMigrationSteps := m.getProviderMigrationSteps(results)
	manualSteps = append(manualSteps, providerMigrationSteps...)

	// Store manual steps in metadata
	for i, step := range manualSteps {
		stk.Metadata[fmt.Sprintf("manual_step_%d", i)] = step
	}

	return stk, nil
}

// extractBucketNames extracts unique bucket names from mapping results.
func (m *StorageMerger) extractBucketNames(results []*mapper.MappingResult) []string {
	bucketSet := make(map[string]bool)
	var buckets []string

	for _, result := range results {
		if result == nil {
			continue
		}

		// Normalize the bucket name for MinIO compatibility
		name := consolidator.NormalizeName(result.SourceResourceName)
		if name == "" {
			continue
		}

		// MinIO bucket names must be 3-63 characters
		if len(name) < 3 {
			name = name + "-bucket"
		}
		if len(name) > 63 {
			name = name[:63]
		}

		if !bucketSet[name] {
			bucketSet[name] = true
			buckets = append(buckets, name)
		}
	}

	return buckets
}

// generateMCSetupScript generates a MinIO client setup script to create buckets.
func (m *StorageMerger) generateMCSetupScript(buckets []string) []byte {
	var sb strings.Builder

	sb.WriteString("#!/bin/sh\n")
	sb.WriteString("# MinIO bucket setup script\n")
	sb.WriteString("# Generated by Homeport\n\n")

	sb.WriteString("set -e\n\n")

	sb.WriteString("# Wait for MinIO to be ready\n")
	sb.WriteString("echo 'Waiting for MinIO to be ready...'\n")
	sb.WriteString("until mc ready myminio 2>/dev/null; do\n")
	sb.WriteString("    echo 'MinIO not ready yet, waiting...'\n")
	sb.WriteString("    sleep 2\n")
	sb.WriteString("done\n")
	sb.WriteString("echo 'MinIO is ready!'\n\n")

	sb.WriteString("# Create buckets\n")
	for _, bucket := range buckets {
		sb.WriteString(fmt.Sprintf("echo 'Creating bucket: %s'\n", bucket))
		sb.WriteString(fmt.Sprintf("mc mb --ignore-existing myminio/%s\n", bucket))
	}

	sb.WriteString("\n# List all buckets\n")
	sb.WriteString("echo 'Buckets created:'\n")
	sb.WriteString("mc ls myminio\n")

	sb.WriteString("\necho 'Bucket setup complete!'\n")

	return []byte(sb.String())
}

// generateMigrationScript generates a data migration script for different cloud providers.
func (m *StorageMerger) generateMigrationScript(results []*mapper.MappingResult) []byte {
	var sb strings.Builder

	sb.WriteString("#!/bin/bash\n")
	sb.WriteString("# Data Migration Script\n")
	sb.WriteString("# Generated by Homeport\n")
	sb.WriteString("# \n")
	sb.WriteString("# Prerequisites:\n")
	sb.WriteString("#   - rclone installed (https://rclone.org/install/)\n")
	sb.WriteString("#   - Cloud provider credentials configured\n")
	sb.WriteString("#   - MinIO running and accessible\n\n")

	sb.WriteString("set -e\n\n")

	sb.WriteString("# Configuration\n")
	sb.WriteString("MINIO_ENDPOINT=${MINIO_ENDPOINT:-http://localhost:9000}\n")
	sb.WriteString("MINIO_ACCESS_KEY=${MINIO_ROOT_USER:-admin}\n")
	sb.WriteString("MINIO_SECRET_KEY=${MINIO_ROOT_PASSWORD:-changeme123}\n\n")

	sb.WriteString("# Configure rclone for MinIO\n")
	sb.WriteString("echo 'Configuring rclone for MinIO...'\n")
	sb.WriteString("rclone config create minio s3 \\\n")
	sb.WriteString("    provider=Minio \\\n")
	sb.WriteString("    env_auth=false \\\n")
	sb.WriteString("    access_key_id=$MINIO_ACCESS_KEY \\\n")
	sb.WriteString("    secret_access_key=$MINIO_SECRET_KEY \\\n")
	sb.WriteString("    endpoint=$MINIO_ENDPOINT\n\n")

	// Group results by provider
	awsBuckets := []string{}
	gcsBuckets := []string{}
	azureBuckets := []string{}

	for _, result := range results {
		if result == nil {
			continue
		}
		bucket := consolidator.NormalizeName(result.SourceResourceName)
		resourceType := strings.ToLower(result.SourceResourceType)

		switch {
		case strings.Contains(resourceType, "s3") || strings.Contains(resourceType, "aws"):
			awsBuckets = append(awsBuckets, bucket)
		case strings.Contains(resourceType, "gcs") || strings.Contains(resourceType, "google"):
			gcsBuckets = append(gcsBuckets, bucket)
		case strings.Contains(resourceType, "blob") || strings.Contains(resourceType, "azure"):
			azureBuckets = append(azureBuckets, bucket)
		}
	}

	// AWS S3 migration
	if len(awsBuckets) > 0 {
		sb.WriteString("# AWS S3 Migration\n")
		sb.WriteString("echo 'Migrating from AWS S3...'\n")
		sb.WriteString("# Ensure AWS credentials are configured (aws configure or environment variables)\n\n")

		for _, bucket := range awsBuckets {
			sb.WriteString(fmt.Sprintf("echo 'Syncing bucket: %s'\n", bucket))
			sb.WriteString("# Using aws s3 sync\n")
			sb.WriteString(fmt.Sprintf("# aws s3 sync s3://%s s3://%s --endpoint-url $MINIO_ENDPOINT\n", bucket, bucket))
			sb.WriteString("# Or using rclone\n")
			sb.WriteString(fmt.Sprintf("# rclone sync aws:%s minio:%s --progress\n\n", bucket, bucket))
		}
	}

	// GCS migration
	if len(gcsBuckets) > 0 {
		sb.WriteString("# Google Cloud Storage Migration\n")
		sb.WriteString("echo 'Migrating from GCS...'\n")
		sb.WriteString("# Ensure GCS credentials are configured (GOOGLE_APPLICATION_CREDENTIALS)\n")
		sb.WriteString("# rclone config create gcs google cloud storage\n\n")

		for _, bucket := range gcsBuckets {
			sb.WriteString(fmt.Sprintf("echo 'Syncing bucket: %s'\n", bucket))
			sb.WriteString(fmt.Sprintf("# rclone sync gcs:%s minio:%s --progress\n\n", bucket, bucket))
		}
	}

	// Azure Blob migration
	if len(azureBuckets) > 0 {
		sb.WriteString("# Azure Blob Storage Migration\n")
		sb.WriteString("echo 'Migrating from Azure Blob Storage...'\n")
		sb.WriteString("# Ensure Azure credentials are configured\n")
		sb.WriteString("# rclone config create azure azureblob account=ACCOUNT key=KEY\n\n")

		for _, bucket := range azureBuckets {
			sb.WriteString(fmt.Sprintf("echo 'Syncing container: %s'\n", bucket))
			sb.WriteString(fmt.Sprintf("# rclone sync azure:%s minio:%s --progress\n\n", bucket, bucket))
		}
	}

	sb.WriteString("echo 'Migration complete!'\n")
	sb.WriteString("echo 'Verify data integrity before decommissioning source buckets.'\n")

	return []byte(sb.String())
}

// generateNginxConfig generates an nginx proxy configuration for MinIO.
func (m *StorageMerger) generateNginxConfig(buckets []string) []byte {
	config := `# Nginx configuration for MinIO proxy
# Generated by Homeport
#
# This configuration provides:
# - SSL termination
# - Custom domain support
# - Path-style and virtual-host style access

upstream minio_api {
    server minio:9000;
}

upstream minio_console {
    server minio:9001;
}

server {
    listen 80;
    listen [::]:80;
    server_name storage.local *.storage.local;

    # Redirect to HTTPS in production
    # return 301 https://$host$request_uri;

    # For local development, proxy directly
    location / {
        proxy_pass http://minio_api;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # Required for MinIO
        proxy_http_version 1.1;
        proxy_set_header Connection "";
        chunked_transfer_encoding off;

        # Large file uploads
        client_max_body_size 0;
        proxy_buffering off;
        proxy_request_buffering off;
    }
}

# MinIO Console
server {
    listen 9001;
    listen [::]:9001;
    server_name storage.local;

    location / {
        proxy_pass http://minio_console;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # WebSocket support for console
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }
}
`
	return []byte(config)
}

// getProviderMigrationSteps returns provider-specific migration guidance.
func (m *StorageMerger) getProviderMigrationSteps(results []*mapper.MappingResult) []string {
	steps := []string{}
	hasAWS := false
	hasGCP := false
	hasAzure := false

	for _, result := range results {
		if result == nil {
			continue
		}
		resourceType := strings.ToLower(result.SourceResourceType)
		switch {
		case strings.Contains(resourceType, "s3") || strings.Contains(resourceType, "aws"):
			hasAWS = true
		case strings.Contains(resourceType, "gcs") || strings.Contains(resourceType, "google"):
			hasGCP = true
		case strings.Contains(resourceType, "blob") || strings.Contains(resourceType, "azure"):
			hasAzure = true
		}
	}

	if hasAWS {
		steps = append(steps,
			"AWS S3: Use 'aws s3 sync' or 'rclone sync' for data migration",
			"AWS S3: Review S3 event notifications and map to MinIO bucket notifications",
			"AWS S3: Check for S3 Select usage - MinIO supports this feature",
		)
	}

	if hasGCP {
		steps = append(steps,
			"GCS: Use 'gsutil rsync' or 'rclone sync' for data migration",
			"GCS: Review GCS IAM permissions and map to MinIO policies",
			"GCS: Check for signed URL usage - MinIO supports presigned URLs",
		)
	}

	if hasAzure {
		steps = append(steps,
			"Azure Blob: Use 'azcopy' or 'rclone sync' for data migration",
			"Azure Blob: Review Azure SAS tokens and map to MinIO policies",
			"Azure Blob: Check for Azure CDN integration for static content",
		)
	}

	return steps
}

// isStorageResource checks if a resource type is an object storage resource.
func isStorageResource(resourceType string) bool {
	resourceType = strings.ToLower(resourceType)
	storagePatterns := []string{
		"s3", "bucket", "gcs", "storage",
		"blob", "container", "object",
	}

	// Exclude block storage patterns
	excludePatterns := []string{
		"ebs", "disk", "volume", "efs", "filestore", "files",
	}

	for _, pattern := range excludePatterns {
		if strings.Contains(resourceType, pattern) {
			return false
		}
	}

	for _, pattern := range storagePatterns {
		if strings.Contains(resourceType, pattern) {
			return true
		}
	}

	return false
}
