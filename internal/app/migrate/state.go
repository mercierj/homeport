package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

// DiscoveryState represents a saved discovery result.
type DiscoveryState struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Provider     string         `json:"provider"`
	Regions      []string       `json:"regions"`
	Resources    []ResourceInfo `json:"resources"`
	ResourceCount int           `json:"resource_count"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// StateStore manages persistent storage of discovery states.
type StateStore struct {
	mu       sync.RWMutex
	filePath string
	states   map[string]*DiscoveryState
}

// NewStateStore creates a new state store.
// If path is empty, defaults to ~/.homeport/discoveries.json
func NewStateStore(path string) (*StateStore, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home dir: %w", err)
		}
		path = filepath.Join(home, ".homeport", "discoveries.json")
	}

	store := &StateStore{
		filePath: path,
		states:   make(map[string]*DiscoveryState),
	}

	// Load existing states if file exists
	if err := store.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load states: %w", err)
	}

	return store, nil
}

// Save persists a discovery result.
func (s *StateStore) Save(name string, provider string, regions []string, resources []ResourceInfo) (*DiscoveryState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	state := &DiscoveryState{
		ID:            uuid.New().String(),
		Name:          name,
		Provider:      provider,
		Regions:       regions,
		Resources:     resources,
		ResourceCount: len(resources),
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	s.states[state.ID] = state

	if err := s.persist(); err != nil {
		delete(s.states, state.ID)
		return nil, fmt.Errorf("failed to persist state: %w", err)
	}

	return state, nil
}

// Get retrieves a discovery by ID.
func (s *StateStore) Get(id string) (*DiscoveryState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state, ok := s.states[id]
	if !ok {
		return nil, fmt.Errorf("discovery not found: %s", id)
	}

	return state, nil
}

// List returns all saved discoveries (without full resources for efficiency).
func (s *StateStore) List() []*DiscoveryState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]*DiscoveryState, 0, len(s.states))
	for _, state := range s.states {
		// Return summary without full resources
		summary := &DiscoveryState{
			ID:            state.ID,
			Name:          state.Name,
			Provider:      state.Provider,
			Regions:       state.Regions,
			ResourceCount: state.ResourceCount,
			CreatedAt:     state.CreatedAt,
			UpdatedAt:     state.UpdatedAt,
		}
		list = append(list, summary)
	}

	return list
}

// Delete removes a discovery by ID.
func (s *StateStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.states[id]; !ok {
		return fmt.Errorf("discovery not found: %s", id)
	}

	delete(s.states, id)

	if err := s.persist(); err != nil {
		return fmt.Errorf("failed to persist after delete: %w", err)
	}

	return nil
}

// Update modifies an existing discovery (e.g., rename).
func (s *StateStore) Update(id string, name string) (*DiscoveryState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.states[id]
	if !ok {
		return nil, fmt.Errorf("discovery not found: %s", id)
	}

	state.Name = name
	state.UpdatedAt = time.Now()

	if err := s.persist(); err != nil {
		return nil, fmt.Errorf("failed to persist update: %w", err)
	}

	return state, nil
}

// load reads states from disk.
func (s *StateStore) load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}

	var states map[string]*DiscoveryState
	if err := json.Unmarshal(data, &states); err != nil {
		return fmt.Errorf("failed to unmarshal states: %w", err)
	}

	s.states = states
	return nil
}

// persist writes states to disk.
func (s *StateStore) persist() error {
	// Ensure directory exists
	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	data, err := json.MarshalIndent(s.states, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal states: %w", err)
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
func (s *StateStore) FilePath() string {
	return s.filePath
}
