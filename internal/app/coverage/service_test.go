package coverage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	domaincoverage "github.com/homeport/homeport/internal/domain/coverage"
)

func TestFindDriftComparesCatalogWithRegisteredMappers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "services.yaml")
	err := os.WriteFile(path, []byte(`
services:
  - provider: aws
    service: S3
    resource_types: [aws_s3_bucket]
    discover: true
    status: mapped
  - provider: aws
    service: Missing
    resource_types: []
    status: missing
  - provider: aws
    service: Fake
    resource_types: [aws_fake_resource]
    discover: true
    status: mapped
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	catalog, err := LoadCatalog(path)
	if err != nil {
		t.Fatal(err)
	}

	service := NewService(*catalog)
	drift := service.FindDrift()

	if !slices.Contains(drift.LedgerWithoutMapper, "aws_fake_resource") {
		t.Fatalf("LedgerWithoutMapper = %v, want aws_fake_resource", drift.LedgerWithoutMapper)
	}
	if slices.Contains(drift.LedgerWithoutMapper, "") {
		t.Fatalf("LedgerWithoutMapper includes empty resource type: %v", drift.LedgerWithoutMapper)
	}
	if slices.Contains(drift.LedgerWithoutMapper, "aws_s3_bucket") {
		t.Fatalf("LedgerWithoutMapper includes supported aws_s3_bucket: %v", drift.LedgerWithoutMapper)
	}
	if !slices.IsSorted(drift.LedgerWithoutMapper) {
		t.Fatalf("LedgerWithoutMapper is not sorted: %v", drift.LedgerWithoutMapper)
	}
	if !slices.Contains(drift.MapperWithoutLedger, "aws_db_instance") {
		t.Fatalf("MapperWithoutLedger = %v, want aws_db_instance", drift.MapperWithoutLedger)
	}

	executors := service.RegisteredExecutors()
	if len(executors) == 0 {
		t.Fatal("RegisteredExecutors returned no executors")
	}
	if !slices.IsSorted(executors) {
		t.Fatalf("RegisteredExecutors is not sorted: %v", executors)
	}
}

func TestManagedSummaryCountsNonFullRows(t *testing.T) {
	catalog := Catalog{Services: []domaincoverage.ServiceCoverage{
		{Provider: "aws", Service: "S3", Status: domaincoverage.StatusFull, ManualStepsResolved: true, Discover: true, Cost: true, Provision: true, Migrate: true, APICompat: true, EnvDNS: true, HA: true, Backup: true, Validate: true, Cutover: true, Rollback: true},
		{Provider: "aws", Service: "Athena", Status: domaincoverage.StatusMissing, Blocker: "not modeled yet"},
		{Provider: "gcp", Service: "Cloud Storage", Status: domaincoverage.StatusGuided, Blocker: "adapter required"},
		{Provider: "azure", Service: "Azure VM", Status: domaincoverage.StatusMapped},
	}}

	summary := NewService(catalog).ManagedSummary()

	if summary.Total != 4 || summary.Full != 1 || summary.NotFull != 3 {
		t.Fatalf("summary = %#v, want 4 total, 1 full, 3 not full", summary)
	}
	if summary.ByProvider["aws"].NotFull != 1 || summary.ByProvider["gcp"].NotFull != 1 || summary.ByProvider["azure"].NotFull != 1 {
		t.Fatalf("provider summary = %#v", summary.ByProvider)
	}
}

func TestManagedGapsRequireTargetAndCompatibilityStrategy(t *testing.T) {
	withConformanceManifest(t, "aws", "S3")
	catalog := Catalog{Services: []domaincoverage.ServiceCoverage{{
		Provider: "aws", Service: "S3", Status: domaincoverage.StatusFull, ManualStepsResolved: true,
		Discover: true, Cost: true, Provision: true, Migrate: true, APICompat: true,
		EnvDNS: true, HA: true, Backup: true, Validate: true, Cutover: true, Rollback: true,
	}}}

	gaps := NewService(catalog).ManagedGaps()
	if len(gaps) != 1 || gaps[0] != "aws/S3" {
		t.Fatalf("gaps = %v, want aws/S3", gaps)
	}
}

func TestDefaultCatalogMatchesDocsLedger(t *testing.T) {
	docsCatalog, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "coverage", "services.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(defaultCatalogData, docsCatalog) {
		t.Fatal("embedded coverage catalog must match docs/coverage/services.yaml")
	}
}

func TestFindDriftMarshalsEmptyDriftListsAsArrays(t *testing.T) {
	service := NewService(Catalog{})
	catalog := Catalog{}
	for _, resourceType := range service.RegisteredMapperTypes() {
		catalog.Services = append(catalog.Services, domaincoverage.ServiceCoverage{
			ResourceTypes: []string{resourceType},
		})
	}

	data, err := json.Marshal(NewService(catalog).FindDrift())
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(string(data), `{"mapper_without_ledger":[],"ledger_without_mapper":[],"executors":`) {
		t.Fatalf("drift JSON = %s, want snake_case fields with empty arrays", data)
	}
}

func TestAddMissingAddsBacklogRow(t *testing.T) {
	catalog := Catalog{}

	err := catalog.AddMissing(domaincoverage.ServiceCoverage{
		Provider:                 "aws",
		Service:                  "Athena",
		Category:                 "analytics/data",
		SourceAPI:                "athena",
		ResourceTypes:            []string{"aws_athena_database"},
		Target:                   "Trino",
		APICompatibilityStrategy: "export queries and recreate catalogs",
	})
	if err != nil {
		t.Fatal(err)
	}

	got := catalog.Services[0]
	if got.Status != domaincoverage.StatusMissing || got.Blocker == "" || got.Category != "analytics/data" {
		t.Fatalf("unexpected missing row: %#v", got)
	}
	if err := catalog.AddMissing(got); err == nil {
		t.Fatal("duplicate missing row should be rejected")
	}
}

func TestPromoteRejectsFullUntilChecklistComplete(t *testing.T) {
	catalog := Catalog{Services: []domaincoverage.ServiceCoverage{
		{Provider: "aws", Service: "S3", Status: domaincoverage.StatusMapped},
	}}

	err := catalog.Promote("aws", "S3", domaincoverage.StatusFull)
	if err == nil || !strings.Contains(err.Error(), "checklist columns") {
		t.Fatalf("expected full promotion guard, got %v", err)
	}
}

func TestPromoteRejectsFullUntilManualStepsResolved(t *testing.T) {
	catalog := Catalog{Services: []domaincoverage.ServiceCoverage{
		{
			Provider: "aws", Service: "S3", Status: domaincoverage.StatusMapped,
			Target: "MinIO", APICompatibilityStrategy: "adapter",
			Discover: true, Cost: true, Provision: true, Migrate: true, APICompat: true,
			EnvDNS: true, HA: true, Backup: true, Validate: true, Cutover: true, Rollback: true,
		},
	}}

	err := catalog.Promote("aws", "S3", domaincoverage.StatusFull)
	if err == nil || !strings.Contains(err.Error(), "manual steps") {
		t.Fatalf("expected manual-step guard, got %v", err)
	}
}

func TestPromoteRejectsFullWithoutConformanceManifest(t *testing.T) {
	withConformanceDir(t, t.TempDir())
	catalog := Catalog{Services: []domaincoverage.ServiceCoverage{{
		Provider: "aws", Service: "S3", Status: domaincoverage.StatusMapped, ManualStepsResolved: true,
		Target: "MinIO", APICompatibilityStrategy: "adapter",
		Discover: true, Cost: true, Provision: true, Migrate: true, APICompat: true,
		EnvDNS: true, HA: true, Backup: true, Validate: true, Cutover: true, Rollback: true,
	}}}

	err := catalog.Promote("aws", "S3", domaincoverage.StatusFull)
	if err == nil || !strings.Contains(err.Error(), "conformance manifest") {
		t.Fatalf("expected conformance manifest guard, got %v", err)
	}
}

func TestPromoteRejectsFullWhenManualStepsOnlyProvidedAsFlag(t *testing.T) {
	catalog := Catalog{Services: []domaincoverage.ServiceCoverage{
		{
			Provider: "aws", Service: "S3", Status: domaincoverage.StatusMapped,
			Target: "MinIO", APICompatibilityStrategy: "adapter",
			Discover: true, Cost: true, Provision: true, Migrate: true, APICompat: true,
			EnvDNS: true, HA: true, Backup: true, Validate: true, Cutover: true, Rollback: true,
		},
	}}

	err := catalog.Promote("aws", "S3", domaincoverage.StatusFull, true)
	if err == nil || !strings.Contains(err.Error(), "manual steps") {
		t.Fatalf("expected persisted manual-step guard, got %v", err)
	}
}

func TestPromoteRecordsManualStepsResolved(t *testing.T) {
	catalog := Catalog{Services: []domaincoverage.ServiceCoverage{
		{Provider: "aws", Service: "S3", Status: domaincoverage.StatusMapped},
	}}

	if err := catalog.Promote("aws", "S3", domaincoverage.StatusMapped, true); err != nil {
		t.Fatal(err)
	}
	if !catalog.Services[0].ManualStepsResolved {
		t.Fatal("ManualStepsResolved = false, want true")
	}
}

func TestPromoteAllowsFullWhenManualStepsResolved(t *testing.T) {
	withConformanceManifest(t, "aws", "S3")
	catalog := Catalog{Services: []domaincoverage.ServiceCoverage{
		{
			Provider: "aws", Service: "S3", Status: domaincoverage.StatusMapped, ManualStepsResolved: true,
			Target: "MinIO", APICompatibilityStrategy: "adapter",
			Discover: true, Cost: true, Provision: true, Migrate: true, APICompat: true,
			EnvDNS: true, HA: true, Backup: true, Validate: true, Cutover: true, Rollback: true,
		},
	}}

	if err := catalog.Promote("aws", "S3", domaincoverage.StatusFull); err != nil {
		t.Fatal(err)
	}
	if got := catalog.Services[0].Status; got != domaincoverage.StatusFull {
		t.Fatalf("status = %q, want full", got)
	}
}

func withConformanceDir(t *testing.T, dir string) {
	t.Helper()
	original := ConformanceDir
	ConformanceDir = dir
	t.Cleanup(func() { ConformanceDir = original })
}

func withConformanceManifest(t *testing.T, provider, service string) {
	t.Helper()
	dir := t.TempDir()
	withConformanceDir(t, dir)
	data := fmt.Sprintf(`
provider: %s
service: %s
checks:
  discover: go test ./test/integration/%s
  cost: go test ./internal/domain/coverage
  provision: go test ./internal/infrastructure/mapper/...
  migrate: go test ./internal/app/datamigration
  api_compat: go test ./test/compat
  env_dns: go test ./internal/app/cutover
  ha: go test ./internal/domain/provider
  backup: go test ./internal/app/backup
  validate: go test ./internal/app/metrics
  cutover: go test ./internal/app/cutover
  rollback: go test ./internal/app/backup
evidence:
  target: MinIO
  app_change_mode: adapter
`, provider, service, provider)
	if err := os.WriteFile(filepath.Join(dir, provider+"-"+strings.ToLower(service)+".yaml"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPromoteUpdatesStatusWhenAllowed(t *testing.T) {
	catalog := Catalog{Services: []domaincoverage.ServiceCoverage{
		{
			Provider: "aws", Service: "S3", Status: domaincoverage.StatusMapped, Blocker: "not modeled yet",
			Discover: true, Cost: true, Provision: true, Migrate: true, APICompat: true,
			EnvDNS: true, HA: true, Backup: true, Validate: true, Cutover: true, Rollback: true,
		},
	}}

	if err := catalog.Promote("aws", "S3", domaincoverage.StatusGuided); err != nil {
		t.Fatal(err)
	}
	if got := catalog.Services[0]; got.Status != domaincoverage.StatusGuided || got.Blocker != "" {
		t.Fatalf("unexpected promoted row: %#v", got)
	}
}
