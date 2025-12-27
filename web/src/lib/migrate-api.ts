import { fetchAPI } from './api';
import { API_BASE } from './config';

export interface Resource {
  id: string;
  name: string;
  type: string;
  category: string;
  arn?: string;
  region?: string;
  dependencies: string[];
  tags?: Record<string, string>;
}

export interface AnalyzeResponse {
  resources: Resource[];
  warnings: string[];
  provider: string;
}

export interface GenerateOptions {
  ha: boolean;
  include_migration: boolean;
  include_monitoring: boolean;
  domain: string;
}

export interface GenerateResponse {
  compose: string;
  scripts: Record<string, string>;
  docs: string;
}

export async function analyzeFiles(type: string, content: string): Promise<AnalyzeResponse> {
  return fetchAPI<AnalyzeResponse>('/migrate/analyze', {
    method: 'POST',
    body: JSON.stringify({ type, content }),
  });
}

export interface DiscoverRequest {
  provider: 'aws' | 'gcp' | 'azure';
  // AWS
  access_key_id?: string;
  secret_access_key?: string;
  region?: string;
  regions?: string[];
  // GCP
  project_id?: string;
  service_account_json?: string;
  // Azure
  subscription_id?: string;
  tenant_id?: string;
  client_id?: string;
  client_secret?: string;
}

export async function discoverInfrastructure(request: DiscoverRequest): Promise<AnalyzeResponse> {
  return fetchAPI<AnalyzeResponse>('/migrate/discover', {
    method: 'POST',
    body: JSON.stringify(request),
  });
}

export async function generateStack(
  resources: Resource[],
  options: Partial<GenerateOptions>
): Promise<GenerateResponse> {
  return fetchAPI<GenerateResponse>('/migrate/generate', {
    method: 'POST',
    body: JSON.stringify({ resources, options }),
  });
}

export async function downloadStack(
  resources: Resource[],
  options: Partial<GenerateOptions>
): Promise<Blob> {
  const response = await fetch(`${API_BASE}/migrate/download`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ resources, options }),
  });

  if (!response.ok) {
    throw new Error('Download failed');
  }

  return response.blob();
}

// Saved discovery state types
export interface DiscoveryState {
  id: string;
  name: string;
  provider: string;
  regions: string[] | null;
  resources?: Resource[];
  resource_count: number;
  created_at: string;
  updated_at: string;
}

// List all saved discoveries (summary without resources)
export async function listDiscoveries(): Promise<DiscoveryState[]> {
  return fetchAPI<DiscoveryState[]>('/migrate/discoveries', {
    method: 'GET',
  });
}

// Get a specific discovery with full resources
export async function getDiscovery(id: string): Promise<DiscoveryState> {
  return fetchAPI<DiscoveryState>(`/migrate/discoveries/${id}`, {
    method: 'GET',
  });
}

// Save a discovery for later use
export async function saveDiscovery(name: string, discovery: AnalyzeResponse): Promise<DiscoveryState> {
  return fetchAPI<DiscoveryState>('/migrate/discoveries', {
    method: 'POST',
    body: JSON.stringify({ name, discovery }),
  });
}

// Rename a saved discovery
export async function renameDiscovery(id: string, name: string): Promise<DiscoveryState> {
  return fetchAPI<DiscoveryState>(`/migrate/discoveries/${id}`, {
    method: 'PATCH',
    body: JSON.stringify({ name }),
  });
}

// Delete a saved discovery
export async function deleteDiscovery(id: string): Promise<void> {
  await fetchAPI<void>(`/migrate/discoveries/${id}`, {
    method: 'DELETE',
  });
}
