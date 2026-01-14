import { fetchAPI } from './api';

// Types for container metrics
export interface ContainerMetrics {
  containerId: string;
  containerName: string;
  cpu: CpuMetrics;
  memory: MemoryMetrics;
  network: NetworkMetrics;
  disk: DiskMetrics;
  timestamp: string;
}

export interface CpuMetrics {
  usagePercent: number;
  systemUsagePercent: number;
  throttledPeriods: number;
  throttledTime: number;
}

export interface MemoryMetrics {
  usage: number;
  limit: number;
  usagePercent: number;
  cache: number;
  rss: number;
}

export interface NetworkMetrics {
  rxBytes: number;
  txBytes: number;
  rxPackets: number;
  txPackets: number;
  rxErrors: number;
  txErrors: number;
  rxDropped: number;
  txDropped: number;
}

export interface DiskMetrics {
  readBytes: number;
  writeBytes: number;
  readOps: number;
  writeOps: number;
}

// Types for system/host metrics
export interface SystemMetrics {
  cpu: SystemCpuMetrics;
  memory: SystemMemoryMetrics;
  disk: SystemDiskMetrics;
  network: SystemNetworkMetrics;
  load: LoadMetrics;
  uptime: number;
  timestamp: string;
}

export interface SystemCpuMetrics {
  usagePercent: number;
  userPercent: number;
  systemPercent: number;
  idlePercent: number;
  cores: number;
}

export interface SystemMemoryMetrics {
  total: number;
  used: number;
  free: number;
  available: number;
  usagePercent: number;
  swapTotal: number;
  swapUsed: number;
  swapFree: number;
}

export interface SystemDiskMetrics {
  total: number;
  used: number;
  free: number;
  usagePercent: number;
  inodesFree: number;
  inodesTotal: number;
}

export interface SystemNetworkMetrics {
  interfaces: NetworkInterface[];
  totalRxBytes: number;
  totalTxBytes: number;
}

export interface NetworkInterface {
  name: string;
  rxBytes: number;
  txBytes: number;
  rxPackets: number;
  txPackets: number;
}

export interface LoadMetrics {
  load1: number;
  load5: number;
  load15: number;
}

// Types for historical metrics
export interface MetricsHistoryPoint {
  timestamp: string;
  value: number;
}

export interface ContainerMetricsHistory {
  containerId: string;
  containerName: string;
  cpuUsage: MetricsHistoryPoint[];
  memoryUsage: MetricsHistoryPoint[];
  networkRx: MetricsHistoryPoint[];
  networkTx: MetricsHistoryPoint[];
  diskRead: MetricsHistoryPoint[];
  diskWrite: MetricsHistoryPoint[];
}

export interface SystemMetricsHistory {
  cpuUsage: MetricsHistoryPoint[];
  memoryUsage: MetricsHistoryPoint[];
  diskUsage: MetricsHistoryPoint[];
  networkRx: MetricsHistoryPoint[];
  networkTx: MetricsHistoryPoint[];
  load1: MetricsHistoryPoint[];
}

// Types for metrics summary
export interface MetricsSummary {
  containers: ContainerSummary[];
  system: SystemSummary;
  alerts: MetricAlert[];
}

export interface ContainerSummary {
  containerId: string;
  containerName: string;
  status: 'healthy' | 'warning' | 'critical';
  cpuUsage: number;
  memoryUsage: number;
  cpuTrend: 'up' | 'down' | 'stable';
  memoryTrend: 'up' | 'down' | 'stable';
}

export interface SystemSummary {
  cpuUsage: number;
  memoryUsage: number;
  diskUsage: number;
  containerCount: number;
  runningContainers: number;
  cpuTrend: 'up' | 'down' | 'stable';
  memoryTrend: 'up' | 'down' | 'stable';
}

export interface MetricAlert {
  id: string;
  severity: 'info' | 'warning' | 'critical';
  type: 'cpu' | 'memory' | 'disk' | 'network';
  message: string;
  containerId?: string;
  containerName?: string;
  timestamp: string;
}

// Time range options for historical data
export type TimeRange = '5m' | '15m' | '1h' | '6h' | '24h' | '7d';

// Response types
export interface ContainerMetricsResponse {
  metrics: ContainerMetrics[];
  count: number;
}

export interface SystemMetricsResponse {
  metrics: SystemMetrics;
}

export interface MetricsHistoryResponse {
  containerHistory?: ContainerMetricsHistory[];
  systemHistory: SystemMetricsHistory;
  timeRange: TimeRange;
}

export interface MetricsSummaryResponse {
  summary: MetricsSummary;
}

// API functions

export async function getContainerMetrics(
  stackId: string = 'default',
  containerId?: string
): Promise<ContainerMetricsResponse> {
  const endpoint = containerId
    ? '/stacks/' + stackId + '/metrics/containers/' + containerId
    : '/stacks/' + stackId + '/metrics/containers';
  return fetchAPI<ContainerMetricsResponse>(endpoint);
}

export async function getSystemMetrics(
  stackId: string = 'default'
): Promise<SystemMetricsResponse> {
  return fetchAPI<SystemMetricsResponse>('/stacks/' + stackId + '/metrics/system');
}

export async function getMetricsHistory(
  stackId: string = 'default',
  timeRange: TimeRange = '1h',
  containerId?: string
): Promise<MetricsHistoryResponse> {
  const params = new URLSearchParams({ range: timeRange });
  if (containerId) {
    params.set('container', containerId);
  }
  return fetchAPI<MetricsHistoryResponse>(
    '/stacks/' + stackId + '/metrics/history?' + params.toString()
  );
}

export async function getMetricsSummary(
  stackId: string = 'default'
): Promise<MetricsSummaryResponse> {
  return fetchAPI<MetricsSummaryResponse>('/stacks/' + stackId + '/metrics/summary');
}

// Utility functions

export function formatBytes(bytes: number, decimals: number = 2): string {
  if (bytes === 0) return '0 B';

  const k = 1024;
  const dm = decimals < 0 ? 0 : decimals;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];

  const i = Math.floor(Math.log(bytes) / Math.log(k));

  return parseFloat((bytes / Math.pow(k, i)).toFixed(dm)) + ' ' + sizes[i];
}

export function formatPercent(value: number, decimals: number = 1): string {
  return value.toFixed(decimals) + '%';
}

export function formatUptime(seconds: number): string {
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);

  const parts: string[] = [];
  if (days > 0) parts.push(days + 'd');
  if (hours > 0) parts.push(hours + 'h');
  if (minutes > 0) parts.push(minutes + 'm');

  return parts.length > 0 ? parts.join(' ') : '< 1m';
}

export function getMetricColor(
  value: number,
  warningThreshold: number = 70,
  criticalThreshold: number = 90
): string {
  if (value >= criticalThreshold) return 'text-red-600';
  if (value >= warningThreshold) return 'text-yellow-600';
  return 'text-green-600';
}

export function getMetricBgColor(
  value: number,
  warningThreshold: number = 70,
  criticalThreshold: number = 90
): string {
  if (value >= criticalThreshold) return 'bg-red-100';
  if (value >= warningThreshold) return 'bg-yellow-100';
  return 'bg-green-100';
}
