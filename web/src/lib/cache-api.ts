import { fetchAPI } from './api';

export type KeyType = 'string' | 'list' | 'set' | 'zset' | 'hash' | 'stream' | 'none';

export interface CacheKey {
  key: string;
  type: KeyType;
  ttl: number;
  size: number;
  value?: string;
}

export interface KeyInfo {
  key: string;
  type: KeyType;
  ttl: number;
  memory_usage: number;
  encoding: string;
  length: number;
}

export interface CacheStats {
  keys_count: number;
  memory_used: number;
  memory_peak: number;
  memory_human: string;
  connected_clients: number;
  hit_rate: number;
  hits: number;
  misses: number;
  uptime_seconds: number;
  version: string;
}

export interface ScanResult {
  keys: CacheKey[];
  cursor: number;
  has_more: boolean;
}

export interface BulkDeleteResult {
  status: string;
  pattern: string;
  deleted: number;
}

export async function listKeys(
  stackId: string,
  pattern = '*',
  limit = 100,
  cursor = 0
): Promise<ScanResult> {
  const params = new URLSearchParams({
    pattern,
    limit: String(limit),
    cursor: String(cursor),
  });
  return fetchAPI<ScanResult>(`/stacks/${stackId}/cache/keys?${params}`);
}

export async function getKey(stackId: string, key: string): Promise<CacheKey> {
  const encodedKey = encodeURIComponent(key);
  return fetchAPI<CacheKey>(`/stacks/${stackId}/cache/keys/${encodedKey}`);
}

export async function setKey(
  stackId: string,
  key: string,
  value: string,
  ttl?: number
): Promise<{ status: string; key: string }> {
  const encodedKey = encodeURIComponent(key);
  return fetchAPI<{ status: string; key: string }>(
    `/stacks/${stackId}/cache/keys/${encodedKey}`,
    {
      method: 'PUT',
      body: JSON.stringify({ value, ttl: ttl ?? 0 }),
    }
  );
}

export async function deleteKey(
  stackId: string,
  key: string
): Promise<{ status: string; key: string }> {
  const encodedKey = encodeURIComponent(key);
  return fetchAPI<{ status: string; key: string }>(
    `/stacks/${stackId}/cache/keys/${encodedKey}`,
    { method: 'DELETE' }
  );
}

export async function bulkDeleteKeys(
  stackId: string,
  pattern: string
): Promise<BulkDeleteResult> {
  const params = new URLSearchParams({ pattern });
  return fetchAPI<BulkDeleteResult>(
    `/stacks/${stackId}/cache/keys?${params}`,
    { method: 'DELETE' }
  );
}

export async function getCacheStats(stackId: string): Promise<CacheStats> {
  return fetchAPI<CacheStats>(`/stacks/${stackId}/cache/stats`);
}

export async function getKeyInfo(stackId: string, key: string): Promise<KeyInfo> {
  const encodedKey = encodeURIComponent(key);
  return fetchAPI<KeyInfo>(`/stacks/${stackId}/cache/keys/${encodedKey}/info`);
}

export function formatTTL(ttl: number): string {
  if (ttl === -1) return 'No expiry';
  if (ttl === -2) return 'Key not found';
  if (ttl < 60) return `${ttl}s`;
  if (ttl < 3600) return `${Math.floor(ttl / 60)}m ${ttl % 60}s`;
  if (ttl < 86400) {
    const hours = Math.floor(ttl / 3600);
    const mins = Math.floor((ttl % 3600) / 60);
    return `${hours}h ${mins}m`;
  }
  const days = Math.floor(ttl / 86400);
  const hours = Math.floor((ttl % 86400) / 3600);
  return `${days}d ${hours}h`;
}

export function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(2))} ${sizes[i]}`;
}
