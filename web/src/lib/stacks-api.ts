import { fetchAPI } from './api';

// Types
export type StackStatus =
  | 'stopped'
  | 'running'
  | 'starting'
  | 'stopping'
  | 'error'
  | 'partial';

export interface ServiceInfo {
  name: string;
  image: string;
  replicas: number;
  running_count: number;
  status: string;
}

export interface CostEstimate {
  currency: string;
  compute: number;
  storage: number;
  database: number;
  network: number;
  other: number;
  total: number;
  details?: Record<string, number>;
  notes?: string[];
}

export interface DeploymentConfig {
  provider: string;
  region: string;
  ha_level: string;
  terraform_path?: string;
  estimated_cost?: CostEstimate;
}

export interface Stack {
  id: string;
  name: string;
  description?: string;
  compose_file: string;
  env_vars?: Record<string, string>;
  labels?: Record<string, string>;
  directory: string;
  status: StackStatus;
  services: ServiceInfo[];
  error?: string;
  created_at: string;
  updated_at: string;
  last_started_at?: string;
  last_stopped_at?: string;
  deployment_config?: DeploymentConfig;
  is_pending?: boolean;
}

export interface StacksResponse {
  stacks: Stack[];
  count: number;
}

export interface CreateStackRequest {
  name: string;
  description?: string;
  compose_file: string;
  env_vars?: Record<string, string>;
  labels?: Record<string, string>;
}

export interface UpdateStackRequest {
  name?: string;
  description?: string;
  compose_file?: string;
  env_vars?: Record<string, string>;
  labels?: Record<string, string>;
}

export interface CreatePendingStackRequest {
  name: string;
  description?: string;
  deployment_config: DeploymentConfig;
}

export interface StackLogsResponse {
  logs: string;
}

// API Functions

export async function listStacks(): Promise<StacksResponse> {
  return fetchAPI<StacksResponse>('/stacks');
}

export async function createStack(request: CreateStackRequest): Promise<Stack> {
  return fetchAPI<Stack>('/stacks', {
    method: 'POST',
    body: JSON.stringify(request),
  });
}

export async function createPendingStack(request: CreatePendingStackRequest): Promise<Stack> {
  return fetchAPI<Stack>('/stacks/pending', {
    method: 'POST',
    body: JSON.stringify(request),
  });
}

export async function getStack(id: string): Promise<Stack> {
  return fetchAPI<Stack>(`/stacks/${encodeURIComponent(id)}`);
}

export async function updateStack(
  id: string,
  request: UpdateStackRequest
): Promise<Stack> {
  return fetchAPI<Stack>(`/stacks/${encodeURIComponent(id)}`, {
    method: 'PUT',
    body: JSON.stringify(request),
  });
}

export async function deleteStack(id: string): Promise<{ status: string; id: string }> {
  return fetchAPI<{ status: string; id: string }>(
    `/stacks/${encodeURIComponent(id)}`,
    {
      method: 'DELETE',
    }
  );
}

export async function startStack(id: string): Promise<{ status: string; id: string }> {
  return fetchAPI<{ status: string; id: string }>(
    `/stacks/${encodeURIComponent(id)}/start`,
    {
      method: 'POST',
    }
  );
}

export async function stopStack(id: string): Promise<{ status: string; id: string }> {
  return fetchAPI<{ status: string; id: string }>(
    `/stacks/${encodeURIComponent(id)}/stop`,
    {
      method: 'POST',
    }
  );
}

export async function restartStack(id: string): Promise<{ status: string; id: string }> {
  return fetchAPI<{ status: string; id: string }>(
    `/stacks/${encodeURIComponent(id)}/restart`,
    {
      method: 'POST',
    }
  );
}

export async function getStackStatus(id: string): Promise<Stack> {
  return fetchAPI<Stack>(`/stacks/${encodeURIComponent(id)}/status`);
}

export async function getStackLogs(
  id: string,
  service?: string,
  tail?: number
): Promise<StackLogsResponse> {
  const params = new URLSearchParams();
  if (service) params.set('service', service);
  if (tail) params.set('tail', tail.toString());

  const queryString = params.toString();
  const url = `/stacks/${encodeURIComponent(id)}/logs${queryString ? `?${queryString}` : ''}`;

  return fetchAPI<StackLogsResponse>(url);
}

// Utility functions

export function getStatusColor(status: StackStatus): string {
  switch (status) {
    case 'running':
      return 'text-green-600 bg-green-100';
    case 'stopped':
      return 'text-gray-600 bg-gray-100';
    case 'starting':
    case 'stopping':
      return 'text-blue-600 bg-blue-100';
    case 'partial':
      return 'text-yellow-600 bg-yellow-100';
    case 'error':
      return 'text-red-600 bg-red-100';
    default:
      return 'text-gray-600 bg-gray-100';
  }
}

export function getStatusLabel(status: StackStatus): string {
  switch (status) {
    case 'running':
      return 'Running';
    case 'stopped':
      return 'Stopped';
    case 'starting':
      return 'Starting';
    case 'stopping':
      return 'Stopping';
    case 'partial':
      return 'Partial';
    case 'error':
      return 'Error';
    default:
      return status;
  }
}

export function getRunningServicesCount(stack: Stack): { running: number; total: number } {
  const total = stack.services.length;
  const running = stack.services.filter((s) => s.status === 'running').length;
  return { running, total };
}

// Get pending deployments (stacks with deployment config but not yet deployed)
export function getPendingDeployments(stacks: Stack[]): Stack[] {
  return stacks.filter((s) => s.is_pending && s.deployment_config);
}

// Format cost with currency
export function formatCost(cost: CostEstimate): string {
  const symbol = cost.currency === 'EUR' ? '€' : cost.currency === 'USD' ? '$' : cost.currency;
  return `${symbol}${cost.total.toFixed(2)}/mo`;
}

// Provider display names
export const providerDisplayNames: Record<string, string> = {
  hetzner: 'Hetzner',
  scaleway: 'Scaleway',
  ovh: 'OVH',
  'self-hosted': 'Self-Hosted',
};
