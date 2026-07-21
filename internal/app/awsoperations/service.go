package awsoperations

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/homeport/homeport/internal/app/migrate"
)

type ActivationInput struct {
	DiscoveryID   string
	TargetStackID string
	Activated     []ServiceKey
	LocalBindings []LocalResourceBinding
}

type Service struct {
	discoveries *migrate.StateStore
	workspaces  WorkspaceStore
}

func NewService(discoveries *migrate.StateStore, workspaces WorkspaceStore) *Service {
	return &Service{discoveries: discoveries, workspaces: workspaces}
}

func (s *Service) Activate(input ActivationInput) (*Workspace, error) {
	if input.DiscoveryID == "" {
		return nil, fmt.Errorf("discovery ID is required")
	}
	if input.TargetStackID == "" {
		return nil, fmt.Errorf("target stack ID is required")
	}
	if s.discoveries == nil || s.workspaces == nil {
		return nil, fmt.Errorf("AWS operations service is not configured")
	}
	discovery, eligible, bindings, err := s.activationData(input)
	if err != nil {
		return nil, err
	}

	selected := make(map[ServiceKey]bool, len(input.Activated))
	for _, service := range input.Activated {
		selected[service] = true
	}
	if err := applyLocalBindings(bindings, input.LocalBindings, input.TargetStackID, selected); err != nil {
		return nil, err
	}
	if existing, err := s.workspaces.GetByDiscoveryID(input.DiscoveryID); err == nil {
		return existing, nil
	}
	services := make(map[ServiceKey]ServiceState, len(eligible))
	for _, metadata := range RegisteredServices() {
		if !eligible[metadata.Key] {
			continue
		}
		capabilities := capabilitiesFor(metadata.Key)
		status := ServiceStatusUnavailable
		reason := "Cutover completed without an attested local binding."
		if selected[metadata.Key] && len(capabilities) > 0 {
			status = ServiceStatusAvailable
			reason = ""
		} else if selected[metadata.Key] {
			reason = fmt.Sprintf("Local target %s has no supported Homeport operations driver.", metadata.Target)
		}
		services[metadata.Key] = ServiceState{Status: status, Capabilities: capabilities, Reason: reason}
	}

	workspace := &Workspace{
		ID:                 uuid.NewString(),
		DiscoveryID:        discovery.ID,
		Name:               discovery.Name,
		Provider:           discovery.Provider,
		CutoverCompletedAt: time.Now().UTC(),
		Services:           services,
		Bindings:           bindings,
	}
	persisted, err := s.workspaces.Create(workspace)
	if err != nil {
		return nil, fmt.Errorf("persist AWS operations workspace: %w", err)
	}
	return persisted, nil
}

// ValidateActivation confirms that the requested services were discovered in AWS.
func (s *Service) ValidateActivation(input ActivationInput) error {
	_, _, bindings, err := s.activationData(input)
	if err != nil {
		return err
	}
	selected := make(map[ServiceKey]bool, len(input.Activated))
	for _, service := range input.Activated {
		selected[service] = true
	}
	return applyLocalBindings(bindings, input.LocalBindings, input.TargetStackID, selected)
}

func (s *Service) GetByDiscoveryID(discoveryID string) (*Workspace, error) {
	if s.workspaces == nil {
		return nil, fmt.Errorf("AWS operations service is not configured")
	}
	return s.workspaces.GetByDiscoveryID(discoveryID)
}

// Get returns one post-cutover AWS workspace by its operation ID.
func (s *Service) Get(id string) (*Workspace, error) {
	if s.workspaces == nil {
		return nil, fmt.Errorf("AWS operations service is not configured")
	}
	return s.workspaces.Get(id)
}

// List returns all persisted AWS operation workspaces.
func (s *Service) List() ([]*Workspace, error) {
	if s.workspaces == nil {
		return nil, fmt.Errorf("AWS operations service is not configured")
	}
	return s.workspaces.List()
}

func (s *Service) activationData(input ActivationInput) (*migrate.DiscoveryState, map[ServiceKey]bool, []ResourceBinding, error) {
	if input.DiscoveryID == "" {
		return nil, nil, nil, fmt.Errorf("discovery ID is required")
	}
	if s.discoveries == nil {
		return nil, nil, nil, fmt.Errorf("AWS operations service is not configured")
	}
	discovery, err := s.discoveries.Get(input.DiscoveryID)
	if err != nil {
		return nil, nil, nil, err
	}
	if discovery.Provider != "aws" {
		return nil, nil, nil, fmt.Errorf("AWS operations require an AWS discovery")
	}

	eligible := make(map[ServiceKey]bool)
	bindings := make([]ResourceBinding, 0)
	for _, resource := range discovery.Resources {
		service, ok := ServiceForResource(resource.Type)
		if !ok {
			continue
		}
		eligible[service] = true
		bindings = append(bindings, ResourceBinding{
			ImportedResourceID: resource.ID,
			Service:            service,
			LocalStackID:       input.TargetStackID,
			Name:               resource.Name,
			Region:             resource.Region,
			Tags:               cloneTags(resource.Tags),
		})
	}
	for _, service := range input.Activated {
		if !eligible[service] {
			return nil, nil, nil, fmt.Errorf("AWS service %q was not found in discovery", service)
		}
	}
	return discovery, eligible, bindings, nil
}

func applyLocalBindings(bindings []ResourceBinding, locals []LocalResourceBinding, targetStackID string, selected map[ServiceKey]bool) error {
	byImportedID := make(map[string]int, len(bindings))
	for i, binding := range bindings {
		byImportedID[binding.ImportedResourceID] = i
	}
	seen := make(map[string]bool, len(locals))
	for _, local := range locals {
		if local.ImportedResourceID == "" || local.LocalResourceID == "" || local.LocalStackID == "" {
			return fmt.Errorf("trusted local binding requires imported resource ID, local resource ID, and local stack ID")
		}
		if local.LocalStackID != targetStackID {
			return fmt.Errorf("trusted local binding stack %q does not match target stack %q", local.LocalStackID, targetStackID)
		}
		index, exists := byImportedID[local.ImportedResourceID]
		if !exists {
			return fmt.Errorf("trusted local binding references undiscovered resource %q", local.ImportedResourceID)
		}
		if seen[local.ImportedResourceID] {
			return fmt.Errorf("duplicate trusted local binding for resource %q", local.ImportedResourceID)
		}
		seen[local.ImportedResourceID] = true
		bindings[index].LocalResourceID = local.LocalResourceID
	}
	for _, binding := range bindings {
		if selected[binding.Service] && binding.LocalResourceID == "" {
			return fmt.Errorf("missing trusted local binding for selected %s resource %q", binding.Service, binding.ImportedResourceID)
		}
	}
	return nil
}

func capabilitiesFor(service ServiceKey) []Capability {
	switch service {
	case ServiceLambda:
		// A post-cutover workspace only manages resources whose local identity was
		// attested by cutover. Browser-driven creation cannot establish that
		// identity, so it is deliberately not advertised as an operation.
		return []Capability{CapabilityList, CapabilityRead, CapabilityUpdate, CapabilityDelete, CapabilityInvoke, CapabilityLogs}
	case ServiceSQS:
		return []Capability{CapabilityList, CapabilityRead, CapabilityDelete, CapabilityPurge, CapabilityRetry}
	default:
		return []Capability{}
	}
}

func cloneTags(tags map[string]string) map[string]string {
	if tags == nil {
		return nil
	}
	copy := make(map[string]string, len(tags))
	for key, value := range tags {
		copy[key] = value
	}
	return copy
}
