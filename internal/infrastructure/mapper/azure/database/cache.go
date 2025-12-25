// Package database provides mappers for Azure database services.
package database

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
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
	svc.Labels = map[string]string{
		"cloudexit.source": "azurerm_redis_cache",
		"cloudexit.engine": "redis",
		"cloudexit.cache":  cacheName,
	}

	redisConfig := m.generateRedisConfig(skuName, family, capacity)
	result.AddConfig("config/redis/redis.conf", []byte(redisConfig))

	migrationScript := m.generateMigrationScript(cacheName)
	result.AddScript("migrate_azure_cache.sh", []byte(migrationScript))

	if strings.ToLower(skuName) == "premium" {
		result.AddWarning("Premium tier detected. Consider Redis Cluster mode for high availability.")
		result.AddManualStep("Configure Redis Sentinel or Redis Cluster for HA")
	}

	if res.GetConfigBool("enable_non_ssl_port") == false {
		result.AddWarning("SSL-only mode detected. Configure TLS for Redis if required.")
	}

	result.AddManualStep("Update Redis connection string in your application")
	result.AddManualStep("Export data using RDB dump or Redis CLI")
	result.AddManualStep("Import data to local Redis")

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
