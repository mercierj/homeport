import { fetchAPI } from './api';
import { API_BASE } from './config';

// Log severity levels
export type LogSeverity = 'error' | 'warn' | 'info' | 'debug' | 'trace';

// Individual log entry
export interface LogEntry {
  timestamp: string;
  severity: LogSeverity;
  message: string;
  container: string;
  source?: string;
}

// Response from getting container logs
export interface ContainerLogsResponse {
  logs: LogEntry[];
  container: string;
  totalLines: number;
  fromTimestamp?: string;
  toTimestamp?: string;
}

// Log statistics for a container
export interface LogStats {
  container: string;
  totalLines: number;
  errorCount: number;
  warnCount: number;
  infoCount: number;
  debugCount: number;
  traceCount: number;
  firstLogTimestamp?: string;
  lastLogTimestamp?: string;
}

// Response from log statistics endpoint
export interface LogStatsResponse {
  stats: LogStats[];
  totalContainers: number;
}

// Search results response
export interface SearchLogsResponse {
  results: LogEntry[];
  totalMatches: number;
  pattern: string;
  containers: string[];
}

// Query parameters for fetching logs
export interface GetLogsParams {
  tail?: number;
  since?: string;
  until?: string;
  severity?: LogSeverity[];
  search?: string;
}

// Query parameters for searching logs
export interface SearchLogsParams {
  pattern: string;
  containers: string[];
  severity?: LogSeverity[];
  since?: string;
  until?: string;
  limit?: number;
  caseSensitive?: boolean;
  regex?: boolean;
}

export function parseLogLine(line: string, container: string): LogEntry {
  const isoTimestampRegex = /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z?)\s*/;
  const severityRegex = /\[(ERROR|WARN(?:ING)?|INFO|DEBUG|TRACE)\]|\b(error|warn(?:ing)?|info|debug|trace)\b:?/i;

  let timestamp = new Date().toISOString();
  let severity: LogSeverity = 'info';
  let message = line;

  const timestampMatch = line.match(isoTimestampRegex);
  if (timestampMatch) {
    timestamp = timestampMatch[1];
    message = line.slice(timestampMatch[0].length);
  }

  const severityMatch = message.match(severityRegex);
  if (severityMatch) {
    const severityStr = (severityMatch[1] || severityMatch[2]).toLowerCase();
    if (severityStr.startsWith('error')) severity = 'error';
    else if (severityStr.startsWith('warn')) severity = 'warn';
    else if (severityStr === 'info') severity = 'info';
    else if (severityStr === 'debug') severity = 'debug';
    else if (severityStr === 'trace') severity = 'trace';
  } else {
    const lowerMessage = message.toLowerCase();
    if (lowerMessage.includes('error') || lowerMessage.includes('exception') || lowerMessage.includes('failed')) {
      severity = 'error';
    } else if (lowerMessage.includes('warn') || lowerMessage.includes('warning')) {
      severity = 'warn';
    } else if (lowerMessage.includes('debug')) {
      severity = 'debug';
    }
  }

  return { timestamp, severity, message: message.trim(), container };
}

export async function getContainerLogs(
  stackId: string,
  containerName: string,
  params: GetLogsParams = {}
): Promise<ContainerLogsResponse> {
  const queryParams = new URLSearchParams();
  if (params.tail) queryParams.set('tail', params.tail.toString());
  if (params.since) queryParams.set('since', params.since);
  if (params.until) queryParams.set('until', params.until);
  if (params.severity?.length) queryParams.set('severity', params.severity.join(','));
  if (params.search) queryParams.set('search', params.search);

  const queryString = queryParams.toString();
  const endpoint = '/stacks/' + stackId + '/containers/' + containerName + '/logs' + (queryString ? '?' + queryString : '');

  const response = await fetchAPI<{ logs: string }>(endpoint);

  const lines = response.logs.split('\n').filter(line => line.trim());
  const logs = lines.map(line => parseLogLine(line, containerName));

  let filteredLogs = logs;
  if (params.severity?.length) {
    filteredLogs = logs.filter(log => params.severity!.includes(log.severity));
  }

  if (params.search) {
    const searchLower = params.search.toLowerCase();
    filteredLogs = filteredLogs.filter(log => log.message.toLowerCase().includes(searchLower));
  }

  return {
    logs: filteredLogs,
    container: containerName,
    totalLines: filteredLogs.length,
    fromTimestamp: filteredLogs[0]?.timestamp,
    toTimestamp: filteredLogs[filteredLogs.length - 1]?.timestamp,
  };
}

export async function getMultiContainerLogs(
  stackId: string,
  containerNames: string[],
  params: GetLogsParams = {}
): Promise<LogEntry[]> {
  const allLogs = await Promise.all(
    containerNames.map(name => getContainerLogs(stackId, name, params))
  );

  const mergedLogs = allLogs.flatMap(response => response.logs);
  mergedLogs.sort((a, b) => new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime());

  return mergedLogs;
}

export function streamLogs(
  stackId: string,
  containerName: string,
  onLog: (entry: LogEntry) => void,
  onError?: (error: Event) => void
): EventSource {
  const url = API_BASE + '/stacks/' + stackId + '/containers/' + containerName + '/logs/stream';
  const eventSource = new EventSource(url, { withCredentials: true });

  eventSource.onmessage = (event) => {
    try {
      const data = JSON.parse(event.data);
      if (data.message) {
        onLog({
          timestamp: data.timestamp || new Date().toISOString(),
          severity: data.severity || 'info',
          message: data.message,
          container: containerName,
          source: data.source,
        });
      } else {
        onLog(parseLogLine(event.data, containerName));
      }
    } catch {
      onLog(parseLogLine(event.data, containerName));
    }
  };

  eventSource.onerror = (error) => {
    onError?.(error);
  };

  return eventSource;
}

export function streamMultiContainerLogs(
  stackId: string,
  containerNames: string[],
  onLog: (entry: LogEntry) => void,
  onError?: (error: Event, container: string) => void
): EventSource[] {
  return containerNames.map(name =>
    streamLogs(stackId, name, onLog, (error) => onError?.(error, name))
  );
}

export async function searchLogs(
  stackId: string,
  params: SearchLogsParams
): Promise<SearchLogsResponse> {
  const allLogs = await getMultiContainerLogs(stackId, params.containers, {
    tail: params.limit || 1000,
    since: params.since,
    until: params.until,
    severity: params.severity,
  });

  let pattern: RegExp;
  try {
    if (params.regex) {
      pattern = new RegExp(params.pattern, params.caseSensitive ? '' : 'i');
    } else {
      const escaped = params.pattern.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
      pattern = new RegExp(escaped, params.caseSensitive ? '' : 'i');
    }
  } catch {
    const escaped = params.pattern.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    pattern = new RegExp(escaped, params.caseSensitive ? '' : 'i');
  }

  const results = allLogs.filter(log => pattern.test(log.message));

  return {
    results: results.slice(0, params.limit || 1000),
    totalMatches: results.length,
    pattern: params.pattern,
    containers: params.containers,
  };
}

export async function getLogStats(
  stackId: string,
  containerNames: string[]
): Promise<LogStatsResponse> {
  const statsPromises = containerNames.map(async (containerName) => {
    const { logs } = await getContainerLogs(stackId, containerName, { tail: 500 });

    const stats: LogStats = {
      container: containerName,
      totalLines: logs.length,
      errorCount: logs.filter(l => l.severity === 'error').length,
      warnCount: logs.filter(l => l.severity === 'warn').length,
      infoCount: logs.filter(l => l.severity === 'info').length,
      debugCount: logs.filter(l => l.severity === 'debug').length,
      traceCount: logs.filter(l => l.severity === 'trace').length,
      firstLogTimestamp: logs[0]?.timestamp,
      lastLogTimestamp: logs[logs.length - 1]?.timestamp,
    };

    return stats;
  });

  const stats = await Promise.all(statsPromises);

  return { stats, totalContainers: stats.length };
}

export function downloadLogs(logs: LogEntry[], filename: string = 'logs.txt'): void {
  const content = logs
    .map(log => '[' + log.timestamp + '] [' + log.severity.toUpperCase() + '] [' + log.container + '] ' + log.message)
    .join('\n');

  const blob = new Blob([content], { type: 'text/plain' });
  const url = URL.createObjectURL(blob);

  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);

  URL.revokeObjectURL(url);
}

export function downloadLogsAsJson(logs: LogEntry[], filename: string = 'logs.json'): void {
  const content = JSON.stringify(logs, null, 2);

  const blob = new Blob([content], { type: 'application/json' });
  const url = URL.createObjectURL(blob);

  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);

  URL.revokeObjectURL(url);
}
