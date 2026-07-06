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
	"strings"
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
	go s.run(context.Background(), job)
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
	job.Logs = append(job.Logs, "$ "+name+" "+strings.Join(args, " "), string(out))
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
	root, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		target, err := filepath.Abs(filepath.Join(root, file.Name))
		if err != nil {
			return err
		}
		if !strings.HasPrefix(target, root+string(os.PathSeparator)) {
			return fmt.Errorf("invalid zip path: %s", file.Name)
		}
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
