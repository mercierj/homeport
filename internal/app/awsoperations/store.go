package awsoperations

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

// Store persists post-cutover workspaces independently from discovery snapshots.
type Store struct {
	mu         sync.RWMutex
	filePath   string
	workspaces map[string]*Workspace
}

// WorkspaceStore is the persistence boundary used by the operations service.
// Create must atomically return the existing workspace for the same discovery.
type WorkspaceStore interface {
	Create(*Workspace) (*Workspace, error)
	Get(string) (*Workspace, error)
	List() ([]*Workspace, error)
	GetByDiscoveryID(string) (*Workspace, error)
}

func NewStore(path string) (*Store, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("get home directory: %w", err)
		}
		path = filepath.Join(home, ".homeport", "aws-operations.json")
	}

	s := &Store{filePath: path, workspaces: make(map[string]*Workspace)}
	if err := s.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("load workspaces: %w", err)
	}
	return s, nil
}

// Create atomically persists a workspace or returns the existing one for the discovery.
func (s *Store) Create(workspace *Workspace) (*Workspace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	release, err := s.lock()
	if err != nil {
		return nil, err
	}
	defer release()
	if err := s.reloadLocked(); err != nil {
		return nil, err
	}

	if workspace == nil || workspace.ID == "" {
		return nil, fmt.Errorf("workspace ID is required")
	}
	if _, exists := s.workspaces[workspace.ID]; exists {
		return nil, fmt.Errorf("workspace already exists: %s", workspace.ID)
	}
	for _, existing := range s.workspaces {
		if existing.DiscoveryID == workspace.DiscoveryID {
			return cloneWorkspace(existing), nil
		}
	}
	s.workspaces[workspace.ID] = cloneWorkspace(workspace)
	if err := s.persist(); err != nil {
		delete(s.workspaces, workspace.ID)
		return nil, err
	}
	return cloneWorkspace(workspace), nil
}

func (s *Store) lock() (func(), error) {
	if err := os.MkdirAll(filepath.Dir(s.filePath), 0700); err != nil {
		return nil, fmt.Errorf("create workspace directory: %w", err)
	}
	lock, err := os.OpenFile(s.filePath+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open workspace lock: %w", err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("lock workspace store: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		_ = lock.Close()
	}, nil
}

func (s *Store) reloadLocked() error {
	if err := s.load(); err != nil {
		if os.IsNotExist(err) {
			s.workspaces = make(map[string]*Workspace)
			return nil
		}
		return fmt.Errorf("reload workspaces: %w", err)
	}
	return nil
}

func (s *Store) Get(id string) (*Workspace, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	workspace, ok := s.workspaces[id]
	if !ok {
		return nil, fmt.Errorf("workspace not found: %s", id)
	}
	return cloneWorkspace(workspace), nil
}

// List returns a detached snapshot of every persisted workspace.
func (s *Store) List() ([]*Workspace, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	workspaces := make([]*Workspace, 0, len(s.workspaces))
	for _, workspace := range s.workspaces {
		workspaces = append(workspaces, cloneWorkspace(workspace))
	}
	return workspaces, nil
}

func (s *Store) GetByDiscoveryID(discoveryID string) (*Workspace, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, workspace := range s.workspaces {
		if workspace.DiscoveryID == discoveryID {
			return cloneWorkspace(workspace), nil
		}
	}
	return nil, fmt.Errorf("workspace not found for discovery: %s", discoveryID)
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}
	var workspaces map[string]*Workspace
	if err := json.Unmarshal(data, &workspaces); err != nil {
		return fmt.Errorf("unmarshal workspaces: %w", err)
	}
	discoveries := make(map[string]string, len(workspaces))
	for id, workspace := range workspaces {
		if workspace == nil || workspace.DiscoveryID == "" {
			continue
		}
		if existingID, exists := discoveries[workspace.DiscoveryID]; exists {
			return fmt.Errorf("duplicate workspace discovery ID %q in workspaces %q and %q", workspace.DiscoveryID, existingID, id)
		}
		discoveries[workspace.DiscoveryID] = id
	}
	s.workspaces = workspaces
	return nil
}

func (s *Store) persist() error {
	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create workspace directory: %w", err)
	}
	data, err := json.MarshalIndent(s.workspaces, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal workspaces: %w", err)
	}
	temporary, err := os.CreateTemp(dir, filepath.Base(s.filePath)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create workspace temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set workspace temporary file permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write workspace temporary file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync workspace temporary file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close workspace temporary file: %w", err)
	}
	if err := os.Rename(temporaryPath, s.filePath); err != nil {
		return fmt.Errorf("rename workspace temporary file: %w", err)
	}
	if directory, err := os.Open(dir); err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return nil
}

func cloneWorkspace(workspace *Workspace) *Workspace {
	copy := *workspace
	copy.Services = make(map[ServiceKey]ServiceState, len(workspace.Services))
	for key, state := range workspace.Services {
		state.Capabilities = append([]Capability(nil), state.Capabilities...)
		copy.Services[key] = state
	}
	copy.Bindings = make([]ResourceBinding, len(workspace.Bindings))
	for i, binding := range workspace.Bindings {
		copy.Bindings[i] = binding
		if binding.Tags != nil {
			copy.Bindings[i].Tags = make(map[string]string, len(binding.Tags))
			for key, value := range binding.Tags {
				copy.Bindings[i].Tags[key] = value
			}
		}
	}
	return &copy
}
