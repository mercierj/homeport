package resource

import (
	"fmt"
)

// Infrastructure represents a complete infrastructure configuration
type Infrastructure struct {
	Resources map[string]*Resource `json:"resources"` // Key is resource ID
	Provider  Provider             `json:"provider"`
	Region    string               `json:"region"`
	Metadata  map[string]string    `json:"metadata"`
}

// NewInfrastructure creates a new Infrastructure instance
func NewInfrastructure(provider Provider) *Infrastructure {
	return &Infrastructure{
		Resources: make(map[string]*Resource),
		Provider:  provider,
		Metadata:  make(map[string]string),
	}
}

// AddResource adds a resource to the infrastructure
func (i *Infrastructure) AddResource(r *Resource) {
	i.Resources[r.ID] = r
}

// GetResource retrieves a resource by ID
func (i *Infrastructure) GetResource(id string) (*Resource, error) {
	r, ok := i.Resources[id]
	if !ok {
		return nil, fmt.Errorf("resource not found: %s", id)
	}
	return r, nil
}

// GetResourcesByType returns all resources of a specific type
func (i *Infrastructure) GetResourcesByType(t Type) []*Resource {
	var resources []*Resource
	for _, r := range i.Resources {
		if r.Type == t {
			resources = append(resources, r)
		}
	}
	return resources
}

// GetDependencies returns all resources that the given resource depends on
func (i *Infrastructure) GetDependencies(resourceID string) ([]*Resource, error) {
	r, err := i.GetResource(resourceID)
	if err != nil {
		return nil, err
	}

	var deps []*Resource
	for _, depID := range r.Dependencies {
		dep, err := i.GetResource(depID)
		if err != nil {
			// Skip missing dependencies
			continue
		}
		deps = append(deps, dep)
	}
	return deps, nil
}

// Validate checks if the infrastructure configuration is valid
func (i *Infrastructure) Validate() error {
	// Check for circular dependencies
	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	for id := range i.Resources {
		if !visited[id] {
			if i.hasCyclicDependency(id, visited, recStack) {
				return fmt.Errorf("circular dependency detected involving resource: %s", id)
			}
		}
	}

	return nil
}

// hasCyclicDependency performs DFS to detect cycles
func (i *Infrastructure) hasCyclicDependency(resourceID string, visited, recStack map[string]bool) bool {
	visited[resourceID] = true
	recStack[resourceID] = true

	r, err := i.GetResource(resourceID)
	if err != nil {
		return false
	}

	for _, depID := range r.Dependencies {
		if !visited[depID] {
			if i.hasCyclicDependency(depID, visited, recStack) {
				return true
			}
		} else if recStack[depID] {
			return true
		}
	}

	recStack[resourceID] = false
	return false
}
