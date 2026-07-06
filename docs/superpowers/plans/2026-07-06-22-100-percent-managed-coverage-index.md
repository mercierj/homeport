# 100 Percent Managed Coverage Index Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make “HomePort manages the full A-to-Z migration for every listed AWS, GCP, and Azure service” a hard, verifiable product gate.

**Architecture:** The coverage ledger remains the source of truth, but `mapped` is no longer treated as enough. A service is complete only when the ledger row is `full`, all checklist columns are true, blockers are empty, application changes are either avoided by an adapter or emitted as a concrete change report, and the centralized wizard shows the result.

**Tech Stack:** Go coverage service, CLI, API handlers, React wizard, Playwright, provider mappers, datamigration executors.

---

## Files

- Create: `docs/superpowers/plans/2026-07-06-23-aws-100-percent-managed-services.md`
- Create: `docs/superpowers/plans/2026-07-06-24-gcp-100-percent-managed-services.md`
- Create: `docs/superpowers/plans/2026-07-06-25-azure-100-percent-managed-services.md`
- Create: `docs/superpowers/plans/2026-07-06-26-application-compatibility-and-change-management.md`
- Create: `docs/superpowers/plans/2026-07-06-27-service-conformance-and-promotion-gates.md`
- Create: `docs/superpowers/plans/2026-07-06-28-final-100-percent-a-z-acceptance.md`
- Modify: `docs/superpowers/plans/2026-07-06-22-100-percent-managed-coverage-index.md`

## Execution Order

Run these plans after the already completed centralized UX plans:

1. `docs/superpowers/plans/2026-07-06-22-100-percent-managed-coverage-index.md`
2. `docs/superpowers/plans/2026-07-06-26-application-compatibility-and-change-management.md`
3. `docs/superpowers/plans/2026-07-06-23-aws-100-percent-managed-services.md`
4. `docs/superpowers/plans/2026-07-06-24-gcp-100-percent-managed-services.md`
5. `docs/superpowers/plans/2026-07-06-25-azure-100-percent-managed-services.md`
6. `docs/superpowers/plans/2026-07-06-27-service-conformance-and-promotion-gates.md`
7. `docs/superpowers/plans/2026-07-06-28-final-100-percent-a-z-acceptance.md`

Do not claim “100% managed” until Plan 28 passes.

## Task 1: Preserve the current truth before closing it

- [ ] Run:

```bash
go test ./internal/domain/coverage ./internal/app/coverage ./internal/cli
```

Expected: existing coverage ledger tests pass.

- [ ] Run:

```bash
go run ./cmd/homeport coverage --format markdown > /tmp/homeport-coverage-before.md
diff -u docs/coverage/services.md /tmp/homeport-coverage-before.md
```

Expected: no diff. If the generated markdown differs, update `docs/coverage/services.md` from the CLI output and commit that sync before changing coverage semantics.

## Task 2: Add a full-management summary command

**Files:**
- Modify: `internal/app/coverage/service.go`
- Modify: `internal/app/coverage/service_test.go`
- Modify: `internal/cli/coverage.go`
- Modify: `internal/cli/coverage_test.go`

- [ ] Add failing test to `internal/app/coverage/service_test.go`:

```go
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
```

- [ ] Run:

```bash
go test ./internal/app/coverage -run TestManagedSummaryCountsNonFullRows
```

Expected: fail because `ManagedSummary` does not exist.

- [ ] Implement the minimal summary in `internal/app/coverage/service.go`:

```go
type ManagedProviderSummary struct {
	Total   int `json:"total"`
	Full    int `json:"full"`
	NotFull int `json:"not_full"`
}

type ManagedSummary struct {
	Total      int                                `json:"total"`
	Full       int                                `json:"full"`
	NotFull    int                                `json:"not_full"`
	ByProvider map[string]ManagedProviderSummary `json:"by_provider"`
}

func (s *Service) ManagedSummary() ManagedSummary {
	out := ManagedSummary{ByProvider: map[string]ManagedProviderSummary{}}
	for _, row := range s.catalog.Services {
		out.Total++
		provider := out.ByProvider[row.Provider]
		provider.Total++
		if domaincoverage.ComputeStatus(row) == domaincoverage.StatusFull && row.Status == domaincoverage.StatusFull {
			out.Full++
			provider.Full++
		} else {
			out.NotFull++
			provider.NotFull++
		}
		out.ByProvider[row.Provider] = provider
	}
	return out
}
```

- [ ] Run:

```bash
go test ./internal/app/coverage -run TestManagedSummaryCountsNonFullRows
```

Expected: pass.

## Task 3: Add `homeport coverage assert-full`

**Files:**
- Modify: `internal/cli/coverage.go`
- Modify: `internal/cli/coverage_test.go`

- [ ] Add failing test to `internal/cli/coverage_test.go`:

```go
func TestCoverageAssertFullFailsWhenAnyRowIsNotFull(t *testing.T) {
	resetCoverageCommandState(t)
	dir := t.TempDir()
	coverageCatalog = filepath.Join(dir, "services.yaml")
	if err := os.WriteFile(coverageCatalog, []byte(`
services:
  - provider: aws
    service: S3
    resource_types: [aws_s3_bucket]
    status: mapped
`), 0o600); err != nil {
		t.Fatal(err)
	}

	err := coverageAssertFullCmd.RunE(coverageAssertFullCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "not 100% managed: 1 of 1 services are not full") {
		t.Fatalf("expected assert-full failure, got %v", err)
	}
}
```

- [ ] Run:

```bash
go test ./internal/cli -run TestCoverageAssertFullFailsWhenAnyRowIsNotFull
```

Expected: fail because `coverageAssertFullCmd` does not exist.

- [ ] Implement the subcommand in `internal/cli/coverage.go`:

```go
var coverageAssertFullCmd = &cobra.Command{
	Use:   "assert-full",
	Short: "Fail unless every coverage row is fully managed",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		catalog, err := appcoverage.LoadCatalog(coverageCatalog)
		if err != nil {
			return fmt.Errorf("load coverage catalog: %w", err)
		}
		summary := appcoverage.NewService(*catalog).ManagedSummary()
		if summary.NotFull > 0 {
			return fmt.Errorf("not 100%% managed: %d of %d services are not full", summary.NotFull, summary.Total)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "100%% managed: %d services full\n", summary.Total)
		return nil
	},
}
```

- [ ] Register it in `init()` beside the existing coverage subcommands:

```go
coverageAssertFullCmd.Flags().StringVar(&coverageCatalog, "catalog", "docs/coverage/services.yaml", "coverage catalog path")
coverageCmd.AddCommand(coverageAssertFullCmd)
```

- [ ] Run:

```bash
go test ./internal/cli -run TestCoverageAssertFullFailsWhenAnyRowIsNotFull
```

Expected: pass.

## Task 4: Commit the gate

- [ ] Run:

```bash
go test ./internal/domain/coverage ./internal/app/coverage ./internal/cli
```

Expected: pass.

- [ ] Run:

```bash
go run ./cmd/homeport coverage assert-full
```

Expected before Plans 23-27: fail with a non-zero count. This is correct.

- [ ] Commit:

```bash
git add internal/app/coverage/service.go internal/app/coverage/service_test.go internal/cli/coverage.go internal/cli/coverage_test.go docs/superpowers/plans/2026-07-06-22-100-percent-managed-coverage-index.md
git commit -m "test: add 100 percent managed coverage gate"
```

