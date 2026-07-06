package cli

import (
	"bytes"
	"encoding/json"
	"os"
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
