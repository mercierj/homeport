// Package shared provides shared utilities for storage mappers.
package shared

import (
	"fmt"

	"github.com/homeport/homeport/internal/domain/mapper"
)

// MapAWSVolumeType maps AWS EBS volume types to PerformanceOptions.
func MapAWSVolumeType(volumeType string, iops, throughput, sizeGB int) *mapper.PerformanceOptions {
	perf := &mapper.PerformanceOptions{}

	switch volumeType {
	case "gp2":
		// gp2: 3 IOPS/GB, min 100, max 16000, burst to 3000
		perf.IOPSPerGB = 3
		baseIOPS := sizeGB * 3
		if baseIOPS < 100 {
			baseIOPS = 100
		}
		if baseIOPS > 16000 {
			baseIOPS = 16000
		}
		perf.IOPS = baseIOPS
		perf.BurstIOPS = 3000
		perf.ThroughputMBps = 250 // Max throughput for gp2
		perf.Tier = mapper.TierBalanced

	case "gp3":
		// gp3: baseline 3000 IOPS, configurable up to 16000
		// baseline 125 MB/s throughput, configurable up to 1000 MB/s
		if iops > 0 {
			perf.IOPS = iops
		} else {
			perf.IOPS = 3000
		}
		if throughput > 0 {
			perf.ThroughputMBps = throughput
		} else {
			perf.ThroughputMBps = 125
		}
		perf.Tier = mapper.TierBalanced

	case "io1":
		// io1: Provisioned IOPS SSD, up to 64000 IOPS
		if iops > 0 {
			perf.IOPS = iops
		} else {
			perf.IOPS = sizeGB * 50 // Default ratio
			if perf.IOPS > 64000 {
				perf.IOPS = 64000
			}
		}
		perf.ThroughputMBps = 1000
		perf.Tier = mapper.TierPremium

	case "io2":
		// io2: Provisioned IOPS SSD, up to 256000 IOPS with io2 Block Express
		if iops > 0 {
			perf.IOPS = iops
		} else {
			perf.IOPS = sizeGB * 500 // io2 allows higher ratio
			if perf.IOPS > 64000 {
				perf.IOPS = 64000 // Standard io2 limit
			}
		}
		perf.ThroughputMBps = 4000
		perf.Tier = mapper.TierPremium

	case "st1":
		// st1: Throughput Optimized HDD
		// 40 MB/s per TB, max 500 MB/s
		perf.ThroughputMBps = (sizeGB / 1024) * 40
		if perf.ThroughputMBps < 40 {
			perf.ThroughputMBps = 40
		}
		if perf.ThroughputMBps > 500 {
			perf.ThroughputMBps = 500
		}
		perf.IOPS = 500
		perf.Tier = mapper.TierStandard

	case "sc1":
		// sc1: Cold HDD
		// 12 MB/s per TB, max 250 MB/s
		perf.ThroughputMBps = (sizeGB / 1024) * 12
		if perf.ThroughputMBps < 12 {
			perf.ThroughputMBps = 12
		}
		if perf.ThroughputMBps > 250 {
			perf.ThroughputMBps = 250
		}
		perf.IOPS = 250
		perf.Tier = mapper.TierCold

	default:
		// Default to gp2-like behavior
		perf.IOPS = 3000
		perf.ThroughputMBps = 125
		perf.Tier = mapper.TierBalanced
	}

	perf.CalculateBlkioLimits()
	return perf
}

// MapGCPDiskType maps GCP disk types to PerformanceOptions.
func MapGCPDiskType(diskType string, provisionedIOPS, sizeGB int) *mapper.PerformanceOptions {
	perf := &mapper.PerformanceOptions{}

	switch diskType {
	case "pd-standard":
		// Standard persistent disk (HDD)
		// Read: 0.75 IOPS/GB, Write: 1.5 IOPS/GB
		perf.IOPS = (sizeGB * 3) / 4
		if perf.IOPS < 100 {
			perf.IOPS = 100
		}
		// Throughput: 0.12 MB/s per GB read, 0.12 MB/s per GB write
		perf.ThroughputMBps = (sizeGB * 12) / 100
		if perf.ThroughputMBps < 12 {
			perf.ThroughputMBps = 12
		}
		if perf.ThroughputMBps > 180 {
			perf.ThroughputMBps = 180
		}
		perf.Tier = mapper.TierStandard

	case "pd-balanced":
		// Balanced persistent disk (SSD)
		// 6 IOPS/GB, max 80000
		perf.IOPS = sizeGB * 6
		if perf.IOPS < 3000 {
			perf.IOPS = 3000
		}
		if perf.IOPS > 80000 {
			perf.IOPS = 80000
		}
		// Throughput: 0.28 MB/s per GB, max 1200
		perf.ThroughputMBps = (sizeGB * 28) / 100
		if perf.ThroughputMBps < 140 {
			perf.ThroughputMBps = 140
		}
		if perf.ThroughputMBps > 1200 {
			perf.ThroughputMBps = 1200
		}
		perf.Tier = mapper.TierBalanced

	case "pd-ssd":
		// SSD persistent disk
		// 30 IOPS/GB, max 100000
		perf.IOPS = sizeGB * 30
		if perf.IOPS < 3000 {
			perf.IOPS = 3000
		}
		if perf.IOPS > 100000 {
			perf.IOPS = 100000
		}
		// Throughput: 0.48 MB/s per GB, max 1200
		perf.ThroughputMBps = (sizeGB * 48) / 100
		if perf.ThroughputMBps < 240 {
			perf.ThroughputMBps = 240
		}
		if perf.ThroughputMBps > 1200 {
			perf.ThroughputMBps = 1200
		}
		perf.Tier = mapper.TierPremium

	case "pd-extreme":
		// Extreme persistent disk
		// Configurable IOPS up to 120000
		if provisionedIOPS > 0 {
			perf.IOPS = provisionedIOPS
		} else {
			perf.IOPS = 100000
		}
		if perf.IOPS > 120000 {
			perf.IOPS = 120000
		}
		perf.ThroughputMBps = 2400
		perf.Tier = mapper.TierUltra

	default:
		// Default to pd-balanced
		perf.IOPS = 3000
		perf.ThroughputMBps = 140
		perf.Tier = mapper.TierBalanced
	}

	perf.CalculateBlkioLimits()
	return perf
}

// MapAzureStorageType maps Azure storage account types to PerformanceOptions.
func MapAzureStorageType(storageType string, sizeGB int) *mapper.PerformanceOptions {
	perf := &mapper.PerformanceOptions{}

	switch storageType {
	case "Standard_LRS", "Standard_GRS", "Standard_RAGRS", "Standard_ZRS":
		// Standard HDD
		// 500 IOPS max, 60 MB/s max
		perf.IOPS = 500
		perf.ThroughputMBps = 60
		perf.Tier = mapper.TierStandard

	case "StandardSSD_LRS", "StandardSSD_ZRS":
		// Standard SSD
		// IOPS: 500 base + (size/32)*10, max 6000
		perf.IOPS = 500 + (sizeGB/32)*10
		if perf.IOPS > 6000 {
			perf.IOPS = 6000
		}
		// Throughput: 60 + (size/32)*2, max 750
		perf.ThroughputMBps = 60 + (sizeGB/32)*2
		if perf.ThroughputMBps > 750 {
			perf.ThroughputMBps = 750
		}
		perf.Tier = mapper.TierBalanced

	case "Premium_LRS", "Premium_ZRS":
		// Premium SSD
		// IOPS scales with size, see Azure docs for tiers
		// Simplified: 120 IOPS/GB, max 20000
		perf.IOPS = sizeGB * 120
		if perf.IOPS < 120 {
			perf.IOPS = 120
		}
		if perf.IOPS > 20000 {
			perf.IOPS = 20000
		}
		// Throughput scales with size, max 900 MB/s
		perf.ThroughputMBps = sizeGB / 4
		if perf.ThroughputMBps < 25 {
			perf.ThroughputMBps = 25
		}
		if perf.ThroughputMBps > 900 {
			perf.ThroughputMBps = 900
		}
		perf.Tier = mapper.TierPremium

	case "UltraSSD_LRS":
		// Ultra SSD
		// Up to 160000 IOPS, 2000 MB/s
		// IOPS and throughput are configurable
		perf.IOPS = 160000
		perf.ThroughputMBps = 2000
		perf.Tier = mapper.TierUltra

	default:
		// Default to Standard SSD
		perf.IOPS = 500
		perf.ThroughputMBps = 60
		perf.Tier = mapper.TierBalanced
	}

	perf.CalculateBlkioLimits()
	return perf
}

// GenerateBlkioConfig creates a BlkioConfig from PerformanceOptions.
func GenerateBlkioConfig(perf *mapper.PerformanceOptions, devicePath string) *mapper.BlkioConfig {
	if perf == nil || !perf.HasBlkioLimits() {
		return nil
	}

	config := &mapper.BlkioConfig{
		Weight: 500, // Default weight (range 10-1000)
	}

	if devicePath == "" {
		devicePath = "/dev/sda" // Default device
	}

	if perf.BlkioReadBps > 0 {
		config.DeviceReadBps = []mapper.DeviceLimit{
			{Path: devicePath, Rate: perf.BlkioReadBps},
		}
	}

	if perf.BlkioWriteBps > 0 {
		config.DeviceWriteBps = []mapper.DeviceLimit{
			{Path: devicePath, Rate: perf.BlkioWriteBps},
		}
	}

	if perf.BlkioReadIOPS > 0 {
		config.DeviceReadIOps = []mapper.DeviceLimit{
			{Path: devicePath, Rate: int64(perf.BlkioReadIOPS)},
		}
	}

	if perf.BlkioWriteIOPS > 0 {
		config.DeviceWriteIOps = []mapper.DeviceLimit{
			{Path: devicePath, Rate: int64(perf.BlkioWriteIOPS)},
		}
	}

	return config
}

// TierDescription returns a human-readable description of the performance tier.
func TierDescription(tier mapper.PerformanceTier) string {
	switch tier {
	case mapper.TierStandard:
		return "Standard (HDD-equivalent) - suitable for infrequent access workloads"
	case mapper.TierBalanced:
		return "Balanced (General Purpose SSD) - good for most workloads"
	case mapper.TierPremium:
		return "Premium (High-Performance SSD) - for I/O intensive applications"
	case mapper.TierUltra:
		return "Ultra (Highest Performance) - for extreme IOPS requirements"
	case mapper.TierCold:
		return "Cold (Infrequent Access) - optimized for archival storage"
	default:
		return "Unknown tier"
	}
}

// GeneratePerformanceWarnings creates warnings about performance limitations.
func GeneratePerformanceWarnings(opts *mapper.VolumeOptions) []string {
	var warnings []string

	if opts.Performance == nil {
		return warnings
	}

	perf := opts.Performance

	warnings = append(warnings, fmt.Sprintf(
		"Original volume type: %s (%s). Self-hosted performance depends on underlying hardware.",
		opts.VolumeType, TierDescription(perf.Tier)))

	if perf.IOPS > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"Cloud IOPS: %d. Docker blkio limits configured: read=%d IOPS, write=%d IOPS",
			perf.IOPS, perf.BlkioReadIOPS, perf.BlkioWriteIOPS))
	}

	if perf.ThroughputMBps > 0 {
		readMBps := perf.BlkioReadBps / (1024 * 1024)
		writeMBps := perf.BlkioWriteBps / (1024 * 1024)
		warnings = append(warnings, fmt.Sprintf(
			"Cloud throughput: %d MB/s. Docker blkio limits configured: read=%d MB/s, write=%d MB/s",
			perf.ThroughputMBps, readMBps, writeMBps))
	}

	if perf.Tier == mapper.TierUltra {
		warnings = append(warnings,
			"Ultra performance tier requires NVMe SSD storage on the host for comparable performance")
	}

	if perf.Tier == mapper.TierPremium {
		warnings = append(warnings,
			"Premium performance tier works best with SSD storage on the host")
	}

	return warnings
}
