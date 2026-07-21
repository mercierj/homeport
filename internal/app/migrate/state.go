package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"time"

	"github.com/google/uuid"
)

// DiscoveryState represents a saved discovery result.
type DiscoveryState struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Provider      string         `json:"provider"`
	Regions       []string       `json:"regions"`
	Resources     []ResourceInfo `json:"resources"`
	ResourceCount int            `json:"resource_count"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
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
		var err error
		path, err = DefaultStatePath()
		if err != nil {
			return nil, err
		}
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

// DefaultStatePath returns the location shared by discovery consumers.
func DefaultStatePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home dir: %w", err)
	}
	return filepath.Join(home, ".homeport", "discoveries.json"), nil
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

	s.states[state.ID] = cloneDiscoveryState(state)

	if err := s.persist(); err != nil {
		delete(s.states, state.ID)
		return nil, fmt.Errorf("failed to persist state: %w", err)
	}

	return cloneDiscoveryState(state), nil
}

// Get retrieves a discovery by ID.
func (s *StateStore) Get(id string) (*DiscoveryState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state, ok := s.states[id]
	if !ok {
		return nil, fmt.Errorf("discovery not found: %s", id)
	}

	return cloneDiscoveryState(state), nil
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
			Regions:       append([]string(nil), state.Regions...),
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

	return cloneDiscoveryState(state), nil
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
		_ = os.Remove(tmpFile)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

// FilePath returns the storage file path.
func (s *StateStore) FilePath() string {
	return s.filePath
}

func cloneDiscoveryState(state *DiscoveryState) *DiscoveryState {
	if state == nil {
		return nil
	}
	copy := *state
	copy.Regions = append([]string(nil), state.Regions...)
	copy.Resources = make([]ResourceInfo, len(state.Resources))
	for i, resource := range state.Resources {
		copy.Resources[i] = resource
		copy.Resources[i].Dependencies = append([]string(nil), resource.Dependencies...)
		copy.Resources[i].Tags = cloneStringMap(resource.Tags)
		copy.Resources[i].Config = cloneConfig(resource.Config)
	}
	return &copy
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	copy := make(map[string]string, len(values))
	for key, value := range values {
		copy[key] = value
	}
	return copy
}

func cloneConfig(config map[string]interface{}) map[string]interface{} {
	if config == nil {
		return nil
	}
	copy := make(map[string]interface{}, len(config))
	for key, value := range config {
		copy[key] = cloneConfigValue(value)
	}
	return copy
}

func cloneConfigValue(value interface{}) interface{} {
	cloned := cloneReflectValue(reflect.ValueOf(value))
	if !cloned.IsValid() || !cloned.CanInterface() {
		return value
	}
	return cloned.Interface()
}

func cloneReflectValue(value reflect.Value) reflect.Value {
	if !value.IsValid() {
		return value
	}
	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		cloned := cloneReflectValue(value.Elem())
		result := reflect.New(value.Type()).Elem()
		if cloned.IsValid() && cloned.Type().AssignableTo(value.Type()) {
			result.Set(cloned)
		} else if cloned.IsValid() && cloned.Type().Implements(value.Type()) {
			result.Set(cloned)
		}
		return result
	case reflect.Ptr:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		result := reflect.New(value.Type().Elem())
		result.Elem().Set(cloneReflectValue(value.Elem()))
		return result
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		result := reflect.MakeMapWithSize(value.Type(), value.Len())
		iterator := value.MapRange()
		for iterator.Next() {
			result.SetMapIndex(cloneReflectValue(iterator.Key()), cloneReflectValue(iterator.Value()))
		}
		return result
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		result := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for i := 0; i < value.Len(); i++ {
			result.Index(i).Set(cloneReflectValue(value.Index(i)))
		}
		return result
	case reflect.Array:
		result := reflect.New(value.Type()).Elem()
		for i := 0; i < value.Len(); i++ {
			result.Index(i).Set(cloneReflectValue(value.Index(i)))
		}
		return result
	default:
		return value
	}
}
