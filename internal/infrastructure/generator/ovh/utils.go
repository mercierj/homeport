// Package ovh provides utility functions for OVH Cloud generator.
package ovh

import (
	"github.com/homeport/homeport/internal/domain/generator"
	"github.com/homeport/homeport/internal/domain/mapper"
)

// getRegion returns the OVH region from config or default.
func getRegion(config *generator.TargetConfig) string {
	if config.TargetConfig != nil && config.TargetConfig.OVH != nil && config.TargetConfig.OVH.Region != "" {
		return config.TargetConfig.OVH.Region
	}
	return "GRA11" // Default to Gravelines, France
}

// getBlockStorageSize returns an estimated block storage size for the result.
func getBlockStorageSize(res *mapper.MappingResult) int {
	if res == nil || res.SourceResource == nil {
		return 50 // Default 50GB
	}

	// Check for size in config
	if size, ok := res.SourceResource.Config["size_gb"].(float64); ok {
		return int(size)
	}
	if size, ok := res.SourceResource.Config["size"].(float64); ok {
		return int(size)
	}
	if size, ok := res.SourceResource.Config["allocated_storage"].(float64); ok {
		return int(size)
	}

	return 50 // Default
}

// ResourceProperties provides a wrapper to access resource properties.
type ResourceProperties map[string]interface{}

// GetString returns a string property or empty string.
func (p ResourceProperties) GetString(key string) string {
	if val, ok := p[key].(string); ok {
		return val
	}
	return ""
}

// GetInt returns an int property or 0.
func (p ResourceProperties) GetInt(key string) int {
	switch val := p[key].(type) {
	case int:
		return val
	case int64:
		return int(val)
	case float64:
		return int(val)
	}
	return 0
}

// GetBool returns a bool property or false.
func (p ResourceProperties) GetBool(key string) bool {
	if val, ok := p[key].(bool); ok {
		return val
	}
	return false
}
