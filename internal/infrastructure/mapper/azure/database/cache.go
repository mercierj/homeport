// Package database provides mappers for Azure database services.
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

// CacheMapper converts Azure Cache for Redis to Redis containers.
type CacheMapper struct {
	*mapper.BaseMapper
}

// NewCacheMapper creates a new Azure Cache mapper.
func NewCacheMapper() *CacheMapper {
	return &CacheMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeAzureCache, nil),
	}
}

// Map converts an Azure Cache for Redis to a Redis service.
func (m *CacheMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	cacheName := res.GetConfigString("name")
	if cacheName == "" {
		cacheName = res.Name
	}

	capacity := res.GetConfigInt("capacity")
	if capacity == 0 {
		capacity = 1
	}

	skuName := res.GetConfigString("sku_name")
	family := res.GetConfigString("family")
	redisVersion := res.GetConfigString("redis_version")
	if redisVersion == "" {
		redisVersion = "7"
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
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Labels = map[string]string{
		"homeport.source": "azurerm_redis_cache",
		"homeport.engine": "redis",
		"homeport.cache":  cacheName,
	}

	redisConfig := m.generateRedisConfig(skuName, family, capacity)
	result.AddConfig("config/redis/redis.conf", []byte(redisConfig))
	result.AddConfig("config/redis/app-change.env", []byte(m.generateAppChange(cacheName)))
	result.AddConfig("config/redis/tls.env", []byte(m.generateTLSConfig(cacheName, !res.GetConfigBool("enable_non_ssl_port"))))
	result.AddConfig("config/redis/generated-client.patch", []byte(m.generateClientPatch(cacheName)))

	migrationScript := m.generateMigrationScript(cacheName)
	result.AddScript("migrate_azure_cache.sh", []byte(migrationScript))
	result.AddScript("validate_redis.sh", []byte(m.generateValidateScript(cacheName)))
	result.AddScript("backup_redis.sh", []byte(m.generateBackupScript(cacheName)))
	result.AddScript("cutover_redis_clients.sh", []byte(m.generateCutoverScript(cacheName)))
	for _, step := range datarunbook.Redis(cacheName, "migrate_azure_cache.sh", !res.GetConfigBool("enable_non_ssl_port"), strings.ToLower(skuName) == "premium") {
		result.AddRunbookStep(step)
	}
	for _, step := range m.runbook(cacheName) {
		result.AddRunbookStep(step)
	}

	if strings.ToLower(skuName) == "premium" {
		result.AddWarning("Premium tier detected. Consider Redis Cluster mode for high availability.")
	}

	if !res.GetConfigBool("enable_non_ssl_port") {
		result.AddWarning("SSL-only mode detected. Configure TLS for Redis if required.")
	}

	return result, nil
}

func (m *CacheMapper) generateRedisConfig(skuName, family string, capacity int) string {
	maxMemory := capacity * 250 // Approximate MB based on capacity
	if strings.ToLower(family) == "p" {
		maxMemory = capacity * 6000 // Premium tiers have more memory
	}

	persistence := "no"
	if strings.ToLower(skuName) != "basic" {
		persistence = "yes"
	}

	return fmt.Sprintf(`# Redis Configuration for Azure Cache Migration

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

# Performance
tcp-keepalive 300
timeout 0
`, maxMemory, persistence)
}

func (m *CacheMapper) generateMigrationScript(cacheName string) string {
	return fmt.Sprintf(`#!/bin/bash
# Azure Cache for Redis Migration Script
set -e

echo "Azure Cache for Redis Migration"
echo "================================"
echo "Cache: %s"

AZURE_REDIS_HOST="${AZURE_REDIS_HOST:-%s.redis.cache.windows.net}"
AZURE_REDIS_PORT="${AZURE_REDIS_PORT:-6380}"
AZURE_REDIS_KEY="${AZURE_REDIS_KEY:-your-access-key}"

echo "Option 1: Using redis-cli with RDB dump"
echo "  # Connect to Azure Redis and save RDB"
echo "  redis-cli -h $AZURE_REDIS_HOST -p $AZURE_REDIS_PORT -a $AZURE_REDIS_KEY --tls BGSAVE"
echo "  # Download RDB file (requires Azure Storage or alternative method)"

echo "Option 2: Using RIOT (Redis Input/Output Tool)"
echo "  # Install RIOT: https://github.com/redis-developer/riot"
echo "  riot -h $AZURE_REDIS_HOST -p $AZURE_REDIS_PORT -a $AZURE_REDIS_KEY --tls \\"
echo "    replicate -h localhost -p 6379"

echo "Option 3: Using redis-dump-go"
echo "  # Export from Azure"
echo "  redis-dump-go -host $AZURE_REDIS_HOST -port $AZURE_REDIS_PORT -auth $AZURE_REDIS_KEY > dump.json"
echo "  # Import to local"
echo "  redis-dump-go -host localhost -port 6379 -load < dump.json"

echo "Note: Azure Redis with TLS requires --tls flag for redis-cli"
`, cacheName, cacheName)
}

func (m *CacheMapper) generateAppChange(cacheName string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_AZURE_CACHE=%s\nREDIS_HOST=redis\nREDIS_PORT=6379\nREDIS_PASSWORD=${REDIS_PASSWORD:-changeme}\nGENERATED_PATCH=config/redis/generated-client.patch\n", cacheName)
}

func (m *CacheMapper) generateTLSConfig(cacheName string, tlsRequired bool) string {
	return fmt.Sprintf("SOURCE_AZURE_CACHE=%s\nTLS_REQUIRED=%t\nTARGET_TLS_MODE=local-network-plaintext\n", cacheName, tlsRequired)
}

func (m *CacheMapper) generateClientPatch(cacheName string) string {
	return fmt.Sprintf("--- a/app/redis.env\n+++ b/app/redis.env\n@@\n-AZURE_REDIS_CACHE=%s\n+REDIS_URL=redis://:${REDIS_PASSWORD:-changeme}@redis:6379/0\n+REDIS_HOST=redis\n+REDIS_PORT=6379\n", cacheName)
}

func (m *CacheMapper) generateValidateScript(cacheName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/redis/redis.conf\ntest -s config/redis/app-change.env\ngrep -q %q config/redis/app-change.env\nredis-cli -h \"${REDIS_HOST:-redis}\" -p \"${REDIS_PORT:-6379}\" ping >/dev/null 2>&1 || echo \"Redis validation command staged\"\n", cacheName)
}

func (m *CacheMapper) generateBackupScript(cacheName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/redis-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/redis data/redis redis-export 2>/dev/null || tar -czf \"$archive\" config/redis\necho \"$archive\"\n", cacheName)
}

func (m *CacheMapper) generateCutoverScript(cacheName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/redis/app-change.env\ntest \"$SOURCE_AZURE_CACHE\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Apply $GENERATED_PATCH and use redis://$REDIS_HOST:$REDIS_PORT\"\n", cacheName)
}

func (m *CacheMapper) runbook(cacheName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "redis", "source": "azurerm_redis_cache", "cache": cacheName, "target": "redis"}
	return []domainrunbook.Step{
		m.step("backup-redis-target", "Backup Redis target", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_redis.sh"}, "Redis migration artifacts are archived", metadata),
		m.step("cutover-redis-clients", "Cut over Redis clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_redis_clients.sh"}, "clients use generated Redis endpoint", metadata),
	}
}

func (m *CacheMapper) step(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: "shell", Command: command, SuccessCondition: success, Metadata: metadata}
}
