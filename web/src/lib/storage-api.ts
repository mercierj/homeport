import { API_BASE } from './config';

export interface StorageCredentials {
  endpoint: string;
  accessKey: string;
  secretKey: string;
}

export interface Bucket {
  name: string;
  created: string;
}

export interface StorageObject {
  key: string;
  size: number;
  last_modified: string;
  content_type: string;
  is_dir: boolean;
}

// Store credentials securely on the backend (associated with session)
export async function setStorageCredentials(creds: StorageCredentials): Promise<void> {
  const response = await fetch(`${API_BASE}/credentials/storage`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    credentials: 'include',
    body: JSON.stringify(creds),
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || 'Failed to store credentials');
  }
}

// Clear stored credentials from the backend
export async function clearStorageCredentials(): Promise<void> {
  const response = await fetch(`${API_BASE}/credentials/storage`, {
    method: 'DELETE',
    credentials: 'include',
  });
  if (!response.ok) {
    throw new Error('Failed to clear credentials');
  }
}

export async function listBuckets(stackId = 'default'): Promise<{ buckets: Bucket[] }> {
  const response = await fetch(`${API_BASE}/stacks/${stackId}/storage/buckets`, {
    credentials: 'include',
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || 'Failed to list buckets');
  }
  return response.json();
}

export async function createBucket(stackId: string, name: string): Promise<void> {
  const response = await fetch(`${API_BASE}/stacks/${stackId}/storage/buckets`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    credentials: 'include',
    body: JSON.stringify({ name }),
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || 'Failed to create bucket');
  }
}

export async function listObjects(
  stackId: string,
  bucket: string,
  prefix = ''
): Promise<{ objects: StorageObject[] }> {
  const params = prefix ? `?prefix=${encodeURIComponent(prefix)}` : '';
  const response = await fetch(
    `${API_BASE}/stacks/${stackId}/storage/buckets/${bucket}/objects${params}`,
    { credentials: 'include' }
  );
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || 'Failed to list objects');
  }
  return response.json();
}

export async function uploadFile(
  stackId: string,
  bucket: string,
  file: File,
  key?: string
): Promise<void> {
  const formData = new FormData();
  formData.append('file', file);
  if (key) formData.append('key', key);

  const response = await fetch(
    `${API_BASE}/stacks/${stackId}/storage/buckets/${bucket}/upload`,
    {
      method: 'POST',
      credentials: 'include',
      body: formData,
    }
  );
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || 'Failed to upload file');
  }
}

export async function deleteObject(
  stackId: string,
  bucket: string,
  key: string
): Promise<void> {
  const response = await fetch(
    `${API_BASE}/stacks/${stackId}/storage/buckets/${bucket}/objects/${encodeURIComponent(key)}`,
    {
      method: 'DELETE',
      credentials: 'include',
    }
  );
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || 'Failed to delete object');
  }
}

export function getDownloadUrl(stackId: string, bucket: string, key: string): string {
  return `${API_BASE}/stacks/${stackId}/storage/buckets/${bucket}/download/${encodeURIComponent(key)}`;
}
