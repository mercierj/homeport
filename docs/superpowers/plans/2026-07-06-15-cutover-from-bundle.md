# Cutover From Bundle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop returning empty cutover data and derive DNS changes plus health checks from bundle metadata and deployment outputs.

**Architecture:** Add a backend preview endpoint for cutover suggestions. The UI calls it when the cutover step opens, lets the user edit suggestions, then starts the existing cutover service.

**Tech Stack:** Existing bundle handler, cutover handler/service, React CutoverStep, stdlib URL parsing.

---

## Files

- Create: `internal/app/cutover/preview.go`
- Create: `internal/app/cutover/preview_test.go`
- Modify: `internal/api/handlers/cutover.go`
- Modify: `internal/api/handlers/cutover_test.go`
- Modify: `web/src/lib/cutover-api.ts`
- Modify: `web/src/components/MigrationWizard/steps/CutoverStep.tsx`

## Task 1: Add preview builder

- [ ] Create `internal/app/cutover/preview.go`:

```go
package cutover

import (
	"fmt"
	"strings"
)

type PreviewInput struct {
	BundleID      string            `json:"bundle_id"`
	Domain        string            `json:"domain"`
	TargetIP      string            `json:"target_ip"`
	ServicePaths  map[string]string `json:"service_paths,omitempty"`
	HealthBaseURL string            `json:"health_base_url,omitempty"`
}

type Preview struct {
	PreChecks  []PreviewHealthCheck `json:"pre_checks"`
	DNSChanges []PreviewDNSChange   `json:"dns_changes"`
	PostChecks []PreviewHealthCheck `json:"post_checks"`
	Warnings   []string             `json:"warnings"`
}

type PreviewHealthCheck struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Endpoint string `json:"endpoint"`
}

type PreviewDNSChange struct {
	ID         string `json:"id"`
	Domain     string `json:"domain"`
	RecordType string `json:"record_type"`
	OldValue   string `json:"old_value"`
	NewValue   string `json:"new_value"`
	TTL        int    `json:"ttl"`
}

func BuildPreview(input PreviewInput) Preview {
	preview := Preview{}
	domain := strings.TrimSpace(input.Domain)
	targetIP := strings.TrimSpace(input.TargetIP)
	if domain == "" {
		preview.Warnings = append(preview.Warnings, "domain is required to suggest DNS changes")
	}
	if targetIP == "" {
		preview.Warnings = append(preview.Warnings, "target IP is required to suggest DNS changes")
	}
	if domain != "" && targetIP != "" {
		preview.DNSChanges = append(preview.DNSChanges, PreviewDNSChange{
			ID: "dns-root", Domain: domain, RecordType: "A", OldValue: "", NewValue: targetIP, TTL: 300,
		})
	}
	baseURL := strings.TrimRight(input.HealthBaseURL, "/")
	if baseURL == "" && domain != "" {
		baseURL = "https://" + domain
	}
	if baseURL != "" {
		preview.PreChecks = append(preview.PreChecks, PreviewHealthCheck{ID: "pre-health", Name: "Current service health", Type: "http", Endpoint: baseURL + "/health"})
		preview.PostChecks = append(preview.PostChecks, PreviewHealthCheck{ID: "post-health", Name: "Migrated service health", Type: "http", Endpoint: baseURL + "/health"})
		for name, path := range input.ServicePaths {
			preview.PostChecks = append(preview.PostChecks, PreviewHealthCheck{
				ID: fmt.Sprintf("post-%s", strings.ToLower(strings.ReplaceAll(name, " ", "-"))),
				Name: name + " health",
				Type: "http",
				Endpoint: baseURL + "/" + strings.TrimLeft(path, "/"),
			})
		}
	}
	return preview
}
```

- [ ] Create `internal/app/cutover/preview_test.go`:

```go
package cutover

import "testing"

func TestBuildPreviewCreatesDNSAndHealthChecks(t *testing.T) {
	preview := BuildPreview(PreviewInput{BundleID: "b1", Domain: "example.com", TargetIP: "203.0.113.10"})
	if len(preview.DNSChanges) != 1 || preview.DNSChanges[0].NewValue != "203.0.113.10" {
		t.Fatalf("unexpected dns changes: %#v", preview.DNSChanges)
	}
	if len(preview.PostChecks) != 1 || preview.PostChecks[0].Endpoint != "https://example.com/health" {
		t.Fatalf("unexpected post checks: %#v", preview.PostChecks)
	}
}

func TestBuildPreviewWarnsWhenMissingInputs(t *testing.T) {
	preview := BuildPreview(PreviewInput{})
	if len(preview.Warnings) != 2 {
		t.Fatalf("warnings = %#v", preview.Warnings)
	}
}
```

- [ ] Run `go test ./internal/app/cutover -run Preview`.
Expected: pass.

## Task 2: Add preview API

- [ ] Modify `internal/api/handlers/cutover.go`:
  - Add route `r.Post("/preview", h.PreviewCutover)`.
  - Implement `PreviewCutover` by decoding `appcutover.PreviewInput` and returning `appcutover.BuildPreview(input)`.

- [ ] Create `internal/api/handlers/cutover_test.go` with this test. If the file already exists, append the test without deleting existing coverage:

```go
func TestCutoverPreview(t *testing.T) {
	handler := NewCutoverHandler()
	req := httptest.NewRequest(http.MethodPost, "/cutover/preview", strings.NewReader(`{"bundle_id":"b1","domain":"example.com","target_ip":"203.0.113.10"}`))
	rec := httptest.NewRecorder()
	router := chi.NewRouter()
	handler.RegisterRoutes(router)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "203.0.113.10") {
		t.Fatalf("missing target IP in response: %s", rec.Body.String())
	}
}
```

- [ ] Run `go test ./internal/api/handlers -run CutoverPreview`.
Expected: pass.

## Task 3: Wire UI cutover suggestions

- [ ] Modify `web/src/lib/cutover-api.ts` to add `previewCutover(input)` returning `{pre_checks,dns_changes,post_checks,warnings}`.
- [ ] Modify `web/src/components/MigrationWizard/steps/CutoverStep.tsx`:
  - Replace `buildFromManifest()` empty return with a call to `previewCutover`.
  - Build the preview request from `bundleId`, `domain`, and a new required `targetIP` input shown in the Cutover step.
  - Disable “Run Dry Run”, “Start Cutover”, and “Complete” until `targetIP` is non-empty when DNS changes are needed.
  - If preview returns warnings, show them in the existing warning panel.
  - If preview returns DNS/health checks, populate `dnsChanges` and `healthChecks`.

- [ ] Add a pure helper `buildCutoverPreviewRequest(bundleId: string | null, domain: string, targetIP: string)` in `CutoverStep.tsx`. It must return `{ bundle_id, domain, target_ip }` with trimmed strings. TypeScript compilation is the minimum check for this helper.
- [ ] Run `cd web && ./node_modules/.bin/tsc -b`.
Expected: pass.

## Task 4: Commit

- [ ] Run `gofmt -w internal/app/cutover/preview.go internal/app/cutover/preview_test.go internal/api/handlers/cutover.go internal/api/handlers/cutover_test.go`.
- [ ] Run `go test ./internal/app/cutover ./internal/api/handlers`.
- [ ] Run `cd web && ./node_modules/.bin/tsc -b`.
- [ ] Commit:

```bash
git add internal/app/cutover/preview.go internal/app/cutover/preview_test.go internal/api/handlers/cutover.go internal/api/handlers/cutover_test.go web/src/lib/cutover-api.ts web/src/components/MigrationWizard/steps/CutoverStep.tsx
git commit -m "feat: derive cutover plan from migration state"
```
