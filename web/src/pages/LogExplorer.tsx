import { useState, useEffect, useRef, useCallback, useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { cn } from '@/lib/utils';
import { listContainers, type Container } from '@/lib/docker-api';
import {
  getMultiContainerLogs,
  getLogStats,
  streamMultiContainerLogs,
  searchLogs,
  downloadLogs,
  downloadLogsAsJson,
  type LogEntry,
  type LogSeverity,
  type LogStats,
} from '@/lib/logs-api';
import {
  Search,
  Download,
  RefreshCw,
  Play,
  Pause,
  ArrowDown,
  X,
  AlertTriangle,
  AlertCircle,
  Info,
  Bug,
  Check,
  ChevronDown,
  FileJson,
  FileText,
} from 'lucide-react';

const SEVERITY_CONFIG: Record<LogSeverity, { levelClass: string; icon: React.ReactNode }> = {
  error: {
    levelClass: 'log-level-error',
    icon: <AlertCircle className="h-3 w-3" />,
  },
  warn: {
    levelClass: 'log-level-warn',
    icon: <AlertTriangle className="h-3 w-3" />,
  },
  info: {
    levelClass: 'log-level-info',
    icon: <Info className="h-3 w-3" />,
  },
  debug: {
    levelClass: 'log-level-debug',
    icon: <Bug className="h-3 w-3" />,
  },
  trace: {
    levelClass: 'log-level-debug',
    icon: <Bug className="h-3 w-3" />,
  },
};

const TIME_RANGES = [
  { label: 'Last 15 minutes', value: '15m' },
  { label: 'Last 1 hour', value: '1h' },
  { label: 'Last 6 hours', value: '6h' },
  { label: 'Last 24 hours', value: '24h' },
  { label: 'Last 7 days', value: '7d' },
  { label: 'All time', value: 'all' },
];

function getTimeRangeSince(range: string): string | undefined {
  if (range === 'all') return undefined;

  const now = new Date();
  const match = range.match(/^(\d+)([mhd])$/);
  if (!match) return undefined;

  const [, amount, unit] = match;
  const num = parseInt(amount, 10);

  switch (unit) {
    case 'm':
      now.setMinutes(now.getMinutes() - num);
      break;
    case 'h':
      now.setHours(now.getHours() - num);
      break;
    case 'd':
      now.setDate(now.getDate() - num);
      break;
  }

  return now.toISOString();
}

interface LogLineProps {
  entry: LogEntry;
  searchPattern?: string;
}

function LogLine({ entry, searchPattern }: LogLineProps) {
  const config = SEVERITY_CONFIG[entry.severity];

  const highlightedMessage = useMemo(() => {
    if (!searchPattern) return entry.message;

    try {
      const regex = new RegExp('(' + searchPattern.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + ')', 'gi');
      const parts = entry.message.split(regex);

      return parts.map((part, i) => {
        if (regex.test(part)) {
          return <mark key={i} className="bg-yellow-300 text-black px-0.5 rounded">{part}</mark>;
        }
        return part;
      });
    } catch {
      return entry.message;
    }
  }, [entry.message, searchPattern]);

  return (
    <div className={cn('flex items-start gap-2 py-1 px-3 font-mono text-xs hover:bg-muted/50', config.levelClass)}>
      <span className="text-muted-foreground shrink-0 w-[180px]">
        {new Date(entry.timestamp).toLocaleString()}
      </span>
      <span className="shrink-0 w-16 flex items-center gap-1">
        {config.icon}
        <span className="uppercase font-medium">{entry.severity}</span>
      </span>
      <span className="shrink-0 text-muted-foreground w-32 truncate" title={entry.container}>
        [{entry.container}]
      </span>
      <span className="flex-1 break-all whitespace-pre-wrap">{highlightedMessage}</span>
    </div>
  );
}

interface ContainerSelectorProps {
  containers: Container[];
  selected: Set<string>;
  onToggle: (name: string) => void;
  onSelectAll: () => void;
  onClearAll: () => void;
}

function ContainerSelector({ containers, selected, onToggle, onSelectAll, onClearAll }: ContainerSelectorProps) {
  const [isOpen, setIsOpen] = useState(false);

  return (
    <div className="relative">
      <Button
        variant="outline"
        size="sm"
        onClick={() => setIsOpen(!isOpen)}
        className="min-w-[200px] justify-between"
      >
        <span className="truncate">
          {selected.size === 0 ? 'Select containers' : selected.size + ' container(s)'}
        </span>
        <ChevronDown className={cn('h-4 w-4 ml-2 transition-transform', isOpen && 'rotate-180')} />
      </Button>

      {isOpen && (
        <>
          <div className="fixed inset-0 z-40" onClick={() => setIsOpen(false)} />
          <div className="absolute top-full left-0 mt-1 w-64 bg-background border rounded-md shadow-lg z-50 max-h-72 overflow-auto">
            <div className="flex items-center justify-between p-2 border-b">
              <Button variant="ghost" size="sm" onClick={onSelectAll}>Select All</Button>
              <Button variant="ghost" size="sm" onClick={onClearAll}>Clear All</Button>
            </div>
            {containers.map(container => (
              <label
                key={container.id}
                className="flex items-center gap-2 px-3 py-2 hover:bg-muted cursor-pointer"
              >
                <input
                  type="checkbox"
                  checked={selected.has(container.name)}
                  onChange={() => onToggle(container.name)}
                  className="h-4 w-4 rounded border-input"
                />
                <span className="flex-1 truncate text-sm">{container.name}</span>
                <span className={cn(
                  'text-xs px-1.5 py-0.5 rounded',
                  container.state === 'running' ? 'bg-success/10 text-success' : 'bg-muted text-muted-foreground'
                )}>
                  {container.state}
                </span>
              </label>
            ))}
          </div>
        </>
      )}
    </div>
  );
}

interface SeverityFilterProps {
  selected: Set<LogSeverity>;
  onToggle: (severity: LogSeverity) => void;
}

const SEVERITY_BUTTON_CLASSES: Record<LogSeverity, string> = {
  error: 'bg-error text-error-foreground',
  warn: 'bg-warning text-warning-foreground',
  info: 'bg-info text-info-foreground',
  debug: 'bg-muted text-muted-foreground',
  trace: 'bg-muted text-muted-foreground',
};

function SeverityFilter({ selected, onToggle }: SeverityFilterProps) {
  const severities: LogSeverity[] = ['error', 'warn', 'info', 'debug', 'trace'];

  return (
    <div className="flex items-center gap-1">
      {severities.map(severity => {
        const config = SEVERITY_CONFIG[severity];
        const isSelected = selected.has(severity);

        return (
          <Button
            key={severity}
            variant={isSelected ? 'default' : 'outline'}
            size="sm"
            onClick={() => onToggle(severity)}
            className={cn(
              'h-7 px-2 text-xs uppercase',
              isSelected && SEVERITY_BUTTON_CLASSES[severity]
            )}
          >
            {config.icon}
            <span className="ml-1">{severity}</span>
          </Button>
        );
      })}
    </div>
  );
}

interface LogStatsDisplayProps {
  stats: LogStats[];
}

function LogStatsDisplay({ stats }: LogStatsDisplayProps) {
  const totals = useMemo(() => {
    return stats.reduce(
      (acc, s) => ({
        total: acc.total + s.totalLines,
        errors: acc.errors + s.errorCount,
        warns: acc.warns + s.warnCount,
        infos: acc.infos + s.infoCount,
        debugs: acc.debugs + s.debugCount,
      }),
      { total: 0, errors: 0, warns: 0, infos: 0, debugs: 0 }
    );
  }, [stats]);

  return (
    <div className="flex items-center gap-4 text-sm">
      <span className="text-muted-foreground">
        Total: <span className="font-medium text-foreground">{totals.total}</span>
      </span>
      <span className="text-error">
        Errors: <span className="font-medium">{totals.errors}</span>
      </span>
      <span className="text-warning">
        Warnings: <span className="font-medium">{totals.warns}</span>
      </span>
      <span className="text-info">
        Info: <span className="font-medium">{totals.infos}</span>
      </span>
      <span className="text-muted-foreground">
        Debug: <span className="font-medium">{totals.debugs}</span>
      </span>
    </div>
  );
}

export function LogExplorer() {
  const stackId = 'default';
  const logContainerRef = useRef<HTMLDivElement>(null);
  const eventSourcesRef = useRef<EventSource[]>([]);

  // State
  const [selectedContainers, setSelectedContainers] = useState<Set<string>>(new Set());
  const hasAutoSelectedRef = useRef(false);
  const [selectedSeverities, setSelectedSeverities] = useState<Set<LogSeverity>>(new Set(['error', 'warn', 'info', 'debug', 'trace']));
  const [timeRange, setTimeRange] = useState('1h');
  const [searchQuery, setSearchQuery] = useState('');
  const [isSearching, setIsSearching] = useState(false);
  const [isStreaming, setIsStreaming] = useState(false);
  const [autoScroll, setAutoScroll] = useState(true);
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [stats, setStats] = useState<LogStats[]>([]);
  const [showDownloadMenu, setShowDownloadMenu] = useState(false);

  // Fetch containers
  const { data: containersData, isLoading: containersLoading } = useQuery({
    queryKey: ['containers', stackId],
    queryFn: () => listContainers(stackId),
    refetchInterval: 10000,
  });

  const containers = containersData?.containers || [];
  const runningContainers = containers.filter(c => c.state === 'running');

  // Auto-select all running containers on first load
  useEffect(() => {
    if (!hasAutoSelectedRef.current && runningContainers.length > 0 && selectedContainers.size === 0) {
      hasAutoSelectedRef.current = true;
      setSelectedContainers(new Set(runningContainers.map(c => c.name)));
    }
  }, [runningContainers, selectedContainers.size]);

  // Fetch logs
  const fetchLogs = useCallback(async () => {
    if (selectedContainers.size === 0) {
      setLogs([]);
      setStats([]);
      return;
    }

    const containerNames = Array.from(selectedContainers);
    const since = getTimeRangeSince(timeRange);

    try {
      setIsSearching(true);

      if (searchQuery.trim()) {
        // Search mode
        const result = await searchLogs(stackId, {
          pattern: searchQuery,
          containers: containerNames,
          severity: Array.from(selectedSeverities),
          since,
          limit: 1000,
        });
        setLogs(result.results);
      } else {
        // Normal fetch mode
        const fetchedLogs = await getMultiContainerLogs(stackId, containerNames, {
          tail: 500,
          since,
          severity: Array.from(selectedSeverities),
        });
        setLogs(fetchedLogs);
      }

      // Fetch stats
      const statsResult = await getLogStats(stackId, containerNames);
      setStats(statsResult.stats);
    } catch (error) {
      console.error('Failed to fetch logs:', error);
    } finally {
      setIsSearching(false);
    }
  }, [selectedContainers, selectedSeverities, timeRange, searchQuery, stackId]);

  // Initial fetch and refetch on filter changes
  useEffect(() => {
    if (!isStreaming) {
      fetchLogs();
    }
  }, [fetchLogs, isStreaming]);

  // Streaming
  const startStreaming = useCallback(() => {
    if (selectedContainers.size === 0) return;

    // Close any existing streams
    eventSourcesRef.current.forEach(es => es.close());

    const containerNames = Array.from(selectedContainers);
    const sources = streamMultiContainerLogs(
      stackId,
      containerNames,
      (entry) => {
        if (selectedSeverities.has(entry.severity)) {
          setLogs(prev => [...prev.slice(-999), entry]);
        }
      },
      (error, container) => {
        console.error('Stream error for', container, error);
      }
    );

    eventSourcesRef.current = sources;
    setIsStreaming(true);
  }, [selectedContainers, selectedSeverities, stackId]);

  const stopStreaming = useCallback(() => {
    eventSourcesRef.current.forEach(es => es.close());
    eventSourcesRef.current = [];
    setIsStreaming(false);
  }, []);

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      eventSourcesRef.current.forEach(es => es.close());
    };
  }, []);

  // Auto-scroll
  const handleScroll = useCallback(() => {
    if (!logContainerRef.current) return;
    const { scrollTop, scrollHeight, clientHeight } = logContainerRef.current;
    setAutoScroll(scrollHeight - scrollTop - clientHeight < 50);
  }, []);

  useEffect(() => {
    if (logContainerRef.current && autoScroll) {
      logContainerRef.current.scrollTop = logContainerRef.current.scrollHeight;
    }
  }, [logs, autoScroll]);

  const scrollToBottom = () => {
    if (logContainerRef.current) {
      logContainerRef.current.scrollTop = logContainerRef.current.scrollHeight;
      setAutoScroll(true);
    }
  };

  // Container selection handlers
  const toggleContainer = (name: string) => {
    setSelectedContainers(prev => {
      const next = new Set(prev);
      if (next.has(name)) {
        next.delete(name);
      } else {
        next.add(name);
      }
      return next;
    });
  };

  const selectAllContainers = () => {
    setSelectedContainers(new Set(runningContainers.map(c => c.name)));
  };

  const clearAllContainers = () => {
    setSelectedContainers(new Set());
  };

  // Severity filter handler
  const toggleSeverity = (severity: LogSeverity) => {
    setSelectedSeverities(prev => {
      const next = new Set(prev);
      if (next.has(severity)) {
        next.delete(severity);
      } else {
        next.add(severity);
      }
      return next;
    });
  };

  // Download handlers
  const handleDownloadText = () => {
    const filename = 'logs-' + new Date().toISOString().slice(0, 19).replace(/:/g, '-') + '.txt';
    downloadLogs(logs, filename);
    setShowDownloadMenu(false);
  };

  const handleDownloadJson = () => {
    const filename = 'logs-' + new Date().toISOString().slice(0, 19).replace(/:/g, '-') + '.json';
    downloadLogsAsJson(logs, filename);
    setShowDownloadMenu(false);
  };

  // Filter displayed logs by severity
  const filteredLogs = useMemo(() => {
    return logs.filter(log => selectedSeverities.has(log.severity));
  }, [logs, selectedSeverities]);

  if (containersLoading) {
    return (
      <div className="space-y-6">
        <div className="flex items-center justify-between">
          <Skeleton className="h-8 w-48" />
          <Skeleton className="h-9 w-32" />
        </div>
        <div className="space-y-2">
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-[500px] w-full" />
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-3xl font-bold">Log Explorer</h1>
        {stats.length > 0 && <LogStatsDisplay stats={stats} />}
      </div>

      {/* Filters Row */}
      <div className="flex flex-wrap items-center gap-3 p-4 bg-muted/30 rounded-lg border">
        <ContainerSelector
          containers={containers}
          selected={selectedContainers}
          onToggle={toggleContainer}
          onSelectAll={selectAllContainers}
          onClearAll={clearAllContainers}
        />

        <select
          value={timeRange}
          onChange={(e) => setTimeRange(e.target.value)}
          className="h-9 px-3 text-sm rounded-md border bg-background"
        >
          {TIME_RANGES.map(range => (
            <option key={range.value} value={range.value}>{range.label}</option>
          ))}
        </select>

        <SeverityFilter
          selected={selectedSeverities}
          onToggle={toggleSeverity}
        />

        <div className="flex-1" />

        <div className="relative flex items-center gap-2">
          <div className="relative">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
            <input
              type="text"
              placeholder="Search logs (grep-like)..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && fetchLogs()}
              className="h-9 pl-9 pr-8 w-64 text-sm rounded-md border bg-background"
            />
            {searchQuery && (
              <button
                onClick={() => setSearchQuery('')}
                className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
              >
                <X className="h-4 w-4" />
              </button>
            )}
          </div>

          <Button variant="outline" size="sm" onClick={fetchLogs} disabled={isSearching}>
            {isSearching ? (
              <RefreshCw className="h-4 w-4 animate-spin" />
            ) : (
              <RefreshCw className="h-4 w-4" />
            )}
          </Button>

          <Button
            variant={isStreaming ? 'error' : 'default'}
            size="sm"
            onClick={isStreaming ? stopStreaming : startStreaming}
            disabled={selectedContainers.size === 0}
          >
            {isStreaming ? (
              <>
                <Pause className="h-4 w-4 mr-1" />
                Stop
              </>
            ) : (
              <>
                <Play className="h-4 w-4 mr-1" />
                Stream
              </>
            )}
          </Button>

          <div className="relative">
            <Button
              variant="outline"
              size="sm"
              onClick={() => setShowDownloadMenu(!showDownloadMenu)}
              disabled={logs.length === 0}
            >
              <Download className="h-4 w-4" />
            </Button>

            {showDownloadMenu && (
              <>
                <div className="fixed inset-0 z-40" onClick={() => setShowDownloadMenu(false)} />
                <div className="absolute top-full right-0 mt-1 w-40 bg-background border rounded-md shadow-lg z-50">
                  <button
                    onClick={handleDownloadText}
                    className="flex items-center gap-2 w-full px-3 py-2 text-sm hover:bg-muted"
                  >
                    <FileText className="h-4 w-4" />
                    Download as TXT
                  </button>
                  <button
                    onClick={handleDownloadJson}
                    className="flex items-center gap-2 w-full px-3 py-2 text-sm hover:bg-muted"
                  >
                    <FileJson className="h-4 w-4" />
                    Download as JSON
                  </button>
                </div>
              </>
            )}
          </div>
        </div>
      </div>

      {/* Log Viewer */}
      <div className="relative border rounded-lg overflow-hidden">
        <div className="bg-muted/50 px-4 py-2 border-b flex items-center justify-between">
          <span className="text-sm text-muted-foreground">
            Showing {filteredLogs.length} log entries
            {isStreaming && (
              <span className="ml-2 inline-flex items-center gap-1 text-success">
                <span className="status-dot-success animate-pulse" />
                Streaming
              </span>
            )}
          </span>
          {searchQuery && (
            <span className="text-sm">
              Search: <code className="px-1 py-0.5 bg-muted rounded">{searchQuery}</code>
            </span>
          )}
        </div>

        <div
          ref={logContainerRef}
          onScroll={handleScroll}
          className="h-[600px] overflow-auto bg-background"
        >
          {filteredLogs.length === 0 ? (
            <div className="flex flex-col items-center justify-center h-full text-muted-foreground">
              {selectedContainers.size === 0 ? (
                <>
                  <AlertTriangle className="h-12 w-12 mb-4" />
                  <p>Select at least one container to view logs</p>
                </>
              ) : isSearching ? (
                <>
                  <RefreshCw className="h-12 w-12 mb-4 animate-spin" />
                  <p>Loading logs...</p>
                </>
              ) : (
                <>
                  <Check className="h-12 w-12 mb-4" />
                  <p>No logs found matching your filters</p>
                </>
              )}
            </div>
          ) : (
            <div className="divide-y divide-border/50">
              {filteredLogs.map((entry, index) => (
                <LogLine
                  key={entry.timestamp + '-' + index}
                  entry={entry}
                  searchPattern={searchQuery || undefined}
                />
              ))}
            </div>
          )}
        </div>

        {!autoScroll && filteredLogs.length > 0 && (
          <Button
            variant="secondary"
            size="sm"
            onClick={scrollToBottom}
            className="absolute bottom-4 right-4 shadow-lg"
          >
            <ArrowDown className="h-4 w-4 mr-1" />
            Scroll to bottom
          </Button>
        )}
      </div>
    </div>
  );
}
