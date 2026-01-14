import { fetchAPI } from './api';

// Types
export type BackupStatus = 'pending' | 'running' | 'completed' | 'failed';

export interface Backup {
  id: string;
  name: string;
  description?: string;
  stack_id: string;
  volumes: string[];
  size: number;
  status: BackupStatus;
  error?: string;
  file_path: string;
  created_at: string;
  completed_at?: string;
}

export interface BackupsResponse {
  backups: Backup[];
  count: number;
}

export interface VolumeInfo {
  name: string;
  driver: string;
  mountpoint: string;
  labels: Record<string, string>;
  stack_id?: string;
  created_at: string;
}

export interface VolumesResponse {
  volumes: VolumeInfo[];
  count: number;
}

export interface CreateBackupRequest {
  name: string;
  description?: string;
  stack_id: string;
  volumes: string[];
}

export interface RestoreBackupRequest {
  target_stack_id?: string;
  volumes?: string[];
}

// API Functions

export async function listBackups(stackId?: string): Promise<BackupsResponse> {
  const params = stackId ? `?stack_id=${encodeURIComponent(stackId)}` : '';
  return fetchAPI<BackupsResponse>(`/backups${params}`);
}

export async function createBackup(request: CreateBackupRequest): Promise<Backup> {
  return fetchAPI<Backup>('/backups', {
    method: 'POST',
    body: JSON.stringify(request),
  });
}

export async function getBackup(id: string): Promise<Backup> {
  return fetchAPI<Backup>(`/backups/${encodeURIComponent(id)}`);
}

export async function deleteBackup(id: string): Promise<{ status: string; id: string }> {
  return fetchAPI<{ status: string; id: string }>(`/backups/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  });
}

export async function restoreBackup(
  id: string,
  request: RestoreBackupRequest = {}
): Promise<{ status: string; backup_id: string }> {
  return fetchAPI<{ status: string; backup_id: string }>(
    `/backups/${encodeURIComponent(id)}/restore`,
    {
      method: 'POST',
      body: JSON.stringify(request),
    }
  );
}

export function getBackupDownloadUrl(id: string): string {
  return `/api/v1/backups/${encodeURIComponent(id)}/download`;
}

export async function listVolumes(stackId?: string): Promise<VolumesResponse> {
  const params = stackId ? `?stack_id=${encodeURIComponent(stackId)}` : '';
  return fetchAPI<VolumesResponse>(`/volumes${params}`);
}

// Utility functions

export function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(2))} ${sizes[i]}`;
}

export function getStatusColor(status: BackupStatus): string {
  switch (status) {
    case 'pending':
      return 'text-yellow-600 bg-yellow-100';
    case 'running':
      return 'text-blue-600 bg-blue-100';
    case 'completed':
      return 'text-green-600 bg-green-100';
    case 'failed':
      return 'text-red-600 bg-red-100';
    default:
      return 'text-gray-600 bg-gray-100';
  }
}

export function getStatusLabel(status: BackupStatus): string {
  switch (status) {
    case 'pending':
      return 'Pending';
    case 'running':
      return 'Running';
    case 'completed':
      return 'Completed';
    case 'failed':
      return 'Failed';
    default:
      return status;
  }
}
