// Package database provides mappers for AWS database services.
package database

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

// ElastiCacheMapper converts AWS ElastiCache clusters to Redis or Memcached.
type ElastiCacheMapper struct {
	*mapper.BaseMapper
}

// NewElastiCacheMapper creates a new ElastiCache to Redis/Memcached mapper.
func NewElastiCacheMapper() *ElastiCacheMapper {
	return &ElastiCacheMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeElastiCache, nil),
	}
}

// Map converts an ElastiCache cluster to a Redis or Memcached service.
func (m *ElastiCacheMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	engine := res.GetConfigString("engine")
	clusterID := res.GetConfigString("cluster_id")
	if clusterID == "" {
		clusterID = res.Name
	}

	// Determine engine type and create appropriate service
	switch engine {
	case "redis":
		return m.createRedisService(res, clusterID)
	case "memcached":
		return m.createMemcachedService(res, clusterID)
	default:
		return nil, fmt.Errorf("unsupported ElastiCache engine: %s", engine)
	}
}

// createRedisService creates a Redis service.
func (m *ElastiCacheMapper) createRedisService(res *resource.AWSResource, clusterID string) (*mapper.MappingResult, error) {
	engineVersion := res.GetConfigString("engine_version")
	if engineVersion == "" {
		engineVersion = "7-alpine"
	} else {
		// Extract major version and add alpine tag
		parts := strings.Split(engineVersion, ".")
		if len(parts) > 0 {
			engineVersion = parts[0] + "-alpine"
		}
	}

	numCacheNodes := res.GetConfigInt("num_cache_nodes")
	port := res.GetConfigInt("port")
	if port == 0 {
		port = 6379
	}

	// Check for cluster mode
	clusterModeEnabled := res.GetConfigBool("cluster_mode_enabled")
	if !clusterModeEnabled {
		if clusterMode := res.Config["cluster_mode"]; clusterMode != nil {
			clusterModeEnabled = true
		}
	}

	// Create result using new API
	result := mapper.NewMappingResult("redis")
	svc := result.DockerService

	// Configure Redis service
	svc.Image = fmt.Sprintf("redis:%s", engineVersion)
	svc.Ports = []string{fmt.Sprintf("%d:%d", port, port)}
	svc.Volumes = []string{
		"./data/redis:/data",
	}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "redis-cli", "ping"},
		Interval: 10 * time.Second,
		Timeout:  3 * time.Second,
		Retries:  5,
	}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"homeport.source":  "aws_elasticache_cluster",
		"homeport.engine":  "redis",
		"homeport.cluster": clusterID,
	}

	// Handle persistence settings
	snapshotRetention := res.GetConfigInt("snapshot_retention_limit")
	if snapshotRetention > 0 {
		// Enable both AOF and RDB persistence
		svc.Command = []string{
			"redis-server",
			"--appendonly", "yes",
			"--appendfsync", "everysec",
			"--save", "900", "1",
			"--save", "300", "10",
			"--save", "60", "10000",
		}
		result.AddWarning("Redis persistence (AOF + RDB) has been enabled to match ElastiCache snapshot behavior.")
	} else {
		svc.Command = []string{
			"redis-server",
			"--appendonly", "no",
		}
	}

	// Handle authentication
	if authToken := res.GetConfigString("auth_token"); authToken != "" {
		svc.Command = append(svc.Command, "--requirepass", "changeme")
		svc.Environment = map[string]string{
			"REDIS_PASSWORD": "changeme",
		}
		result.AddManualStep("Update Redis password in docker-compose.yml (replace 'changeme')")
	}

	// Handle transit encryption
	if res.GetConfigBool("transit_encryption_enabled") {
		result.AddWarning("Transit encryption (TLS) is enabled in ElastiCache. Configure Redis TLS manually if required.")
		result.AddManualStep("Configure Redis TLS: https://redis.io/docs/manual/security/encryption/")
	}

	// Handle at-rest encryption
	if res.GetConfigBool("at_rest_encryption_enabled") {
		result.AddWarning("At-rest encryption is enabled in ElastiCache. Consider using encrypted volumes for your self-hosted Redis.")
	}

	// Add Redis configuration file
	redisConfig := m.generateRedisConfig(res, snapshotRetention)
	result.AddConfig("config/redis/redis.conf", []byte(redisConfig))

	// Handle cluster mode
	if clusterModeEnabled {
		result.AddWarning("ElastiCache cluster mode is enabled. This requires Redis Cluster setup with multiple nodes.")
		result.AddManualStep("Set up Redis Cluster with multiple nodes for horizontal scaling")
		result.AddManualStep("Refer to: https://redis.io/docs/management/scaling/")

		clusterScript := m.generateRedisClusterScript(numCacheNodes)
		result.AddScript("setup_redis_cluster.sh", []byte(clusterScript))
	}

	// Add migration script
	migrationScript := m.generateRedisMigrationScript(clusterID)
	result.AddScript("migrate_redis.sh", []byte(migrationScript))

	return result, nil
}

// createMemcachedService creates a Memcached service.
func (m *ElastiCacheMapper) createMemcachedService(res *resource.AWSResource, clusterID string) (*mapper.MappingResult, error) {
	engineVersion := res.GetConfigString("engine_version")
	if engineVersion == "" {
		engineVersion = "1.6-alpine"
	} else {
		// Extract major.minor version
		parts := strings.Split(engineVersion, ".")
		if len(parts) >= 2 {
			engineVersion = parts[0] + "." + parts[1] + "-alpine"
		}
	}

	nodeType := res.GetConfigString("node_type")
	port := res.GetConfigInt("port")
	if port == 0 {
		port = 11211
	}

	// Calculate memory limit from node type
	memoryMB := m.getMemoryFromNodeType(nodeType)

	// Create result using new API
	result := mapper.NewMappingResult("memcached")
	svc := result.DockerService

	// Configure Memcached service
	svc.Image = fmt.Sprintf("memcached:%s", engineVersion)
	svc.Ports = []string{fmt.Sprintf("%d:%d", port, port)}
	svc.Command = []string{
		"memcached",
		"-m", fmt.Sprintf("%d", memoryMB),
		"-c", "1024",
		"-t", "4",
	}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "timeout", "1", "bash", "-c", "cat < /dev/null > /dev/tcp/127.0.0.1/11211"},
		Interval: 10 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  3,
	}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"homeport.source":  "aws_elasticache_cluster",
		"homeport.engine":  "memcached",
		"homeport.cluster": clusterID,
	}

	result.AddWarning("Memcached is stateless and does not persist data. Ensure your application handles cache warming appropriately.")

	return result, nil
}

// generateRedisConfig creates a Redis configuration file.
func (m *ElastiCacheMapper) generateRedisConfig(res *resource.AWSResource, snapshotRetention int) string {
	config := `# Redis Configuration
# Generated from ElastiCache cluster settings

# Network
bind 0.0.0.0
protected-mode yes
port 6379
tcp-backlog 511
timeout 0
tcp-keepalive 300

# General
daemonize no
supervised no
loglevel notice
databases 16

`

	if snapshotRetention > 0 {
		config += `# Persistence (RDB)
save 900 1
save 300 10
save 60 10000
stop-writes-on-bgsave-error yes
rdbcompression yes
rdbchecksum yes
dbfilename dump.rdb
dir /data

# Persistence (AOF)
appendonly yes
appendfilename "appendonly.aof"
appendfsync everysec
no-appendfsync-on-rewrite no
auto-aof-rewrite-percentage 100
auto-aof-rewrite-min-size 64mb
`
	} else {
		config += `# Persistence disabled
save ""
appendonly no
`
	}

	config += `
# Limits
maxclients 10000

# Memory Management
maxmemory-policy allkeys-lru
`

	return config
}

// generateRedisClusterScript creates a script to set up Redis Cluster.
func (m *ElastiCacheMapper) generateRedisClusterScript(numNodes int) string {
	if numNodes == 0 {
		numNodes = 3
	}

	script := fmt.Sprintf(`#!/bin/bash
# Redis Cluster Setup Script
# Sets up a Redis cluster with %d nodes

set -e

echo "Redis Cluster Setup"
echo "==================="
echo "This script will create a %d-node Redis cluster"
echo ""

# Create docker-compose override with cluster nodes
cat > docker-compose.cluster.yml <<EOF
version: '3.8'
services:
`, numNodes, numNodes)

	for i := 1; i <= numNodes; i++ {
		port := 6379 + i - 1
		script += fmt.Sprintf(`  redis-node-%d:
    image: redis:7-alpine
    command: redis-server --cluster-enabled yes --cluster-config-file nodes.conf --cluster-node-timeout 5000 --appendonly yes --port %d
    ports:
      - "%d:%d"
    volumes:
      - ./data/redis-node-%d:/data
    networks:
      - homeport
`, i, port, port, port, i)
	}

	script += `EOF

echo "Starting cluster nodes..."
docker-compose -f docker-compose.yml -f docker-compose.cluster.yml up -d

echo "Waiting for nodes to start..."
sleep 10

echo "Creating cluster..."
docker exec -it redis-node-1 redis-cli --cluster create \
`

	for i := 1; i <= numNodes; i++ {
		port := 6379 + i - 1
		script += fmt.Sprintf("  redis-node-%d:%d \\\n", i, port)
	}

	script += `  --cluster-replicas 1 --cluster-yes

echo "Cluster created successfully!"
echo "Connect to cluster: redis-cli -c -h localhost -p 6379"
`

	return script
}

// generateRedisMigrationScript creates a script to migrate data from ElastiCache to Redis.
func (m *ElastiCacheMapper) generateRedisMigrationScript(clusterID string) string {
	script := `#!/bin/bash
# Redis Migration Script
# Migrates data from AWS ElastiCache to local Redis

set -e

echo "Redis Migration from ElastiCache"
echo "================================"

# Variables
ELASTICACHE_HOST="${ELASTICACHE_HOST:-your-cluster.cache.amazonaws.com}"
ELASTICACHE_PORT="${ELASTICACHE_PORT:-6379}"
LOCAL_HOST="localhost"
LOCAL_PORT="6379"

echo "Source: $ELASTICACHE_HOST:$ELASTICACHE_PORT"
echo "Destination: $LOCAL_HOST:$LOCAL_PORT"
echo ""

# Method 1: Using redis-dump-go (recommended)
echo "Option 1: Using redis-dump-go"
echo "Install: go install github.com/yannh/redis-dump-go@latest"
echo "Run: redis-dump-go -host $ELASTICACHE_HOST -port $ELASTICACHE_PORT | redis-cli -h $LOCAL_HOST -p $LOCAL_PORT"
echo ""

# Method 2: Using RDB snapshot (if available)
echo "Option 2: Using RDB snapshot"
echo "1. Trigger a snapshot in ElastiCache console"
echo "2. Export snapshot to S3"
echo "3. Download dump.rdb file"
echo "4. Place in ./data/redis/ and restart Redis container"
echo ""

# Method 3: Using RIOT (Redis Input/Output Tools)
echo "Option 3: Using RIOT"
echo "Install: https://github.com/redis-developer/riot"
echo "Run: riot replicate redis://ELASTICACHE_HOST:6379 redis://localhost:6379"
echo ""

echo "Choose the method that best fits your use case"
`

	return script
}

// getMemoryFromNodeType extracts memory size in MB from node type.
func (m *ElastiCacheMapper) getMemoryFromNodeType(nodeType string) int {
	// Simplified mapping - adjust as needed
	if strings.Contains(nodeType, "micro") {
		return 512
	} else if strings.Contains(nodeType, "small") {
		return 1024
	} else if strings.Contains(nodeType, "medium") {
		return 3072
	} else if strings.Contains(nodeType, "large") {
		return 6144
	} else if strings.Contains(nodeType, "xlarge") {
		if strings.Contains(nodeType, "2xlarge") {
			return 25600
		} else if strings.Contains(nodeType, "4xlarge") {
			return 51200
		}
		return 12800
	}
	return 1024
}
