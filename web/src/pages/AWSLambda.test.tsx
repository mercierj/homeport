import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { AWSLambda } from './AWSLambda';

const api = vi.hoisted(() => ({ listAWSOperationsWorkspaces: vi.fn(), listAWSOperationResources: vi.fn(), invokeAWSLambda: vi.fn(), updateAWSLambda: vi.fn(), deleteAWSLambda: vi.fn(), getAWSLambdaLogs: vi.fn() }));
vi.mock('@/lib/aws-operations-api', () => api);
afterEach(() => vi.clearAllMocks());
function renderPage() { const client = new QueryClient({ defaultOptions: { queries: { retry: false } } }); return render(<QueryClientProvider client={client}><MemoryRouter><AWSLambda /></MemoryRouter></QueryClientProvider>); }

describe('AWSLambda', () => {
  it('renders only actions supplied by Lambda capabilities and invokes a bound function', async () => {
    api.listAWSOperationsWorkspaces.mockResolvedValue({ workspaces: [{ id: 'ws-1', name: 'Production', provider: 'aws', services: { lambda: { status: 'available', capabilities: ['list', 'read', 'invoke'] } }, bindings: [] }] });
    api.listAWSOperationResources.mockResolvedValue({ resources: [{ id: 'fn-1', name: 'thumbnail', runtime: 'nodejs20', handler: 'index.handler', memory_mb: 256, timeout_seconds: 10, environment: { STAGE: 'prod' }, status: 'active', imported_resource_id: 'import-1', region: 'eu-west-3', tags: {} }] });
    api.invokeAWSLambda.mockResolvedValue({ status_code: 200, body: { ok: true } });
    renderPage();
    expect(await screen.findByText('thumbnail')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /delete/i })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: /invoke thumbnail/i }));
    fireEvent.click(await screen.findByRole('button', { name: /^invoke$/i }));
    await waitFor(() => expect(api.invokeAWSLambda).toHaveBeenCalledWith('ws-1', 'fn-1', {}));
  });

  it('shows detailed safe metadata and sends an update only when capability permits it', async () => {
    api.listAWSOperationsWorkspaces.mockResolvedValue({ workspaces: [{ id: 'ws-1', name: 'Production', provider: 'aws', services: { lambda: { status: 'available', capabilities: ['list', 'read', 'update', 'logs'] } }, bindings: [] }] });
    api.listAWSOperationResources.mockResolvedValue({ resources: [{ id: 'fn-1', name: 'thumbnail', runtime: 'nodejs20', handler: 'index.handler', memory_mb: 256, timeout_seconds: 10, environment: { SECRET: '<redacted>' }, status: 'active', invocation_count: 4, created_at: '2026-07-21T10:00:00Z', updated_at: '2026-07-21T11:00:00Z', imported_resource_id: 'import-1', region: 'eu-west-3', tags: { team: 'media' } }] });
    api.getAWSLambdaLogs.mockResolvedValue({ logs: [] }); api.updateAWSLambda.mockResolvedValue({});
    renderPage();
    fireEvent.click(await screen.findByText('thumbnail'));
    expect(screen.getByText('Configuration')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Environment' }));
    expect(screen.getByText(/SECRET/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    fireEvent.change(screen.getByLabelText('Memory (MB)'), { target: { value: '512' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save changes' }));
    await waitFor(() => expect(api.updateAWSLambda).toHaveBeenCalledWith('ws-1', 'fn-1', expect.objectContaining({ memory_mb: 512 })));
  });
});
