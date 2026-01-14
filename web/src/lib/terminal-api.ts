import { config } from './config';

// WebSocket message types
export type MessageType = 'input' | 'resize' | 'ping' | 'output' | 'error' | 'connected' | 'pong';

// Message envelope
export interface WSMessage<T = unknown> {
  type: MessageType;
  data?: T;
}

// Input message data
export interface InputData {
  data: string;
}

// Resize message data
export interface ResizeData {
  cols: number;
  rows: number;
}

// Output message data
export interface OutputData {
  data: string;
}

// Error message data
export interface ErrorData {
  error: string;
  code?: string;
}

// Connected message data
export interface ConnectedData {
  session_id: string;
  container_id: string;
}

// Terminal connection options
export interface TerminalConnectionOptions {
  containerID: string;
  cols?: number;
  rows?: number;
  onOutput: (data: string) => void;
  onConnected?: (data: ConnectedData) => void;
  onError?: (error: ErrorData) => void;
  onClose?: () => void;
}

// Terminal connection class
export class TerminalConnection {
  private ws: WebSocket | null = null;
  private options: TerminalConnectionOptions;
  private pingInterval: number | null = null;

  constructor(options: TerminalConnectionOptions) {
    this.options = options;
  }

  connect(): void {
    const { containerID, cols = 80, rows = 24 } = this.options;

    // Build WebSocket URL
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const host = config.api.baseUrl
      ? new URL(config.api.baseUrl).host
      : window.location.host;
    const url = `${protocol}//${host}/api/v1/terminal/containers/${encodeURIComponent(containerID)}/exec?cols=${cols}&rows=${rows}`;

    this.ws = new WebSocket(url);

    this.ws.onopen = () => {
      this.startPing();
    };

    this.ws.onmessage = (event) => {
      try {
        const msg: WSMessage = JSON.parse(event.data);
        this.handleMessage(msg);
      } catch {
        // Handle raw data if not JSON
        this.options.onOutput(event.data);
      }
    };

    this.ws.onerror = () => {
      this.options.onError?.({ error: 'WebSocket connection error' });
    };

    this.ws.onclose = () => {
      this.stopPing();
      this.options.onClose?.();
    };
  }

  private handleMessage(msg: WSMessage): void {
    switch (msg.type) {
      case 'output': {
        const output = msg.data as OutputData;
        this.options.onOutput(output.data);
        break;
      }

      case 'connected': {
        const connected = msg.data as ConnectedData;
        this.options.onConnected?.(connected);
        break;
      }

      case 'error': {
        const error = msg.data as ErrorData;
        this.options.onError?.(error);
        break;
      }

      case 'pong':
        // Keep-alive received
        break;
    }
  }

  send(data: string): void {
    if (this.ws?.readyState === WebSocket.OPEN) {
      const msg: WSMessage<InputData> = {
        type: 'input',
        data: { data },
      };
      this.ws.send(JSON.stringify(msg));
    }
  }

  resize(cols: number, rows: number): void {
    if (this.ws?.readyState === WebSocket.OPEN) {
      const msg: WSMessage<ResizeData> = {
        type: 'resize',
        data: { cols, rows },
      };
      this.ws.send(JSON.stringify(msg));
    }
  }

  private startPing(): void {
    this.pingInterval = window.setInterval(() => {
      if (this.ws?.readyState === WebSocket.OPEN) {
        const msg: WSMessage = { type: 'ping' };
        this.ws.send(JSON.stringify(msg));
      }
    }, 25000);
  }

  private stopPing(): void {
    if (this.pingInterval !== null) {
      clearInterval(this.pingInterval);
      this.pingInterval = null;
    }
  }

  disconnect(): void {
    this.stopPing();
    if (this.ws) {
      this.ws.close(1000, 'User closed terminal');
      this.ws = null;
    }
  }

  get isConnected(): boolean {
    return this.ws?.readyState === WebSocket.OPEN;
  }
}

// Factory function
export function createTerminalConnection(options: TerminalConnectionOptions): TerminalConnection {
  return new TerminalConnection(options);
}
