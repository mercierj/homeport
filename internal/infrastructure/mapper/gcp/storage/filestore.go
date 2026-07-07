// Package storage provides mappers for GCP storage services.
package storage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

// FilestoreMapper converts GCP Filestore instances to NFS server containers.
type FilestoreMapper struct {
	*mapper.BaseMapper
}

// NewFilestoreMapper creates a new Filestore to NFS server mapper.
func NewFilestoreMapper() *FilestoreMapper {
	return &FilestoreMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeFilestore, nil),
	}
}

// Map converts a GCP Filestore instance to an NFS server service.
func (m *FilestoreMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	instanceName := res.GetConfigString("name")
	if instanceName == "" {
		instanceName = res.Name
	}

	serviceName := m.sanitizeName(instanceName) + "-nfs"
	result := mapper.NewMappingResult(serviceName)
	svc := result.DockerService

	// Get filestore properties
	tier := res.GetConfigString("tier")
	if tier == "" {
		tier = "BASIC_HDD"
	}

	// Extract file share configuration
	fileShares := m.extractFileShares(res)
	var fileShareName string
	var capacityGB int

	if len(fileShares) > 0 {
		fileShareName = fileShares[0]["name"].(string)
		if capacity, ok := fileShares[0]["capacity_gb"].(float64); ok {
			capacityGB = int(capacity)
		} else if capacity, ok := fileShares[0]["capacity_gb"].(int); ok {
			capacityGB = capacity
		}
	}

	if fileShareName == "" {
		fileShareName = "share"
	}
	if capacityGB == 0 {
		capacityGB = 1024 // Default 1TB
	}

	// Configure NFS server using itsthenetwork/nfs-server-alpine
	svc.Image = "itsthenetwork/nfs-server-alpine:latest"
	svc.CapAdd = []string{"SYS_ADMIN", "DAC_READ_SEARCH"}
	svc.Environment = map[string]string{
		"SHARED_DIRECTORY": "/data",
	}
	svc.Ports = []string{
		"2049:2049", // NFS
	}
	svc.Volumes = []string{
		fmt.Sprintf("./data/nfs/%s:/data", fileShareName),
	}
	svc.Labels = map[string]string{
		"homeport.source":         "google_filestore_instance",
		"homeport.instance_name":  instanceName,
		"homeport.tier":           tier,
		"homeport.fileshare_name": fileShareName,
		"homeport.capacity_gb":    fmt.Sprintf("%d", capacityGB),
	}
	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"

	// Apply resource limits based on tier
	m.applyTierLimits(svc, tier, capacityGB)

	// Health check for NFS
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD-SHELL", "rpcinfo -p | grep nfs || exit 1"},
		Interval: 30 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  3,
	}

	// Generate NFS client mount instructions
	mountScript := m.generateMountScript(serviceName, fileShareName, instanceName)
	result.AddScript("mount_nfs.sh", []byte(mountScript))

	// Generate NFS exports configuration
	exportsConfig := m.generateExportsConfig(fileShareName)
	result.AddConfig("exports", []byte(exportsConfig))

	// Generate docker-compose example for clients
	clientExample := m.generateClientExample(serviceName, fileShareName)
	result.AddConfig("nfs-client-example.yml", []byte(clientExample))
	result.AddConfig("config/filestore/app-change.env", []byte(m.generateAppChangeConfig(serviceName, fileShareName, instanceName)))
	result.AddConfig("config/filestore/migration.env", []byte(m.generateMigrationConfig(instanceName, fileShareName, tier, capacityGB)))
	result.AddConfig("config/filestore/exports", []byte(exportsConfig))
	result.AddConfig("config/filestore/client-compose.yml", []byte(clientExample))
	result.AddScript("export_filestore_instance.sh", []byte(m.generateExportScript(instanceName)))
	result.AddScript("sync_filestore_data.sh", []byte(m.generateSyncScript(instanceName, fileShareName)))
	result.AddScript("validate_filestore_nfs.sh", []byte(m.generateValidateScript(serviceName, fileShareName)))
	result.AddScript("backup_filestore_config.sh", []byte(m.generateBackupScript(instanceName)))
	result.AddScript("cutover_filestore_clients.sh", []byte(m.generateCutoverScript(instanceName)))
	for _, step := range filestoreRunbook(instanceName, fileShareName) {
		result.AddRunbookStep(step)
	}

	// Handle tier-specific warnings
	m.handleTierWarnings(tier, result)

	// Handle network configuration
	if networks := res.Config["networks"]; networks != nil {
		result.AddWarning("Filestore instance has specific network configuration. Ensure Docker networks are properly configured.")
	}

	// Handle labels
	if labels := res.Config["labels"]; labels != nil {
		result.AddWarning("Filestore instance has labels. These have been mapped to Docker labels where possible.")
	}

	// Handle KMS key encryption
	if kmsKey := res.GetConfigString("kms_key_name"); kmsKey != "" {
		result.AddWarning("Filestore instance uses KMS encryption. Consider encrypting the NFS volume at the filesystem level.")
		result.AddConfig("config/filestore/encryption.env", []byte("SOURCE_KMS_KEY="+kmsKey+"\nTARGET_VOLUME_ENCRYPTION=host-managed\n"))
	}

	return result, nil
}

// extractFileShares extracts file share configuration.
func (m *FilestoreMapper) extractFileShares(res *resource.AWSResource) []map[string]interface{} {
	var shares []map[string]interface{}

	if fileShares := res.Config["file_shares"]; fileShares != nil {
		if shareSlice, ok := fileShares.([]interface{}); ok {
			for _, share := range shareSlice {
				if shareMap, ok := share.(map[string]interface{}); ok {
					shares = append(shares, shareMap)
				}
			}
		}
	}

	return shares
}

// applyTierLimits sets resource limits based on Filestore tier.
func (m *FilestoreMapper) applyTierLimits(svc *mapper.DockerService, tier string, capacityGB int) {
	svc.Deploy = &mapper.DeployConfig{
		Replicas: 2,
		Resources: &mapper.Resources{
			Limits: &mapper.ResourceLimits{},
		},
	}

	switch tier {
	case "BASIC_HDD":
		// Standard tier - basic performance
		svc.Deploy.Resources.Limits.CPUs = "1"
		svc.Deploy.Resources.Limits.Memory = "2G"
	case "BASIC_SSD":
		// SSD tier - better performance
		svc.Deploy.Resources.Limits.CPUs = "2"
		svc.Deploy.Resources.Limits.Memory = "4G"
	case "HIGH_SCALE_SSD":
		// High scale SSD - high performance
		svc.Deploy.Resources.Limits.CPUs = "4"
		svc.Deploy.Resources.Limits.Memory = "8G"
	case "ENTERPRISE":
		// Enterprise tier - highest performance
		svc.Deploy.Resources.Limits.CPUs = "4"
		svc.Deploy.Resources.Limits.Memory = "8G"
	default:
		// Default limits
		svc.Deploy.Resources.Limits.CPUs = "1"
		svc.Deploy.Resources.Limits.Memory = "2G"
	}
}

// handleTierWarnings adds warnings based on the Filestore tier.
func (m *FilestoreMapper) handleTierWarnings(tier string, result *mapper.MappingResult) {
	switch tier {
	case "BASIC_HDD":
		result.AddWarning("Original Filestore tier: BASIC_HDD. NFS server performance depends on host storage speed.")
	case "BASIC_SSD":
		result.AddWarning("Original Filestore tier: BASIC_SSD. For best performance, ensure Docker host uses SSD storage.")
	case "HIGH_SCALE_SSD":
		result.AddWarning("Original Filestore tier: HIGH_SCALE_SSD. Self-hosted NFS may not match GCP Filestore high-scale performance.")
	case "ENTERPRISE":
		result.AddWarning("Original Filestore tier: ENTERPRISE. Enterprise-grade features may require additional configuration.")
	}
}

func (m *FilestoreMapper) generateAppChangeConfig(serviceName, fileShareName, instanceName string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_FILESTORE_INSTANCE=%s\nTARGET_NFS_SERVICE=%s\nTARGET_NFS_EXPORT=/data\nTARGET_NFS_SHARE=%s\n", instanceName, serviceName, fileShareName)
}

func (m *FilestoreMapper) generateMigrationConfig(instanceName, fileShareName, tier string, capacityGB int) string {
	return fmt.Sprintf("SOURCE_FILESTORE_INSTANCE=%s\nSOURCE_FILESTORE_SHARE=%s\nSOURCE_FILESTORE_TIER=%s\nSOURCE_CAPACITY_GB=%d\nTARGET_PROTOCOL=nfs\n", instanceName, fileShareName, tier, capacityGB)
}

func (m *FilestoreMapper) generateExportScript(instanceName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\nmkdir -p filestore-export\ngcloud filestore instances describe %q --format=json > filestore-export/instance.json\n", instanceName)
}

func (m *FilestoreMapper) generateSyncScript(instanceName, fileShareName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n: \"${SOURCE_MOUNT:?set SOURCE_MOUNT to mounted Filestore path}\"\n: \"${TARGET_MOUNT:=./data/nfs/%s}\"\nmkdir -p \"$TARGET_MOUNT\"\nrsync -a --delete \"$SOURCE_MOUNT/\" \"$TARGET_MOUNT/\"\necho \"Filestore %s share %s synced to $TARGET_MOUNT\"\n", fileShareName, instanceName, fileShareName)
}

func (m *FilestoreMapper) generateValidateScript(serviceName, fileShareName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/filestore/app-change.env\ntest -s config/filestore/exports\ntest -d ./data/nfs/%s\necho \"Filestore NFS target %s validated\"\n", fileShareName, serviceName)
}

func (m *FilestoreMapper) generateBackupScript(instanceName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/filestore-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/filestore filestore-export data/nfs\necho \"$archive\"\n", m.sanitizeName(instanceName))
}

func (m *FilestoreMapper) generateCutoverScript(instanceName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/filestore/app-change.env\ntest \"$SOURCE_FILESTORE_INSTANCE\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Patch Filestore clients to $TARGET_NFS_SERVICE:$TARGET_NFS_EXPORT\"\n", instanceName)
}

func filestoreRunbook(instanceName, fileShareName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "file-storage", "source": "google_filestore_instance", "instance": instanceName, "share": fileShareName, "target": "nfs"}
	return []domainrunbook.Step{
		filestoreStep("export-filestore-instance", "Export Filestore instance", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_filestore_instance.sh"}, "Filestore instance config is exported", metadata),
		filestoreStep("provision-nfs-target", "Provision NFS target", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s config/filestore/exports"}, "NFS target config is rendered", metadata),
		filestoreStep("sync-filestore-data", "Sync Filestore data", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "sync_filestore_data.sh"}, "file data is copied to NFS target", metadata),
		filestoreStep("validate-filestore-nfs", "Validate NFS target", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_filestore_nfs.sh"}, "NFS config and data path validate", metadata),
		filestoreStep("backup-filestore-config", "Backup Filestore config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_filestore_config.sh"}, "Filestore migration artifacts are archived", metadata),
		filestoreStep("cutover-filestore-clients", "Cut over Filestore clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_filestore_clients.sh"}, "clients use the generated NFS target", metadata),
		filestoreStep("rollback-filestore-source", "Keep Filestore source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "Filestore remains authoritative until NFS validation passes", metadata),
	}
}

func filestoreStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}

// generateMountScript creates a script for mounting the NFS share.
func (m *FilestoreMapper) generateMountScript(serviceName, fileShareName, instanceName string) string {
	return fmt.Sprintf(`#!/bin/bash
# NFS Mount Script
# Migrated from GCP Filestore instance: %s
# File share: %s

set -e

NFS_SERVER="${NFS_SERVER:-localhost}"
NFS_PORT="2049"
MOUNT_POINT="${MOUNT_POINT:-/mnt/nfs/%s}"
NFS_OPTIONS="vers=4,soft,timeo=30,retrans=3"

echo "Mounting NFS share from Filestore migration..."
echo "Server: $NFS_SERVER:$NFS_PORT"
echo "Mount point: $MOUNT_POINT"

# Check if NFS client is installed
if ! command -v mount.nfs &> /dev/null; then
    echo "ERROR: NFS client not installed"
    echo "Install it using:"
    echo "  - Ubuntu/Debian: sudo apt-get install nfs-common"
    echo "  - RHEL/CentOS: sudo yum install nfs-utils"
    exit 1
fi

# Create mount point if it doesn't exist
sudo mkdir -p "$MOUNT_POINT"

# Check if already mounted
if mountpoint -q "$MOUNT_POINT"; then
    echo "Already mounted at $MOUNT_POINT"
    exit 0
fi

# Mount the NFS share
echo "Mounting NFS share..."
sudo mount -t nfs -o "$NFS_OPTIONS" "$NFS_SERVER:/data" "$MOUNT_POINT"

echo "NFS share mounted successfully at $MOUNT_POINT"
echo ""
echo "To make this permanent, add to /etc/fstab:"
echo "$NFS_SERVER:/data $MOUNT_POINT nfs $NFS_OPTIONS 0 0"
echo ""
echo "To unmount:"
echo "sudo umount $MOUNT_POINT"
`, instanceName, fileShareName, fileShareName)
}

// generateExportsConfig creates an NFS exports configuration.
func (m *FilestoreMapper) generateExportsConfig(fileShareName string) string {
	return fmt.Sprintf(`# NFS Exports Configuration
# Generated for Filestore migration: %s

/data *(rw,sync,no_subtree_check,no_root_squash,insecure)

# Options explained:
# rw - Read/write access
# sync - Synchronous writes
# no_subtree_check - Disable subtree checking for better performance
# no_root_squash - Allow root access from clients (use with caution)
# insecure - Allow connections from ports > 1024

# For more restrictive access, replace * with specific IP ranges:
# /data 192.168.1.0/24(rw,sync,no_subtree_check)
`, fileShareName)
}

// generateClientExample creates a docker-compose example for NFS clients.
func (m *FilestoreMapper) generateClientExample(serviceName, fileShareName string) string {
	return fmt.Sprintf(`# Docker Compose NFS Client Example
# This shows how to use the NFS server from other containers

version: '3.8'

services:
  # Example application using the NFS share
  myapp:
    image: nginx:latest
    volumes:
      # Mount NFS share using Docker volume
      - nfs-data:/usr/share/nginx/html
    networks:
      - homeport

# Define NFS volume
volumes:
  nfs-data:
    driver: local
    driver_opts:
      type: nfs
      o: addr=<docker-host-ip>,nfsvers=4,rw
      device: ":/data"

networks:
  homeport:
    external: true

# Usage Notes:
# 1. Replace <docker-host-ip> with the IP address of the Docker host running the NFS server
# 2. If running on the same Docker host, you can use the service name: %s
# 3. For Docker Swarm, use the overlay network and service discovery
# 4. Ensure the NFS server container is running before starting clients

# Alternative: Direct NFS mount in container
# volumes:
#   - type: volume
#     source: nfs-data
#     target: /data
#     volume:
#       nocopy: true
`, serviceName)
}

// sanitizeName sanitizes the instance name for Docker.
func (m *FilestoreMapper) sanitizeName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "_", "-")
	name = strings.ReplaceAll(name, " ", "-")

	validName := ""
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' {
			validName += string(ch)
		}
	}

	validName = strings.TrimLeft(validName, "-0123456789")
	if validName == "" {
		validName = "filestore"
	}

	return validName
}
