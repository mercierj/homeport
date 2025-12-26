import { fetchAPI } from './api';

export interface Container {
  id: string;
  name: string;
  image: string;
  status: string;
  state: 'running' | 'exited' | 'paused' | 'restarting';
  ports: PortBinding[];
  created: string;
  labels: Record<string, string>;
}

export interface PortBinding {
  host_port: string;
  container_port: string;
  protocol: string;
}

export interface ContainersResponse {
  containers: Container[];
  count: number;
}

export async function listContainers(stackId: string = 'default'): Promise<ContainersResponse> {
  return fetchAPI<ContainersResponse>(`/stacks/${stackId}/containers`);
}

export async function getContainerLogs(stackId: string, name: string, tail = 100): Promise<{ logs: string }> {
  return fetchAPI<{ logs: string }>(`/stacks/${stackId}/containers/${name}/logs?tail=${tail}`);
}

export async function restartContainer(stackId: string, name: string): Promise<void> {
  await fetchAPI(`/stacks/${stackId}/containers/${name}/restart`, { method: 'POST' });
}

export async function stopContainer(stackId: string, name: string): Promise<void> {
  await fetchAPI(`/stacks/${stackId}/containers/${name}/stop`, { method: 'POST' });
}

export async function startContainer(stackId: string, name: string): Promise<void> {
  await fetchAPI(`/stacks/${stackId}/containers/${name}/start`, { method: 'POST' });
}
