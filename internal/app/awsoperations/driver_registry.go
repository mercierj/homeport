package awsoperations

import (
	"context"
	"fmt"
)

// DriverRegistry is the server-owned declaration boundary: every catalogue
// service has a driver, even where its local target cannot yet prove an
// operation. Functional drivers replace their unavailable declaration at
// server construction time.
type DriverRegistry struct {
	drivers map[ServiceKey]Driver
}

func NewDriverRegistry(overrides ...Driver) (*DriverRegistry, error) {
	registry := &DriverRegistry{drivers: make(map[ServiceKey]Driver, len(RegisteredServices()))}
	for _, metadata := range RegisteredServices() {
		registry.drivers[metadata.Key] = UnavailableDriver{metadata: metadata}
	}
	for _, driver := range overrides {
		if driver == nil {
			continue
		}
		if _, found := registry.drivers[driver.Service()]; !found {
			return nil, fmt.Errorf("driver %q is not in the AWS service catalogue", driver.Service())
		}
		registry.drivers[driver.Service()] = driver
	}
	return registry, nil
}

func (r *DriverRegistry) Get(key ServiceKey) (Driver, bool) {
	if r == nil {
		return nil, false
	}
	driver, found := r.drivers[key]
	return driver, found
}

func (r *DriverRegistry) Len() int {
	if r == nil {
		return 0
	}
	return len(r.drivers)
}

// UnavailableResourceRecord exposes only attested discovery/binding data. It
// deliberately contains no browser-provided local identity or pretend AWS
// operation capability.
type UnavailableResourceRecord struct {
	ImportedResourceID string            `json:"imported_resource_id"`
	LocalResourceID    string            `json:"local_resource_id,omitempty"`
	LocalStackID       string            `json:"local_stack_id,omitempty"`
	Name               string            `json:"name"`
	Region             string            `json:"region,omitempty"`
	Tags               map[string]string `json:"tags,omitempty"`
	Target             string            `json:"target"`
	Status             ServiceStatus     `json:"status"`
	Reason             string            `json:"reason"`
}

type UnavailableDriver struct{ metadata ServiceMetadata }

func NewUnavailableDriver(metadata ServiceMetadata) UnavailableDriver {
	return UnavailableDriver{metadata: metadata}
}

func (d UnavailableDriver) Service() ServiceKey { return d.metadata.Key }

func (d UnavailableDriver) Capabilities(Workspace) []Capability { return []Capability{} }

func (d UnavailableDriver) List(_ context.Context, workspace Workspace) ([]any, error) {
	items := make([]any, 0)
	reason := fmt.Sprintf("Local target %s has no supported Homeport operations driver.", d.metadata.Target)
	for _, binding := range bindingsFor(workspace, d.metadata.Key) {
		items = append(items, UnavailableResourceRecord{
			ImportedResourceID: binding.ImportedResourceID,
			LocalResourceID:    binding.LocalResourceID,
			LocalStackID:       binding.LocalStackID,
			Name:               binding.Name,
			Region:             binding.Region,
			Tags:               cloneTags(binding.Tags),
			Target:             d.metadata.Target,
			Status:             ServiceStatusUnavailable,
			Reason:             reason,
		})
	}
	return items, nil
}
