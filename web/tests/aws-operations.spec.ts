import { expect, test } from '@playwright/test';

const workspace = {
  id: 'workspace-1', discovery_id: 'discovery-1', name: 'Operations', provider: 'aws', bindings: [],
  services: {
    lambda: { status: 'available', capabilities: ['list', 'read', 'invoke'] },
    sqs: { status: 'available', capabilities: ['list', 'read', 'retry', 'purge'] },
    s3: { status: 'unavailable', capabilities: [], reason: 'Local object storage is not configured' },
  },
};

test('traverses post-cutover services without entering migration', async ({ page }) => {
  const requests: { url: string; method: string; body: string | null }[] = [];
  await page.route('**/api/v1/**', async (route) => {
    const url = route.request().url(); requests.push({ url, method: route.request().method(), body: route.request().postData() });
    if (url.includes('/docker/containers')) return route.fulfill({ json: { containers: [] } });
    if (url.includes('/aws/operations/workspaces') && url.includes('/lambda/resources')) return route.fulfill({ json: { workspace_id: 'workspace-1', service: 'lambda', resources: [{ id: 'fn-1', name: 'thumbnail', runtime: 'nodejs20', handler: 'index.handler', memory_mb: 256, timeout_seconds: 10, status: 'active', imported_resource_id: 'import-fn' }] } });
    if (url.includes('/aws/operations/workspaces') && url.includes('/sqs/resources/events/messages')) return route.fulfill({ json: { messages: [{ ID: 'message-1', Status: 'failed', Data: { event: 'resize' } }] } });
    if (url.includes('/aws/operations/workspaces') && url.includes('/sqs/resources')) return route.fulfill({ json: { workspace_id: 'workspace-1', service: 'sqs', resources: [{ name: 'events', PendingCount: 1, FailedCount: 1, TotalCount: 2, imported_resource_id: 'import-queue' }] } });
    if (url.includes('/aws/operations/workspaces')) return route.fulfill({ json: { workspaces: [workspace] } });
    return route.fulfill({ json: {} });
  });
  await page.goto('/aws');
  await expect(page.locator('h1', { hasText: 'AWS operations' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Operations', exact: true })).toBeVisible();
  await page.getByRole('link', { name: /open lambda operations/i }).click();
  await expect(page.getByText('thumbnail')).toBeVisible();
  await page.getByRole('button', { name: /invoke thumbnail/i }).click();
  await page.getByRole('button', { name: /^invoke$/i }).click();
  await page.goto('/aws/sqs');
  await page.getByRole('button', { name: /events/i }).click();
  await expect(page.getByText('message-1')).toBeVisible();
  await page.getByText('message-1').click();
  await page.getByRole('button', { name: /retry/i }).click();
  expect(requests.some((request) => request.url.includes('/migrate'))).toBeFalsy();
  expect(requests).toEqual(expect.arrayContaining([
    expect.objectContaining({ url: expect.stringContaining('/services/lambda/resources/fn-1/invoke'), method: 'POST', body: '{}' }),
    expect.objectContaining({ url: expect.stringContaining('/services/sqs/resources/events/messages/message-1/retry'), method: 'POST' }),
  ]));
});

test('shows an inactive SQS service without mutation controls', async ({ page }) => {
  await page.route('**/api/v1/**', async (route) => {
    const url = route.request().url();
    if (url.includes('/aws/operations/workspaces/workspace-1/services/lambda/resources')) {
      return route.fulfill({ json: { workspace_id: 'workspace-1', service: 'lambda', resources: [] } });
    }
    if (url.includes('/aws/operations/workspaces')) return route.fulfill({ json: { workspaces: [{ ...workspace, services: { lambda: workspace.services.lambda, sqs: { status: 'unavailable', capabilities: [], reason: 'Cutover pending' } } }] } });
    return route.fulfill({ json: { containers: [] } });
  });
  await page.goto('/aws');
  await expect(page.getByText('Cutover pending')).toBeVisible();
  await expect(page.getByRole('link', { name: /open SQS operations/i })).toHaveCount(1);
});

test('traverses an unavailable catalogue service through the shared route without AWS requests', async ({ page }) => {
  const requests: string[] = [];
  await page.route('**/api/v1/**', async (route) => {
    const url = route.request().url(); requests.push(url);
    if (url.includes('/services/s3/resources')) return route.fulfill({ json: { workspace_id: 'workspace-1', service: 's3', resources: [{ imported_resource_id: 'bucket-1', name: 'assets', target: 'MinIO', status: 'unavailable', reason: 'Local object storage is not configured' }] } });
    if (url.includes('/aws/operations/workspaces')) return route.fulfill({ json: { workspaces: [workspace] } });
    return route.fulfill({ json: { containers: [] } });
  });
  await page.goto('/aws');
  await page.getByRole('link', { name: /open S3 operations/i }).click();
  await expect(page.getByText('assets')).toBeVisible();
  expect(requests.some((url) => /amazonaws\.com|\/migrate/.test(url))).toBeFalsy();
});
