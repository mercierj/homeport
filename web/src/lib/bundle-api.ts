import { fetchAPI } from './api';
import { API_BASE } from './config';
import type { Resource } from './migrate-api';
import type { BundleManifest, SecretReference } from '@/stores/wizard';

// Bundle creation request
export interface CreateBundleRequest {
  resources: Resource[];
  options: {
    domain?: string;
    consolidate?: boolean;
    detect_secrets?: boolean;
    include_migration?: boolean;
    include_monitoring?: boolean;
  };
}

// Bundle creation response
export interface CreateBundleResponse {
  bundle_id: string;
  manifest: BundleManifest;
  secrets: SecretReference[];
  download_url: string;
}

// Bundle info response
export interface BundleInfoResponse {
  bundle_id: string;
  manifest: BundleManifest;
  secrets: SecretReference[];
  files: string[];
  size: number;
  created_at: string;
}

// Upload bundle response
export interface UploadBundleResponse {
  bundle_id: string;
  manifest: BundleManifest;
  secrets: SecretReference[];
  valid: boolean;
  errors?: string[];
}

// Provide secrets request
export interface ProvideSecretsRequest {
  secrets: Record<string, string>;
}

// Provide secrets response
export interface ProvideSecretsResponse {
  success: boolean;
  resolved: string[];
  missing: string[];
}

// Pull secrets request
export interface PullSecretsRequest {
  provider: 'aws' | 'gcp' | 'azure';

  // AWS credentials
  access_key_id?: string;
  secret_access_key?: string;
  region?: string;

  // GCP credentials
  project_id?: string;
  service_account_json?: string;

  // Azure credentials
  subscription_id?: string;
  tenant_id?: string;
  client_id?: string;
  client_secret?: string;
}

// Pull secrets response
export interface PullSecretsResponse {
  success: boolean;
  resolved: Record<string, string>;
  failed: string[];
  errors?: Record<string, string>;
}

// Import bundle request
export interface ImportBundleRequest {
  bundle_id: string;
  target: 'local' | 'ssh';
  ssh_config?: {
    host: string;
    port: number;
    username: string;
    auth_method: 'key' | 'password';
    key_path?: string;
    password?: string;
  };
  deploy: boolean;
  dry_run?: boolean;
}

// Import bundle response
export interface ImportBundleResponse {
  import_id: string;
  status: 'started' | 'completed' | 'failed';
  extracted_to: string;
  missing_secrets: string[];
}

// Export bundle from analyzed resources
export async function exportBundle(request: CreateBundleRequest): Promise<CreateBundleResponse> {
  return fetchAPI<CreateBundleResponse>('/bundle/export', {
    method: 'POST',
    body: JSON.stringify(request),
  });
}

// Upload an existing .hprt bundle file
export async function uploadBundle(file: File): Promise<UploadBundleResponse> {
  const formData = new FormData();
  formData.append('bundle', file);

  const response = await fetch(`${API_BASE}/bundle/upload`, {
    method: 'POST',
    credentials: 'include',
    body: formData,
  });

  if (!response.ok) {
    const error = await response.json().catch(() => ({ message: 'Upload failed' }));
    throw new Error(error.message);
  }

  return response.json();
}

// Get bundle information
export async function getBundleInfo(bundleId: string): Promise<BundleInfoResponse> {
  return fetchAPI<BundleInfoResponse>(`/bundle/${bundleId}`, {
    method: 'GET',
  });
}

// Download bundle file
export async function downloadBundle(bundleId: string): Promise<Blob> {
  const response = await fetch(`${API_BASE}/bundle/${bundleId}/download`, {
    method: 'GET',
    credentials: 'include',
  });

  if (!response.ok) {
    throw new Error('Download failed');
  }

  return response.blob();
}

// Get required secrets for a bundle
export async function getBundleSecrets(bundleId: string): Promise<SecretReference[]> {
  return fetchAPI<SecretReference[]>(`/bundle/${bundleId}/secrets`, {
    method: 'GET',
  });
}

// Provide secret values for a bundle
export async function provideSecrets(
  bundleId: string,
  request: ProvideSecretsRequest
): Promise<ProvideSecretsResponse> {
  return fetchAPI<ProvideSecretsResponse>(`/bundle/${bundleId}/secrets`, {
    method: 'POST',
    body: JSON.stringify(request),
  });
}

// Pull secrets from a cloud provider
export async function pullSecrets(
  bundleId: string,
  request: PullSecretsRequest
): Promise<PullSecretsResponse> {
  return fetchAPI<PullSecretsResponse>(`/bundle/${bundleId}/secrets/pull`, {
    method: 'POST',
    body: JSON.stringify(request),
  });
}

// Import bundle to target
export async function importBundle(request: ImportBundleRequest): Promise<ImportBundleResponse> {
  return fetchAPI<ImportBundleResponse>('/bundle/import', {
    method: 'POST',
    body: JSON.stringify(request),
  });
}

// Delete a bundle
export async function deleteBundle(bundleId: string): Promise<void> {
  await fetchAPI<void>(`/bundle/${bundleId}`, {
    method: 'DELETE',
  });
}

// Get bundle compose file content
export interface BundleComposeResponse {
  content: string;
}

export async function getBundleCompose(bundleId: string): Promise<BundleComposeResponse> {
  return fetchAPI<BundleComposeResponse>(`/bundle/${bundleId}/compose`, {
    method: 'GET',
  });
}

// List all bundles
export interface BundleSummary {
  bundle_id: string;
  name: string;
  provider: string;
  resource_count: number;
  created_at: string;
  size: number;
}

export async function listBundles(): Promise<BundleSummary[]> {
  return fetchAPI<BundleSummary[]>('/bundle', {
    method: 'GET',
  });
}

// Streaming export with progress
export interface ExportProgressEvent {
  type: 'progress' | 'complete' | 'error';
  step: string;
  message: string;
  progress: number;
}

export async function exportBundleWithProgress(
  request: CreateBundleRequest,
  onProgress: (event: ExportProgressEvent) => void
): Promise<CreateBundleResponse> {
  const response = await fetch(`${API_BASE}/bundle/export/stream`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify(request),
  });

  if (!response.ok) {
    throw new Error('Export failed');
  }

  const reader = response.body?.getReader();
  if (!reader) {
    throw new Error('Streaming not supported');
  }

  const decoder = new TextDecoder();
  let buffer = '';
  let result: CreateBundleResponse | null = null;

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;

    buffer += decoder.decode(value, { stream: true });

    const lines = buffer.split('\n');
    buffer = lines.pop() || '';

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
            onProgress(parsed as ExportProgressEvent);
          } else if (eventType === 'error') {
            throw new Error(parsed.message || 'Export failed');
          } else if (eventType === 'complete') {
            result = parsed as CreateBundleResponse;
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
    throw new Error('No result received from export');
  }

  return result;
}
