# Wizard Session Contract Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a persisted migration wizard session so the frontend and backend share one durable A-to-Z workflow state.

**Architecture:** Add a tiny Go session model and file-backed service under `.homeport/wizard-sessions.json`. The React wizard keeps using Zustand for live UI state, but saves/restores through `/api/v1/wizard/sessions`.

**Tech Stack:** Go domain/app/API handler, chi routes, React Zustand, existing `web/src/lib/api.ts`.

---

## Files

- Create: `internal/domain/wizard/session.go`
- Create: `internal/domain/wizard/session_test.go`
- Create: `internal/app/wizard/service.go`
- Create: `internal/app/wizard/service_test.go`
- Create: `internal/api/handlers/wizard.go`
- Create: `internal/api/handlers/wizard_test.go`
- Modify: `internal/api/server.go`
- Create: `web/src/lib/wizard-session-api.ts`
- Modify: `web/src/stores/wizard.ts`
- Modify: `web/src/pages/Migrate.tsx`

## Task 1: Define the session model

- [ ] Create `internal/domain/wizard/session.go` with this shape:

```go
package wizard

import (
	"fmt"
	"time"
)

type Step string

const (
	StepAnalyze Step = "analyze"
	StepExport  Step = "export"
	StepSecrets Step = "secrets"
	StepDeploy  Step = "deploy"
	StepSync    Step = "sync"
	StepCutover Step = "cutover"
	StepDone    Step = "done"
)

type Session struct {
	ID                string            `json:"id"`
	CurrentStep       Step              `json:"current_step"`
	CompletedSteps    []Step            `json:"completed_steps"`
	SourceProvider    string            `json:"source_provider,omitempty"`
	SelectedResources []string          `json:"selected_resources,omitempty"`
	BundleID          string            `json:"bundle_id,omitempty"`
	SecretsResolved   bool              `json:"secrets_resolved"`
	DeploymentID      string            `json:"deployment_id,omitempty"`
	RunbookID         string            `json:"runbook_id,omitempty"`
	SyncPlanID        string            `json:"sync_plan_id,omitempty"`
	CutoverID         string            `json:"cutover_id,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

func (s Session) Validate() error {
	if s.ID == "" {
		return fmt.Errorf("session id is required")
	}
	switch s.CurrentStep {
	case StepAnalyze, StepExport, StepSecrets, StepDeploy, StepSync, StepCutover, StepDone:
		return nil
	default:
		return fmt.Errorf("invalid current step %q", s.CurrentStep)
	}
}
```

- [ ] Create `internal/domain/wizard/session_test.go`:

```go
package wizard

import "testing"

func TestSessionValidateRejectsInvalidStep(t *testing.T) {
	err := Session{ID: "s1", CurrentStep: Step("bogus")}.Validate()
	if err == nil {
		t.Fatal("expected invalid step error")
	}
}

func TestSessionValidateAcceptsDeploy(t *testing.T) {
	if err := (Session{ID: "s1", CurrentStep: StepDeploy}).Validate(); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] Run `go test ./internal/domain/wizard`.
Expected: pass.

## Task 2: Add file-backed session service

- [ ] Create `internal/app/wizard/service.go`:

```go
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
	session.SecretsResolved = patch.SecretsResolved
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
```

- [ ] Create `internal/app/wizard/service_test.go`:

```go
package wizard

import (
	"testing"

	domainwizard "github.com/homeport/homeport/internal/domain/wizard"
)

func TestServicePersistsSession(t *testing.T) {
	dir := t.TempDir()
	service := NewService(dir)
	session, err := service.Create()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Update(session.ID, domainwizard.Session{CurrentStep: domainwizard.StepDeploy, BundleID: "bundle-1"}); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewService(dir).Get(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.CurrentStep != domainwizard.StepDeploy || reloaded.BundleID != "bundle-1" || reloaded.RunbookID != "bundle-1" {
		t.Fatalf("unexpected session: %#v", reloaded)
	}
}
```

- [ ] Run `go test ./internal/app/wizard`.
Expected: pass.

## Task 3: Add HTTP endpoints

- [ ] Create `internal/api/handlers/wizard.go` with `POST /wizard/sessions`, `GET /wizard/sessions/{id}`, and `PATCH /wizard/sessions/{id}` using `httputil.DecodeJSON` and `render.JSON`.
- [ ] Register it in `internal/api/server.go` next to the other API v1 handlers:

```go
wizardHandler := handlers.NewWizardHandler(appwizard.NewService("."))
wizardHandler.RegisterRoutes(r)
```

- [ ] Create `internal/api/handlers/wizard_test.go` with `httptest` coverage for create, patch bundle ID, and get.
- [ ] Run `go test ./internal/api/handlers -run Wizard`.
Expected: pass.

## Task 4: Add frontend API and store fields

- [ ] Create `web/src/lib/wizard-session-api.ts`:

```ts
import { fetchAPI } from './api';

export type WizardSessionStep = 'analyze' | 'export' | 'secrets' | 'deploy' | 'sync' | 'cutover' | 'done';

export interface WizardSession {
  id: string;
  current_step: WizardSessionStep;
  completed_steps: WizardSessionStep[];
  source_provider?: string;
  selected_resources?: string[];
  bundle_id?: string;
  secrets_resolved: boolean;
  deployment_id?: string;
  runbook_id?: string;
  sync_plan_id?: string;
  cutover_id?: string;
  metadata?: Record<string, string>;
}

export function createWizardSession(): Promise<WizardSession> {
  return fetchAPI<WizardSession>('/wizard/sessions', { method: 'POST' });
}

export function getWizardSession(id: string): Promise<WizardSession> {
  return fetchAPI<WizardSession>(`/wizard/sessions/${id}`, { method: 'GET' });
}

export function updateWizardSession(id: string, patch: Partial<WizardSession>): Promise<WizardSession> {
  return fetchAPI<WizardSession>(`/wizard/sessions/${id}`, {
    method: 'PATCH',
    body: JSON.stringify(patch),
  });
}
```

- [ ] Modify `web/src/stores/wizard.ts` to add `sessionId: string | null`, `setSessionId`, and `hydrateFromSession(session: WizardSession)`.
- [ ] Modify `web/src/pages/Migrate.tsx` so entering the wizard creates a session if `sessionId` is null, and every successful step updates the session.
- [ ] Run `cd web && ./node_modules/.bin/tsc -b`.
Expected: pass.

## Task 5: Commit

- [ ] Run `gofmt -w internal/domain/wizard internal/app/wizard internal/api/handlers/wizard.go internal/api/handlers/wizard_test.go`.
- [ ] Run `go test ./internal/domain/wizard ./internal/app/wizard ./internal/api/handlers`.
- [ ] Run `cd web && ./node_modules/.bin/tsc -b`.
- [ ] Commit:

```bash
git add internal/domain/wizard internal/app/wizard internal/api/handlers/wizard.go internal/api/handlers/wizard_test.go internal/api/server.go web/src/lib/wizard-session-api.ts web/src/stores/wizard.ts web/src/pages/Migrate.tsx
git commit -m "feat: persist migration wizard sessions"
```

