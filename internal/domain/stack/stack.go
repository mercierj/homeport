package stack

import (
	"github.com/homeport/homeport/internal/domain/resource"
)

// Service represents a container service within a stack.
// Each service maps to a Docker Compose service definition.
type Service struct {
	// Name is the service name (used in docker-compose.yml)
	Name string

	// Image is the Docker image to use
	Image string

	// Environment contains environment variables for the container
	Environment map[string]string

	// Ports lists port mappings in "host:container" format
	Ports []string

	// Volumes lists volume mounts in "source:target" format
	Volumes []string

	// Command overrides the container's default command
	Command []string

	// DependsOn lists service names this service depends on
	DependsOn []string

	// Labels are Docker labels for the container
	Labels map[string]string

	// Networks lists network names the service should join
	Networks []string

	// HealthCheck defines the health check configuration
	HealthCheck *HealthCheck

	// Restart policy (e.g., "always", "unless-stopped", "on-failure")
	Restart string

	// Deploy contains deployment configuration (replicas, resources, etc.)
	Deploy *DeployConfig
}

// HealthCheck defines health check configuration for a service.
type HealthCheck struct {
	// Test is the health check command
	Test []string

	// Interval between health checks (e.g., "30s")
	Interval string

	// Timeout for each health check (e.g., "10s")
	Timeout string

	// Retries before marking unhealthy
	Retries int

	// StartPeriod is the initialization time before health checks start
	StartPeriod string
}

// DeployConfig contains deployment-specific configuration.
type DeployConfig struct {
	// Replicas is the number of container instances
	Replicas int

	// Resources defines resource limits and reservations
	Resources *ResourceConfig

	// RestartPolicy defines restart behavior
	RestartPolicy *RestartPolicy
}

// ResourceConfig defines resource limits and reservations.
type ResourceConfig struct {
	// Limits are the maximum resources the container can use
	Limits *ResourceSpec

	// Reservations are the guaranteed minimum resources
	Reservations *ResourceSpec
}

// ResourceSpec defines specific resource values.
type ResourceSpec struct {
	// CPUs is the CPU limit (e.g., "0.5" for half a CPU)
	CPUs string

	// Memory is the memory limit (e.g., "512M", "1G")
	Memory string
}

// RestartPolicy defines restart behavior for a service.
type RestartPolicy struct {
	// Condition is when to restart (e.g., "on-failure", "any", "none")
	Condition string

	// Delay between restart attempts
	Delay string

	// MaxAttempts is the maximum number of restart attempts
	MaxAttempts int

	// Window is the time window for restart policy evaluation
	Window string
}

// Volume represents a named volume in Docker Compose.
type Volume struct {
	// Name is the volume name
	Name string

	// Driver is the volume driver (e.g., "local", "nfs")
	Driver string

	// DriverOpts are driver-specific options
	DriverOpts map[string]string

	// External indicates if the volume is managed externally
	External bool

	// Labels are metadata for the volume
	Labels map[string]string
}

// Network represents a network configuration for Docker Compose.
type Network struct {
	// Name is the network name
	Name string

	// Driver is the network driver (e.g., "bridge", "overlay")
	Driver string

	// External indicates if the network is managed externally
	External bool

	// Attachable allows manual container attachment (for overlay networks)
	Attachable bool

	// IPAM contains IP address management configuration
	IPAM *IPAMConfig
}

// IPAMConfig contains IP address management configuration.
type IPAMConfig struct {
	// Driver is the IPAM driver
	Driver string

	// Config contains subnet configurations
	Config []IPAMPoolConfig
}

// IPAMPoolConfig contains subnet configuration.
type IPAMPoolConfig struct {
	// Subnet is the subnet in CIDR notation
	Subnet string

	// Gateway is the gateway IP address
	Gateway string
}

// Stack represents a consolidated logical stack.
// A stack groups related cloud resources into a single deployable unit.
type Stack struct {
	// Type identifies the kind of stack (database, cache, messaging, etc.)
	Type StackType

	// Name is the unique name for this stack instance
	Name string

	// Description provides context about what this stack contains
	Description string

	// Services are the Docker services that make up this stack
	Services []*Service

	// Configs maps filename to file content for configuration files
	Configs map[string][]byte

	// Scripts maps filename to file content for helper scripts
	Scripts map[string][]byte

	// Volumes are named volumes used by the stack
	Volumes []Volume

	// Networks are networks used by the stack
	Networks []Network

	// DependsOn lists stack types this stack depends on
	DependsOn []StackType

	// SourceResources are the original cloud resources that were consolidated
	SourceResources []*resource.Resource

	// Metadata contains additional stack metadata
	Metadata map[string]string
}

// ConsolidationMetadata contains statistics about the consolidation process.
type ConsolidationMetadata struct {
	// TotalSourceResources is the count of original cloud resources
	TotalSourceResources int

	// TotalStacks is the count of generated stacks
	TotalStacks int

	// TotalServices is the count of Docker services generated
	TotalServices int

	// ConsolidationRatio is resources/services (higher = more consolidation)
	ConsolidationRatio float64

	// ByProvider maps provider name to resource count
	ByProvider map[string]int

	// ByStackType maps stack type to resource count
	ByStackType map[StackType]int

	// ByCategory maps resource category to count
	ByCategory map[string]int
}

// ConsolidatedResult is the output of the consolidation process.
// It contains all stacks, passthrough resources, and metadata.
type ConsolidatedResult struct {
	// Stacks are the consolidated logical stacks
	Stacks []*Stack

	// Passthrough are resources that don't consolidate (VMs, K8s clusters, etc.)
	Passthrough []*resource.Resource

	// Metadata contains statistics about the consolidation
	Metadata *ConsolidationMetadata

	// Warnings are non-fatal issues encountered during consolidation
	Warnings []string

	// ManualSteps are actions that require manual intervention
	ManualSteps []string
}

// NewStack creates a new stack with the given type and name.
// Initializes all maps and slices to avoid nil pointer issues.
func NewStack(stackType StackType, name string) *Stack {
	return &Stack{
		Type:            stackType,
		Name:            name,
		Services:        make([]*Service, 0),
		Configs:         make(map[string][]byte),
		Scripts:         make(map[string][]byte),
		Volumes:         make([]Volume, 0),
		Networks:        make([]Network, 0),
		DependsOn:       make([]StackType, 0),
		SourceResources: make([]*resource.Resource, 0),
		Metadata:        make(map[string]string),
	}
}

// NewService creates a new service with the given name and image.
// Initializes all maps and slices to avoid nil pointer issues.
func NewService(name, image string) *Service {
	return &Service{
		Name:        name,
		Image:       image,
		Environment: make(map[string]string),
		Ports:       make([]string, 0),
		Volumes:     make([]string, 0),
		Command:     make([]string, 0),
		DependsOn:   make([]string, 0),
		Labels:      make(map[string]string),
		Networks:    make([]string, 0),
		Restart:     "unless-stopped",
	}
}

// NewConsolidatedResult creates a new result with initialized fields.
func NewConsolidatedResult() *ConsolidatedResult {
	return &ConsolidatedResult{
		Stacks:      make([]*Stack, 0),
		Passthrough: make([]*resource.Resource, 0),
		Metadata: &ConsolidationMetadata{
			ByProvider:  make(map[string]int),
			ByStackType: make(map[StackType]int),
			ByCategory:  make(map[string]int),
		},
		Warnings:    make([]string, 0),
		ManualSteps: make([]string, 0),
	}
}

// AddService adds a service to the stack.
func (s *Stack) AddService(svc *Service) {
	if s.Services == nil {
		s.Services = make([]*Service, 0)
	}
	s.Services = append(s.Services, svc)
}

// AddConfig adds a configuration file to the stack.
func (s *Stack) AddConfig(name string, content []byte) {
	if s.Configs == nil {
		s.Configs = make(map[string][]byte)
	}
	s.Configs[name] = content
}

// AddScript adds a script file to the stack.
func (s *Stack) AddScript(name string, content []byte) {
	if s.Scripts == nil {
		s.Scripts = make(map[string][]byte)
	}
	s.Scripts[name] = content
}

// AddVolume adds a volume to the stack.
func (s *Stack) AddVolume(vol Volume) {
	if s.Volumes == nil {
		s.Volumes = make([]Volume, 0)
	}
	s.Volumes = append(s.Volumes, vol)
}

// AddNetwork adds a network to the stack.
func (s *Stack) AddNetwork(net Network) {
	if s.Networks == nil {
		s.Networks = make([]Network, 0)
	}
	s.Networks = append(s.Networks, net)
}

// AddDependency adds a stack type dependency.
func (s *Stack) AddDependency(dep StackType) {
	if s.DependsOn == nil {
		s.DependsOn = make([]StackType, 0)
	}
	// Avoid duplicates
	for _, existing := range s.DependsOn {
		if existing == dep {
			return
		}
	}
	s.DependsOn = append(s.DependsOn, dep)
}

// AddSourceResource records an original cloud resource that was consolidated.
func (s *Stack) AddSourceResource(res *resource.Resource) {
	if s.SourceResources == nil {
		s.SourceResources = make([]*resource.Resource, 0)
	}
	s.SourceResources = append(s.SourceResources, res)
}

// ServiceCount returns the number of services in the stack.
func (s *Stack) ServiceCount() int {
	return len(s.Services)
}

// SourceResourceCount returns the number of source resources.
func (s *Stack) SourceResourceCount() int {
	return len(s.SourceResources)
}

// TotalServices returns the total number of services across all stacks.
func (r *ConsolidatedResult) TotalServices() int {
	total := 0
	for _, stack := range r.Stacks {
		total += stack.ServiceCount()
	}
	return total
}

// TotalStacks returns the number of stacks in the result.
func (r *ConsolidatedResult) TotalStacks() int {
	return len(r.Stacks)
}

// AddStack adds a stack to the result.
func (r *ConsolidatedResult) AddStack(stack *Stack) {
	if r.Stacks == nil {
		r.Stacks = make([]*Stack, 0)
	}
	r.Stacks = append(r.Stacks, stack)
}

// AddPassthrough adds a passthrough resource to the result.
func (r *ConsolidatedResult) AddPassthrough(res *resource.Resource) {
	if r.Passthrough == nil {
		r.Passthrough = make([]*resource.Resource, 0)
	}
	r.Passthrough = append(r.Passthrough, res)
}

// AddWarning adds a warning message to the result.
func (r *ConsolidatedResult) AddWarning(msg string) {
	if r.Warnings == nil {
		r.Warnings = make([]string, 0)
	}
	r.Warnings = append(r.Warnings, msg)
}

// AddManualStep adds a manual step to the result.
func (r *ConsolidatedResult) AddManualStep(msg string) {
	if r.ManualSteps == nil {
		r.ManualSteps = make([]string, 0)
	}
	r.ManualSteps = append(r.ManualSteps, msg)
}

// GetStackByType returns the first stack of the given type, or nil if not found.
func (r *ConsolidatedResult) GetStackByType(stackType StackType) *Stack {
	for _, stack := range r.Stacks {
		if stack.Type == stackType {
			return stack
		}
	}
	return nil
}

// GetStacksByType returns all stacks of the given type.
func (r *ConsolidatedResult) GetStacksByType(stackType StackType) []*Stack {
	var result []*Stack
	for _, stack := range r.Stacks {
		if stack.Type == stackType {
			result = append(result, stack)
		}
	}
	return result
}

// HasWarnings returns true if there are any warnings.
func (r *ConsolidatedResult) HasWarnings() bool {
	return len(r.Warnings) > 0
}

// HasManualSteps returns true if there are any manual steps required.
func (r *ConsolidatedResult) HasManualSteps() bool {
	return len(r.ManualSteps) > 0
}

// CalculateMetadata computes consolidation statistics from the result.
func (r *ConsolidatedResult) CalculateMetadata() {
	if r.Metadata == nil {
		r.Metadata = &ConsolidationMetadata{
			ByProvider:  make(map[string]int),
			ByStackType: make(map[StackType]int),
			ByCategory:  make(map[string]int),
		}
	}

	totalResources := 0
	totalServices := 0

	// Count resources and services from stacks
	for _, stack := range r.Stacks {
		totalServices += stack.ServiceCount()
		r.Metadata.ByStackType[stack.Type] += stack.SourceResourceCount()

		for _, res := range stack.SourceResources {
			totalResources++
			provider := res.Type.Provider().String()
			r.Metadata.ByProvider[provider]++
			category := res.Type.GetCategory().String()
			r.Metadata.ByCategory[category]++
		}
	}

	// Count passthrough resources
	for _, res := range r.Passthrough {
		totalResources++
		totalServices++ // Each passthrough is its own service
		r.Metadata.ByStackType[StackTypePassthrough]++
		provider := res.Type.Provider().String()
		r.Metadata.ByProvider[provider]++
		category := res.Type.GetCategory().String()
		r.Metadata.ByCategory[category]++
	}

	r.Metadata.TotalSourceResources = totalResources
	r.Metadata.TotalStacks = len(r.Stacks)
	r.Metadata.TotalServices = totalServices

	if totalServices > 0 {
		r.Metadata.ConsolidationRatio = float64(totalResources) / float64(totalServices)
	}
}
