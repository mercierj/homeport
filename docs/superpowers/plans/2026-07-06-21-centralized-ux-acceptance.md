# Centralized UX Acceptance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the claim “one clear centralized A-to-Z UX” verifiable by automated checks and documentation.

**Architecture:** Add Playwright coverage for source entry, bundle entry, route consolidation, deploy target choices, dry-run cutover behavior, and final completion. Update the acceptance target and README to define exactly what readiness means.

**Tech Stack:** Playwright, npm, Go tests, Makefile.

---

## Files

- Create: `web/tests/centralized-a-z-wizard.spec.ts`
- Modify: `web/tests/a-z-wizard-smoke.spec.ts`
- Modify: `Makefile`
- Modify: `README.md`

## Task 1: Add full centralized wizard E2E

- [ ] Create `web/tests/centralized-a-z-wizard.spec.ts`:

```ts
import { test, expect } from '@playwright/test';

function mockCoreApis(
  page: import('@playwright/test').Page,
  options: {
    currentStep?: 'analyze' | 'export' | 'secrets' | 'deploy' | 'sync' | 'cutover' | 'done';
    completedSteps?: string[];
    cutoverPreview?: unknown;
  } = {}
) {
  const currentStep = options.currentStep ?? 'analyze';
  const completedSteps = options.completedSteps ?? [];

  return page.route('**/api/v1/**', async (route) => {
    const url = route.request().url();
    const method = route.request().method();
    if (url.includes('/wizard/sessions')) {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          id: 'session-1',
          current_step: currentStep,
          completed_steps: completedSteps,
          bundle_id: 'bundle-1',
          secrets_resolved: false,
        }),
      });
      return;
    }
    if (url.includes('/bundle/upload')) {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          bundle_id: 'bundle-1',
          valid: true,
          manifest: {
            version: '1.0.0',
            format: 'hprt',
            created: new Date().toISOString(),
            homeport_version: 'test',
            source: { provider: 'aws', region: 'eu-west-1', resource_count: 1, analyzed_at: new Date().toISOString() },
            target: { type: 'docker-compose', consolidation: true, stack_count: 1 },
            stacks: [],
            checksums: {},
            rollback: { supported: true, snapshot_required: false },
          },
          secrets: [],
        }),
      });
      return;
    }
    if (url.includes('/bundle/bundle-1/compose')) {
      await route.fulfill({ contentType: 'application/json', body: JSON.stringify({ content: 'services:\\n  app:\\n    image: nginx' }) });
      return;
    }
    if (url.includes('/providers/compare')) {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          estimates: [
            { provider: 'hetzner', display_name: 'Hetzner', total_monthly: 10, currency: 'EUR' },
            { provider: 'scaleway', display_name: 'Scaleway', total_monthly: 12, currency: 'EUR' },
            { provider: 'ovh', display_name: 'OVH', total_monthly: 14, currency: 'EUR' },
          ],
          best_value: 'hetzner',
        }),
      });
      return;
    }
    if (url.includes('/cloud-deploy/start') && method === 'POST') {
      await route.fulfill({ contentType: 'application/json', body: JSON.stringify({ id: 'cloud-1', status: 'planned', apply: false, logs: [] }) });
      return;
    }
    if (url.includes('/cloud-deploy/cloud-1/apply')) {
      await route.fulfill({ contentType: 'application/json', body: JSON.stringify({ id: 'cloud-1', status: 'applied', apply: true, logs: [] }) });
      return;
    }
    if (url.includes('/cloud-deploy/cloud-1')) {
      await route.fulfill({ contentType: 'application/json', body: JSON.stringify({ id: 'cloud-1', status: 'applied', apply: true, logs: [] }) });
      return;
    }
    if (url.includes('/cutover/preview')) {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify(options.cutoverPreview ?? { pre_checks: [], dns_changes: [], post_checks: [], warnings: [] }),
      });
      return;
    }
    if (url.includes('/runbooks/bundle-1')) {
      await route.fulfill({ contentType: 'application/json', body: JSON.stringify({ id: 'bundle-1', steps: [] }) });
      return;
    }
    await route.fulfill({ contentType: 'application/json', body: '{}' });
  });
}

test('centralized wizard owns source and bundle entry points', async ({ page }) => {
  await mockCoreApis(page);
  await page.goto('/migrate');
  await expect(page.getByRole('button', { name: /Analyze Source/i })).toBeVisible();
  await expect(page.getByRole('button', { name: /Upload Bundle/i })).toBeVisible();
});

test('legacy deploy page is not a separate journey', async ({ page }) => {
  await mockCoreApis(page);
  await page.goto('/deploy');
  await expect(page).toHaveURL(/\/migrate$/);
  await expect(page.getByRole('button', { name: /Analyze Source/i })).toBeVisible();
});

test('cutover dry run does not complete migration', async ({ page }) => {
  await mockCoreApis(page, {
    currentStep: 'cutover',
    completedSteps: ['analyze', 'export', 'secrets', 'deploy', 'sync'],
    cutoverPreview: {
      pre_checks: [{ id: 'pre-1', name: 'HTTP check', endpoint: 'https://example.test' }],
      dns_changes: [{ id: 'dns-1', domain: 'example.test', record_type: 'A', old_value: '198.51.100.10', new_value: '203.0.113.10' }],
      post_checks: [],
      warnings: [],
    },
  });
  await page.goto('/migrate');
  await page.getByRole('button', { name: /Analyze Source/i }).click();
  await expect(page.getByRole('heading', { name: /DNS Cutover/i })).toBeVisible();
  await expect(page.getByRole('button', { name: /Run Dry Run/i })).toBeVisible();
  await expect(page.getByRole('heading', { name: /Migration Complete/i })).toHaveCount(0);
});

test('wizard deploy step contains local ssh and cloud targets', async ({ page }) => {
  await mockCoreApis(page, {
    currentStep: 'deploy',
    completedSteps: ['analyze', 'export', 'secrets'],
  });
  await page.goto('/migrate');
  await page.getByRole('button', { name: /Analyze Source/i }).click();
  await expect(page.getByRole('button', { name: /Local Docker/i })).toBeVisible();
  await expect(page.getByRole('button', { name: /Remote SSH/i })).toBeVisible();
  await expect(page.getByRole('button', { name: /Cloud Provider/i })).toBeVisible();
});

test('centralized navigation exposes no Deploy link', async ({ page }) => {
  await mockCoreApis(page);
  await page.goto('/');
  await expect(page.getByRole('link', { name: /Migrate/i })).toBeVisible();
  await expect(page.getByRole('link', { name: /^Deploy$/i })).toHaveCount(0);
});
```

- [ ] Run:

```bash
cd web
PATH=/opt/homebrew/bin:$PATH npm run test:e2e -- --reporter=line centralized-a-z-wizard.spec.ts
```

Expected: tests pass after Plans 18-20.

## Task 2: Strengthen existing smoke test labels

- [ ] Modify `web/tests/a-z-wizard-smoke.spec.ts` so it checks exact visible step labels and the completion label:

```ts
await expect(page.getByText('Analyze', { exact: true }).first()).toBeVisible();
await expect(page.getByText('Export', { exact: true }).first()).toBeVisible();
await expect(page.getByText('Secrets', { exact: true }).first()).toBeVisible();
await expect(page.getByText('Deploy', { exact: true }).first()).toBeVisible();
await expect(page.getByText('Sync', { exact: true }).first()).toBeVisible();
await expect(page.getByText('Cutover', { exact: true }).first()).toBeVisible();
await expect(page.getByText('Done', { exact: true }).first()).toBeVisible();
```

- [ ] Run:

```bash
cd web
PATH=/opt/homebrew/bin:$PATH npm run test:e2e -- --reporter=line a-z-wizard-smoke.spec.ts
```

Expected: smoke tests pass.

## Task 3: Make acceptance include centralized UX

- [ ] Modify `Makefile` acceptance target so it still runs Go tests, web build, and all Playwright tests. Keep this shape:

```make
acceptance: test web-build ## Run full A-to-Z readiness checks
	cd $(WEB_DIR) && npm run test:e2e -- --reporter=line
```

- [ ] Run:

```bash
make acceptance
```

Expected: Go tests, Vite build, and every Playwright test pass.

## Task 4: Document the actual product rule

- [ ] Modify `README.md` in the A-to-Z readiness section to include:

```markdown
### Centralized A-to-Z migration UX

The supported migration journey is `/migrate`.

`/migrate` is responsible for:
- analyzing a source or uploading a `.hprt` bundle,
- resolving required secrets,
- choosing local Docker, remote SSH, or EU cloud provider deployment,
- exporting Docker or Terraform artifacts when manual deployment is preferred,
- running sync and cutover checks,
- showing the final migration completion state.

`/deploy` is a legacy URL and redirects to `/migrate`; it must not present a second deployment wizard.
```

- [ ] Run:

```bash
rg -n "Centralized A-to-Z migration UX|legacy URL" README.md
```

Expected: both phrases are present.

## Task 5: Final verification and commit

- [ ] Run:

```bash
git diff --check
GOCACHE=/private/tmp/exit-gafam-go-build go test ./...
cd web && PATH=/opt/homebrew/bin:$PATH npm run build
cd web && PATH=/opt/homebrew/bin:$PATH npm run test:e2e -- --reporter=line
PATH=/opt/homebrew/bin:$PATH GOCACHE=/private/tmp/exit-gafam-go-build make acceptance
```

Expected: all commands pass.

- [ ] Commit:

```bash
git add web/tests/centralized-a-z-wizard.spec.ts web/tests/a-z-wizard-smoke.spec.ts Makefile README.md
git commit -m "test: verify centralized A-to-Z migration UX"
```
