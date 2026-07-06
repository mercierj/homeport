package coverage

import (
	"bytes"
	"encoding/json"
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
