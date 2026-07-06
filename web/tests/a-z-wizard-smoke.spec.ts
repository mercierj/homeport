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
