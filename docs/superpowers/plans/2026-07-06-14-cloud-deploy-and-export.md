# Cloud Deploy And Export Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make cloud target handling explicit: Terraform ZIP export works from the main wizard, and optional Terraform apply is a tracked deployment job instead of a dead-end preview.

**Architecture:** Reuse existing Terraform ZIP generation. Add a backend cloud deployment job that runs `terraform init`, `terraform plan`, and optionally `terraform apply` in a temp directory, streaming status like local/SSH deployments.

**Tech Stack:** Existing Go migrate service, new small clouddeploy app service, existing React deployment components, Terraform CLI when installed.

---

## Files

- Create: `internal/app/clouddeploy/service.go`
- Create: `internal/app/clouddeploy/service_test.go`
- Create: `internal/api/handlers/clouddeploy.go`
- Create: `internal/api/handlers/clouddeploy_test.go`
- Modify: `internal/api/server.go`
- Modify: `web/src/lib/deploy-api.ts`
- Modify: `web/src/pages/Deploy.tsx`
- Modify: `web/src/components/DeploymentWizard/TerraformExport.tsx`
- Modify: `web/src/components/DeploymentWizard/TargetSelector.tsx`

## Task 1: Add cloud deployment job service

- [ ] Create `internal/app/clouddeploy/service.go`:

```go
package clouddeploy

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

type Status string

const (
	StatusPending Status = "pending"
	StatusRunning Status = "running"
	StatusPlanned Status = "planned"
	StatusApplied Status = "applied"
	StatusFailed  Status = "failed"
)

type Job struct {
	ID        string    `json:"id"`
	Status    Status    `json:"status"`
	Apply     bool      `json:"apply"`
	Logs      []string  `json:"logs"`
	Error     string    `json:"error,omitempty"`
	WorkDir   string    `json:"-"`
	CreatedAt time.Time `json:"created_at"`
}

type Service struct {
	baseDir string
	mu      sync.Mutex
	jobs    map[string]*Job
}

func NewService(baseDir string) *Service {
	if baseDir == "" {
		baseDir = os.TempDir()
	}
	return &Service{baseDir: baseDir, jobs: map[string]*Job{}}
}

func (s *Service) Start(ctx context.Context, id string, zipData []byte, apply bool) (*Job, error) {
	if id == "" {
		id = fmt.Sprintf("cloud-%d", time.Now().UnixNano())
	}
	workDir := filepath.Join(s.baseDir, "homeport-clouddeploy", id)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return nil, err
	}
	if err := unzipTerraform(zipData, workDir); err != nil {
		return nil, err
	}
	job := &Job{ID: id, Status: StatusPending, Apply: apply, WorkDir: filepath.Join(workDir, "terraform"), CreatedAt: time.Now().UTC()}
	s.mu.Lock()
	s.jobs[id] = job
	s.mu.Unlock()
	go s.run(ctx, job)
	return job, nil
}

func (s *Service) Get(id string) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job := s.jobs[id]
	if job == nil {
		return nil, fmt.Errorf("cloud deploy job not found: %s", id)
	}
	return job, nil
}

func (s *Service) run(ctx context.Context, job *Job) {
	s.set(job, StatusRunning, "")
	if !s.command(ctx, job, "terraform", "init", "-input=false") {
		return
	}
	if !s.command(ctx, job, "terraform", "plan", "-input=false", "-out=tfplan") {
		return
	}
	if !job.Apply {
		s.set(job, StatusPlanned, "")
		return
	}
	if !s.command(ctx, job, "terraform", "apply", "-input=false", "-auto-approve", "tfplan") {
		return
	}
	s.set(job, StatusApplied, "")
}

func (s *Service) command(ctx context.Context, job *Job, name string, args ...string) bool {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = job.WorkDir
	out, err := cmd.CombinedOutput()
	s.mu.Lock()
	job.Logs = append(job.Logs, "$ "+name+" "+fmt.Sprint(args), string(out))
	s.mu.Unlock()
	if err != nil {
		s.set(job, StatusFailed, err.Error())
		return false
	}
	return true
}

func (s *Service) set(job *Job, status Status, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job.Status = status
	job.Error = message
}

func unzipTerraform(data []byte, dir string) error {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		target := filepath.Join(dir, file.Name)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		src, err := file.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			_ = src.Close()
			return err
		}
		_, err = io.Copy(out, src)
		_ = src.Close()
		_ = out.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] Create `internal/app/clouddeploy/service_test.go` verifying `unzipTerraform` writes `terraform/main.tf`.
- [ ] Run `go test ./internal/app/clouddeploy`.
Expected: pass.

## Task 2: Add cloud deploy API

- [ ] Create `internal/api/handlers/clouddeploy.go`:
  - `POST /cloud-deploy/start` accepts `{resources, config, apply}`.
  - It calls `migrate.Service.GenerateTerraformZip`, then `clouddeploy.Service.Start`.
  - `GET /cloud-deploy/{id}` returns job status/logs.

- [ ] Create `internal/api/handlers/clouddeploy_test.go`:
  - start with invalid provider returns 400.
  - get missing job returns 404.

- [ ] Register routes in `internal/api/server.go`.
- [ ] Run `go test ./internal/api/handlers -run CloudDeploy`.
Expected: pass.

## Task 3: Make frontend choices explicit

- [ ] Modify `web/src/components/DeploymentWizard/TargetSelector.tsx`:
  - Rename “Export ZIP” label to “Export Docker ZIP”.
  - Keep cloud provider path for “Cloud Terraform”.

- [ ] Modify `web/src/pages/Deploy.tsx`:
  - For `selectedTarget === 'export'`, call existing `downloadStack(selectedResources, options)` and save the blob instead of showing “not available”.
  - In cloud configure, add two buttons: “Download Terraform ZIP” and “Plan Terraform Deploy”.
  - Only show “Apply Terraform” after a successful plan.

- [ ] Modify `web/src/lib/deploy-api.ts` to add:

```ts
export interface CloudDeployRequest {
  resources: import('./migrate-api').Resource[];
  config: import('./migrate-api').ExportTerraformConfig;
  apply: boolean;
}

export interface CloudDeployJob {
  id: string;
  status: 'pending' | 'running' | 'planned' | 'applied' | 'failed';
  apply: boolean;
  logs: string[];
  error?: string;
}
```

- [ ] Add `startCloudDeploy(request)` and `getCloudDeploy(id)` functions.
- [ ] Run `cd web && ./node_modules/.bin/tsc -b`.
Expected: pass.

## Task 4: Commit

- [ ] Run `gofmt -w internal/app/clouddeploy internal/api/handlers/clouddeploy.go internal/api/handlers/clouddeploy_test.go`.
- [ ] Run `go test ./internal/app/clouddeploy ./internal/api/handlers`.
- [ ] Run `cd web && ./node_modules/.bin/tsc -b`.
- [ ] Commit:

```bash
git add internal/app/clouddeploy internal/api/handlers/clouddeploy.go internal/api/handlers/clouddeploy_test.go internal/api/server.go web/src/lib/deploy-api.ts web/src/pages/Deploy.tsx web/src/components/DeploymentWizard/TerraformExport.tsx web/src/components/DeploymentWizard/TargetSelector.tsx
git commit -m "feat: add cloud terraform deploy flow"
```
