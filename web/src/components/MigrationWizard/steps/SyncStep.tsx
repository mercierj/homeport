import { useState, useEffect, useRef, useCallback } from 'react';
import {
  Database,
  HardDrive,
  RefreshCw,
  Play,
  Pause,
  CheckCircle2,
  XCircle,
  AlertCircle,
  Loader2,
  SkipForward,
} from 'lucide-react';
import { cn } from '@/lib/utils';
import { buttonVariants } from '@/lib/button-variants';
import { useWizardStore, type SyncTask, type SyncStatus } from '@/stores/wizard';
import {
  startSync,
  subscribeToSync,
  pauseSync,
  resumeSync,
  cancelSync,
  type SyncTaskRequest,
  type SyncEvent,
} from '@/lib/sync-api';

// Sync type icons
const SYNC_TYPE_ICONS: Record<string, React.ElementType> = {
  database: Database,
  storage: HardDrive,
  cache: RefreshCw,
};

// Format bytes to human readable
function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

// Format duration
function formatDuration(seconds: number): string {
  if (seconds < 60) return `${seconds}s`;
  const mins = Math.floor(seconds / 60);
  const secs = seconds % 60;
  if (mins < 60) return `${mins}m ${secs}s`;
  const hours = Math.floor(mins / 60);
  const remainingMins = mins % 60;
  return `${hours}h ${remainingMins}m`;
}

export function SyncStep() {
  const {
    bundleManifest,
    syncTasks,
    syncProgress,
    isSyncing,
    setSyncPlanId,
    setSyncTasks,
    updateSyncTask,
    setSyncProgress,
    setIsSyncing,
    setError,
    nextStep,
  } = useWizardStore();

  const [isPaused, setIsPaused] = useState(false);
  const [syncComplete, setSyncComplete] = useState(false);
  const [syncError, setSyncError] = useState<string | null>(null);
  const [elapsedTime, setElapsedTime] = useState(0);
  const [estimatedTimeRemaining, setEstimatedTimeRemaining] = useState<number | null>(null);
  const [syncId, setSyncId] = useState<string | null>(null);
  const unsubscribeRef = useRef<(() => void) | null>(null);

  // Build sync tasks from bundle manifest
  const buildSyncTasksFromManifest = useCallback((): SyncTask[] => {
    const tasks: SyncTask[] = [];

    if (!bundleManifest?.data_sync) {
      return tasks;
    }

    // Add database tasks
    if (bundleManifest.data_sync.databases) {
      bundleManifest.data_sync.databases.forEach((db, index) => {
        tasks.push({
          id: `db-${index}`,
          type: 'database',
          name: db,
          source: `cloud:${db}`,
          target: `local:${db}`,
          status: 'pending',
          progress: 0,
          bytesTotal: 0,
          bytesTransferred: 0,
          itemsTotal: 0,
          itemsCompleted: 0,
        });
      });
    }

    // Add storage tasks
    if (bundleManifest.data_sync.storage) {
      bundleManifest.data_sync.storage.forEach((storage, index) => {
        tasks.push({
          id: `storage-${index}`,
          type: 'storage',
          name: storage,
          source: `cloud:${storage}`,
          target: `minio:${storage}`,
          status: 'pending',
          progress: 0,
          bytesTotal: 0,
          bytesTransferred: 0,
          itemsTotal: 0,
          itemsCompleted: 0,
        });
      });
    }

    return tasks;
  }, [bundleManifest]);

  // Track if we've initialized
  const hasInitialized = useRef(false);

  // Initialize tasks from bundle on mount
  useEffect(() => {
    if (!hasInitialized.current && syncTasks.length === 0) {
      hasInitialized.current = true;
      const tasks = buildSyncTasksFromManifest();
      if (tasks.length > 0) {
        setSyncTasks(tasks);
      }
    }
  }, [buildSyncTasksFromManifest, setSyncTasks, syncTasks.length]);

  // Cleanup SSE subscription on unmount
  useEffect(() => {
    return () => {
      if (unsubscribeRef.current) {
        unsubscribeRef.current();
      }
    };
  }, []);

  // Timer for elapsed time
  useEffect(() => {
    let interval: ReturnType<typeof setInterval>;
    if (isSyncing && !isPaused) {
      interval = setInterval(() => {
        setElapsedTime((prev) => prev + 1);
      }, 1000);
    }
    return () => clearInterval(interval);
  }, [isSyncing, isPaused]);

  // Handle SSE events
  const handleTaskStart = (event: SyncEvent) => {
    if (event.task_id) {
      updateSyncTask(event.task_id, { status: 'running' });
    }
  };

  const handleProgress = (event: SyncEvent) => {
    if (event.task_id) {
      updateSyncTask(event.task_id, {
        progress: event.progress || 0,
        bytesTransferred: event.bytes_done || 0,
        bytesTotal: event.bytes_total || 0,
        itemsCompleted: Number(event.items_done) || 0,
        itemsTotal: Number(event.items_total) || 0,
      });

      // Update overall progress
      const tasks = syncTasks;
      const completedWeight = tasks.filter(t => t.status === 'completed').length;
      const currentProgress = (event.progress || 0) / 100;
      const overallProgress = Math.round(((completedWeight + currentProgress) / tasks.length) * 100);
      setSyncProgress(overallProgress);

      // Estimate time remaining
      if (elapsedTime > 0 && overallProgress > 0) {
        const remaining = Math.round((elapsedTime / overallProgress) * (100 - overallProgress));
        setEstimatedTimeRemaining(remaining);
      }
    }
  };

  const handleTaskComplete = (event: SyncEvent) => {
    if (event.task_id) {
      updateSyncTask(event.task_id, { status: 'completed', progress: 100 });
    }
  };

  const handleTaskError = (event: SyncEvent) => {
    if (event.task_id) {
      updateSyncTask(event.task_id, {
        status: 'failed',
        error: event.error,
      });
    }
    setSyncError(event.error || 'Sync task failed');
  };

  const handleSyncComplete = () => {
    setSyncProgress(100);
    setIsSyncing(false);
    setSyncComplete(true);
  };

  // Start real sync
  const handleStartSync = async () => {
    setError(null);
    setSyncError(null);

    const tasksToSync = syncTasks.length > 0 ? syncTasks : buildSyncTasksFromManifest();

    if (tasksToSync.length === 0) {
      // No data to sync, skip to next step
      setSyncComplete(true);
      return;
    }

    try {
      setIsSyncing(true);
      setSyncComplete(false);
      setElapsedTime(0);

      // Reset task statuses
      setSyncTasks(tasksToSync.map(t => ({ ...t, status: 'pending' as SyncStatus, progress: 0 })));

      // Convert to API format
      const apiTasks: SyncTaskRequest[] = tasksToSync.map(task => ({
        id: task.id,
        name: task.name,
        type: task.type,
        source: {
          type: task.type === 'database' ? 'postgres' : task.type === 'cache' ? 'redis' : 'minio',
          host: 'localhost',
          // These would come from actual config in production
        },
        target: {
          type: task.type === 'database' ? 'postgres' : task.type === 'cache' ? 'redis' : 'minio',
          host: 'localhost',
        },
      }));

      // Start sync
      const response = await startSync({ tasks: apiTasks });
      setSyncId(response.sync_id);
      setSyncPlanId(response.sync_id);

      // Subscribe to SSE for real-time updates
      unsubscribeRef.current = subscribeToSync(response.sync_id, {
        onTaskStart: handleTaskStart,
        onProgress: handleProgress,
        onTaskComplete: handleTaskComplete,
        onError: handleTaskError,
        onComplete: handleSyncComplete,
        onClose: () => {
          if (!syncComplete && isSyncing) {
            setSyncError('Connection to sync stream closed');
          }
        },
      });
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to start sync';
      setError(message);
      setSyncError(message);
      setIsSyncing(false);
    }
  };

  // Handle pause/resume
  const handleTogglePause = async () => {
    if (!syncId) return;

    try {
      if (isPaused) {
        await resumeSync(syncId);
        setIsPaused(false);
      } else {
        await pauseSync(syncId);
        setIsPaused(true);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to pause/resume sync');
    }
  };

  // Handle cancel
  const handleCancel = async () => {
    if (syncId) {
      try {
        await cancelSync(syncId);
      } catch {
        // Ignore cancel errors
      }
    }
    if (unsubscribeRef.current) {
      unsubscribeRef.current();
    }
    setIsSyncing(false);
    setSyncComplete(false);
    setSyncProgress(0);
  };

  // Handle skip sync
  const handleSkip = () => {
    setSyncComplete(true);
    setIsSyncing(false);
  };

  // Calculate totals
  const tasksToShow = syncTasks;
  const totalBytes = tasksToShow.reduce((sum, t) => sum + t.bytesTotal, 0);
  const transferredBytes = tasksToShow.reduce((sum, t) => sum + t.bytesTransferred, 0);
  const completedTasks = tasksToShow.filter((t) => t.status === 'completed').length;

  // Check if there's data to sync
  const hasDataToSync = tasksToShow.length > 0;

  return (
    <div className="space-y-6">
      {/* Header */}
      <div>
        <h3 className="text-lg font-semibold mb-2">Data Synchronization</h3>
        <p className="text-muted-foreground">
          {hasDataToSync
            ? 'Sync your data from cloud sources to the new self-hosted containers.'
            : 'No data synchronization required for this migration.'}
        </p>
      </div>

      {/* No data to sync */}
      {!hasDataToSync && !syncComplete && (
        <div className="bg-info/5 border border-info/20 rounded-lg p-4">
          <div className="flex items-start gap-3">
            <AlertCircle className="w-5 h-5 text-info flex-shrink-0" />
            <div>
              <p className="font-medium text-info">No Data Sync Required</p>
              <p className="text-sm text-muted-foreground mt-1">
                Your migration bundle doesn't include any databases, storage, or cache data that needs to be synchronized.
              </p>
            </div>
          </div>
        </div>
      )}

      {/* Overall progress */}
      {(isSyncing || syncComplete) && hasDataToSync && (
        <div className="bg-card border border-border rounded-lg p-4">
          <div className="flex items-center justify-between mb-4">
            <div>
              <div className="flex items-center gap-2">
                <span className="font-medium">Overall Progress</span>
                {syncComplete && <CheckCircle2 className="w-4 h-4 text-accent" />}
                {syncError && !isSyncing && <AlertCircle className="w-4 h-4 text-error" />}
              </div>
              <p className="text-sm text-muted-foreground">
                {completedTasks} of {tasksToShow.length} tasks completed
              </p>
            </div>
            <div className="text-right">
              <p className="font-mono text-lg">{syncProgress}%</p>
              {totalBytes > 0 && (
                <p className="text-xs text-muted-foreground">
                  {formatBytes(transferredBytes)} / {formatBytes(totalBytes)}
                </p>
              )}
            </div>
          </div>
          <div className="progress h-3">
            <div
              className="progress-indicator transition-all"
              style={{ width: `${syncProgress}%` }}
            />
          </div>
          <div className="flex items-center justify-between mt-2 text-xs text-muted-foreground">
            <span>Elapsed: {formatDuration(elapsedTime)}</span>
            {estimatedTimeRemaining !== null && !syncComplete && (
              <span>Remaining: ~{formatDuration(estimatedTimeRemaining)}</span>
            )}
          </div>
        </div>
      )}

      {/* Sync tasks */}
      {hasDataToSync && (
        <div className="space-y-4">
          <h4 className="font-medium">Sync Tasks</h4>
          {tasksToShow.map((task) => {
            const Icon = SYNC_TYPE_ICONS[task.type] || Database;
            const statusColors: Record<SyncStatus, string> = {
              pending: 'text-muted-foreground',
              running: 'text-primary',
              completed: 'text-accent',
              failed: 'text-error',
              skipped: 'text-warning',
            };

            return (
              <div
                key={task.id}
                className={cn(
                  'bg-card border rounded-lg p-4',
                  task.status === 'running' && 'border-primary/50',
                  task.status === 'completed' && 'border-accent/50',
                  task.status === 'failed' && 'border-error/50'
                )}
              >
                <div className="flex items-start gap-4">
                  <div
                    className={cn(
                      'p-2 rounded-lg',
                      task.type === 'database' && 'bg-purple-500/10 text-purple-500',
                      task.type === 'storage' && 'bg-green-500/10 text-green-500',
                      task.type === 'cache' && 'bg-orange-500/10 text-orange-500'
                    )}
                  >
                    <Icon className="w-5 h-5" />
                  </div>
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-2">
                        <span className="font-medium">{task.name}</span>
                        {task.status === 'running' && (
                          <Loader2 className="w-4 h-4 text-primary animate-spin" />
                        )}
                        {task.status === 'completed' && (
                          <CheckCircle2 className="w-4 h-4 text-accent" />
                        )}
                        {task.status === 'failed' && (
                          <XCircle className="w-4 h-4 text-error" />
                        )}
                      </div>
                      <span className={cn('text-sm font-medium', statusColors[task.status])}>
                        {task.progress > 0 ? `${Math.round(task.progress)}%` : task.status}
                      </span>
                    </div>
                    <p className="text-sm text-muted-foreground mt-1">
                      {task.source} → {task.target}
                    </p>

                    {/* Progress bar for running/completed tasks */}
                    {(task.status === 'running' || task.status === 'completed') && (
                      <div className="mt-3">
                        <div className="progress h-2">
                          <div
                            className="progress-indicator transition-all"
                            style={{ width: `${task.progress}%` }}
                          />
                        </div>
                        <div className="flex items-center justify-between mt-1 text-xs text-muted-foreground">
                          <span>
                            {formatBytes(task.bytesTransferred)} / {formatBytes(task.bytesTotal)}
                          </span>
                          <span>
                            {task.itemsCompleted.toLocaleString()} / {task.itemsTotal.toLocaleString()} items
                          </span>
                        </div>
                      </div>
                    )}

                    {/* Error message */}
                    {task.error && (
                      <div className="mt-2 flex items-center gap-2 text-sm text-error">
                        <AlertCircle className="w-4 h-4" />
                        {task.error}
                      </div>
                    )}
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      )}

      {/* Actions */}
      <div className="flex items-center justify-between pt-4 border-t border-border">
        {!isSyncing && !syncComplete && (
          <>
            <button
              onClick={handleSkip}
              className={cn(buttonVariants({ variant: 'outline' }), 'gap-2')}
            >
              <SkipForward className="w-4 h-4" />
              Skip Sync
            </button>
            <button
              onClick={handleStartSync}
              className={cn(buttonVariants({ variant: 'primary' }), 'gap-2')}
            >
              <Play className="w-4 h-4" />
              {hasDataToSync ? 'Start Sync' : 'Continue'}
            </button>
          </>
        )}

        {isSyncing && (
          <>
            <button
              onClick={handleTogglePause}
              className={cn(buttonVariants({ variant: 'outline' }), 'gap-2')}
            >
              {isPaused ? (
                <>
                  <Play className="w-4 h-4" />
                  Resume
                </>
              ) : (
                <>
                  <Pause className="w-4 h-4" />
                  Pause
                </>
              )}
            </button>
            <button
              onClick={handleCancel}
              className={cn(buttonVariants({ variant: 'outline' }), 'gap-2')}
            >
              Cancel
            </button>
          </>
        )}

        {syncComplete && (
          <>
            <button
              onClick={() => {
                setSyncComplete(false);
                setSyncProgress(0);
                setElapsedTime(0);
              }}
              className={cn(buttonVariants({ variant: 'outline' }), 'gap-2')}
            >
              <RefreshCw className="w-4 h-4" />
              Sync Again
            </button>
            <button
              onClick={nextStep}
              className={buttonVariants({ variant: 'primary' })}
            >
              Continue to Cutover
            </button>
          </>
        )}
      </div>
    </div>
  );
}
