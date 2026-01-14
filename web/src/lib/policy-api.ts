import { API_BASE } from './config';
import type {
  Policy,
  PolicyCollection,
  PolicyFilter,
  PolicySummary,
  KeycloakMapping,
  ValidationResult,
  NormalizedPolicy,
} from './policy-types';

const BASE_URL = `${API_BASE}/policies`;

// List policies with optional filtering
export async function listPolicies(filter?: PolicyFilter): Promise<PolicyCollection> {
  const params = new URLSearchParams();

  if (filter?.types) {
    filter.types.forEach((t) => params.append('type', t));
  }
  if (filter?.providers) {
    filter.providers.forEach((p) => params.append('provider', p));
  }
  if (filter?.resource_type) {
    params.set('resource_type', filter.resource_type);
  }
  if (filter?.search) {
    params.set('search', filter.search);
  }
  if (filter?.has_warnings !== undefined) {
    params.set('has_warnings', String(filter.has_warnings));
  }

  const url = params.toString() ? `${BASE_URL}?${params}` : BASE_URL;
  const response = await fetch(url);

  if (!response.ok) {
    throw new Error(`Failed to fetch policies: ${response.statusText}`);
  }

  return response.json();
}

// Get a single policy by ID
export async function getPolicy(id: string): Promise<Policy> {
  const response = await fetch(`${BASE_URL}/${id}`);

  if (!response.ok) {
    throw new Error(`Failed to fetch policy: ${response.statusText}`);
  }

  return response.json();
}

// Create a new policy
export async function createPolicy(policy: Partial<Policy>): Promise<Policy> {
  const response = await fetch(BASE_URL, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(policy),
  });

  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.error || 'Failed to create policy');
  }

  return response.json();
}

// Update an existing policy
export async function updatePolicy(
  id: string,
  updates: {
    name?: string;
    original_document?: unknown;
    normalized_policy?: NormalizedPolicy;
    warnings?: string[];
  }
): Promise<Policy> {
  const response = await fetch(`${BASE_URL}/${id}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(updates),
  });

  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.error || 'Failed to update policy');
  }

  return response.json();
}

// Delete a policy
export async function deletePolicy(id: string): Promise<void> {
  const response = await fetch(`${BASE_URL}/${id}`, {
    method: 'DELETE',
  });

  if (!response.ok) {
    throw new Error(`Failed to delete policy: ${response.statusText}`);
  }
}

// Validate a policy
export async function validatePolicy(id: string): Promise<ValidationResult> {
  const response = await fetch(`${BASE_URL}/${id}/validate`, {
    method: 'POST',
  });

  if (!response.ok) {
    throw new Error(`Failed to validate policy: ${response.statusText}`);
  }

  return response.json();
}

// Get Keycloak mapping preview
export async function getKeycloakPreview(id: string): Promise<KeycloakMapping> {
  const response = await fetch(`${BASE_URL}/${id}/keycloak-preview`);

  if (!response.ok) {
    throw new Error(`Failed to get Keycloak preview: ${response.statusText}`);
  }

  return response.json();
}

// Regenerate Keycloak mapping
export async function regenerateKeycloakMapping(id: string): Promise<Policy> {
  const response = await fetch(`${BASE_URL}/${id}/keycloak-regenerate`, {
    method: 'POST',
  });

  if (!response.ok) {
    throw new Error(`Failed to regenerate mapping: ${response.statusText}`);
  }

  return response.json();
}

// Get original document
export async function getOriginalDocument(id: string): Promise<{ document: string; format: string }> {
  const response = await fetch(`${BASE_URL}/${id}/original`);

  if (!response.ok) {
    throw new Error(`Failed to get original document: ${response.statusText}`);
  }

  const contentType = response.headers.get('Content-Type') || 'application/json';
  const text = await response.text();

  let format = 'json';
  if (contentType.includes('yaml')) {
    format = 'yaml';
  } else if (contentType.includes('text/plain')) {
    format = 'hcl';
  }

  return { document: text, format };
}

// Get policy summary
export async function getPolicySummary(): Promise<PolicySummary> {
  const response = await fetch(`${BASE_URL}/summary`);

  if (!response.ok) {
    throw new Error(`Failed to get summary: ${response.statusText}`);
  }

  return response.json();
}

// Import policies
export async function importPolicies(policies: Policy[]): Promise<{ imported: number; message: string }> {
  const response = await fetch(`${BASE_URL}/import`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ policies }),
  });

  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.error || 'Failed to import policies');
  }

  return response.json();
}
