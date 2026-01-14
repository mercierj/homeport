// Package ovh generates Terraform configurations for OVHcloud deployments.
package ovh

// OVH Object Storage classes (S3-compatible)
const (
	StorageClassStandard = "standard"  // High performance
	StorageClassHigh     = "high_perf" // High performance SSD
	StorageClassCold     = "cold"      // Cold archive storage
)

// OVH Block Storage volume types
const (
	VolumeTypeClassic       = "classic"        // Standard HDD
	VolumeTypeHighSpeed     = "high-speed"     // High-speed SSD
	VolumeTypeHighSpeedGen2 = "high-speed-gen2" // Gen2 NVMe SSD
)
