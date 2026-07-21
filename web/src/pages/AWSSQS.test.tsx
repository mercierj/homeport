import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { AWSSQS } from './AWSSQS';

const api = vi.hoisted(() => ({ listAWSOperationsWorkspaces: vi.fn(), listAWSOperationResources: vi.fn(), listAWSSQSMessages: vi.fn(), retryAWSSQSMessage: vi.fn(), deleteAWSSQSMessage: vi.fn(), purgeAWSSQS: vi.fn() }));
vi.mock('@/lib/aws-operations-api', () => api);
afterEach(() => vi.clearAllMocks());
function renderPage() { const client = new QueryClient({ defaultOptions: { queries: { retry: false } } }); return render(<QueryClientProvider client={client}><MemoryRouter><AWSSQS /></MemoryRouter></QueryClientProvider>); }

describe('AWSSQS', () => {
  it('does not offer queue creation or deletion and confirms purge before dispatch', async () => {
    api.listAWSOperationsWorkspaces.mockResolvedValue({ workspaces: [{ id: 'ws-1', name: 'Production', provider: 'aws', services: { sqs: { status: 'available', capabilities: ['list', 'read', 'purge', 'retry'] } }, bindings: [] }] });
    api.listAWSOperationResources.mockResolvedValue({ resources: [{ id: 'queue-1', name: 'events', pending_count: 1, failed_count: 1, imported_resource_id: 'import-1', region: 'eu-west-3', tags: { team: 'ops' } }] });
    api.listAWSSQSMessages.mockResolvedValue({ messages: [] });
    renderPage();
    expect(await screen.findByText('events')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /create queue/i })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /delete queue/i })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: /events/i }));
    fireEvent.click(await screen.findByRole('button', { name: /purge pending messages/i }));
    expect(await screen.findByText(/permanently remove/i)).toBeInTheDocument();
  });

  it('filters messages, opens details, and confirms delete before dispatch', async () => {
    api.listAWSOperationsWorkspaces.mockResolvedValue({ workspaces: [{ id: 'ws-1', name: 'Production', provider: 'aws', services: { sqs: { status: 'available', capabilities: ['list', 'read', 'retry', 'delete'] } }, bindings: [] }] });
    api.listAWSOperationResources.mockResolvedValue({ resources: [{ name: 'events', pending_count: 1, imported_resource_id: 'import-1' }] });
    api.listAWSSQSMessages.mockResolvedValue({ messages: [{ ID: 'msg-1', Status: 'failed', Data: { type: 'resize' }, Attempts: 2 }] });
    renderPage();
    fireEvent.click(await screen.findByRole('button', { name: /events/i }));
    fireEvent.click(screen.getByRole('button', { name: 'failed' }));
    expect(await screen.findByText('msg-1')).toBeInTheDocument();
    fireEvent.click(screen.getByText('msg-1'));
    expect(await screen.findByText(/resize/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Delete message' }));
    expect(await screen.findByText(/permanently remove this message/i)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
    await waitFor(() => expect(api.deleteAWSSQSMessage).toHaveBeenCalledWith('ws-1', 'events', 'msg-1'));
  });

  it('rejects direct SQS operation navigation while the service is unavailable', async () => {
    api.listAWSOperationsWorkspaces.mockResolvedValue({ workspaces: [{ id: 'ws-1', name: 'Production', provider: 'aws', services: { sqs: { status: 'unavailable', capabilities: [], reason: 'Local target is not ready' } }, bindings: [] }] });
    renderPage();
    expect(await screen.findByText(/Local target is not ready/)).toBeInTheDocument();
    expect(api.listAWSOperationResources).not.toHaveBeenCalled();
  });
});
