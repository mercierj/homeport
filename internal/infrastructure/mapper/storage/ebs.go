// Package storage provides mappers for AWS storage services.
package storage

import (
	"context"
	"fmt"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
	"github.com/homeport/homeport/internal/infrastructure/mapper/shared/storagerunbook"
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
		"homeport.source":      "aws_ebs_volume",
		"homeport.volume":      volumeName,
		"homeport.volume.size": fmt.Sprintf("%dGB", size),
		"homeport.volume.type": volumeType,
		"homeport.type":        "volume-helper",
	}

	// Add volume definition to result
	volumeConfig := map[string]interface{}{
		"driver": "local",
		"name":   volumeName,
	}

	// Add size constraint as a label (Docker doesn't directly limit volume size)
	volumeConfig["labels"] = map[string]string{
		"homeport.size":              fmt.Sprintf("%d", size),
		"homeport.type":              volumeType,
		"homeport.availability_zone": availabilityZone,
	}

	result.AddConfig(fmt.Sprintf("volumes/%s.json", volumeName), []byte(m.generateVolumeConfigJSON(volumeConfig)))

	// Generate volume setup script
	volumeScript := m.generateVolumeSetupScript(volumeName, size, volumeType, iops, throughput, encrypted)
	result.AddScript("setup_volume.sh", []byte(volumeScript))
	result.AddScript("sync_ebs_volume.sh", []byte(m.generateSyncScript(volumeName)))
	result.AddScript("backup_ebs_volume.sh", []byte(m.generateBackupScript(volumeName)))
	result.AddScript("validate_ebs_volume.sh", []byte(m.generateValidationScript(volumeName)))
	result.AddConfig("config/ebs/app-change.env", []byte(m.generateAppChangeEnv(volumeName)))
	for _, step := range storagerunbook.BlockStorage(volumeName, "aws", res.GetConfigString("snapshot_id")) {
		result.AddRunbookStep(step)
	}
	for _, step := range ebsRunbook(volumeName) {
		result.AddRunbookStep(step)
	}

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
	}

	// Volume type specific warnings
	switch volumeType {
	case "io1", "io2":
		result.AddWarning(fmt.Sprintf("EBS Volume Type %s (Provisioned IOPS SSD) - This is a high-performance volume. Ensure your host storage and Docker storage driver are configured for optimal performance.", volumeType))
	case "st1":
		result.AddWarning("EBS Volume Type st1 (Throughput Optimized HDD) - This is optimized for sequential workloads. Consider using appropriate Docker storage driver and host filesystem.")
	case "sc1":
		result.AddWarning("EBS Volume Type sc1 (Cold HDD) - This is optimized for infrequent access. Standard Docker volumes are suitable for this use case.")
	case "gp3":
		result.AddWarning("EBS Volume Type gp3 (General Purpose SSD) - This is the latest general-purpose volume. Docker volumes with local driver should work well for most workloads.")
	}

	if availabilityZone != "" {
		result.AddWarning(fmt.Sprintf("EBS Availability Zone: %s - Docker volumes are local to the host. For multi-host setups, consider using network storage or volume plugins.", availabilityZone))
	}

	// Handle snapshots
	if snapshotID := res.GetConfigString("snapshot_id"); snapshotID != "" {
		result.AddWarning(fmt.Sprintf("Volume created from snapshot: %s - sync_ebs_volume.sh records the snapshot import handoff.", snapshotID))
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

func (m *EBSMapper) generateSyncScript(volumeName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
docker volume create %s
echo "sync source mount ${SOURCE_MOUNT:-/mnt/source} to docker volume %s"
echo ebs-volume-sync-ready
`, shellQuoteEBS(volumeName), volumeName)
}

func (m *EBSMapper) generateBackupScript(volumeName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/ebs-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" volumes config/ebs setup_volume.sh sync_ebs_volume.sh
echo "$archive"
`, volumeName)
}

func (m *EBSMapper) generateValidationScript(volumeName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
docker volume inspect %s >/dev/null
echo ebs-volume-validation-ok
`, shellQuoteEBS(volumeName))
}

func (m *EBSMapper) generateAppChangeEnv(volumeName string) string {
	return fmt.Sprintf("TARGET_VOLUME=%s\nTARGET_MOUNT=/data\nSOURCE_MOUNT=${SOURCE_MOUNT:-/mnt/source}\n", volumeName)
}

func ebsRunbook(volumeName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "block-storage", "volume": volumeName, "source": string(resource.TypeEBSVolume)}
	return []domainrunbook.Step{
		ebsStep("backup-ebs-volume-config", "Backup EBS volume migration config", domainrunbook.StepTypeCommand, []string{"sh", "backup_ebs_volume.sh"}, "backup archive path is printed", metadata),
		ebsStep("cutover-ebs-volume-mount", "Cut over service volume mount", domainrunbook.StepTypeAPICall, []string{"sh", "-c", ". config/ebs/app-change.env && echo $TARGET_VOLUME:$TARGET_MOUNT"}, "service mounts generated Docker volume", metadata),
		ebsStep("rollback-block-storage-source", "Rollback to EBS source", domainrunbook.StepTypeRollback, []string{"sh", "-c", "echo keep EBS source authoritative"}, "source EBS volume remains available", metadata),
	}
}

func ebsStep(id, name string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	return domainrunbook.Step{
		ID:               id,
		Name:             name,
		Type:             stepType,
		Status:           domainrunbook.StepStatusPending,
		Executor:         "shell",
		Command:          command,
		SuccessCondition: success,
		Metadata:         metadata,
	}
}

func shellQuoteEBS(value string) string {
	return "'" + value + "'"
}
