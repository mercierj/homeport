// Package azure provides mappers for Microsoft Azure resources.
package azure

import (
	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/infrastructure/mapper/azure/compute"
	"github.com/agnostech/agnostech/internal/infrastructure/mapper/azure/database"
	"github.com/agnostech/agnostech/internal/infrastructure/mapper/azure/messaging"
	"github.com/agnostech/agnostech/internal/infrastructure/mapper/azure/networking"
	"github.com/agnostech/agnostech/internal/infrastructure/mapper/azure/security"
	"github.com/agnostech/agnostech/internal/infrastructure/mapper/azure/storage"
)

// MapperRegistrar is an interface for registering mappers.
type MapperRegistrar interface {
	Register(m mapper.Mapper)
}

// RegisterAll registers all Azure mappers with the provided registry.
func RegisterAll(registry MapperRegistrar) {
	// Compute mappers
	registry.Register(compute.NewVMMapper())
	registry.Register(compute.NewWindowsVMMapper())
	registry.Register(compute.NewFunctionMapper())
	registry.Register(compute.NewAKSMapper())
	registry.Register(compute.NewContainerInstanceMapper())

	// Storage mappers
	registry.Register(storage.NewBlobMapper())
	registry.Register(storage.NewStorageAccountMapper())
	registry.Register(storage.NewManagedDiskMapper())
	registry.Register(storage.NewFilesMapper())

	// Database mappers
	registry.Register(database.NewAzureSQLMapper())
	registry.Register(database.NewPostgresMapper())
	registry.Register(database.NewMySQLMapper())
	registry.Register(database.NewCosmosDBMapper())
	registry.Register(database.NewCacheMapper())

	// Networking mappers
	registry.Register(networking.NewLBMapper())
	registry.Register(networking.NewAppGatewayMapper())
	registry.Register(networking.NewDNSMapper())
	registry.Register(networking.NewCDNMapper())
	registry.Register(networking.NewFrontDoorMapper())
	registry.Register(networking.NewVNetMapper())

	// Security mappers
	registry.Register(security.NewADB2CMapper())
	registry.Register(security.NewKeyVaultMapper())
	registry.Register(security.NewFirewallMapper())

	// Messaging mappers
	registry.Register(messaging.NewServiceBusMapper())
	registry.Register(messaging.NewServiceBusQueueMapper())
	registry.Register(messaging.NewEventHubMapper())
	registry.Register(messaging.NewEventGridMapper())
	registry.Register(messaging.NewLogicAppMapper())
}
