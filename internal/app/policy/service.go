package policy

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/homeport/homeport/internal/domain/policy"
)

// Service provides policy operations.
type Service struct {
	store     *Store
	validator *Validator
}

// Config holds policy service configuration.
type Config struct {
	StorePath string
}

// NewService creates a new policy service.
func NewService(cfg *Config) (*Service, error) {
	path := ""
	if cfg != nil {
		path = cfg.StorePath
	}

	store, err := NewStore(path)
	if err != nil {
		return nil, fmt.Errorf("failed to create store: %w", err)
	}

	return &Service{
		store:     store,
		validator: NewValidator(),
	}, nil
}

// List returns all policies, optionally filtered.
func (s *Service) List(ctx context.Context, filter *policy.PolicyFilter) (*policy.PolicyCollection, error) {
	policies := s.store.List(filter)
	return policy.NewPolicyCollection(policies), nil
}

// Get retrieves a policy by ID.
func (s *Service) Get(ctx context.Context, id string) (*policy.Policy, error) {
	return s.store.Get(id)
}

// Create adds a new policy.
func (s *Service) Create(ctx context.Context, p *policy.Policy) (*policy.Policy, error) {
	// Validate if normalized policy is provided
	if p.NormalizedPolicy != nil {
		if errs := s.validator.Validate(p); len(errs) > 0 {
			return nil, fmt.Errorf("validation failed: %v", errs)
		}
	}

	return s.store.Save(p)
}

// Update modifies an existing policy.
func (s *Service) Update(ctx context.Context, id string, updates *policy.Policy) (*policy.Policy, error) {
	// Validate if normalized policy is provided
	if updates.NormalizedPolicy != nil {
		if errs := s.validator.Validate(updates); len(errs) > 0 {
			return nil, fmt.Errorf("validation failed: %v", errs)
		}
	}

	return s.store.Update(id, updates)
}

// Delete removes a policy.
func (s *Service) Delete(ctx context.Context, id string) error {
	return s.store.Delete(id)
}

// Validate checks a policy for errors.
func (s *Service) Validate(ctx context.Context, p *policy.Policy) (*ValidationResult, error) {
	return s.validator.ValidateWithDetails(p), nil
}

// GetKeycloakPreview generates a Keycloak mapping preview for a policy.
func (s *Service) GetKeycloakPreview(ctx context.Context, id string) (*policy.KeycloakMapping, error) {
	p, err := s.store.Get(id)
	if err != nil {
		return nil, err
	}

	// If mapping already exists, return it
	if p.KeycloakMapping != nil {
		return p.KeycloakMapping, nil
	}

	// Generate mapping
	mapping := s.generateKeycloakMapping(p)
	return mapping, nil
}

// GetSummary returns policy statistics.
func (s *Service) GetSummary(ctx context.Context) (*policy.PolicySummary, error) {
	return s.store.GetSummary(), nil
}

// ImportPolicies imports policies from a mapping result.
func (s *Service) ImportPolicies(ctx context.Context, policies []*policy.Policy) error {
	// Generate Keycloak mappings for each policy
	for _, p := range policies {
		if p.KeycloakMapping == nil && p.Type != policy.PolicyTypeNetwork {
			p.KeycloakMapping = s.generateKeycloakMapping(p)
		}
	}

	return s.store.SaveBatch(policies)
}

// ExportOriginal returns the original document for a policy.
func (s *Service) ExportOriginal(ctx context.Context, id string) (json.RawMessage, string, error) {
	p, err := s.store.Get(id)
	if err != nil {
		return nil, "", err
	}

	format := p.OriginalFormat
	if format == "" {
		format = "json"
	}

	return p.OriginalDocument, format, nil
}

// RegenerateKeycloakMapping regenerates the Keycloak mapping for a policy.
func (s *Service) RegenerateKeycloakMapping(ctx context.Context, id string) (*policy.Policy, error) {
	p, err := s.store.Get(id)
	if err != nil {
		return nil, err
	}

	p.KeycloakMapping = s.generateKeycloakMapping(p)

	return s.store.Update(id, p)
}

// generateKeycloakMapping creates a Keycloak mapping from a policy.
func (s *Service) generateKeycloakMapping(p *policy.Policy) *policy.KeycloakMapping {
	if p.NormalizedPolicy == nil {
		return nil
	}

	// Network policies don't map to Keycloak
	if p.Type == policy.PolicyTypeNetwork {
		return nil
	}

	mapping := &policy.KeycloakMapping{
		Realm:             "homeport",
		Roles:             make([]policy.KeycloakRole, 0),
		Policies:          make([]policy.KeycloakPolicy, 0),
		ManualReviewNotes: make([]string, 0),
		UnmappedActions:   make([]string, 0),
	}

	// Collect all actions from statements
	allActions := make([]string, 0)
	for _, stmt := range p.NormalizedPolicy.Statements {
		allActions = append(allActions, stmt.Actions...)
	}

	// Map actions to scopes
	scopes, unmapped := policy.MapActionsToScopes(p.Provider, allActions)
	mapping.UnmappedActions = unmapped

	// Calculate confidence
	mapping.MappingConfidence = policy.CalculateConfidence(len(allActions), len(unmapped))

	// Create roles based on scopes
	rolesByCategory := make(map[string][]string)
	for _, scope := range scopes {
		// Parse scope like "storage:read" into category and action
		parts := splitScope(scope)
		if len(parts) == 2 {
			rolesByCategory[parts[0]] = append(rolesByCategory[parts[0]], parts[1])
		}
	}

	// Generate roles
	for category, actions := range rolesByCategory {
		role := policy.KeycloakRole{
			Name:          fmt.Sprintf("%s-%s-role", p.ResourceName, category),
			Description:   fmt.Sprintf("Role for %s %s access", p.ResourceName, category),
			SourceActions: actions,
			Attributes:    make(map[string][]string),
		}
		role.Attributes["source_provider"] = []string{string(p.Provider)}
		role.Attributes["source_resource"] = []string{p.ResourceID}
		mapping.Roles = append(mapping.Roles, role)
	}

	// Add review notes based on policy type
	if p.Type == policy.PolicyTypeIAM {
		mapping.ManualReviewNotes = append(mapping.ManualReviewNotes,
			"Review trust relationships for service account access",
			"Verify role scope matches intended access level",
		)
	}
	if len(unmapped) > 0 {
		mapping.ManualReviewNotes = append(mapping.ManualReviewNotes,
			fmt.Sprintf("%d actions could not be automatically mapped", len(unmapped)),
		)
	}

	return mapping
}

// splitScope splits a scope like "storage:read" into ["storage", "read"].
func splitScope(scope string) []string {
	for i, c := range scope {
		if c == ':' {
			return []string{scope[:i], scope[i+1:]}
		}
	}
	return []string{scope}
}

// Close cleans up resources.
func (s *Service) Close() error {
	return nil
}
