# Route And Entry Consolidation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `/migrate` the only visible migration/deployment journey entry point.

**Architecture:** Remove the sidebar `Deploy` item, redirect `/deploy` to `/migrate`, and point dashboard pending deployment CTAs to `/migrate`. Keep deployment API and reusable deployment components untouched because the wizard still needs them.

**Tech Stack:** React Router, Sidebar navigation, Playwright.

---

## Files

- Modify: `web/src/App.tsx`
- Modify: `web/src/components/navigation/Sidebar.tsx`
- Modify: `web/src/pages/Dashboard.tsx`
- Create: `web/tests/centralized-entry.spec.ts`

## Task 1: Add failing E2E for one visible journey entry

- [ ] Create `web/tests/centralized-entry.spec.ts`:

```ts
import { test, expect } from '@playwright/test';

test('migrate is the only visible migration/deployment journey entry', async ({ page }) => {
  await page.route('**/api/v1/**', async (route) => {
    const url = route.request().url();
    if (url.includes('/docker/containers')) {
      await route.fulfill({ contentType: 'application/json', body: JSON.stringify({ containers: [] }) });
      return;
    }
    if (url.includes('/stacks')) {
      await route.fulfill({ contentType: 'application/json', body: JSON.stringify({ stacks: [] }) });
      return;
    }
    await route.fulfill({ contentType: 'application/json', body: '{}' });
  });

  await page.goto('/');
  await expect(page.getByRole('link', { name: /Migrate/i })).toBeVisible();
  await expect(page.getByRole('link', { name: /^Deploy$/i })).toHaveCount(0);
});

test('legacy deploy URL redirects to migrate', async ({ page }) => {
  await page.route('**/api/v1/**', async (route) => {
    await route.fulfill({ contentType: 'application/json', body: JSON.stringify({ containers: [] }) });
  });

  await page.goto('/deploy');
  await expect(page).toHaveURL(/\/migrate$/);
  await expect(page.getByText('How would you like to start?')).toBeVisible();
});
```

- [ ] Run:

```bash
cd web
PATH=/opt/homebrew/bin:$PATH npm run test:e2e -- --reporter=line centralized-entry.spec.ts
```

Expected: fails because the sidebar still shows `Deploy` and `/deploy` still renders the Deploy page.

## Task 2: Remove the Deploy sidebar entry

- [ ] Modify `web/src/components/navigation/Sidebar.tsx`:
  - Remove `Rocket` from the `lucide-react` import list.
  - Remove this line from the Compute section:

```tsx
<SidebarItem icon={Rocket} href="/deploy" label="Deploy" />
```

- [ ] Run:

```bash
cd web
PATH=/opt/homebrew/bin:$PATH npm run build
```

Expected: build fails if `Rocket` is still imported or referenced; otherwise build passes.

## Task 3: Redirect `/deploy` to `/migrate`

- [ ] Modify `web/src/App.tsx`:
  - Remove this import:

```ts
import { Deploy } from './pages/Deploy';
```

  - Replace the deploy route:

```tsx
<Route path="/deploy" element={<Deploy />} />
```

with:

```tsx
<Route path="/deploy" element={<Navigate to="/migrate" replace />} />
```

- [ ] Run:

```bash
cd web
PATH=/opt/homebrew/bin:$PATH npm run build
```

Expected: build passes.

## Task 4: Point pending deployment CTAs to the canonical wizard

- [ ] Modify `web/src/pages/Dashboard.tsx`. Replace:

```tsx
onClick={() => navigate(`/deploy?stack=${stack.id}`)}
```

with:

```tsx
onClick={() => navigate('/migrate')}
```

- [ ] Run:

```bash
rg -n "/deploy|Deploy" web/src/App.tsx web/src/components/navigation/Sidebar.tsx web/src/pages/Dashboard.tsx
```

Expected: no `/deploy` navigation remains except the redirect route in `App.tsx`.

## Task 5: Verify and commit

- [ ] Run:

```bash
git diff --check
cd web && PATH=/opt/homebrew/bin:$PATH npm run build
cd web && PATH=/opt/homebrew/bin:$PATH npm run test:e2e -- --reporter=line centralized-entry.spec.ts
```

Expected: all commands pass.

- [ ] Commit:

```bash
git add web/src/App.tsx web/src/components/navigation/Sidebar.tsx web/src/pages/Dashboard.tsx web/tests/centralized-entry.spec.ts
git commit -m "fix: make migrate the single journey entry"
```
