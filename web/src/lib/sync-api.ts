import { fetchAPI } from './api';
import { API_BASE } from './config';

export interface SyncEndpoint {
  type: string;
  host?: string;
  port?: number;
  database?: string;
  bucket?: string;
  path?: string;
  region?: string;
  username?: string;
  password?: string;
  access_key?: string;
  secret_key?: string;
  ssl?: boolean;
  ssl_mode?: string;
  options?: Record<string, string>;
}

export interface SyncTaskRequest {
  id: string;
  name: string;
  type: 'database' | 'storage' | 'cache';
  strategy?: string;
  source: SyncEndpoint;
  target: SyncEndpoint;
}

export interface StartSyncRequest {
  tasks: SyncTaskRequest[];
}

export interface StartSyncResponse {
  sync_id: string;
}

export interface SyncTaskStatus {
  id: string;
  name: string;
  type: string;
  status: string;
  progress: number;
  bytes_total: number;
  bytes_done: number;
  items_total: number;
  items_done: number;
  error?: string;
}

export interface SyncStatusResponse {
  sync_id: string;
  status: string;
  progress: number;
  tasks: SyncTaskStatus[];
  started_at?: string;
  error?: string;
}

export interface SyncEvent {
  type: string;
  plan_id: string;
  task_id?: string;
  task_name?: string;
  task_type?: string;
  status?: string;
  progress?: number;
  bytes_total?: number;
  bytes_done?: number;
  items_total?: number;
  items_done?: number;
  error?: string;
  message?: string;
}

export interface StrategiesResponse {
  strategies: string[];
}

// Start a new sync operation
export async function startSync(request: StartSyncRequest): Promise<StartSyncResponse> {
  return fetchAPI<StartSyncResponse>('/sync/start', {
    method: 'POST',
    body: JSON.stringify(request),
  });
}

// Get sync status
export async function getSyncStatus(syncId: string): Promise<SyncStatusResponse> {
  return fetchAPI<SyncStatusResponse>(`/sync/${syncId}/status`, {
    method: 'GET',
  });
}

// Get available sync strategies
export async function getSyncStrategies(): Promise<StrategiesResponse> {
  return fetchAPI<StrategiesResponse>('/sync/strategies', {
    method: 'GET',
  });
}

// Pause a running sync
export async function pauseSync(syncId: string): Promise<void> {
  await fetchAPI<void>(`/sync/${syncId}/pause`, {
    method: 'POST',
  });
}

// Resume a paused sync
export async function resumeSync(syncId: string): Promise<void> {
  await fetchAPI<void>(`/sync/${syncId}/resume`, {
    method: 'POST',
  });
}

// Cancel a running sync
export async function cancelSync(syncId: string): Promise<void> {
  await fetchAPI<void>(`/sync/${syncId}/cancel`, {
    method: 'POST',
  });
}

// Subscribe to sync progress via SSE
export function subscribeToSync(
  syncId: string,
  callbacks: {
    onTaskStart?: (event: SyncEvent) => void;
    onProgress?: (event: SyncEvent) => void;
    onTaskComplete?: (event: SyncEvent) => void;
    onError?: (event: SyncEvent) => void;
    onComplete?: (event: SyncEvent) => void;
    onClose?: () => void;
  }
): () => void {
  const eventSource = new EventSource(`${API_BASE}/sync/${syncId}/stream`, {
    withCredentials: true,
  });

  eventSource.addEventListener('task_start', (e: MessageEvent) => {
    const data = JSON.parse(e.data) as SyncEvent;
    callbacks.onTaskStart?.(data);
  });

  eventSource.addEventListener('progress', (e: MessageEvent) => {
    const data = JSON.parse(e.data) as SyncEvent;
    callbacks.onProgress?.(data);
  });

  eventSource.addEventListener('task_complete', (e: MessageEvent) => {
    const data = JSON.parse(e.data) as SyncEvent;
    callbacks.onTaskComplete?.(data);
  });

  eventSource.addEventListener('error', (e: MessageEvent) => {
    if (e.data) {
      const data = JSON.parse(e.data) as SyncEvent;
      callbacks.onError?.(data);
    }
  });

  eventSource.addEventListener('complete', (e: MessageEvent) => {
    const data = JSON.parse(e.data) as SyncEvent;
    callbacks.onComplete?.(data);
    eventSource.close();
  });

  eventSource.onerror = () => {
    callbacks.onClose?.();
    eventSource.close();
  };

  return () => {
    eventSource.close();
  };
}
