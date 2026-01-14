package mapper

import (
	"time"

	"github.com/homeport/homeport/internal/domain/policy"
	"github.com/homeport/homeport/internal/domain/resource"
)

// MappingResult represents the outcome of mapping a cloud resource to self-hosted equivalents.
// It contains everything needed to deploy and run the mapped service.
type MappingResult struct {
	// DockerService defines the main Docker service configuration
	DockerService *DockerService

	// AdditionalServices contains additional Docker services needed (sidecars, databases, etc.)
	AdditionalServices []*DockerService

	// Configs contains configuration files needed by the service (filename -> content)
	Configs map[string][]byte

	// Scripts contains shell scripts for initialization, migration, etc. (filename -> content)
	Scripts map[string][]byte

	// Volumes defines Docker volumes needed by the service
	Volumes []Volume

	// Networks defines Docker networks the service should be connected to
	Networks []string

	// Warnings contains non-critical issues or recommendations
	Warnings []string

	// ManualSteps contains steps that require manual intervention
	ManualSteps []string

	// Policies contains IAM and resource policies extracted from the source
	Policies []*policy.Policy

	// Source resource information
	SourceResource     *resource.AWSResource
	SourceResourceType string
	SourceResourceName string
	SourceCategory     resource.Category
}

// DockerService represents a Docker Compose service definition.
type DockerService struct {
	// Name is the name of the Docker service
	Name string

	// Image is the Docker image to use
	Image string

	// Build is the build context for the service (optional)
	Build *DockerBuild

	// Ports are the port mappings (host:container)
	Ports []string

	// Volumes are the volume mounts (source:target or named-volume:target)
	Volumes []string

	// Environment contains environment variables (key=value)
	Environment map[string]string

	// Labels are Docker labels for metadata and service discovery
	Labels map[string]string

	// DependsOn lists other services this service depends on
	DependsOn []string

	// Networks lists the networks this service should be connected to
	Networks []string

	// HealthCheck defines the health check configuration
	HealthCheck *HealthCheck

	// Deploy contains deployment configuration (replicas, resources, etc.)
	Deploy *DeployConfig

	// Command overrides the default container command
	Command []string

	// Restart defines the restart policy (always, unless-stopped, on-failure, no)
	Restart string

	// CapAdd lists Linux capabilities to add
	CapAdd []string

	// CapDrop lists Linux capabilities to drop
	CapDrop []string

	// User specifies the user to run the container as
	User string

	// WorkingDir sets the working directory
	WorkingDir string

	// ExtraHosts adds custom host-to-IP mappings
	ExtraHosts []string

	// Sysctls sets kernel parameters
	Sysctls map[string]string

	// Ulimits sets resource limits
	Ulimits map[string]Ulimit
}

// DockerBuild defines Docker build configuration.
type DockerBuild struct {
	// Context is the build context path
	Context string

	// Dockerfile is the path to the Dockerfile
	Dockerfile string

	// Args are build arguments
	Args map[string]string

	// Target is the build target stage
	Target string
}

// HealthCheck defines a Docker health check configuration.
type HealthCheck struct {
	// Test is the command to run for health check
	Test []string

	// Interval is how often to run the health check
	Interval time.Duration

	// Timeout is the maximum time to wait for health check
	Timeout time.Duration

	// Retries is the number of consecutive failures needed to mark container as unhealthy
	Retries int

	// StartPeriod is the initialization time before health checks count
	StartPeriod time.Duration
}

// DeployConfig defines Docker Swarm/Compose deployment configuration.
type DeployConfig struct {
	// Replicas is the number of service replicas
	Replicas int

	// Resources defines resource constraints
	Resources *Resources

	// RestartPolicy defines how to restart containers
	RestartPolicy *RestartPolicy

	// UpdateConfig defines how to update the service
	UpdateConfig *UpdateConfig

	// Placement defines where to deploy containers
	Placement *Placement
}

// Resources defines resource requirements and limits.
type Resources struct {
	// Limits defines maximum resources
	Limits *ResourceLimits

	// Reservations defines minimum guaranteed resources
	Reservations *ResourceLimits
}

// ResourceLimits defines resource limits.
type ResourceLimits struct {
	// CPUs is the CPU limit (e.g., "1.5" for 1.5 cores)
	CPUs string

	// Memory is the memory limit (e.g., "512M", "2G")
	Memory string
}

// RestartPolicy defines how containers should be restarted.
type RestartPolicy struct {
	// Condition is when to restart (none, on-failure, any)
	Condition string

	// Delay is the time between restart attempts
	Delay time.Duration

	// MaxAttempts is the maximum number of restart attempts
	MaxAttempts int

	// Window is the time window to evaluate restart attempts
	Window time.Duration
}

// UpdateConfig defines how to update a service.
type UpdateConfig struct {
	// Parallelism is the number of containers to update at once
	Parallelism int

	// Delay is the time between update batches
	Delay time.Duration

	// FailureAction is what to do if update fails (pause, continue, rollback)
	FailureAction string

	// Order is the update order (start-first, stop-first)
	Order string
}

// Placement defines deployment placement constraints.
type Placement struct {
	// Constraints are placement constraints (e.g., "node.role==worker")
	Constraints []string

	// Preferences are placement preferences
	Preferences []string
}

// Volume represents a Docker volume configuration.
type Volume struct {
	// Name is the volume name
	Name string

	// Driver is the volume driver (local, nfs, etc.)
	Driver string

	// DriverOpts are driver-specific options
	DriverOpts map[string]string

	// Labels are volume labels
	Labels map[string]string
}

// Ulimit represents a resource limit.
type Ulimit struct {
	// Soft is the soft limit
	Soft int

	// Hard is the hard limit
	Hard int
}

// NewMappingResult creates a new mapping result with initialized maps.
func NewMappingResult(serviceName string) *MappingResult {
	return &MappingResult{
		DockerService: &DockerService{
			Name:        serviceName,
			Environment: make(map[string]string),
			Labels:      make(map[string]string),
			Sysctls:     make(map[string]string),
			Ulimits:     make(map[string]Ulimit),
			Restart:     "unless-stopped",
		},
		AdditionalServices: make([]*DockerService, 0),
		Configs:            make(map[string][]byte),
		Scripts:            make(map[string][]byte),
		Volumes:            make([]Volume, 0),
		Networks:           make([]string, 0),
		Warnings:           make([]string, 0),
		ManualSteps:        make([]string, 0),
		Policies:           make([]*policy.Policy, 0),
	}
}

// NewDockerService creates a new Docker service with initialized maps.
func NewDockerService(name string) *DockerService {
	return &DockerService{
		Name:        name,
		Environment: make(map[string]string),
		Labels:      make(map[string]string),
		Sysctls:     make(map[string]string),
		Ulimits:     make(map[string]Ulimit),
		Restart:     "unless-stopped",
	}
}

// AddWarning adds a warning message to the result.
func (r *MappingResult) AddWarning(warning string) {
	r.Warnings = append(r.Warnings, warning)
}

// AddManualStep adds a manual step to the result.
func (r *MappingResult) AddManualStep(step string) {
	r.ManualSteps = append(r.ManualSteps, step)
}

// AddConfig adds a configuration file to the result.
func (r *MappingResult) AddConfig(filename string, content []byte) {
	r.Configs[filename] = content
}

// AddScript adds a script file to the result.
func (r *MappingResult) AddScript(filename string, content []byte) {
	r.Scripts[filename] = content
}

// AddVolume adds a volume to the result.
func (r *MappingResult) AddVolume(volume Volume) {
	r.Volumes = append(r.Volumes, volume)
}

// AddNetwork adds a network to the result.
func (r *MappingResult) AddNetwork(network string) {
	r.Networks = append(r.Networks, network)
}

// AddService adds an additional Docker service to the result.
func (r *MappingResult) AddService(service *DockerService) {
	r.AdditionalServices = append(r.AdditionalServices, service)
}

// HasWarnings returns true if there are any warnings.
func (r *MappingResult) HasWarnings() bool {
	return len(r.Warnings) > 0
}

// HasManualSteps returns true if there are any manual steps.
func (r *MappingResult) HasManualSteps() bool {
	return len(r.ManualSteps) > 0
}

// AddPolicy adds a policy to the result.
func (r *MappingResult) AddPolicy(p *policy.Policy) {
	r.Policies = append(r.Policies, p)
}

// HasPolicies returns true if there are any policies.
func (r *MappingResult) HasPolicies() bool {
	return len(r.Policies) > 0
}
