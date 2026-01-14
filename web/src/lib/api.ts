import { API_BASE } from './config';

export interface APIErrorResponse {
  message: string;
  code?: string;
  details?: Record<string, unknown>;
}

export class APIError extends Error {
  status: number;
  code?: string;

  constructor(message: string, status: number, code?: string) {
    super(message);
    this.name = 'APIError';
    this.status = status;
    this.code = code;
  }
}

export async function fetchAPI<T>(
  endpoint: string,
  options?: RequestInit
): Promise<T> {
  const response = await fetch(`${API_BASE}${endpoint}`, {
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
      ...options?.headers,
    },
    ...options,
  });

  if (!response.ok) {
    const errorData = await response.json().catch(() => ({
      message: `API error: ${response.status}`,
    })) as APIErrorResponse;
    throw new APIError(errorData.message, response.status, errorData.code);
  }

  return response.json();
}

export interface DependencyStatus {
  status: 'healthy' | 'degraded' | 'unhealthy';
  latency?: string;
  error?: string;
}

export interface DetailedHealthResponse {
  status: 'healthy' | 'degraded' | 'unhealthy';
  version?: string;
  uptime?: string;
  started_at?: string;
  dependencies?: Record<string, DependencyStatus>;
  system?: {
    go_version: string;
    num_goroutine: number;
    num_cpu: number;
  };
}

export const api = {
  health: () => fetchAPI<{ status: string }>('/health'),
  healthDetailed: () => fetchAPI<DetailedHealthResponse>('/health/detailed'),
  version: () => fetchAPI<{ status: string; version: string }>('/'),
};
