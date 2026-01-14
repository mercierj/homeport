import type { Node, Edge } from '@xyflow/react';
import type { Resource } from './migrate-api';

export type ResourceNodeData = {
  resource: Resource;
  selected: boolean;
  hasWarning: boolean;
  [key: string]: unknown;
};

export type ResourceNode = Node<ResourceNodeData, 'resource'>;
export type DependencyEdge = Edge;

// Hex colors for diagrams/charts (match CSS variables in index.css)
export const categoryColors: Record<string, string> = {
  compute: '#3b82f6',    // blue-500 (resource-icon-compute)
  storage: '#22c55e',    // green-500 (resource-icon-storage)
  database: '#a855f7',   // purple-500 (resource-icon-database)
  networking: '#f97316', // orange-500 (resource-icon-network)
  security: '#ef4444',   // red-500 (resource-icon-security)
  messaging: '#ec4899',  // pink-500 (resource-icon-messaging)
};

// Design system badge classes - use these for category badges
// See DESIGN_CHEATSHEET.md for full reference
export const categoryBadgeClasses: Record<string, string> = {
  compute: 'badge bg-blue-500 text-white',
  storage: 'badge bg-green-500 text-white',
  database: 'badge bg-purple-500 text-white',
  networking: 'badge bg-orange-500 text-white',
  security: 'badge bg-red-500 text-white',
  messaging: 'badge bg-pink-500 text-white',
};

// Resource icon wrapper classes (from design system)
export const categoryIconClasses: Record<string, string> = {
  compute: 'resource-icon-compute',
  storage: 'resource-icon-storage',
  database: 'resource-icon-database',
  networking: 'resource-icon-network',
  security: 'resource-icon-security',
  messaging: 'resource-icon-messaging',
};

// Status badge classes (from design system)
export const statusBadgeClasses: Record<string, string> = {
  running: 'badge-success',
  healthy: 'badge-success',
  active: 'badge-success',
  stopped: 'badge-error',
  exited: 'badge-error',
  failed: 'badge-error',
  error: 'badge-error',
  paused: 'badge-warning',
  degraded: 'badge-warning',
  pending: 'badge-info',
  restarting: 'badge-info',
};

// Cloud provider badge classes (from design system)
export const providerBadgeClasses: Record<string, string> = {
  aws: 'badge-aws',
  gcp: 'badge-gcp',
  azure: 'badge-azure',
  'self-hosted': 'badge-freedom',
  docker: 'badge-freedom',
};

export const categoryLabels: Record<string, string> = {
  compute: 'Compute',
  storage: 'Storage',
  database: 'Database',
  networking: 'Networking',
  security: 'Security & Identity',
  messaging: 'Messaging & Events',
};

export const categoryOrder = ['networking', 'security', 'compute', 'storage', 'database', 'messaging'];
