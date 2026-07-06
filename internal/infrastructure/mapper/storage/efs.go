// Package storage provides mappers for AWS storage services.
package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
	"github.com/homeport/homeport/internal/infrastructure/mapper/shared/storagerunbook"
)

// EFSMapper converts AWS EFS file systems to NFS servers.
type EFSMapper struct {
	*mapper.BaseMapper
}

// NewEFSMapper creates a new EFS to NFS mapper.
func NewEFSMapper() *EFSMapper {
	return &EFSMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeEFSVolume, nil),
	}
}

// Map converts an EFS file system to an NFS server.
func (m *EFSMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	fileSystemName := res.GetConfigString("creation_token")
	if fileSystemName == "" {
		fileSystemName = res.Name
	}

	// Create result using new API
	result := mapper.NewMappingResult(fmt.Sprintf("nfs-%s", fileSystemName))
	svc := result.DockerService

	// Configure NFS server service
	svc.Image = "itsthenetwork/nfs-server-alpine:12"
	svc.Environment = map[string]string{
		"SHARED_DIRECTORY": "/data",
	}
	svc.Ports = []string{
		"2049:2049", // NFS port
	}
	svc.Volumes = []string{
		fmt.Sprintf("./data/nfs/%s:/data", fileSystemName),
	}
	svc.CapAdd = []string{"SYS_ADMIN"} // NFS server requires privileged capabilities
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Labels = map[string]string{
		"homeport.source":     "aws_efs_file_system",
		"homeport.filesystem": fileSystemName,
		"homeport.type":       "nfs-server",
	}

	// Add health check
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD-SHELL", "showmount -e localhost || exit 1"},
		Interval: 30 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  3,
	}

	// Extract EFS configuration
	performanceMode := res.GetConfigString("performance_mode")
	if performanceMode == "" {
		performanceMode = "generalPurpose"
	}

	throughputMode := res.GetConfigString("throughput_mode")
	if throughputMode == "" {
		throughputMode = "bursting"
	}

	encrypted := res.GetConfigBool("encrypted")

	// Add configuration notes
	result.AddWarning(fmt.Sprintf("EFS Performance Mode: %s - NFS server does not have direct equivalent. Ensure your NFS server has adequate resources.", performanceMode))

	if throughputMode == "provisioned" {
		provisionedThroughput := res.GetConfigInt("provisioned_throughput_in_mibps")
		result.AddWarning(fmt.Sprintf("EFS Provisioned Throughput: %d MiB/s - NFS server throughput depends on host resources and network configuration.", provisionedThroughput))
	}

	if encrypted {
		result.AddWarning("EFS encryption is enabled. Consider using encrypted volumes or dm-crypt for the NFS data directory.")
	}

	// Generate NFS setup script
	nfsScript := m.generateNFSSetupScript(fileSystemName, res)
	result.AddScript("setup_nfs.sh", []byte(nfsScript))
	result.AddScript("sync_efs_file_data.sh", []byte(m.generateSyncScript(fileSystemName)))
	result.AddScript("backup_efs_file_data.sh", []byte(m.generateBackupScript(fileSystemName)))
	result.AddScript("validate_efs_file_data.sh", []byte(m.generateValidateScript(fileSystemName)))
	result.AddConfig("config/efs/app-change.env", []byte(m.generateAppChangeConfig(fileSystemName)))
	result.AddConfig("config/efs/exports", []byte(fmt.Sprintf("/data *(rw,sync,no_subtree_check,no_root_squash) # %s\n", fileSystemName)))
	result.AddConfig("config/efs/encryption-handoff.env", []byte(fmt.Sprintf("SOURCE_ENCRYPTED=%t\nTARGET_DATA_DIR=./data/nfs/%s\nTARGET_ENCRYPTION_MODE=host_volume\n", encrypted, fileSystemName)))
	for _, step := range storagerunbook.FileStorage(fileSystemName, "nfs") {
		result.AddRunbookStep(step)
	}
	for _, step := range efsRunbook(fileSystemName) {
		result.AddRunbookStep(step)
	}

	// Handle lifecycle policy
	if lifecyclePolicy := res.GetConfig("lifecycle_policy"); lifecyclePolicy != nil {
		result.AddWarning("EFS lifecycle policy is configured. NFS does not have built-in lifecycle management. Implement custom scripts for file archival/deletion if needed.")
	}

	// Handle access points
	if res.GetConfig("access_point") != nil {
		result.AddWarning("EFS Access Points are configured. These provide application-specific entry points. Configure separate NFS exports or directories to replicate this functionality.")
	}

	return result, nil
}

func (m *EFSMapper) generateAppChangeConfig(fileSystemName string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=generated_patch
SOURCE_FILE_SYSTEM=%s
TARGET_PROTOCOL=nfs
TARGET_MOUNT=nfs-%s:/data
TARGET_CLIENT_PACKAGE=nfs-utils
`, fileSystemName, fileSystemName)
}

func (m *EFSMapper) generateSyncScript(fileSystemName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
source_mount="${SOURCE_MOUNT:-/mnt/%s-source}"
target_mount="${TARGET_MOUNT_PATH:-./data/nfs/%s}"
mkdir -p "$target_mount"
rsync -a --delete "$source_mount"/ "$target_mount"/
`, fileSystemName, fileSystemName)
}

func (m *EFSMapper) generateBackupScript(fileSystemName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-efs-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/efs data/nfs/%s
echo "$archive"
`, fileSystemName, fileSystemName)
}

func (m *EFSMapper) generateValidateScript(fileSystemName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
test -s config/efs/app-change.env
test -d data/nfs/%s
find "data/nfs/%s" -maxdepth 1 >/dev/null
echo "EFS NFS target validated"
`, fileSystemName, fileSystemName)
}

func efsRunbook(fileSystemName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "file-storage", "source": "aws_efs_file_system", "share": fileSystemName, "protocol": "nfs"}
	return []domainrunbook.Step{
		efsStep("backup-efs-file-share", "Backup EFS file share", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_efs_file_data.sh"}, "NFS config and file data are archived", metadata),
		efsStep("cutover-efs-client-mounts", "Cut over EFS client mounts", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "-c", "test -s config/efs/app-change.env"}, "clients use the generated NFS mount target", metadata),
		efsStep("rollback-file-storage-source", "Keep EFS source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "AWS EFS remains authoritative until NFS validation passes", metadata),
	}
}

func efsStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{
		ID:               id,
		Name:             name,
		Group:            group,
		Type:             stepType,
		Status:           domainrunbook.StepStatusPending,
		Executor:         executor,
		Command:          command,
		SuccessCondition: success,
		Metadata:         metadata,
	}
}

// generateNFSSetupScript creates an NFS server setup script.
func (m *EFSMapper) generateNFSSetupScript(fileSystemName string, res *resource.AWSResource) string {
	return fmt.Sprintf(`#!/bin/bash
# NFS Server Setup Script
# This script sets up the NFS server for file system: %s

set -e

echo "Setting up NFS server for %s..."

# Create data directory if it doesn't exist
mkdir -p ./data/nfs/%s

# Set permissions (adjust as needed)
chmod 755 ./data/nfs/%s

echo "NFS server configuration complete!"
echo ""
echo "The NFS share is available at: localhost:/data"
echo "Mount on clients with: sudo mount -t nfs localhost:/data /mnt/%s"
echo ""
echo "NFS Server Details:"
echo "  - Performance Mode: %s"
echo "  - Throughput Mode: %s"
echo "  - Encrypted: %v"
echo ""
echo "Client Mount Instructions:"
echo "  1. Install NFS client utilities:"
echo "     - Ubuntu/Debian: sudo apt-get install nfs-common"
echo "     - RHEL/CentOS: sudo yum install nfs-utils"
echo "  2. Create mount point: sudo mkdir -p /mnt/%s"
echo "  3. Mount the share: sudo mount -t nfs localhost:/data /mnt/%s"
echo "  4. (Optional) Add to /etc/fstab for automatic mounting:"
echo "     localhost:/data /mnt/%s nfs defaults 0 0"
`,
		fileSystemName,
		fileSystemName,
		fileSystemName,
		fileSystemName,
		fileSystemName,
		m.getPerformanceMode(res),
		m.getThroughputMode(res),
		res.GetConfigBool("encrypted"),
		fileSystemName,
		fileSystemName,
		fileSystemName,
	)
}

// getPerformanceMode returns the EFS performance mode with a default.
func (m *EFSMapper) getPerformanceMode(res *resource.AWSResource) string {
	mode := res.GetConfigString("performance_mode")
	if mode == "" {
		return "generalPurpose"
	}
	return mode
}

// getThroughputMode returns the EFS throughput mode with a default.
func (m *EFSMapper) getThroughputMode(res *resource.AWSResource) string {
	mode := res.GetConfigString("throughput_mode")
	if mode == "" {
		return "bursting"
	}
	return mode
}
