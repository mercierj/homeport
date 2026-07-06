package appchange

import (
	"os"
	"path/filepath"
	"testing"

	domain "github.com/homeport/homeport/internal/domain/appchange"
)

func TestScanPathDetectsGCSCodeChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.js")
	if err := os.WriteFile(path, []byte(`fetch("https://storage.googleapis.com/bucket")`), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := NewService().ScanPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Changes) != 1 || report.Changes[0].Mode != domain.ModeGeneratedPatch || report.Changes[0].Service != "Cloud Storage" {
		t.Fatalf("report = %#v", report)
	}
}
