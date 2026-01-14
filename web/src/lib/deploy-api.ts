import { API_BASE } from './config';

export type DeployTarget = 'local' | 'ssh' | 'cloud';

/** Container runtime: auto-detect, Docker, or Podman */
export type ContainerRuntime = 'auto' | 'docker' | 'podman';

export interface LocalDeployConfig {
  projectName: string;
  dataDirectory: string;
  networkMode: 'bridge' | 'host';
  autoStart: boolean;
  enableMonitoring: boolean;
  composeContent: string;
  scripts: Record<string, string>;
  /** Container runtime to use (default: auto) */
  runtime: ContainerRuntime;
  // AWS credentials for data migration
  awsAccessKeyId?: string;
  awsSecretAccessKey?: string;
  awsRegion?: string;
  // Lambda functions to download (ARN -> function name)
  lambdaFunctions?: Record<string, string>;
  // S3 buckets to migrate (bucket names)
  s3Buckets?: string[];
  // RDS databases to export
  rdsDatabases?: Array<{
    identifier: string;
    engine: string;
    endpoint: string;
    database: string;
    username: string;
    password: string;
  }>;
  // DynamoDB tables to migrate
  dynamodbTables?: string[];
}

export interface SSHDeployConfig {
  host: string;
  port: number;
  username: string;
  authMethod: 'key' | 'password';
  keyPath: string;
  password: string;
  remoteDir: string;
  composeContent: string;
  scripts: Record<string, string>;
  projectName: string;
  /** Container runtime to use on remote server (default: auto) */
  runtime: ContainerRuntime;
}

export type DeployConfig = LocalDeployConfig | SSHDeployConfig;

export interface StartDeploymentResponse {
  deployment_id: string;
}

export interface DeploymentStatus {
  id: string;
  status: 'pending' | 'running' | 'completed' | 'failed' | 'cancelled';
  current_phase: number;
  total_phases: number;
  error?: string;
}

export interface PhaseEvent {
  phase: string;
  index: number;
  total: number;
}

export interface ProgressEvent {
  percent: number;
}

export interface LogEvent {
  timestamp: string;
  level: 'info' | 'warn' | 'error';
  message: string;
}

export interface ServiceStatus {
  name: string;
  healthy: boolean;
  ports: string[];
}

export interface CompleteEvent {
  services: ServiceStatus[];
}

export interface ErrorEvent {
  message: string;
  phase: string;
  recoverable: boolean;
}

export type DeployEvent =
  | { type: 'phase'; data: PhaseEvent }
  | { type: 'progress'; data: ProgressEvent }
  | { type: 'log'; data: LogEvent }
  | { type: 'complete'; data: CompleteEvent }
  | { type: 'error'; data: ErrorEvent };

export async function startDeployment(
  target: DeployTarget,
  config: DeployConfig
): Promise<StartDeploymentResponse> {
  const response = await fetch(`${API_BASE}/deploy/start`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ target, config }),
  });

  if (!response.ok) {
    const error = await response.text();
    throw new Error(error || 'Failed to start deployment');
  }

  return response.json();
}

export async function getDeploymentStatus(id: string): Promise<DeploymentStatus> {
  const response = await fetch(`${API_BASE}/deploy/${id}/status`, {
    credentials: 'include',
  });

  if (!response.ok) {
    throw new Error('Failed to get deployment status');
  }

  return response.json();
}

export async function cancelDeployment(id: string): Promise<void> {
  const response = await fetch(`${API_BASE}/deploy/${id}/cancel`, {
    method: 'POST',
    credentials: 'include',
  });

  if (!response.ok) {
    throw new Error('Failed to cancel deployment');
  }
}

export async function retryDeployment(id: string): Promise<StartDeploymentResponse> {
  const response = await fetch(`${API_BASE}/deploy/${id}/retry`, {
    method: 'POST',
    credentials: 'include',
  });

  if (!response.ok) {
    throw new Error('Failed to retry deployment');
  }

  return response.json();
}

export function subscribeToDeployment(
  id: string,
  callbacks: {
    onPhase?: (event: PhaseEvent) => void;
    onProgress?: (event: ProgressEvent) => void;
    onLog?: (event: LogEvent) => void;
    onComplete?: (event: CompleteEvent) => void;
    onError?: (event: ErrorEvent) => void;
    onClose?: () => void;
  }
): () => void {
  const eventSource = new EventSource(`${API_BASE}/deploy/${id}/stream`, {
    withCredentials: true,
  });

  eventSource.addEventListener('phase', (e: MessageEvent) => {
    const data = JSON.parse(e.data) as PhaseEvent;
    callbacks.onPhase?.(data);
  });

  eventSource.addEventListener('progress', (e: MessageEvent) => {
    const data = JSON.parse(e.data) as ProgressEvent;
    callbacks.onProgress?.(data);
  });

  eventSource.addEventListener('log', (e: MessageEvent) => {
    const data = JSON.parse(e.data) as LogEvent;
    callbacks.onLog?.(data);
  });

  eventSource.addEventListener('complete', (e: MessageEvent) => {
    const data = JSON.parse(e.data) as CompleteEvent;
    callbacks.onComplete?.(data);
    eventSource.close();
  });

  eventSource.addEventListener('error', (e: MessageEvent) => {
    if (e.data) {
      const data = JSON.parse(e.data) as ErrorEvent;
      callbacks.onError?.(data);
    }
  });

  eventSource.onerror = () => {
    callbacks.onClose?.();
    eventSource.close();
  };

  return () => {
    eventSource.close();
  };
}
