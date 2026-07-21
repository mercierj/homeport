export type AWSOperationServiceKey = string;
export type AWSOperationServiceStatus = 'available' | 'unavailable' | 'degraded';
export type AWSOperationCapability = 'list' | 'read' | 'create' | 'update' | 'delete' | 'invoke' | 'logs' | 'purge' | 'retry';

export interface AWSOperationServiceState {
  status: AWSOperationServiceStatus;
  capabilities: AWSOperationCapability[];
  reason?: string;
}

export interface AWSOperationService {
  service: AWSOperationServiceKey;
  display_name: string;
  target: string;
  family: string;
  panel_kind: string;
  status: AWSOperationServiceStatus;
  capabilities: AWSOperationCapability[];
  reason?: string;
}

export interface AWSOperationBinding {
  imported_resource_id: string;
  service: AWSOperationServiceKey;
  local_resource_id: string;
  local_stack_id: string;
  name: string;
  region?: string;
  tags?: Record<string, string>;
}

export interface AWSOperationWorkspace {
  id: string;
  discovery_id: string;
  name: string;
  provider: 'aws';
  cutover_completed_at?: string;
  services: Partial<Record<AWSOperationServiceKey, AWSOperationServiceState>>;
  bindings: AWSOperationBinding[];
}

export interface AWSLambdaResource {
  id: string;
  name: string;
  runtime: string;
  handler: string;
  memory_mb: number;
  timeout_seconds: number;
  environment?: Record<string, string>;
  description?: string;
  status: string;
  invocation_count?: number;
  last_invoked?: string;
  created_at?: string;
  updated_at?: string;
  imported_resource_id: string;
  region?: string;
  tags?: Record<string, string>;
}

export interface AWSSQSResource {
  name: string;
  pending_count?: number;
  active_count?: number;
  completed_count?: number;
  failed_count?: number;
  total_count?: number;
  PendingCount?: number;
  ActiveCount?: number;
  CompletedCount?: number;
  FailedCount?: number;
  TotalCount?: number;
  imported_resource_id: string;
  region?: string;
  tags?: Record<string, string>;
}

export interface AWSSQSMessage {
  ID?: string; id?: string;
  QueueName?: string; queue_name?: string;
  Status?: string; status?: string;
  Data?: Record<string, unknown>; data?: Record<string, unknown>;
  Attempts?: number; attempts?: number;
  MaxAttempts?: number; max_attempts?: number;
  Error?: string; error?: string;
  CreatedAt?: string; created_at?: string;
}
