import { fetchAPI } from './api';

// Runtime types for serverless functions
export type FunctionRuntime =
  | 'nodejs20'
  | 'nodejs18'
  | 'python3.11'
  | 'python3.10'
  | 'go1.21'
  | 'go1.20';

// Function status
export type FunctionStatus = 'active' | 'inactive' | 'deploying' | 'error';

// Function configuration
export interface FunctionConfig {
  name: string;
  runtime: FunctionRuntime;
  handler: string;
  memory: number; // MB
  timeout: number; // seconds
  environment?: Record<string, string>;
  code?: string;
}

// Full function info returned from API
export interface FunctionInfo {
  id: string;
  name: string;
  runtime: FunctionRuntime;
  handler: string;
  memory: number;
  timeout: number;
  environment: Record<string, string>;
  status: FunctionStatus;
  invocation_count: number;
  last_invoked_at?: string;
  created_at: string;
  updated_at: string;
  code?: string;
}

// Result from invoking a function
export interface InvocationResult {
  request_id: string;
  status_code: number;
  body: unknown;
  duration_ms: number;
  logs?: string[];
  error?: string;
}

// Function log entry
export interface FunctionLog {
  timestamp: string;
  request_id: string;
  level: 'info' | 'warn' | 'error' | 'debug';
  message: string;
}

// Response types
export interface FunctionsResponse {
  functions: FunctionInfo[];
  count: number;
}

export interface FunctionLogsResponse {
  logs: FunctionLog[];
  count: number;
}

// List all functions
export async function listFunctions(): Promise<FunctionsResponse> {
  return fetchAPI<FunctionsResponse>('/functions');
}

// Get a specific function by ID
export async function getFunction(id: string): Promise<FunctionInfo> {
  return fetchAPI<FunctionInfo>(`/functions/${encodeURIComponent(id)}`);
}

// Create a new function
export async function createFunction(config: FunctionConfig): Promise<FunctionInfo> {
  return fetchAPI<FunctionInfo>('/functions', {
    method: 'POST',
    body: JSON.stringify(config),
  });
}

// Update an existing function
export async function updateFunction(
  id: string,
  config: Partial<FunctionConfig>
): Promise<FunctionInfo> {
  return fetchAPI<FunctionInfo>(`/functions/${encodeURIComponent(id)}`, {
    method: 'PUT',
    body: JSON.stringify(config),
  });
}

// Delete a function
export async function deleteFunction(id: string): Promise<{ status: string; id: string }> {
  return fetchAPI<{ status: string; id: string }>(`/functions/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  });
}

// Invoke a function
export async function invokeFunction(
  id: string,
  payload?: unknown
): Promise<InvocationResult> {
  return fetchAPI<InvocationResult>(`/functions/${encodeURIComponent(id)}/invoke`, {
    method: 'POST',
    body: payload ? JSON.stringify(payload) : undefined,
  });
}

// Get function logs
export async function getFunctionLogs(
  id: string,
  since?: string
): Promise<FunctionLogsResponse> {
  const params = since ? `?since=${encodeURIComponent(since)}` : '';
  return fetchAPI<FunctionLogsResponse>(`/functions/${encodeURIComponent(id)}/logs${params}`);
}

// Helper function to get runtime icon/label
export function getRuntimeIcon(runtime: FunctionRuntime): { icon: string; label: string } {
  switch (runtime) {
    case 'nodejs20':
      return { icon: 'N', label: 'Node.js 20' };
    case 'nodejs18':
      return { icon: 'N', label: 'Node.js 18' };
    case 'python3.11':
      return { icon: 'P', label: 'Python 3.11' };
    case 'python3.10':
      return { icon: 'P', label: 'Python 3.10' };
    case 'go1.21':
      return { icon: 'G', label: 'Go 1.21' };
    case 'go1.20':
      return { icon: 'G', label: 'Go 1.20' };
    default:
      return { icon: '?', label: runtime };
  }
}

// Helper function to get status badge class
export function getStatusBadgeClass(status: FunctionStatus): string {
  switch (status) {
    case 'active':
      return 'bg-green-100 text-green-800';
    case 'inactive':
      return 'bg-gray-100 text-gray-800';
    case 'deploying':
      return 'bg-blue-100 text-blue-800';
    case 'error':
      return 'bg-red-100 text-red-800';
    default:
      return 'bg-gray-100 text-gray-800';
  }
}

// Helper function to format duration
export function formatDuration(ms: number): string {
  if (ms < 1) {
    return '<1ms';
  }
  if (ms < 1000) {
    return `${Math.round(ms)}ms`;
  }
  if (ms < 60000) {
    return `${(ms / 1000).toFixed(2)}s`;
  }
  const minutes = Math.floor(ms / 60000);
  const seconds = ((ms % 60000) / 1000).toFixed(0);
  return `${minutes}m ${seconds}s`;
}

// Available runtimes for dropdowns
export const AVAILABLE_RUNTIMES: { value: FunctionRuntime; label: string }[] = [
  { value: 'nodejs20', label: 'Node.js 20' },
  { value: 'nodejs18', label: 'Node.js 18' },
  { value: 'python3.11', label: 'Python 3.11' },
  { value: 'python3.10', label: 'Python 3.10' },
  { value: 'go1.21', label: 'Go 1.21' },
  { value: 'go1.20', label: 'Go 1.20' },
];

// Default memory options (in MB)
export const MEMORY_OPTIONS = [128, 256, 512, 1024, 2048, 4096];

// Default timeout options (in seconds)
export const TIMEOUT_OPTIONS = [3, 10, 30, 60, 120, 300, 600];
