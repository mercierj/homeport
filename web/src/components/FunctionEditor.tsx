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
  Zap,
  Play,
  X,
  ChevronLeft,
  Clock,
  Activity,
  Terminal,
  Code,
  Settings,
  AlertCircle,
  CheckCircle,
  Info,
  AlertTriangle,
} from 'lucide-react';
import {
  listFunctions,
  createFunction,
  updateFunction,
  deleteFunction,
  invokeFunction,
  getFunctionLogs,
  getStatusBadgeClass,
  getRuntimeIcon,
  formatDuration,
  AVAILABLE_RUNTIMES,
  MEMORY_OPTIONS,
  TIMEOUT_OPTIONS,
  type FunctionInfo,
  type FunctionConfig,
  type FunctionRuntime,
  type InvocationResult,
  type FunctionLog,
} from '@/lib/functions-api';

interface FunctionEditorProps {
  onError?: (error: Error) => void;
  onSuccess?: (message: string) => void;
}

type ViewMode = 'list' | 'create' | 'edit' | 'details';

const logLevelIcons: Record<FunctionLog['level'], typeof Info> = {
  info: Info,
  warn: AlertTriangle,
  error: AlertCircle,
  debug: Terminal,
};

// Design system log level colors
const logLevelColors: Record<FunctionLog['level'], string> = {
  info: 'text-info',
  warn: 'text-warning',
  error: 'text-error',
  debug: 'text-muted-foreground',
};

export function FunctionEditor({ onError, onSuccess }: FunctionEditorProps) {
  const queryClient = useQueryClient();
  const [viewMode, setViewMode] = useState<ViewMode>('list');
  const [selectedFunction, setSelectedFunction] = useState<FunctionInfo | null>(null);
  const [activeTab, setActiveTab] = useState<'code' | 'settings' | 'logs'>('code');

  // Form state
  const [formName, setFormName] = useState('');
  const [formRuntime, setFormRuntime] = useState<FunctionRuntime>('nodejs20');
  const [formHandler, setFormHandler] = useState('index.handler');
  const [formMemory, setFormMemory] = useState(256);
  const [formTimeout, setFormTimeout] = useState(30);
  const [formCode, setFormCode] = useState('');
  const [formEnvVars, setFormEnvVars] = useState<{ key: string; value: string }[]>([]);

  // Invoke modal state
  const [showInvokeModal, setShowInvokeModal] = useState(false);
  const [invokePayload, setInvokePayload] = useState('{}');
  const [invokeResult, setInvokeResult] = useState<InvocationResult | null>(null);

  // Delete confirmation state
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false);
  const [functionToDelete, setFunctionToDelete] = useState<FunctionInfo | null>(null);

  // Queries
  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ['functions'],
    queryFn: listFunctions,
    refetchInterval: 30000,
    refetchIntervalInBackground: false,
  });

  const { data: logsData, isLoading: logsLoading } = useQuery({
    queryKey: ['function-logs', selectedFunction?.id],
    queryFn: () => (selectedFunction ? getFunctionLogs(selectedFunction.id) : Promise.resolve({ logs: [], count: 0 })),
    enabled: !!selectedFunction && activeTab === 'logs',
    refetchInterval: 5000,
  });

  // Mutations
  const createMutation = useMutation({
    mutationFn: (config: FunctionConfig) => createFunction(config),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['functions'] });
      resetForm();
      setViewMode('list');
      onSuccess?.('Function created successfully');
    },
    onError: (err: Error) => {
      onError?.(err);
    },
  });

  const updateMutation = useMutation({
    mutationFn: ({ id, config }: { id: string; config: Partial<FunctionConfig> }) =>
      updateFunction(id, config),
    onSuccess: (updatedFunc) => {
      queryClient.invalidateQueries({ queryKey: ['functions'] });
      setSelectedFunction(updatedFunc);
      onSuccess?.('Function updated successfully');
    },
    onError: (err: Error) => {
      onError?.(err);
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => deleteFunction(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['functions'] });
      setShowDeleteConfirm(false);
      setFunctionToDelete(null);
      if (viewMode === 'details' || viewMode === 'edit') {
        setViewMode('list');
        setSelectedFunction(null);
      }
      onSuccess?.('Function deleted successfully');
    },
    onError: (err: Error) => {
      onError?.(err);
    },
  });

  const invokeMutation = useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: unknown }) =>
      invokeFunction(id, payload),
    onSuccess: (result) => {
      setInvokeResult(result);
      queryClient.invalidateQueries({ queryKey: ['functions'] });
    },
    onError: (err: Error) => {
      onError?.(err);
    },
  });

  const resetForm = () => {
    setFormName('');
    setFormRuntime('nodejs20');
    setFormHandler('index.handler');
    setFormMemory(256);
    setFormTimeout(30);
    setFormCode('');
    setFormEnvVars([]);
  };

  const populateFormFromFunction = (func: FunctionInfo) => {
    setFormName(func.name);
    setFormRuntime(func.runtime);
    setFormHandler(func.handler);
    setFormMemory(func.memory);
    setFormTimeout(func.timeout);
    setFormCode(func.code || '');
    setFormEnvVars(
      Object.entries(func.environment || {}).map(([key, value]) => ({ key, value }))
    );
  };

  const handleCreate = () => {
    resetForm();
    setViewMode('create');
  };

  const handleEdit = (func: FunctionInfo) => {
    setSelectedFunction(func);
    populateFormFromFunction(func);
    setViewMode('edit');
  };

  const handleDetails = (func: FunctionInfo) => {
    setSelectedFunction(func);
    populateFormFromFunction(func);
    setActiveTab('code');
    setViewMode('details');
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!formName.trim()) return;

    const envObj: Record<string, string> = {};
    formEnvVars.forEach(({ key, value }) => {
      if (key.trim()) {
        envObj[key.trim()] = value;
      }
    });

    const config: FunctionConfig = {
      name: formName.trim(),
      runtime: formRuntime,
      handler: formHandler.trim(),
      memory: formMemory,
      timeout: formTimeout,
      code: formCode,
      environment: Object.keys(envObj).length > 0 ? envObj : undefined,
    };

    if (viewMode === 'create') {
      createMutation.mutate(config);
    } else if (viewMode === 'edit' && selectedFunction) {
      updateMutation.mutate({ id: selectedFunction.id, config });
    }
  };

  const handleInvoke = (func: FunctionInfo) => {
    setSelectedFunction(func);
    setInvokePayload('{}');
    setInvokeResult(null);
    setShowInvokeModal(true);
  };

  const handleInvokeSubmit = () => {
    if (!selectedFunction) return;
    try {
      const payload = JSON.parse(invokePayload);
      invokeMutation.mutate({ id: selectedFunction.id, payload });
    } catch {
      onError?.(new Error('Invalid JSON payload'));
    }
  };

  const handleDeleteConfirm = (func: FunctionInfo) => {
    setFunctionToDelete(func);
    setShowDeleteConfirm(true);
  };

  const handleDeleteExecute = () => {
    if (functionToDelete) {
      deleteMutation.mutate(functionToDelete.id);
    }
  };

  const addEnvVar = () => {
    setFormEnvVars([...formEnvVars, { key: '', value: '' }]);
  };

  const removeEnvVar = (index: number) => {
    setFormEnvVars(formEnvVars.filter((_, i) => i !== index));
  };

  const updateEnvVar = (index: number, field: 'key' | 'value', value: string) => {
    const updated = [...formEnvVars];
    updated[index][field] = value;
    setFormEnvVars(updated);
  };

  // Loading state
  if (isLoading) {
    return (
      <div className="space-y-4 rounded-lg border p-4">
        <div className="flex items-center justify-between">
          <Skeleton className="h-6 w-48" />
          <Skeleton className="h-9 w-24" />
        </div>
        <div className="space-y-2">
          {[1, 2, 3].map((i) => (
            <div key={i} className="flex items-center justify-between p-4 rounded-lg border">
              <div className="flex items-center gap-4">
                <Skeleton className="h-8 w-8 rounded" />
                <div className="space-y-2">
                  <Skeleton className="h-4 w-40" />
                  <Skeleton className="h-3 w-32" />
                </div>
              </div>
              <div className="flex items-center gap-2">
                <Skeleton className="h-8 w-8" />
                <Skeleton className="h-8 w-8" />
              </div>
            </div>
          ))}
        </div>
      </div>
    );
  }

  // Error state
  if (error) {
    return (
      <div className="card p-4">
        <div className="text-error">
          Error loading functions. Function manager may not be configured.
        </div>
      </div>
    );
  }

  const functions = data?.functions || [];

  // Form view (create/edit)
  if (viewMode === 'create' || viewMode === 'edit') {
    return (
      <div className="card p-4 space-y-4">
        <div className="flex items-center gap-2">
          <Button variant="ghost" size="sm" onClick={() => setViewMode('list')}>
            <ChevronLeft className="h-4 w-4 mr-1" />
            Back
          </Button>
          <h2 className="text-lg font-semibold">
            {viewMode === 'create' ? 'Create Function' : 'Edit Function'}
          </h2>
        </div>

        <form onSubmit={handleSubmit} className="space-y-6">
          {/* Basic Info */}
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium mb-1">Name *</label>
              <input
                type="text"
                value={formName}
                onChange={(e) => setFormName(e.target.value)}
                placeholder="my-function"
                className="w-full px-3 py-2 rounded-md border bg-background"
                required
                disabled={viewMode === 'edit'}
              />
            </div>
            <div>
              <label className="block text-sm font-medium mb-1">Runtime *</label>
              <select
                value={formRuntime}
                onChange={(e) => setFormRuntime(e.target.value as FunctionRuntime)}
                className="w-full px-3 py-2 rounded-md border bg-background"
              >
                {AVAILABLE_RUNTIMES.map((rt) => (
                  <option key={rt.value} value={rt.value}>
                    {rt.label}
                  </option>
                ))}
              </select>
            </div>
          </div>

          <div>
            <label className="block text-sm font-medium mb-1">Handler *</label>
            <input
              type="text"
              value={formHandler}
              onChange={(e) => setFormHandler(e.target.value)}
              placeholder="index.handler"
              className="w-full px-3 py-2 rounded-md border bg-background"
              required
            />
            <p className="text-xs text-muted-foreground mt-1">
              Format: filename.functionName (e.g., index.handler)
            </p>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium mb-1">Memory (MB)</label>
              <select
                value={formMemory}
                onChange={(e) => setFormMemory(Number(e.target.value))}
                className="w-full px-3 py-2 rounded-md border bg-background"
              >
                {MEMORY_OPTIONS.map((mem) => (
                  <option key={mem} value={mem}>
                    {mem} MB
                  </option>
                ))}
              </select>
            </div>
            <div>
              <label className="block text-sm font-medium mb-1">Timeout (seconds)</label>
              <select
                value={formTimeout}
                onChange={(e) => setFormTimeout(Number(e.target.value))}
                className="w-full px-3 py-2 rounded-md border bg-background"
              >
                {TIMEOUT_OPTIONS.map((t) => (
                  <option key={t} value={t}>
                    {t}s
                  </option>
                ))}
              </select>
            </div>
          </div>

          {/* Code Editor */}
          <div>
            <label className="block text-sm font-medium mb-1">Code</label>
            <textarea
              value={formCode}
              onChange={(e) => setFormCode(e.target.value)}
              placeholder="// Your function code here"
              className="w-full px-3 py-2 rounded-md border bg-background font-mono text-sm min-h-[200px]"
              spellCheck={false}
            />
          </div>

          {/* Environment Variables */}
          <div>
            <div className="flex items-center justify-between mb-2">
              <label className="block text-sm font-medium">Environment Variables</label>
              <Button type="button" variant="outline" size="sm" onClick={addEnvVar}>
                <Plus className="h-4 w-4 mr-1" />
                Add Variable
              </Button>
            </div>
            {formEnvVars.length === 0 ? (
              <p className="text-sm text-muted-foreground">No environment variables configured</p>
            ) : (
              <div className="space-y-2">
                {formEnvVars.map((envVar, index) => (
                  <div key={index} className="flex items-center gap-2">
                    <input
                      type="text"
                      value={envVar.key}
                      onChange={(e) => updateEnvVar(index, 'key', e.target.value)}
                      placeholder="KEY"
                      className="flex-1 px-3 py-2 rounded-md border bg-background font-mono text-sm"
                    />
                    <input
                      type="text"
                      value={envVar.value}
                      onChange={(e) => updateEnvVar(index, 'value', e.target.value)}
                      placeholder="value"
                      className="flex-1 px-3 py-2 rounded-md border bg-background font-mono text-sm"
                    />
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      onClick={() => removeEnvVar(index)}
                    >
                      <X className="h-4 w-4" />
                    </Button>
                  </div>
                ))}
              </div>
            )}
          </div>

          {/* Submit */}
          <div className="flex items-center gap-2">
            <Button
              type="submit"
              disabled={createMutation.isPending || updateMutation.isPending || !formName.trim()}
            >
              {(createMutation.isPending || updateMutation.isPending) && (
                <Loader2 className="h-4 w-4 mr-2 animate-spin" />
              )}
              {viewMode === 'create' ? 'Create Function' : 'Update Function'}
            </Button>
            <Button type="button" variant="outline" onClick={() => setViewMode('list')}>
              Cancel
            </Button>
          </div>

          {(createMutation.error || updateMutation.error) && (
            <p className="text-sm text-error">
              {((createMutation.error || updateMutation.error) as Error).message}
            </p>
          )}
        </form>
      </div>
    );
  }

  // Details view
  if (viewMode === 'details' && selectedFunction) {
    const runtimeInfo = getRuntimeIcon(selectedFunction.runtime);

    return (
      <div className="card p-4 space-y-4">
        <div className="flex items-center justify-between flex-wrap gap-2">
          <div className="flex items-center gap-2">
            <Button variant="ghost" size="sm" onClick={() => setViewMode('list')}>
              <ChevronLeft className="h-4 w-4 mr-1" />
              Back
            </Button>
            <h2 className="text-lg font-semibold">{selectedFunction.name}</h2>
            <span
              className={cn(
                'px-2 py-0.5 rounded text-xs font-medium',
                getStatusBadgeClass(selectedFunction.status)
              )}
            >
              {selectedFunction.status}
            </span>
          </div>
          <div className="flex items-center gap-2">
            <Button variant="outline" size="sm" onClick={() => handleInvoke(selectedFunction)}>
              <Play className="h-4 w-4 mr-2" />
              Invoke
            </Button>
            <Button variant="outline" size="sm" onClick={() => handleEdit(selectedFunction)}>
              <Settings className="h-4 w-4 mr-2" />
              Edit
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => handleDeleteConfirm(selectedFunction)}
            >
              <Trash2 className="h-4 w-4" />
            </Button>
          </div>
        </div>

        {/* Function info */}
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4 p-4 bg-muted/50 rounded-lg">
          <div>
            <p className="text-xs text-muted-foreground">Runtime</p>
            <p className="font-medium">{runtimeInfo.label}</p>
          </div>
          <div>
            <p className="text-xs text-muted-foreground">Handler</p>
            <p className="font-medium font-mono text-sm">{selectedFunction.handler}</p>
          </div>
          <div>
            <p className="text-xs text-muted-foreground">Memory</p>
            <p className="font-medium">{selectedFunction.memory} MB</p>
          </div>
          <div>
            <p className="text-xs text-muted-foreground">Timeout</p>
            <p className="font-medium">{selectedFunction.timeout}s</p>
          </div>
          <div>
            <p className="text-xs text-muted-foreground">Invocations</p>
            <p className="font-medium">{selectedFunction.invocation_count}</p>
          </div>
          <div>
            <p className="text-xs text-muted-foreground">Last Invoked</p>
            <p className="font-medium">
              {selectedFunction.last_invoked_at
                ? new Date(selectedFunction.last_invoked_at).toLocaleString()
                : 'Never'}
            </p>
          </div>
        </div>

        {/* Tabs */}
        <div className="border-b">
          <div className="flex gap-4">
            <button
              className={cn(
                'px-4 py-2 text-sm font-medium border-b-2 -mb-px',
                activeTab === 'code'
                  ? 'border-primary text-primary'
                  : 'border-transparent text-muted-foreground hover:text-foreground'
              )}
              onClick={() => setActiveTab('code')}
            >
              <Code className="h-4 w-4 inline mr-2" />
              Code
            </button>
            <button
              className={cn(
                'px-4 py-2 text-sm font-medium border-b-2 -mb-px',
                activeTab === 'settings'
                  ? 'border-primary text-primary'
                  : 'border-transparent text-muted-foreground hover:text-foreground'
              )}
              onClick={() => setActiveTab('settings')}
            >
              <Settings className="h-4 w-4 inline mr-2" />
              Settings
            </button>
            <button
              className={cn(
                'px-4 py-2 text-sm font-medium border-b-2 -mb-px',
                activeTab === 'logs'
                  ? 'border-primary text-primary'
                  : 'border-transparent text-muted-foreground hover:text-foreground'
              )}
              onClick={() => setActiveTab('logs')}
            >
              <Terminal className="h-4 w-4 inline mr-2" />
              Logs
            </button>
          </div>
        </div>

        {/* Tab content */}
        <div className="min-h-[300px]">
          {activeTab === 'code' && (
            <div>
              <pre className="p-4 bg-muted rounded-lg font-mono text-sm overflow-auto max-h-[400px]">
                {selectedFunction.code || '// No code available'}
              </pre>
            </div>
          )}

          {activeTab === 'settings' && (
            <div className="space-y-4">
              <div>
                <h3 className="font-medium mb-2">Environment Variables</h3>
                {Object.keys(selectedFunction.environment || {}).length === 0 ? (
                  <p className="text-sm text-muted-foreground">No environment variables</p>
                ) : (
                  <div className="bg-muted rounded-lg p-4 space-y-2">
                    {Object.entries(selectedFunction.environment).map(([key, value]) => (
                      <div key={key} className="flex items-center gap-2 font-mono text-sm">
                        <span className="font-semibold">{key}</span>
                        <span>=</span>
                        <span className="text-muted-foreground">{value}</span>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </div>
          )}

          {activeTab === 'logs' && (
            <div>
              {logsLoading ? (
                <div className="space-y-2">
                  {[1, 2, 3].map((i) => (
                    <Skeleton key={i} className="h-6 w-full" />
                  ))}
                </div>
              ) : logsData?.logs.length === 0 ? (
                <p className="text-sm text-muted-foreground text-center py-8">No logs available</p>
              ) : (
                <div className="space-y-1 font-mono text-sm max-h-[400px] overflow-auto">
                  {logsData?.logs.map((log, index) => {
                    const LogIcon = logLevelIcons[log.level];
                    return (
                      <div key={index} className="flex items-start gap-2 p-2 hover:bg-muted rounded">
                        <LogIcon className={cn('h-4 w-4 mt-0.5', logLevelColors[log.level])} />
                        <span className="text-muted-foreground text-xs">
                          {new Date(log.timestamp).toLocaleTimeString()}
                        </span>
                        <span className="text-xs text-muted-foreground">[{log.request_id.slice(0, 8)}]</span>
                        <span className="flex-1">{log.message}</span>
                      </div>
                    );
                  })}
                </div>
              )}
            </div>
          )}
        </div>
      </div>
    );
  }

  // List view (default)
  return (
    <div className="card p-4 space-y-4">
      <div className="flex items-center justify-between flex-wrap gap-2">
        <div className="flex items-center gap-2">
          <Zap className="h-5 w-5 text-warning" />
          <h2 className="text-lg font-semibold">Functions ({functions.length})</h2>
        </div>
        <div className="flex items-center gap-2 flex-wrap">
          <Button variant="outline" size="sm" onClick={() => refetch()}>
            <RefreshCw className="h-4 w-4 mr-2" />
            Refresh
          </Button>
          <Button size="sm" onClick={handleCreate}>
            <Plus className="h-4 w-4 mr-2" />
            New Function
          </Button>
        </div>
      </div>

      {/* Function List */}
      {functions.length === 0 ? (
        <div className="empty-state border rounded-lg">
          <Zap className="empty-state-icon" />
          <p className="empty-state-title">No functions found</p>
          <p className="empty-state-description">Click "New Function" to create your first serverless function</p>
        </div>
      ) : (
        <div className="space-y-2">
          {functions.map((func) => {
            const runtimeInfo = getRuntimeIcon(func.runtime);

            return (
              <div
                key={func.id}
                className="flex items-center justify-between p-4 rounded-lg border hover:bg-muted/50 cursor-pointer"
                onClick={() => handleDetails(func)}
              >
                <div className="flex items-center gap-4 flex-wrap">
                  <div className="w-8 h-8 rounded bg-muted flex items-center justify-center font-semibold text-sm">
                    {runtimeInfo.icon}
                  </div>
                  <div>
                    <p className="font-medium">{func.name}</p>
                    <div className="flex items-center gap-2 text-sm text-muted-foreground mt-1">
                      <span
                        className={cn(
                          'px-2 py-0.5 rounded text-xs font-medium',
                          getStatusBadgeClass(func.status)
                        )}
                      >
                        {func.status}
                      </span>
                      <span>{runtimeInfo.label}</span>
                      <span className="flex items-center gap-1">
                        <Activity className="h-3 w-3" />
                        {func.invocation_count} invocations
                      </span>
                    </div>
                  </div>
                </div>

                <div className="flex items-center gap-2" onClick={(e) => e.stopPropagation()}>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => handleInvoke(func)}
                    title="Invoke function"
                  >
                    <Play className="h-4 w-4" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => handleEdit(func)}
                    title="Edit function"
                  >
                    <Settings className="h-4 w-4" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => handleDeleteConfirm(func)}
                    title="Delete function"
                  >
                    <Trash2 className="h-4 w-4" />
                  </Button>
                </div>
              </div>
            );
          })}
        </div>
      )}

      {/* Invoke Modal */}
      {showInvokeModal && selectedFunction && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
          <div className="bg-background rounded-lg p-6 max-w-2xl w-full mx-4 max-h-[80vh] overflow-auto">
            <div className="flex items-center justify-between mb-4">
              <h3 className="text-lg font-semibold">Invoke: {selectedFunction.name}</h3>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => {
                  setShowInvokeModal(false);
                  setInvokeResult(null);
                }}
              >
                <X className="h-4 w-4" />
              </Button>
            </div>

            <div className="space-y-4">
              <div>
                <label className="block text-sm font-medium mb-1">Payload (JSON)</label>
                <textarea
                  value={invokePayload}
                  onChange={(e) => setInvokePayload(e.target.value)}
                  className="w-full px-3 py-2 rounded-md border bg-background font-mono text-sm min-h-[100px]"
                  placeholder='{"key": "value"}'
                  spellCheck={false}
                />
              </div>

              <Button
                onClick={handleInvokeSubmit}
                disabled={invokeMutation.isPending}
              >
                {invokeMutation.isPending ? (
                  <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                ) : (
                  <Play className="h-4 w-4 mr-2" />
                )}
                Invoke
              </Button>

              {invokeResult && (
                <div className="space-y-2">
                  <div className="flex items-center gap-4 text-sm">
                    <span className="flex items-center gap-1">
                      {invokeResult.error ? (
                        <AlertCircle className="h-4 w-4 text-error" />
                      ) : (
                        <CheckCircle className="h-4 w-4 text-green-500" />
                      )}
                      Status: {invokeResult.status_code}
                    </span>
                    <span className="flex items-center gap-1">
                      <Clock className="h-4 w-4" />
                      {formatDuration(invokeResult.duration_ms)}
                    </span>
                  </div>

                  <div>
                    <label className="block text-sm font-medium mb-1">Response</label>
                    <pre className="p-4 bg-muted rounded-lg font-mono text-sm overflow-auto max-h-[200px]">
                      {JSON.stringify(invokeResult.body, null, 2)}
                    </pre>
                  </div>

                  {invokeResult.error && (
                    <div className="text-sm text-error">
                      Error: {invokeResult.error}
                    </div>
                  )}

                  {invokeResult.logs && invokeResult.logs.length > 0 && (
                    <div>
                      <label className="block text-sm font-medium mb-1">Logs</label>
                      <pre className="p-4 bg-muted rounded-lg font-mono text-xs overflow-auto max-h-[150px]">
                        {invokeResult.logs.join('\n')}
                      </pre>
                    </div>
                  )}
                </div>
              )}

              {invokeMutation.error && (
                <p className="text-sm text-error">
                  {(invokeMutation.error as Error).message}
                </p>
              )}
            </div>
          </div>
        </div>
      )}

      {/* Delete Confirmation Modal */}
      {showDeleteConfirm && functionToDelete && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
          <div className="bg-background rounded-lg p-6 max-w-md w-full mx-4">
            <div className="flex items-center gap-2 mb-4">
              <AlertCircle className="h-5 w-5 text-error" />
              <h3 className="text-lg font-semibold">Delete Function</h3>
            </div>
            <p className="text-muted-foreground mb-4">
              Are you sure you want to delete <strong>{functionToDelete.name}</strong>? This action
              cannot be undone.
            </p>
            <div className="flex items-center gap-2 justify-end">
              <Button
                variant="outline"
                onClick={() => {
                  setShowDeleteConfirm(false);
                  setFunctionToDelete(null);
                }}
              >
                Cancel
              </Button>
              <Button
                variant="error"
                onClick={handleDeleteExecute}
                disabled={deleteMutation.isPending}
              >
                {deleteMutation.isPending ? (
                  <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                ) : (
                  <Trash2 className="h-4 w-4 mr-2" />
                )}
                Delete
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
