import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import {
  listSecrets,
  getSecret,
  createSecret,
  updateSecret,
  deleteSecret,
  
  type SecretMetadata,
} from '@/lib/secrets-api';

import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';
import {
  Key,
  RefreshCw,
  Trash2,
  Plus,
  Edit,
  Save,
  X,
  Eye,
  EyeOff,
  Copy,
  Check,
  Lock,
  Loader2,
} from 'lucide-react';
import { Skeleton } from '@/components/ui/skeleton';

export function SecretManager({ stackId = 'default' }: { stackId?: string }) {
  const [selectedSecret, setSelectedSecret] = useState<string | null>(null);
  const [showValue, setShowValue] = useState(false);
  const [copiedKey, setCopiedKey] = useState<string | null>(null);
  const [editMode, setEditMode] = useState(false);
  const [editValue, setEditValue] = useState('');
  const [showNewSecret, setShowNewSecret] = useState(false);
  const [newSecretName, setNewSecretName] = useState('');
  const [newSecretValue, setNewSecretValue] = useState('');
  const queryClient = useQueryClient();

  const secretsQuery = useQuery({
    queryKey: ['secrets', stackId],
    queryFn: () => listSecrets(stackId),
  });

  const secretQuery = useQuery({
    queryKey: ['secret', stackId, selectedSecret],
    queryFn: () => getSecret(stackId, selectedSecret!),
    enabled: !!selectedSecret,
  });

  const createMutation = useMutation({
    mutationFn: ({ name, value }: { name: string; value: string }) =>
      createSecret(stackId, { name, value }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['secrets', stackId] });
      setShowNewSecret(false);
      setNewSecretName('');
      setNewSecretValue('');
      toast.success('Secret created');
    },
    onError: (error) => {
      toast.error('Failed to create secret', {
        description: error instanceof Error ? error.message : 'Unknown error',
      });
    },
  });

  const updateMutation = useMutation({
    mutationFn: ({ name, value }: { name: string; value: string }) =>
      updateSecret(stackId, name, value),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['secrets', stackId] });
      queryClient.invalidateQueries({ queryKey: ['secret', stackId, selectedSecret] });
      setEditMode(false);
      toast.success('Secret updated');
    },
    onError: (error) => {
      toast.error('Failed to update secret', {
        description: error instanceof Error ? error.message : 'Unknown error',
      });
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (name: string) => deleteSecret(stackId, name),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['secrets', stackId] });
      setSelectedSecret(null);
      toast.success('Secret deleted');
    },
    onError: (error) => {
      toast.error('Failed to delete secret', {
        description: error instanceof Error ? error.message : 'Unknown error',
      });
    },
  });

  const copyToClipboard = async (value: string, key: string) => {
    try {
      await navigator.clipboard.writeText(value);
      setCopiedKey(key);
      setTimeout(() => setCopiedKey(null), 2000);
      toast.success('Copied to clipboard');
    } catch {
      toast.error('Failed to copy to clipboard');
    }
  };

  const maskValue = (value: string): string => {
    if (value.length <= 8) return '••••••••';
    return value.substring(0, 4) + '••••••••' + value.substring(value.length - 4);
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold">Secret Manager</h2>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => secretsQuery.refetch()}
            disabled={secretsQuery.isFetching}
          >
            <RefreshCw className={cn("h-4 w-4 mr-2", secretsQuery.isFetching && "animate-spin")} />
            Refresh
          </Button>
          <Button size="sm" onClick={() => setShowNewSecret(true)}>
            <Plus className="h-4 w-4 mr-2" />
            New Secret
          </Button>
        </div>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        {/* Secrets List */}
        <div className="border rounded-lg p-4">
          <h3 className="font-medium mb-3">Secrets</h3>

          {/* New Secret Form */}
          {showNewSecret && (
            <div className="mb-3 p-3 border rounded bg-muted/30 space-y-2">
              <input
                type="text"
                value={newSecretName}
                onChange={(e) => setNewSecretName(e.target.value)}
                placeholder="Secret name"
                className="w-full px-2 py-1.5 text-sm border rounded bg-background"
              />
              <input
                type="password"
                value={newSecretValue}
                onChange={(e) => setNewSecretValue(e.target.value)}
                placeholder="Secret value"
                className="w-full px-2 py-1.5 text-sm border rounded bg-background"
              />
              <div className="flex gap-1">
                <Button
                  size="sm"
                  onClick={() => createMutation.mutate({ name: newSecretName, value: newSecretValue })}
                  disabled={!newSecretName || !newSecretValue || createMutation.isPending}
                >
                  {createMutation.isPending ? (
                    <Loader2 className="h-4 w-4 animate-spin mr-1" />
                  ) : (
                    <Save className="h-4 w-4 mr-1" />
                  )}
                  Create
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => {
                    setShowNewSecret(false);
                    setNewSecretName('');
                    setNewSecretValue('');
                  }}
                >
                  <X className="h-4 w-4" />
                </Button>
              </div>
            </div>
          )}

          {secretsQuery.isLoading ? (
            <div className="space-y-2">
              {[1, 2, 3, 4].map((i) => (
                <Skeleton key={i} className="h-12 w-full" />
              ))}
            </div>
          ) : secretsQuery.error ? (
            <p className="text-sm text-destructive">Failed to load secrets</p>
          ) : (
            <ul className="space-y-1">
              {secretsQuery.data?.secrets.map((secret: SecretMetadata) => (
                <li key={secret.name}>
                  <button
                    onClick={() => {
                      setSelectedSecret(secret.name);
                      setShowValue(false);
                      setEditMode(false);
                    }}
                    className={cn(
                      "w-full text-left px-3 py-2 rounded text-sm",
                      selectedSecret === secret.name
                        ? "bg-primary text-primary-foreground"
                        : "hover:bg-muted"
                    )}
                  >
                    <div className="flex items-center gap-2">
                      <Key className="h-4 w-4 flex-shrink-0" />
                      <span className="truncate">{secret.name}</span>
                    </div>
                    {secret.updated_at && (
                      <p className="text-xs opacity-70 mt-1 truncate">
                        Updated: {new Date(secret.updated_at).toLocaleDateString()}
                      </p>
                    )}
                  </button>
                </li>
              ))}
              {secretsQuery.data?.secrets.length === 0 && (
                <li className="text-sm text-muted-foreground px-2 py-4 text-center">
                  No secrets found
                </li>
              )}
            </ul>
          )}
        </div>

        {/* Secret Details */}
        <div className="md:col-span-2 border rounded-lg p-4">
          {selectedSecret ? (
            <>
              <div className="flex items-center justify-between mb-4">
                <div className="flex items-center gap-2">
                  <Lock className="h-5 w-5 text-muted-foreground" />
                  <h3 className="font-mono text-sm">{selectedSecret}</h3>
                </div>
                <div className="flex gap-2">
                  {!editMode && (
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => {
                        setEditMode(true);
                        setEditValue(secretQuery.data?.value || '');
                      }}
                    >
                      <Edit className="h-4 w-4 mr-1" />
                      Edit
                    </Button>
                  )}
                  <Button
                    size="sm"
                    variant="error"
                    onClick={() => deleteMutation.mutate(selectedSecret)}
                    disabled={deleteMutation.isPending}
                  >
                    <Trash2 className="h-4 w-4 mr-1" />
                    Delete
                  </Button>
                </div>
              </div>

              {secretQuery.isLoading ? (
                <Skeleton className="h-24 w-full" />
              ) : secretQuery.error ? (
                <p className="text-destructive">Failed to load secret</p>
              ) : editMode ? (
                <div className="space-y-3">
                  <div>
                    <label className="text-sm font-medium mb-1 block">Value</label>
                    <textarea
                      value={editValue}
                      onChange={(e) => setEditValue(e.target.value)}
                      rows={6}
                      className="w-full px-3 py-2 text-sm font-mono border rounded bg-background resize-none"
                    />
                  </div>
                  <div className="flex gap-2">
                    <Button
                      size="sm"
                      onClick={() => updateMutation.mutate({ name: selectedSecret, value: editValue })}
                      disabled={updateMutation.isPending}
                    >
                      {updateMutation.isPending ? (
                        <Loader2 className="h-4 w-4 animate-spin mr-1" />
                      ) : (
                        <Save className="h-4 w-4 mr-1" />
                      )}
                      Save Changes
                    </Button>
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => setEditMode(false)}
                    >
                      Cancel
                    </Button>
                  </div>
                </div>
              ) : (
                <>
                  <div className="mb-4">
                    <div className="flex items-center justify-between mb-2">
                      <label className="text-sm font-medium">Value</label>
                      <div className="flex gap-1">
                        <Button
                          size="sm"
                          variant="ghost"
                          onClick={() => setShowValue(!showValue)}
                        >
                          {showValue ? (
                            <EyeOff className="h-4 w-4" />
                          ) : (
                            <Eye className="h-4 w-4" />
                          )}
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          onClick={() => copyToClipboard(secretQuery.data?.value || '', selectedSecret)}
                        >
                          {copiedKey === selectedSecret ? (
                            <Check className="h-4 w-4 text-green-500" />
                          ) : (
                            <Copy className="h-4 w-4" />
                          )}
                        </Button>
                      </div>
                    </div>
                    <pre className="text-sm font-mono bg-muted p-3 rounded overflow-auto max-h-[200px]">
                      {showValue
                        ? secretQuery.data?.value
                        : maskValue(secretQuery.data?.value || '')}
                    </pre>
                  </div>

                  {secretQuery.data?.metadata && (
                    <div className="border-t pt-4 space-y-2">
                      <h4 className="text-sm font-medium">Metadata</h4>
                      <dl className="grid grid-cols-2 gap-2 text-sm">
                        <dt className="text-muted-foreground">Created</dt>
                        <dd>{new Date(secretQuery.data.metadata.created_at).toLocaleString()}</dd>
                        <dt className="text-muted-foreground">Updated</dt>
                        <dd>{new Date(secretQuery.data.metadata.updated_at).toLocaleString()}</dd>
                        <dt className="text-muted-foreground">Version</dt>
                        <dd>{secretQuery.data.metadata.version}</dd>
                      </dl>
                    </div>
                  )}

                  {secretQuery.data?.labels && Object.keys(secretQuery.data.labels).length > 0 && (
                    <div className="border-t pt-4 mt-4">
                      <h4 className="text-sm font-medium mb-2">Labels</h4>
                      <div className="flex flex-wrap gap-2">
                        {Object.entries(secretQuery.data.labels).map(([key, value]) => (
                          <span
                            key={key}
                            className="px-2 py-1 text-xs rounded bg-muted"
                          >
                            {key}: {value}
                          </span>
                        ))}
                      </div>
                    </div>
                  )}
                </>
              )}
            </>
          ) : (
            <div className="flex items-center justify-center py-12 text-muted-foreground">
              <Key className="h-8 w-8 mr-2" />
              <span>Select a secret to view its details</span>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
