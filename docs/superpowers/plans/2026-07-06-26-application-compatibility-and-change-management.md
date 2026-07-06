# Application Compatibility And Change Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ensure every migration either avoids customer code changes through a compatibility adapter or emits exact, validated code/config changes in the A-to-Z wizard.

**Architecture:** Add a small cross-provider app-change report model and scanner. Provider service implementations attach adapter endpoints, env rewrites, SDK compatibility notes, and generated patch instructions to the migration bundle; the wizard displays them before deploy.

**Tech Stack:** Go bundle/runbook generation, lightweight source scanner, React wizard, Playwright.

---

## Files

- Create: `internal/domain/appchange/report.go`
- Create: `internal/domain/appchange/report_test.go`
- Create: `internal/app/appchange/service.go`
- Create: `internal/app/appchange/service_test.go`
- Modify: `internal/app/migrate/service.go`
- Modify: `internal/api/handlers/migrate.go`
- Modify: `web/src/lib/migrate-api.ts`
- Modify: `web/src/components/MigrationWizard/steps/AnalyzeStep.tsx`
- Modify: `web/src/components/MigrationWizard/steps/ExportStep.tsx`
- Modify: `web/tests/centralized-a-z-wizard.spec.ts`

## Task 1: Add the app-change report domain model

- [ ] Create `internal/domain/appchange/report.go`:

```go
package appchange

type Mode string

const (
	ModeNone           Mode = "none"
	ModeAdapter        Mode = "adapter"
	ModeGeneratedPatch Mode = "generated_patch"
	ModeManualReview   Mode = "manual_review"
)

type Change struct {
	Service       string `json:"service"`
	ResourceID    string `json:"resource_id"`
	Mode          Mode   `json:"mode"`
	Reason        string `json:"reason"`
	AdapterURL    string `json:"adapter_url,omitempty"`
	File          string `json:"file,omitempty"`
	Search        string `json:"search,omitempty"`
	Replace       string `json:"replace,omitempty"`
	ValidationCmd string `json:"validation_cmd,omitempty"`
}

type Report struct {
	Changes []Change `json:"changes"`
}

func (r Report) RequiresAction() bool {
	for _, change := range r.Changes {
		if change.Mode == ModeGeneratedPatch || change.Mode == ModeManualReview {
			return true
		}
	}
	return false
}
```

- [ ] Create `internal/domain/appchange/report_test.go`:

```go
package appchange

import "testing"

func TestRequiresActionIgnoresAdapterOnlyChanges(t *testing.T) {
	report := Report{Changes: []Change{{Service: "S3", Mode: ModeAdapter, AdapterURL: "http://minio:9000"}}}
	if report.RequiresAction() {
		t.Fatal("adapter-only report should not require customer code action")
	}
}

func TestRequiresActionDetectsGeneratedPatch(t *testing.T) {
	report := Report{Changes: []Change{{Service: "Cloud Storage", Mode: ModeGeneratedPatch, File: ".env"}}}
	if !report.RequiresAction() {
		t.Fatal("generated patch should require customer action")
	}
}
```

- [ ] Run:

```bash
go test ./internal/domain/appchange
```

Expected: pass.

## Task 2: Add a minimal source scanner

- [ ] Create `internal/app/appchange/service.go`:

```go
package appchange

import (
	"os"
	"path/filepath"
	"strings"

	domain "github.com/homeport/homeport/internal/domain/appchange"
)

type Service struct{}

func NewService() *Service { return &Service{} }

func (s *Service) ScanPath(root string) (domain.Report, error) {
	report := domain.Report{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(data)
		switch {
		case strings.Contains(text, "AWS_S3_ENDPOINT") || strings.Contains(text, "s3.amazonaws.com"):
			report.Changes = append(report.Changes, domain.Change{Service: "S3", Mode: domain.ModeAdapter, File: path, Reason: "S3 endpoint can be redirected to MinIO", AdapterURL: "http://minio:9000"})
		case strings.Contains(text, "storage.googleapis.com"):
			report.Changes = append(report.Changes, domain.Change{Service: "Cloud Storage", Mode: domain.ModeGeneratedPatch, File: path, Search: "storage.googleapis.com", Replace: "${HOMEPORT_STORAGE_ENDPOINT}", Reason: "Native GCS endpoint must point to the HomePort storage adapter", ValidationCmd: "grep -R HOMEPORT_STORAGE_ENDPOINT ."})
		case strings.Contains(text, "servicebus.windows.net"):
			report.Changes = append(report.Changes, domain.Change{Service: "Service Bus", Mode: domain.ModeGeneratedPatch, File: path, Search: "servicebus.windows.net", Replace: "${HOMEPORT_SERVICEBUS_ENDPOINT}", Reason: "Azure Service Bus SDK endpoint must point to the HomePort adapter", ValidationCmd: "grep -R HOMEPORT_SERVICEBUS_ENDPOINT ."})
		}
		return nil
	})
	return report, err
}
```

- [ ] Create `internal/app/appchange/service_test.go`:

```go
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
```

- [ ] Run:

```bash
go test ./internal/app/appchange
```

Expected: pass.

## Task 3: Attach reports to analysis and bundles

- [ ] Extend the migrate analyze response type with:

```go
AppChangeReport appchange.Report `json:"app_change_report"`
```

- [ ] When a source path is available, call:

```go
report, err := appchange.NewService().ScanPath(req.Path)
if err == nil {
	resp.AppChangeReport = report
}
```

- [ ] Persist the report in `.hprt` bundle metadata so upload/resume keeps the same change information.

- [ ] Add a handler test proving `/api/v1/migrate/analyze` returns `app_change_report.changes` for a fixture containing `storage.googleapis.com`.

## Task 4: Show the report in the centralized wizard

- [ ] Extend `web/src/lib/migrate-api.ts`:

```ts
export interface AppChange {
  service: string;
  resource_id: string;
  mode: 'none' | 'adapter' | 'generated_patch' | 'manual_review';
  reason: string;
  adapter_url?: string;
  file?: string;
  search?: string;
  replace?: string;
  validation_cmd?: string;
}

export interface AppChangeReport {
  changes: AppChange[];
}
```

- [ ] Add `app_change_report?: AppChangeReport` to the analyze response type.

- [ ] In `AnalyzeStep`, render a compact “Application changes” section when `analysisResult.app_change_report.changes.length > 0`, with service, mode, file, reason, and adapter URL or replacement string.

- [ ] In `ExportStep`, include the same report in the bundle summary so users see required app changes before creating `.hprt`.

## Task 5: Verify and commit app-change management

- [ ] Run:

```bash
go test ./internal/domain/appchange ./internal/app/appchange ./internal/api/handlers
cd web && PATH=/opt/homebrew/bin:$PATH npm run build
cd web && PATH=/opt/homebrew/bin:$PATH npm run test:e2e -- --reporter=line centralized-a-z-wizard.spec.ts
```

Expected: pass.

- [ ] Commit:

```bash
git add internal/domain/appchange internal/app/appchange internal/app/migrate/service.go internal/api/handlers/migrate.go web/src/lib/migrate-api.ts web/src/components/MigrationWizard/steps/AnalyzeStep.tsx web/src/components/MigrationWizard/steps/ExportStep.tsx web/tests/centralized-a-z-wizard.spec.ts
git commit -m "feat: report application compatibility changes"
```
