import { fetchAPI } from './api';

export type RunbookStepType =
  | 'input'
  | 'command'
  | 'api_call'
  | 'dns_check'
  | 'health_check'
  | 'data_verify'
  | 'approval'
  | 'rollback';

export type RunbookStepStatus =
  | 'pending'
  | 'running'
  | 'passed'
  | 'failed'
  | 'skipped'
  | 'blocked';

export interface RunbookStepResult {
  status: RunbookStepStatus;
  output?: string;
  error?: string;
  started_at?: string;
  ended_at?: string;
}

export interface RunbookStep {
  id: string;
  name: string;
  description?: string;
  group?: string;
  type: RunbookStepType;
  status: RunbookStepStatus;
  optional?: boolean;
  executor?: string;
  success_condition?: string;
  metadata?: Record<string, string>;
  result?: RunbookStepResult;
}

export interface Runbook {
  id: string;
  name: string;
  steps: RunbookStep[];
  created_at?: string;
  updated_at?: string;
}

export function getRunbook(id: string): Promise<Runbook> {
  return fetchAPI<Runbook>(`/runbooks/${id}`);
}

export function runRunbookStep(id: string, stepId: string): Promise<RunbookStepResult> {
  return fetchAPI<RunbookStepResult>(`/runbooks/${id}/steps/${stepId}/run`, {
    method: 'POST',
  });
}

export function runRunbook(id: string): Promise<Runbook> {
  return fetchAPI<Runbook>(`/runbooks/${id}/run`, {
    method: 'POST',
  });
}

export function rollbackRunbook(id: string): Promise<Runbook> {
  return fetchAPI<Runbook>(`/runbooks/${id}/rollback`, {
    method: 'POST',
  });
}
