import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { cn } from '@/lib/utils';
import {
  RefreshCw,
  Plus,
  Trash2,
  Loader2,
  Layers,
  Play,
  Square,
  RotateCcw,
  Pencil,
  X,
  Check,
  Clock,
  AlertCircle,
  FileCode,
  Terminal,
  Rocket,
  MapPin,
} from 'lucide-react';
import {
  listStacks,
  createStack,
  updateStack,
  deleteStack,
  startStack,
  stopStack,
  restartStack,
  getStackLogs,
  getStatusColor,
  getStatusLabel,
  getRunningServicesCount,
  formatCost,
  providerDisplayNames,
  type Stack,
  type CreateStackRequest,
  type UpdateStackRequest,
} from '@/lib/stacks-api';

interface StackManagerProps {
  onStackSelect?: (stackId: string) => void;
  onError?: (error: Error) => void;
}

export function StackManager({ onStackSelect, onError }: StackManagerProps) {
  const queryClient = useQueryClient();

  // Stack form state
  const [showStackForm, setShowStackForm] = useState(false);
  const [editingStack, setEditingStack] = useState<Stack | null>(null);
  const [stackFormData, setStackFormData] = useState({
    name: '',
    description: '',
    composeFile: '',
    envVars: [] as { key: string; value: string }[],
  });

  // Logs dialog state
  const [showLogs, setShowLogs] = useState<{ stack: Stack; service?: string } | null>(null);
  const [logs, setLogs] = useState<string>('');

  const [activeAction, setActiveAction] = useState<{ id: string; action: string } | null>(null);

  // Queries
  const stacksQuery = useQuery({
    queryKey: ['stacks'],
    queryFn: listStacks,
    refetchInterval: 10000, // Poll for status updates
  });

  const clearActiveAction = () => setActiveAction(null);

  // Mutations
  const createStackMutation = useMutation({
    mutationFn: (req: CreateStackRequest) => createStack(req),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['stacks'] });
      resetStackForm();
    },
    onError: (err: Error) => onError?.(err),
  });

  const updateStackMutation = useMutation({
    mutationFn: ({ id, req }: { id: string; req: UpdateStackRequest }) => updateStack(id, req),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['stacks'] });
      resetStackForm();
    },
    onError: (err: Error) => onError?.(err),
  });

  const deleteStackMutation = useMutation({
    mutationFn: (id: string) => deleteStack(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['stacks'] });
      clearActiveAction();
    },
    onError: (err: Error) => {
      clearActiveAction();
      onError?.(err);
    },
  });

  const startStackMutation = useMutation({
    mutationFn: (id: string) => startStack(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['stacks'] });
      clearActiveAction();
    },
    onError: (err: Error) => {
      clearActiveAction();
      onError?.(err);
    },
  });

  const stopStackMutation = useMutation({
    mutationFn: (id: string) => stopStack(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['stacks'] });
      clearActiveAction();
    },
    onError: (err: Error) => {
      clearActiveAction();
      onError?.(err);
    },
  });

  const restartStackMutation = useMutation({
    mutationFn: (id: string) => restartStack(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['stacks'] });
      clearActiveAction();
    },
    onError: (err: Error) => {
      clearActiveAction();
      onError?.(err);
    },
  });

  const resetStackForm = () => {
    setShowStackForm(false);
    setEditingStack(null);
    setStackFormData({
      name: '',
      description: '',
      composeFile: '',
      envVars: [],
    });
  };

  const handleEditStack = (stack: Stack) => {
    setEditingStack(stack);
    const envVars = stack.env_vars
      ? Object.entries(stack.env_vars).map(([key, value]) => ({ key, value }))
      : [];
    setStackFormData({
      name: stack.name,
      description: stack.description || '',
      composeFile: stack.compose_file,
      envVars,
    });
    setShowStackForm(true);
  };

  const handleStackFormSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!stackFormData.name.trim() || !stackFormData.composeFile.trim()) return;

    // Convert env vars array to object
    const envVarsObj: Record<string, string> = {};
    stackFormData.envVars.forEach(({ key, value }) => {
      if (key.trim()) {
        envVarsObj[key.trim()] = value;
      }
    });

    if (editingStack) {
      updateStackMutation.mutate({
        id: editingStack.id,
        req: {
          name: stackFormData.name,
          description: stackFormData.description,
          compose_file: stackFormData.composeFile,
          env_vars: Object.keys(envVarsObj).length > 0 ? envVarsObj : undefined,
        },
      });
    } else {
      createStackMutation.mutate({
        name: stackFormData.name,
        description: stackFormData.description,
        compose_file: stackFormData.composeFile,
        env_vars: Object.keys(envVarsObj).length > 0 ? envVarsObj : undefined,
      });
    }
  };

  const addEnvVar = () => {
    setStackFormData((prev) => ({
      ...prev,
      envVars: [...prev.envVars, { key: '', value: '' }],
    }));
  };

  const removeEnvVar = (index: number) => {
    setStackFormData((prev) => ({
      ...prev,
      envVars: prev.envVars.filter((_, i) => i !== index),
    }));
  };

  const updateEnvVar = (index: number, field: 'key' | 'value', value: string) => {
    setStackFormData((prev) => ({
      ...prev,
      envVars: prev.envVars.map((ev, i) =>
        i === index ? { ...ev, [field]: value } : ev
      ),
    }));
  };

  const handleShowLogs = async (stack: Stack, service?: string) => {
    setShowLogs({ stack, service });
    try {
      const response = await getStackLogs(stack.id, service, 200);
      setLogs(response.logs);
    } catch (err) {
      setLogs('Failed to fetch logs');
    }
  };

  const isLoading = stacksQuery.isLoading;
  const error = stacksQuery.error;

  if (isLoading) {
    return (
      <div className="space-y-4 rounded-lg border p-4">
        <div className="flex items-center justify-between">
          <Skeleton className="h-6 w-48" />
          <Skeleton className="h-9 w-24" />
        </div>
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {[1, 2, 3].map((i) => (
            <div key={i} className="rounded-lg border p-4">
              <div className="space-y-2">
                <Skeleton className="h-4 w-32" />
                <Skeleton className="h-3 w-24" />
                <Skeleton className="h-8 w-full mt-4" />
              </div>
            </div>
          ))}
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="rounded-lg border p-4">
        <div className="text-error">
          Error loading stacks data. Stacks service may not be available.
        </div>
      </div>
    );
  }

  const stacks = stacksQuery.data?.stacks || [];

  return (
    <div className="space-y-4 rounded-lg border p-4">
      {/* Header */}
      <div className="flex items-center justify-between flex-wrap gap-2">
        <div className="flex items-center gap-2">
          <Layers className="h-5 w-5 text-primary" />
          <h2 className="text-lg font-semibold">Stack Management</h2>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={() => stacksQuery.refetch()}>
            <RefreshCw className="h-4 w-4 mr-2" />
            Refresh
          </Button>
          <Button
            size="sm"
            onClick={() => {
              resetStackForm();
              setShowStackForm(true);
            }}
          >
            <Plus className="h-4 w-4 mr-2" />
            New Stack
          </Button>
        </div>
      </div>

      {/* Stack Form */}
      {showStackForm && (
        <div className="rounded-lg border p-4 bg-muted/50">
          <div className="flex items-center justify-between mb-4">
            <h3 className="font-medium">
              {editingStack ? 'Edit Stack' : 'Create New Stack'}
            </h3>
            <Button variant="ghost" size="sm" onClick={resetStackForm}>
              <X className="h-4 w-4" />
            </Button>
          </div>
          <form onSubmit={handleStackFormSubmit} className="space-y-4">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div>
                <label className="block text-sm font-medium mb-1">Stack Name *</label>
                <input
                  type="text"
                  value={stackFormData.name}
                  onChange={(e) =>
                    setStackFormData((prev) => ({ ...prev, name: e.target.value }))
                  }
                  placeholder="my-stack"
                  className="w-full px-3 py-2 rounded-md border bg-background"
                  required
                />
              </div>
              <div>
                <label className="block text-sm font-medium mb-1">Description</label>
                <input
                  type="text"
                  value={stackFormData.description}
                  onChange={(e) =>
                    setStackFormData((prev) => ({ ...prev, description: e.target.value }))
                  }
                  placeholder="Optional description"
                  className="w-full px-3 py-2 rounded-md border bg-background"
                />
              </div>
            </div>

            <div>
              <label className="block text-sm font-medium mb-1">
                Docker Compose File *
              </label>
              <textarea
                value={stackFormData.composeFile}
                onChange={(e) =>
                  setStackFormData((prev) => ({ ...prev, composeFile: e.target.value }))
                }
                placeholder={`version: '3.8'\nservices:\n  web:\n    image: nginx:latest\n    ports:\n      - "80:80"`}
                className="w-full px-3 py-2 rounded-md border bg-background font-mono text-sm h-48"
                required
              />
            </div>

            <div>
              <div className="flex items-center justify-between mb-2">
                <label className="text-sm font-medium">Environment Variables</label>
                <Button type="button" variant="outline" size="sm" onClick={addEnvVar}>
                  <Plus className="h-3 w-3 mr-1" />
                  Add
                </Button>
              </div>
              {stackFormData.envVars.length > 0 && (
                <div className="space-y-2">
                  {stackFormData.envVars.map((env, idx) => (
                    <div key={idx} className="flex gap-2">
                      <input
                        type="text"
                        value={env.key}
                        onChange={(e) => updateEnvVar(idx, 'key', e.target.value)}
                        placeholder="KEY"
                        className="flex-1 px-3 py-2 rounded-md border bg-background text-sm"
                      />
                      <input
                        type="text"
                        value={env.value}
                        onChange={(e) => updateEnvVar(idx, 'value', e.target.value)}
                        placeholder="value"
                        className="flex-1 px-3 py-2 rounded-md border bg-background text-sm"
                      />
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        onClick={() => removeEnvVar(idx)}
                      >
                        <X className="h-4 w-4" />
                      </Button>
                    </div>
                  ))}
                </div>
              )}
            </div>

            <div className="flex items-center gap-2">
              <Button
                type="submit"
                disabled={
                  createStackMutation.isPending ||
                  updateStackMutation.isPending ||
                  !stackFormData.name.trim() ||
                  !stackFormData.composeFile.trim()
                }
              >
                {createStackMutation.isPending || updateStackMutation.isPending ? (
                  <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                ) : (
                  <Check className="h-4 w-4 mr-2" />
                )}
                {editingStack ? 'Update Stack' : 'Create Stack'}
              </Button>
              <Button type="button" variant="outline" onClick={resetStackForm}>
                Cancel
              </Button>
            </div>
            {(createStackMutation.error || updateStackMutation.error) && (
              <p className="text-sm text-error">
                {((createStackMutation.error || updateStackMutation.error) as Error).message}
              </p>
            )}
          </form>
        </div>
      )}

      {/* Logs Dialog */}
      {showLogs && (
        <div className="rounded-lg border p-4 bg-muted/50">
          <div className="flex items-center justify-between mb-4">
            <h3 className="font-medium flex items-center gap-2">
              <Terminal className="h-4 w-4" />
              Logs: {showLogs.stack.name}
              {showLogs.service && ` / ${showLogs.service}`}
            </h3>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => {
                setShowLogs(null);
                setLogs('');
              }}
            >
              <X className="h-4 w-4" />
            </Button>
          </div>
          <pre className="bg-black text-green-400 p-4 rounded-lg overflow-auto max-h-96 text-xs font-mono">
            {logs || 'No logs available'}
          </pre>
        </div>
      )}

      {/* Stack List */}
      {stacks.length === 0 ? (
        <div className="text-center py-8 text-muted-foreground border rounded-lg">
          <Layers className="h-12 w-12 mx-auto mb-2 opacity-50" />
          <p>No stacks found</p>
          <p className="text-sm mt-1">Click "New Stack" to create a stack</p>
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {stacks.map((stack) => {
            const { running, total } = getRunningServicesCount(stack);
            const isTransitioning =
              stack.status === 'starting' || stack.status === 'stopping';

            return (
              <div
                key={stack.id}
                className="rounded-lg border p-4 hover:border-blue-300 transition-colors cursor-pointer"
                onClick={() => onStackSelect?.(stack.id)}
              >
                {/* Stack Header */}
                <div className="flex items-start justify-between mb-3">
                  <div>
                    <h3 className="font-medium flex items-center gap-2">
                      <Layers className="h-4 w-4 text-primary" />
                      {stack.name}
                      {stack.is_pending && (
                        <span className="badge-warning text-xs">Pending</span>
                      )}
                    </h3>
                    {stack.description && (
                      <p className="text-xs text-muted-foreground mt-1">
                        {stack.description}
                      </p>
                    )}
                  </div>
                  <span
                    className={cn(
                      'inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs font-medium',
                      getStatusColor(stack.status)
                    )}
                  >
                    {isTransitioning && <Loader2 className="h-3 w-3 animate-spin" />}
                    {stack.status === 'error' && <AlertCircle className="h-3 w-3" />}
                    {stack.status === 'running' && <Check className="h-3 w-3" />}
                    {getStatusLabel(stack.status)}
                  </span>
                </div>

                {/* Deployment Config Info (for pending stacks) */}
                {stack.is_pending && stack.deployment_config && (
                  <div className="flex items-center gap-2 mb-3 p-2 rounded-md bg-warning/10 border border-warning/20">
                    <Rocket className="h-4 w-4 text-warning" />
                    <div className="flex-1 text-xs">
                      <div className="flex items-center gap-2">
                        <span className="font-medium">
                          {providerDisplayNames[stack.deployment_config.provider] || stack.deployment_config.provider}
                        </span>
                        {stack.deployment_config.region && (
                          <span className="flex items-center gap-1 text-muted-foreground">
                            <MapPin className="h-3 w-3" />
                            {stack.deployment_config.region}
                          </span>
                        )}
                      </div>
                      {stack.deployment_config.estimated_cost && (
                        <span className="text-success font-medium">
                          {formatCost(stack.deployment_config.estimated_cost)}
                        </span>
                      )}
                    </div>
                  </div>
                )}

                {/* Services info */}
                <div className="text-sm text-muted-foreground mb-3">
                  <span className="flex items-center gap-1">
                    <FileCode className="h-3 w-3" />
                    {running}/{total} services running
                  </span>
                </div>

                {/* Error message */}
                {stack.error && (
                  <p className="text-xs text-error mb-3 line-clamp-2">{stack.error}</p>
                )}

                {/* Last started/stopped */}
                <div className="text-xs text-muted-foreground mb-3">
                  {stack.last_started_at && (
                    <span className="flex items-center gap-1">
                      <Clock className="h-3 w-3" />
                      Started: {new Date(stack.last_started_at).toLocaleString()}
                    </span>
                  )}
                </div>

                {/* Actions */}
                <div
                  className="flex items-center gap-1 pt-3 border-t"
                  onClick={(e) => e.stopPropagation()}
                >
                  {stack.status === 'stopped' || stack.status === 'error' ? (
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => {
                        setActiveAction({ id: stack.id, action: 'start' });
                        startStackMutation.mutate(stack.id);
                      }}
                      disabled={isTransitioning}
                    >
                      {activeAction?.id === stack.id && activeAction.action === 'start' ? (
                        <Loader2 className="h-4 w-4 animate-spin" />
                      ) : (
                        <Play className="h-4 w-4" />
                      )}
                    </Button>
                  ) : (
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => {
                        setActiveAction({ id: stack.id, action: 'stop' });
                        stopStackMutation.mutate(stack.id);
                      }}
                      disabled={isTransitioning}
                    >
                      {activeAction?.id === stack.id && activeAction.action === 'stop' ? (
                        <Loader2 className="h-4 w-4 animate-spin" />
                      ) : (
                        <Square className="h-4 w-4" />
                      )}
                    </Button>
                  )}
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => {
                      setActiveAction({ id: stack.id, action: 'restart' });
                      restartStackMutation.mutate(stack.id);
                    }}
                    disabled={isTransitioning || stack.status === 'stopped'}
                    title="Restart"
                  >
                    {activeAction?.id === stack.id && activeAction.action === 'restart' ? (
                      <Loader2 className="h-4 w-4 animate-spin" />
                    ) : (
                      <RotateCcw className="h-4 w-4" />
                    )}
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => handleShowLogs(stack)}
                    title="View logs"
                  >
                    <Terminal className="h-4 w-4" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => handleEditStack(stack)}
                    disabled={isTransitioning}
                    title="Edit"
                  >
                    <Pencil className="h-4 w-4" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => {
                      if (confirm(`Delete stack ${stack.name}?`)) {
                        setActiveAction({ id: stack.id, action: 'delete' });
                        deleteStackMutation.mutate(stack.id);
                      }
                    }}
                    disabled={
                      deleteStackMutation.isPending ||
                      stack.status === 'running' ||
                      stack.status === 'partial' ||
                      isTransitioning
                    }
                    title="Delete"
                  >
                    {activeAction?.id === stack.id && activeAction.action === 'delete' ? (
                      <Loader2 className="h-4 w-4 animate-spin" />
                    ) : (
                      <Trash2 className="h-4 w-4" />
                    )}
                  </Button>
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
