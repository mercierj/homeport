package conformance

import (
	"os"
	"path/filepath"
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
