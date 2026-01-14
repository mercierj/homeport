// Package provider defines instance sizing logic for cloud migration.
// This includes resource requirement extraction from mapping results and
// instance selection based on requirements for target providers.
package provider

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/homeport/homeport/internal/domain/mapper"
)

// ResourceRequirements represents the total resource requirements for a migration.
type ResourceRequirements struct {
	// VCPUs is the total number of virtual CPU cores required
	VCPUs int `json:"vcpus"`

	// MemoryGB is the total amount of RAM in gigabytes required
	MemoryGB float64 `json:"memory_gb"`

	// StorageGB is the total storage in gigabytes required
	StorageGB int `json:"storage_gb"`

	// Description is a human-readable summary of the requirements
	Description string `json:"description"`
}

// ExtractRequirements analyzes mapping results and calculates total resource requirements.
// It sums up CPU, memory, and storage from all services, and adds a 20% headroom buffer.
func ExtractRequirements(results []*mapper.MappingResult) ResourceRequirements {
	var totalCPUs float64
	var totalMemoryGB float64
	var totalStorageGB int

	for _, result := range results {
		if result == nil {
			continue
		}

		// Extract from main DockerService
		if result.DockerService != nil {
			cpus, mem := extractResourcesFromService(result.DockerService)
			totalCPUs += cpus
			totalMemoryGB += mem
		}

		// Extract from additional services
		for _, svc := range result.AdditionalServices {
			if svc != nil {
				cpus, mem := extractResourcesFromService(svc)
				totalCPUs += cpus
				totalMemoryGB += mem
			}
		}

		// Extract storage from volumes
		for _, vol := range result.Volumes {
			storageGB := extractStorageFromVolume(vol)
			totalStorageGB += storageGB
		}
	}

	// Apply 20% headroom buffer for overhead
	const headroomPercent = 0.20
	totalCPUs *= (1 + headroomPercent)
	totalMemoryGB *= (1 + headroomPercent)
	totalStorageGB = int(float64(totalStorageGB) * (1 + headroomPercent))

	// Ensure minimum requirements
	vcpus := int(math.Ceil(totalCPUs))
	if vcpus < 1 {
		vcpus = 1
	}
	if totalMemoryGB < 1 {
		totalMemoryGB = 1
	}
	if totalStorageGB < 10 {
		totalStorageGB = 10
	}

	return ResourceRequirements{
		VCPUs:     vcpus,
		MemoryGB:  totalMemoryGB,
		StorageGB: totalStorageGB,
		Description: fmt.Sprintf("%d vCPU(s), %.1f GB RAM, %d GB storage (incl. 20%% headroom)",
			vcpus, totalMemoryGB, totalStorageGB),
	}
}

// extractResourcesFromService extracts CPU and memory requirements from a Docker service.
func extractResourcesFromService(svc *mapper.DockerService) (cpus float64, memoryGB float64) {
	if svc.Deploy == nil || svc.Deploy.Resources == nil {
		// Default minimal resources if not specified
		return 0.25, 0.5
	}

	res := svc.Deploy.Resources

	// Check limits first, then reservations
	if res.Limits != nil {
		cpus = parseCPUs(res.Limits.CPUs)
		memoryGB = parseMemory(res.Limits.Memory)
	}

	// If limits are zero, try reservations
	if cpus == 0 && res.Reservations != nil {
		cpus = parseCPUs(res.Reservations.CPUs)
	}
	if memoryGB == 0 && res.Reservations != nil {
		memoryGB = parseMemory(res.Reservations.Memory)
	}

	// Apply defaults if still zero
	if cpus == 0 {
		cpus = 0.25
	}
	if memoryGB == 0 {
		memoryGB = 0.5
	}

	return cpus, memoryGB
}

// parseCPUs parses a CPU string like "1.5" or "0.5" to a float64.
func parseCPUs(s string) float64 {
	if s == "" {
		return 0
	}
	val, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return val
}

// parseMemory parses a memory string like "512M", "2G", "1024m" to GB.
func parseMemory(s string) float64 {
	if s == "" {
		return 0
	}

	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return 0
	}

	// Get the unit suffix
	lastChar := strings.ToUpper(string(s[len(s)-1]))
	numStr := s[:len(s)-1]

	// Handle case where there's no suffix (assume bytes)
	if lastChar[0] >= '0' && lastChar[0] <= '9' {
		numStr = s
		lastChar = "B"
	}

	val, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0
	}

	switch lastChar {
	case "K":
		return val / (1024 * 1024) // KB to GB
	case "M":
		return val / 1024 // MB to GB
	case "G":
		return val // Already GB
	case "T":
		return val * 1024 // TB to GB
	default:
		return val / (1024 * 1024 * 1024) // Bytes to GB
	}
}

// extractStorageFromVolume extracts storage size from a volume configuration.
func extractStorageFromVolume(vol mapper.Volume) int {
	// Check driver opts for size hints
	if vol.DriverOpts != nil {
		if sizeStr, ok := vol.DriverOpts["size"]; ok {
			return parseStorageSize(sizeStr)
		}
		if sizeStr, ok := vol.DriverOpts["capacity"]; ok {
			return parseStorageSize(sizeStr)
		}
	}

	// Default volume size if not specified
	return 10
}

// parseStorageSize parses a storage size string like "100G", "50GB", "1T" to GB.
func parseStorageSize(s string) int {
	s = strings.TrimSpace(strings.ToUpper(s))
	if len(s) == 0 {
		return 10
	}

	// Remove trailing "B" if present (e.g., "100GB" -> "100G")
	s = strings.TrimSuffix(s, "B")
	if len(s) == 0 {
		return 10
	}

	lastChar := string(s[len(s)-1])
	numStr := s[:len(s)-1]

	// Handle case where there's no suffix
	if lastChar[0] >= '0' && lastChar[0] <= '9' {
		numStr = s
		lastChar = "G" // Assume GB
	}

	val, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 10
	}

	switch lastChar {
	case "K":
		return int(val / (1024 * 1024))
	case "M":
		return int(val / 1024)
	case "G":
		return int(val)
	case "T":
		return int(val * 1024)
	default:
		return int(val)
	}
}

// SelectInstance selects the cheapest instance that meets the requirements for a provider.
// If no instance meets the requirements, returns the largest available instance.
func SelectInstance(p Provider, req ResourceRequirements) *InstancePricing {
	instances := GetInstanceTypes(p)
	if len(instances) == 0 {
		return nil
	}

	// Filter instances that meet requirements
	var suitable []InstancePricing
	for _, inst := range instances {
		if inst.VCPUs >= req.VCPUs && inst.MemoryGB >= req.MemoryGB {
			suitable = append(suitable, inst)
		}
	}

	// If no instance meets requirements, return the largest available
	if len(suitable) == 0 {
		// Sort by resources (VCPUs * MemoryGB) descending
		sort.Slice(instances, func(i, j int) bool {
			scoreI := float64(instances[i].VCPUs) * instances[i].MemoryGB
			scoreJ := float64(instances[j].VCPUs) * instances[j].MemoryGB
			return scoreI > scoreJ
		})
		result := instances[0]
		return &result
	}

	// Sort suitable instances by price ascending
	sort.Slice(suitable, func(i, j int) bool {
		return suitable[i].PricePerMonth < suitable[j].PricePerMonth
	})

	// Return the cheapest suitable instance
	result := suitable[0]
	return &result
}

// SelectInstanceWithBuffer selects an instance with an additional buffer applied to requirements.
func SelectInstanceWithBuffer(p Provider, req ResourceRequirements, bufferPercent float64) *InstancePricing {
	bufferedReq := ResourceRequirements{
		VCPUs:     int(math.Ceil(float64(req.VCPUs) * (1 + bufferPercent/100))),
		MemoryGB:  req.MemoryGB * (1 + bufferPercent/100),
		StorageGB: int(float64(req.StorageGB) * (1 + bufferPercent/100)),
		Description: fmt.Sprintf("%s (with %.0f%% buffer)", req.Description, bufferPercent),
	}

	return SelectInstance(p, bufferedReq)
}

// EstimateServerCount returns the number of servers needed for a given HA level.
func EstimateServerCount(haLevel string) int {
	switch strings.ToLower(haLevel) {
	case "none":
		return 1
	case "basic":
		return 1
	case "multi-server":
		return 2
	case "cluster":
		return 3
	default:
		return 1
	}
}

// CalculateTotalCost calculates the complete cost breakdown for a migration.
func CalculateTotalCost(p Provider, req ResourceRequirements, haLevel string, storageGB int, egressGB int) *CostBreakdown {
	pricing := GetProviderPricing(p)
	if pricing == nil {
		return nil
	}

	// Select appropriate instance
	instance := SelectInstance(p, req)
	if instance == nil {
		return nil
	}

	// Calculate server count for HA level
	serverCount := EstimateServerCount(haLevel)

	// Calculate compute cost
	computeCost := instance.PricePerMonth * float64(serverCount)

	// Calculate storage cost
	storageCost := pricing.Storage.EstimateStorageCost(storageGB)

	// Calculate network cost (with free tier)
	networkCost := pricing.Network.EstimateEgressCost(egressGB)

	breakdown := &CostBreakdown{
		ComputeCost: computeCost,
		StorageCost: storageCost,
		NetworkCost: networkCost,
		Currency:    pricing.Storage.Currency,
	}
	breakdown.CalculateTotal()

	return breakdown
}
