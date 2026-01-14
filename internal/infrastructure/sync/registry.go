package sync

import (
	"github.com/homeport/homeport/internal/domain/sync"
)

// NewDefaultRegistry creates a strategy registry with all built-in sync strategies registered.
func NewDefaultRegistry() *sync.StrategyRegistry {
	registry := sync.NewStrategyRegistry()

	// Register database sync strategies
	registry.Register(NewPostgresSync())
	registry.Register(NewMySQLSync())

	// Register cache sync strategies
	registry.Register(NewRedisSync())

	return registry
}

// RegisterAllStrategies registers all built-in strategies to an existing registry.
func RegisterAllStrategies(registry *sync.StrategyRegistry) {
	// Database strategies
	registry.Register(NewPostgresSync())
	registry.Register(NewMySQLSync())

	// Cache strategies
	registry.Register(NewRedisSync())
}

// GetDatabaseStrategies returns all database sync strategies.
func GetDatabaseStrategies() []sync.SyncStrategy {
	return []sync.SyncStrategy{
		NewPostgresSync(),
		NewMySQLSync(),
	}
}

// GetCacheStrategies returns all cache sync strategies.
func GetCacheStrategies() []sync.SyncStrategy {
	return []sync.SyncStrategy{
		NewRedisSync(),
	}
}

// GetAllStrategies returns all available sync strategies.
func GetAllStrategies() []sync.SyncStrategy {
	strategies := GetDatabaseStrategies()
	strategies = append(strategies, GetCacheStrategies()...)
	return strategies
}
