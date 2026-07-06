# Final 100 Percent A-Z Acceptance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the final “100% managed A-to-Z migration” claim pass only when every coverage row is full and the centralized wizard exposes automated support, adapters, or exact app-change instructions.

**Architecture:** Add one final acceptance target that combines coverage full-gate, backend tests, frontend build, wizard E2E, provider conformance, and documentation checks. Update README language only after the gate passes.

**Tech Stack:** Makefile, Go tests, Playwright, coverage CLI, README.

---

## Files

- Modify: `Makefile`
- Modify: `README.md`
- Modify: `web/tests/centralized-a-z-wizard.spec.ts`
- Modify: `docs/coverage/services.md`

## Task 1: Add a final acceptance target

- [ ] Modify `Makefile`:

```make
acceptance-100-managed: ## Run final 100% managed A-to-Z acceptance gate
	go test ./internal/domain/coverage ./internal/app/coverage ./internal/cli
	go test ./internal/domain/appchange ./internal/app/appchange
	go test ./internal/domain/conformance ./internal/app/conformance
	go test ./internal/infrastructure/parser/aws/... ./internal/infrastructure/parser/gcp/... ./internal/infrastructure/parser/azure/...
	go test ./internal/infrastructure/mapper/aws/... ./internal/infrastructure/mapper/gcp/... ./internal/infrastructure/mapper/azure/...
	go test ./internal/app/datamigration ./test/compat/... ./test/integration/aws/... ./test/integration/gcp/... ./test/integration/azure/...
	go run ./cmd/homeport coverage assert-full --catalog docs/coverage/services.yaml
	cd web && npm run build
	cd web && npm run test:e2e -- --reporter=line centralized-a-z-wizard.spec.ts a-z-wizard-smoke.spec.ts centralized-entry.spec.ts
```

- [ ] Run:

```bash
make acceptance-100-managed
```

Expected before Plans 23-27 are complete: fail. Expected after they are complete: pass.

## Task 2: Add wizard acceptance for coverage and app changes

- [ ] Extend `web/tests/centralized-a-z-wizard.spec.ts` mock analyze response to include:

```ts
app_change_report: {
  changes: [
    {
      service: 'Cloud Storage',
      resource_id: 'google_storage_bucket.assets',
      mode: 'generated_patch',
      reason: 'Native GCS endpoint must point to the HomePort storage adapter',
      file: '.env',
      search: 'storage.googleapis.com',
      replace: 'HOMEPORT_STORAGE_ENDPOINT',
      validation_cmd: 'grep -R HOMEPORT_STORAGE_ENDPOINT .',
    },
  ],
},
```

- [ ] Add test:

```ts
test('wizard shows exact application changes before export', async ({ page }) => {
  await mockCoreApis(page);
  await page.goto('/migrate');
  await page.getByRole('button', { name: /Analyze Source/i }).click();
  await expect(page.getByText(/Application changes/i)).toBeVisible();
  await expect(page.getByText(/Cloud Storage/i)).toBeVisible();
  await expect(page.getByText(/storage.googleapis.com/i)).toBeVisible();
  await expect(page.getByText(/HOMEPORT_STORAGE_ENDPOINT/i)).toBeVisible();
});
```

- [ ] Run:

```bash
cd web
PATH=/opt/homebrew/bin:$PATH npm run test:e2e -- --reporter=line centralized-a-z-wizard.spec.ts
```

Expected: pass.

## Task 3: Refuse final wording until coverage is full

- [ ] Search for unsupported claims:

```bash
rg -n "100% managed|fully managed|all services|tous les services|single clear A-to-Z UX|single clear A-Z UX" README.md docs web/src
```

Expected: every claim is either guarded by the acceptance target or refers to a passing `acceptance-100-managed` result.

- [ ] Update README only after `make acceptance-100-managed` passes with this text:

```markdown
### 100% managed A-to-Z migration

HomePort supports the centralized `/migrate` A-to-Z workflow for every service listed in `docs/coverage/services.md`.

This claim is gated by `make acceptance-100-managed`.

The gate requires every service coverage row to be `full`, every blocker to be cleared, every application compatibility issue to be handled by an adapter or exact generated change report, and the centralized wizard E2E checks to pass.
```

## Task 4: Final verification and commit

- [ ] Run:

```bash
go run ./cmd/homeport coverage --format markdown > /tmp/homeport-coverage-final.md
diff -u docs/coverage/services.md /tmp/homeport-coverage-final.md
make acceptance-100-managed
git diff --check
```

Expected: coverage markdown is generated from the catalog with no diff, full acceptance passes, diff check passes.

- [ ] Commit:

```bash
git add Makefile README.md web/tests/centralized-a-z-wizard.spec.ts docs/coverage/services.md docs/coverage/services.yaml internal/app/coverage/services.yaml
git commit -m "test: gate 100 percent managed A-to-Z migration"
```
