// Policy types for the frontend

export type PolicyType = 'iam' | 'resource' | 'network';
export type Provider = 'aws' | 'gcp' | 'azure';
export type Effect = 'Allow' | 'Deny';

export interface Principal {
  type: string;
  id: string;
}

export interface Condition {
  operator: string;
  key: string;
  values: string[];
}

export interface Statement {
  sid?: string;
  effect: Effect;
  principals?: Principal[];
  actions: string[];
  not_actions?: string[];
  resources: string[];
  not_resources?: string[];
  conditions?: Condition[];
}

export interface NetworkRule {
  direction: 'ingress' | 'egress';
  protocol: string;
  from_port: number;
  to_port: number;
  cidr_blocks?: string[];
  security_groups?: string[];
  description?: string;
  priority?: number;
  action?: string;
}

export interface NormalizedPolicy {
  version?: string;
  statements: Statement[];
  network_rules?: NetworkRule[];
}

export interface KeycloakRole {
  name: string;
  description?: string;
  composite?: boolean;
  composite_roles?: string[];
  attributes?: Record<string, string[]>;
  source_actions?: string[];
}

export interface KeycloakClient {
  client_id: string;
  name?: string;
  service_accounts_enabled: boolean;
  authorization_enabled: boolean;
  standard_flow_enabled: boolean;
  direct_access_grants_enabled: boolean;
}

export interface KeycloakPolicy {
  name: string;
  type: string;
  logic: string;
  decision_strategy?: string;
  roles?: string[];
  scopes?: string[];
  resources?: string[];
}

export interface KeycloakMapping {
  realm: string;
  roles: KeycloakRole[];
  clients?: KeycloakClient[];
  policies?: KeycloakPolicy[];
  mapping_confidence: number;
  manual_review_notes?: string[];
  unmapped_actions?: string[];
}

export interface Policy {
  id: string;
  name: string;
  type: PolicyType;
  provider: Provider;
  resource_id: string;
  resource_type: string;
  resource_name: string;
  original_document: unknown;
  original_format?: string;
  normalized_policy?: NormalizedPolicy;
  keycloak_mapping?: KeycloakMapping;
  warnings?: string[];
  created_at: string;
  updated_at: string;
}

export interface PolicySummary {
  total_count: number;
  by_type: Record<PolicyType, number>;
  by_provider: Record<Provider, number>;
  with_warnings: number;
  high_confidence_count: number;
  low_confidence_count: number;
  unmappable_count: number;
}

export interface PolicyCollection {
  policies: Policy[];
  summary: PolicySummary;
}

export interface PolicyFilter {
  types?: PolicyType[];
  providers?: Provider[];
  resource_type?: string;
  search?: string;
  has_warnings?: boolean;
}

export interface ValidationError {
  field: string;
  message: string;
  severe: boolean;
}

export interface ValidationResult {
  valid: boolean;
  errors?: ValidationError[];
}

// Action categories for the policy editor
export interface ActionCategory {
  name: string;
  actions: string[];
}

export const PREDEFINED_ACTIONS: ActionCategory[] = [
  {
    name: 'Storage',
    actions: ['read', 'write', 'delete', 'list'],
  },
  {
    name: 'Compute',
    actions: ['invoke', 'manage', 'deploy', 'scale'],
  },
  {
    name: 'Database',
    actions: ['read', 'write', 'admin', 'backup'],
  },
  {
    name: 'Messaging',
    actions: ['send', 'receive', 'manage', 'subscribe'],
  },
  {
    name: 'Security',
    actions: ['encrypt', 'decrypt', 'manage-keys', 'audit'],
  },
];

// Badge class mappings
export const policyTypeBadgeClasses: Record<PolicyType, string> = {
  iam: 'badge-info',
  resource: 'badge-success',
  network: 'badge-warning',
};

export const providerBadgeClasses: Record<Provider, string> = {
  aws: 'badge-aws',
  gcp: 'badge-gcp',
  azure: 'badge-azure',
};

// Helper functions
export function getConfidenceColor(confidence: number): string {
  if (confidence >= 0.8) return 'text-green-600';
  if (confidence >= 0.5) return 'text-yellow-600';
  return 'text-red-600';
}

export function getConfidenceLabel(confidence: number): string {
  if (confidence >= 0.8) return 'High';
  if (confidence >= 0.5) return 'Medium';
  return 'Low';
}

export function formatPolicyType(type: PolicyType): string {
  switch (type) {
    case 'iam':
      return 'IAM';
    case 'resource':
      return 'Resource';
    case 'network':
      return 'Network';
  }
}

export function formatProvider(provider: Provider): string {
  switch (provider) {
    case 'aws':
      return 'AWS';
    case 'gcp':
      return 'GCP';
    case 'azure':
      return 'Azure';
  }
}
