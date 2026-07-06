# Final Acceptance Build And E2E Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make “A-to-Z wizard ready” a verifiable claim with a fixed Node toolchain, production build, and browser smoke test.

**Architecture:** Pin the frontend runtime to a Vite-compatible Node version, keep package-lock reproducible, add a lightweight Playwright smoke test that exercises the wizard shell and mocked API path, and document the exact acceptance command.

**Tech Stack:** npm, Vite, TypeScript, Playwright or existing browser verification wrapper, Go tests.

---

## Files

- Create: `web/.nvmrc`
- Modify: `web/package.json`
- Modify: `web/package-lock.json`
- Create: `web/tests/a-z-wizard-smoke.spec.ts`
- Create: `web/playwright.config.ts`
- Modify: `README.md`
- Modify: `Makefile`

## Task 1: Pin Node and repair lockfile

- [ ] Create `web/.nvmrc`:

```text
22.12.0
```

- [ ] Modify `web/package.json` to add:

```json
"engines": {
  "node": "^20.19.0 || >=22.12.0"
}
```

- [ ] Run with Node `22.12.0` or newer:

```bash
cd web
npm install
npm run build
```

Expected: `tsc -b && vite build` exits 0 and `web/dist/index.html` exists.

- [ ] Commit only the package changes produced by this install:

```bash
git add web/.nvmrc web/package.json web/package-lock.json
git commit -m "build: pin Vite-compatible Node runtime"
```

## Task 2: Add a browser smoke test

- [ ] Add `@playwright/test` as a dev dependency:

```bash
cd web
npm install -D @playwright/test
```

- [ ] Create `web/playwright.config.ts`. If the file already exists, replace it with this minimal config and re-add any existing project-specific settings only after the smoke test passes:

```ts
import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests',
  webServer: {
    command: 'npm run dev -- --host 127.0.0.1',
    url: 'http://127.0.0.1:5173',
    reuseExistingServer: true,
  },
  use: {
    baseURL: 'http://127.0.0.1:5173',
  },
});
```

- [ ] Create `web/tests/a-z-wizard-smoke.spec.ts`:

```ts
import { test, expect } from '@playwright/test';

test('migration wizard exposes the A-to-Z path without dead-end buttons', async ({ page }) => {
  await page.route('**/api/v1/**', async (route) => {
    const url = route.request().url();
    if (url.includes('/wizard/sessions')) {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          id: 'session-1',
          current_step: 'analyze',
          completed_steps: [],
          secrets_resolved: false,
        }),
      });
      return;
    }
    await route.fulfill({ contentType: 'application/json', body: '{}' });
  });

  await page.goto('/migrate');
  await expect(page.getByText(/Analyze/i)).toBeVisible();
  await expect(page.getByText(/Export/i)).toBeVisible();
  await expect(page.getByText(/Secrets/i)).toBeVisible();
  await expect(page.getByText(/Deploy/i)).toBeVisible();
  await expect(page.getByText(/Sync/i)).toBeVisible();
  await expect(page.getByText(/Cutover/i)).toBeVisible();
});
```

- [ ] Add scripts to `web/package.json`:

```json
"test:e2e": "playwright test",
"test:e2e:headed": "playwright test --headed"
```

- [ ] Run:

```bash
cd web
npm run test:e2e -- --reporter=line
```

Expected: one passing smoke test.

## Task 3: Add root acceptance target

- [ ] Modify `Makefile`:

```make
.PHONY: acceptance

acceptance: test web-build ## Run full A-to-Z readiness checks
	cd $(WEB_DIR) && npm run test:e2e -- --reporter=line
```

- [ ] Run:

```bash
make acceptance
```

Expected: Go tests pass, Vite build passes, Playwright smoke test passes.

## Task 4: Document readiness

- [ ] Modify `README.md` near the web dashboard section:

~~~markdown
### A-to-Z wizard readiness check

Use Node 22.12.0+ or 20.19.0+ for the web build:

```bash
cd web
nvm use
cd ..
make acceptance
```

The wizard is considered ready only when Go tests, the production web build, and the Playwright A-to-Z smoke test pass.
~~~

- [ ] Run `rg -n "A-to-Z wizard readiness" README.md`.
Expected: one match.

## Task 5: Final verification and commit

- [ ] Run:

```bash
git diff --check
GOCACHE=/private/tmp/exit-gafam-go-build go test ./...
cd web && npm run build
cd web && npm run test:e2e -- --reporter=line
```

- [ ] Commit:

```bash
git add web/.nvmrc web/package.json web/package-lock.json web/playwright.config.ts web/tests/a-z-wizard-smoke.spec.ts README.md Makefile
git commit -m "test: add A-to-Z wizard acceptance checks"
```
