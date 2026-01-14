import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { cn } from '@/lib/utils';
import { statusBadgeClasses } from '@/lib/diagram-types';
import {
  RefreshCw,
  Plus,
  Trash2,
  Loader2,
  Archive,
  HardDrive,
  Download,
  RotateCcw,
  X,
  Check,
  Clock,
  AlertCircle,
} from 'lucide-react';
import {
  listBackups,
  listVolumes,
  createBackup,
  deleteBackup,
  restoreBackup,
  getBackupDownloadUrl,
  formatBytes,
  getStatusLabel,
  type Backup,
  type CreateBackupRequest,
} from '@/lib/backup-api';

interface BackupManagerProps {
  stackId?: string;
  onError?: (error: Error) => void;
}

type TabType = 'backups' | 'volumes';

export function BackupManager({ stackId, onError }: BackupManagerProps) {
  const queryClient = useQueryClient();
  const [activeTab, setActiveTab] = useState<TabType>('backups');

  // Backup form state
  const [showBackupForm, setShowBackupForm] = useState(false);
  const [backupFormData, setBackupFormData] = useState({
    name: '',
    description: '',
    volumes: [] as string[],
  });

  // Restore dialog state
  const [restoreBackup_, setRestoreBackup] = useState<Backup | null>(null);
  const [restoreVolumes, setRestoreVolumes] = useState<string[]>([]);

  const [activeAction, setActiveAction] = useState<{ id: string; action: string } | null>(null);

  // Queries
  const backupsQuery = useQuery({
    queryKey: ['backups', stackId],
    queryFn: () => listBackups(stackId),
    refetchInterval: 5000, // Poll for status updates
  });

  const volumesQuery = useQuery({
    queryKey: ['volumes', stackId],
    queryFn: () => listVolumes(stackId),
    refetchInterval: 30000,
  });

  const clearActiveAction = () => setActiveAction(null);

  // Mutations
  const createBackupMutation = useMutation({
    mutationFn: (req: CreateBackupRequest) => createBackup(req),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['backups'] });
      resetBackupForm();
    },
    onError: (err: Error) => onError?.(err),
  });

  const deleteBackupMutation = useMutation({
    mutationFn: (id: string) => deleteBackup(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['backups'] });
      clearActiveAction();
    },
    onError: (err: Error) => {
      clearActiveAction();
      onError?.(err);
    },
  });

  const restoreBackupMutation = useMutation({
    mutationFn: ({ id, volumes }: { id: string; volumes?: string[] }) =>
      restoreBackup(id, { volumes }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['backups'] });
      setRestoreBackup(null);
      setRestoreVolumes([]);
    },
    onError: (err: Error) => onError?.(err),
  });

  const resetBackupForm = () => {
    setShowBackupForm(false);
    setBackupFormData({
      name: '',
      description: '',
      volumes: [],
    });
  };

  const handleBackupFormSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!backupFormData.name.trim() || backupFormData.volumes.length === 0) return;

    createBackupMutation.mutate({
      name: backupFormData.name,
      description: backupFormData.description,
      stack_id: stackId || 'default',
      volumes: backupFormData.volumes,
    });
  };

  const toggleVolumeSelection = (volumeName: string) => {
    setBackupFormData((prev) => ({
      ...prev,
      volumes: prev.volumes.includes(volumeName)
        ? prev.volumes.filter((v) => v !== volumeName)
        : [...prev.volumes, volumeName],
    }));
  };

  const toggleRestoreVolume = (volumeName: string) => {
    setRestoreVolumes((prev) =>
      prev.includes(volumeName)
        ? prev.filter((v) => v !== volumeName)
        : [...prev, volumeName]
    );
  };

  const handleRestore = () => {
    if (!restoreBackup_) return;
    restoreBackupMutation.mutate({
      id: restoreBackup_.id,
      volumes: restoreVolumes.length > 0 ? restoreVolumes : undefined,
    });
  };

  const handleDownload = (backup: Backup) => {
    const url = getBackupDownloadUrl(backup.id);
    window.open(url, '_blank');
  };

  const isLoading = backupsQuery.isLoading || volumesQuery.isLoading;
  const error = backupsQuery.error || volumesQuery.error;

  if (isLoading) {
    return (
      <div className="space-y-4 rounded-lg border p-4">
        <div className="flex items-center justify-between">
          <Skeleton className="h-6 w-48" />
          <Skeleton className="h-9 w-24" />
        </div>
        <div className="flex gap-2">
          <Skeleton className="h-10 w-24" />
          <Skeleton className="h-10 w-24" />
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

  if (error) {
    return (
      <div className="card p-4">
        <div className="text-error">
          Error loading backup data. Backup service may not be available.
        </div>
      </div>
    );
  }

  const backups = backupsQuery.data?.backups || [];
  const volumes = volumesQuery.data?.volumes || [];

  return (
    <div className="card p-4 space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between flex-wrap gap-2">
        <div className="flex items-center gap-2">
          <Archive className="h-5 w-5 text-primary" />
          <h2 className="text-lg font-semibold">Backup Management</h2>
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              backupsQuery.refetch();
              volumesQuery.refetch();
            }}
          >
            <RefreshCw className="h-4 w-4 mr-2" />
            Refresh
          </Button>
        </div>
      </div>

      {/* Tabs */}
      <div className="flex gap-2 border-b pb-2">
        <button
          onClick={() => setActiveTab('backups')}
          className={cn(
            'px-4 py-2 rounded-t font-medium text-sm transition-colors',
            activeTab === 'backups'
              ? 'bg-primary text-primary-foreground'
              : 'hover:bg-muted'
          )}
        >
          <Archive className="h-4 w-4 inline-block mr-2" />
          Backups ({backups.length})
        </button>
        <button
          onClick={() => setActiveTab('volumes')}
          className={cn(
            'px-4 py-2 rounded-t font-medium text-sm transition-colors',
            activeTab === 'volumes'
              ? 'bg-primary text-primary-foreground'
              : 'hover:bg-muted'
          )}
        >
          <HardDrive className="h-4 w-4 inline-block mr-2" />
          Volumes ({volumes.length})
        </button>
      </div>

      {/* Backups Tab */}
      {activeTab === 'backups' && (
        <div className="space-y-4">
          <div className="flex justify-end">
            <Button
              size="sm"
              onClick={() => {
                resetBackupForm();
                setShowBackupForm(true);
              }}
              disabled={volumes.length === 0}
            >
              <Plus className="h-4 w-4 mr-2" />
              New Backup
            </Button>
          </div>

          {/* Backup Form */}
          {showBackupForm && (
            <div className="rounded-lg border p-4 bg-muted/50">
              <div className="flex items-center justify-between mb-4">
                <h3 className="font-medium">Create New Backup</h3>
                <Button variant="ghost" size="sm" onClick={resetBackupForm}>
                  <X className="h-4 w-4" />
                </Button>
              </div>
              <form onSubmit={handleBackupFormSubmit} className="space-y-4">
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                  <div>
                    <label className="block text-sm font-medium mb-1">Backup Name *</label>
                    <input
                      type="text"
                      value={backupFormData.name}
                      onChange={(e) =>
                        setBackupFormData((prev) => ({ ...prev, name: e.target.value }))
                      }
                      placeholder="my-backup"
                      className="w-full px-3 py-2 rounded-md border bg-background"
                      required
                    />
                  </div>
                  <div>
                    <label className="block text-sm font-medium mb-1">Description</label>
                    <input
                      type="text"
                      value={backupFormData.description}
                      onChange={(e) =>
                        setBackupFormData((prev) => ({ ...prev, description: e.target.value }))
                      }
                      placeholder="Optional description"
                      className="w-full px-3 py-2 rounded-md border bg-background"
                    />
                  </div>
                </div>
                <div>
                  <label className="block text-sm font-medium mb-2">
                    Select Volumes to Backup *
                  </label>
                  <div className="flex flex-wrap gap-2 max-h-40 overflow-y-auto p-2 border rounded-md bg-background">
                    {volumes.length > 0 ? (
                      volumes.map((volume) => (
                        <button
                          key={volume.name}
                          type="button"
                          onClick={() => toggleVolumeSelection(volume.name)}
                          className={cn(
                            'px-3 py-1 rounded-full text-sm border transition-colors',
                            backupFormData.volumes.includes(volume.name)
                              ? 'bg-primary text-primary-foreground border-primary'
                              : 'bg-background hover:bg-muted'
                          )}
                        >
                          {backupFormData.volumes.includes(volume.name) && (
                            <Check className="h-3 w-3 inline-block mr-1" />
                          )}
                          {volume.name}
                        </button>
                      ))
                    ) : (
                      <p className="text-sm text-muted-foreground">No volumes available</p>
                    )}
                  </div>
                  <p className="text-xs text-muted-foreground mt-1">
                    {backupFormData.volumes.length} volume(s) selected
                  </p>
                </div>
                <div className="flex items-center gap-2">
                  <Button
                    type="submit"
                    disabled={
                      createBackupMutation.isPending ||
                      !backupFormData.name.trim() ||
                      backupFormData.volumes.length === 0
                    }
                  >
                    {createBackupMutation.isPending ? (
                      <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                    ) : (
                      <Archive className="h-4 w-4 mr-2" />
                    )}
                    Create Backup
                  </Button>
                  <Button type="button" variant="outline" onClick={resetBackupForm}>
                    Cancel
                  </Button>
                </div>
                {createBackupMutation.error && (
                  <p className="text-sm text-error">
                    {(createBackupMutation.error as Error).message}
                  </p>
                )}
              </form>
            </div>
          )}

          {/* Restore Dialog */}
          {restoreBackup_ && (
            <div className="rounded-lg border p-4 bg-muted/50">
              <div className="flex items-center justify-between mb-4">
                <h3 className="font-medium">Restore from Backup: {restoreBackup_.name}</h3>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => {
                    setRestoreBackup(null);
                    setRestoreVolumes([]);
                  }}
                >
                  <X className="h-4 w-4" />
                </Button>
              </div>
              <div className="space-y-4">
                <div>
                  <label className="block text-sm font-medium mb-2">
                    Select Volumes to Restore (leave empty to restore all)
                  </label>
                  <div className="flex flex-wrap gap-2 p-2 border rounded-md bg-background">
                    {restoreBackup_.volumes.map((vol) => (
                      <button
                        key={vol}
                        type="button"
                        onClick={() => toggleRestoreVolume(vol)}
                        className={cn(
                          'px-3 py-1 rounded-full text-sm border transition-colors',
                          restoreVolumes.includes(vol)
                            ? 'bg-primary text-primary-foreground border-primary'
                            : 'bg-background hover:bg-muted'
                        )}
                      >
                        {restoreVolumes.includes(vol) && (
                          <Check className="h-3 w-3 inline-block mr-1" />
                        )}
                        {vol}
                      </button>
                    ))}
                  </div>
                  <p className="text-xs text-muted-foreground mt-1">
                    {restoreVolumes.length === 0
                      ? 'All volumes will be restored'
                      : `${restoreVolumes.length} volume(s) selected`}
                  </p>
                </div>
                <div className="flex items-center gap-2">
                  <Button
                    onClick={handleRestore}
                    disabled={restoreBackupMutation.isPending}
                  >
                    {restoreBackupMutation.isPending ? (
                      <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                    ) : (
                      <RotateCcw className="h-4 w-4 mr-2" />
                    )}
                    Restore
                  </Button>
                  <Button
                    variant="outline"
                    onClick={() => {
                      setRestoreBackup(null);
                      setRestoreVolumes([]);
                    }}
                  >
                    Cancel
                  </Button>
                </div>
                {restoreBackupMutation.error && (
                  <p className="text-sm text-error">
                    {(restoreBackupMutation.error as Error).message}
                  </p>
                )}
              </div>
            </div>
          )}

          {/* Backup List */}
          {backups.length === 0 ? (
            <div className="empty-state border rounded-lg">
              <Archive className="empty-state-icon" />
              <p className="empty-state-title">No backups found</p>
              <p className="empty-state-description">Click "New Backup" to create a backup</p>
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead className="bg-muted">
                  <tr>
                    <th className="px-4 py-2 text-left font-medium">Status</th>
                    <th className="px-4 py-2 text-left font-medium">Name</th>
                    <th className="px-4 py-2 text-left font-medium">Volumes</th>
                    <th className="px-4 py-2 text-left font-medium">Size</th>
                    <th className="px-4 py-2 text-left font-medium">Created</th>
                    <th className="px-4 py-2 text-right font-medium">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {backups.map((backup) => (
                    <tr key={backup.id} className="border-t">
                      <td className="px-4 py-3">
                        <span
                          className={cn(
                            'inline-flex items-center gap-1',
                            statusBadgeClasses[backup.status] || 'badge-secondary'
                          )}
                        >
                          {backup.status === 'running' && (
                            <Loader2 className="h-3 w-3 animate-spin" />
                          )}
                          {backup.status === 'failed' && <AlertCircle className="h-3 w-3" />}
                          {backup.status === 'completed' && <Check className="h-3 w-3" />}
                          {backup.status === 'pending' && <Clock className="h-3 w-3" />}
                          {getStatusLabel(backup.status)}
                        </span>
                        {backup.error && (
                          <p className="text-xs text-error mt-1">{backup.error}</p>
                        )}
                      </td>
                      <td className="px-4 py-3">
                        <div>
                          <span className="font-medium">{backup.name}</span>
                          {backup.description && (
                            <p className="text-xs text-muted-foreground">{backup.description}</p>
                          )}
                        </div>
                      </td>
                      <td className="px-4 py-3">
                        <div className="flex flex-wrap gap-1 max-w-xs">
                          {backup.volumes.slice(0, 3).map((vol) => (
                            <span
                              key={vol}
                              className="badge-info"
                            >
                              {vol}
                            </span>
                          ))}
                          {backup.volumes.length > 3 && (
                            <span className="badge-secondary">
                              +{backup.volumes.length - 3} more
                            </span>
                          )}
                        </div>
                      </td>
                      <td className="px-4 py-3 text-muted-foreground">
                        {backup.status === 'completed' ? formatBytes(backup.size) : '-'}
                      </td>
                      <td className="px-4 py-3 text-muted-foreground">
                        <span className="flex items-center gap-1">
                          <Clock className="h-3 w-3" />
                          {new Date(backup.created_at).toLocaleString()}
                        </span>
                      </td>
                      <td className="px-4 py-3 text-right">
                        <div className="flex items-center justify-end gap-1">
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => {
                              setRestoreBackup(backup);
                              setRestoreVolumes([]);
                            }}
                            disabled={backup.status !== 'completed'}
                            title="Restore backup"
                          >
                            <RotateCcw className="h-4 w-4" />
                          </Button>
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => handleDownload(backup)}
                            disabled={backup.status !== 'completed'}
                            title="Download backup"
                          >
                            <Download className="h-4 w-4" />
                          </Button>
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => {
                              if (confirm(`Delete backup ${backup.name}?`)) {
                                setActiveAction({ id: backup.id, action: 'delete' });
                                deleteBackupMutation.mutate(backup.id);
                              }
                            }}
                            disabled={
                              deleteBackupMutation.isPending ||
                              backup.status === 'running'
                            }
                            title="Delete backup"
                          >
                            {activeAction?.id === backup.id &&
                            activeAction.action === 'delete' ? (
                              <Loader2 className="h-4 w-4 animate-spin" />
                            ) : (
                              <Trash2 className="h-4 w-4" />
                            )}
                          </Button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}

      {/* Volumes Tab */}
      {activeTab === 'volumes' && (
        <div className="space-y-4">
          {volumes.length === 0 ? (
            <div className="empty-state border rounded-lg">
              <HardDrive className="empty-state-icon" />
              <p className="empty-state-title">No volumes found</p>
              <p className="empty-state-description">Docker volumes will appear here</p>
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead className="bg-muted">
                  <tr>
                    <th className="px-4 py-2 text-left font-medium">Name</th>
                    <th className="px-4 py-2 text-left font-medium">Driver</th>
                    <th className="px-4 py-2 text-left font-medium">Stack</th>
                    <th className="px-4 py-2 text-left font-medium">Mountpoint</th>
                  </tr>
                </thead>
                <tbody>
                  {volumes.map((volume) => (
                    <tr key={volume.name} className="border-t">
                      <td className="px-4 py-3">
                        <span className="flex items-center gap-2 font-medium">
                          <HardDrive className="h-4 w-4 text-primary" />
                          {volume.name}
                        </span>
                      </td>
                      <td className="px-4 py-3 text-muted-foreground">{volume.driver}</td>
                      <td className="px-4 py-3">
                        {volume.stack_id ? (
                          <span className="badge-success">
                            {volume.stack_id}
                          </span>
                        ) : (
                          <span className="text-muted-foreground">-</span>
                        )}
                      </td>
                      <td className="px-4 py-3 text-muted-foreground font-mono text-xs">
                        {volume.mountpoint}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
