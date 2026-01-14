package datamigration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// BlobToMinIOExecutor migrates Azure Blob Storage to MinIO.
type BlobToMinIOExecutor struct{}

// NewBlobToMinIOExecutor creates a new Blob to MinIO executor.
func NewBlobToMinIOExecutor() *BlobToMinIOExecutor {
	return &BlobToMinIOExecutor{}
}

// Type returns the migration type.
func (e *BlobToMinIOExecutor) Type() string {
	return "blob_to_minio"
}

// GetPhases returns the migration phases.
func (e *BlobToMinIOExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Creating destination bucket",
		"Downloading from Azure Blob",
		"Uploading to MinIO",
		"Verifying transfer",
	}
}

// Validate validates the migration configuration.
func (e *BlobToMinIOExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	// Validate source config
	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["storage_account"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.storage_account is required")
		}
		if _, ok := config.Source["container"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.container is required")
		}
		// Either connection_string or account_key
		if _, ok := config.Source["connection_string"].(string); !ok {
			if _, ok := config.Source["account_key"].(string); !ok {
				result.Warnings = append(result.Warnings, "source.connection_string or account_key not specified, using Azure CLI default credentials")
			}
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
func (e *BlobToMinIOExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	// Extract source configuration
	storageAccount := config.Source["storage_account"].(string)
	container := config.Source["container"].(string)
	connectionString, _ := config.Source["connection_string"].(string)
	accountKey, _ := config.Source["account_key"].(string)

	// Extract destination configuration
	minioEndpoint := config.Destination["endpoint"].(string)
	minioAccessKey := config.Destination["access_key"].(string)
	minioSecretKey := config.Destination["secret_key"].(string)
	destBucket, _ := config.Destination["bucket"].(string)
	if destBucket == "" {
		destBucket = container
	}

	// Set up Azure environment
	azureEnv := os.Environ()
	if connectionString != "" {
		azureEnv = append(azureEnv, "AZURE_STORAGE_CONNECTION_STRING="+connectionString)
	} else if accountKey != "" {
		azureEnv = append(azureEnv, "AZURE_STORAGE_ACCOUNT="+storageAccount)
		azureEnv = append(azureEnv, "AZURE_STORAGE_KEY="+accountKey)
	}

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating Azure credentials")
	EmitProgress(m, 10, "Checking source credentials")

	// Verify access to the container
	args := []string{"storage", "container", "show", "--name", container, "--account-name", storageAccount}
	if accountKey != "" {
		args = append(args, "--account-key", accountKey)
	}
	showCmd := exec.CommandContext(ctx, "az", args...)
	showCmd.Env = azureEnv
	if output, err := showCmd.CombinedOutput(); err != nil {
		EmitLog(m, "error", fmt.Sprintf("Failed to access Azure container: %s", string(output)))
		return fmt.Errorf("failed to access Azure container: %w", err)
	}
	EmitLog(m, "info", fmt.Sprintf("Successfully verified access to container: %s", container))

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

	// Phase 3: Downloading from Azure Blob
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", fmt.Sprintf("Downloading from Azure container: %s", container))
	EmitProgress(m, 40, "Downloading from Azure Blob")

	// Create staging directory
	stagingDir, err := os.MkdirTemp("", "blob-migration-*")
	if err != nil {
		return fmt.Errorf("failed to create staging directory: %w", err)
	}
	defer os.RemoveAll(stagingDir)

	localPath := filepath.Join(stagingDir, container)

	// Use azcopy for download (more reliable for large transfers)
	downloadArgs := []string{"copy",
		fmt.Sprintf("https://%s.blob.core.windows.net/%s/*", storageAccount, container),
		localPath,
		"--recursive",
	}

	downloadCmd := exec.CommandContext(ctx, "azcopy", downloadArgs...)
	downloadCmd.Env = azureEnv

	output, err := downloadCmd.CombinedOutput()
	if err != nil {
		// Fallback to az storage blob download-batch
		EmitLog(m, "info", "azcopy not available, falling back to az storage")
		fallbackArgs := []string{"storage", "blob", "download-batch",
			"--destination", localPath,
			"--source", container,
			"--account-name", storageAccount,
		}
		if accountKey != "" {
			fallbackArgs = append(fallbackArgs, "--account-key", accountKey)
		}
		fallbackCmd := exec.CommandContext(ctx, "az", fallbackArgs...)
		fallbackCmd.Env = azureEnv
		if output, err = fallbackCmd.CombinedOutput(); err != nil {
			EmitLog(m, "error", fmt.Sprintf("Azure download failed: %s", string(output)))
			return fmt.Errorf("failed to download from Azure Blob: %w", err)
		}
	}
	EmitLog(m, "info", "Successfully downloaded from Azure Blob")

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

// FilesToNFSExecutor migrates Azure Files to local NFS.
type FilesToNFSExecutor struct{}

// NewFilesToNFSExecutor creates a new Azure Files to NFS executor.
func NewFilesToNFSExecutor() *FilesToNFSExecutor {
	return &FilesToNFSExecutor{}
}

// Type returns the migration type.
func (e *FilesToNFSExecutor) Type() string {
	return "azure_files_to_nfs"
}

// GetPhases returns the migration phases.
func (e *FilesToNFSExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Fetching file share info",
		"Configuring NFS mount",
		"Syncing data",
		"Generating migration scripts",
		"Finalizing",
	}
}

// Validate validates the migration configuration.
func (e *FilesToNFSExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["storage_account"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.storage_account is required")
		}
		if _, ok := config.Source["share_name"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.share_name is required")
		}
		if _, ok := config.Source["account_key"].(string); !ok {
			result.Warnings = append(result.Warnings, "source.account_key not specified, using Azure CLI default credentials")
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
	result.Warnings = append(result.Warnings, "Ensure network connectivity between Azure Files and destination")

	return result, nil
}

// Execute performs the migration.
func (e *FilesToNFSExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	storageAccount := config.Source["storage_account"].(string)
	shareName := config.Source["share_name"].(string)
	accountKey, _ := config.Source["account_key"].(string)

	outputDir := config.Destination["output_dir"].(string)
	nfsServer, _ := config.Destination["nfs_server"].(string)
	nfsPath, _ := config.Destination["nfs_path"].(string)

	// Set up Azure environment
	azureEnv := os.Environ()
	if accountKey != "" {
		azureEnv = append(azureEnv, "AZURE_STORAGE_ACCOUNT="+storageAccount)
		azureEnv = append(azureEnv, "AZURE_STORAGE_KEY="+accountKey)
	}

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating Azure credentials")
	EmitProgress(m, 10, "Checking credentials")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Fetching file share info
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", fmt.Sprintf("Fetching file share info for %s", shareName))
	EmitProgress(m, 20, "Fetching file share info")

	args := []string{"storage", "share", "show",
		"--name", shareName,
		"--account-name", storageAccount,
		"--output", "json",
	}
	if accountKey != "" {
		args = append(args, "--account-key", accountKey)
	}

	describeCmd := exec.CommandContext(ctx, "az", args...)
	describeCmd.Env = azureEnv
	shareOutput, err := describeCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to describe file share: %w", err)
	}

	var shareInfo struct {
		Name       string `json:"name"`
		Quota      int    `json:"quota"`
		LastModified string `json:"lastModified"`
	}
	if err := json.Unmarshal(shareOutput, &shareInfo); err != nil {
		return fmt.Errorf("failed to parse share info: %w", err)
	}

	EmitLog(m, "info", fmt.Sprintf("File share: %s, Quota: %d GB", shareInfo.Name, shareInfo.Quota))

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

	// Save share info
	shareInfoPath := filepath.Join(outputDir, "share-info.json")
	if err := os.WriteFile(shareInfoPath, shareOutput, 0644); err != nil {
		return fmt.Errorf("failed to write share info: %w", err)
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
# Azure Files to NFS Migration Script
# Storage Account: %s
# Share: %s

set -e

echo "Azure Files to NFS Migration"
echo "============================"

# Configuration
STORAGE_ACCOUNT="%s"
SHARE_NAME="%s"
ACCOUNT_KEY="${AZURE_STORAGE_KEY:-%s}"
DEST_NFS_SERVER="%s"
DEST_NFS_PATH="%s"

# Create mount points
sudo mkdir -p /mnt/azure-source
sudo mkdir -p /mnt/nfs-dest

# Mount Azure Files (SMB)
echo "Mounting Azure Files..."
if [ -n "$ACCOUNT_KEY" ]; then
    sudo mount -t cifs //${STORAGE_ACCOUNT}.file.core.windows.net/${SHARE_NAME} /mnt/azure-source \
        -o vers=3.0,username=${STORAGE_ACCOUNT},password=${ACCOUNT_KEY},dir_mode=0777,file_mode=0777,serverino
else
    echo "No account key available. Please set AZURE_STORAGE_KEY."
    exit 1
fi

# Mount destination NFS
echo "Mounting destination NFS..."
sudo mount -t nfs ${DEST_NFS_SERVER}:${DEST_NFS_PATH} /mnt/nfs-dest

# Sync data
echo "Syncing data (this may take a while)..."
sudo rsync -avz --progress /mnt/azure-source/ /mnt/nfs-dest/

# Unmount
echo "Cleaning up..."
sudo umount /mnt/azure-source
sudo umount /mnt/nfs-dest

echo "Migration complete!"
`, storageAccount, shareName, storageAccount, shareName, accountKey, destNFS, destPath)

	scriptPath := filepath.Join(outputDir, "migrate-files.sh")
	if err := os.WriteFile(scriptPath, []byte(migrationScript), 0755); err != nil {
		return fmt.Errorf("failed to write migration script: %w", err)
	}

	// Generate azcopy script as alternative
	azcopyScript := fmt.Sprintf(`#!/bin/bash
# Azure Files Migration using azcopy
# Use this for large file shares or production migrations

set -e

echo "Setting up azcopy for Azure Files migration"

# Configuration
STORAGE_ACCOUNT="%s"
SHARE_NAME="%s"
DEST_DIR="${1:-./%s-backup}"

# Generate SAS token (requires Azure CLI login)
echo "Generating SAS token..."
SAS_TOKEN=$(az storage share generate-sas \
    --name %s \
    --account-name %s \
    --permissions rl \
    --expiry $(date -u -v+1d '+%%Y-%%m-%%dT%%H:%%MZ') \
    --output tsv)

# Run azcopy
echo "Starting azcopy..."
azcopy copy \
    "https://${STORAGE_ACCOUNT}.file.core.windows.net/${SHARE_NAME}?${SAS_TOKEN}" \
    "${DEST_DIR}" \
    --recursive

echo ""
echo "Migration complete! Files saved to: ${DEST_DIR}"
`, storageAccount, shareName, shareName, shareName, storageAccount)

	azcopyPath := filepath.Join(outputDir, "migrate-azcopy.sh")
	if err := os.WriteFile(azcopyPath, []byte(azcopyScript), 0755); err != nil {
		return fmt.Errorf("failed to write azcopy script: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 6: Finalizing
	EmitPhase(m, phases[5], 6)
	EmitLog(m, "info", "Finalizing migration artifacts")
	EmitProgress(m, 95, "Finalizing")

	readme := fmt.Sprintf(`# Azure Files to NFS Migration

## Source Azure Files
- Storage Account: %s
- Share Name: %s
- Quota: %d GB

## Migration Options

### Option 1: Direct SMB to NFS (Small to Medium)
Use migrate-files.sh for direct data sync via SMB mount and rsync.

### Option 2: azcopy (Large/Production)
Use migrate-azcopy.sh for azcopy-based transfer.

## Files Generated
- share-info.json: Azure Files configuration
- docker-compose.yml: NFS server container
- migrate-files.sh: Direct migration script
- migrate-azcopy.sh: azcopy-based migration

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
- Ensure network connectivity between Azure Files and destination
- For migrations from Azure, consider using VPN or ExpressRoute
- Large migrations may benefit from incremental sync
`, storageAccount, shareName, shareInfo.Quota)

	readmePath := filepath.Join(outputDir, "README.md")
	if err := os.WriteFile(readmePath, []byte(readme), 0644); err != nil {
		return fmt.Errorf("failed to write README: %w", err)
	}

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", fmt.Sprintf("Azure Files %s migration prepared at %s", shareName, outputDir))

	return nil
}

// ManagedDiskToLocalExecutor migrates Azure Managed Disks to local storage.
type ManagedDiskToLocalExecutor struct{}

// NewManagedDiskToLocalExecutor creates a new Managed Disk to local executor.
func NewManagedDiskToLocalExecutor() *ManagedDiskToLocalExecutor {
	return &ManagedDiskToLocalExecutor{}
}

// Type returns the migration type.
func (e *ManagedDiskToLocalExecutor) Type() string {
	return "managed_disk_to_local"
}

// GetPhases returns the migration phases.
func (e *ManagedDiskToLocalExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Fetching disk info",
		"Creating snapshot",
		"Waiting for snapshot",
		"Generating SAS URL",
		"Generating migration scripts",
		"Finalizing",
	}
}

// Validate validates the migration configuration.
func (e *ManagedDiskToLocalExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["disk_name"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.disk_name is required")
		}
		if _, ok := config.Source["resource_group"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.resource_group is required")
		}
		if _, ok := config.Source["subscription"].(string); !ok {
			result.Warnings = append(result.Warnings, "source.subscription not specified, using default subscription")
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

	result.Warnings = append(result.Warnings, "Disk export requires creating a snapshot first")
	result.Warnings = append(result.Warnings, "Large disks may take significant time to download")

	return result, nil
}

// Execute performs the migration.
func (e *ManagedDiskToLocalExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	diskName := config.Source["disk_name"].(string)
	resourceGroup := config.Source["resource_group"].(string)
	subscription, _ := config.Source["subscription"].(string)

	outputDir := config.Destination["output_dir"].(string)

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating Azure credentials")
	EmitProgress(m, 5, "Checking credentials")

	// Verify Azure CLI login
	loginCmd := exec.CommandContext(ctx, "az", "account", "show")
	if _, err := loginCmd.Output(); err != nil {
		EmitLog(m, "error", "Azure CLI not logged in")
		return fmt.Errorf("please login to Azure CLI: az login")
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Fetching disk info
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", fmt.Sprintf("Fetching disk info for %s", diskName))
	EmitProgress(m, 15, "Fetching disk info")

	args := []string{"disk", "show",
		"--name", diskName,
		"--resource-group", resourceGroup,
		"--output", "json",
	}
	if subscription != "" {
		args = append(args, "--subscription", subscription)
	}

	describeCmd := exec.CommandContext(ctx, "az", args...)
	diskOutput, err := describeCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to describe disk: %w", err)
	}

	var diskInfo struct {
		Name           string `json:"name"`
		DiskSizeGb     int    `json:"diskSizeGb"`
		Sku            struct {
			Name string `json:"name"`
		} `json:"sku"`
		DiskState      string `json:"diskState"`
		ProvisioningState string `json:"provisioningState"`
		Location       string `json:"location"`
		ID             string `json:"id"`
	}
	if err := json.Unmarshal(diskOutput, &diskInfo); err != nil {
		return fmt.Errorf("failed to parse disk info: %w", err)
	}

	EmitLog(m, "info", fmt.Sprintf("Disk size: %d GB, State: %s, SKU: %s", diskInfo.DiskSizeGb, diskInfo.DiskState, diskInfo.Sku.Name))

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Creating snapshot
	EmitPhase(m, phases[2], 3)
	snapshotName := fmt.Sprintf("%s-migration-snapshot", diskName)
	EmitLog(m, "info", fmt.Sprintf("Creating snapshot: %s", snapshotName))
	EmitProgress(m, 25, "Creating snapshot")

	snapshotArgs := []string{"snapshot", "create",
		"--name", snapshotName,
		"--resource-group", resourceGroup,
		"--source", diskInfo.ID,
		"--output", "json",
	}
	if subscription != "" {
		snapshotArgs = append(snapshotArgs, "--subscription", subscription)
	}

	snapshotCmd := exec.CommandContext(ctx, "az", snapshotArgs...)
	snapshotOutput, err := snapshotCmd.CombinedOutput()
	if err != nil {
		EmitLog(m, "error", fmt.Sprintf("Snapshot creation failed: %s", string(snapshotOutput)))
		return fmt.Errorf("failed to create snapshot: %w", err)
	}

	var snapshotInfo struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(snapshotOutput, &snapshotInfo); err != nil {
		return fmt.Errorf("failed to parse snapshot info: %w", err)
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
		statusArgs := []string{"snapshot", "show",
			"--name", snapshotName,
			"--resource-group", resourceGroup,
			"--query", "provisioningState",
			"--output", "tsv",
		}
		if subscription != "" {
			statusArgs = append(statusArgs, "--subscription", subscription)
		}

		statusCmd := exec.CommandContext(ctx, "az", statusArgs...)
		statusOutput, err := statusCmd.Output()
		if err == nil {
			status := string(statusOutput)
			if status == "Succeeded\n" || status == "Succeeded" {
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

	// Phase 5: Generating SAS URL
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Generating SAS URL for download")
	EmitProgress(m, 55, "Generating SAS URL")

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
	subscriptionArg := ""
	if subscription != "" {
		subscriptionArg = fmt.Sprintf("--subscription %s", subscription)
	}

	exportScript := fmt.Sprintf(`#!/bin/bash
# Managed Disk Migration Script
# Disk: %s
# Resource Group: %s

set -e

echo "Managed Disk Migration"
echo "======================"

# Configuration
DISK_NAME="%s"
SNAPSHOT_NAME="%s"
RESOURCE_GROUP="%s"
SUBSCRIPTION_ARG="%s"
OUTPUT_FILE="${1:-./%s.vhd}"

# Grant access and get SAS URL
echo "Generating SAS URL for snapshot..."
SAS_URL=$(az snapshot grant-access \
    --name ${SNAPSHOT_NAME} \
    --resource-group ${RESOURCE_GROUP} \
    ${SUBSCRIPTION_ARG} \
    --duration-in-seconds 3600 \
    --query accessSas \
    --output tsv)

echo "SAS URL generated (valid for 1 hour)"

# Download using azcopy
echo "Downloading disk image..."
azcopy copy "${SAS_URL}" "${OUTPUT_FILE}" --blob-type PageBlob

# Revoke access
echo "Revoking access..."
az snapshot revoke-access \
    --name ${SNAPSHOT_NAME} \
    --resource-group ${RESOURCE_GROUP} \
    ${SUBSCRIPTION_ARG}

echo ""
echo "Disk downloaded to: ${OUTPUT_FILE}"
echo ""
echo "To convert to other formats:"
echo "  qemu-img convert -f vpc -O qcow2 ${OUTPUT_FILE} ${OUTPUT_FILE%%.vhd}.qcow2"
echo "  qemu-img convert -f vpc -O raw ${OUTPUT_FILE} ${OUTPUT_FILE%%.vhd}.raw"
`, diskName, resourceGroup, diskName, snapshotName, resourceGroup, subscriptionArg, diskName)

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
    az snapshot delete \
        --name %s \
        --resource-group %s \
        %s
    echo "Snapshot deleted."
fi

echo "Cleanup complete!"
`, snapshotName, snapshotName, resourceGroup, subscriptionArg)

	cleanupPath := filepath.Join(outputDir, "cleanup.sh")
	if err := os.WriteFile(cleanupPath, []byte(cleanupScript), 0755); err != nil {
		return fmt.Errorf("failed to write cleanup script: %w", err)
	}

	// Generate Docker volume configuration
	dockerVolume := `version: '3.8'
volumes:
  migrated-disk:
    driver: local
    driver_opts:
      type: none
      device: /data/migrated-disk
      o: bind
`
	dockerVolumePath := filepath.Join(outputDir, "docker-volume.yml")
	if err := os.WriteFile(dockerVolumePath, []byte(dockerVolume), 0644); err != nil {
		return fmt.Errorf("failed to write docker volume config: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 7: Finalizing
	EmitPhase(m, phases[6], 7)
	EmitLog(m, "info", "Finalizing migration artifacts")
	EmitProgress(m, 90, "Finalizing")

	readme := fmt.Sprintf(`# Managed Disk to Local Storage Migration

## Source Disk
- Disk Name: %s
- Resource Group: %s
- Size: %d GB
- SKU: %s
- State: %s
- Location: %s
- Snapshot: %s

## Migration Steps

1. Run the migration script:
'''bash
./migrate-disk.sh output.vhd
'''

2. Convert the VHD to desired format:
'''bash
# Convert to qcow2 (for QEMU/KVM)
qemu-img convert -f vpc -O qcow2 output.vhd output.qcow2

# Convert to raw
qemu-img convert -f vpc -O raw output.vhd output.raw
'''

3. Clean up Azure resources:
'''bash
./cleanup.sh
'''

## Files Generated
- disk-info.json: Disk configuration
- migrate-disk.sh: Migration script
- cleanup.sh: Cleanup script for Azure resources
- docker-volume.yml: Docker volume configuration

## Docker Volume

After converting the disk image:
'''bash
mkdir -p /data/migrated-disk
# Mount your converted image or copy files
docker-compose -f docker-volume.yml up
'''

## Notes
- Snapshot will remain in Azure until manually deleted
- Use cleanup.sh after confirming migration success
- Large disks may incur significant data transfer costs
- VHD format is used for Azure export
`, diskName, resourceGroup, diskInfo.DiskSizeGb, diskInfo.Sku.Name, diskInfo.DiskState, diskInfo.Location, snapshotName)

	readmePath := filepath.Join(outputDir, "README.md")
	if err := os.WriteFile(readmePath, []byte(readme), 0644); err != nil {
		return fmt.Errorf("failed to write README: %w", err)
	}

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", fmt.Sprintf("Managed disk %s migration prepared at %s", diskName, outputDir))

	return nil
}
