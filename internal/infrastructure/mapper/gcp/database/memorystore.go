// Package database provides mappers for GCP database services.
package database

import (
	"context"
	"fmt"
	"time"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

// MemorystoreMapper converts GCP Memorystore to Redis containers.
type MemorystoreMapper struct {
	*mapper.BaseMapper
}

// NewMemorystoreMapper creates a new Memorystore mapper.
func NewMemorystoreMapper() *MemorystoreMapper {
	return &MemorystoreMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeMemorystore, nil),
	}
}

// Map converts a Memorystore instance to a Redis service.
func (m *MemorystoreMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	instanceName := res.GetConfigString("name")
	if instanceName == "" {
		instanceName = res.Name
	}

	memorySizeGB := res.GetConfigInt("memory_size_gb")
	if memorySizeGB == 0 {
		memorySizeGB = 1
	}

	tier := res.GetConfigString("tier")
	redisVersion := res.GetConfigString("redis_version")
	if redisVersion == "" {
		redisVersion = "7.0"
	}

	result := mapper.NewMappingResult("redis")
	svc := result.DockerService

	svc.Image = fmt.Sprintf("redis:%s-alpine", redisVersion)
	svc.Ports = []string{"6379:6379"}
	svc.Volumes = []string{
		"./data/redis:/data",
		"./config/redis/redis.conf:/usr/local/etc/redis/redis.conf:ro",
	}
	svc.Command = []string{"redis-server", "/usr/local/etc/redis/redis.conf"}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "redis-cli", "ping"},
		Interval: 10 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  5,
	}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"cloudexit.source":   "google_redis_instance",
		"cloudexit.engine":   "redis",
		"cloudexit.instance": instanceName,
	}

	redisConfig := m.generateRedisConfig(memorySizeGB, tier)
	result.AddConfig("config/redis/redis.conf", []byte(redisConfig))

	migrationScript := m.generateMigrationScript(instanceName)
	result.AddScript("migrate_memorystore.sh", []byte(migrationScript))

	if tier == "STANDARD_HA" {
		result.AddWarning("Standard HA tier detected. Consider setting up Redis Sentinel for high availability.")
		result.AddManualStep("Configure Redis Sentinel for automatic failover")
	}

	result.AddManualStep("Update Redis connection string in your application")
	result.AddManualStep("Export and import data using RDB dump or redis-cli --rdb")

	return result, nil
}

func (m *MemorystoreMapper) generateRedisConfig(memorySizeGB int, tier string) string {
	maxMemory := memorySizeGB * 1024
	persistence := "no"
	if tier == "STANDARD_HA" {
		persistence = "yes"
	}

	return fmt.Sprintf(`# Redis Configuration for Memorystore Migration

# Network
bind 0.0.0.0
port 6379
protected-mode no

# Memory
maxmemory %dmb
maxmemory-policy allkeys-lru

# Persistence
appendonly %s
appendfsync everysec
save 900 1
save 300 10
save 60 10000

# Logging
loglevel notice
logfile ""

# Performance
tcp-keepalive 300
timeout 0
`, maxMemory, persistence)
}

func (m *MemorystoreMapper) generateMigrationScript(instanceName string) string {
	return fmt.Sprintf(`#!/bin/bash
# Memorystore to Redis Migration Script
set -e

echo "Memorystore to Redis Migration"
echo "==============================="
echo "Instance: %s"

MEMORYSTORE_HOST="${MEMORYSTORE_HOST:-your-memorystore-ip}"
LOCAL_HOST="localhost"

echo "Option 1: Using RDB dump (recommended for large datasets)"
echo "  # Export from Memorystore using gcloud"
echo "  gcloud redis instances export gs://BUCKET/redis.rdb --instance=INSTANCE --region=REGION"
echo "  gsutil cp gs://BUCKET/redis.rdb ./data/redis/dump.rdb"
echo "  # Restart local Redis to load the dump"
echo "  docker-compose restart redis"

echo "Option 2: Using redis-cli MIGRATE (for live migration)"
echo "  # Connect to Memorystore and dump keys"
echo "  redis-cli -h $MEMORYSTORE_HOST --rdb /tmp/dump.rdb"
echo "  cp /tmp/dump.rdb ./data/redis/"
echo "  docker-compose restart redis"

echo "Option 3: Using RIOT (for complex migrations)"
echo "  # Use RIOT tool for advanced migration scenarios"
echo "  # https://github.com/redis-developer/riot"
`, instanceName)
}
