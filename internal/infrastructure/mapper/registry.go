// Package mapper provides infrastructure for AWS resource mappers.
package mapper

import (
	"context"
	"fmt"
	"sync"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
	"github.com/agnostech/agnostech/internal/infrastructure/mapper/azure"
	"github.com/agnostech/agnostech/internal/infrastructure/mapper/compute"
	"github.com/agnostech/agnostech/internal/infrastructure/mapper/database"
	"github.com/agnostech/agnostech/internal/infrastructure/mapper/gcp"
	"github.com/agnostech/agnostech/internal/infrastructure/mapper/messaging"
	"github.com/agnostech/agnostech/internal/infrastructure/mapper/networking"
	"github.com/agnostech/agnostech/internal/infrastructure/mapper/security"
	"github.com/agnostech/agnostech/internal/infrastructure/mapper/storage"
)

// Registry manages all available resource mappers.
type Registry struct {
	mappers map[resource.Type]mapper.Mapper
	mu      sync.RWMutex
}

// NewRegistry creates a new mapper registry with all default mappers registered.
func NewRegistry() *Registry {
	registry := &Registry{
		mappers: make(map[resource.Type]mapper.Mapper),
	}

	// Register all mappers
	registry.RegisterDefaults()

	return registry
}

// RegisterDefaults registers all default mappers.
func (r *Registry) RegisterDefaults() {
	// ─────────────────────────────────────────────────────
	// AWS Mappers
	// ─────────────────────────────────────────────────────

	// AWS Storage mappers
	r.Register(storage.NewS3Mapper())
	r.Register(storage.NewEFSMapper())
	r.Register(storage.NewEBSMapper())

	// AWS Database mappers
	r.Register(database.NewRDSMapper())
	r.Register(database.NewRDSClusterMapper())
	r.Register(database.NewDynamoDBMapper())
	r.Register(database.NewElastiCacheMapper())

	// AWS Compute mappers
	r.Register(compute.NewEC2Mapper())
	r.Register(compute.NewLambdaMapper())
	r.Register(compute.NewECSMapper())
	r.Register(compute.NewECSTaskDefMapper())
	r.Register(compute.NewEKSMapper())

	// AWS Networking mappers
	r.Register(networking.NewALBMapper())
	r.Register(networking.NewAPIGatewayMapper())
	r.Register(networking.NewCloudFrontMapper())
	r.Register(networking.NewRoute53Mapper())
	r.Register(networking.NewVPCMapper())

	// AWS Security mappers
	r.Register(security.NewCognitoMapper())
	r.Register(security.NewSecretsManagerMapper())
	r.Register(security.NewACMMapper())
	r.Register(security.NewIAMMapper())

	// AWS Messaging mappers
	r.Register(messaging.NewSQSMapper())
	r.Register(messaging.NewSNSMapper())
	r.Register(messaging.NewEventBridgeMapper())
	r.Register(messaging.NewKinesisMapper())

	// ─────────────────────────────────────────────────────
	// GCP Mappers
	// ─────────────────────────────────────────────────────
	gcp.RegisterAll(r)

	// ─────────────────────────────────────────────────────
	// Azure Mappers
	// ─────────────────────────────────────────────────────
	azure.RegisterAll(r)
}

// Register registers a mapper for a specific resource type.
func (r *Registry) Register(m mapper.Mapper) {
	r.mu.Lock()
	defer r.mu.Unlock()

	resourceType := m.ResourceType()
	r.mappers[resourceType] = m
}

// Get retrieves a mapper for the given resource type.
func (r *Registry) Get(resourceType resource.Type) (mapper.Mapper, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	m, ok := r.mappers[resourceType]
	if !ok {
		return nil, fmt.Errorf("no mapper registered for resource type: %s", resourceType)
	}

	return m, nil
}

// Map maps an AWS resource to self-hosted alternatives using the appropriate mapper.
func (r *Registry) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	m, err := r.Get(res.Type)
	if err != nil {
		return nil, err
	}

	return m.Map(ctx, res)
}

// MapBatch maps multiple AWS resources to self-hosted alternatives.
func (r *Registry) MapBatch(ctx context.Context, resources []*resource.AWSResource) ([]*mapper.MappingResult, error) {
	results := make([]*mapper.MappingResult, 0, len(resources))

	for _, res := range resources {
		result, err := r.Map(ctx, res)
		if err != nil {
			// Continue mapping other resources even if one fails
			// Add error as a warning in a dummy result
			result = mapper.NewMappingResult("error")
			result.AddWarning(fmt.Sprintf("Failed to map resource %s: %v", res.ID, err))
		}
		results = append(results, result)
	}

	return results, nil
}

// HasMapper returns true if a mapper is registered for the given resource type.
func (r *Registry) HasMapper(resourceType resource.Type) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, ok := r.mappers[resourceType]
	return ok
}

// SupportedTypes returns a list of all supported resource types.
func (r *Registry) SupportedTypes() []resource.Type {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]resource.Type, 0, len(r.mappers))
	for t := range r.mappers {
		types = append(types, t)
	}

	return types
}

// Unregister removes a mapper for the given resource type.
func (r *Registry) Unregister(resourceType resource.Type) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.mappers, resourceType)
}

// Clear removes all registered mappers.
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.mappers = make(map[resource.Type]mapper.Mapper)
}

// GlobalRegistry is the default global registry instance.
var GlobalRegistry = NewRegistry()
