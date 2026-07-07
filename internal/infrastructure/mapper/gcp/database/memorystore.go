// Package database provides mappers for GCP database services.
package database

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
	"github.com/homeport/homeport/internal/infrastructure/mapper/shared/datarunbook"
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
	redisVersion = normalizeRedisVersion(redisVersion)

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
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"homeport.source":   "google_redis_instance",
		"homeport.engine":   "redis",
		"homeport.instance": instanceName,
	}

	redisConfig := m.generateRedisConfig(memorySizeGB, tier)
	result.AddConfig("config/redis/redis.conf", []byte(redisConfig))

	migrationScript := m.generateMigrationScript(instanceName)
	result.AddScript("migrate_memorystore.sh", []byte(migrationScript))
	result.AddScript("backup_memorystore_config.sh", []byte(m.generateBackupScript(instanceName)))
	result.AddScript("validate_redis.sh", []byte(m.generateValidateScript(instanceName)))
	result.AddConfig("config/redis/app-change.env", []byte(m.generateAppChangeConfig(instanceName)))
	if tier == "STANDARD_HA" {
		result.AddConfig("config/redis/ha.env", []byte(fmt.Sprintf("SOURCE_INSTANCE=%s\nTARGET_REPLICAS=2\nTARGET_FAILOVER=redis-sentinel-or-cluster\n", instanceName)))
	}
	for _, step := range datarunbook.Redis(instanceName, "migrate_memorystore.sh", false, tier == "STANDARD_HA") {
		result.AddRunbookStep(step)
	}
	for _, step := range memorystoreRunbook(instanceName) {
		result.AddRunbookStep(step)
	}

	if tier == "STANDARD_HA" {
		result.AddWarning("Standard HA tier detected. Generated Redis HA handoff config is included.")
	}

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

func (m *MemorystoreMapper) generateAppChangeConfig(instanceName string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=generated_patch
SOURCE_INSTANCE=%s
TARGET_ENDPOINT=redis:6379
TARGET_ENGINE=redis
REDIS_PASSWORD=${REDIS_PASSWORD:-changeme}
`, instanceName)
}

func (m *MemorystoreMapper) generateBackupScript(instanceName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-memorystore-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/redis data/redis
echo "$archive"
`, instanceName)
}

func (m *MemorystoreMapper) generateValidateScript(instanceName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
test -s config/redis/app-change.env
redis-cli -h "${REDIS_HOST:-localhost}" -p "${REDIS_PORT:-6379}" ping
echo "Memorystore Redis target for %s validated"
`, instanceName)
}

func memorystoreRunbook(instanceName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "redis", "source": "google_redis_instance", "instance": instanceName}
	return []domainrunbook.Step{
		memorystoreStep("backup-memorystore-config", "Backup Memorystore config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_memorystore_config.sh"}, "Redis config and data are archived", metadata),
		memorystoreStep("cutover-memorystore-endpoint", "Cut over Memorystore endpoint", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "-c", "test -s config/redis/app-change.env"}, "applications use the generated Redis endpoint", metadata),
	}
}

func memorystoreStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: "shell", Command: command, SuccessCondition: success, Metadata: metadata}
}

func normalizeRedisVersion(version string) string {
	version = strings.TrimPrefix(version, "REDIS_")
	version = strings.ReplaceAll(version, "_", ".")
	return version
}
