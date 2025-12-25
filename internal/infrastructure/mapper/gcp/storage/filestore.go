// Package storage provides mappers for GCP storage services.
package storage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cloudexit/cloudexit/internal/domain/mapper"
	"github.com/cloudexit/cloudexit/internal/domain/resource"
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
		"cloudexit.source":         "google_filestore_instance",
		"cloudexit.instance_name":  instanceName,
		"cloudexit.tier":           tier,
		"cloudexit.fileshare_name": fileShareName,
		"cloudexit.capacity_gb":    fmt.Sprintf("%d", capacityGB),
	}
	svc.Networks = []string{"cloudexit"}
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
		result.AddManualStep("Set up encryption for /data/nfs/" + fileShareName + " using LUKS or similar")
	}

	// Add usage instructions
	result.AddManualStep(fmt.Sprintf("NFS server will be accessible at: nfs://<docker-host-ip>:2049/data"))
	result.AddManualStep("Mount on clients using: mount -t nfs -o vers=4 <docker-host-ip>:/data /mnt/nfs")
	result.AddManualStep(fmt.Sprintf("Or use the NFS volume driver in docker-compose (see nfs-client-example.yml)"))
	result.AddManualStep("Ensure NFS client packages are installed on client systems: nfs-common (Debian/Ubuntu) or nfs-utils (RHEL/CentOS)")

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
		result.AddManualStep("Consider using distributed filesystem solutions like GlusterFS or Ceph for high-scale requirements")
	case "ENTERPRISE":
		result.AddWarning("Original Filestore tier: ENTERPRISE. Enterprise-grade features may require additional configuration.")
		result.AddManualStep("Review Filestore enterprise features and configure NFS server accordingly")
	}
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
      - cloudexit

# Define NFS volume
volumes:
  nfs-data:
    driver: local
    driver_opts:
      type: nfs
      o: addr=<docker-host-ip>,nfsvers=4,rw
      device: ":/data"

networks:
  cloudexit:
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
