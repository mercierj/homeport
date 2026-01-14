// Package shared provides shared utilities for storage mappers.
package shared

import (
	"strings"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

// ExtractVolumeOptions extracts VolumeOptions from an AWS/GCP/Azure resource.
func ExtractVolumeOptions(res *resource.AWSResource, provider string) *mapper.VolumeOptions {
	name := res.GetConfigString("name")
	if name == "" {
		name = res.Name
	}

	sizeGB := res.GetConfigInt("size")
	if sizeGB == 0 {
		sizeGB = res.GetConfigInt("disk_size_gb") // Azure
	}
	if sizeGB == 0 {
		sizeGB = 10 // Default
	}

	opts := mapper.NewVolumeOptions(SanitizeVolumeName(name), sizeGB)
	opts.CloudProvider = provider
	opts.CloudRegion = res.Region
	opts.CloudZone = res.GetConfigString("availability_zone")
	if opts.CloudZone == "" {
		opts.CloudZone = res.GetConfigString("zone")
	}

	return opts
}

// SanitizeVolumeName ensures the name is valid for Docker volumes.
// Docker volume names must match [a-zA-Z0-9][a-zA-Z0-9_.-]*
func SanitizeVolumeName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "_", "-")
	name = strings.ReplaceAll(name, " ", "-")

	validName := ""
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '.' {
			validName += string(ch)
		}
	}

	// Trim leading/trailing special chars
	validName = strings.Trim(validName, "-.")

	// Ensure it doesn't start with a number
	if len(validName) > 0 && validName[0] >= '0' && validName[0] <= '9' {
		validName = "vol-" + validName
	}

	if validName == "" {
		validName = "volume"
	}

	return validName
}

// GenerateProjectID creates a stable project ID from volume name for XFS quotas.
// Starting at 1000 to avoid conflicts with system projects.
func GenerateProjectID(volumeName string) int {
	hash := 1000
	for _, c := range volumeName {
		hash = (hash*31 + int(c)) % 65535
	}
	if hash < 1000 {
		hash += 1000
	}
	return hash
}
