import { API_BASE } from './config';

// WebSocket message types
export type WSMessageType =
  | 'sync_progress'
  | 'sync_complete'
  | 'sync_error'
  | 'deploy_progress'
  | 'deploy_complete'
  | 'deploy_error'
  | 'export_progress'
  | 'export_complete'
  | 'export_error'
  | 'health_check'
  | 'log'
  | 'ping'
  | 'pong';

// Base message interface
export interface WSMessage {
  type: WSMessageType;
  timestamp: string;
  data: unknown;
}

// Sync progress message
export interface SyncProgressMessage extends WSMessage {
  type: 'sync_progress';
  data: {
    plan_id: string;
    task_id: string;
    task_type: 'database' | 'storage' | 'cache';
    name: string;
    status: 'pending' | 'running' | 'completed' | 'failed';
    progress: number;
    bytes_transferred: number;
    bytes_total: number;
    items_completed: number;
    items_total: number;
    message?: string;
  };
}

// Sync complete message
export interface SyncCompleteMessage extends WSMessage {
  type: 'sync_complete';
  data: {
    plan_id: string;
    duration: string;
    tasks_completed: number;
    tasks_failed: number;
    total_bytes: number;
  };
}

// Sync error message
export interface SyncErrorMessage extends WSMessage {
  type: 'sync_error';
  data: {
    plan_id: string;
    task_id?: string;
    error: string;
    recoverable: boolean;
  };
}

// Deploy progress message
export interface DeployProgressMessage extends WSMessage {
  type: 'deploy_progress';
  data: {
    deployment_id: string;
    phase: string;
    phase_index: number;
    total_phases: number;
    status: 'pending' | 'running' | 'completed' | 'failed';
    message: string;
    progress: number;
  };
}

// Deploy complete message
export interface DeployCompleteMessage extends WSMessage {
  type: 'deploy_complete';
  data: {
    deployment_id: string;
    duration: string;
    services: Array<{
      name: string;
      status: 'running' | 'error' | 'stopped';
      ports?: number[];
    }>;
  };
}

// Deploy error message
export interface DeployErrorMessage extends WSMessage {
  type: 'deploy_error';
  data: {
    deployment_id: string;
    phase: string;
    error: string;
    logs?: string[];
  };
}

// Log message
export interface LogMessage extends WSMessage {
  type: 'log';
  data: {
    level: 'debug' | 'info' | 'warn' | 'error';
    source: string;
    message: string;
    context?: Record<string, unknown>;
  };
}

// Export progress message
export interface ExportProgressMessage extends WSMessage {
  type: 'export_progress';
  data: {
    bundle_id: string;
    step: string;
    message: string;
    progress: number;
  };
}

// Ping/Pong message types
export interface PingMessage extends WSMessage {
  type: 'ping';
  data: null;
}

export interface PongMessage extends WSMessage {
  type: 'pong';
  data: null;
}

// Union type for all messages
export type WSTypedMessage =
  | SyncProgressMessage
  | SyncCompleteMessage
  | SyncErrorMessage
  | DeployProgressMessage
  | DeployCompleteMessage
  | DeployErrorMessage
  | ExportProgressMessage
  | LogMessage
  | PingMessage
  | PongMessage;

// WebSocket event handlers
export interface WSEventHandlers {
  onOpen?: () => void;
  onClose?: (event: CloseEvent) => void;
  onError?: (error: Event) => void;
  onMessage?: (message: WSTypedMessage) => void;
  onSyncProgress?: (data: SyncProgressMessage['data']) => void;
  onSyncComplete?: (data: SyncCompleteMessage['data']) => void;
  onSyncError?: (data: SyncErrorMessage['data']) => void;
  onDeployProgress?: (data: DeployProgressMessage['data']) => void;
  onDeployComplete?: (data: DeployCompleteMessage['data']) => void;
  onDeployError?: (data: DeployErrorMessage['data']) => void;
  onExportProgress?: (data: ExportProgressMessage['data']) => void;
  onLog?: (data: LogMessage['data']) => void;
}

// WebSocket connection options
export interface WSConnectionOptions {
  reconnect?: boolean;
  reconnectInterval?: number;
  maxReconnectAttempts?: number;
  pingInterval?: number;
}

// Default options
const DEFAULT_OPTIONS: WSConnectionOptions = {
  reconnect: true,
  reconnectInterval: 3000,
  maxReconnectAttempts: 5,
  pingInterval: 30000,
};

// WebSocket client class
export class WebSocketClient {
  private ws: WebSocket | null = null;
  private url: string;
  private handlers: WSEventHandlers;
  private options: WSConnectionOptions;
  private reconnectAttempts = 0;
  private pingTimer: ReturnType<typeof setInterval> | null = null;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private isClosing = false;

  constructor(
    path: string,
    handlers: WSEventHandlers,
    options: WSConnectionOptions = {}
  ) {
    // Convert http(s) to ws(s)
    const wsBase = API_BASE.replace(/^http/, 'ws');
    this.url = `${wsBase}${path}`;
    this.handlers = handlers;
    this.options = { ...DEFAULT_OPTIONS, ...options };
  }

  connect(): void {
    if (this.ws?.readyState === WebSocket.OPEN) {
      return;
    }

    this.isClosing = false;
    this.ws = new WebSocket(this.url);

    this.ws.onopen = () => {
      this.reconnectAttempts = 0;
      this.startPing();
      this.handlers.onOpen?.();
    };

    this.ws.onclose = (event) => {
      this.stopPing();
      this.handlers.onClose?.(event);

      if (!this.isClosing && this.options.reconnect && this.reconnectAttempts < (this.options.maxReconnectAttempts || 5)) {
        this.scheduleReconnect();
      }
    };

    this.ws.onerror = (error) => {
      this.handlers.onError?.(error);
    };

    this.ws.onmessage = (event) => {
      try {
        const message = JSON.parse(event.data) as WSTypedMessage;

        // Handle ping/pong
        if (message.type === 'pong') {
          return;
        }

        // Call generic message handler
        this.handlers.onMessage?.(message);

        // Call type-specific handlers
        switch (message.type) {
          case 'sync_progress':
            this.handlers.onSyncProgress?.(message.data as SyncProgressMessage['data']);
            break;
          case 'sync_complete':
            this.handlers.onSyncComplete?.(message.data as SyncCompleteMessage['data']);
            break;
          case 'sync_error':
            this.handlers.onSyncError?.(message.data as SyncErrorMessage['data']);
            break;
          case 'deploy_progress':
            this.handlers.onDeployProgress?.(message.data as DeployProgressMessage['data']);
            break;
          case 'deploy_complete':
            this.handlers.onDeployComplete?.(message.data as DeployCompleteMessage['data']);
            break;
          case 'deploy_error':
            this.handlers.onDeployError?.(message.data as DeployErrorMessage['data']);
            break;
          case 'export_progress':
            this.handlers.onExportProgress?.(message.data as ExportProgressMessage['data']);
            break;
          case 'log':
            this.handlers.onLog?.(message.data as LogMessage['data']);
            break;
        }
      } catch (err) {
        console.error('Failed to parse WebSocket message:', err);
      }
    };
  }

  disconnect(): void {
    this.isClosing = true;
    this.stopPing();
    this.clearReconnectTimer();

    if (this.ws) {
      this.ws.close();
      this.ws = null;
    }
  }

  send(message: Record<string, unknown>): void {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(message));
    }
  }

  isConnected(): boolean {
    return this.ws?.readyState === WebSocket.OPEN;
  }

  private startPing(): void {
    if (this.options.pingInterval && this.options.pingInterval > 0) {
      this.pingTimer = setInterval(() => {
        this.send({ type: 'ping' });
      }, this.options.pingInterval);
    }
  }

  private stopPing(): void {
    if (this.pingTimer) {
      clearInterval(this.pingTimer);
      this.pingTimer = null;
    }
  }

  private scheduleReconnect(): void {
    this.reconnectAttempts++;
    const delay = this.options.reconnectInterval || 3000;

    this.reconnectTimer = setTimeout(() => {
      console.log(`WebSocket reconnecting... (attempt ${this.reconnectAttempts})`);
      this.connect();
    }, delay);
  }

  private clearReconnectTimer(): void {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
  }
}

// Helper function to create a sync WebSocket connection
export function createSyncWebSocket(
  planId: string,
  handlers: Pick<WSEventHandlers, 'onSyncProgress' | 'onSyncComplete' | 'onSyncError' | 'onLog'>
): WebSocketClient {
  return new WebSocketClient(`/ws/sync/${planId}`, handlers);
}

// Helper function to create a deploy WebSocket connection
export function createDeployWebSocket(
  deploymentId: string,
  handlers: Pick<WSEventHandlers, 'onDeployProgress' | 'onDeployComplete' | 'onDeployError' | 'onLog'>
): WebSocketClient {
  return new WebSocketClient(`/ws/deploy/${deploymentId}`, handlers);
}

// Helper function to create an export WebSocket connection
export function createExportWebSocket(
  bundleId: string,
  handlers: Pick<WSEventHandlers, 'onExportProgress' | 'onLog'>
): WebSocketClient {
  return new WebSocketClient(`/ws/export/${bundleId}`, handlers);
}

// React hook for WebSocket connection
import { useEffect, useRef, useState, useCallback } from 'react';

export interface UseWebSocketOptions extends WSConnectionOptions {
  autoConnect?: boolean;
}

export interface UseWebSocketReturn {
  isConnected: boolean;
  connect: () => void;
  disconnect: () => void;
  send: (message: Record<string, unknown>) => void;
}

export function useWebSocket(
  path: string,
  handlers: WSEventHandlers,
  options: UseWebSocketOptions = {}
): UseWebSocketReturn {
  const { autoConnect = true, ...wsOptions } = options;
  const clientRef = useRef<WebSocketClient | null>(null);
  const [isConnected, setIsConnected] = useState(false);

  // Wrap handlers to track connection state
  const wrappedHandlers: WSEventHandlers = {
    ...handlers,
    onOpen: () => {
      setIsConnected(true);
      handlers.onOpen?.();
    },
    onClose: (event) => {
      setIsConnected(false);
      handlers.onClose?.(event);
    },
  };

  useEffect(() => {
    clientRef.current = new WebSocketClient(path, wrappedHandlers, wsOptions);

    if (autoConnect) {
      clientRef.current.connect();
    }

    return () => {
      clientRef.current?.disconnect();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [path]);

  const connect = useCallback(() => {
    clientRef.current?.connect();
  }, []);

  const disconnect = useCallback(() => {
    clientRef.current?.disconnect();
  }, []);

  const send = useCallback((message: Record<string, unknown>) => {
    clientRef.current?.send(message);
  }, []);

  return {
    isConnected,
    connect,
    disconnect,
    send,
  };
}
