import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it, vi } from 'vitest';
import { AWSOperations } from './AWSOperations';

const api = vi.hoisted(() => ({ listAWSOperationsWorkspaces: vi.fn(), listAWSOperationServices: vi.fn(), listAWSOperationResources: vi.fn() }));
vi.mock('@/lib/aws-operations-api', () => api);

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}><MemoryRouter><AWSOperations /></MemoryRouter></QueryClientProvider>);
}

describe('AWSOperations', () => {
  it('explains that no post-cutover workspace is available', async () => {
    api.listAWSOperationsWorkspaces.mockResolvedValue({ workspaces: [] });
    renderPage();
    expect(await screen.findByText(/no AWS operations workspace/i)).toBeInTheDocument();
  });

  it('shows every server-provided service with a parameterised route and unavailable reason', async () => {
    api.listAWSOperationsWorkspaces.mockResolvedValue({ workspaces: [{ id: 'ws-1', name: 'Production', provider: 'aws', services: { lambda: { status: 'available', capabilities: ['list', 'invoke'] }, sqs: { status: 'unavailable', capabilities: [], reason: 'Cutover has not completed' }, s3: { status: 'unavailable', capabilities: [], reason: 'Local storage target is offline' } }, bindings: [] }] });
	api.listAWSOperationServices.mockResolvedValue({ services: [{ service: 'lambda', display_name: 'Lambda', family: 'Compute and orchestration', status: 'available', capabilities: ['list', 'invoke'] }, { service: 'sqs', display_name: 'SQS', family: 'Messaging and events', status: 'unavailable', capabilities: [], reason: 'Cutover has not completed' }, { service: 's3', display_name: 'S3', family: 'Storage, database and analytics', status: 'unavailable', capabilities: [], reason: 'Local storage target is offline' }] });
    renderPage();
    expect(await screen.findByRole('link', { name: /open Lambda operations/i })).toHaveAttribute('href', '/aws/lambda');
    expect(screen.getByRole('link', { name: /open S3 operations/i })).toHaveAttribute('href', '/aws/s3');
    expect(screen.getByText('Cutover has not completed')).toBeInTheDocument();
    expect(screen.getByText('Local storage target is offline')).toBeInTheDocument();
  });

  it('groups server metadata by family and filters the catalogue', async () => {
    api.listAWSOperationsWorkspaces.mockResolvedValue({ workspaces: [{ id: 'ws-1', name: 'Production', provider: 'aws', services: { lambda: { status: 'available', capabilities: [] }, s3: { status: 'unavailable', capabilities: [], reason: 'Offline' } }, bindings: [] }] });
    api.listAWSOperationServices.mockResolvedValue({ services: [{ service: 'lambda', display_name: 'Lambda', family: 'Compute and orchestration', status: 'available', capabilities: [] }, { service: 's3', display_name: 'S3', family: 'Storage, database and analytics', status: 'unavailable', capabilities: [], reason: 'Offline' }] });
    renderPage();
    expect(await screen.findByRole('heading', { name: 'Compute and orchestration' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Storage, database and analytics' })).toBeInTheDocument();
    fireEvent.change(screen.getByRole('searchbox', { name: /filter services/i }), { target: { value: 's3' } });
    expect(screen.queryByRole('link', { name: /open Lambda operations/i })).not.toBeInTheDocument();
    expect(screen.getByRole('link', { name: /open S3 operations/i })).toBeInTheDocument();
  });
});
