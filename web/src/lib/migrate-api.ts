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
  config?: Record<string, unknown>;
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
  consolidate: boolean;
}

// Stack consolidation preview types
export interface ConsolidationPreview {
  stacks: StackPreview[];
  source_count: number;
  service_count: number;
  reduction_ratio: number;
}

export interface StackPreview {
  type: string;
  display_name: string;
  resource_count: number;
  service_count: number;
  resources: string[];
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

// Progress event from streaming discover
export interface DiscoverProgressEvent {
  type: 'progress' | 'error' | 'complete';
  step: string;
  message: string;
  region?: string;
  service?: string;
  current_region: number;
  total_regions: number;
  current_service: number;
  total_services: number;
  resources_found: number;
}

// Streaming discover with progress updates
export async function discoverInfrastructureWithProgress(
  request: DiscoverRequest,
  onProgress: (event: DiscoverProgressEvent) => void
): Promise<AnalyzeResponse> {
  const response = await fetch(`${API_BASE}/migrate/discover/stream`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(request),
  });

  if (!response.ok) {
    throw new Error('Discovery failed');
  }

  const reader = response.body?.getReader();
  if (!reader) {
    throw new Error('Streaming not supported');
  }

  const decoder = new TextDecoder();
  let buffer = '';
  let result: AnalyzeResponse | null = null;

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;

    buffer += decoder.decode(value, { stream: true });

    // Parse SSE events
    const lines = buffer.split('\n');
    buffer = lines.pop() || ''; // Keep incomplete line in buffer

    let eventType = '';
    for (const line of lines) {
      if (line.startsWith('event:')) {
        eventType = line.slice(6).trim();
      } else if (line.startsWith('data:')) {
        const data = line.slice(5).trim();
        if (!data) continue;

        try {
          const parsed = JSON.parse(data);
          if (eventType === 'progress') {
            onProgress(parsed as DiscoverProgressEvent);
          } else if (eventType === 'error') {
            throw new Error(parsed.message || 'Discovery failed');
          } else if (eventType === 'complete') {
            result = parsed as AnalyzeResponse;
          }
        } catch (e) {
          if (e instanceof SyntaxError) {
            console.warn('Failed to parse SSE data:', data);
          } else {
            throw e;
          }
        }
      }
    }
  }

  if (!result) {
    throw new Error('No result received from discovery');
  }

  return result;
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

// Terraform export types
export interface ExportTerraformConfig {
  provider: 'hetzner' | 'scaleway' | 'ovh';
  project_name: string;
  domain: string;
  region: string;
}

// Export Terraform configuration as a ZIP file
export async function exportTerraform(
  resources: Resource[],
  config: ExportTerraformConfig
): Promise<Blob> {
  const response = await fetch(`${API_BASE}/migrate/export/${config.provider}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ resources, config }),
  });

  if (!response.ok) {
    throw new Error('Export failed');
  }

  return response.blob();
}
