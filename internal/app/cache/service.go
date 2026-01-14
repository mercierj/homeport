// Package cache provides Redis cache management functionality.
package cache

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// KeyType represents the type of a Redis key.
type KeyType string

const (
	KeyTypeString KeyType = "string"
	KeyTypeList   KeyType = "list"
	KeyTypeSet    KeyType = "set"
	KeyTypeZSet   KeyType = "zset"
	KeyTypeHash   KeyType = "hash"
	KeyTypeStream KeyType = "stream"
	KeyTypeNone   KeyType = "none"
)

// CacheKey represents a Redis key with its metadata.
type CacheKey struct {
	Key   string  `json:"key"`
	Type  KeyType `json:"type"`
	TTL   int64   `json:"ttl"`
	Size  int64   `json:"size"`
	Value string  `json:"value,omitempty"`
}

// KeyInfo provides detailed information about a key.
type KeyInfo struct {
	Key         string  `json:"key"`
	Type        KeyType `json:"type"`
	TTL         int64   `json:"ttl"`
	MemoryUsage int64   `json:"memory_usage"`
	Encoding    string  `json:"encoding"`
	Length      int64   `json:"length"`
}

// CacheStats provides Redis instance statistics.
type CacheStats struct {
	KeysCount        int64   `json:"keys_count"`
	MemoryUsed       int64   `json:"memory_used"`
	MemoryPeak       int64   `json:"memory_peak"`
	MemoryHuman      string  `json:"memory_human"`
	ConnectedClients int64   `json:"connected_clients"`
	HitRate          float64 `json:"hit_rate"`
	Hits             int64   `json:"hits"`
	Misses           int64   `json:"misses"`
	Uptime           int64   `json:"uptime_seconds"`
	Version          string  `json:"version"`
}

// ScanResult represents the result of a key scan operation.
type ScanResult struct {
	Keys    []CacheKey `json:"keys"`
	Cursor  uint64     `json:"cursor"`
	HasMore bool       `json:"has_more"`
}

// Config holds Redis connection configuration.
type Config struct {
	Address  string
	Password string
	DB       int
}

// Service provides Redis cache operations.
type Service struct {
	client *redis.Client
}

// NewService creates a new cache service connected to Redis.
func NewService(cfg Config) (*Service, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Address,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &Service{client: client}, nil
}

// Close closes the Redis connection.
func (s *Service) Close() error {
	return s.client.Close()
}

// ListKeys scans Redis keys matching the given pattern.
func (s *Service) ListKeys(ctx context.Context, stackID, pattern string, limit int64, cursor uint64) (*ScanResult, error) {
	if pattern == "" {
		pattern = "*"
	}

	if stackID != "" && stackID != "default" {
		pattern = stackID + ":" + pattern
	}

	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	keys, nextCursor, err := s.client.Scan(ctx, cursor, pattern, limit).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to scan keys: %w", err)
	}

	cacheKeys := make([]CacheKey, 0, len(keys))
	for _, key := range keys {
		keyType, err := s.client.Type(ctx, key).Result()
		if err != nil {
			continue
		}

		ttl, err := s.client.TTL(ctx, key).Result()
		if err != nil {
			ttl = -2 * time.Second
		}

		size, _ := s.client.MemoryUsage(ctx, key).Result()

		cacheKeys = append(cacheKeys, CacheKey{
			Key:  key,
			Type: KeyType(keyType),
			TTL:  int64(ttl.Seconds()),
			Size: size,
		})
	}

	return &ScanResult{
		Keys:    cacheKeys,
		Cursor:  nextCursor,
		HasMore: nextCursor != 0,
	}, nil
}

// GetKey retrieves a key's value and metadata.
func (s *Service) GetKey(ctx context.Context, stackID, key string) (*CacheKey, error) {
	fullKey := key
	if stackID != "" && stackID != "default" && !strings.HasPrefix(key, stackID+":") {
		fullKey = stackID + ":" + key
	}

	keyType, err := s.client.Type(ctx, fullKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get key type: %w", err)
	}
	if keyType == "none" {
		return nil, fmt.Errorf("key not found: %s", key)
	}

	ttl, err := s.client.TTL(ctx, fullKey).Result()
	if err != nil {
		ttl = -2 * time.Second
	}

	size, _ := s.client.MemoryUsage(ctx, fullKey).Result()

	var value string
	switch KeyType(keyType) {
	case KeyTypeString:
		value, err = s.client.Get(ctx, fullKey).Result()
		if err != nil && err != redis.Nil {
			return nil, fmt.Errorf("failed to get string value: %w", err)
		}
	case KeyTypeList:
		values, err := s.client.LRange(ctx, fullKey, 0, 100).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to get list value: %w", err)
		}
		value = strings.Join(values, "\n")
	case KeyTypeSet:
		values, err := s.client.SMembers(ctx, fullKey).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to get set value: %w", err)
		}
		value = strings.Join(values, "\n")
	case KeyTypeHash:
		hashMap, err := s.client.HGetAll(ctx, fullKey).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to get hash value: %w", err)
		}
		var pairs []string
		for k, v := range hashMap {
			pairs = append(pairs, fmt.Sprintf("%s: %s", k, v))
		}
		value = strings.Join(pairs, "\n")
	case KeyTypeZSet:
		values, err := s.client.ZRangeWithScores(ctx, fullKey, 0, 100).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to get zset value: %w", err)
		}
		var pairs []string
		for _, z := range values {
			pairs = append(pairs, fmt.Sprintf("%v (score: %v)", z.Member, z.Score))
		}
		value = strings.Join(pairs, "\n")
	case KeyTypeStream:
		value = "[stream data]"
	default:
		value = "[unknown type]"
	}

	return &CacheKey{
		Key:   fullKey,
		Type:  KeyType(keyType),
		TTL:   int64(ttl.Seconds()),
		Size:  size,
		Value: value,
	}, nil
}

// SetKey sets or updates a key with optional TTL.
func (s *Service) SetKey(ctx context.Context, stackID, key, value string, ttl int64) error {
	fullKey := key
	if stackID != "" && stackID != "default" && !strings.HasPrefix(key, stackID+":") {
		fullKey = stackID + ":" + key
	}

	var expiration time.Duration
	if ttl > 0 {
		expiration = time.Duration(ttl) * time.Second
	}

	err := s.client.Set(ctx, fullKey, value, expiration).Err()
	if err != nil {
		return fmt.Errorf("failed to set key: %w", err)
	}

	return nil
}

// DeleteKey deletes a single key.
func (s *Service) DeleteKey(ctx context.Context, stackID, key string) error {
	fullKey := key
	if stackID != "" && stackID != "default" && !strings.HasPrefix(key, stackID+":") {
		fullKey = stackID + ":" + key
	}

	result, err := s.client.Del(ctx, fullKey).Result()
	if err != nil {
		return fmt.Errorf("failed to delete key: %w", err)
	}
	if result == 0 {
		return fmt.Errorf("key not found: %s", key)
	}

	return nil
}

// DeleteKeys deletes keys matching a pattern.
func (s *Service) DeleteKeys(ctx context.Context, stackID, pattern string) (int64, error) {
	if pattern == "" {
		return 0, fmt.Errorf("pattern is required for bulk delete")
	}

	fullPattern := pattern
	if stackID != "" && stackID != "default" {
		fullPattern = stackID + ":" + pattern
	}

	var deleted int64
	var cursor uint64

	for {
		keys, nextCursor, err := s.client.Scan(ctx, cursor, fullPattern, 100).Result()
		if err != nil {
			return deleted, fmt.Errorf("failed to scan keys: %w", err)
		}

		if len(keys) > 0 {
			result, err := s.client.Del(ctx, keys...).Result()
			if err != nil {
				return deleted, fmt.Errorf("failed to delete keys: %w", err)
			}
			deleted += result
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return deleted, nil
}

// GetKeyInfo retrieves detailed information about a key.
func (s *Service) GetKeyInfo(ctx context.Context, stackID, key string) (*KeyInfo, error) {
	fullKey := key
	if stackID != "" && stackID != "default" && !strings.HasPrefix(key, stackID+":") {
		fullKey = stackID + ":" + key
	}

	keyType, err := s.client.Type(ctx, fullKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get key type: %w", err)
	}
	if keyType == "none" {
		return nil, fmt.Errorf("key not found: %s", key)
	}

	ttl, err := s.client.TTL(ctx, fullKey).Result()
	if err != nil {
		ttl = -2 * time.Second
	}

	memoryUsage, _ := s.client.MemoryUsage(ctx, fullKey).Result()
	encoding, _ := s.client.ObjectEncoding(ctx, fullKey).Result()

	var length int64
	switch KeyType(keyType) {
	case KeyTypeString:
		length, _ = s.client.StrLen(ctx, fullKey).Result()
	case KeyTypeList:
		length, _ = s.client.LLen(ctx, fullKey).Result()
	case KeyTypeSet:
		length, _ = s.client.SCard(ctx, fullKey).Result()
	case KeyTypeHash:
		length, _ = s.client.HLen(ctx, fullKey).Result()
	case KeyTypeZSet:
		length, _ = s.client.ZCard(ctx, fullKey).Result()
	case KeyTypeStream:
		length, _ = s.client.XLen(ctx, fullKey).Result()
	}

	return &KeyInfo{
		Key:         fullKey,
		Type:        KeyType(keyType),
		TTL:         int64(ttl.Seconds()),
		MemoryUsage: memoryUsage,
		Encoding:    encoding,
		Length:      length,
	}, nil
}

// GetStats retrieves Redis server statistics.
func (s *Service) GetStats(ctx context.Context, stackID string) (*CacheStats, error) {
	info, err := s.client.Info(ctx, "stats", "memory", "server", "clients", "keyspace").Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get Redis info: %w", err)
	}

	stats := &CacheStats{}

	lines := strings.Split(info, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		key, value := parts[0], parts[1]

		switch key {
		case "used_memory":
			stats.MemoryUsed, _ = strconv.ParseInt(value, 10, 64)
		case "used_memory_peak":
			stats.MemoryPeak, _ = strconv.ParseInt(value, 10, 64)
		case "used_memory_human":
			stats.MemoryHuman = value
		case "connected_clients":
			stats.ConnectedClients, _ = strconv.ParseInt(value, 10, 64)
		case "keyspace_hits":
			stats.Hits, _ = strconv.ParseInt(value, 10, 64)
		case "keyspace_misses":
			stats.Misses, _ = strconv.ParseInt(value, 10, 64)
		case "uptime_in_seconds":
			stats.Uptime, _ = strconv.ParseInt(value, 10, 64)
		case "redis_version":
			stats.Version = value
		}
	}

	total := stats.Hits + stats.Misses
	if total > 0 {
		stats.HitRate = float64(stats.Hits) / float64(total) * 100
	}

	dbSize, err := s.client.DBSize(ctx).Result()
	if err == nil {
		stats.KeysCount = dbSize
	}

	return stats, nil
}
