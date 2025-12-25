// Package storage provides mappers for GCP storage services.
package storage

import (
	"context"
	"fmt"
	"strings"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

// PersistentDiskMapper converts GCP Persistent Disks to Docker volumes.
type PersistentDiskMapper struct {
	*mapper.BaseMapper
}

// NewPersistentDiskMapper creates a new Persistent Disk to Docker volume mapper.
func NewPersistentDiskMapper() *PersistentDiskMapper {
	return &PersistentDiskMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypePersistentDisk, nil),
	}
}

// Map converts a GCP Persistent Disk to a Docker volume.
func (m *PersistentDiskMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	diskName := res.GetConfigString("name")
	if diskName == "" {
		diskName = res.Name
	}

	// Sanitize disk name for Docker volume
	volumeName := m.sanitizeName(diskName)

	// Create result - for volumes, we don't create a service but provide volume configuration
	result := mapper.NewMappingResult(volumeName + "-volume")
	svc := result.DockerService

	// Get disk properties
	diskSize := res.GetConfigInt("size")
	if diskSize == 0 {
		diskSize = 10 // Default 10GB
	}

	diskType := res.GetConfigString("type")
	if diskType == "" {
		diskType = "pd-standard"
	}

	// Map GCP disk type to Docker volume driver options
	volumeDriver := "local"
	volumeOpts := m.mapDiskTypeToVolumeOpts(diskType, diskSize)

	// Since this is a volume, we create a minimal service that manages the volume
	// or we can just document the volume creation
	svc.Image = "busybox:latest"
	svc.Command = []string{"/bin/sh", "-c", "echo 'Volume holder for " + volumeName + "' && sleep infinity"}
	svc.Volumes = []string{
		fmt.Sprintf("%s:/data", volumeName),
	}
	svc.Labels = map[string]string{
		"cloudexit.source":    "google_compute_disk",
		"cloudexit.disk_name": diskName,
		"cloudexit.disk_type": diskType,
		"cloudexit.disk_size": fmt.Sprintf("%dGB", diskSize),
	}
	svc.Restart = "unless-stopped"

	// Generate docker-compose volume configuration
	volumeConfig := m.generateVolumeConfig(volumeName, volumeDriver, volumeOpts)
	result.AddConfig("volumes.yml", []byte(volumeConfig))

	// Generate script to create and manage the volume
	setupScript := m.generateSetupScript(volumeName, diskSize, diskType)
	result.AddScript("setup_volume.sh", []byte(setupScript))

	// Handle disk snapshots
	if snapshots := m.getSnapshots(res); len(snapshots) > 0 {
		result.AddWarning(fmt.Sprintf("Disk has %d snapshot(s). Consider using Docker volume backups.", len(snapshots)))
		result.AddManualStep("Set up volume backup strategy using 'docker run --rm -v " + volumeName + ":/data -v $(pwd):/backup busybox tar czf /backup/backup.tar.gz /data'")
	}

	// Handle disk encryption
	if encryption := res.Config["disk_encryption_key"]; encryption != nil {
		result.AddWarning("Disk encryption is configured. Docker volumes can be encrypted using dm-crypt or encrypted filesystems.")
		result.AddManualStep("Consider setting up LUKS encryption for the Docker volume if needed")
	}

	// Handle zonal disk location
	zone := res.GetConfigString("zone")
	if zone != "" {
		result.AddWarning(fmt.Sprintf("Disk is in zone '%s'. Docker volumes are local to the host.", zone))
	}

	// Handle disk source image
	if sourceImage := res.GetConfigString("image"); sourceImage != "" {
		result.AddWarning(fmt.Sprintf("Disk created from image '%s'. You may need to restore data to the volume.", sourceImage))
		result.AddManualStep("If the disk contains data from an image, export the GCP disk and import to Docker volume")
	}

	// Handle disk source snapshot
	if sourceSnapshot := res.GetConfigString("snapshot"); sourceSnapshot != "" {
		result.AddWarning(fmt.Sprintf("Disk created from snapshot '%s'. Export snapshot data and restore to volume.", sourceSnapshot))
	}

	// Handle replica zones (for regional disks)
	if replicaZones := res.Config["replica_zones"]; replicaZones != nil {
		result.AddWarning("Regional persistent disk detected. Docker volumes are single-host. Consider using distributed storage solutions like GlusterFS or Ceph.")
		result.AddManualStep("For high availability, consider Docker Swarm with distributed volume plugins")
	}

	// Handle provisioned IOPS (for pd-extreme)
	if provisionedIops := res.GetConfigInt("provisioned_iops"); provisionedIops > 0 {
		result.AddWarning(fmt.Sprintf("Disk has provisioned IOPS: %d. Docker volume performance depends on host storage.", provisionedIops))
	}

	result.AddManualStep(fmt.Sprintf("Create the Docker volume: docker volume create %s", volumeName))
	result.AddManualStep(fmt.Sprintf("Attach volume to containers using: volumes: - %s:/mount/path", volumeName))

	return result, nil
}

// mapDiskTypeToVolumeOpts maps GCP disk types to Docker volume options.
func (m *PersistentDiskMapper) mapDiskTypeToVolumeOpts(diskType string, sizeGB int) map[string]string {
	opts := make(map[string]string)

	// Set volume driver options based on disk type
	switch diskType {
	case "pd-standard":
		// Standard persistent disk - basic performance
		opts["type"] = "none"
		opts["device"] = "tmpfs"
		opts["o"] = "size=" + fmt.Sprintf("%dG", sizeGB)
	case "pd-balanced":
		// Balanced persistent disk - good performance
		opts["type"] = "none"
		opts["device"] = "tmpfs"
		opts["o"] = "size=" + fmt.Sprintf("%dG", sizeGB)
	case "pd-ssd":
		// SSD persistent disk - high performance
		opts["type"] = "none"
		opts["device"] = "tmpfs"
		opts["o"] = "size=" + fmt.Sprintf("%dG", sizeGB)
	case "pd-extreme":
		// Extreme persistent disk - highest performance
		opts["type"] = "none"
		opts["device"] = "tmpfs"
		opts["o"] = "size=" + fmt.Sprintf("%dG", sizeGB)
	default:
		opts["type"] = "none"
	}

	return opts
}

// generateVolumeConfig generates docker-compose volume configuration.
func (m *PersistentDiskMapper) generateVolumeConfig(volumeName, driver string, opts map[string]string) string {
	config := fmt.Sprintf(`# Docker Compose Volume Configuration
# Migrated from GCP Persistent Disk: %s

volumes:
  %s:
    driver: %s
`, volumeName, volumeName, driver)

	if len(opts) > 0 {
		config += "    driver_opts:\n"
		for key, value := range opts {
			config += fmt.Sprintf("      %s: \"%s\"\n", key, value)
		}
	}

	config += `
# Usage in docker-compose.yml:
# services:
#   myapp:
#     volumes:
#       - ` + volumeName + `:/data
`

	return config
}

// generateSetupScript creates a script to set up the volume.
func (m *PersistentDiskMapper) generateSetupScript(volumeName string, sizeGB int, diskType string) string {
	return fmt.Sprintf(`#!/bin/bash
# Docker Volume Setup Script
# Migrated from GCP Persistent Disk

set -e

VOLUME_NAME="%s"
DISK_SIZE="%dGB"
DISK_TYPE="%s"

echo "Setting up Docker volume: $VOLUME_NAME"
echo "Disk type: $DISK_TYPE"
echo "Size: $DISK_SIZE"

# Create Docker volume
if docker volume inspect "$VOLUME_NAME" >/dev/null 2>&1; then
    echo "Volume '$VOLUME_NAME' already exists"
else
    echo "Creating volume '$VOLUME_NAME'..."
    docker volume create "$VOLUME_NAME"
    echo "Volume created successfully"
fi

# Display volume information
echo ""
echo "Volume details:"
docker volume inspect "$VOLUME_NAME"

echo ""
echo "Volume '$VOLUME_NAME' is ready!"
echo ""
echo "To use in docker-compose.yml:"
echo "  volumes:"
echo "    - $VOLUME_NAME:/data"
echo ""
echo "To backup the volume:"
echo "  docker run --rm -v $VOLUME_NAME:/data -v \$(pwd):/backup busybox tar czf /backup/$VOLUME_NAME-backup.tar.gz /data"
echo ""
echo "To restore the volume:"
echo "  docker run --rm -v $VOLUME_NAME:/data -v \$(pwd):/backup busybox tar xzf /backup/$VOLUME_NAME-backup.tar.gz -C /"

# Performance notes based on disk type
case "$DISK_TYPE" in
    "pd-ssd"|"pd-extreme")
        echo ""
        echo "NOTE: Original disk was SSD-based ($DISK_TYPE)."
        echo "For best performance, ensure your Docker host uses SSD storage."
        ;;
    "pd-balanced")
        echo ""
        echo "NOTE: Original disk was balanced ($DISK_TYPE)."
        echo "Standard Docker volume should provide similar performance."
        ;;
esac
`, volumeName, sizeGB, diskType)
}

// getSnapshots extracts snapshot information if available.
func (m *PersistentDiskMapper) getSnapshots(res *resource.AWSResource) []string {
	var snapshots []string

	// Note: Snapshot information might not be directly in the disk resource
	// This is a placeholder for future enhancement
	if snapshot := res.GetConfigString("snapshot"); snapshot != "" {
		snapshots = append(snapshots, snapshot)
	}

	return snapshots
}

// sanitizeName sanitizes the disk name for Docker volume.
func (m *PersistentDiskMapper) sanitizeName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "_", "-")
	name = strings.ReplaceAll(name, " ", "-")

	validName := ""
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '.' {
			validName += string(ch)
		}
	}

	validName = strings.Trim(validName, "-.")
	if validName == "" {
		validName = "disk"
	}

	return validName
}
