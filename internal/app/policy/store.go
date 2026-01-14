package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/homeport/homeport/internal/domain/policy"
)

// Store manages persistent storage of policies.
type Store struct {
	mu       sync.RWMutex
	filePath string
	policies map[string]*policy.Policy
}

// NewStore creates a new policy store.
// If path is empty, defaults to ~/.homeport/policies.json
func NewStore(path string) (*Store, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home dir: %w", err)
		}
		path = filepath.Join(home, ".homeport", "policies.json")
	}

	store := &Store{
		filePath: path,
		policies: make(map[string]*policy.Policy),
	}

	// Load existing policies if file exists
	if err := store.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load policies: %w", err)
	}

	return store, nil
}

// Save persists a new policy.
func (s *Store) Save(p *policy.Policy) (*policy.Policy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Generate ID if not set
	if p.ID == "" {
		p.ID = uuid.New().String()
	}

	now := time.Now()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now

	s.policies[p.ID] = p

	if err := s.persist(); err != nil {
		delete(s.policies, p.ID)
		return nil, fmt.Errorf("failed to persist policy: %w", err)
	}

	return p, nil
}

// SaveBatch persists multiple policies at once.
func (s *Store) SaveBatch(policies []*policy.Policy) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	savedIDs := make([]string, 0, len(policies))

	for _, p := range policies {
		if p.ID == "" {
			p.ID = uuid.New().String()
		}
		if p.CreatedAt.IsZero() {
			p.CreatedAt = now
		}
		p.UpdatedAt = now
		s.policies[p.ID] = p
		savedIDs = append(savedIDs, p.ID)
	}

	if err := s.persist(); err != nil {
		// Rollback
		for _, id := range savedIDs {
			delete(s.policies, id)
		}
		return fmt.Errorf("failed to persist policies: %w", err)
	}

	return nil
}

// Get retrieves a policy by ID.
func (s *Store) Get(id string) (*policy.Policy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	p, ok := s.policies[id]
	if !ok {
		return nil, fmt.Errorf("policy not found: %s", id)
	}

	return p, nil
}

// List returns all policies, optionally filtered.
func (s *Store) List(filter *policy.PolicyFilter) []*policy.Policy {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]*policy.Policy, 0, len(s.policies))
	for _, p := range s.policies {
		if filter == nil || filter.Matches(p) {
			list = append(list, p)
		}
	}

	return list
}

// Update modifies an existing policy.
func (s *Store) Update(id string, updates *policy.Policy) (*policy.Policy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.policies[id]
	if !ok {
		return nil, fmt.Errorf("policy not found: %s", id)
	}

	// Apply updates
	if updates.Name != "" {
		existing.Name = updates.Name
	}
	if updates.NormalizedPolicy != nil {
		existing.NormalizedPolicy = updates.NormalizedPolicy
	}
	if updates.KeycloakMapping != nil {
		existing.KeycloakMapping = updates.KeycloakMapping
	}
	if len(updates.OriginalDocument) > 0 {
		existing.OriginalDocument = updates.OriginalDocument
	}
	existing.Warnings = updates.Warnings
	existing.UpdatedAt = time.Now()

	if err := s.persist(); err != nil {
		return nil, fmt.Errorf("failed to persist update: %w", err)
	}

	return existing, nil
}

// Delete removes a policy by ID.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.policies[id]; !ok {
		return fmt.Errorf("policy not found: %s", id)
	}

	delete(s.policies, id)

	if err := s.persist(); err != nil {
		return fmt.Errorf("failed to persist after delete: %w", err)
	}

	return nil
}

// DeleteByResource removes all policies for a resource.
func (s *Store) DeleteByResource(resourceID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	toDelete := make([]string, 0)
	for id, p := range s.policies {
		if p.ResourceID == resourceID {
			toDelete = append(toDelete, id)
		}
	}

	for _, id := range toDelete {
		delete(s.policies, id)
	}

	if len(toDelete) > 0 {
		if err := s.persist(); err != nil {
			return 0, fmt.Errorf("failed to persist after delete: %w", err)
		}
	}

	return len(toDelete), nil
}

// Count returns the total number of policies.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.policies)
}

// GetSummary returns policy statistics.
func (s *Store) GetSummary() *policy.PolicySummary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	policies := make([]*policy.Policy, 0, len(s.policies))
	for _, p := range s.policies {
		policies = append(policies, p)
	}

	collection := policy.NewPolicyCollection(policies)
	return collection.Summary
}

// load reads policies from disk.
func (s *Store) load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}

	var policies map[string]*policy.Policy
	if err := json.Unmarshal(data, &policies); err != nil {
		return fmt.Errorf("failed to unmarshal policies: %w", err)
	}

	s.policies = policies
	return nil
}

// persist writes policies to disk atomically.
func (s *Store) persist() error {
	// Ensure directory exists
	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	data, err := json.MarshalIndent(s.policies, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal policies: %w", err)
	}

	// Write atomically via temp file
	tmpFile := s.filePath + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0600); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	if err := os.Rename(tmpFile, s.filePath); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

// FilePath returns the storage file path.
func (s *Store) FilePath() string {
	return s.filePath
}

// Clear removes all policies.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.policies = make(map[string]*policy.Policy)

	if err := s.persist(); err != nil {
		return fmt.Errorf("failed to persist after clear: %w", err)
	}

	return nil
}
