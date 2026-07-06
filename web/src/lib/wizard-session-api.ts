import { fetchAPI } from './api';

export type WizardSessionStep = 'analyze' | 'export' | 'secrets' | 'deploy' | 'sync' | 'cutover' | 'done';

export interface WizardSession {
  id: string;
  current_step: WizardSessionStep;
  completed_steps: WizardSessionStep[];
  source_provider?: string;
  selected_resources?: string[];
  bundle_id?: string;
  secrets_resolved: boolean;
  deployment_id?: string;
  runbook_id?: string;
  sync_plan_id?: string;
  cutover_id?: string;
  metadata?: Record<string, string>;
}

export function createWizardSession(): Promise<WizardSession> {
  return fetchAPI<WizardSession>('/wizard/sessions', { method: 'POST' });
}

export function getWizardSession(id: string): Promise<WizardSession> {
  return fetchAPI<WizardSession>(`/wizard/sessions/${id}`, { method: 'GET' });
}

export function updateWizardSession(id: string, patch: Partial<WizardSession>): Promise<WizardSession> {
  return fetchAPI<WizardSession>(`/wizard/sessions/${id}`, {
    method: 'PATCH',
    body: JSON.stringify(patch),
  });
}
