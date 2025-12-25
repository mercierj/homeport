// Package storage provides mappers for AWS storage services.
package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudexit/cloudexit/internal/domain/mapper"
	"github.com/cloudexit/cloudexit/internal/domain/resource"
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
	svc.Labels = map[string]string{
		"cloudexit.source":      "aws_efs_file_system",
		"cloudexit.filesystem":  fileSystemName,
		"cloudexit.type":        "nfs-server",
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
		result.AddManualStep("Configure encryption for NFS data directory if required: https://wiki.archlinux.org/title/Dm-crypt")
	}

	// Generate NFS setup script
	nfsScript := m.generateNFSSetupScript(fileSystemName, res)
	result.AddScript("setup_nfs.sh", []byte(nfsScript))

	// Add manual steps for NFS client mounting
	result.AddManualStep(fmt.Sprintf("To mount this NFS share on clients, use: sudo mount -t nfs localhost:/data /mnt/%s", fileSystemName))
	result.AddManualStep("Ensure NFS client utilities are installed on client machines: apt-get install nfs-common (Ubuntu/Debian) or yum install nfs-utils (RHEL/CentOS)")

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
