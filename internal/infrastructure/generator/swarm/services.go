package swarm

// SwarmService represents a Docker Swarm service with deploy configurations.
type SwarmService struct {
	// Name is the service name
	Name string

	// Image is the Docker image
	Image string

	// Build is the build context (if building locally)
	Build string

	// Environment contains environment variables
	Environment map[string]string

	// Ports are the port mappings
	Ports []string

	// Volumes are volume mounts
	Volumes []string

	// Networks are the networks to connect to
	Networks []string

	// DependsOn lists service dependencies
	DependsOn []string

	// Command overrides the default command
	Command []string

	// Labels are service labels
	Labels map[string]string

	// Deploy contains Swarm-specific deploy configuration
	Deploy *DeployConfig

	// HealthCheck defines health check configuration
	HealthCheck *HealthCheck

	// Secrets lists secrets to mount
	Secrets []SecretRef

	// Configs lists configs to mount
	Configs []ConfigRef

	// User specifies the user to run as
	User string

	// WorkingDir sets the working directory
	WorkingDir string

	// StopGracePeriod is the time to wait before killing the container
	StopGracePeriod string

	// Logging configures logging driver
	Logging *LoggingConfig
}

// DeployConfig contains Docker Swarm deployment configuration.
type DeployConfig struct {
	// Replicas is the number of service instances
	Replicas int

	// Mode is the service mode (replicated or global)
	Mode string

	// UpdateConfig defines rolling update behavior
	UpdateConfig *UpdateConfig

	// RollbackConfig defines rollback behavior
	RollbackConfig *RollbackConfig

	// RestartPolicy defines restart behavior
	RestartPolicy *RestartPolicy

	// Placement defines where to place containers
	Placement *Placement

	// Resources defines resource constraints
	Resources *Resources

	// Labels are deploy-level labels
	Labels map[string]string

	// EndpointMode is the endpoint mode (vip or dnsrr)
	EndpointMode string
}

// UpdateConfig defines how to update the service during rolling updates.
type UpdateConfig struct {
	// Parallelism is the number of containers to update at once
	Parallelism int

	// Delay is the time between update batches
	Delay string

	// FailureAction is what to do on failure (pause, continue, rollback)
	FailureAction string

	// Monitor is how long to monitor after update
	Monitor string

	// MaxFailureRatio is the maximum failure ratio to tolerate
	MaxFailureRatio float64

	// Order is the update order (start-first, stop-first)
	Order string
}

// RollbackConfig defines how to rollback the service.
type RollbackConfig struct {
	// Parallelism is the number of containers to rollback at once
	Parallelism int

	// Delay is the time between rollback batches
	Delay string

	// FailureAction is what to do on failure (pause, continue)
	FailureAction string

	// Monitor is how long to monitor after rollback
	Monitor string

	// MaxFailureRatio is the maximum failure ratio to tolerate
	MaxFailureRatio float64

	// Order is the rollback order (start-first, stop-first)
	Order string
}

// RestartPolicy defines how containers should be restarted.
type RestartPolicy struct {
	// Condition is when to restart (none, on-failure, any)
	Condition string

	// Delay is the time between restart attempts
	Delay string

	// MaxAttempts is the maximum restart attempts (0 = unlimited)
	MaxAttempts int

	// Window is the time window for restart attempts
	Window string
}

// Placement defines where to deploy containers.
type Placement struct {
	// Constraints are placement constraints
	Constraints []string

	// Preferences are placement preferences
	Preferences []PlacementPreference

	// MaxReplicas limits replicas per node
	MaxReplicas int
}

// PlacementPreference defines a placement preference.
type PlacementPreference struct {
	// Spread spreads replicas across the given field
	Spread string
}

// Resources defines resource constraints.
type Resources struct {
	// Limits are maximum resources
	Limits *ResourceLimits

	// Reservations are guaranteed minimum resources
	Reservations *ResourceLimits
}

// ResourceLimits defines CPU and memory limits.
type ResourceLimits struct {
	// CPUs is the CPU limit (e.g., "0.5", "2")
	CPUs string

	// Memory is the memory limit (e.g., "512M", "2G")
	Memory string

	// Pids is the process limit
	Pids int
}

// HealthCheck defines health check configuration.
type HealthCheck struct {
	// Test is the command to run
	Test []string

	// Interval is how often to check
	Interval string

	// Timeout is the check timeout
	Timeout string

	// Retries is the number of retries before unhealthy
	Retries int

	// StartPeriod is the initialization period
	StartPeriod string

	// Disable disables the health check
	Disable bool
}

// SecretRef references a Docker secret.
type SecretRef struct {
	// Source is the secret name
	Source string

	// Target is the path in the container
	Target string

	// UID is the user ID
	UID string

	// GID is the group ID
	GID string

	// Mode is the file mode
	Mode uint32
}

// ConfigRef references a Docker config.
type ConfigRef struct {
	// Source is the config name
	Source string

	// Target is the path in the container
	Target string

	// UID is the user ID
	UID string

	// GID is the group ID
	GID string

	// Mode is the file mode
	Mode uint32
}

// LoggingConfig defines logging driver configuration.
type LoggingConfig struct {
	// Driver is the logging driver (json-file, syslog, etc.)
	Driver string

	// Options are driver-specific options
	Options map[string]string
}

// NewSwarmService creates a new SwarmService with defaults.
func NewSwarmService(name string) *SwarmService {
	return &SwarmService{
		Name:        name,
		Environment: make(map[string]string),
		Labels:      make(map[string]string),
		Ports:       make([]string, 0),
		Volumes:     make([]string, 0),
		Networks:    make([]string, 0),
		DependsOn:   make([]string, 0),
		Command:     make([]string, 0),
		Secrets:     make([]SecretRef, 0),
		Configs:     make([]ConfigRef, 0),
		Deploy:      NewDeployConfig(),
	}
}

// NewDeployConfig creates a default deploy configuration.
func NewDeployConfig() *DeployConfig {
	return &DeployConfig{
		Replicas:       1,
		Mode:           "replicated",
		UpdateConfig:   NewUpdateConfig(),
		RollbackConfig: NewRollbackConfig(),
		RestartPolicy:  NewRestartPolicy(),
		Labels:         make(map[string]string),
		EndpointMode:   "vip",
	}
}

// NewUpdateConfig creates a default update configuration.
func NewUpdateConfig() *UpdateConfig {
	return &UpdateConfig{
		Parallelism:   1,
		Delay:         "10s",
		FailureAction: "rollback",
		Monitor:       "30s",
		Order:         "start-first",
	}
}

// NewRollbackConfig creates a default rollback configuration.
func NewRollbackConfig() *RollbackConfig {
	return &RollbackConfig{
		Parallelism: 1,
		Delay:       "5s",
		Order:       "stop-first",
	}
}

// NewRestartPolicy creates a default restart policy.
func NewRestartPolicy() *RestartPolicy {
	return &RestartPolicy{
		Condition:   "on-failure",
		Delay:       "5s",
		MaxAttempts: 3,
		Window:      "120s",
	}
}

// WithReplicas sets the number of replicas.
func (d *DeployConfig) WithReplicas(n int) *DeployConfig {
	d.Replicas = n
	return d
}

// WithGlobalMode sets the service to global mode.
func (d *DeployConfig) WithGlobalMode() *DeployConfig {
	d.Mode = "global"
	d.Replicas = 0
	return d
}

// WithPlacement adds placement constraints.
func (d *DeployConfig) WithPlacement(constraints []string) *DeployConfig {
	if d.Placement == nil {
		d.Placement = &Placement{}
	}
	d.Placement.Constraints = constraints
	return d
}

// WithResources sets resource limits and reservations.
func (d *DeployConfig) WithResources(limits, reservations *ResourceLimits) *DeployConfig {
	d.Resources = &Resources{
		Limits:       limits,
		Reservations: reservations,
	}
	return d
}

// WithLabels adds deploy labels.
func (d *DeployConfig) WithLabels(labels map[string]string) *DeployConfig {
	for k, v := range labels {
		d.Labels[k] = v
	}
	return d
}

// ConfigureForHA configures the deploy settings for high availability.
func (d *DeployConfig) ConfigureForHA(replicas int) *DeployConfig {
	d.Replicas = replicas

	// Configure update for zero-downtime
	d.UpdateConfig = &UpdateConfig{
		Parallelism:   1,
		Delay:         "10s",
		FailureAction: "rollback",
		Monitor:       "60s",
		Order:         "start-first",
	}

	// Configure restart for resilience
	d.RestartPolicy = &RestartPolicy{
		Condition:   "any",
		Delay:       "5s",
		MaxAttempts: 5,
		Window:      "300s",
	}

	// Spread across nodes
	d.Placement = &Placement{
		Preferences: []PlacementPreference{
			{Spread: "node.id"},
		},
	}

	return d
}

// ConfigureForCluster configures the deploy settings for cluster mode.
func (d *DeployConfig) ConfigureForCluster() *DeployConfig {
	d.Replicas = 3

	// Configure update for zero-downtime
	d.UpdateConfig = &UpdateConfig{
		Parallelism:   1,
		Delay:         "10s",
		FailureAction: "rollback",
		Monitor:       "60s",
		Order:         "start-first",
	}

	// Configure rollback
	d.RollbackConfig = &RollbackConfig{
		Parallelism: 1,
		Delay:       "5s",
		Order:       "stop-first",
	}

	// Configure restart for resilience
	d.RestartPolicy = &RestartPolicy{
		Condition:   "any",
		Delay:       "5s",
		MaxAttempts: 5,
		Window:      "300s",
	}

	// Spread across nodes and constrain to workers
	d.Placement = &Placement{
		Constraints: []string{
			"node.role == worker",
		},
		Preferences: []PlacementPreference{
			{Spread: "node.id"},
		},
		MaxReplicas: 1,
	}

	return d
}

// AddConstraint adds a placement constraint.
func (p *Placement) AddConstraint(constraint string) {
	p.Constraints = append(p.Constraints, constraint)
}

// AddPreference adds a placement preference.
func (p *Placement) AddPreference(spread string) {
	p.Preferences = append(p.Preferences, PlacementPreference{Spread: spread})
}

// WithTest sets the health check test command.
func (h *HealthCheck) WithTest(test []string) *HealthCheck {
	h.Test = test
	return h
}

// WithInterval sets the health check interval.
func (h *HealthCheck) WithInterval(interval string) *HealthCheck {
	h.Interval = interval
	return h
}

// WithTimeout sets the health check timeout.
func (h *HealthCheck) WithTimeout(timeout string) *HealthCheck {
	h.Timeout = timeout
	return h
}

// WithRetries sets the health check retries.
func (h *HealthCheck) WithRetries(retries int) *HealthCheck {
	h.Retries = retries
	return h
}

// AddEnvironment adds an environment variable.
func (s *SwarmService) AddEnvironment(key, value string) {
	s.Environment[key] = value
}

// AddLabel adds a label.
func (s *SwarmService) AddLabel(key, value string) {
	s.Labels[key] = value
}

// AddPort adds a port mapping.
func (s *SwarmService) AddPort(port string) {
	s.Ports = append(s.Ports, port)
}

// AddVolume adds a volume mount.
func (s *SwarmService) AddVolume(volume string) {
	s.Volumes = append(s.Volumes, volume)
}

// AddNetwork adds a network.
func (s *SwarmService) AddNetwork(network string) {
	s.Networks = append(s.Networks, network)
}

// AddDependency adds a service dependency.
func (s *SwarmService) AddDependency(service string) {
	s.DependsOn = append(s.DependsOn, service)
}

// AddSecret adds a secret reference.
func (s *SwarmService) AddSecret(source, target string) {
	s.Secrets = append(s.Secrets, SecretRef{
		Source: source,
		Target: target,
	})
}

// AddConfig adds a config reference.
func (s *SwarmService) AddConfig(source, target string) {
	s.Configs = append(s.Configs, ConfigRef{
		Source: source,
		Target: target,
	})
}

// SetHealthCheck sets the health check configuration.
func (s *SwarmService) SetHealthCheck(test []string, interval, timeout string, retries int) {
	s.HealthCheck = &HealthCheck{
		Test:     test,
		Interval: interval,
		Timeout:  timeout,
		Retries:  retries,
	}
}

// SetLogging configures logging.
func (s *SwarmService) SetLogging(driver string, options map[string]string) {
	s.Logging = &LoggingConfig{
		Driver:  driver,
		Options: options,
	}
}
