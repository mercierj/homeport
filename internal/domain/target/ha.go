// Package target defines deployment target platforms and HA configurations.
package target

// HALevel represents the high availability level for deployments.
type HALevel string

const (
	// HALevelNone is Level 0: Single server, all services on one machine.
	// - 1 server, all services
	// - Manual backups (or none)
	// - Basic monitoring
	// - RTO: hours, RPO: hours
	HALevelNone HALevel = "none"

	// HALevelBasic is Level 1: Single server with automated backups and monitoring.
	// - 1 server principal
	// - Automated backups (restic/borg)
	// - DB async replication to backup storage
	// - S3/MinIO replicated to backup
	// - Full monitoring (Prometheus/Grafana)
	// - Alerting configured
	// - RTO: ~1h, RPO: minutes
	HALevelBasic HALevel = "basic"

	// HALevelMultiServer is Level 2: Active-Passive with failover.
	// - 2+ servers (1 active, N passive)
	// - Floating IP or DNS failover
	// - DB synchronous replication
	// - Shared storage or real-time sync
	// - Health checks with auto-failover
	// - RTO: minutes, RPO: seconds
	HALevelMultiServer HALevel = "multi-server"

	// HALevelCluster is Level 3: Active-Active cluster.
	// - 3+ servers all active
	// - Load balancer distributes traffic
	// - DB cluster (Patroni, Galera, etc.)
	// - Distributed storage (MinIO cluster)
	// - Redis Sentinel/Cluster
	// - Zero-downtime deployments
	// - RTO: seconds, RPO: ~0
	HALevelCluster HALevel = "cluster"

	// HALevelGeo is Level 4: Multi-datacenter / Geo-redundant.
	// - Servers in 2+ datacenters
	// - GeoDNS or Anycast
	// - Async replication cross-DC
	// - Disaster recovery ready
	// - RTO: seconds, RPO: seconds-minutes
	HALevelGeo HALevel = "geo"
)

// String returns the string representation of the HA level.
func (h HALevel) String() string {
	return string(h)
}

// Level returns the numeric level (0-4).
func (h HALevel) Level() int {
	switch h {
	case HALevelNone:
		return 0
	case HALevelBasic:
		return 1
	case HALevelMultiServer:
		return 2
	case HALevelCluster:
		return 3
	case HALevelGeo:
		return 4
	default:
		return 0
	}
}

// RequiresMultiServer returns true if the level requires multiple servers.
func (h HALevel) RequiresMultiServer() bool {
	return h == HALevelMultiServer || h == HALevelCluster || h == HALevelGeo
}

// RequiresCluster returns true if the level requires clustered services.
func (h HALevel) RequiresCluster() bool {
	return h == HALevelCluster || h == HALevelGeo
}

// RequiresGeo returns true if the level requires geo-distribution.
func (h HALevel) RequiresGeo() bool {
	return h == HALevelGeo
}

// HARequirements defines the requirements for each HA level.
type HARequirements struct {
	// MinServers is the minimum number of servers required
	MinServers int

	// DBReplicas is the number of database replicas required
	DBReplicas int

	// StorageReplicas is the number of storage replicas required
	StorageReplicas int

	// CacheReplicas is the number of cache replicas required
	CacheReplicas int

	// RTO (Recovery Time Objective) - maximum acceptable downtime
	RTO string

	// RPO (Recovery Point Objective) - maximum acceptable data loss
	RPO string

	// Description is a human-readable description
	Description string

	// Features lists the features included at this level
	Features []string
}

// Requirements returns the requirements for this HA level.
func (h HALevel) Requirements() HARequirements {
	switch h {
	case HALevelNone:
		return HARequirements{
			MinServers:      1,
			DBReplicas:      0,
			StorageReplicas: 0,
			CacheReplicas:   0,
			RTO:             "hours",
			RPO:             "hours",
			Description:     "Single server with manual recovery",
			Features: []string{
				"Single Docker Compose deployment",
				"Local volume storage",
				"Manual backup (optional)",
			},
		}
	case HALevelBasic:
		return HARequirements{
			MinServers:      1,
			DBReplicas:      1, // Async replica to backup
			StorageReplicas: 1,
			CacheReplicas:   0,
			RTO:             "1h",
			RPO:             "minutes",
			Description:     "Single server with automated backups and monitoring",
			Features: []string{
				"Automated backups (restic/borg)",
				"DB async replication to backup storage",
				"S3/MinIO backup replication",
				"Prometheus + Grafana monitoring",
				"Alert manager configured",
				"Health check endpoints",
			},
		}
	case HALevelMultiServer:
		return HARequirements{
			MinServers:      2,
			DBReplicas:      1, // Sync replica
			StorageReplicas: 1,
			CacheReplicas:   1,
			RTO:             "minutes",
			RPO:             "seconds",
			Description:     "Active-passive with floating IP failover",
			Features: []string{
				"Docker Swarm or K3s orchestration",
				"Floating IP or DNS failover",
				"PostgreSQL sync replication",
				"Redis replica with Sentinel",
				"Shared/synced storage",
				"Automated failover",
				"Health checks with auto-recovery",
			},
		}
	case HALevelCluster:
		return HARequirements{
			MinServers:      3,
			DBReplicas:      2, // Full cluster
			StorageReplicas: 3,
			CacheReplicas:   2,
			RTO:             "seconds",
			RPO:             "~0",
			Description:     "Active-active cluster with load balancing",
			Features: []string{
				"Docker Swarm or Kubernetes cluster",
				"Load balancer (Traefik/HAProxy)",
				"PostgreSQL Patroni cluster",
				"Redis Sentinel/Cluster",
				"MinIO distributed mode",
				"Zero-downtime deployments",
				"Rolling updates",
				"Auto-scaling ready",
			},
		}
	case HALevelGeo:
		return HARequirements{
			MinServers:      4, // 2+ per DC
			DBReplicas:      3,
			StorageReplicas: 4,
			CacheReplicas:   3,
			RTO:             "seconds",
			RPO:             "seconds",
			Description:     "Multi-datacenter with geo-redundancy",
			Features: []string{
				"Servers in 2+ datacenters",
				"GeoDNS or Anycast routing",
				"Cross-DC async replication",
				"CockroachDB for global SQL",
				"Multi-region MinIO",
				"Disaster recovery automation",
				"Latency-based routing",
			},
		}
	default:
		return HARequirements{
			MinServers:  1,
			Description: "Unknown HA level",
		}
	}
}

// HAConfig holds specific HA configuration options.
type HAConfig struct {
	// Level is the HA level
	Level HALevel

	// BackupSchedule is a cron expression for backup frequency
	BackupSchedule string

	// BackupRetention is the number of days to keep backups
	BackupRetention int

	// FloatingIP enables floating IP for failover (Level 2+)
	FloatingIP bool

	// FloatingIPAddress is the floating IP address to use
	FloatingIPAddress string

	// Datacenters is the list of datacenters for geo deployment
	Datacenters []string

	// GeoDNS enables geographic DNS routing
	GeoDNS bool

	// MonitoringEnabled enables Prometheus/Grafana stack
	MonitoringEnabled bool

	// AlertingEnabled enables alerting (requires monitoring)
	AlertingEnabled bool

	// AlertingWebhook is the webhook URL for alerts
	AlertingWebhook string
}

// NewHAConfig creates a new HA config with defaults for the given level.
func NewHAConfig(level HALevel) *HAConfig {
	config := &HAConfig{
		Level:             level,
		BackupSchedule:    "0 2 * * *", // Daily at 2 AM
		BackupRetention:   7,
		MonitoringEnabled: level.Level() >= 1,
		AlertingEnabled:   level.Level() >= 1,
	}

	if level.RequiresMultiServer() {
		config.FloatingIP = true
	}

	return config
}

// ValidHALevels returns all valid HA level values.
func ValidHALevels() []HALevel {
	return []HALevel{
		HALevelNone,
		HALevelBasic,
		HALevelMultiServer,
		HALevelCluster,
		HALevelGeo,
	}
}

// ParseHALevel parses a string into an HALevel.
func ParseHALevel(s string) (HALevel, bool) {
	h := HALevel(s)
	for _, valid := range ValidHALevels() {
		if h == valid {
			return h, true
		}
	}
	// Check aliases and numeric levels
	switch s {
	case "0", "single":
		return HALevelNone, true
	case "1", "backup", "backups":
		return HALevelBasic, true
	case "2", "multi", "failover":
		return HALevelMultiServer, true
	case "3", "ha", "active-active":
		return HALevelCluster, true
	case "4", "multi-dc", "multidc", "geographic":
		return HALevelGeo, true
	}
	return "", false
}

// SupportedHALevelsForPlatform returns the HA levels supported by a platform.
func SupportedHALevelsForPlatform(platform Platform) []HALevel {
	switch platform {
	case PlatformDockerCompose:
		// Docker Compose only supports single-server levels
		return []HALevel{HALevelNone, HALevelBasic}
	case PlatformDockerSwarm:
		// Docker Swarm supports up to cluster level
		return []HALevel{HALevelNone, HALevelBasic, HALevelMultiServer, HALevelCluster}
	case PlatformK3s, PlatformKubernetes:
		// Kubernetes supports all levels
		return []HALevel{HALevelNone, HALevelBasic, HALevelMultiServer, HALevelCluster, HALevelGeo}
	case PlatformScaleway, PlatformOVH, PlatformHetzner:
		// Cloud platforms support managed HA
		return []HALevel{HALevelNone, HALevelBasic, HALevelMultiServer, HALevelCluster}
	default:
		return []HALevel{HALevelNone}
	}
}
