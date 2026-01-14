import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import {
  listKeys,
  getKey,
  setKey,
  deleteKey,
  getCacheStats,
  bulkDeleteKeys,
  type CacheKey,
  type KeyType,
} from '@/lib/cache-api';

import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';
import {
  Database,
  RefreshCw,
  Trash2,
  Search,
  Plus,
  Edit,
  Save,
  X,
  Clock,
} from 'lucide-react';
import { Skeleton } from '@/components/ui/skeleton';

// Cache type colors - using design system semantic colors
const TYPE_COLORS: Record<KeyType, string> = {
  string: 'bg-success/10 text-success',
  list: 'bg-info/10 text-info',
  set: 'bg-purple-500/10 text-purple-600',
  zset: 'bg-warning/10 text-warning',
  hash: 'bg-warning/10 text-warning',
  stream: 'bg-pink-500/10 text-pink-600',
  none: 'bg-muted text-muted-foreground',
};

export function CacheBrowser({ stackId = 'default' }: { stackId?: string }) {
  const [searchPattern, setSearchPattern] = useState('*');
  const [editingKey, setEditingKey] = useState<string | null>(null);
  const [editValue, setEditValue] = useState('');
  const [newKeyName, setNewKeyName] = useState('');
  const [newKeyValue, setNewKeyValue] = useState('');
  const [showNewKey, setShowNewKey] = useState(false);
  const [expandedKey, setExpandedKey] = useState<string | null>(null);
  const queryClient = useQueryClient();

  const keysQuery = useQuery({
    queryKey: ['cache-keys', stackId, searchPattern],
    queryFn: () => listKeys(stackId, searchPattern),
    refetchInterval: 10000,
  });

  const statsQuery = useQuery({
    queryKey: ['cache-stats', stackId],
    queryFn: () => getCacheStats(stackId),
    refetchInterval: 5000,
  });

  const valueQuery = useQuery({
    queryKey: ['cache-value', stackId, expandedKey],
    queryFn: () => getKey(stackId, expandedKey!),
    enabled: !!expandedKey,
  });

  const setValueMutation = useMutation({
    mutationFn: ({ key, value }: { key: string; value: string }) =>
      setKey(stackId, key, value),
    onSuccess: (_, { key }) => {
      queryClient.invalidateQueries({ queryKey: ['cache-keys', stackId] });
      queryClient.invalidateQueries({ queryKey: ['cache-value', stackId, key] });
      setEditingKey(null);
      setShowNewKey(false);
      setNewKeyName('');
      setNewKeyValue('');
      toast.success('Value saved');
    },
    onError: (error) => {
      toast.error('Failed to save value', {
        description: error instanceof Error ? error.message : 'Unknown error',
      });
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (key: string) => deleteKey(stackId, key),
    onSuccess: (_, key) => {
      queryClient.invalidateQueries({ queryKey: ['cache-keys', stackId] });
      if (expandedKey === key) setExpandedKey(null);
      toast.success('Key deleted');
    },
    onError: (error) => {
      toast.error('Failed to delete key', {
        description: error instanceof Error ? error.message : 'Unknown error',
      });
    },
  });

  const flushMutation = useMutation({
    mutationFn: () => bulkDeleteKeys(stackId, '*'),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['cache-keys', stackId] });
      queryClient.invalidateQueries({ queryKey: ['cache-stats', stackId] });
      setExpandedKey(null);
      toast.success('Cache flushed');
    },
    onError: (error) => {
      toast.error('Failed to flush cache', {
        description: error instanceof Error ? error.message : 'Unknown error',
      });
    },
  });

  const formatTTL = (ttl: number): string => {
    if (ttl === -1) return 'No expiry';
    if (ttl === -2) return 'Expired';
    if (ttl < 60) return `${ttl}s`;
    if (ttl < 3600) return `${Math.floor(ttl / 60)}m`;
    if (ttl < 86400) return `${Math.floor(ttl / 3600)}h`;
    return `${Math.floor(ttl / 86400)}d`;
  };

  const formatBytes = (bytes: number): string => {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold">Cache Browser</h2>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => keysQuery.refetch()}
            disabled={keysQuery.isFetching}
          >
            <RefreshCw className={cn("h-4 w-4 mr-2", keysQuery.isFetching && "animate-spin")} />
            Refresh
          </Button>
          <Button
            variant="error"
            size="sm"
            onClick={() => flushMutation.mutate()}
            disabled={flushMutation.isPending}
          >
            <Trash2 className="h-4 w-4 mr-2" />
            Flush All
          </Button>
        </div>
      </div>

      {/* Stats Bar */}
      {statsQuery.data && (
        <div className="grid grid-cols-4 gap-4">
          <StatsCard
            label="Total Keys"
            value={statsQuery.data.keys_count.toLocaleString()}
          />
          <StatsCard
            label="Memory Used"
            value={formatBytes(statsQuery.data.memory_used)}
          />
          <StatsCard
            label="Hit Rate"
            value={`${(statsQuery.data.hit_rate * 100).toFixed(1)}%`}
          />
          <StatsCard
            label="Connected Clients"
            value={statsQuery.data.connected_clients.toString()}
          />
        </div>
      )}

      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        {/* Key List */}
        <div className="card p-4">
          <div className="mb-3">
            <div className="flex items-center justify-between mb-2">
              <h3 className="font-medium">Keys</h3>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => setShowNewKey(true)}
              >
                <Plus className="h-4 w-4" />
              </Button>
            </div>

            {/* Search */}
            <div className="relative">
              <Search className="absolute left-2 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
              <input
                type="text"
                value={searchPattern}
                onChange={(e) => setSearchPattern(e.target.value || '*')}
                placeholder="Pattern (e.g., user:*)"
                className="w-full pl-8 pr-3 py-1.5 text-sm border rounded bg-background"
              />
            </div>
          </div>

          {/* New Key Form */}
          {showNewKey && (
            <div className="mb-3 p-2 border rounded bg-muted/30 space-y-2">
              <input
                type="text"
                value={newKeyName}
                onChange={(e) => setNewKeyName(e.target.value)}
                placeholder="Key name"
                className="w-full px-2 py-1 text-sm border rounded bg-background"
              />
              <textarea
                value={newKeyValue}
                onChange={(e) => setNewKeyValue(e.target.value)}
                placeholder="Value"
                rows={2}
                className="w-full px-2 py-1 text-sm border rounded bg-background resize-none"
              />
              <div className="flex gap-1">
                <Button
                  size="sm"
                  onClick={() => setValueMutation.mutate({ key: newKeyName, value: newKeyValue })}
                  disabled={!newKeyName || setValueMutation.isPending}
                >
                  <Save className="h-4 w-4 mr-1" />
                  Save
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => {
                    setShowNewKey(false);
                    setNewKeyName('');
                    setNewKeyValue('');
                  }}
                >
                  <X className="h-4 w-4" />
                </Button>
              </div>
            </div>
          )}

          {keysQuery.isLoading ? (
            <div className="space-y-2">
              {[1, 2, 3, 4, 5].map((i) => (
                <Skeleton key={i} className="h-10 w-full" />
              ))}
            </div>
          ) : keysQuery.error ? (
            <p className="text-sm text-destructive">Failed to load keys</p>
          ) : (
            <ul className="space-y-1 max-h-[400px] overflow-auto">
              {keysQuery.data?.keys.map((key: CacheKey) => (
                <li key={key.key}>
                  <button
                    onClick={() => setExpandedKey(expandedKey === key.key ? null : key.key)}
                    className={cn(
                      "w-full text-left px-2 py-1.5 rounded text-sm",
                      expandedKey === key.key
                        ? "bg-primary text-primary-foreground"
                        : "hover:bg-muted"
                    )}
                  >
                    <div className="flex items-center justify-between">
                      <span className="truncate font-mono text-xs">{key.key}</span>
                      <span className={cn("text-xs px-1.5 py-0.5 rounded", TYPE_COLORS[key.type])}>
                        {key.type}
                      </span>
                    </div>
                    <div className="flex items-center justify-between mt-1 text-xs opacity-70">
                      <span>{formatBytes(key.size)}</span>
                      <span className="flex items-center gap-1">
                        <Clock className="h-3 w-3" />
                        {formatTTL(key.ttl)}
                      </span>
                    </div>
                  </button>
                </li>
              ))}
              {keysQuery.data?.keys.length === 0 && (
                <li className="text-sm text-muted-foreground px-2 py-4 text-center">
                  No keys found
                </li>
              )}
            </ul>
          )}
        </div>

        {/* Value Panel */}
        <div className="md:col-span-2 card p-4">
          {expandedKey ? (
            <>
              <div className="flex items-center justify-between mb-4">
                <h3 className="font-mono text-sm truncate flex-1">{expandedKey}</h3>
                <div className="flex gap-2">
                  {editingKey !== expandedKey && (
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => {
                        setEditingKey(expandedKey);
                        setEditValue(valueQuery.data?.value || '');
                      }}
                    >
                      <Edit className="h-4 w-4 mr-1" />
                      Edit
                    </Button>
                  )}
                  <Button
                    size="sm"
                    variant="error"
                    onClick={() => deleteMutation.mutate(expandedKey)}
                    disabled={deleteMutation.isPending}
                  >
                    <Trash2 className="h-4 w-4 mr-1" />
                    Delete
                  </Button>
                </div>
              </div>

              {valueQuery.isLoading ? (
                <Skeleton className="h-48 w-full" />
              ) : valueQuery.error ? (
                <p className="text-destructive">Failed to load value</p>
              ) : editingKey === expandedKey ? (
                <div className="space-y-3">
                  <textarea
                    value={editValue}
                    onChange={(e) => setEditValue(e.target.value)}
                    rows={10}
                    className="w-full px-3 py-2 text-sm font-mono border rounded bg-background resize-none"
                  />
                  <div className="flex gap-2">
                    <Button
                      size="sm"
                      onClick={() => setValueMutation.mutate({ key: expandedKey, value: editValue })}
                      disabled={setValueMutation.isPending}
                    >
                      <Save className="h-4 w-4 mr-1" />
                      Save
                    </Button>
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => setEditingKey(null)}
                    >
                      Cancel
                    </Button>
                  </div>
                </div>
              ) : (
                <pre className="text-sm font-mono bg-muted p-3 rounded overflow-auto max-h-[400px] whitespace-pre-wrap">
                  {formatValue(valueQuery.data?.value)}
                </pre>
              )}

              {valueQuery.data && (
                <div className="mt-4 flex gap-4 text-xs text-muted-foreground">
                  <span>Type: {valueQuery.data.type}</span>
                  <span>Size: {formatBytes(valueQuery.data.size)}</span>
                  <span>TTL: {formatTTL(valueQuery.data.ttl)}</span>
                </div>
              )}
            </>
          ) : (
            <div className="flex items-center justify-center py-12 text-muted-foreground">
              <Database className="h-8 w-8 mr-2" />
              <span>Select a key to view its value</span>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

function StatsCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="card-stat">
      <p className="card-stat-label">{label}</p>
      <p className="card-stat-value text-xl">{value}</p>
    </div>
  );
}

function formatValue(value: string | undefined): string {
  if (!value) return '';
  try {
    const parsed = JSON.parse(value);
    return JSON.stringify(parsed, null, 2);
  } catch {
    return value;
  }
}
