// Package storage provides mappers for Azure storage services.
package storage

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudexit/cloudexit/internal/domain/mapper"
	"github.com/cloudexit/cloudexit/internal/domain/resource"
)

// ManagedDiskMapper converts Azure Managed Disks to Docker volumes.
type ManagedDiskMapper struct {
	*mapper.BaseMapper
}

// NewManagedDiskMapper creates a new Azure Managed Disk to Docker volume mapper.
func NewManagedDiskMapper() *ManagedDiskMapper {
	return &ManagedDiskMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeManagedDisk, nil),
	}
}

// Map converts an Azure Managed Disk to a Docker volume.
func (m *ManagedDiskMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	diskName := res.GetConfigString("name")
	if diskName == "" {
		diskName = res.Name
	}

	// Managed disks don't map to a service, they map to Docker volumes
	// We'll create a result with volume configuration
	result := mapper.NewMappingResult(m.sanitizeName(diskName) + "-volume")
	svc := result.DockerService

	// This is a placeholder service - managed disks are volumes, not services
	// The actual volume will be added to the result
	svc.Image = "busybox:latest"
	svc.Command = []string{"true"} // No-op command
	svc.Labels = map[string]string{
		"cloudexit.source":    "azurerm_managed_disk",
		"cloudexit.disk_name": diskName,
		"cloudexit.note":      "This is a volume placeholder - attach to your VM services",
	}

	// Get disk configuration
	diskSizeGB := res.GetConfigInt("disk_size_gb")
	if diskSizeGB == 0 {
		diskSizeGB = 128 // Default size
	}

	storageAccountType := res.GetConfigString("storage_account_type")
	if storageAccountType == "" {
		storageAccountType = "Standard_LRS"
	}

	createOption := res.GetConfigString("create_option")
	if createOption == "" {
		createOption = "Empty"
	}

	// Create Docker volume
	volumeName := m.sanitizeName(diskName)
	volume := mapper.Volume{
		Name:   volumeName,
		Driver: "local",
		Labels: map[string]string{
			"cloudexit.source":              "azurerm_managed_disk",
			"cloudexit.disk_name":           diskName,
			"cloudexit.disk_size_gb":        fmt.Sprintf("%d", diskSizeGB),
			"cloudexit.storage_account_type": storageAccountType,
			"cloudexit.create_option":       createOption,
		},
	}

	// Map storage account type to performance characteristics
	switch storageAccountType {
	case "Premium_LRS", "Premium_ZRS":
		volume.Labels["cloudexit.performance"] = "premium-ssd"
		result.AddWarning(fmt.Sprintf("Premium SSD disk (%s). Docker volumes don't enforce performance tiers - performance depends on host storage.", storageAccountType))
	case "StandardSSD_LRS", "StandardSSD_ZRS":
		volume.Labels["cloudexit.performance"] = "standard-ssd"
	case "Standard_LRS":
		volume.Labels["cloudexit.performance"] = "standard-hdd"
	case "UltraSSD_LRS":
		volume.Labels["cloudexit.performance"] = "ultra-ssd"
		result.AddWarning("Ultra SSD disk detected. Docker volumes don't enforce Ultra SSD performance - ensure host has appropriate storage.")
	}

	// Handle ZRS (Zone Redundant Storage)
	if strings.Contains(storageAccountType, "ZRS") {
		result.AddWarning("Zone-redundant storage is configured. Docker volumes are single-instance - consider backups for data redundancy.")
	}

	result.AddVolume(volume)

	// Handle disk encryption
	if encryptionSettings := res.Config["encryption_settings"]; encryptionSettings != nil {
		result.AddWarning("Disk encryption is configured. Docker volumes are not encrypted by default - enable host-level encryption if needed.")
	}

	// Handle disk access
	if diskAccessID := res.GetConfigString("disk_access_id"); diskAccessID != "" {
		result.AddWarning("Disk access resource is configured. Map to appropriate Docker volume access controls.")
	}

	// Handle network access policy
	if networkAccessPolicy := res.GetConfigString("network_access_policy"); networkAccessPolicy != "" {
		result.AddWarning(fmt.Sprintf("Network access policy '%s' is configured. Docker volumes don't have network access policies.", networkAccessPolicy))
	}

	// Handle source configurations
	switch createOption {
	case "Copy":
		sourceResourceID := res.GetConfigString("source_resource_id")
		if sourceResourceID != "" {
			result.AddWarning(fmt.Sprintf("Disk is created from copy (source: %s). Manually copy data to the Docker volume if needed.", sourceResourceID))
			result.AddManualStep(fmt.Sprintf("If needed, copy data to volume: docker run --rm -v %s:/target -v /source/path:/source busybox cp -r /source/* /target/", volumeName))
		}
	case "FromImage":
		imageReferenceID := res.GetConfigString("image_reference_id")
		if imageReferenceID != "" {
			result.AddWarning(fmt.Sprintf("Disk is created from image (source: %s). Initialize volume with appropriate data.", imageReferenceID))
		}
	case "Import":
		sourceURI := res.GetConfigString("source_uri")
		storageAccountID := res.GetConfigString("storage_account_id")
		if sourceURI != "" {
			result.AddWarning(fmt.Sprintf("Disk is imported from URI: %s. Download and populate the Docker volume manually.", sourceURI))
			result.AddManualStep(fmt.Sprintf("Import disk data: download from '%s' and copy to volume '%s'", sourceURI, volumeName))
		}
		if storageAccountID != "" {
			result.AddWarning(fmt.Sprintf("Import uses storage account: %s", storageAccountID))
		}
	case "Restore":
		sourceSnapshotID := res.GetConfigString("source_snapshot_id")
		if sourceSnapshotID != "" {
			result.AddWarning(fmt.Sprintf("Disk is restored from snapshot: %s. Restore data to the Docker volume manually.", sourceSnapshotID))
		}
	case "Empty":
		// Empty disk - no special handling needed
	}

	// Generate usage documentation
	usageDoc := m.generateUsageDoc(diskName, volumeName, diskSizeGB, storageAccountType)
	result.AddConfig(fmt.Sprintf("config/volumes/%s-usage.txt", diskName), []byte(usageDoc))

	// Generate mount script
	mountScript := m.generateMountScript(diskName, volumeName)
	result.AddScript(fmt.Sprintf("mount_%s.sh", diskName), []byte(mountScript))

	result.AddManualStep(fmt.Sprintf("Volume '%s' created. Attach to VM services using: volumes: - %s:/mnt/disk", volumeName, volumeName))
	result.AddManualStep(fmt.Sprintf("Disk size: %d GB. Docker volumes grow dynamically - no size limit enforced.", diskSizeGB))

	return result, nil
}

// generateUsageDoc creates documentation for using the disk volume.
func (m *ManagedDiskMapper) generateUsageDoc(diskName, volumeName string, sizeGB int, storageType string) string {
	return fmt.Sprintf(`Azure Managed Disk: %s
Docker Volume: %s

Configuration:
- Size: %d GB
- Storage Type: %s
- Volume Name: %s

Usage in Docker Compose:
services:
  my-service:
    image: my-image
    volumes:
      - %s:/mnt/disk

To attach to an existing service:
1. Add the volume to the service's volumes list
2. Mount point can be customized (e.g., /mnt/data, /data, etc.)

Volume Management:
- List volumes: docker volume ls
- Inspect volume: docker volume inspect %s
- Backup volume: docker run --rm -v %s:/source -v $(pwd):/backup busybox tar czf /backup/%s-backup.tar.gz -C /source .
- Restore volume: docker run --rm -v %s:/target -v $(pwd):/backup busybox tar xzf /backup/%s-backup.tar.gz -C /target

Notes:
- Docker volumes don't enforce size limits - %d GB is advisory
- Performance depends on host storage, not volume configuration
- Consider regular backups for data redundancy
`, diskName, volumeName, sizeGB, storageType, volumeName, volumeName, volumeName, volumeName, diskName, volumeName, diskName, sizeGB)
}

// generateMountScript creates a helper script for mounting the volume.
func (m *ManagedDiskMapper) generateMountScript(diskName, volumeName string) string {
	return fmt.Sprintf(`#!/bin/bash
# Mount script for Azure Managed Disk: %s
# Docker Volume: %s

# This script demonstrates how to use the Docker volume

echo "Docker volume '%s' is available for use"
echo ""
echo "To attach to a service, add to docker-compose.yml:"
echo ""
echo "services:"
echo "  your-service:"
echo "    volumes:"
echo "      - %s:/mnt/disk"
echo ""
echo "To inspect the volume:"
echo "  docker volume inspect %s"
echo ""
echo "To access the volume data:"
echo "  docker run --rm -it -v %s:/data busybox sh"
echo ""
echo "To backup the volume:"
echo "  docker run --rm -v %s:/source -v \$(pwd):/backup busybox tar czf /backup/%s-backup.tar.gz -C /source ."
`, diskName, volumeName, volumeName, volumeName, volumeName, volumeName, volumeName, diskName)
}

// sanitizeName ensures the name is valid for Docker volume names.
func (m *ManagedDiskMapper) sanitizeName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "_", "-")
	validName := ""
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' {
			validName += string(ch)
		}
	}
	validName = strings.TrimLeft(validName, "-0123456789")
	if validName == "" {
		validName = "disk"
	}
	return validName
}
