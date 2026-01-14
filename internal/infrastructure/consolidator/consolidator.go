package consolidator

import (
	"context"
	"fmt"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	"github.com/homeport/homeport/internal/domain/stack"
)

// MergeOptions controls consolidation behavior.
// It allows customization of which stacks are enabled and how resources are mapped.
type MergeOptions struct {
	// EnabledStacks limits which stack types are processed.
	// If nil or empty, all stack types are enabled.
	EnabledStacks []stack.StackType

	// Mapping provides custom resource mapping rules.
	// If nil, the default mapping is used.
	Mapping *stack.ResourceStackMapping

	// DatabaseEngine specifies which database engine to use.
	// Options: "postgres", "mysql", "mariadb"
	// Default: "postgres"
	DatabaseEngine string

	// MessagingBroker specifies which messaging broker to use.
	// Options: "rabbitmq", "nats", "kafka"
	// Default: "rabbitmq"
	MessagingBroker string

	// NamePrefix is added to generated stack and service names.
	// Useful for distinguishing between environments.
	NamePrefix string

	// IncludeSupportServices determines whether to include support services
	// (e.g., Grafana for observability, pgBouncer for database).
	// Default: true
	IncludeSupportServices bool
}

// Consolidator groups cloud resources into logical stacks.
// It uses mergers to combine related resources and generates
// unified Docker Compose configurations.
type Consolidator struct {
	registry *Registry
	mapping  *stack.ResourceStackMapping
}

// New creates a new Consolidator with default settings.
func New() *Consolidator {
	return &Consolidator{
		registry: NewRegistry(),
		mapping:  stack.DefaultMapping(),
	}
}

// NewWithMapping creates a Consolidator with custom resource mapping.
func NewWithMapping(mapping *stack.ResourceStackMapping) *Consolidator {
	return &Consolidator{
		registry: NewRegistry(),
		mapping:  mapping,
	}
}

// NewWithRegistry creates a Consolidator with a custom registry.
func NewWithRegistry(registry *Registry) *Consolidator {
	return &Consolidator{
		registry: registry,
		mapping:  stack.DefaultMapping(),
	}
}

// RegisterMerger registers a merger for a stack type.
func (c *Consolidator) RegisterMerger(stackType stack.StackType, merger Merger) {
	c.registry.Register(merger)
}

// GetRegistry returns the consolidator's registry.
func (c *Consolidator) GetRegistry() *Registry {
	return c.registry
}

// SetMapping updates the resource mapping used by the consolidator.
func (c *Consolidator) SetMapping(mapping *stack.ResourceStackMapping) {
	c.mapping = mapping
}

// DefaultOptions returns sensible default merge options.
func DefaultOptions() *MergeOptions {
	return &MergeOptions{
		EnabledStacks:          nil, // All stacks enabled
		Mapping:                nil, // Use default mapping
		DatabaseEngine:         "postgres",
		MessagingBroker:        "rabbitmq",
		NamePrefix:             "",
		IncludeSupportServices: true,
	}
}

// Consolidate takes mapping results and groups them into consolidated stacks.
// It processes each result, determines its target stack type, and uses
// the appropriate merger to combine related resources.
func (c *Consolidator) Consolidate(ctx context.Context, results []*mapper.MappingResult, opts *MergeOptions) (*stack.ConsolidatedResult, error) {
	if opts == nil {
		opts = DefaultOptions()
	}

	// Use custom mapping if provided
	mapping := c.mapping
	if opts.Mapping != nil {
		mapping = opts.Mapping
	}

	// Initialize result
	consolidatedResult := stack.NewConsolidatedResult()

	// Group results by target stack type
	groupedResults := c.groupByStackType(results, mapping)

	// Process each stack type
	for stackType, stackResults := range groupedResults {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Skip disabled stack types
		if !c.isStackEnabled(stackType, opts.EnabledStacks) {
			continue
		}

		// Handle passthrough resources separately
		if stackType == stack.StackTypePassthrough {
			c.handlePassthroughResources(stackResults, consolidatedResult)
			continue
		}

		// Skip empty stacks
		if len(stackResults) == 0 {
			continue
		}

		// Get the merger for this stack type
		merger, hasMerger := c.registry.Get(stackType)

		var stk *stack.Stack
		var err error

		if hasMerger && merger.CanMerge(stackResults) {
			// Use registered merger
			stk, err = merger.Merge(ctx, stackResults, opts)
			if err != nil {
				consolidatedResult.AddWarning(fmt.Sprintf("Failed to merge %s stack: %v", stackType, err))
				// Fall back to default stack creation
				stk = c.createDefaultStackFromResults(stackType, stackResults, opts)
			}
		} else {
			// No merger registered, create default stack
			stk = c.createDefaultStackFromResults(stackType, stackResults, opts)
		}

		if stk != nil {
			consolidatedResult.AddStack(stk)
		}
	}

	// Calculate metadata
	consolidatedResult.CalculateMetadata()

	return consolidatedResult, nil
}

// groupByStackType groups mapping results by their target stack type.
func (c *Consolidator) groupByStackType(results []*mapper.MappingResult, mapping *stack.ResourceStackMapping) map[stack.StackType][]*mapper.MappingResult {
	grouped := make(map[stack.StackType][]*mapper.MappingResult)

	for _, result := range results {
		if result == nil {
			continue
		}

		// Determine stack type from resource
		var stackType stack.StackType

		// Try to get resource from result
		if result.SourceResource != nil {
			res := &resource.Resource{
				Type: resource.Type(result.SourceResourceType),
				Name: result.SourceResourceName,
			}
			stackType = mapping.GetStackType(res)
		} else if result.SourceResourceType != "" {
			// Use type string if resource not available
			stackType = mapping.GetStackTypeForString(result.SourceResourceType)
		} else {
			// Default to passthrough if we can't determine the type
			stackType = stack.StackTypePassthrough
		}

		grouped[stackType] = append(grouped[stackType], result)
	}

	return grouped
}

// isStackEnabled checks if a stack type is in the enabled list.
func (c *Consolidator) isStackEnabled(stackType stack.StackType, enabledStacks []stack.StackType) bool {
	if len(enabledStacks) == 0 {
		return true // All enabled if no filter
	}

	for _, enabled := range enabledStacks {
		if enabled == stackType {
			return true
		}
	}

	return false
}

// handlePassthroughResources adds passthrough resources to the result.
func (c *Consolidator) handlePassthroughResources(results []*mapper.MappingResult, consolidatedResult *stack.ConsolidatedResult) {
	for _, result := range results {
		if result == nil {
			continue
		}

		// Create a resource from the mapping result
		res := &resource.Resource{
			Type: resource.Type(result.SourceResourceType),
			Name: result.SourceResourceName,
		}

		consolidatedResult.AddPassthrough(res)

		// Also add warnings and manual steps
		for _, warning := range result.Warnings {
			consolidatedResult.AddWarning(warning)
		}
		for _, step := range result.ManualSteps {
			consolidatedResult.AddManualStep(step)
		}
	}
}

// createDefaultStackFromResults creates a stack from results when no merger is available.
func (c *Consolidator) createDefaultStackFromResults(stackType stack.StackType, results []*mapper.MappingResult, opts *MergeOptions) *stack.Stack {
	stk := CreateDefaultStack(stackType, opts.NamePrefix)

	// Add source resources
	for _, result := range results {
		if result != nil {
			res := &resource.Resource{
				Type: resource.Type(result.SourceResourceType),
				Name: result.SourceResourceName,
			}
			stk.AddSourceResource(res)

			// Collect warnings
			for _, warning := range result.Warnings {
				// Add to stack metadata or to a warnings field if it exists
				stk.Metadata["warning_"+result.SourceResourceName] = warning
			}
		}
	}

	// Merge configs from all results
	for _, result := range results {
		for name, content := range result.Configs {
			stk.AddConfig(name, content)
		}
		for name, content := range result.Scripts {
			stk.AddScript(name, content)
		}
	}

	// Extract and add volumes
	volumes := ExtractVolumes(results)
	for _, vol := range volumes {
		stk.AddVolume(vol)
	}

	// Add networks from results
	networks := ExtractNetworks(results)
	for _, network := range networks {
		stk.AddNetwork(stack.Network{Name: network})
	}

	return stk
}

// ConsolidateWithDefaults is a convenience function that uses default options.
func (c *Consolidator) ConsolidateWithDefaults(ctx context.Context, results []*mapper.MappingResult) (*stack.ConsolidatedResult, error) {
	return c.Consolidate(ctx, results, DefaultOptions())
}

// ValidateResults checks that mapping results can be consolidated.
// Returns a list of validation errors.
func ValidateResults(results []*mapper.MappingResult) []error {
	var errors []error

	for i, result := range results {
		if result == nil {
			errors = append(errors, fmt.Errorf("result at index %d is nil", i))
			continue
		}

		if result.DockerService == nil {
			errors = append(errors, fmt.Errorf("result %d has no Docker service", i))
		} else if result.DockerService.Name == "" {
			errors = append(errors, fmt.Errorf("result %d Docker service has no name", i))
		}
	}

	return errors
}

// FilterResultsByStackType returns only results that map to the given stack type.
func FilterResultsByStackType(results []*mapper.MappingResult, stackType stack.StackType, mapping *stack.ResourceStackMapping) []*mapper.MappingResult {
	var filtered []*mapper.MappingResult

	if mapping == nil {
		mapping = stack.DefaultMapping()
	}

	for _, result := range results {
		if result == nil {
			continue
		}

		resultStackType := mapping.GetStackTypeForString(result.SourceResourceType)
		if resultStackType == stackType {
			filtered = append(filtered, result)
		}
	}

	return filtered
}

// GetStackTypesFromResults returns all unique stack types present in the results.
func GetStackTypesFromResults(results []*mapper.MappingResult, mapping *stack.ResourceStackMapping) []stack.StackType {
	if mapping == nil {
		mapping = stack.DefaultMapping()
	}

	typeSet := make(map[stack.StackType]bool)

	for _, result := range results {
		if result == nil {
			continue
		}

		stackType := mapping.GetStackTypeForString(result.SourceResourceType)
		typeSet[stackType] = true
	}

	types := make([]stack.StackType, 0, len(typeSet))
	for t := range typeSet {
		types = append(types, t)
	}

	return types
}
