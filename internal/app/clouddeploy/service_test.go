package clouddeploy

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUnzipTerraformWritesMainTF(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("terraform/main.tf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("terraform {}")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if err := unzipTerraform(buf.Bytes(), dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "terraform", "main.tf")); err != nil {
		t.Fatal(err)
	}
}

func TestStartContinuesAfterCallerContextCancelled(t *testing.T) {
	binDir := t.TempDir()
	callsPath := filepath.Join(t.TempDir(), "terraform-calls.log")
	terraformPath := filepath.Join(binDir, "terraform")
	if err := os.WriteFile(terraformPath, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$TERRAFORM_CALLS\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TERRAFORM_CALLS", callsPath)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	service := NewService(t.TempDir())
	job, err := service.Start(ctx, "job-1", terraformZip(t), false)
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current, err := service.Get(job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Status == StatusPlanned || current.Status == StatusFailed {
			job = current
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if job.Status != StatusPlanned {
		t.Fatalf("status = %q, error = %q, want %q", job.Status, job.Error, StatusPlanned)
	}
	calls, err := os.ReadFile(callsPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(calls); !strings.Contains(got, "init -input=false") || !strings.Contains(got, "plan -input=false -out=tfplan") {
		t.Fatalf("terraform calls = %q", got)
	}
}

func terraformZip(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("terraform/main.tf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("terraform {}")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
