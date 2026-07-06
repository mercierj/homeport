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
