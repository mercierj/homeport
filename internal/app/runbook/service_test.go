package runbook

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestFromMappingResultMapsDNSManualStep(t *testing.T) {
	result := mapper.NewMappingResult("web")
	result.AddManualStep("Update DNS records for app.example.com")

	book, err := FromMappingResult(result)
	if err != nil {
		t.Fatalf("FromMappingResult() error = %v", err)
	}
	if len(book.Steps) != 1 {
		t.Fatalf("len(Steps) = %d, want 1", len(book.Steps))
	}
	if got := book.Steps[0].Type; got != domainrunbook.StepTypeDNSCheck {
		t.Fatalf("step type = %q, want %q", got, domainrunbook.StepTypeDNSCheck)
	}
	if got := book.Steps[0].Status; got != domainrunbook.StepStatusBlocked {
		t.Fatalf("step status = %q, want %q", got, domainrunbook.StepStatusBlocked)
	}
}

func TestFromMappingResultBlocksApplicationCodeWithoutAdapter(t *testing.T) {
	result := mapper.NewMappingResult("api")
	result.AddManualStep("Update application code to use the new endpoint")

	book, err := FromMappingResult(result)
	if err != nil {
		t.Fatalf("FromMappingResult() error = %v", err)
	}
	if got := book.Steps[0].Status; got != domainrunbook.StepStatusBlocked {
		t.Fatalf("step status = %q, want %q", got, domainrunbook.StepStatusBlocked)
	}
	if !HasUnresolvedManualText(book) {
		t.Fatal("HasUnresolvedManualText() = false, want true")
	}
}

func TestFromMappingResultUsesStructuredRunbookSteps(t *testing.T) {
	result := mapper.NewMappingResult("minio")
	result.AddManualStep("legacy manual text should not appear")
	result.AddRunbookStep(domainrunbook.Step{
		ID:               "provision-minio-bucket",
		Name:             "Provision MinIO bucket",
		Group:            "Object Storage",
		Type:             domainrunbook.StepTypeCommand,
		Status:           domainrunbook.StepStatusPending,
		Executor:         "shell",
		SuccessCondition: "bucket exists",
		Command:          []string{"sh", "setup_minio.sh"},
		Metadata:         map[string]string{"kind": "object-storage"},
	})

	book, err := FromMappingResult(result)
	if err != nil {
		t.Fatalf("FromMappingResult() error = %v", err)
	}
	if len(book.Steps) != 1 {
		t.Fatalf("len(Steps) = %d, want 1", len(book.Steps))
	}
	if HasUnresolvedManualText(book) {
		t.Fatal("HasUnresolvedManualText() = true, want false")
	}
	if got := book.Steps[0].Metadata["kind"]; got != "object-storage" {
		t.Fatalf("step kind = %q, want object-storage", got)
	}
}

func TestRunNextResumesAfterFailedStep(t *testing.T) {
	service := NewService(t.TempDir())
	service.RegisterExecutor("fail-once", func(context.Context, domainrunbook.Step) domainrunbook.StepResult {
		return domainrunbook.StepResult{Status: domainrunbook.StepStatusFailed, Error: "boom"}
	})

	book := &domainrunbook.Runbook{
		ID:   "resume",
		Name: "Resume test",
		Steps: []domainrunbook.Step{{
			ID:               "first",
			Name:             "First",
			Type:             domainrunbook.StepTypeCommand,
			Status:           domainrunbook.StepStatusPending,
			Executor:         "fail-once",
			SuccessCondition: "passed",
		}, {
			ID:               "second",
			Name:             "Second",
			Type:             domainrunbook.StepTypeCommand,
			Status:           domainrunbook.StepStatusPending,
			Executor:         "noop",
			SuccessCondition: "passed",
		}},
	}

	if err := service.Save(book); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if _, err := service.RunNext(context.Background(), "resume"); err == nil {
		t.Fatal("RunNext() error = nil, want failed step error")
	}

	service.RegisterExecutor("fail-once", func(context.Context, domainrunbook.Step) domainrunbook.StepResult {
		return domainrunbook.StepResult{Status: domainrunbook.StepStatusPassed}
	})
	if _, err := service.RunNext(context.Background(), "resume"); err != nil {
		t.Fatalf("RunNext() retry error = %v", err)
	}
	if _, err := service.RunNext(context.Background(), "resume"); err != nil {
		t.Fatalf("RunNext() second step error = %v", err)
	}

	got, err := service.Get("resume")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	for _, step := range got.Steps {
		if step.Status != domainrunbook.StepStatusPassed {
			t.Fatalf("step %s status = %q, want passed", step.ID, step.Status)
		}
	}
}

func TestShellExecutorRunsInsideOutputDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mark.sh"), []byte("printf ok > marker.txt\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	service := NewService(dir)
	book := &domainrunbook.Runbook{
		ID:   "cwd",
		Name: "CWD test",
		Steps: []domainrunbook.Step{{
			ID:               "script",
			Name:             "Script",
			Type:             domainrunbook.StepTypeCommand,
			Status:           domainrunbook.StepStatusPending,
			Executor:         "shell",
			SuccessCondition: "marker written",
			Command:          []string{"sh", "mark.sh"},
		}},
	}
	if err := service.Save(book); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if _, err := service.RunNext(context.Background(), "cwd"); err != nil {
		t.Fatalf("RunNext() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "marker.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != "ok" {
		t.Fatalf("marker = %q, want ok", data)
	}
}

func TestRollbackRunsRollbackStepOnly(t *testing.T) {
	service := NewService(t.TempDir())
	ran := false
	service.RegisterExecutor("rollback", func(context.Context, domainrunbook.Step) domainrunbook.StepResult {
		ran = true
		return domainrunbook.StepResult{Status: domainrunbook.StepStatusPassed}
	})

	book := &domainrunbook.Runbook{
		ID:   "rollback",
		Name: "Rollback test",
		Steps: []domainrunbook.Step{{
			ID:               "deploy",
			Name:             "Deploy",
			Type:             domainrunbook.StepTypeCommand,
			Status:           domainrunbook.StepStatusPending,
			Executor:         "noop",
			SuccessCondition: "passed",
		}, {
			ID:               "rollback",
			Name:             "Rollback",
			Type:             domainrunbook.StepTypeRollback,
			Status:           domainrunbook.StepStatusPending,
			Executor:         "rollback",
			SuccessCondition: "passed",
		}},
	}
	if err := service.Save(book); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if err := service.Rollback(context.Background(), "rollback"); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if !ran {
		t.Fatal("rollback executor did not run")
	}
	got, err := service.Get("rollback")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Steps[0].Status != domainrunbook.StepStatusPending {
		t.Fatalf("forward step status = %q, want pending", got.Steps[0].Status)
	}
}

func TestRunAllSkipsRollbackSteps(t *testing.T) {
	service := NewService(t.TempDir())
	book := &domainrunbook.Runbook{
		ID:   "forward",
		Name: "Forward test",
		Steps: []domainrunbook.Step{{
			ID:               "deploy",
			Name:             "Deploy",
			Type:             domainrunbook.StepTypeCommand,
			Status:           domainrunbook.StepStatusPending,
			Executor:         "noop",
			SuccessCondition: "passed",
		}, {
			ID:               "rollback",
			Name:             "Rollback",
			Type:             domainrunbook.StepTypeRollback,
			Status:           domainrunbook.StepStatusPending,
			Executor:         "noop",
			SuccessCondition: "passed",
		}},
	}
	if err := service.Save(book); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if err := service.RunAll(context.Background(), "forward"); err != nil {
		t.Fatalf("RunAll() error = %v", err)
	}
	got, err := service.Get("forward")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Steps[1].Status != domainrunbook.StepStatusPending {
		t.Fatalf("rollback step status = %q, want pending", got.Steps[1].Status)
	}
}
