import { API_BASE } from './config';

// Provider identifiers
export type Provider = 'hetzner' | 'scaleway' | 'ovh' | 'aws' | 'gcp' | 'azure';

// EU providers supported for deployment
export const SUPPORTED_PROVIDERS: Provider[] = ['hetzner', 'scaleway', 'ovh'];

// Reference providers (not supported for deployment)
export const REFERENCE_PROVIDERS: Provider[] = ['aws', 'gcp', 'azure'];

// Region information
export interface Region {
  id: string;
  name: string;
  location: string;
  available: boolean;
}

// Provider information
export interface ProviderInfo {
  id: Provider;
  display_name: string;
  regions: Region[];
  is_eu: boolean;
  is_supported: boolean;
}

// Instance pricing
export interface InstancePricing {
  type: string;
  vcpus: number;
  memory_gb: number;
  storage_gb: number;
  price_per_month: number;
  price_per_hour: number;
  currency: string;
}

// Cost breakdown
export interface CostBreakdown {
  compute_cost: number;
  storage_cost: number;
  network_cost: number;
  total_monthly: number;
  currency: string;
}

// Provider cost estimate
export interface ProviderCostEstimate {
  provider: Provider;
  display_name: string;
  is_eu: boolean;
  breakdown: CostBreakdown;
  total_monthly: number;
  currency: string;
  savings: number;
  savings_percentage: number;
}

// Compare request
export interface CompareRequest {
  mapping_results: unknown;
  providers: Provider[];
  ha_level: 'none' | 'basic' | 'full';
  estimated_storage_gb: number;
  estimated_egress_gb: number;
}

// Compare response
export interface CompareResponse {
  estimates: ProviderCostEstimate[];
  best_value: Provider;
  current_cost: number;
  currency: string;
}

// List providers response
export interface ListProvidersResponse {
  providers: ProviderInfo[];
}

// Provider regions response
export interface ProviderRegionsResponse {
  regions: Region[];
}

// Provider instances response
export interface ProviderInstancesResponse {
  instances: InstancePricing[];
}

/**
 * Fetch all providers with their info.
 */
export async function listProviders(): Promise<ProviderInfo[]> {
  const response = await fetch(`${API_BASE}/providers`, {
    credentials: 'include',
  });

  if (!response.ok) {
    throw new Error('Failed to fetch providers');
  }

  const data: ListProvidersResponse = await response.json();
  return data.providers;
}

/**
 * Fetch a single provider's details.
 */
export async function getProvider(id: Provider): Promise<ProviderInfo> {
  const response = await fetch(`${API_BASE}/providers/${id}`, {
    credentials: 'include',
  });

  if (!response.ok) {
    if (response.status === 404) {
      throw new Error('Provider not found');
    }
    throw new Error('Failed to fetch provider');
  }

  return response.json();
}

/**
 * Fetch regions for a specific provider.
 */
export async function getProviderRegions(id: Provider): Promise<Region[]> {
  const response = await fetch(`${API_BASE}/providers/${id}/regions`, {
    credentials: 'include',
  });

  if (!response.ok) {
    if (response.status === 404) {
      throw new Error('Provider not found');
    }
    throw new Error('Failed to fetch provider regions');
  }

  const data: ProviderRegionsResponse = await response.json();
  return data.regions;
}

/**
 * Fetch instance types and pricing for a specific provider.
 */
export async function getProviderInstances(id: Provider): Promise<InstancePricing[]> {
  const response = await fetch(`${API_BASE}/providers/${id}/instances`, {
    credentials: 'include',
  });

  if (!response.ok) {
    if (response.status === 404) {
      throw new Error('Provider not found');
    }
    throw new Error('Failed to fetch provider instances');
  }

  const data: ProviderInstancesResponse = await response.json();
  return data.instances;
}

/**
 * Compare costs across multiple providers for the given infrastructure.
 */
export async function compareProviders(request: CompareRequest): Promise<CompareResponse> {
  const response = await fetch(`${API_BASE}/providers/compare`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify(request),
  });

  if (!response.ok) {
    const error = await response.text();
    throw new Error(error || 'Failed to compare providers');
  }

  return response.json();
}

/**
 * Get display name for a provider.
 */
export function getProviderDisplayName(id: Provider): string {
  const names: Record<Provider, string> = {
    hetzner: 'Hetzner Cloud',
    scaleway: 'Scaleway',
    ovh: 'OVHcloud',
    aws: 'Amazon Web Services',
    gcp: 'Google Cloud Platform',
    azure: 'Microsoft Azure',
  };
  return names[id] || id;
}

/**
 * Check if a provider is EU-based.
 */
export function isEUProvider(id: Provider): boolean {
  return SUPPORTED_PROVIDERS.includes(id);
}

/**
 * Format currency value.
 */
export function formatCurrency(value: number, currency: string = 'EUR'): string {
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency,
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(value);
}

/**
 * Format percentage value.
 */
export function formatPercentage(value: number): string {
  const sign = value > 0 ? '+' : '';
  return `${sign}${value.toFixed(0)}%`;
}
