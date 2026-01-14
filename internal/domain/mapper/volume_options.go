// Package mapper provides domain types for cloud resource mapping.
package mapper

// VolumeOptions represents the configuration options for a block storage volume.
// This struct provides a cloud-agnostic abstraction for volume features.
type VolumeOptions struct {
	// Basic properties
	Name       string
	SizeGB     int
	VolumeType string // Original cloud volume type (e.g., "gp3", "pd-ssd", "Premium_LRS")

	// Encryption settings
	Encryption *EncryptionOptions

	// Performance settings
	Performance *PerformanceOptions

	// Quota/limits
	Quota *QuotaOptions

	// Backup settings
	Backup *BackupOptions

	// Source (for restoration)
	Source *VolumeSource

	// Cloud-specific metadata
	CloudProvider string // "aws", "gcp", "azure"
	CloudRegion   string
	CloudZone     string
	Labels        map[string]string
}

// EncryptionOptions defines encryption configuration for volumes.
type EncryptionOptions struct {
	Enabled   bool
	Algorithm string // "aes-xts-plain64" for LUKS
	KeySize   int    // 256 or 512
	KeySource string // "generated", "file", "tpm"
	KeyFile   string // Path to key file if KeySource is "file"

	// Cloud-specific (for documentation/warnings)
	CloudKeyType string // "aws-kms", "gcp-cmek", "azure-cmk"
	CloudKeyID   string // Original KMS key ARN/ID
}

// PerformanceOptions defines IOPS and throughput settings.
type PerformanceOptions struct {
	// IOPS settings
	IOPS      int // Provisioned IOPS
	IOPSPerGB int // IOPS per GB ratio (for burst calculation)
	BurstIOPS int // Maximum burst IOPS

	// Throughput settings
	ThroughputMBps  int // MB/s throughput limit
	BurstThroughput int // Maximum burst throughput

	// Performance tier (mapped from cloud type)
	Tier PerformanceTier

	// Docker blkio settings (calculated)
	BlkioReadIOPS  int
	BlkioWriteIOPS int
	BlkioReadBps   int64 // Bytes per second
	BlkioWriteBps  int64
}

// PerformanceTier represents the performance category.
type PerformanceTier string

const (
	TierStandard PerformanceTier = "standard" // HDD-like
	TierBalanced PerformanceTier = "balanced" // General purpose SSD
	TierPremium  PerformanceTier = "premium"  // High-performance SSD
	TierUltra    PerformanceTier = "ultra"    // Highest performance
	TierCold     PerformanceTier = "cold"     // Infrequent access
)

// QuotaOptions defines size and usage limits.
type QuotaOptions struct {
	EnforceSize bool
	SizeGB      int

	// XFS project quota settings
	UseXFSQuota bool
	ProjectID   int

	// Loop device settings
	UseLoopDevice bool
	LoopFilePath  string
}

// BackupOptions defines backup/snapshot configuration.
type BackupOptions struct {
	EnableSnapshots bool
	ScheduleCron    string // Cron expression for automated backups
	RetentionDays   int
	BackupLocation  string // Directory for backups

	// Source snapshot to restore from
	SourceSnapshotID string
}

// VolumeSource defines the source for volume restoration.
type VolumeSource struct {
	Type      string // "snapshot", "image", "copy", "import"
	SourceID  string // Snapshot ID, image ID, etc.
	SourceURI string // For imports
}

// NewVolumeOptions creates a VolumeOptions with defaults.
func NewVolumeOptions(name string, sizeGB int) *VolumeOptions {
	return &VolumeOptions{
		Name:   name,
		SizeGB: sizeGB,
		Labels: make(map[string]string),
		Performance: &PerformanceOptions{
			Tier: TierBalanced,
		},
		Quota: &QuotaOptions{
			EnforceSize: true,
			SizeGB:      sizeGB,
		},
	}
}

// WithEncryption enables encryption with specified settings.
func (v *VolumeOptions) WithEncryption(enabled bool) *VolumeOptions {
	if enabled {
		v.Encryption = &EncryptionOptions{
			Enabled:   true,
			Algorithm: "aes-xts-plain64",
			KeySize:   256,
			KeySource: "generated",
		}
	}
	return v
}

// WithPerformance sets performance options.
func (v *VolumeOptions) WithPerformance(iops, throughputMBps int, tier PerformanceTier) *VolumeOptions {
	v.Performance = &PerformanceOptions{
		IOPS:           iops,
		ThroughputMBps: throughputMBps,
		Tier:           tier,
	}
	v.Performance.CalculateBlkioLimits()
	return v
}

// WithQuota sets quota options.
func (v *VolumeOptions) WithQuota(sizeGB int, useXFS bool) *VolumeOptions {
	v.Quota = &QuotaOptions{
		EnforceSize: true,
		SizeGB:      sizeGB,
		UseXFSQuota: useXFS,
	}
	return v
}

// WithBackup sets backup options.
func (v *VolumeOptions) WithBackup(retentionDays int, location string) *VolumeOptions {
	v.Backup = &BackupOptions{
		EnableSnapshots: true,
		RetentionDays:   retentionDays,
		BackupLocation:  location,
	}
	return v
}

// CalculateBlkioLimits converts IOPS/throughput to Docker blkio settings.
func (p *PerformanceOptions) CalculateBlkioLimits() {
	if p.IOPS > 0 {
		// Split IOPS between read and write (can be adjusted)
		p.BlkioReadIOPS = p.IOPS / 2
		p.BlkioWriteIOPS = p.IOPS / 2
	}
	if p.ThroughputMBps > 0 {
		// Convert MB/s to bytes/s
		p.BlkioReadBps = int64(p.ThroughputMBps) * 1024 * 1024 / 2
		p.BlkioWriteBps = int64(p.ThroughputMBps) * 1024 * 1024 / 2
	}
}

// HasBlkioLimits returns true if any blkio limits are set.
func (p *PerformanceOptions) HasBlkioLimits() bool {
	return p.BlkioReadIOPS > 0 || p.BlkioWriteIOPS > 0 ||
		p.BlkioReadBps > 0 || p.BlkioWriteBps > 0
}

// BlkioConfig represents Docker compose blkio configuration.
type BlkioConfig struct {
	Weight          int           // Block IO weight (10-1000)
	DeviceReadBps   []DeviceLimit // Read rate limit per device
	DeviceWriteBps  []DeviceLimit // Write rate limit per device
	DeviceReadIOps  []DeviceLimit // Read IOPS limit per device
	DeviceWriteIOps []DeviceLimit // Write IOPS limit per device
}

// DeviceLimit represents a rate limit for a specific device.
type DeviceLimit struct {
	Path string // Device path (e.g., "/dev/sda")
	Rate int64  // Rate limit (bytes/s for bps, ops/s for IOPS)
}
