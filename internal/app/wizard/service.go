package wizard

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	domainwizard "github.com/homeport/homeport/internal/domain/wizard"
)

type Service struct {
	path     string
	mu       sync.Mutex
	sessions map[string]*domainwizard.Session
}

type SessionPatch struct {
	CurrentStep       domainwizard.Step
	CompletedSteps    []domainwizard.Step
	SourceProvider    string
	SelectedResources []string
	BundleID          string
	SecretsResolved   *bool
	DeploymentID      string
	SyncPlanID        string
	CutoverID         string
	Metadata          map[string]string
}

func NewService(baseDir string) *Service {
	if baseDir == "" {
		baseDir = "."
	}
	s := &Service{
		path:     filepath.Join(baseDir, ".homeport", "wizard-sessions.json"),
		sessions: map[string]*domainwizard.Session{},
	}
	_ = s.load()
	return s
}

func (s *Service) Create() (*domainwizard.Session, error) {
	now := time.Now().UTC()
	session := &domainwizard.Session{
		ID:             uuid.NewString(),
		CurrentStep:    domainwizard.StepAnalyze,
		CompletedSteps: []domainwizard.Step{},
		Metadata:       map[string]string{},
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := session.Validate(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.sessions[session.ID] = session
	s.mu.Unlock()
	return session, s.persist()
}

func (s *Service) Get(id string) (*domainwizard.Session, error) {
	if err := s.load(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.sessions[id]
	if session == nil {
		return nil, fmt.Errorf("wizard session not found: %s", id)
	}
	return session, nil
}

func (s *Service) Update(id string, patch domainwizard.Session) (*domainwizard.Session, error) {
	var secretsResolved *bool
	if patch.SecretsResolved {
		secretsResolved = &patch.SecretsResolved
	}
	return s.UpdatePatch(id, SessionPatch{
		CurrentStep:       patch.CurrentStep,
		CompletedSteps:    patch.CompletedSteps,
		SourceProvider:    patch.SourceProvider,
		SelectedResources: patch.SelectedResources,
		BundleID:          patch.BundleID,
		SecretsResolved:   secretsResolved,
		DeploymentID:      patch.DeploymentID,
		SyncPlanID:        patch.SyncPlanID,
		CutoverID:         patch.CutoverID,
		Metadata:          patch.Metadata,
	})
}

func (s *Service) UpdatePatch(id string, patch SessionPatch) (*domainwizard.Session, error) {
	session, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if patch.CurrentStep != "" {
		session.CurrentStep = patch.CurrentStep
	}
	if patch.CompletedSteps != nil {
		session.CompletedSteps = patch.CompletedSteps
	}
	if patch.SourceProvider != "" {
		session.SourceProvider = patch.SourceProvider
	}
	if patch.SelectedResources != nil {
		session.SelectedResources = patch.SelectedResources
	}
	if patch.BundleID != "" {
		session.BundleID = patch.BundleID
		session.RunbookID = patch.BundleID
	}
	if patch.SecretsResolved != nil {
		session.SecretsResolved = *patch.SecretsResolved
	}
	if patch.DeploymentID != "" {
		session.DeploymentID = patch.DeploymentID
	}
	if patch.SyncPlanID != "" {
		session.SyncPlanID = patch.SyncPlanID
	}
	if patch.CutoverID != "" {
		session.CutoverID = patch.CutoverID
	}
	if patch.Metadata != nil {
		session.Metadata = patch.Metadata
	}
	session.UpdatedAt = time.Now().UTC()
	if err := session.Validate(); err != nil {
		return nil, err
	}
	return session, s.persist()
}

func (s *Service) load() error {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var sessions map[string]*domainwizard.Session
	if err := json.Unmarshal(data, &sessions); err != nil {
		return err
	}
	s.mu.Lock()
	s.sessions = sessions
	s.mu.Unlock()
	return nil
}

func (s *Service) persist() error {
	s.mu.Lock()
	data, err := json.MarshalIndent(s.sessions, "", "  ")
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}
