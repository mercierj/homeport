package mapper

import (
	"sync"

	"github.com/agnostech/agnostech/internal/domain/resource"
)

// Registry manages the collection of available mappers.
// It provides thread-safe registration and retrieval of mappers.
type Registry struct {
	mu      sync.RWMutex
	mappers map[resource.Type]Mapper
}

// NewRegistry creates a new mapper registry.
func NewRegistry() *Registry {
	return &Registry{
		mappers: make(map[resource.Type]Mapper),
	}
}

// Register registers a mapper for a specific resource type.
// Returns an error if a mapper is already registered for this type.
func (r *Registry) Register(mapper Mapper) error {
	if mapper == nil {
		return WrapError("cannot register nil mapper", nil)
	}

	resourceType := mapper.ResourceType()
	if !resourceType.IsValid() {
		return NewErrUnsupportedResource(resourceType)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.mappers[resourceType]; exists {
		return NewErrMapperAlreadyRegistered(resourceType)
	}

	r.mappers[resourceType] = mapper
	return nil
}

// MustRegister registers a mapper and panics if registration fails.
// Useful for initialization code where failure is not acceptable.
func (r *Registry) MustRegister(mapper Mapper) {
	if err := r.Register(mapper); err != nil {
		panic(err)
	}
}

// Get retrieves a mapper for the given resource type.
// Returns an error if no mapper is found.
func (r *Registry) Get(resourceType resource.Type) (Mapper, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	mapper, exists := r.mappers[resourceType]
	if !exists {
		return nil, NewErrMapperNotFound(resourceType)
	}

	return mapper, nil
}

// Has checks if a mapper exists for the given resource type.
func (r *Registry) Has(resourceType resource.Type) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, exists := r.mappers[resourceType]
	return exists
}

// All returns all registered mappers.
// The returned map is a copy and can be safely modified.
func (r *Registry) All() map[resource.Type]Mapper {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[resource.Type]Mapper, len(r.mappers))
	for k, v := range r.mappers {
		result[k] = v
	}
	return result
}

// Types returns all registered resource types.
func (r *Registry) Types() []resource.Type {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]resource.Type, 0, len(r.mappers))
	for t := range r.mappers {
		types = append(types, t)
	}
	return types
}

// Count returns the number of registered mappers.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.mappers)
}

// Unregister removes a mapper for the given resource type.
// Returns true if a mapper was removed, false if none existed.
func (r *Registry) Unregister(resourceType resource.Type) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.mappers[resourceType]; exists {
		delete(r.mappers, resourceType)
		return true
	}
	return false
}

// Clear removes all registered mappers.
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.mappers = make(map[resource.Type]Mapper)
}

// DefaultRegistry is the global mapper registry.
var DefaultRegistry = NewRegistry()

// Register registers a mapper in the default registry.
func Register(mapper Mapper) error {
	return DefaultRegistry.Register(mapper)
}

// MustRegister registers a mapper in the default registry and panics on error.
func MustRegister(mapper Mapper) {
	DefaultRegistry.MustRegister(mapper)
}

// Get retrieves a mapper from the default registry.
func Get(resourceType resource.Type) (Mapper, error) {
	return DefaultRegistry.Get(resourceType)
}

// Has checks if a mapper exists in the default registry.
func Has(resourceType resource.Type) bool {
	return DefaultRegistry.Has(resourceType)
}

// All returns all mappers from the default registry.
func All() map[resource.Type]Mapper {
	return DefaultRegistry.All()
}

// Types returns all registered resource types from the default registry.
func Types() []resource.Type {
	return DefaultRegistry.Types()
}

// Count returns the number of registered mappers in the default registry.
func Count() int {
	return DefaultRegistry.Count()
}
