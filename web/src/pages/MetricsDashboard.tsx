import { useState, useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { cn } from '@/lib/utils';
import {
  getContainerMetrics,
  getSystemMetrics,
  getMetricsHistory,
  getMetricsSummary,
  formatBytes,
  formatPercent,
  formatUptime,
  getMetricColor,
  getMetricBgColor,
  type TimeRange,
  type ContainerMetrics,
  type MetricsHistoryPoint,
} from '@/lib/metrics-api';
import {
  RefreshCw,
  Cpu,
  HardDrive,
  Network,
  Activity,
  TrendingUp,
  TrendingDown,
  Minus,
  AlertTriangle,
  AlertCircle,
  Info,
  Server,
  Container,
} from 'lucide-react';

const TIME_RANGE_OPTIONS: { value: TimeRange; label: string }[] = [
  { value: '5m', label: '5 min' },
  { value: '15m', label: '15 min' },
  { value: '1h', label: '1 hour' },
  { value: '6h', label: '6 hours' },
  { value: '24h', label: '24 hours' },
  { value: '7d', label: '7 days' },
];

const REFRESH_INTERVALS = [
  { value: 0, label: 'Off' },
  { value: 5000, label: '5s' },
  { value: 10000, label: '10s' },
  { value: 30000, label: '30s' },
  { value: 60000, label: '1m' },
];

interface MetricCardProps {
  title: string;
  value: string;
  subtitle?: string;
  trend?: 'up' | 'down' | 'stable';
  icon: React.ReactNode;
  colorClass?: string;
  bgColorClass?: string;
}

function MetricCard({
  title,
  value,
  subtitle,
  trend,
  icon,
  colorClass = 'text-foreground',
  bgColorClass,
}: MetricCardProps) {
  const TrendIcon = trend === 'up' ? TrendingUp : trend === 'down' ? TrendingDown : Minus;
  const trendColor = trend === 'up' ? 'text-error' : trend === 'down' ? 'text-green-500' : 'text-muted-foreground';

  return (
    <div className={cn('rounded-lg border p-4', bgColorClass)}>
      <div className="flex items-center justify-between">
        <h3 className="font-medium text-muted-foreground text-sm">{title}</h3>
        <div className="text-muted-foreground">{icon}</div>
      </div>
      <div className="mt-2 flex items-baseline gap-2">
        <p className={cn('text-2xl font-bold', colorClass)}>{value}</p>
        {trend && (
          <TrendIcon className={cn('h-4 w-4', trendColor)} />
        )}
      </div>
      {subtitle && (
        <p className="text-xs text-muted-foreground mt-1">{subtitle}</p>
      )}
    </div>
  );
}

interface MiniChartProps {
  data: MetricsHistoryPoint[];
  color?: string;
  height?: number;
}

function MiniChart({ data, color = '#3b82f6', height = 40 }: MiniChartProps) {
  if (!data || data.length === 0) {
    return (
      <div className="flex items-center justify-center h-10 text-xs text-muted-foreground">
        No data
      </div>
    );
  }

  const values = data.map(d => d.value);
  const max = Math.max(...values, 1);
  const min = Math.min(...values, 0);
  const range = max - min || 1;

  const width = 200;
  const points = data.map((d, i) => {
    const x = (i / (data.length - 1)) * width;
    const y = height - ((d.value - min) / range) * height;
    return x + ',' + y;
  }).join(' ');

  return (
    <svg width="100%" height={height} viewBox={'0 0 ' + width + ' ' + height} preserveAspectRatio="none">
      <polyline
        fill="none"
        stroke={color}
        strokeWidth="2"
        points={points}
      />
    </svg>
  );
}

interface ContainerMetricsRowProps {
  metrics: ContainerMetrics;
  isSelected: boolean;
  onSelect: () => void;
}

function ContainerMetricsRow({ metrics, isSelected, onSelect }: ContainerMetricsRowProps) {
  const cpuColor = getMetricColor(metrics.cpu.usagePercent);
  const memColor = getMetricColor(metrics.memory.usagePercent);

  return (
    <div
      className={cn(
        'flex items-center justify-between p-4 rounded-lg border cursor-pointer transition-colors',
        isSelected ? 'border-primary bg-primary/5' : 'hover:bg-muted/50'
      )}
      onClick={onSelect}
    >
      <div className="flex items-center gap-4">
        <Container className="h-5 w-5 text-muted-foreground" />
        <div>
          <p className="font-medium">{metrics.containerName}</p>
          <p className="text-xs text-muted-foreground">{metrics.containerId.substring(0, 12)}</p>
        </div>
      </div>
      <div className="flex items-center gap-6">
        <div className="text-right">
          <p className="text-xs text-muted-foreground">CPU</p>
          <p className={cn('font-medium', cpuColor)}>
            {formatPercent(metrics.cpu.usagePercent)}
          </p>
        </div>
        <div className="text-right">
          <p className="text-xs text-muted-foreground">Memory</p>
          <p className={cn('font-medium', memColor)}>
            {formatPercent(metrics.memory.usagePercent)}
          </p>
        </div>
        <div className="text-right">
          <p className="text-xs text-muted-foreground">Net I/O</p>
          <p className="font-medium text-sm">
            {formatBytes(metrics.network.rxBytes)} / {formatBytes(metrics.network.txBytes)}
          </p>
        </div>
        <div className="text-right">
          <p className="text-xs text-muted-foreground">Disk I/O</p>
          <p className="font-medium text-sm">
            {formatBytes(metrics.disk.readBytes)} / {formatBytes(metrics.disk.writeBytes)}
          </p>
        </div>
      </div>
    </div>
  );
}

interface AlertItemProps {
  severity: 'info' | 'warning' | 'critical';
  message: string;
  timestamp: string;
  containerName?: string;
}

function AlertItem({ severity, message, timestamp, containerName }: AlertItemProps) {
  const Icon = severity === 'critical' ? AlertCircle : severity === 'warning' ? AlertTriangle : Info;
  const colorClass = severity === 'critical' ? 'text-error' : severity === 'warning' ? 'text-warning' : 'text-info';
  const bgClass = severity === 'critical' ? 'bg-error/10' : severity === 'warning' ? 'bg-warning/10' : 'bg-info/10';

  return (
    <div className={cn('flex items-start gap-3 p-3 rounded-lg', bgClass)}>
      <Icon className={cn('h-5 w-5 mt-0.5', colorClass)} />
      <div className="flex-1 min-w-0">
        <p className="text-sm font-medium">{message}</p>
        <div className="flex items-center gap-2 mt-1">
          {containerName && (
            <span className="text-xs text-muted-foreground">{containerName}</span>
          )}
          <span className="text-xs text-muted-foreground">
            {new Date(timestamp).toLocaleTimeString()}
          </span>
        </div>
      </div>
    </div>
  );
}

export function MetricsDashboard() {
  const [timeRange, setTimeRange] = useState<TimeRange>('1h');
  const [refreshInterval, setRefreshInterval] = useState(10000);
  const [selectedContainer, setSelectedContainer] = useState<string | undefined>();

  // Query for container metrics
  const {
    data: containerData,
    isLoading: containerLoading,
    error: containerError,
    refetch: refetchContainers,
  } = useQuery({
    queryKey: ['containerMetrics', 'default'],
    queryFn: () => getContainerMetrics('default'),
    refetchInterval: refreshInterval || false,
  });

  // Query for system metrics
  const {
    data: systemData,
    isLoading: systemLoading,
    error: systemError,
    refetch: refetchSystem,
  } = useQuery({
    queryKey: ['systemMetrics', 'default'],
    queryFn: () => getSystemMetrics('default'),
    refetchInterval: refreshInterval || false,
  });

  // Query for metrics history
  const {
    data: historyData,
    isLoading: historyLoading,
    refetch: refetchHistory,
  } = useQuery({
    queryKey: ['metricsHistory', 'default', timeRange, selectedContainer],
    queryFn: () => getMetricsHistory('default', timeRange, selectedContainer),
    refetchInterval: refreshInterval || false,
  });

  // Query for metrics summary
  const {
    data: summaryData,
    isLoading: summaryLoading,
    refetch: refetchSummary,
  } = useQuery({
    queryKey: ['metricsSummary', 'default'],
    queryFn: () => getMetricsSummary('default'),
    refetchInterval: refreshInterval || false,
  });

  const handleRefreshAll = () => {
    refetchContainers();
    refetchSystem();
    refetchHistory();
    refetchSummary();
  };

  const systemMetrics = systemData?.metrics;
  const containerMetrics = containerData?.metrics || [];
  const summary = summaryData?.summary;
  const alerts = summary?.alerts || [];

  // Get the selected container's history for charts
  const selectedContainerHistory = useMemo(() => {
    if (!selectedContainer || !historyData?.containerHistory) return null;
    return historyData.containerHistory.find(h => h.containerId === selectedContainer);
  }, [selectedContainer, historyData]);

  const isLoading = containerLoading || systemLoading || historyLoading || summaryLoading;

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <h1 className="text-3xl font-bold">Metrics Dashboard</h1>
        <div className="flex items-center gap-4">
          {/* Time Range Selector */}
          <div className="flex items-center gap-2">
            <span className="text-sm text-muted-foreground">Range:</span>
            <div className="flex rounded-md border">
              {TIME_RANGE_OPTIONS.map((option) => (
                <button
                  key={option.value}
                  onClick={() => setTimeRange(option.value)}
                  className={cn(
                    'px-3 py-1.5 text-sm transition-colors',
                    timeRange === option.value
                      ? 'bg-primary text-primary-foreground'
                      : 'hover:bg-muted'
                  )}
                >
                  {option.label}
                </button>
              ))}
            </div>
          </div>

          {/* Refresh Interval Selector */}
          <div className="flex items-center gap-2">
            <span className="text-sm text-muted-foreground">Refresh:</span>
            <select
              value={refreshInterval}
              onChange={(e) => setRefreshInterval(Number(e.target.value))}
              className="rounded-md border px-3 py-1.5 text-sm bg-background"
            >
              {REFRESH_INTERVALS.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
          </div>

          <Button variant="outline" size="sm" onClick={handleRefreshAll} disabled={isLoading}>
            <RefreshCw className={cn('h-4 w-4 mr-2', isLoading && 'animate-spin')} />
            Refresh
          </Button>
        </div>
      </div>

      {/* System Metrics Overview */}
      <div>
        <h2 className="text-lg font-semibold mb-4 flex items-center gap-2">
          <Server className="h-5 w-5" />
          System Metrics
        </h2>
        {systemLoading ? (
          <div className="grid gap-4 md:grid-cols-5">
            {[1, 2, 3, 4, 5].map((i) => (
              <Skeleton key={i} className="h-24 rounded-lg" />
            ))}
          </div>
        ) : systemError ? (
          <div className="text-error p-4 border rounded-lg">
            Error loading system metrics
          </div>
        ) : systemMetrics ? (
          <div className="grid gap-4 md:grid-cols-5">
            <MetricCard
              title="CPU Usage"
              value={formatPercent(systemMetrics.cpu.usagePercent)}
              subtitle={systemMetrics.cpu.cores + ' cores'}
              trend={summary?.system.cpuTrend}
              icon={<Cpu className="h-5 w-5" />}
              colorClass={getMetricColor(systemMetrics.cpu.usagePercent)}
              bgColorClass={getMetricBgColor(systemMetrics.cpu.usagePercent)}
            />
            <MetricCard
              title="Memory Usage"
              value={formatPercent(systemMetrics.memory.usagePercent)}
              subtitle={formatBytes(systemMetrics.memory.used) + ' / ' + formatBytes(systemMetrics.memory.total)}
              trend={summary?.system.memoryTrend}
              icon={<Activity className="h-5 w-5" />}
              colorClass={getMetricColor(systemMetrics.memory.usagePercent)}
              bgColorClass={getMetricBgColor(systemMetrics.memory.usagePercent)}
            />
            <MetricCard
              title="Disk Usage"
              value={formatPercent(systemMetrics.disk.usagePercent)}
              subtitle={formatBytes(systemMetrics.disk.used) + ' / ' + formatBytes(systemMetrics.disk.total)}
              icon={<HardDrive className="h-5 w-5" />}
              colorClass={getMetricColor(systemMetrics.disk.usagePercent)}
              bgColorClass={getMetricBgColor(systemMetrics.disk.usagePercent)}
            />
            <MetricCard
              title="Network I/O"
              value={formatBytes(systemMetrics.network.totalRxBytes + systemMetrics.network.totalTxBytes)}
              subtitle={'RX: ' + formatBytes(systemMetrics.network.totalRxBytes) + ' / TX: ' + formatBytes(systemMetrics.network.totalTxBytes)}
              icon={<Network className="h-5 w-5" />}
            />
            <MetricCard
              title="System Uptime"
              value={formatUptime(systemMetrics.uptime)}
              subtitle={'Load: ' + systemMetrics.load.load1.toFixed(2) + ' / ' + systemMetrics.load.load5.toFixed(2) + ' / ' + systemMetrics.load.load15.toFixed(2)}
              icon={<Activity className="h-5 w-5" />}
            />
          </div>
        ) : null}
      </div>

      {/* Charts Section */}
      <div className="grid gap-6 md:grid-cols-2">
        {/* CPU History Chart */}
        <div className="rounded-lg border p-4">
          <h3 className="font-semibold mb-4">CPU Usage History</h3>
          {historyLoading ? (
            <Skeleton className="h-40" />
          ) : historyData?.systemHistory?.cpuUsage ? (
            <div className="h-40">
              <MiniChart data={historyData.systemHistory.cpuUsage} color="#3b82f6" height={160} />
            </div>
          ) : (
            <div className="h-40 flex items-center justify-center text-muted-foreground">
              No data available
            </div>
          )}
        </div>

        {/* Memory History Chart */}
        <div className="rounded-lg border p-4">
          <h3 className="font-semibold mb-4">Memory Usage History</h3>
          {historyLoading ? (
            <Skeleton className="h-40" />
          ) : historyData?.systemHistory?.memoryUsage ? (
            <div className="h-40">
              <MiniChart data={historyData.systemHistory.memoryUsage} color="#10b981" height={160} />
            </div>
          ) : (
            <div className="h-40 flex items-center justify-center text-muted-foreground">
              No data available
            </div>
          )}
        </div>

        {/* Network History Chart */}
        <div className="rounded-lg border p-4">
          <h3 className="font-semibold mb-4">Network I/O History</h3>
          {historyLoading ? (
            <Skeleton className="h-40" />
          ) : historyData?.systemHistory?.networkRx ? (
            <div className="h-40">
              <MiniChart data={historyData.systemHistory.networkRx} color="#f59e0b" height={160} />
            </div>
          ) : (
            <div className="h-40 flex items-center justify-center text-muted-foreground">
              No data available
            </div>
          )}
        </div>

        {/* Load History Chart */}
        <div className="rounded-lg border p-4">
          <h3 className="font-semibold mb-4">System Load History</h3>
          {historyLoading ? (
            <Skeleton className="h-40" />
          ) : historyData?.systemHistory?.load1 ? (
            <div className="h-40">
              <MiniChart data={historyData.systemHistory.load1} color="#8b5cf6" height={160} />
            </div>
          ) : (
            <div className="h-40 flex items-center justify-center text-muted-foreground">
              No data available
            </div>
          )}
        </div>
      </div>

      {/* Alerts Section */}
      {alerts.length > 0 && (
        <div className="rounded-lg border p-4">
          <h2 className="text-lg font-semibold mb-4 flex items-center gap-2">
            <AlertTriangle className="h-5 w-5 text-warning" />
            Active Alerts ({alerts.length})
          </h2>
          <div className="space-y-2">
            {alerts.map((alert) => (
              <AlertItem
                key={alert.id}
                severity={alert.severity}
                message={alert.message}
                timestamp={alert.timestamp}
                containerName={alert.containerName}
              />
            ))}
          </div>
        </div>
      )}

      {/* Container Metrics */}
      <div>
        <h2 className="text-lg font-semibold mb-4 flex items-center gap-2">
          <Container className="h-5 w-5" />
          Container Metrics ({containerMetrics.length})
        </h2>
        {containerLoading ? (
          <div className="space-y-2">
            {[1, 2, 3].map((i) => (
              <Skeleton key={i} className="h-20 rounded-lg" />
            ))}
          </div>
        ) : containerError ? (
          <div className="text-error p-4 border rounded-lg">
            Error loading container metrics
          </div>
        ) : containerMetrics.length === 0 ? (
          <div className="text-center py-8 text-muted-foreground border rounded-lg">
            No containers found
          </div>
        ) : (
          <div className="space-y-2">
            {containerMetrics.map((metrics) => (
              <ContainerMetricsRow
                key={metrics.containerId}
                metrics={metrics}
                isSelected={selectedContainer === metrics.containerId}
                onSelect={() =>
                  setSelectedContainer(
                    selectedContainer === metrics.containerId ? undefined : metrics.containerId
                  )
                }
              />
            ))}
          </div>
        )}
      </div>

      {/* Selected Container Details */}
      {selectedContainer && selectedContainerHistory && (
        <div className="rounded-lg border p-4">
          <h2 className="text-lg font-semibold mb-4">
            Container Details: {selectedContainerHistory.containerName}
          </h2>
          <div className="grid gap-4 md:grid-cols-2">
            <div>
              <h3 className="font-medium mb-2">CPU Usage</h3>
              <div className="h-32">
                <MiniChart data={selectedContainerHistory.cpuUsage} color="#3b82f6" height={128} />
              </div>
            </div>
            <div>
              <h3 className="font-medium mb-2">Memory Usage</h3>
              <div className="h-32">
                <MiniChart data={selectedContainerHistory.memoryUsage} color="#10b981" height={128} />
              </div>
            </div>
            <div>
              <h3 className="font-medium mb-2">Network RX/TX</h3>
              <div className="h-32">
                <MiniChart data={selectedContainerHistory.networkRx} color="#f59e0b" height={128} />
              </div>
            </div>
            <div>
              <h3 className="font-medium mb-2">Disk Read/Write</h3>
              <div className="h-32">
                <MiniChart data={selectedContainerHistory.diskRead} color="#8b5cf6" height={128} />
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
