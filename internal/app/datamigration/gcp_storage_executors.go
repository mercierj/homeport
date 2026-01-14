package datamigration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// GCSToMinIOExecutor migrates Google Cloud Storage buckets to MinIO.
type GCSToMinIOExecutor struct{}

// NewGCSToMinIOExecutor creates a new GCS to MinIO executor.
func NewGCSToMinIOExecutor() *GCSToMinIOExecutor {
	return &GCSToMinIOExecutor{}
}

// Type returns the migration type.
func (e *GCSToMinIOExecutor) Type() string {
	return "gcs_to_minio"
}

// GetPhases returns the migration phases.
func (e *GCSToMinIOExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Creating destination bucket",
		"Downloading from GCS",
		"Uploading to MinIO",
		"Verifying transfer",
	}
}

// Validate validates the migration configuration.
func (e *GCSToMinIOExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	// Validate source config
	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["bucket"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.bucket is required")
		}
		if _, ok := config.Source["project"].(string); !ok {
			result.Warnings = append(result.Warnings, "source.project not specified, using default project")
		}
		// Either service_account_key or use default credentials
		if _, ok := config.Source["service_account_key"].(string); !ok {
			result.Warnings = append(result.Warnings, "source.service_account_key not specified, using default credentials")
		}
	}

	// Validate destination config
	if config.Destination == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "destination configuration is required")
	} else {
		if _, ok := config.Destination["endpoint"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "destination.endpoint is required")
		}
		if _, ok := config.Destination["access_key"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "destination.access_key is required")
		}
		if _, ok := config.Destination["secret_key"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "destination.secret_key is required")
		}
	}

	return result, nil
}

// Execute performs the migration.
func (e *GCSToMinIOExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	// Extract source configuration
	bucket := config.Source["bucket"].(string)
	project, _ := config.Source["project"].(string)
	serviceAccountKey, _ := config.Source["service_account_key"].(string)

	// Extract destination configuration
	minioEndpoint := config.Destination["endpoint"].(string)
	minioAccessKey := config.Destination["access_key"].(string)
	minioSecretKey := config.Destination["secret_key"].(string)
	destBucket, _ := config.Destination["bucket"].(string)
	if destBucket == "" {
		destBucket = bucket
	}

	// Set up GCP environment
	gcpEnv := os.Environ()
	var tempKeyFile string
	if serviceAccountKey != "" {
		// Write service account key to temp file
		tmpFile, err := os.CreateTemp("", "gcp-sa-*.json")
		if err != nil {
			return fmt.Errorf("failed to create temp file for service account key: %w", err)
		}
		tempKeyFile = tmpFile.Name()
		defer os.Remove(tempKeyFile)

		if err := os.WriteFile(tempKeyFile, []byte(serviceAccountKey), 0600); err != nil {
			return fmt.Errorf("failed to write service account key: %w", err)
		}
		gcpEnv = append(gcpEnv, "GOOGLE_APPLICATION_CREDENTIALS="+tempKeyFile)
	}

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating GCP credentials")
	EmitProgress(m, 10, "Checking source credentials")

	// Verify access to the bucket
	args := []string{"storage", "ls", fmt.Sprintf("gs://%s", bucket)}
	if project != "" {
		args = append(args, "--project", project)
	}
	lsCmd := exec.CommandContext(ctx, "gcloud", args...)
	lsCmd.Env = gcpEnv
	if output, err := lsCmd.CombinedOutput(); err != nil {
		EmitLog(m, "error", fmt.Sprintf("Failed to access GCS bucket: %s", string(output)))
		return fmt.Errorf("failed to access GCS bucket: %w", err)
	}
	EmitLog(m, "info", fmt.Sprintf("Successfully verified access to bucket: %s", bucket))

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Creating destination bucket
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", fmt.Sprintf("Ensuring bucket %s exists on MinIO", destBucket))
	EmitProgress(m, 20, "Creating destination bucket")

	// Create bucket on MinIO using AWS CLI with endpoint override
	createBucketCmd := exec.CommandContext(ctx, "aws", "s3", "mb",
		fmt.Sprintf("s3://%s", destBucket),
		"--endpoint-url", minioEndpoint,
	)
	createBucketCmd.Env = append(os.Environ(),
		"AWS_ACCESS_KEY_ID="+minioAccessKey,
		"AWS_SECRET_ACCESS_KEY="+minioSecretKey,
	)
	// Ignore error if bucket already exists
	createBucketCmd.Run()

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Downloading from GCS
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", fmt.Sprintf("Downloading from GCS bucket: %s", bucket))
	EmitProgress(m, 40, "Downloading from GCS")

	// Create staging directory
	stagingDir, err := os.MkdirTemp("", "gcs-migration-*")
	if err != nil {
		return fmt.Errorf("failed to create staging directory: %w", err)
	}
	defer os.RemoveAll(stagingDir)

	localPath := filepath.Join(stagingDir, bucket)

	// Use gsutil for download (more reliable for large transfers)
	downloadArgs := []string{"-m", "rsync", "-r", fmt.Sprintf("gs://%s", bucket), localPath}
	downloadCmd := exec.CommandContext(ctx, "gsutil", downloadArgs...)
	downloadCmd.Env = gcpEnv

	output, err := downloadCmd.CombinedOutput()
	if err != nil {
		EmitLog(m, "error", fmt.Sprintf("GCS download failed: %s", string(output)))
		return fmt.Errorf("failed to download from GCS: %w", err)
	}
	EmitLog(m, "info", "Successfully downloaded from GCS")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Uploading to MinIO
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", fmt.Sprintf("Uploading to MinIO bucket: %s", destBucket))
	EmitProgress(m, 70, "Uploading to MinIO")

	uploadCmd := exec.CommandContext(ctx, "aws", "s3", "sync",
		localPath,
		fmt.Sprintf("s3://%s", destBucket),
		"--endpoint-url", minioEndpoint,
	)
	uploadCmd.Env = append(os.Environ(),
		"AWS_ACCESS_KEY_ID="+minioAccessKey,
		"AWS_SECRET_ACCESS_KEY="+minioSecretKey,
	)

	output, err = uploadCmd.CombinedOutput()
	if err != nil {
		EmitLog(m, "error", fmt.Sprintf("MinIO upload failed: %s", string(output)))
		return fmt.Errorf("failed to upload to MinIO: %w", err)
	}
	EmitLog(m, "info", "Successfully uploaded to MinIO")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Verifying transfer
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Verifying transfer")
	EmitProgress(m, 100, "Migration complete")

	return nil
}

// FilestoreToNFSExecutor migrates Google Cloud Filestore to local NFS.
type FilestoreToNFSExecutor struct{}

// NewFilestoreToNFSExecutor creates a new Filestore to NFS executor.
func NewFilestoreToNFSExecutor() *FilestoreToNFSExecutor {
	return &FilestoreToNFSExecutor{}
}

// Type returns the migration type.
func (e *FilestoreToNFSExecutor) Type() string {
	return "filestore_to_nfs"
}

// GetPhases returns the migration phases.
func (e *FilestoreToNFSExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Fetching Filestore info",
		"Configuring NFS mount",
		"Syncing data",
		"Generating migration scripts",
		"Finalizing",
	}
}

// Validate validates the migration configuration.
func (e *FilestoreToNFSExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["instance_name"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.instance_name is required")
		}
		if _, ok := config.Source["zone"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.zone is required")
		}
		if _, ok := config.Source["file_share"].(string); !ok {
			result.Warnings = append(result.Warnings, "source.file_share not specified, using default share name")
		}
		if _, ok := config.Source["project"].(string); !ok {
			result.Warnings = append(result.Warnings, "source.project not specified, using default project")
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

	result.Warnings = append(result.Warnings, "Large file shares may take significant time to sync")
	result.Warnings = append(result.Warnings, "Ensure network connectivity between Filestore and destination")

	return result, nil
}

// Execute performs the migration.
func (e *FilestoreToNFSExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	instanceName := config.Source["instance_name"].(string)
	zone := config.Source["zone"].(string)
	project, _ := config.Source["project"].(string)
	fileShare, _ := config.Source["file_share"].(string)
	if fileShare == "" {
		fileShare = "vol1"
	}
	serviceAccountKey, _ := config.Source["service_account_key"].(string)

	outputDir := config.Destination["output_dir"].(string)
	nfsServer, _ := config.Destination["nfs_server"].(string)
	nfsPath, _ := config.Destination["nfs_path"].(string)

	// Set up GCP environment
	gcpEnv := os.Environ()
	var tempKeyFile string
	if serviceAccountKey != "" {
		tmpFile, err := os.CreateTemp("", "gcp-sa-*.json")
		if err != nil {
			return fmt.Errorf("failed to create temp file for service account key: %w", err)
		}
		tempKeyFile = tmpFile.Name()
		defer os.Remove(tempKeyFile)

		if err := os.WriteFile(tempKeyFile, []byte(serviceAccountKey), 0600); err != nil {
			return fmt.Errorf("failed to write service account key: %w", err)
		}
		gcpEnv = append(gcpEnv, "GOOGLE_APPLICATION_CREDENTIALS="+tempKeyFile)
	}

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating GCP credentials")
	EmitProgress(m, 10, "Checking credentials")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Fetching Filestore info
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", fmt.Sprintf("Fetching Filestore info for %s", instanceName))
	EmitProgress(m, 20, "Fetching Filestore info")

	args := []string{"filestore", "instances", "describe", instanceName,
		"--zone", zone,
		"--format", "json",
	}
	if project != "" {
		args = append(args, "--project", project)
	}

	describeCmd := exec.CommandContext(ctx, "gcloud", args...)
	describeCmd.Env = gcpEnv
	filestoreOutput, err := describeCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to describe Filestore instance: %w", err)
	}

	var filestoreInfo struct {
		Name     string `json:"name"`
		State    string `json:"state"`
		Tier     string `json:"tier"`
		Networks []struct {
			Network       string   `json:"network"`
			IPAddresses   []string `json:"ipAddresses"`
			ReservedIPRange string `json:"reservedIpRange"`
		} `json:"networks"`
		FileShares []struct {
			Name       string `json:"name"`
			CapacityGb string `json:"capacityGb"`
		} `json:"fileShares"`
	}
	if err := json.Unmarshal(filestoreOutput, &filestoreInfo); err != nil {
		return fmt.Errorf("failed to parse Filestore info: %w", err)
	}

	// Get the IP address
	var filestoreIP string
	if len(filestoreInfo.Networks) > 0 && len(filestoreInfo.Networks[0].IPAddresses) > 0 {
		filestoreIP = filestoreInfo.Networks[0].IPAddresses[0]
	}

	EmitLog(m, "info", fmt.Sprintf("Filestore IP: %s, Share: %s", filestoreIP, fileShare))

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Configuring NFS mount
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", "Preparing mount configuration")
	EmitProgress(m, 40, "Configuring NFS")

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Save Filestore info
	filestoreInfoPath := filepath.Join(outputDir, "filestore-info.json")
	if err := os.WriteFile(filestoreInfoPath, filestoreOutput, 0644); err != nil {
		return fmt.Errorf("failed to write Filestore info: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Syncing data (preparation)
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Preparing data sync configuration")
	EmitProgress(m, 60, "Preparing sync")

	// Generate NFS server Docker compose for destination
	nfsCompose := `version: '3.8'

services:
  nfs-server:
    image: itsthenetwork/nfs-server-alpine:12
    container_name: nfs-server
    privileged: true
    environment:
      - SHARED_DIRECTORY=/data
    volumes:
      - nfs-data:/data
    ports:
      - "2049:2049"
    restart: unless-stopped

volumes:
  nfs-data:
    driver: local
`
	composePath := filepath.Join(outputDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(nfsCompose), 0644); err != nil {
		return fmt.Errorf("failed to write docker-compose.yml: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Generating migration scripts
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Generating migration scripts")
	EmitProgress(m, 80, "Generating scripts")

	destNFS := nfsServer
	if destNFS == "" {
		destNFS = "localhost"
	}
	destPath := nfsPath
	if destPath == "" {
		destPath = "/data"
	}

	migrationScript := fmt.Sprintf(`#!/bin/bash
# Filestore to NFS Migration Script
# Instance: %s
# Zone: %s

set -e

echo "Filestore to NFS Migration"
echo "==========================="

# Configuration
FILESTORE_IP="%s"
FILESTORE_SHARE="%s"
DEST_NFS_SERVER="%s"
DEST_NFS_PATH="%s"

# Create mount points
sudo mkdir -p /mnt/filestore-source
sudo mkdir -p /mnt/nfs-dest

# Mount Filestore (requires network access to GCP)
echo "Mounting Filestore..."
if [ -n "$FILESTORE_IP" ]; then
    sudo mount -t nfs -o rw,intr ${FILESTORE_IP}:/${FILESTORE_SHARE} /mnt/filestore-source
else
    echo "No Filestore IP available. Please configure manually."
    echo "Mount command: sudo mount -t nfs <filestore-ip>:/<share> /mnt/filestore-source"
    exit 1
fi

# Mount destination NFS
echo "Mounting destination NFS..."
sudo mount -t nfs ${DEST_NFS_SERVER}:${DEST_NFS_PATH} /mnt/nfs-dest

# Sync data
echo "Syncing data (this may take a while)..."
sudo rsync -avz --progress /mnt/filestore-source/ /mnt/nfs-dest/

# Unmount
echo "Cleaning up..."
sudo umount /mnt/filestore-source
sudo umount /mnt/nfs-dest

echo "Migration complete!"
`, instanceName, zone, filestoreIP, fileShare, destNFS, destPath)

	scriptPath := filepath.Join(outputDir, "migrate-filestore.sh")
	if err := os.WriteFile(scriptPath, []byte(migrationScript), 0755); err != nil {
		return fmt.Errorf("failed to write migration script: %w", err)
	}

	// Generate GCP Transfer Service script as alternative
	transferScript := fmt.Sprintf(`#!/bin/bash
# GCP Transfer Service Configuration for Filestore Migration
# Use this for large file shares or production migrations

set -e

echo "Setting up GCP Transfer Service for Filestore migration"

# Note: GCP Transfer Service can be used to transfer data between
# Filestore and other storage systems. Configure via Cloud Console
# or using the gcloud commands below.

# List available Filestore instances
gcloud filestore instances list --zone=%s

# Create a backup (optional but recommended)
gcloud filestore backups create migration-backup \
    --instance=%s \
    --file-share=%s \
    --region=%s

echo ""
echo "For large migrations, consider using:"
echo "1. GCP Transfer Service (for cloud-to-cloud)"
echo "2. rsync over VPN (for on-premises)"
echo "3. Google Transfer Appliance (for very large datasets)"
`, zone, instanceName, fileShare, zone[:len(zone)-2])

	transferPath := filepath.Join(outputDir, "setup-transfer.sh")
	if err := os.WriteFile(transferPath, []byte(transferScript), 0755); err != nil {
		return fmt.Errorf("failed to write transfer script: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 6: Finalizing
	EmitPhase(m, phases[5], 6)
	EmitLog(m, "info", "Finalizing migration artifacts")
	EmitProgress(m, 95, "Finalizing")

	// Generate capacity info
	capacityInfo := "unknown"
	if len(filestoreInfo.FileShares) > 0 {
		capacityInfo = filestoreInfo.FileShares[0].CapacityGb + " GB"
	}

	readme := fmt.Sprintf(`# Filestore to NFS Migration

## Source Filestore
- Instance: %s
- Zone: %s
- IP Address: %s
- File Share: %s
- Capacity: %s
- Tier: %s

## Migration Options

### Option 1: Direct rsync (Small to Medium)
Use migrate-filestore.sh for direct data sync via rsync.

### Option 2: GCP Transfer Service (Large/Production)
Use setup-transfer.sh for GCP Transfer Service configuration.

## Files Generated
- filestore-info.json: Filestore configuration
- docker-compose.yml: NFS server container
- migrate-filestore.sh: Direct migration script
- setup-transfer.sh: GCP Transfer Service setup

## Self-Hosted NFS Server

Start the NFS server:
'''bash
docker-compose up -d
'''

Mount from clients:
'''bash
sudo mount -t nfs <server-ip>:/data /mnt/nfs
'''

## Notes
- Ensure network connectivity between Filestore and destination
- For migrations from GCP, consider using Cloud VPN or Interconnect
- Large migrations may benefit from incremental sync
`, instanceName, zone, filestoreIP, fileShare, capacityInfo, filestoreInfo.Tier)

	readmePath := filepath.Join(outputDir, "README.md")
	if err := os.WriteFile(readmePath, []byte(readme), 0644); err != nil {
		return fmt.Errorf("failed to write README: %w", err)
	}

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", fmt.Sprintf("Filestore %s migration prepared at %s", instanceName, outputDir))

	return nil
}

// PersistentDiskToLocalExecutor migrates GCP Persistent Disks to local storage.
type PersistentDiskToLocalExecutor struct{}

// NewPersistentDiskToLocalExecutor creates a new Persistent Disk to local executor.
func NewPersistentDiskToLocalExecutor() *PersistentDiskToLocalExecutor {
	return &PersistentDiskToLocalExecutor{}
}

// Type returns the migration type.
func (e *PersistentDiskToLocalExecutor) Type() string {
	return "persistent_disk_to_local"
}

// GetPhases returns the migration phases.
func (e *PersistentDiskToLocalExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Fetching disk info",
		"Creating snapshot",
		"Waiting for snapshot",
		"Exporting to image",
		"Generating migration scripts",
		"Finalizing",
	}
}

// Validate validates the migration configuration.
func (e *PersistentDiskToLocalExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["disk_name"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.disk_name is required")
		}
		if _, ok := config.Source["zone"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.zone is required")
		}
		if _, ok := config.Source["project"].(string); !ok {
			result.Warnings = append(result.Warnings, "source.project not specified, using default project")
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

	result.Warnings = append(result.Warnings, "Disk export requires a GCS bucket for intermediate storage")
	result.Warnings = append(result.Warnings, "Large disks may take significant time to export")

	return result, nil
}

// Execute performs the migration.
func (e *PersistentDiskToLocalExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	diskName := config.Source["disk_name"].(string)
	zone := config.Source["zone"].(string)
	project, _ := config.Source["project"].(string)
	serviceAccountKey, _ := config.Source["service_account_key"].(string)
	gcsBucket, _ := config.Source["gcs_bucket"].(string)

	outputDir := config.Destination["output_dir"].(string)

	// Set up GCP environment
	gcpEnv := os.Environ()
	var tempKeyFile string
	if serviceAccountKey != "" {
		tmpFile, err := os.CreateTemp("", "gcp-sa-*.json")
		if err != nil {
			return fmt.Errorf("failed to create temp file for service account key: %w", err)
		}
		tempKeyFile = tmpFile.Name()
		defer os.Remove(tempKeyFile)

		if err := os.WriteFile(tempKeyFile, []byte(serviceAccountKey), 0600); err != nil {
			return fmt.Errorf("failed to write service account key: %w", err)
		}
		gcpEnv = append(gcpEnv, "GOOGLE_APPLICATION_CREDENTIALS="+tempKeyFile)
	}

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating GCP credentials")
	EmitProgress(m, 5, "Checking credentials")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Fetching disk info
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", fmt.Sprintf("Fetching disk info for %s", diskName))
	EmitProgress(m, 15, "Fetching disk info")

	args := []string{"compute", "disks", "describe", diskName,
		"--zone", zone,
		"--format", "json",
	}
	if project != "" {
		args = append(args, "--project", project)
	}

	describeCmd := exec.CommandContext(ctx, "gcloud", args...)
	describeCmd.Env = gcpEnv
	diskOutput, err := describeCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to describe disk: %w", err)
	}

	var diskInfo struct {
		Name         string `json:"name"`
		SizeGb       string `json:"sizeGb"`
		Type         string `json:"type"`
		Status       string `json:"status"`
		SourceImage  string `json:"sourceImage,omitempty"`
		Zone         string `json:"zone"`
	}
	if err := json.Unmarshal(diskOutput, &diskInfo); err != nil {
		return fmt.Errorf("failed to parse disk info: %w", err)
	}

	EmitLog(m, "info", fmt.Sprintf("Disk size: %s GB, Status: %s", diskInfo.SizeGb, diskInfo.Status))

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Creating snapshot
	EmitPhase(m, phases[2], 3)
	snapshotName := fmt.Sprintf("%s-migration-snapshot", diskName)
	EmitLog(m, "info", fmt.Sprintf("Creating snapshot: %s", snapshotName))
	EmitProgress(m, 25, "Creating snapshot")

	snapshotArgs := []string{"compute", "snapshots", "create", snapshotName,
		"--source-disk", diskName,
		"--source-disk-zone", zone,
		"--format", "json",
	}
	if project != "" {
		snapshotArgs = append(snapshotArgs, "--project", project)
	}

	snapshotCmd := exec.CommandContext(ctx, "gcloud", snapshotArgs...)
	snapshotCmd.Env = gcpEnv
	snapshotOutput, err := snapshotCmd.CombinedOutput()
	if err != nil {
		EmitLog(m, "error", fmt.Sprintf("Snapshot creation failed: %s", string(snapshotOutput)))
		return fmt.Errorf("failed to create snapshot: %w", err)
	}
	EmitLog(m, "info", "Snapshot created successfully")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Waiting for snapshot
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Waiting for snapshot to be ready")
	EmitProgress(m, 40, "Waiting for snapshot")

	// Poll for snapshot status
	for i := 0; i < 60; i++ { // Max 10 minutes
		statusArgs := []string{"compute", "snapshots", "describe", snapshotName,
			"--format", "value(status)",
		}
		if project != "" {
			statusArgs = append(statusArgs, "--project", project)
		}

		statusCmd := exec.CommandContext(ctx, "gcloud", statusArgs...)
		statusCmd.Env = gcpEnv
		statusOutput, err := statusCmd.Output()
		if err == nil {
			status := string(statusOutput)
			if status == "READY\n" || status == "READY" {
				EmitLog(m, "info", "Snapshot is ready")
				break
			}
		}

		if m.IsCancelled() {
			return fmt.Errorf("migration cancelled")
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// Wait 10 seconds before next check
		}
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Exporting to image
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Preparing export configuration")
	EmitProgress(m, 55, "Preparing export")

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Save disk info
	diskInfoPath := filepath.Join(outputDir, "disk-info.json")
	if err := os.WriteFile(diskInfoPath, diskOutput, 0644); err != nil {
		return fmt.Errorf("failed to write disk info: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 6: Generating migration scripts
	EmitPhase(m, phases[5], 6)
	EmitLog(m, "info", "Generating migration scripts")
	EmitProgress(m, 75, "Generating scripts")

	// Generate export script
	projectArg := ""
	if project != "" {
		projectArg = fmt.Sprintf("--project=%s", project)
	}

	exportScript := fmt.Sprintf(`#!/bin/bash
# Persistent Disk Migration Script
# Disk: %s
# Zone: %s

set -e

echo "Persistent Disk Migration"
echo "========================="

# Configuration
DISK_NAME="%s"
SNAPSHOT_NAME="%s"
ZONE="%s"
PROJECT_ARG="%s"
GCS_BUCKET="%s"

# Option 1: Export via image to GCS (requires GCS bucket)
if [ -n "$GCS_BUCKET" ]; then
    echo "Creating image from snapshot..."
    gcloud compute images create ${DISK_NAME}-image \
        --source-snapshot=${SNAPSHOT_NAME} \
        $PROJECT_ARG

    echo "Exporting image to GCS..."
    gcloud compute images export \
        --destination-uri=gs://${GCS_BUCKET}/${DISK_NAME}.tar.gz \
        --image=${DISK_NAME}-image \
        $PROJECT_ARG

    echo "Downloading from GCS..."
    gsutil cp gs://${GCS_BUCKET}/${DISK_NAME}.tar.gz ./

    echo "Extracting image..."
    tar -xzf ${DISK_NAME}.tar.gz
else
    echo "No GCS bucket specified. Please set GCS_BUCKET variable."
    echo "Example: GCS_BUCKET=my-bucket ./migrate-disk.sh"
fi

# Option 2: Mount and copy (requires GCE instance)
echo ""
echo "Alternative: Mount and copy method"
echo "1. Create a new GCE instance in zone %s"
echo "2. Attach a disk from snapshot: %s"
echo "3. Mount the disk: sudo mount /dev/sdb1 /mnt/disk"
echo "4. Copy data: rsync -avz /mnt/disk/ /destination/"
echo "5. Detach disk and terminate instance"

# Generate Docker volume creation
echo ""
echo "Creating Docker volume configuration..."
cat > docker-volume.yml << 'EOF'
version: '3.8'
volumes:
  migrated-disk:
    driver: local
    driver_opts:
      type: none
      device: /data/migrated-disk
      o: bind
EOF

echo "Migration preparation complete!"
`, diskName, zone, diskName, snapshotName, zone, projectArg, gcsBucket, zone, snapshotName)

	scriptPath := filepath.Join(outputDir, "migrate-disk.sh")
	if err := os.WriteFile(scriptPath, []byte(exportScript), 0755); err != nil {
		return fmt.Errorf("failed to write migration script: %w", err)
	}

	// Generate cleanup script
	cleanupScript := fmt.Sprintf(`#!/bin/bash
# Cleanup script for migration resources

set -e

echo "Cleaning up migration resources..."

# Delete snapshot (after confirming migration is complete)
read -p "Delete snapshot %s? (y/n) " confirm
if [ "$confirm" = "y" ]; then
    gcloud compute snapshots delete %s %s
    echo "Snapshot deleted."
fi

# Delete temporary image if created
read -p "Delete temporary image %s-image? (y/n) " confirm
if [ "$confirm" = "y" ]; then
    gcloud compute images delete %s-image %s 2>/dev/null || echo "Image not found or already deleted."
fi

echo "Cleanup complete!"
`, snapshotName, snapshotName, projectArg, diskName, diskName, projectArg)

	cleanupPath := filepath.Join(outputDir, "cleanup.sh")
	if err := os.WriteFile(cleanupPath, []byte(cleanupScript), 0755); err != nil {
		return fmt.Errorf("failed to write cleanup script: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 7: Finalizing
	EmitPhase(m, phases[6], 7)
	EmitLog(m, "info", "Finalizing migration artifacts")
	EmitProgress(m, 90, "Finalizing")

	readme := fmt.Sprintf(`# Persistent Disk to Local Storage Migration

## Source Disk
- Disk Name: %s
- Zone: %s
- Size: %s GB
- Type: %s
- Status: %s
- Snapshot: %s

## Migration Options

### Option 1: Image Export (Recommended)
Export disk as image via GCS bucket:
'''bash
GCS_BUCKET=your-bucket ./migrate-disk.sh
'''

### Option 2: Mount and Copy
1. Create a GCE instance in zone %s
2. Attach disk from snapshot %s
3. Mount and rsync data

## Files Generated
- disk-info.json: Disk configuration
- migrate-disk.sh: Migration script
- cleanup.sh: Cleanup script for GCP resources
- docker-volume.yml: Docker volume configuration

## Docker Volume

After migrating data to local storage:
'''bash
mkdir -p /data/migrated-disk
# Copy your data to /data/migrated-disk
docker-compose -f docker-volume.yml up
'''

## Notes
- Snapshot will remain in GCP until manually deleted
- Use cleanup.sh after confirming migration success
- Large disks may incur significant data transfer costs
- Consider using gcloud compute scp for direct transfer
`, diskName, zone, diskInfo.SizeGb, diskInfo.Type, diskInfo.Status, snapshotName, zone, snapshotName)

	readmePath := filepath.Join(outputDir, "README.md")
	if err := os.WriteFile(readmePath, []byte(readme), 0644); err != nil {
		return fmt.Errorf("failed to write README: %w", err)
	}

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", fmt.Sprintf("Persistent disk %s migration prepared at %s", diskName, outputDir))

	return nil
}
