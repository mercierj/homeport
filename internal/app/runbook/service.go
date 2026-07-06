package runbook

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type Executor func(context.Context, domainrunbook.Step) domainrunbook.StepResult

type Service struct {
	outputDir string
	runbooks  map[string]*domainrunbook.Runbook
	executors map[string]Executor
	mu        sync.RWMutex
}

type stateFile struct {
	Runbooks map[string]*domainrunbook.Runbook `json:"runbooks"`
}

func NewService(outputDir string) *Service {
	s := &Service{
		outputDir: outputDir,
		runbooks:  make(map[string]*domainrunbook.Runbook),
		executors: make(map[string]Executor),
	}
	s.RegisterExecutor("noop", func(context.Context, domainrunbook.Step) domainrunbook.StepResult {
		return domainrunbook.StepResult{Status: domainrunbook.StepStatusPassed}
	})
	s.RegisterExecutor("user", func(context.Context, domainrunbook.Step) domainrunbook.StepResult {
		return domainrunbook.StepResult{Status: domainrunbook.StepStatusBlocked, Error: "waiting for user action"}
	})
	s.RegisterExecutor("dns", func(context.Context, domainrunbook.Step) domainrunbook.StepResult {
		return domainrunbook.StepResult{Status: domainrunbook.StepStatusBlocked, Error: "waiting for DNS verification"}
	})
	s.RegisterExecutor("shell", shellExecutor)
	_ = s.load()
	return s
}

func FromMappingResult(result *mapper.MappingResult) (*domainrunbook.Runbook, error) {
	if result == nil || result.DockerService == nil {
		return nil, fmt.Errorf("mapping result with docker service is required")
	}
	now := time.Now().UTC()
	book := &domainrunbook.Runbook{
		ID:        result.DockerService.Name,
		Name:      fmt.Sprintf("%s migration", result.DockerService.Name),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if len(result.RunbookSteps) > 0 {
		book.Steps = append(book.Steps, result.RunbookSteps...)
		return book, book.Validate()
	}
	for i, text := range result.ManualSteps {
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		step := stepFromManualText(i+1, text)
		book.Steps = append(book.Steps, step)
	}
	if len(book.Steps) == 0 {
		book.Steps = append(book.Steps, domainrunbook.Step{
			ID:               "validate",
			Name:             "Validate generated service",
			Group:            "Validate",
			Type:             domainrunbook.StepTypeHealth,
			Status:           domainrunbook.StepStatusPending,
			Executor:         "noop",
			SuccessCondition: "validation passed",
		})
	}
	return book, book.Validate()
}

func shellExecutor(ctx context.Context, step domainrunbook.Step) domainrunbook.StepResult {
	if len(step.Command) == 0 {
		return domainrunbook.StepResult{Status: domainrunbook.StepStatusFailed, Error: "command is required"}
	}
	cmd := exec.CommandContext(ctx, step.Command[0], step.Command[1:]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return domainrunbook.StepResult{Status: domainrunbook.StepStatusFailed, Output: string(out), Error: err.Error()}
	}
	return domainrunbook.StepResult{Status: domainrunbook.StepStatusPassed, Output: string(out)}
}

func HasUnresolvedManualText(book *domainrunbook.Runbook) bool {
	if book == nil {
		return false
	}
	for _, step := range book.Steps {
		if step.Metadata["legacy_manual_text"] != "" {
			return true
		}
	}
	return false
}

func (s *Service) RegisterExecutor(name string, executor Executor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.executors[name] = executor
}

func (s *Service) Save(book *domainrunbook.Runbook) error {
	if err := book.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	book.UpdatedAt = time.Now().UTC()
	s.runbooks[book.ID] = book
	s.mu.Unlock()
	return s.persist()
}

func (s *Service) Get(id string) (*domainrunbook.Runbook, error) {
	if err := s.load(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	book := s.runbooks[id]
	if book == nil {
		return nil, fmt.Errorf("runbook not found: %s", id)
	}
	return book, nil
}

func (s *Service) RunNext(ctx context.Context, id string) (*domainrunbook.StepResult, error) {
	book, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	step := book.FirstUnpassedStep()
	if step == nil {
		return nil, nil
	}
	if step.Status == domainrunbook.StepStatusBlocked {
		return nil, fmt.Errorf("step blocked: %s", step.ID)
	}
	return s.runStep(ctx, step)
}

func (s *Service) runStep(ctx context.Context, step *domainrunbook.Step) (*domainrunbook.StepResult, error) {
	s.mu.RLock()
	executor := s.executors[step.Executor]
	s.mu.RUnlock()
	if executor == nil {
		return nil, fmt.Errorf("executor not found: %s", step.Executor)
	}

	started := time.Now().UTC()
	step.Status = domainrunbook.StepStatusRunning
	step.Result = &domainrunbook.StepResult{Status: domainrunbook.StepStatusRunning, StartedAt: started}
	_ = s.persist()

	result := executor(ctx, *step)
	result.StartedAt = started
	result.EndedAt = time.Now().UTC()
	step.Result = &result
	step.Status = result.Status
	if step.Status == "" {
		step.Status = domainrunbook.StepStatusPassed
		step.Result.Status = step.Status
	}
	if err := s.persist(); err != nil {
		return &result, err
	}
	if step.Status == domainrunbook.StepStatusFailed || step.Status == domainrunbook.StepStatusBlocked {
		return &result, fmt.Errorf("step %s %s: %s", step.ID, step.Status, result.Error)
	}
	return &result, nil
}

func (s *Service) RunAll(ctx context.Context, id string) error {
	for {
		book, err := s.Get(id)
		if err != nil {
			return err
		}
		step := firstUnpassedForwardStep(book)
		if step == nil {
			return nil
		}
		if step.Status == domainrunbook.StepStatusBlocked {
			return fmt.Errorf("step blocked: %s", step.ID)
		}
		if _, err := s.runStep(ctx, step); err != nil {
			return err
		}
	}
}

func firstUnpassedForwardStep(book *domainrunbook.Runbook) *domainrunbook.Step {
	for i := range book.Steps {
		step := &book.Steps[i]
		if step.Type == domainrunbook.StepTypeRollback {
			continue
		}
		if step.Status != domainrunbook.StepStatusPassed && step.Status != domainrunbook.StepStatusSkipped {
			return step
		}
	}
	return nil
}

func (s *Service) Rollback(ctx context.Context, id string) error {
	book, err := s.Get(id)
	if err != nil {
		return err
	}
	found := false
	for i := len(book.Steps) - 1; i >= 0; i-- {
		if book.Steps[i].Type != domainrunbook.StepTypeRollback {
			continue
		}
		found = true
		if book.Steps[i].Status == domainrunbook.StepStatusPassed {
			continue
		}
		if _, err := s.runStep(ctx, &book.Steps[i]); err != nil {
			return err
		}
	}
	if !found {
		return fmt.Errorf("runbook has no rollback step: %s", id)
	}
	return nil
}

func stepFromManualText(index int, text string) domainrunbook.Step {
	lower := strings.ToLower(text)
	step := domainrunbook.Step{
		ID:               fmt.Sprintf("guided-%d", index),
		Name:             text,
		Description:      text,
		Status:           domainrunbook.StepStatusBlocked,
		Executor:         "user",
		SuccessCondition: "user confirmed",
		Metadata:         map[string]string{"legacy_manual_text": text},
	}
	switch {
	case strings.Contains(lower, "dns"):
		step.Type = domainrunbook.StepTypeDNSCheck
		step.Group = "Cutover"
		step.Executor = "dns"
		step.SuccessCondition = "dns resolves"
	case strings.Contains(lower, "application code"):
		step.Type = domainrunbook.StepTypeApproval
		step.Group = "Validate"
		step.SuccessCondition = "api compatibility adapter available"
	case strings.Contains(lower, "credential") || strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "environment"):
		step.Type = domainrunbook.StepTypeInput
		step.Group = "Credentials"
		step.SuccessCondition = "input provided"
	default:
		step.Type = domainrunbook.StepTypeApproval
		step.Group = "Provision"
	}
	return step
}

func (s *Service) statePath() string {
	return filepath.Join(s.outputDir, ".homeport", "runbook.json")
}

func (s *Service) load() error {
	path := s.statePath()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var state stateFile
	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if state.Runbooks == nil {
		state.Runbooks = make(map[string]*domainrunbook.Runbook)
	}
	s.runbooks = state.Runbooks
	return nil
}

func (s *Service) persist() error {
	s.mu.RLock()
	state := stateFile{Runbooks: s.runbooks}
	s.mu.RUnlock()

	path := s.statePath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
