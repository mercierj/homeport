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
  await page.getByRole('button', { name: /Analyze Source/i }).click();
  await expect(page.getByText('Analyze', { exact: true }).first()).toBeVisible();
  await expect(page.getByText('Export', { exact: true }).first()).toBeVisible();
  await expect(page.getByText('Secrets', { exact: true }).first()).toBeVisible();
  await expect(page.getByText('Deploy', { exact: true }).first()).toBeVisible();
  await expect(page.getByText('Sync', { exact: true }).first()).toBeVisible();
  await expect(page.getByText('Cutover', { exact: true }).first()).toBeVisible();
});

test('bundle entry keeps the upload step after session creation', async ({ page }) => {
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
  const created = page.waitForResponse(
    (response) => response.url().includes('/wizard/sessions') && response.request().method() === 'POST'
  );
  await page.getByRole('button', { name: /Upload Bundle/i }).click();
  await created;

  await expect(page.getByRole('heading', { name: 'Upload Migration Bundle' })).toBeVisible();
});

test('wizard deploy step exposes local ssh and cloud choices in one flow', async ({ page }) => {
  await page.route('**/api/v1/**', async (route) => {
    const url = route.request().url();
    if (url.includes('/wizard/sessions')) {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          id: 'session-1',
          current_step: 'deploy',
          completed_steps: ['analyze', 'export', 'secrets'],
          bundle_id: 'bundle-1',
          secrets_resolved: true,
        }),
      });
      return;
    }
    if (url.includes('/bundle/bundle-1/compose')) {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({ content: 'services:\\n  app:\\n    image: nginx' }),
      });
      return;
    }
    await route.fulfill({ contentType: 'application/json', body: '{}' });
  });

  await page.goto('/migrate');
  await page.getByRole('button', { name: /Analyze Source/i }).click();
  await expect(page.getByText('Select Deployment Target')).toBeVisible();
  await expect(page.getByRole('button', { name: /Local Docker/i })).toBeVisible();
  await expect(page.getByRole('button', { name: /Remote SSH/i })).toBeVisible();
  await expect(page.getByRole('button', { name: /Cloud Provider/i })).toBeVisible();
  await expect(page.getByRole('button', { name: /Download Docker ZIP/i })).toBeVisible();
});

test('wizard shows a final completion screen instead of looping on cutover', async ({ page }) => {
  await page.route('**/api/v1/**', async (route) => {
    const url = route.request().url();
    if (url.includes('/wizard/sessions')) {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          id: 'session-1',
          current_step: 'cutover',
          completed_steps: ['analyze', 'export', 'secrets', 'deploy', 'sync'],
          bundle_id: 'bundle-1',
          secrets_resolved: true,
        }),
      });
      return;
    }
    if (url.includes('/cutover/preview')) {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({ pre_checks: [], dns_changes: [], post_checks: [], warnings: [] }),
      });
      return;
    }
    if (url.includes('/runbooks/bundle-1')) {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({ id: 'bundle-1', steps: [] }),
      });
      return;
    }
    await route.fulfill({ contentType: 'application/json', body: '{}' });
  });

  await page.goto('/migrate');
  await page.getByRole('button', { name: /Analyze Source/i }).click();
  await page.getByRole('button', { name: /Skip Cutover/i }).click();
  await page.getByRole('button', { name: /Complete Migration/i }).click();
  await expect(page.getByRole('heading', { name: /Migration Complete/i })).toBeVisible();
});
