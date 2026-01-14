import { fetchAPI } from './api';

export interface Secret {
  name: string;
  value?: string;
  metadata: SecretMetadata;
  labels?: Record<string, string>;
}

export interface SecretMetadata {
  name: string;
  description?: string;
  created_at: string;
  updated_at: string;
  created_by?: string;
  version: number;
  labels?: Record<string, string>;
}

export interface SecretVersion {
  version: number;
  value?: string;
  created_at: string;
  created_by?: string;
}

export interface CreateSecretRequest {
  name: string;
  value: string;
  description?: string;
  labels?: Record<string, string>;
}

export interface SecretsResponse {
  secrets: SecretMetadata[];
  count: number;
}

export interface VersionsResponse {
  versions: SecretVersion[];
  count: number;
}

export async function listSecrets(stackId: string = 'default'): Promise<SecretsResponse> {
  return fetchAPI<SecretsResponse>(`/stacks/${stackId}/secrets`);
}

export async function getSecret(stackId: string, name: string): Promise<Secret> {
  return fetchAPI<Secret>(`/stacks/${stackId}/secrets/${encodeURIComponent(name)}`);
}

export async function createSecret(
  stackId: string,
  request: CreateSecretRequest
): Promise<SecretMetadata> {
  return fetchAPI<SecretMetadata>(`/stacks/${stackId}/secrets`, {
    method: 'POST',
    body: JSON.stringify(request),
  });
}

export async function updateSecret(
  stackId: string,
  name: string,
  value: string
): Promise<SecretMetadata> {
  return fetchAPI<SecretMetadata>(
    `/stacks/${stackId}/secrets/${encodeURIComponent(name)}`,
    {
      method: 'PUT',
      body: JSON.stringify({ value }),
    }
  );
}

export async function deleteSecret(
  stackId: string,
  name: string
): Promise<{ status: string; name: string }> {
  return fetchAPI<{ status: string; name: string }>(
    `/stacks/${stackId}/secrets/${encodeURIComponent(name)}`,
    { method: 'DELETE' }
  );
}

export async function getSecretMetadata(
  stackId: string,
  name: string
): Promise<SecretMetadata> {
  return fetchAPI<SecretMetadata>(
    `/stacks/${stackId}/secrets/${encodeURIComponent(name)}/metadata`
  );
}

export async function listSecretVersions(
  stackId: string,
  name: string
): Promise<VersionsResponse> {
  return fetchAPI<VersionsResponse>(
    `/stacks/${stackId}/secrets/${encodeURIComponent(name)}/versions`
  );
}

export function formatSecretDate(dateStr: string): string {
  const date = new Date(dateStr);
  return date.toLocaleString();
}

export function timeSince(dateStr: string): string {
  const date = new Date(dateStr);
  const seconds = Math.floor((Date.now() - date.getTime()) / 1000);

  if (seconds < 60) return 'just now';
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
  if (seconds < 2592000) return `${Math.floor(seconds / 86400)}d ago`;
  return date.toLocaleDateString();
}
