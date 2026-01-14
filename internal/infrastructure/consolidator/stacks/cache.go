package stacks

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	"github.com/homeport/homeport/internal/domain/stack"
	"github.com/homeport/homeport/internal/infrastructure/consolidator"
)

// MaxRedisDatabases is the maximum number of Redis databases (0-15).
const MaxRedisDatabases = 16

// CacheMerger consolidates cache resources into a single Redis stack.
// It combines multiple ElastiCache, Memorystore, Azure Cache, and other cache resources
// into a unified Redis deployment using different DB numbers for logical separation.
type CacheMerger struct {
	*consolidator.BaseMerger
}

// NewCacheMerger creates a new CacheMerger.
func NewCacheMerger() *CacheMerger {
	return &CacheMerger{
		BaseMerger: consolidator.NewBaseMerger(stack.StackTypeCache),
	}
}

// StackType returns the stack type this merger handles.
func (m *CacheMerger) StackType() stack.StackType {
	return stack.StackTypeCache
}

// CanMerge checks if this merger can handle the given results.
// Returns true if there is at least one cache resource to merge.
func (m *CacheMerger) CanMerge(results []*mapper.MappingResult) bool {
	if len(results) == 0 {
		return false
	}

	// Check if any results are cache-related
	for _, r := range results {
		if r != nil && isCacheResource(r.SourceResourceType) {
			return true
		}
	}

	return false
}

// Merge consolidates multiple mapping results into a single cache stack.
// It creates a Redis container with logical separation using DB numbers (0-15)
// for different source caches.
func (m *CacheMerger) Merge(ctx context.Context, results []*mapper.MappingResult, opts *consolidator.MergeOptions) (*stack.Stack, error) {
	if len(results) == 0 {
		return nil, fmt.Errorf("no results to merge")
	}

	// Create the stack
	name := "cache"
	if opts != nil && opts.NamePrefix != "" {
		name = opts.NamePrefix + "-" + name
	}

	stk := stack.NewStack(stack.StackTypeCache, name)
	stk.Description = "Consolidated cache stack with Redis"

	// Assign DB numbers to each source cache
	dbAssignments := m.assignDatabaseNumbers(results)

	// Add warning if we have more caches than available DB numbers
	if len(results) > MaxRedisDatabases {
		stk.Metadata["warning_db_limit"] = fmt.Sprintf(
			"Warning: %d cache resources exceed Redis DB limit of %d. Some caches share DB numbers.",
			len(results), MaxRedisDatabases)
	}

	// Create primary Redis service
	redisService := m.createRedisService(len(dbAssignments))
	stk.AddService(redisService)

	// Create Redis Commander UI for management (if support services enabled)
	if opts == nil || opts.IncludeSupportServices {
		commanderService := m.createRedisCommanderService()
		stk.AddService(commanderService)
	}

	// Generate redis.conf
	redisConfig := m.generateRedisConfig(len(dbAssignments))
	stk.AddConfig("redis.conf", redisConfig)

	// Generate mapping documentation
	mappingDoc := m.generateMappingDoc(dbAssignments)
	stk.AddScript("CACHE_MAPPING.md", mappingDoc)

	// Generate migration documentation
	migrationDoc := m.generateMigrationDoc(results, dbAssignments)
	stk.AddScript("MIGRATION.md", migrationDoc)

	// Add data volume
	stk.AddVolume(stack.Volume{
		Name:   "cache-data",
		Driver: "local",
		Labels: map[string]string{
			"homeport.stack": "cache",
			"homeport.role":  "primary-data",
		},
	})

	// Track source resources
	for _, result := range results {
		if result != nil {
			res := &resource.Resource{
				Type: resource.Type(result.SourceResourceType),
				Name: result.SourceResourceName,
			}
			stk.AddSourceResource(res)

			// Collect warnings
			for _, warning := range result.Warnings {
				stk.Metadata["warning_"+result.SourceResourceName] = warning
			}
		}
	}

	// Add manual steps
	stk.Metadata["manual_step_1"] = "Update application Redis connection strings to include the correct DB number"
	stk.Metadata["manual_step_2"] = "Migrate data from source caches using redis-cli MIGRATE or DUMP/RESTORE"
	stk.Metadata["manual_step_3"] = "Verify cache functionality after migration"

	// Store DB assignments in metadata for reference
	for cacheName, dbNum := range dbAssignments {
		stk.Metadata[fmt.Sprintf("db_assignment_%s", cacheName)] = fmt.Sprintf("%d", dbNum)
	}

	// Merge configs and scripts from source results
	configs := consolidator.ExtractConfigs(results)
	for name, content := range configs {
		stk.AddConfig(name, content)
	}

	scripts := consolidator.ExtractScripts(results)
	for name, content := range scripts {
		stk.AddScript(name, content)
	}

	return stk, nil
}

// assignDatabaseNumbers assigns Redis DB numbers to each source cache.
// Returns a map of cache name to DB number (0-15).
func (m *CacheMerger) assignDatabaseNumbers(results []*mapper.MappingResult) map[string]int {
	assignments := make(map[string]int)

	// Collect unique cache names
	var cacheNames []string
	for _, result := range results {
		if result == nil {
			continue
		}
		name := result.SourceResourceName
		if name == "" {
			name = consolidator.NormalizeName(result.SourceResourceType)
		}
		cacheNames = append(cacheNames, name)
	}

	// Sort for consistent ordering
	sort.Strings(cacheNames)

	// Assign DB numbers (0-15), wrapping if necessary
	for i, name := range cacheNames {
		dbNum := i % MaxRedisDatabases
		assignments[name] = dbNum
	}

	return assignments
}

// createRedisService creates the main Redis service.
func (m *CacheMerger) createRedisService(numDatabases int) *stack.Service {
	svc := stack.NewService("redis", "redis:7")
	svc.Restart = "unless-stopped"

	// Use custom redis.conf
	svc.Command = []string{"redis-server", "/usr/local/etc/redis/redis.conf"}

	svc.Ports = []string{"6379:6379"}

	svc.Volumes = []string{
		"cache-data:/data",
		"./redis.conf:/usr/local/etc/redis/redis.conf:ro",
	}

	svc.HealthCheck = &stack.HealthCheck{
		Test:        []string{"CMD", "redis-cli", "ping"},
		Interval:    "10s",
		Timeout:     "5s",
		Retries:     5,
		StartPeriod: "10s",
	}

	// Set environment variables
	svc.Environment["REDIS_PASSWORD"] = "${REDIS_PASSWORD:-}"

	// Add labels
	svc.Labels["homeport.stack"] = "cache"
	svc.Labels["homeport.role"] = "primary"
	svc.Labels["homeport.databases"] = fmt.Sprintf("%d", numDatabases)

	return svc
}

// createRedisCommanderService creates a Redis Commander web UI service.
func (m *CacheMerger) createRedisCommanderService() *stack.Service {
	svc := stack.NewService("redis-commander", "rediscommander/redis-commander:latest")
	svc.Restart = "unless-stopped"

	svc.Ports = []string{"8081:8081"}

	svc.Environment["REDIS_HOSTS"] = "local:redis:6379"
	svc.Environment["HTTP_USER"] = "${REDIS_COMMANDER_USER:-admin}"
	svc.Environment["HTTP_PASSWORD"] = "${REDIS_COMMANDER_PASSWORD:-admin}"

	svc.DependsOn = []string{"redis"}

	svc.Labels["homeport.stack"] = "cache"
	svc.Labels["homeport.role"] = "ui"

	return svc
}

// generateRedisConfig generates a redis.conf configuration file.
func (m *CacheMerger) generateRedisConfig(numDatabases int) []byte {
	var sb strings.Builder

	sb.WriteString("# ============================================================\n")
	sb.WriteString("# Redis Configuration\n")
	sb.WriteString("# Generated by Homeport Stack Consolidation\n")
	sb.WriteString(fmt.Sprintf("# Generated at: %s\n", time.Now().Format(time.RFC3339)))
	sb.WriteString("# ============================================================\n")
	sb.WriteString("#\n")
	sb.WriteString(fmt.Sprintf("# This Redis instance consolidates %d cache resources.\n", numDatabases))
	sb.WriteString("# Each source cache is assigned a separate database number (0-15).\n")
	sb.WriteString("# See CACHE_MAPPING.md for the complete mapping.\n")
	sb.WriteString("#\n\n")

	sb.WriteString("# ============================================================\n")
	sb.WriteString("# GENERAL\n")
	sb.WriteString("# ============================================================\n\n")

	sb.WriteString("# Bind to all interfaces (use 127.0.0.1 for local only)\n")
	sb.WriteString("bind 0.0.0.0\n\n")

	sb.WriteString("# Protected mode (disable if not using password)\n")
	sb.WriteString("protected-mode no\n\n")

	sb.WriteString("# Port\n")
	sb.WriteString("port 6379\n\n")

	sb.WriteString("# Number of databases (0-15)\n")
	sb.WriteString("databases 16\n\n")

	sb.WriteString("# ============================================================\n")
	sb.WriteString("# PERSISTENCE\n")
	sb.WriteString("# ============================================================\n\n")

	sb.WriteString("# Enable AOF persistence for durability\n")
	sb.WriteString("appendonly yes\n")
	sb.WriteString("appendfilename \"appendonly.aof\"\n")
	sb.WriteString("appendfsync everysec\n\n")

	sb.WriteString("# RDB snapshots (disabled when AOF is enabled)\n")
	sb.WriteString("# save 900 1\n")
	sb.WriteString("# save 300 10\n")
	sb.WriteString("# save 60 10000\n\n")

	sb.WriteString("# Data directory\n")
	sb.WriteString("dir /data\n\n")

	sb.WriteString("# ============================================================\n")
	sb.WriteString("# MEMORY MANAGEMENT\n")
	sb.WriteString("# ============================================================\n\n")

	sb.WriteString("# Maximum memory limit (adjust based on your needs)\n")
	sb.WriteString("# maxmemory 256mb\n\n")

	sb.WriteString("# Eviction policy when maxmemory is reached\n")
	sb.WriteString("# Options: volatile-lru, allkeys-lru, volatile-lfu, allkeys-lfu,\n")
	sb.WriteString("#          volatile-random, allkeys-random, volatile-ttl, noeviction\n")
	sb.WriteString("maxmemory-policy allkeys-lru\n\n")

	sb.WriteString("# Sample count for LRU/LFU algorithms\n")
	sb.WriteString("maxmemory-samples 5\n\n")

	sb.WriteString("# ============================================================\n")
	sb.WriteString("# CONNECTION SETTINGS\n")
	sb.WriteString("# ============================================================\n\n")

	sb.WriteString("# Max number of connected clients\n")
	sb.WriteString("maxclients 10000\n\n")

	sb.WriteString("# Client idle timeout (0 = disabled)\n")
	sb.WriteString("timeout 0\n\n")

	sb.WriteString("# TCP keepalive\n")
	sb.WriteString("tcp-keepalive 300\n\n")

	sb.WriteString("# ============================================================\n")
	sb.WriteString("# SECURITY\n")
	sb.WriteString("# ============================================================\n\n")

	sb.WriteString("# Require password (uncomment and set your password)\n")
	sb.WriteString("# requirepass your-strong-password-here\n\n")

	sb.WriteString("# Disable dangerous commands (recommended for production)\n")
	sb.WriteString("# rename-command FLUSHDB \"\"\n")
	sb.WriteString("# rename-command FLUSHALL \"\"\n")
	sb.WriteString("# rename-command DEBUG \"\"\n")
	sb.WriteString("# rename-command CONFIG \"\"\n\n")

	sb.WriteString("# ============================================================\n")
	sb.WriteString("# LOGGING\n")
	sb.WriteString("# ============================================================\n\n")

	sb.WriteString("# Log level: debug, verbose, notice, warning\n")
	sb.WriteString("loglevel notice\n\n")

	sb.WriteString("# Log to stdout (for Docker)\n")
	sb.WriteString("logfile \"\"\n\n")

	sb.WriteString("# ============================================================\n")
	sb.WriteString("# PERFORMANCE TUNING\n")
	sb.WriteString("# ============================================================\n\n")

	sb.WriteString("# Lazy freeing for better performance\n")
	sb.WriteString("lazyfree-lazy-eviction yes\n")
	sb.WriteString("lazyfree-lazy-expire yes\n")
	sb.WriteString("lazyfree-lazy-server-del yes\n")
	sb.WriteString("replica-lazy-flush yes\n")

	return []byte(sb.String())
}

// generateMappingDoc generates a markdown document showing cache to DB number mapping.
func (m *CacheMerger) generateMappingDoc(assignments map[string]int) []byte {
	var sb strings.Builder

	sb.WriteString("# Cache to Redis Database Mapping\n\n")
	sb.WriteString("This document shows how your cloud cache resources are mapped to Redis database numbers.\n\n")

	sb.WriteString("## Overview\n\n")
	sb.WriteString("Redis supports 16 databases (numbered 0-15) by default. Each of your source cache\n")
	sb.WriteString("resources has been assigned a unique database number for logical separation.\n\n")

	sb.WriteString("## Mapping Table\n\n")
	sb.WriteString("| Source Cache | Redis DB Number | Connection String |\n")
	sb.WriteString("|--------------|-----------------|-------------------|\n")

	// Sort keys for consistent output
	var keys []string
	for k := range assignments {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, cacheName := range keys {
		dbNum := assignments[cacheName]
		connStr := fmt.Sprintf("redis://localhost:6379/%d", dbNum)
		sb.WriteString(fmt.Sprintf("| %s | %d | `%s` |\n", cacheName, dbNum, connStr))
	}
	sb.WriteString("\n")

	sb.WriteString("## Usage Examples\n\n")

	sb.WriteString("### Redis CLI\n\n")
	sb.WriteString("```bash\n")
	sb.WriteString("# Connect to specific database\n")
	sb.WriteString("redis-cli -n <DB_NUMBER>\n\n")
	sb.WriteString("# Switch database within session\n")
	sb.WriteString("127.0.0.1:6379> SELECT <DB_NUMBER>\n")
	sb.WriteString("```\n\n")

	sb.WriteString("### Node.js (ioredis)\n\n")
	sb.WriteString("```javascript\n")
	sb.WriteString("const Redis = require('ioredis');\n")
	sb.WriteString("const redis = new Redis({\n")
	sb.WriteString("  host: 'localhost',\n")
	sb.WriteString("  port: 6379,\n")
	sb.WriteString("  db: <DB_NUMBER>  // Use the mapped database number\n")
	sb.WriteString("});\n")
	sb.WriteString("```\n\n")

	sb.WriteString("### Python (redis-py)\n\n")
	sb.WriteString("```python\n")
	sb.WriteString("import redis\n")
	sb.WriteString("r = redis.Redis(\n")
	sb.WriteString("    host='localhost',\n")
	sb.WriteString("    port=6379,\n")
	sb.WriteString("    db=<DB_NUMBER>  # Use the mapped database number\n")
	sb.WriteString(")\n")
	sb.WriteString("```\n\n")

	sb.WriteString("### Go (go-redis)\n\n")
	sb.WriteString("```go\n")
	sb.WriteString("import \"github.com/redis/go-redis/v9\"\n\n")
	sb.WriteString("rdb := redis.NewClient(&redis.Options{\n")
	sb.WriteString("    Addr: \"localhost:6379\",\n")
	sb.WriteString("    DB:   <DB_NUMBER>, // Use the mapped database number\n")
	sb.WriteString("})\n")
	sb.WriteString("```\n\n")

	sb.WriteString("## Important Notes\n\n")
	sb.WriteString("1. **Database Isolation**: Each database is logically separate, but they share the same Redis instance and memory.\n")
	sb.WriteString("2. **FLUSHDB vs FLUSHALL**: `FLUSHDB` only clears the current database, while `FLUSHALL` clears all databases.\n")
	sb.WriteString("3. **Monitoring**: Use `INFO keyspace` to see key counts per database.\n")
	sb.WriteString("4. **Memory**: All databases share the same `maxmemory` limit.\n")

	if len(assignments) > MaxRedisDatabases {
		sb.WriteString("\n## Warning\n\n")
		sb.WriteString(fmt.Sprintf("You have %d cache resources but Redis only supports %d databases.\n",
			len(assignments), MaxRedisDatabases))
		sb.WriteString("Some caches are sharing database numbers. Consider:\n")
		sb.WriteString("- Using key prefixes to separate data within shared databases\n")
		sb.WriteString("- Deploying multiple Redis instances for true isolation\n")
	}

	return []byte(sb.String())
}

// generateMigrationDoc generates a markdown document with migration instructions.
func (m *CacheMerger) generateMigrationDoc(results []*mapper.MappingResult, assignments map[string]int) []byte {
	var sb strings.Builder

	sb.WriteString("# Cache Migration Guide\n\n")
	sb.WriteString("This document provides instructions for migrating your cloud cache resources to the consolidated Redis stack.\n\n")

	sb.WriteString("## Source Caches\n\n")
	sb.WriteString("The following caches have been consolidated:\n\n")
	sb.WriteString("| Source Resource | Type | Target Redis DB |\n")
	sb.WriteString("|-----------------|------|------------------|\n")
	for _, result := range results {
		if result != nil {
			dbNum := assignments[result.SourceResourceName]
			sb.WriteString(fmt.Sprintf("| %s | %s | %d |\n",
				result.SourceResourceName,
				result.SourceResourceType,
				dbNum))
		}
	}
	sb.WriteString("\n")

	sb.WriteString("## Target Configuration\n\n")
	sb.WriteString("- **Engine**: Redis 7\n")
	sb.WriteString("- **Port**: 6379\n")
	sb.WriteString("- **Web UI**: http://localhost:8081 (Redis Commander)\n")
	sb.WriteString("- **Persistence**: AOF (Append Only File)\n")
	sb.WriteString(fmt.Sprintf("- **Databases Used**: %d of %d\n\n", len(assignments), MaxRedisDatabases))

	sb.WriteString("## Migration Steps\n\n")

	sb.WriteString("### 1. Start the Cache Stack\n\n")
	sb.WriteString("```bash\n")
	sb.WriteString("docker compose up -d\n")
	sb.WriteString("```\n\n")

	sb.WriteString("### 2. Migrate Data from ElastiCache (AWS)\n\n")
	sb.WriteString("Option A: Using redis-cli MIGRATE (for small datasets):\n")
	sb.WriteString("```bash\n")
	sb.WriteString("# Connect to source ElastiCache\n")
	sb.WriteString("redis-cli -h <elasticache-endpoint> -p 6379\n\n")
	sb.WriteString("# For each key, migrate to target\n")
	sb.WriteString("MIGRATE localhost 6379 <key> <target-db> 5000 COPY\n")
	sb.WriteString("```\n\n")

	sb.WriteString("Option B: Using DUMP/RESTORE (recommended for larger datasets):\n")
	sb.WriteString("```bash\n")
	sb.WriteString("# Export keys from source\n")
	sb.WriteString("redis-cli -h <source> --scan --pattern '*' | while read key; do\n")
	sb.WriteString("    echo \"DUMP $key\"\n")
	sb.WriteString("    redis-cli -h <source> DUMP \"$key\" > \"$key.dump\"\n")
	sb.WriteString("done\n\n")
	sb.WriteString("# Import to target (select correct DB first)\n")
	sb.WriteString("redis-cli -n <target-db> RESTORE <key> 0 \"$(cat $key.dump)\"\n")
	sb.WriteString("```\n\n")

	sb.WriteString("Option C: Using redis-dump-go (for full database migration):\n")
	sb.WriteString("```bash\n")
	sb.WriteString("# Install redis-dump-go\n")
	sb.WriteString("go install github.com/yannh/redis-dump-go@latest\n\n")
	sb.WriteString("# Export from source\n")
	sb.WriteString("redis-dump-go -host <source-host> -port 6379 > backup.resp\n\n")
	sb.WriteString("# Import to target (pipe to specific DB)\n")
	sb.WriteString("cat backup.resp | redis-cli -n <target-db> --pipe\n")
	sb.WriteString("```\n\n")

	sb.WriteString("### 3. Migrate Data from Memorystore (GCP)\n\n")
	sb.WriteString("```bash\n")
	sb.WriteString("# Export from Memorystore (requires RDB export)\n")
	sb.WriteString("# 1. Export to GCS bucket via Console or gcloud\n")
	sb.WriteString("# 2. Download the RDB file\n")
	sb.WriteString("# 3. Restore to local Redis\n\n")
	sb.WriteString("# Copy RDB file into container\n")
	sb.WriteString("docker cp dump.rdb $(docker compose ps -q redis):/data/\n\n")
	sb.WriteString("# Restart Redis to load RDB\n")
	sb.WriteString("docker compose restart redis\n")
	sb.WriteString("```\n\n")

	sb.WriteString("### 4. Migrate Data from Azure Cache for Redis\n\n")
	sb.WriteString("```bash\n")
	sb.WriteString("# Export from Azure Cache (requires Premium tier or Enterprise)\n")
	sb.WriteString("# 1. Export via Azure Portal to blob storage\n")
	sb.WriteString("# 2. Download the RDB file\n")
	sb.WriteString("# 3. Follow the RDB restore steps above\n")
	sb.WriteString("```\n\n")

	sb.WriteString("### 5. Update Application Connection Strings\n\n")
	sb.WriteString("Update your applications to use the new Redis endpoint with the correct database number:\n\n")
	sb.WriteString("```\n")
	sb.WriteString("# Format: redis://[password@]host:port/database\n")
	sb.WriteString("redis://localhost:6379/<DB_NUMBER>\n\n")
	sb.WriteString("# With password\n")
	sb.WriteString("redis://:yourpassword@localhost:6379/<DB_NUMBER>\n")
	sb.WriteString("```\n\n")

	sb.WriteString("See `CACHE_MAPPING.md` for the complete mapping of source caches to database numbers.\n\n")

	sb.WriteString("### 6. Verify Migration\n\n")
	sb.WriteString("```bash\n")
	sb.WriteString("# Check keyspace info\n")
	sb.WriteString("redis-cli INFO keyspace\n\n")
	sb.WriteString("# Check specific database\n")
	sb.WriteString("redis-cli -n <DB_NUMBER> DBSIZE\n\n")
	sb.WriteString("# Scan keys in a database\n")
	sb.WriteString("redis-cli -n <DB_NUMBER> SCAN 0\n")
	sb.WriteString("```\n\n")

	sb.WriteString("## Environment Variables\n\n")
	sb.WriteString("| Variable | Description | Default |\n")
	sb.WriteString("|----------|-------------|----------|\n")
	sb.WriteString("| REDIS_PASSWORD | Redis password | (empty) |\n")
	sb.WriteString("| REDIS_COMMANDER_USER | Web UI username | admin |\n")
	sb.WriteString("| REDIS_COMMANDER_PASSWORD | Web UI password | admin |\n\n")

	sb.WriteString("## Troubleshooting\n\n")

	sb.WriteString("### Connection Issues\n")
	sb.WriteString("- Ensure Redis container is running: `docker compose ps`\n")
	sb.WriteString("- Check logs: `docker compose logs redis`\n")
	sb.WriteString("- Verify port is accessible: `redis-cli ping`\n\n")

	sb.WriteString("### Memory Issues\n")
	sb.WriteString("- Check memory usage: `redis-cli INFO memory`\n")
	sb.WriteString("- Adjust maxmemory in redis.conf if needed\n")
	sb.WriteString("- Consider enabling key eviction policy\n\n")

	sb.WriteString("### Persistence Issues\n")
	sb.WriteString("- Check AOF status: `redis-cli INFO persistence`\n")
	sb.WriteString("- Verify data directory permissions\n")
	sb.WriteString("- Monitor disk space for AOF growth\n")

	return []byte(sb.String())
}

// isCacheResource checks if a resource type is a cache resource.
func isCacheResource(resourceType string) bool {
	cacheTypes := []string{
		// AWS
		"aws_elasticache_cluster",
		"aws_elasticache_replication_group",
		"aws_elasticache_serverless_cache",
		// GCP
		"google_redis_instance",
		"google_memcache_instance",
		// Azure
		"azurerm_redis_cache",
		"azurerm_redis_enterprise_cluster",
		"azurerm_redis_enterprise_database",
	}

	for _, t := range cacheTypes {
		if strings.Contains(strings.ToLower(resourceType), strings.ToLower(t)) {
			return true
		}
	}

	// Also check for generic cache indicators
	lowerType := strings.ToLower(resourceType)
	return strings.Contains(lowerType, "cache") ||
		strings.Contains(lowerType, "redis") ||
		strings.Contains(lowerType, "memcache") ||
		strings.Contains(lowerType, "elasticache") ||
		strings.Contains(lowerType, "memorystore")
}
