package datamigration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// EFSToNFSExecutor migrates EFS file systems to local NFS.
type EFSToNFSExecutor struct{}

// NewEFSToNFSExecutor creates a new EFS to NFS executor.
func NewEFSToNFSExecutor() *EFSToNFSExecutor {
	return &EFSToNFSExecutor{}
}

// Type returns the migration type.
func (e *EFSToNFSExecutor) Type() string {
	return "efs_to_nfs"
}

// GetPhases returns the migration phases.
func (e *EFSToNFSExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Fetching file system info",
		"Setting up DataSync",
		"Configuring NFS server",
		"Generating migration scripts",
		"Finalizing",
	}
}

// Validate validates the migration configuration.
func (e *EFSToNFSExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["file_system_id"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.file_system_id is required")
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

	if config.Destination == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "destination configuration is required")
	} else {
		if _, ok := config.Destination["output_dir"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "destination.output_dir is required")
		}
	}

	result.Warnings = append(result.Warnings, "Large file systems may take significant time to sync")
	result.Warnings = append(result.Warnings, "Consider using AWS DataSync for large migrations")

	return result, nil
}

// Execute performs the migration.
func (e *EFSToNFSExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	fileSystemID := config.Source["file_system_id"].(string)
	accessKeyID := config.Source["access_key_id"].(string)
	secretAccessKey := config.Source["secret_access_key"].(string)
	region, _ := config.Source["region"].(string)
	if region == "" {
		region = "us-east-1"
	}
	outputDir := config.Destination["output_dir"].(string)
	nfsServer, _ := config.Destination["nfs_server"].(string)
	nfsPath, _ := config.Destination["nfs_path"].(string)

	awsEnv := []string{
		"AWS_ACCESS_KEY_ID=" + accessKeyID,
		"AWS_SECRET_ACCESS_KEY=" + secretAccessKey,
		"AWS_DEFAULT_REGION=" + region,
	}

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating AWS credentials")
	EmitProgress(m, 10, "Checking credentials")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Fetching file system info
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", fmt.Sprintf("Fetching EFS info for %s", fileSystemID))
	EmitProgress(m, 20, "Fetching EFS info")

	describeCmd := exec.CommandContext(ctx, "aws", "efs", "describe-file-systems",
		"--file-system-id", fileSystemID,
		"--region", region,
		"--output", "json",
	)
	describeCmd.Env = append(os.Environ(), awsEnv...)
	efsOutput, err := describeCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to describe file system: %w", err)
	}

	var efsResult struct {
		FileSystems []struct {
			FileSystemID   string `json:"FileSystemId"`
			SizeInBytes    struct {
				Value int64 `json:"Value"`
			} `json:"SizeInBytes"`
			PerformanceMode string `json:"PerformanceMode"`
			ThroughputMode  string `json:"ThroughputMode"`
		} `json:"FileSystems"`
	}
	if err := json.Unmarshal(efsOutput, &efsResult); err != nil {
		return fmt.Errorf("failed to parse EFS info: %w", err)
	}

	// Get mount targets
	mountTargetsCmd := exec.CommandContext(ctx, "aws", "efs", "describe-mount-targets",
		"--file-system-id", fileSystemID,
		"--region", region,
		"--output", "json",
	)
	mountTargetsCmd.Env = append(os.Environ(), awsEnv...)
	mountTargetsOutput, err := mountTargetsCmd.Output()
	if err != nil {
		EmitLog(m, "warn", "Failed to get mount targets")
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Setting up DataSync configuration
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", "Preparing DataSync configuration")
	EmitProgress(m, 40, "Configuring DataSync")

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Save EFS info
	efsInfoPath := filepath.Join(outputDir, "efs-info.json")
	if err := os.WriteFile(efsInfoPath, efsOutput, 0644); err != nil {
		return fmt.Errorf("failed to write EFS info: %w", err)
	}

	if len(mountTargetsOutput) > 0 {
		mountTargetsPath := filepath.Join(outputDir, "mount-targets.json")
		if err := os.WriteFile(mountTargetsPath, mountTargetsOutput, 0644); err != nil {
			EmitLog(m, "warn", "Failed to write mount targets info")
		}
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Configuring NFS server
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Generating NFS server configuration")
	EmitProgress(m, 60, "Configuring NFS")

	// Generate NFS server Docker compose
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

	// Get a mount target IP for the script
	var mountTargetIP string
	var mountTargets struct {
		MountTargets []struct {
			IpAddress string `json:"IpAddress"`
		} `json:"MountTargets"`
	}
	if len(mountTargetsOutput) > 0 {
		_ = json.Unmarshal(mountTargetsOutput, &mountTargets)
		if len(mountTargets.MountTargets) > 0 {
			mountTargetIP = mountTargets.MountTargets[0].IpAddress
		}
	}

	destNFS := nfsServer
	if destNFS == "" {
		destNFS = "localhost"
	}
	destPath := nfsPath
	if destPath == "" {
		destPath = "/data"
	}

	migrationScript := fmt.Sprintf(`#!/bin/bash
# EFS to NFS Migration Script
# File System ID: %s
# Region: %s

set -e

echo "EFS to NFS Migration"
echo "===================="

# Configuration
EFS_ID="%s"
EFS_MOUNT_TARGET="%s"
DEST_NFS_SERVER="%s"
DEST_NFS_PATH="%s"

# Create mount points
sudo mkdir -p /mnt/efs-source
sudo mkdir -p /mnt/nfs-dest

# Mount EFS (requires NFS client and network access)
echo "Mounting EFS..."
if [ -n "$EFS_MOUNT_TARGET" ]; then
    sudo mount -t nfs4 -o nfsvers=4.1,rsize=1048576,wsize=1048576,hard,timeo=600,retrans=2 \
        ${EFS_MOUNT_TARGET}:/ /mnt/efs-source
else
    echo "No mount target IP available. Please configure manually."
    echo "Mount command: sudo mount -t nfs4 <mount-target-ip>:/ /mnt/efs-source"
    exit 1
fi

# Mount destination NFS
echo "Mounting destination NFS..."
sudo mount -t nfs ${DEST_NFS_SERVER}:${DEST_NFS_PATH} /mnt/nfs-dest

# Sync data
echo "Syncing data (this may take a while)..."
sudo rsync -avz --progress /mnt/efs-source/ /mnt/nfs-dest/

# Unmount
echo "Cleaning up..."
sudo umount /mnt/efs-source
sudo umount /mnt/nfs-dest

echo "Migration complete!"
`, fileSystemID, region, fileSystemID, mountTargetIP, destNFS, destPath)

	scriptPath := filepath.Join(outputDir, "migrate-efs.sh")
	if err := os.WriteFile(scriptPath, []byte(migrationScript), 0755); err != nil {
		return fmt.Errorf("failed to write migration script: %w", err)
	}

	// DataSync alternative script
	dataSyncScript := fmt.Sprintf(`#!/bin/bash
# AWS DataSync Configuration for EFS Migration
# Use this for large file systems or production migrations

set -e

echo "Setting up AWS DataSync for EFS migration"

# Create source location (EFS)
aws datasync create-location-efs \
    --efs-filesystem-arn "arn:aws:elasticfilesystem:%s::file-system/%s" \
    --ec2-config SubnetArn=<subnet-arn>,SecurityGroupArns=<sg-arn> \
    --subdirectory "/" \
    --region %s

# Create destination location (NFS server)
# Note: Requires DataSync agent on-premises
aws datasync create-location-nfs \
    --server-hostname "%s" \
    --subdirectory "%s" \
    --on-prem-config AgentArns=<agent-arn> \
    --region %s

echo "Configure task in AWS Console or using:"
echo "aws datasync create-task --source-location-arn <source> --destination-location-arn <dest>"
`, region, fileSystemID, region, destNFS, destPath, region)

	dataSyncPath := filepath.Join(outputDir, "setup-datasync.sh")
	if err := os.WriteFile(dataSyncPath, []byte(dataSyncScript), 0755); err != nil {
		return fmt.Errorf("failed to write DataSync script: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 6: Finalizing
	EmitPhase(m, phases[5], 6)
	EmitLog(m, "info", "Finalizing migration artifacts")
	EmitProgress(m, 95, "Finalizing")

	// Generate README
	sizeInfo := "unknown"
	if len(efsResult.FileSystems) > 0 {
		sizeGB := efsResult.FileSystems[0].SizeInBytes.Value / (1024 * 1024 * 1024)
		sizeInfo = fmt.Sprintf("%d GB", sizeGB)
	}

	readme := fmt.Sprintf(`# EFS to NFS Migration

## Source EFS
- File System ID: %s
- Region: %s
- Size: %s

## Migration Options

### Option 1: Direct rsync (Small to Medium)
Use migrate-efs.sh for direct data sync via rsync.

### Option 2: AWS DataSync (Large/Production)
Use setup-datasync.sh for AWS DataSync configuration.

## Files Generated
- efs-info.json: EFS configuration
- mount-targets.json: Mount target information
- docker-compose.yml: NFS server container
- migrate-efs.sh: Direct migration script
- setup-datasync.sh: AWS DataSync setup

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
- Ensure network connectivity between EFS and destination
- For large migrations, use AWS DataSync
- Consider incremental sync for ongoing data
`, fileSystemID, region, sizeInfo)

	readmePath := filepath.Join(outputDir, "README.md")
	if err := os.WriteFile(readmePath, []byte(readme), 0644); err != nil {
		return fmt.Errorf("failed to write README: %w", err)
	}

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", fmt.Sprintf("EFS %s migration prepared at %s", fileSystemID, outputDir))

	return nil
}
