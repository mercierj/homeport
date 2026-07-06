package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appcoverage "github.com/homeport/homeport/internal/app/coverage"
	domaincoverage "github.com/homeport/homeport/internal/domain/coverage"
)

func TestValidateCoverageFormat(t *testing.T) {
	if err := validateCoverageFormat("json"); err != nil {
		t.Fatalf("json should be valid: %v", err)
	}
	if err := validateCoverageFormat("xml"); err == nil {
		t.Fatal("xml should be invalid")
	}
}

func TestFilterCoverageServices(t *testing.T) {
	services := []domaincoverage.ServiceCoverage{
		{Provider: "aws", Service: "S3"},
		{Provider: "gcp", Service: "Cloud Storage"},
	}

	got := filterCoverageServices(services, "aws")
	if len(got) != 1 || got[0].Service != "S3" {
		t.Fatalf("expected only AWS S3, got %#v", got)
	}
}

func TestPrintCoverageTable(t *testing.T) {
	var buf bytes.Buffer
	if err := printCoverage(&buf, "table", testCoverageServices(), appcoverage.Drift{}); err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	if !strings.Contains(got, "PROVIDER SERVICE STATUS RESOURCES") ||
		!strings.Contains(got, "aws") ||
		!strings.Contains(got, "aws_s3_bucket") {
		t.Fatalf("unexpected table output:\n%s", got)
	}
}

func TestPrintCoverageJSON(t *testing.T) {
	var buf bytes.Buffer
	drift := appcoverage.Drift{MapperWithoutLedger: []string{"aws_db_instance"}}
	if err := printCoverage(&buf, "json", testCoverageServices(), drift); err != nil {
		t.Fatal(err)
	}

	var got struct {
		Services []domaincoverage.ServiceCoverage `json:"services"`
		Drift    appcoverage.Drift                `json:"drift"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Services) != 1 || got.Services[0].Service != "S3" || len(got.Drift.MapperWithoutLedger) != 1 {
		t.Fatalf("unexpected json output: %#v", got)
	}
}

func TestPrintCoverageMarkdown(t *testing.T) {
	var buf bytes.Buffer
	if err := printCoverage(&buf, "markdown", testCoverageServices(), appcoverage.Drift{}); err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	if !strings.Contains(got, "| PROVIDER | SERVICE | STATUS | RESOURCES |") ||
		!strings.Contains(got, "| aws | S3 | mapped | aws_s3_bucket |") {
		t.Fatalf("unexpected markdown output:\n%s", got)
	}
}

func TestHasCoverageDrift(t *testing.T) {
	if hasCoverageDrift(appcoverage.Drift{}) {
		t.Fatal("empty drift should be false")
	}
	if !hasCoverageDrift(appcoverage.Drift{LedgerWithoutMapper: []string{"missing"}}) {
		t.Fatal("ledger drift should be true")
	}
}

func TestCoverageCommandRejectsArgs(t *testing.T) {
	if coverageCmd.Args == nil {
		t.Fatal("coverage command should reject positional args")
	}
	if err := coverageCmd.Args(coverageCmd, []string{"extra"}); err == nil {
		t.Fatal("expected positional arg to be rejected")
	}
}

func TestCoverageCommandRunsOutsideRepoRoot(t *testing.T) {
	originalFormat := coverageFormat
	originalProvider := coverageProvider
	originalStrict := coverageStrict
	t.Cleanup(func() {
		coverageFormat = originalFormat
		coverageProvider = originalProvider
		coverageStrict = originalStrict
	})

	previousDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousDir); err != nil {
			t.Fatal(err)
		}
	})

	coverageFormat = "json"
	coverageProvider = "aws"
	coverageStrict = false

	var buf bytes.Buffer
	coverageCmd.SetOut(&buf)
	t.Cleanup(func() {
		coverageCmd.SetOut(nil)
	})

	if err := coverageCmd.RunE(coverageCmd, nil); err != nil {
		t.Fatalf("coverage command should not depend on cwd: %v", err)
	}
	if !strings.Contains(buf.String(), `"provider": "aws"`) {
		t.Fatalf("expected aws coverage output, got:\n%s", buf.String())
	}
}

func TestCoverageAddMissingCommandWritesCatalogAndMarkdown(t *testing.T) {
	resetCoverageCommandState(t)
	dir := t.TempDir()
	coverageCatalog = filepath.Join(dir, "services.yaml")
	coverageMarkdown = filepath.Join(dir, "services.md")
	addMissingProvider = "aws"
	addMissingService = "Athena"
	addMissingCategory = "analytics/data"
	addMissingSourceAPI = "athena"
	addMissingResources = "aws_athena_database, aws_athena_workgroup"
	addMissingTarget = "Trino"
	addMissingAPIStrategy = "recreate catalogs and export saved queries"

	if err := os.WriteFile(coverageCatalog, []byte("services: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	coverageAddMissingCmd.SetOut(&buf)
	t.Cleanup(func() { coverageAddMissingCmd.SetOut(nil) })

	if err := coverageAddMissingCmd.RunE(coverageAddMissingCmd, nil); err != nil {
		t.Fatal(err)
	}

	catalog, err := appcoverage.LoadCatalog(coverageCatalog)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Services) != 1 || catalog.Services[0].Status != domaincoverage.StatusMissing {
		t.Fatalf("unexpected catalog: %#v", catalog.Services)
	}
	markdown, err := os.ReadFile(coverageMarkdown)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(markdown), "| aws | Athena | missing | aws_athena_database, aws_athena_workgroup |") {
		t.Fatalf("missing markdown row:\n%s", markdown)
	}
}

func TestCoveragePromoteCommandWritesChecklist(t *testing.T) {
	resetCoverageCommandState(t)
	dir := t.TempDir()
	coverageCatalog = filepath.Join(dir, "services.yaml")
	coverageChecklistDir = filepath.Join(dir, "checklists")
	promoteProvider = "aws"
	promoteService = "Athena"
	promoteStatus = "guided"

	if err := os.WriteFile(coverageCatalog, []byte(`
services:
  - provider: aws
    service: Athena
    resource_types: []
    status: missing
    blocker: not modeled yet
`), 0o600); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	coveragePromoteCmd.SetOut(&buf)
	t.Cleanup(func() { coveragePromoteCmd.SetOut(nil) })

	if err := coveragePromoteCmd.RunE(coveragePromoteCmd, nil); err != nil {
		t.Fatal(err)
	}

	catalog, err := appcoverage.LoadCatalog(coverageCatalog)
	if err != nil {
		t.Fatal(err)
	}
	if got := catalog.Services[0]; got.Status != domaincoverage.StatusGuided || got.Blocker != "" {
		t.Fatalf("unexpected promoted row: %#v", got)
	}
	checklist := filepath.Join(coverageChecklistDir, "aws-athena.md")
	data, err := os.ReadFile(checklist)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Coverage Conformance Checklist") {
		t.Fatalf("unexpected checklist:\n%s", data)
	}
}

func testCoverageServices() []domaincoverage.ServiceCoverage {
	return []domaincoverage.ServiceCoverage{
		{
			Provider:      "aws",
			Service:       "S3",
			ResourceTypes: []string{"aws_s3_bucket"},
			Status:        domaincoverage.StatusMapped,
		},
	}
}

func resetCoverageCommandState(t *testing.T) {
	t.Helper()
	original := struct {
		catalog, markdown, checklistDir string
		addProvider, addService         string
		addCategory, addSourceAPI       string
		addResources, addTarget         string
		addStrategy, addImpossible      string
		promoteProvider, promoteService string
		promoteStatus                   string
	}{
		coverageCatalog, coverageMarkdown, coverageChecklistDir,
		addMissingProvider, addMissingService,
		addMissingCategory, addMissingSourceAPI,
		addMissingResources, addMissingTarget,
		addMissingAPIStrategy, addMissingImpossible,
		promoteProvider, promoteService,
		promoteStatus,
	}
	t.Cleanup(func() {
		coverageCatalog = original.catalog
		coverageMarkdown = original.markdown
		coverageChecklistDir = original.checklistDir
		addMissingProvider = original.addProvider
		addMissingService = original.addService
		addMissingCategory = original.addCategory
		addMissingSourceAPI = original.addSourceAPI
		addMissingResources = original.addResources
		addMissingTarget = original.addTarget
		addMissingAPIStrategy = original.addStrategy
		addMissingImpossible = original.addImpossible
		promoteProvider = original.promoteProvider
		promoteService = original.promoteService
		promoteStatus = original.promoteStatus
	})
}
