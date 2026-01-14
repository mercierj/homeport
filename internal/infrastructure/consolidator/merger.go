// Package consolidator provides infrastructure for consolidating cloud resources into logical stacks.
// It implements the stack consolidation feature by grouping related cloud resources
// and generating unified Docker Compose configurations.
package consolidator

import (
	"context"
	"fmt"
	"strings"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/stack"
)

// Merger is the interface for stack-specific consolidation logic.
// Each implementation handles a specific stack type (database, cache, messaging, etc.)
// and knows how to combine multiple cloud resources into a unified stack.
type Merger interface {
	// StackType returns which stack type this merger handles.
	StackType() stack.StackType

	// CanMerge checks if this merger can handle the given results.
	// Returns true if the merger can process the provided mapping results.
	CanMerge(results []*mapper.MappingResult) bool

	// Merge consolidates multiple mapping results into a single stack.
	// It combines services, configurations, volumes, and other resources
	// from multiple cloud resources into a unified stack.
	Merge(ctx context.Context, results []*mapper.MappingResult, opts *MergeOptions) (*stack.Stack, error)
}

// BaseMerger provides common functionality for all mergers.
// It implements basic methods and helper functions that are shared
// across different merger implementations.
type BaseMerger struct {
	stackType stack.StackType
}

// NewBaseMerger creates a new BaseMerger for the given stack type.
func NewBaseMerger(stackType stack.StackType) *BaseMerger {
	return &BaseMerger{
		stackType: stackType,
	}
}

// StackType returns which stack type this merger handles.
func (b *BaseMerger) StackType() stack.StackType {
	return b.stackType
}

// CanMerge returns true if there are results to merge.
// Override this method in specific mergers for more complex validation.
func (b *BaseMerger) CanMerge(results []*mapper.MappingResult) bool {
	return len(results) > 0
}

// ExtractEnvironmentVars extracts and merges environment variables from multiple mapping results.
// It collects all environment variables from each result's Docker service
// and returns a unified map.
func ExtractEnvironmentVars(results []*mapper.MappingResult) map[string]string {
	env := make(map[string]string)

	for _, result := range results {
		if result.DockerService != nil && result.DockerService.Environment != nil {
			for k, v := range result.DockerService.Environment {
				env[k] = v
			}
		}
	}

	return env
}

// ExtractVolumes extracts all volumes from multiple mapping results.
// It deduplicates volumes by name and returns a unified list.
func ExtractVolumes(results []*mapper.MappingResult) []stack.Volume {
	volumeMap := make(map[string]stack.Volume)

	for _, result := range results {
		for _, vol := range result.Volumes {
			// Convert mapper.Volume to stack.Volume
			stackVol := stack.Volume{
				Name:       vol.Name,
				Driver:     vol.Driver,
				DriverOpts: vol.DriverOpts,
				Labels:     vol.Labels,
			}
			volumeMap[vol.Name] = stackVol
		}
	}

	volumes := make([]stack.Volume, 0, len(volumeMap))
	for _, vol := range volumeMap {
		volumes = append(volumes, vol)
	}

	return volumes
}

// ExtractLabels extracts and merges labels from multiple mapping results.
// It collects all labels from each result's Docker service
// and returns a unified map.
func ExtractLabels(results []*mapper.MappingResult) map[string]string {
	labels := make(map[string]string)

	for _, result := range results {
		if result.DockerService != nil && result.DockerService.Labels != nil {
			for k, v := range result.DockerService.Labels {
				labels[k] = v
			}
		}
	}

	return labels
}

// GenerateUniqueName generates a unique name based on a base string.
// If the base name already exists in the existing slice,
// it appends a numeric suffix to make it unique.
func GenerateUniqueName(base string, existing []string) string {
	existingSet := make(map[string]bool)
	for _, name := range existing {
		existingSet[name] = true
	}

	if !existingSet[base] {
		return base
	}

	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if !existingSet[candidate] {
			return candidate
		}
	}
}

// MergeEnvironments merges multiple environment variable maps into one.
// Later maps take precedence over earlier ones for duplicate keys.
func MergeEnvironments(envs ...map[string]string) map[string]string {
	result := make(map[string]string)

	for _, env := range envs {
		for k, v := range env {
			result[k] = v
		}
	}

	return result
}

// ExtractNetworks extracts all unique network names from multiple mapping results.
func ExtractNetworks(results []*mapper.MappingResult) []string {
	networkSet := make(map[string]bool)

	for _, result := range results {
		for _, network := range result.Networks {
			networkSet[network] = true
		}
		if result.DockerService != nil {
			for _, network := range result.DockerService.Networks {
				networkSet[network] = true
			}
		}
	}

	networks := make([]string, 0, len(networkSet))
	for network := range networkSet {
		networks = append(networks, network)
	}

	return networks
}

// ExtractWarnings collects all warnings from multiple mapping results.
func ExtractWarnings(results []*mapper.MappingResult) []string {
	var warnings []string

	for _, result := range results {
		warnings = append(warnings, result.Warnings...)
	}

	return warnings
}

// ExtractManualSteps collects all manual steps from multiple mapping results.
func ExtractManualSteps(results []*mapper.MappingResult) []string {
	var steps []string

	for _, result := range results {
		steps = append(steps, result.ManualSteps...)
	}

	return steps
}

// ExtractConfigs extracts all configuration files from multiple mapping results.
// If multiple results have the same config filename, the last one wins.
func ExtractConfigs(results []*mapper.MappingResult) map[string][]byte {
	configs := make(map[string][]byte)

	for _, result := range results {
		for name, content := range result.Configs {
			configs[name] = content
		}
	}

	return configs
}

// ExtractScripts extracts all script files from multiple mapping results.
// If multiple results have the same script filename, the last one wins.
func ExtractScripts(results []*mapper.MappingResult) map[string][]byte {
	scripts := make(map[string][]byte)

	for _, result := range results {
		for name, content := range result.Scripts {
			scripts[name] = content
		}
	}

	return scripts
}

// NormalizeName converts a string to a valid Docker service name.
// It lowercases, replaces invalid characters with hyphens,
// and removes leading/trailing hyphens.
func NormalizeName(name string) string {
	// Convert to lowercase
	name = strings.ToLower(name)

	// Replace invalid characters with hyphens
	var result strings.Builder
	for _, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			result.WriteRune(c)
		} else if c == '_' || c == ' ' || c == '.' {
			result.WriteRune('-')
		}
	}

	// Remove leading/trailing hyphens and collapse multiple hyphens
	normalized := result.String()
	normalized = strings.Trim(normalized, "-")

	// Collapse multiple consecutive hyphens into one
	for strings.Contains(normalized, "--") {
		normalized = strings.ReplaceAll(normalized, "--", "-")
	}

	return normalized
}

// ConvertMapperServiceToStackService converts a mapper.DockerService to a stack.Service.
func ConvertMapperServiceToStackService(ds *mapper.DockerService) *stack.Service {
	if ds == nil {
		return nil
	}

	svc := stack.NewService(ds.Name, ds.Image)
	svc.Ports = ds.Ports
	svc.Volumes = ds.Volumes
	svc.Command = ds.Command
	svc.DependsOn = ds.DependsOn
	svc.Networks = ds.Networks
	svc.Restart = ds.Restart

	// Copy environment
	for k, v := range ds.Environment {
		svc.Environment[k] = v
	}

	// Copy labels
	for k, v := range ds.Labels {
		svc.Labels[k] = v
	}

	// Convert health check
	if ds.HealthCheck != nil {
		svc.HealthCheck = &stack.HealthCheck{
			Test:        ds.HealthCheck.Test,
			Interval:    ds.HealthCheck.Interval.String(),
			Timeout:     ds.HealthCheck.Timeout.String(),
			Retries:     ds.HealthCheck.Retries,
			StartPeriod: ds.HealthCheck.StartPeriod.String(),
		}
	}

	// Convert deploy config
	if ds.Deploy != nil {
		svc.Deploy = &stack.DeployConfig{
			Replicas: ds.Deploy.Replicas,
		}
		if ds.Deploy.Resources != nil {
			svc.Deploy.Resources = &stack.ResourceConfig{}
			if ds.Deploy.Resources.Limits != nil {
				svc.Deploy.Resources.Limits = &stack.ResourceSpec{
					CPUs:   ds.Deploy.Resources.Limits.CPUs,
					Memory: ds.Deploy.Resources.Limits.Memory,
				}
			}
			if ds.Deploy.Resources.Reservations != nil {
				svc.Deploy.Resources.Reservations = &stack.ResourceSpec{
					CPUs:   ds.Deploy.Resources.Reservations.CPUs,
					Memory: ds.Deploy.Resources.Reservations.Memory,
				}
			}
		}
		if ds.Deploy.RestartPolicy != nil {
			svc.Deploy.RestartPolicy = &stack.RestartPolicy{
				Condition:   ds.Deploy.RestartPolicy.Condition,
				Delay:       ds.Deploy.RestartPolicy.Delay.String(),
				MaxAttempts: ds.Deploy.RestartPolicy.MaxAttempts,
				Window:      ds.Deploy.RestartPolicy.Window.String(),
			}
		}
	}

	return svc
}
