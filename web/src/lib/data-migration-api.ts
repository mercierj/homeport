import { API_BASE } from './config';
import { fetchAPI } from './api';
import type {
  MigrationConfiguration,
  ValidateMigrationResponse,
  ExecuteMigrationResponse,
  CancelMigrationResponse,
  MigrationProgress,
  MigrationPhaseEvent,
  MigrationProgressEvent,
  MigrationLogEvent,
  MigrationCompleteEvent,
  MigrationErrorEvent,
} from '@/components/MigrationWizard/types';

// ============================================================================
// API Functions
// ============================================================================

/**
 * Validate a migration configuration before execution.
 * Returns validation results, estimated duration, and data size.
 */
export async function validateMigration(
  configuration: MigrationConfiguration
): Promise<ValidateMigrationResponse> {
  return fetchAPI<ValidateMigrationResponse>('/data-migration/validate', {
    method: 'POST',
    body: JSON.stringify({ configuration }),
  });
}

/**
 * Start executing a data migration.
 * Returns the migration ID for tracking progress.
 */
export async function executeMigration(
  configuration: MigrationConfiguration
): Promise<ExecuteMigrationResponse> {
  return fetchAPI<ExecuteMigrationResponse>('/data-migration/execute', {
    method: 'POST',
    body: JSON.stringify({ configuration }),
  });
}

/**
 * Cancel a running migration.
 * @param migrationId - The ID of the migration to cancel
 * @param graceful - If true, waits for current operations to complete
 */
export async function cancelMigration(
  migrationId: string,
  graceful: boolean = true
): Promise<CancelMigrationResponse> {
  return fetchAPI<CancelMigrationResponse>(`/data-migration/${migrationId}/cancel`, {
    method: 'POST',
    body: JSON.stringify({ graceful }),
  });
}

/**
 * Get the current status of a migration.
 */
export async function getMigrationStatus(
  migrationId: string
): Promise<MigrationProgress> {
  return fetchAPI<MigrationProgress>(`/data-migration/${migrationId}/status`, {
    method: 'GET',
  });
}

/**
 * Pause a running migration.
 */
export async function pauseMigration(
  migrationId: string
): Promise<{ success: boolean; message: string }> {
  return fetchAPI<{ success: boolean; message: string }>(
    `/data-migration/${migrationId}/pause`,
    {
      method: 'POST',
    }
  );
}

/**
 * Resume a paused migration.
 */
export async function resumeMigration(
  migrationId: string
): Promise<{ success: boolean; message: string }> {
  return fetchAPI<{ success: boolean; message: string }>(
    `/data-migration/${migrationId}/resume`,
    {
      method: 'POST',
    }
  );
}

// ============================================================================
// SSE Subscription
// ============================================================================

export interface MigrationEventCallbacks {
  onPhase?: (event: MigrationPhaseEvent) => void;
  onProgress?: (event: MigrationProgressEvent) => void;
  onLog?: (event: MigrationLogEvent) => void;
  onComplete?: (event: MigrationCompleteEvent) => void;
  onError?: (event: MigrationErrorEvent) => void;
  onClose?: () => void;
  onReconnect?: (attempt: number) => void;
}

interface SubscriptionOptions {
  maxRetries?: number;
  retryDelayMs?: number;
  onReconnect?: (attempt: number) => void;
}

/**
 * Subscribe to real-time migration progress events via SSE.
 * Returns an unsubscribe function to close the connection.
 *
 * @param migrationId - The ID of the migration to subscribe to
 * @param callbacks - Event handlers for different event types
 * @param options - Subscription options (retry behavior)
 * @returns Unsubscribe function
 */
export function subscribeToMigration(
  migrationId: string,
  callbacks: MigrationEventCallbacks,
  options: SubscriptionOptions = {}
): () => void {
  const { maxRetries = 3, retryDelayMs = 1000 } = options;

  let eventSource: EventSource | null = null;
  let retryCount = 0;
  let isClosed = false;

  const connect = () => {
    if (isClosed) return;

    eventSource = new EventSource(
      `${API_BASE}/data-migration/${migrationId}/stream`,
      { withCredentials: true }
    );

    // Reset retry count on successful connection
    eventSource.onopen = () => {
      retryCount = 0;
    };

    // Handle phase events
    eventSource.addEventListener('phase', (e: MessageEvent) => {
      try {
        const data = JSON.parse(e.data) as MigrationPhaseEvent;
        callbacks.onPhase?.({ ...data, type: 'phase' });
      } catch (err) {
        console.error('Failed to parse phase event:', err);
      }
    });

    // Handle progress events
    eventSource.addEventListener('progress', (e: MessageEvent) => {
      try {
        const data = JSON.parse(e.data) as MigrationProgressEvent;
        callbacks.onProgress?.({ ...data, type: 'progress' });
      } catch (err) {
        console.error('Failed to parse progress event:', err);
      }
    });

    // Handle log events
    eventSource.addEventListener('log', (e: MessageEvent) => {
      try {
        const data = JSON.parse(e.data) as MigrationLogEvent;
        callbacks.onLog?.({ ...data, type: 'log' });
      } catch (err) {
        console.error('Failed to parse log event:', err);
      }
    });

    // Handle complete events
    eventSource.addEventListener('complete', (e: MessageEvent) => {
      try {
        const data = JSON.parse(e.data) as MigrationCompleteEvent;
        callbacks.onComplete?.({ ...data, type: 'complete' });
        // Close connection on completion
        eventSource?.close();
        callbacks.onClose?.();
      } catch (err) {
        console.error('Failed to parse complete event:', err);
      }
    });

    // Handle error events (migration errors, not connection errors)
    eventSource.addEventListener('error', (e: MessageEvent) => {
      // Check if this is a data event (migration error) vs connection error
      if (e.data) {
        try {
          const data = JSON.parse(e.data) as MigrationErrorEvent;
          callbacks.onError?.({ ...data, type: 'error' });
        } catch (err) {
          console.error('Failed to parse error event:', err);
        }
      }
    });

    // Handle connection errors
    eventSource.onerror = () => {
      eventSource?.close();

      if (isClosed) {
        callbacks.onClose?.();
        return;
      }

      // Attempt to reconnect
      if (retryCount < maxRetries) {
        retryCount++;
        callbacks.onReconnect?.(retryCount);

        setTimeout(() => {
          connect();
        }, retryDelayMs * retryCount);
      } else {
        callbacks.onError?.({
          type: 'error',
          migrationId,
          category: null,
          message: 'Lost connection to server. Max retries exceeded.',
          recoverable: false,
        });
        callbacks.onClose?.();
      }
    };
  };

  // Start connection
  connect();

  // Return unsubscribe function
  return () => {
    isClosed = true;
    eventSource?.close();
  };
}

// ============================================================================
// Helper Types
// ============================================================================

/**
 * Helper to create a properly typed migration subscription hook.
 * Useful for React components to manage subscription lifecycle.
 */
export function createMigrationSubscription(
  migrationId: string,
  callbacks: MigrationEventCallbacks
): {
  subscribe: () => void;
  unsubscribe: () => void;
  isSubscribed: () => boolean;
} {
  let unsubscribeFn: (() => void) | null = null;

  return {
    subscribe: () => {
      if (unsubscribeFn) return;
      unsubscribeFn = subscribeToMigration(migrationId, callbacks);
    },
    unsubscribe: () => {
      unsubscribeFn?.();
      unsubscribeFn = null;
    },
    isSubscribed: () => unsubscribeFn !== null,
  };
}

// ============================================================================
// Migration History (optional - for viewing past migrations)
// ============================================================================

export interface MigrationHistoryItem {
  migrationId: string;
  status: 'completed' | 'failed' | 'cancelled';
  startedAt: string;
  completedAt: string;
  duration: string;
  categoriesCompleted: string[];
  totalItemsMigrated: number;
  totalBytesMigrated: number;
  errorCount: number;
}

/**
 * Get migration history.
 */
export async function getMigrationHistory(
  limit: number = 10,
  offset: number = 0
): Promise<{ migrations: MigrationHistoryItem[]; total: number }> {
  return fetchAPI<{ migrations: MigrationHistoryItem[]; total: number }>(
    `/data-migration/history?limit=${limit}&offset=${offset}`,
    {
      method: 'GET',
    }
  );
}

/**
 * Get details of a specific past migration.
 */
export async function getMigrationDetails(
  migrationId: string
): Promise<MigrationProgress & { logs: MigrationLogEvent[] }> {
  return fetchAPI<MigrationProgress & { logs: MigrationLogEvent[] }>(
    `/data-migration/${migrationId}`,
    {
      method: 'GET',
    }
  );
}
