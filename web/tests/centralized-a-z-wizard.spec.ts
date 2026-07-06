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
