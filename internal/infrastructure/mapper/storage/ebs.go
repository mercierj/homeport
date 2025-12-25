// Package storage provides mappers for AWS storage services.
package storage

import (
	"context"
	"fmt"

	"github.com/cloudexit/cloudexit/internal/domain/mapper"
	"github.com/cloudexit/cloudexit/internal/domain/resource"
)

// EBSMapper converts AWS EBS volumes to Docker volumes.
type EBSMapper struct {
	*mapper.BaseMapper
}

// NewEBSMapper creates a new EBS to Docker volume mapper.
func NewEBSMapper() *EBSMapper {
	return &EBSMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeEBSVolume, nil),
	}
}

// Map converts an EBS volume to a Docker volume configuration.
func (m *EBSMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	volumeName := res.Name
	if volumeName == "" {
		volumeName = fmt.Sprintf("ebs-volume-%s", res.ID)
	}

	// Create result using new API
	// EBS volumes are mapped to Docker volumes, not services
	// We create a helper service that demonstrates volume usage
	result := mapper.NewMappingResult(fmt.Sprintf("volume-%s", volumeName))
	svc := result.DockerService

	// Extract EBS configuration
	size := res.GetConfigInt("size")
	volumeType := res.GetConfigString("type")
	if volumeType == "" {
		volumeType = "gp2"
	}
	iops := res.GetConfigInt("iops")
	throughput := res.GetConfigInt("throughput")
	encrypted := res.GetConfigBool("encrypted")
	availabilityZone := res.GetConfigString("availability_zone")

	// Configure a simple alpine container to demonstrate volume usage
	// In practice, users will attach this volume to their actual services
	svc.Image = "alpine:latest"
	svc.Command = []string{"sh", "-c", "echo 'Volume ready' && tail -f /dev/null"}
	svc.Volumes = []string{
		fmt.Sprintf("%s:/data", volumeName),
	}
	svc.Labels = map[string]string{
		"cloudexit.source":           "aws_ebs_volume",
		"cloudexit.volume":           volumeName,
		"cloudexit.volume.size":      fmt.Sprintf("%dGB", size),
		"cloudexit.volume.type":      volumeType,
		"cloudexit.type":             "volume-helper",
	}

	// Add volume definition to result
	volumeConfig := map[string]interface{}{
		"driver": "local",
		"name":   volumeName,
	}

	// Add size constraint as a label (Docker doesn't directly limit volume size)
	volumeConfig["labels"] = map[string]string{
		"cloudexit.size":              fmt.Sprintf("%d", size),
		"cloudexit.type":              volumeType,
		"cloudexit.availability_zone": availabilityZone,
	}

	result.AddConfig(fmt.Sprintf("volumes/%s.json", volumeName), []byte(m.generateVolumeConfigJSON(volumeConfig)))

	// Generate volume setup script
	volumeScript := m.generateVolumeSetupScript(volumeName, size, volumeType, iops, throughput, encrypted)
	result.AddScript("setup_volume.sh", []byte(volumeScript))

	// Add warnings and manual steps based on configuration
	result.AddWarning(fmt.Sprintf("EBS Volume Type: %s - Docker volumes don't have performance tiers. Consider using different storage drivers for performance characteristics.", volumeType))

	if size > 0 {
		result.AddWarning(fmt.Sprintf("EBS Volume Size: %d GB - Docker volumes are not size-limited by default. Monitor disk usage manually.", size))
	}

	if iops > 0 {
		result.AddWarning(fmt.Sprintf("EBS IOPS: %d - Docker volumes don't have IOPS guarantees. Performance depends on host storage configuration.", iops))
	}

	if throughput > 0 {
		result.AddWarning(fmt.Sprintf("EBS Throughput: %d MB/s - Docker volumes don't have throughput guarantees. Performance depends on host storage configuration.", throughput))
	}

	if encrypted {
		result.AddWarning("EBS encryption is enabled. Consider using encrypted filesystems (LUKS/dm-crypt) for the Docker volume data.")
		result.AddManualStep("To encrypt Docker volumes, use LUKS: https://docs.docker.com/storage/volumes/#use-a-volume-with-encryption")
	}

	// Volume type specific warnings
	switch volumeType {
	case "io1", "io2":
		result.AddWarning(fmt.Sprintf("EBS Volume Type %s (Provisioned IOPS SSD) - This is a high-performance volume. Ensure your host storage and Docker storage driver are configured for optimal performance.", volumeType))
		result.AddManualStep("Consider using a high-performance storage driver like overlay2 or btrfs for better I/O performance")
	case "st1":
		result.AddWarning("EBS Volume Type st1 (Throughput Optimized HDD) - This is optimized for sequential workloads. Consider using appropriate Docker storage driver and host filesystem.")
	case "sc1":
		result.AddWarning("EBS Volume Type sc1 (Cold HDD) - This is optimized for infrequent access. Standard Docker volumes are suitable for this use case.")
	case "gp3":
		result.AddWarning("EBS Volume Type gp3 (General Purpose SSD) - This is the latest general-purpose volume. Docker volumes with local driver should work well for most workloads.")
	}

	// Add manual steps
	result.AddManualStep(fmt.Sprintf("Create Docker volume: docker volume create %s", volumeName))
	result.AddManualStep(fmt.Sprintf("Attach volume to your service by adding to volumes section: %s:/path/to/mount", volumeName))
	result.AddManualStep("To inspect volume: docker volume inspect " + volumeName)

	if availabilityZone != "" {
		result.AddWarning(fmt.Sprintf("EBS Availability Zone: %s - Docker volumes are local to the host. For multi-host setups, consider using network storage or volume plugins.", availabilityZone))
	}

	// Handle snapshots
	if snapshotID := res.GetConfigString("snapshot_id"); snapshotID != "" {
		result.AddWarning(fmt.Sprintf("Volume created from snapshot: %s - You'll need to manually restore data from your EBS snapshot backup.", snapshotID))
		result.AddManualStep("Restore snapshot data manually to the Docker volume location")
	}

	// Handle KMS encryption
	if kmsKeyID := res.GetConfigString("kms_key_id"); kmsKeyID != "" {
		result.AddWarning(fmt.Sprintf("Volume encrypted with KMS key: %s - Docker volumes don't support KMS. Use alternative encryption methods.", kmsKeyID))
	}

	return result, nil
}

// generateVolumeSetupScript creates a volume setup script.
func (m *EBSMapper) generateVolumeSetupScript(volumeName string, size int, volumeType string, iops, throughput int, encrypted bool) string {
	return fmt.Sprintf(`#!/bin/bash
# Docker Volume Setup Script
# This script creates and configures the Docker volume for: %s

set -e

echo "Setting up Docker volume: %s"

# Create Docker volume
docker volume create %s

# Display volume information
echo ""
echo "Volume created successfully!"
echo ""
echo "Volume Details:"
echo "  Name: %s"
echo "  Size (from EBS): %d GB"
echo "  Type (from EBS): %s"
echo "  IOPS (from EBS): %d"
echo "  Throughput (from EBS): %d MB/s"
echo "  Encrypted (from EBS): %v"
echo ""
echo "IMPORTANT NOTES:"
echo "  - Docker volumes are not size-limited. Monitor disk usage manually."
echo "  - Performance depends on host storage and Docker storage driver."
echo "  - For encryption, consider using LUKS or other filesystem-level encryption."
echo ""
echo "To use this volume in a service:"
echo "  volumes:"
echo "    - %s:/path/to/mount"
echo ""
echo "To inspect the volume:"
echo "  docker volume inspect %s"
echo ""
echo "To find where Docker stores this volume:"
echo "  docker volume inspect %s | grep Mountpoint"
`,
		volumeName,
		volumeName,
		volumeName,
		volumeName,
		size,
		volumeType,
		iops,
		throughput,
		encrypted,
		volumeName,
		volumeName,
		volumeName,
	)
}

// generateVolumeConfigJSON creates a JSON configuration for the volume.
func (m *EBSMapper) generateVolumeConfigJSON(config map[string]interface{}) string {
	// Simple JSON generation for volume config
	jsonStr := "{\n"
	jsonStr += fmt.Sprintf("  \"name\": \"%s\",\n", config["name"])
	jsonStr += fmt.Sprintf("  \"driver\": \"%s\",\n", config["driver"])

	if labels, ok := config["labels"].(map[string]string); ok {
		jsonStr += "  \"labels\": {\n"
		first := true
		for k, v := range labels {
			if !first {
				jsonStr += ",\n"
			}
			jsonStr += fmt.Sprintf("    \"%s\": \"%s\"", k, v)
			first = false
		}
		jsonStr += "\n  }\n"
	}

	jsonStr += "}\n"
	return jsonStr
}
