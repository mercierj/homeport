// Package gcp provides mappers for Google Cloud Platform resources.
package gcp

import (
	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/infrastructure/mapper/gcp/compute"
	"github.com/homeport/homeport/internal/infrastructure/mapper/gcp/database"
	"github.com/homeport/homeport/internal/infrastructure/mapper/gcp/devops"
	"github.com/homeport/homeport/internal/infrastructure/mapper/gcp/messaging"
	"github.com/homeport/homeport/internal/infrastructure/mapper/gcp/networking"
	"github.com/homeport/homeport/internal/infrastructure/mapper/gcp/observability"
	"github.com/homeport/homeport/internal/infrastructure/mapper/gcp/security"
	"github.com/homeport/homeport/internal/infrastructure/mapper/gcp/storage"
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
	registry.Register(compute.NewAppEngineMapper())
	registry.Register(compute.NewCloudSchedulerMapper())
	registry.Register(compute.NewArtifactRegistryMapper())
	registry.Register(compute.NewTPUNodeMapper())
	registry.Register(compute.NewTPUV2VMMapper())
	registry.Register(compute.NewVertexAIMapper())
	registry.Register(devops.NewCloudBuildMapper())
	registry.Register(devops.NewCloudDeployPipelineMapper())
	registry.Register(devops.NewCloudDeployTargetMapper())
	registry.Register(devops.NewComposerMapper())
	registry.Register(devops.NewDataflowMapper())
	registry.Register(devops.NewDataprocMapper())
	registry.Register(devops.NewWorkflowsMapper())

	// Database mappers
	registry.Register(database.NewCloudSQLMapper())
	registry.Register(database.NewFirestoreMapper())
	registry.Register(database.NewBigtableMapper())
	registry.Register(database.NewMemorystoreMapper())
	registry.Register(database.NewSpannerMapper())
	registry.Register(database.NewBigQueryMapper())
	registry.Register(database.NewDataplexLakeMapper())
	registry.Register(database.NewDataplexZoneMapper())
	registry.Register(database.NewLookerMapper())

	// Storage mappers
	registry.Register(storage.NewGCSMapper())
	registry.Register(storage.NewPersistentDiskMapper())
	registry.Register(storage.NewFilestoreMapper())

	// Networking mappers
	registry.Register(networking.NewCloudLBMapper())
	registry.Register(networking.NewCloudDNSMapper())
	registry.Register(networking.NewCloudCDNMapper())
	registry.Register(networking.NewVPCMapper())
	registry.Register(networking.NewApigeeMapper())

	// Security mappers
	registry.Register(security.NewIdentityPlatformMapper())
	registry.Register(security.NewSecretManagerMapper())
	registry.Register(security.NewCloudArmorMapper())
	registry.Register(security.NewIAMMapper())

	// Messaging mappers
	registry.Register(messaging.NewPubSubMapper())
	registry.Register(messaging.NewPubSubSubscriptionMapper())
	registry.Register(messaging.NewCloudTasksMapper())
	registry.Register(messaging.NewEventarcMapper())

	// Observability mappers
	registry.Register(observability.NewCloudLoggingMapper())
	registry.Register(observability.NewCloudMonitoringAlertPolicyMapper())
	registry.Register(observability.NewCloudMonitoringDashboardMapper())
	registry.Register(observability.NewCloudTraceMapper())
	registry.Register(observability.NewErrorReportingMapper())
	registry.Register(observability.NewProfilerMapper())
}
