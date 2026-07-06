package conformance

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	domain "github.com/homeport/homeport/internal/domain/conformance"
)

func TestLoadReturnsManifest(t *testing.T) {
	dir := t.TempDir()
	data := []byte(`
provider: aws
service: S3
checks:
  discover: go test ./test/integration/aws -run S3
  cost: go test ./internal/domain/provider ./internal/app/providers
  provision: go test ./internal/infrastructure/mapper/... -run S3
  migrate: go test ./internal/app/datamigration -run S3
  api_compat: go test ./test/compat -run S3
  env_dns: go test ./internal/app/runbook ./internal/app/cutover
  ha: go test ./internal/domain/provider ./internal/infrastructure/generator/...
  backup: go test ./internal/app/backup
  validate: go test ./internal/app/runbook ./internal/app/metrics
  cutover: go test ./internal/app/cutover
  rollback: go test ./internal/app/cutover ./internal/app/backup
evidence:
  target: MinIO
  app_change_mode: adapter
`)
	if err := os.WriteFile(filepath.Join(dir, "aws-s3.yaml"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	manifest, err := NewService(dir).Load("aws", "S3")
	if err != nil {
		t.Fatal(err)
	}
	if missing := manifest.MissingChecks(); len(missing) != 0 {
		t.Fatalf("missing checks = %v", missing)
	}
	if manifest.Checks[domain.CheckDiscover] == "" || manifest.Evidence["target"] != "MinIO" {
		t.Fatalf("manifest = %#v", manifest)
	}
}

func TestRunRejectsGoTestWithNoTestsRun(t *testing.T) {
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "go.mod"), []byte("module example.test/conformance\n\ngo 1.22\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pkgDir := filepath.Join(workDir, "pkg")
	if err := os.Mkdir(pkgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "pkg_test.go"), []byte(`package pkg

import "testing"

func TestExisting(t *testing.T) {}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := completeManifest("go test ./pkg -run Missing")

	err := NewService(t.TempDir()).Run(context.Background(), manifest, workDir)
	if err == nil || !strings.Contains(err.Error(), "ran no tests") {
		t.Fatalf("expected no-tests failure, got %v", err)
	}
}

func TestRunExecutesEveryRequiredCheck(t *testing.T) {
	workDir := t.TempDir()
	marker := filepath.Join(workDir, "checks.log")
	manifest := completeManifest("printf x >> checks.log")

	if err := NewService(t.TempDir()).Run(context.Background(), manifest, workDir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(data); got != len(domain.RequiredChecks()) {
		t.Fatalf("ran %d checks, want %d", got, len(domain.RequiredChecks()))
	}
}

func completeManifest(command string) domain.Manifest {
	checks := map[domain.Check]string{}
	for _, check := range domain.RequiredChecks() {
		checks[check] = command
	}
	return domain.Manifest{
		Provider: "aws",
		Service:  "S3",
		Checks:   checks,
		Evidence: map[string]string{"target": "MinIO", "app_change_mode": "adapter"},
	}
}
