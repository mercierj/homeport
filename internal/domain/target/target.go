// Package target defines deployment target platforms and their configurations.
package target

// Platform represents a deployment target platform.
type Platform string

const (
	// Self-hosted platforms
	PlatformDockerCompose Platform = "docker-compose" // Single server, Docker Compose
	PlatformDockerSwarm   Platform = "docker-swarm"   // Multi-server, Docker Swarm
	PlatformK3s           Platform = "k3s"            // Lightweight Kubernetes
	PlatformKubernetes    Platform = "kubernetes"     // Full Kubernetes

	// EU Cloud platforms
	PlatformScaleway  Platform = "scaleway"  // Scaleway (France)
	PlatformOVH       Platform = "ovh"       // OVHcloud (France)
	PlatformHetzner   Platform = "hetzner"   // Hetzner (Germany)
	PlatformExoscale  Platform = "exoscale"  // Exoscale (Switzerland)
	PlatformInfomaniak Platform = "infomaniak" // Infomaniak (Switzerland)

	// Hybrid
	PlatformHybrid Platform = "hybrid" // Mix of self-hosted and cloud
)

// String returns the string representation of the platform.
func (p Platform) String() string {
	return string(p)
}

// IsSelfHosted returns true if the platform is self-hosted.
func (p Platform) IsSelfHosted() bool {
	switch p {
	case PlatformDockerCompose, PlatformDockerSwarm, PlatformK3s, PlatformKubernetes:
		return true
	default:
		return false
	}
}

// IsCloud returns true if the platform is a cloud provider.
func (p Platform) IsCloud() bool {
	switch p {
	case PlatformScaleway, PlatformOVH, PlatformHetzner, PlatformExoscale, PlatformInfomaniak:
		return true
	default:
		return false
	}
}

// RequiresTerraform returns true if the platform requires Terraform output.
func (p Platform) RequiresTerraform() bool {
	return p.IsCloud()
}

// RequiresCredentials returns true if the platform requires cloud credentials.
func (p Platform) RequiresCredentials() bool {
	return p.IsCloud()
}

// TargetConfig holds configuration for a deployment target.
type TargetConfig struct {
	// Platform is the target deployment platform
	Platform Platform

	// HALevel is the high availability level
	HALevel HALevel

	// Region is the deployment region (for cloud platforms)
	Region string

	// Zones are the availability zones to use
	Zones []string

	// ServerCount is the number of servers for multi-server deployments
	ServerCount int

	// Provider-specific configurations
	Scaleway  *ScalewayConfig
	OVH       *OVHConfig
	Hetzner   *HetznerConfig
	Exoscale  *ExoscaleConfig
}

// NewTargetConfig creates a new target config with defaults.
func NewTargetConfig(platform Platform) *TargetConfig {
	return &TargetConfig{
		Platform:    platform,
		HALevel:     HALevelNone,
		ServerCount: 1,
	}
}

// WithHALevel sets the HA level.
func (c *TargetConfig) WithHALevel(level HALevel) *TargetConfig {
	c.HALevel = level
	return c
}

// WithRegion sets the region.
func (c *TargetConfig) WithRegion(region string) *TargetConfig {
	c.Region = region
	return c
}

// WithZones sets the availability zones.
func (c *TargetConfig) WithZones(zones []string) *TargetConfig {
	c.Zones = zones
	return c
}

// WithServerCount sets the server count.
func (c *TargetConfig) WithServerCount(count int) *TargetConfig {
	c.ServerCount = count
	return c
}

// ScalewayConfig holds Scaleway-specific configuration.
type ScalewayConfig struct {
	ProjectID string
	Zone      string // e.g., "fr-par-1"
	Region    string // e.g., "fr-par"
	AccessKey string
	SecretKey string
}

// OVHConfig holds OVHcloud-specific configuration.
type OVHConfig struct {
	Endpoint   string // e.g., "ovh-eu"
	TenantName string
	Username   string
	Password   string
	Region     string // e.g., "GRA11"
	ProjectID  string
}

// HetznerConfig holds Hetzner-specific configuration.
type HetznerConfig struct {
	Token       string
	Location    string // e.g., "fsn1", "nbg1"
	NetworkZone string // e.g., "eu-central"
}

// ExoscaleConfig holds Exoscale-specific configuration.
type ExoscaleConfig struct {
	APIKey    string
	APISecret string
	Zone      string // e.g., "ch-gva-2"
}

// ValidPlatforms returns all valid platform values.
func ValidPlatforms() []Platform {
	return []Platform{
		PlatformDockerCompose,
		PlatformDockerSwarm,
		PlatformK3s,
		PlatformKubernetes,
		PlatformScaleway,
		PlatformOVH,
		PlatformHetzner,
		PlatformExoscale,
		PlatformInfomaniak,
		PlatformHybrid,
	}
}

// ParsePlatform parses a string into a Platform.
func ParsePlatform(s string) (Platform, bool) {
	p := Platform(s)
	for _, valid := range ValidPlatforms() {
		if p == valid {
			return p, true
		}
	}
	// Check aliases
	switch s {
	case "self-hosted", "selfhosted", "compose":
		return PlatformDockerCompose, true
	case "swarm":
		return PlatformDockerSwarm, true
	case "k8s":
		return PlatformKubernetes, true
	}
	return "", false
}
