import { fetchAPI } from './api';
import { API_BASE } from './config';

export interface HealthCheckRequest {
  id: string;
  name: string;
  type: 'http' | 'tcp';
  endpoint: string;
  timeout_seconds?: number;
  expect_code?: number;
  expect_body?: string;
}

export interface DNSChangeRequest {
  id: string;
  domain: string;
  record_type: 'A' | 'CNAME' | 'AAAA';
  old_value: string;
  new_value: string;
  ttl?: number;
}

export interface CreateCutoverRequest {
  bundle_id: string;
  name?: string;
  pre_checks: HealthCheckRequest[];
  dns_changes: DNSChangeRequest[];
  post_checks: HealthCheckRequest[];
  dry_run: boolean;
  dns_provider?: string;
}

export interface CreateCutoverResponse {
  cutover_id: string;
}

export interface ValidatePlanResponse {
  valid: boolean;
  errors: string[];
  warnings: string[];
}

export interface CutoverStepStatus {
  order: number;
  type: string;
  description: string;
  status: string;
  error?: string;
}

export interface CutoverStatusResponse {
  cutover_id: string;
  status: string;
  progress: number;
  steps: CutoverStepStatus[];
  started_at?: string;
  logs: string[];
  error?: string;
}

export interface CutoverEvent {
  type: string;
  plan_id: string;
  step_index?: number;
  step_type?: string;
  description?: string;
  status?: string;
  error?: string;
  message?: string;
}

// Validate a cutover plan
export async function validateCutoverPlan(
  request: CreateCutoverRequest
): Promise<ValidatePlanResponse> {
  return fetchAPI<ValidatePlanResponse>('/cutover/validate', {
    method: 'POST',
    body: JSON.stringify(request),
  });
}

// Start a new cutover operation
export async function startCutover(
  request: CreateCutoverRequest
): Promise<CreateCutoverResponse> {
  return fetchAPI<CreateCutoverResponse>('/cutover/start', {
    method: 'POST',
    body: JSON.stringify(request),
  });
}

// Get cutover status
export async function getCutoverStatus(cutoverId: string): Promise<CutoverStatusResponse> {
  return fetchAPI<CutoverStatusResponse>(`/cutover/${cutoverId}/status`, {
    method: 'GET',
  });
}

// Cancel a running cutover
export async function cancelCutover(cutoverId: string): Promise<void> {
  await fetchAPI<void>(`/cutover/${cutoverId}/cancel`, {
    method: 'POST',
  });
}

// Trigger manual rollback
export async function rollbackCutover(cutoverId: string): Promise<void> {
  await fetchAPI<void>(`/cutover/${cutoverId}/rollback`, {
    method: 'POST',
  });
}

// Subscribe to cutover progress via SSE
export function subscribeToCutover(
  cutoverId: string,
  callbacks: {
    onStepStart?: (event: CutoverEvent) => void;
    onStepComplete?: (event: CutoverEvent) => void;
    onStepFailed?: (event: CutoverEvent) => void;
    onRollback?: (event: CutoverEvent) => void;
    onComplete?: (event: CutoverEvent) => void;
    onError?: (event: CutoverEvent) => void;
    onClose?: () => void;
  }
): () => void {
  const eventSource = new EventSource(`${API_BASE}/cutover/${cutoverId}/stream`, {
    withCredentials: true,
  });

  eventSource.addEventListener('step_start', (e: MessageEvent) => {
    const data = JSON.parse(e.data) as CutoverEvent;
    callbacks.onStepStart?.(data);
  });

  eventSource.addEventListener('step_complete', (e: MessageEvent) => {
    const data = JSON.parse(e.data) as CutoverEvent;
    callbacks.onStepComplete?.(data);
  });

  eventSource.addEventListener('step_failed', (e: MessageEvent) => {
    const data = JSON.parse(e.data) as CutoverEvent;
    callbacks.onStepFailed?.(data);
  });

  eventSource.addEventListener('rollback', (e: MessageEvent) => {
    const data = JSON.parse(e.data) as CutoverEvent;
    callbacks.onRollback?.(data);
  });

  eventSource.addEventListener('complete', (e: MessageEvent) => {
    const data = JSON.parse(e.data) as CutoverEvent;
    callbacks.onComplete?.(data);
    eventSource.close();
  });

  eventSource.addEventListener('error', (e: MessageEvent) => {
    if (e.data) {
      const data = JSON.parse(e.data) as CutoverEvent;
      callbacks.onError?.(data);
    }
  });

  eventSource.onerror = () => {
    callbacks.onClose?.();
    eventSource.close();
  };

  return () => {
    eventSource.close();
  };
}
