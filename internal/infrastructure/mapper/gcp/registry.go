// Package gcp provides mappers for Google Cloud Platform resources.
package gcp

import (
	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/infrastructure/mapper/gcp/compute"
	"github.com/agnostech/agnostech/internal/infrastructure/mapper/gcp/database"
	"github.com/agnostech/agnostech/internal/infrastructure/mapper/gcp/messaging"
	"github.com/agnostech/agnostech/internal/infrastructure/mapper/gcp/networking"
	"github.com/agnostech/agnostech/internal/infrastructure/mapper/gcp/security"
	"github.com/agnostech/agnostech/internal/infrastructure/mapper/gcp/storage"
)

// MapperRegistrar is an interface for registering mappers.
type MapperRegistrar interface {
	Register(m mapper.Mapper)
}

// RegisterAll registers all GCP mappers with the provided registry.
func RegisterAll(registry MapperRegistrar) {
	// Compute mappers
	registry.Register(compute.NewGCEMapper())
	registry.Register(compute.NewCloudRunMapper())
	registry.Register(compute.NewCloudFunctionMapper())
	registry.Register(compute.NewGKEMapper())

	// Database mappers
	registry.Register(database.NewCloudSQLMapper())
	registry.Register(database.NewFirestoreMapper())
	registry.Register(database.NewBigtableMapper())
	registry.Register(database.NewMemorystoreMapper())
	registry.Register(database.NewSpannerMapper())

	// Storage mappers
	registry.Register(storage.NewGCSMapper())
	registry.Register(storage.NewPersistentDiskMapper())
	registry.Register(storage.NewFilestoreMapper())

	// Networking mappers
	registry.Register(networking.NewCloudLBMapper())
	registry.Register(networking.NewCloudDNSMapper())
	registry.Register(networking.NewCloudCDNMapper())

	// Security mappers
	registry.Register(security.NewIdentityPlatformMapper())
	registry.Register(security.NewSecretManagerMapper())

	// Messaging mappers
	registry.Register(messaging.NewPubSubMapper())
	registry.Register(messaging.NewCloudTasksMapper())
}
