// ============================================================================
// Service Configuration Interfaces
// ============================================================================

// Base interface for all service configurations
export interface BaseServiceConfig {
  enabled: boolean;
  sourceType: string;
}

// Database configuration (RDS, Aurora → PostgreSQL/MySQL/ScyllaDB)
export interface DatabaseConfig extends BaseServiceConfig {
  sourceType: 'rds' | 'aurora' | 'dynamodb' | 'documentdb';
  connectionString: string;
  database: string;
  tables: string[];
  batchSize: number;
  parallelWorkers: number;
  includeSchema: boolean;
  truncateBeforeImport: boolean;
}

// Storage configuration (S3 → MinIO, EBS → Docker Volumes, EFS → NFS)
export interface StorageConfig extends BaseServiceConfig {
  sourceType: 's3' | 'azure-blob' | 'gcs';
  buckets: BucketMigration[];
  preserveMetadata: boolean;
  preserveVersions: boolean;
  filterPattern?: string;
  excludePattern?: string;
  // EBS and EFS sub-configurations
  ebs: EbsConfig;
  efs: EfsConfig;
}

export interface BucketMigration {
  sourceBucket: string;
  targetBucket: string;
  prefix?: string;
}

// EBS Volume Migration (EBS → Docker Volumes)
export interface EbsVolumeMigration {
  volumeId: string;
  volumeName: string;
  size: number;
  storageDriver: 'local' | 'nfs' | 'overlay2';
}

export interface EbsConfig {
  enabled: boolean;
  volumes: EbsVolumeMigration[];
  outputDirectory: string;
  createSnapshots: boolean;
  encryptionEnabled: boolean;
}

// EFS File System Migration (EFS → NFS/Local)
export interface EfsMountTarget {
  fileSystemId: string;
  fileSystemName: string;
  targetPath: string;
}

export interface EfsConfig {
  enabled: boolean;
  fileSystems: EfsMountTarget[];
  nfsServerImage: string;
  exportOptions: string;
  syncMethod: 'rsync' | 'datasync';
}

// Queue configuration (SQS → RabbitMQ, SNS → NATS)
export interface QueueConfig extends BaseServiceConfig {
  sourceType: 'sqs' | 'sns' | 'eventbridge';
  queues: QueueMigration[];
  migrateDeadLetterQueues: boolean;
  preserveMessageAttributes: boolean;
}

export interface QueueMigration {
  sourceQueue: string;
  targetQueue: string;
  targetExchange?: string;
}

// Cache configuration (ElastiCache → Redis/Valkey)
export interface CacheConfig extends BaseServiceConfig {
  sourceType: 'elasticache-redis' | 'elasticache-memcached' | 'azure-cache';
  endpoint: string;
  databases: number[];
  keyPattern?: string;
  excludePattern?: string;
  ttlPreservation: boolean;
}

// Authentication configuration (Cognito → Keycloak)
export interface AuthConfig extends BaseServiceConfig {
  sourceType: 'cognito' | 'azure-ad' | 'firebase-auth';
  userPoolId: string;
  migrateUsers: boolean;
  migrateGroups: boolean;
  migrateRoles: boolean;
  passwordPolicy: 'reset' | 'preserve-hash' | 'send-reset-email';
  mfaHandling: 'disable' | 'preserve' | 'require-reconfigure';
}

// Secrets configuration (Secrets Manager, SSM → Vault)
export interface SecretsConfig extends BaseServiceConfig {
  sourceType: 'secrets-manager' | 'ssm-parameter-store' | 'azure-keyvault';
  secretPaths: string[];
  targetPath: string;
  encryption: boolean;
}

// DNS configuration (Route53 → CoreDNS/external DNS)
export interface DNSConfig extends BaseServiceConfig {
  sourceType: 'route53' | 'azure-dns' | 'cloud-dns';
  hostedZones: string[];
  exportFormat: 'zone-file' | 'json';
  targetProvider?: string;
}

// Lambda/Functions configuration (Lambda → Docker containers)
export interface FunctionsConfig extends BaseServiceConfig {
  sourceType: 'lambda' | 'azure-functions' | 'cloud-functions';
  functions: FunctionMigration[];
  includeEnvironmentVariables: boolean;
  includeLayers: boolean;
}

export interface FunctionMigration {
  functionArn: string;
  functionName: string;
  targetContainerName: string;
  runtime: string;
}

// ============================================================================
// Migration Categories
// ============================================================================

export type MigrationCategory =
  | 'database'
  | 'storage'
  | 'queue'
  | 'cache'
  | 'auth'
  | 'secrets'
  | 'dns'
  | 'functions';

export const MIGRATION_CATEGORIES: MigrationCategory[] = [
  'database',
  'storage',
  'queue',
  'cache',
  'auth',
  'secrets',
  'dns',
  'functions',
];

export const CATEGORY_LABELS: Record<MigrationCategory, string> = {
  database: 'Databases',
  storage: 'Storage',
  queue: 'Queues & Events',
  cache: 'Caching',
  auth: 'Authentication',
  secrets: 'Secrets & Config',
  dns: 'DNS',
  functions: 'Functions',
};

export const CATEGORY_DESCRIPTIONS: Record<MigrationCategory, string> = {
  database: 'Migrate data from RDS, DynamoDB, Aurora to PostgreSQL, MySQL, ScyllaDB',
  storage: 'Transfer files from S3, Azure Blob, GCS to MinIO',
  queue: 'Migrate message queues from SQS, SNS to RabbitMQ, NATS',
  cache: 'Export cache data from ElastiCache to Redis/Valkey',
  auth: 'Migrate users and groups from Cognito, Azure AD to Keycloak',
  secrets: 'Transfer secrets from Secrets Manager, SSM to Vault',
  dns: 'Export DNS zones from Route53, Azure DNS',
  functions: 'Package Lambda functions as Docker containers',
};

// ============================================================================
// Aggregate Configuration Type
// ============================================================================

export interface MigrationConfiguration {
  // Source cloud credentials (from infrastructure discovery)
  sourceCredentials: SourceCredentials;

  // Per-category configurations
  database: DatabaseConfig;
  storage: StorageConfig;
  queue: QueueConfig;
  cache: CacheConfig;
  auth: AuthConfig;
  secrets: SecretsConfig;
  dns: DNSConfig;
  functions: FunctionsConfig;

  // Global options
  options: MigrationOptions;
}

export interface SourceCredentials {
  provider: 'aws' | 'gcp' | 'azure';

  // AWS
  awsAccessKeyId?: string;
  awsSecretAccessKey?: string;
  awsRegion?: string;

  // GCP
  gcpProjectId?: string;
  gcpServiceAccountJson?: string;

  // Azure
  azureSubscriptionId?: string;
  azureTenantId?: string;
  azureClientId?: string;
  azureClientSecret?: string;
}

export interface MigrationOptions {
  dryRun: boolean;
  continueOnError: boolean;
  logLevel: 'debug' | 'info' | 'warn' | 'error';
  maxConcurrentTasks: number;
  verifyAfterMigration: boolean;
  createBackup: boolean;
}

// ============================================================================
// Default Configurations
// ============================================================================

export const DEFAULT_DATABASE_CONFIG: DatabaseConfig = {
  enabled: true,
  sourceType: 'rds',
  connectionString: '',
  database: '',
  tables: [],
  batchSize: 1000,
  parallelWorkers: 4,
  includeSchema: true,
  truncateBeforeImport: false,
};

export const DEFAULT_EBS_CONFIG: EbsConfig = {
  enabled: false,
  volumes: [],
  outputDirectory: '/data/volumes',
  createSnapshots: true,
  encryptionEnabled: false,
};

export const DEFAULT_EFS_CONFIG: EfsConfig = {
  enabled: false,
  fileSystems: [],
  nfsServerImage: 'itsthenetwork/nfs-server-alpine:12',
  exportOptions: 'rw,sync,no_subtree_check,no_root_squash',
  syncMethod: 'rsync',
};

export const DEFAULT_STORAGE_CONFIG: StorageConfig = {
  enabled: true,
  sourceType: 's3',
  buckets: [],
  preserveMetadata: true,
  preserveVersions: false,
  ebs: DEFAULT_EBS_CONFIG,
  efs: DEFAULT_EFS_CONFIG,
};

export const DEFAULT_QUEUE_CONFIG: QueueConfig = {
  enabled: true,
  sourceType: 'sqs',
  queues: [],
  migrateDeadLetterQueues: true,
  preserveMessageAttributes: true,
};

export const DEFAULT_CACHE_CONFIG: CacheConfig = {
  enabled: true,
  sourceType: 'elasticache-redis',
  endpoint: '',
  databases: [0],
  ttlPreservation: true,
};

export const DEFAULT_AUTH_CONFIG: AuthConfig = {
  enabled: true,
  sourceType: 'cognito',
  userPoolId: '',
  migrateUsers: true,
  migrateGroups: true,
  migrateRoles: true,
  passwordPolicy: 'send-reset-email',
  mfaHandling: 'require-reconfigure',
};

export const DEFAULT_SECRETS_CONFIG: SecretsConfig = {
  enabled: true,
  sourceType: 'secrets-manager',
  secretPaths: [],
  targetPath: '/secrets',
  encryption: true,
};

export const DEFAULT_DNS_CONFIG: DNSConfig = {
  enabled: true,
  sourceType: 'route53',
  hostedZones: [],
  exportFormat: 'zone-file',
};

export const DEFAULT_FUNCTIONS_CONFIG: FunctionsConfig = {
  enabled: true,
  sourceType: 'lambda',
  functions: [],
  includeEnvironmentVariables: true,
  includeLayers: true,
};

export const DEFAULT_MIGRATION_OPTIONS: MigrationOptions = {
  dryRun: false,
  continueOnError: true,
  logLevel: 'info',
  maxConcurrentTasks: 4,
  verifyAfterMigration: true,
  createBackup: true,
};

// ============================================================================
// API Request/Response Types
// ============================================================================

// Validation request/response
export interface ValidateMigrationRequest {
  configuration: MigrationConfiguration;
}

export interface ValidationResult {
  category: MigrationCategory;
  valid: boolean;
  errors: ValidationError[];
  warnings: ValidationWarning[];
}

export interface ValidationError {
  field: string;
  message: string;
  code: string;
}

export interface ValidationWarning {
  field: string;
  message: string;
}

export interface ValidateMigrationResponse {
  valid: boolean;
  results: ValidationResult[];
  estimatedDuration: string;
  estimatedDataSize: string;
}

// Execution request/response
export interface ExecuteMigrationRequest {
  configuration: MigrationConfiguration;
}

export interface ExecuteMigrationResponse {
  migrationId: string;
  status: 'started';
}

// Progress tracking
export interface MigrationProgress {
  migrationId: string;
  status: MigrationStatus;
  overallProgress: number;
  currentCategory: MigrationCategory | null;
  categoryProgress: Record<MigrationCategory, CategoryProgress>;
  startedAt: string;
  estimatedCompletion: string | null;
  error: string | null;
}

export type MigrationStatus =
  | 'pending'
  | 'running'
  | 'paused'
  | 'completed'
  | 'failed'
  | 'cancelled';

export interface CategoryProgress {
  status: MigrationStatus;
  progress: number;
  itemsTotal: number;
  itemsCompleted: number;
  bytesTotal: number;
  bytesTransferred: number;
  currentItem: string | null;
  errors: string[];
}

// SSE Event types
export interface MigrationPhaseEvent {
  type: 'phase';
  category: MigrationCategory;
  phase: string;
  index: number;
  total: number;
}

export interface MigrationProgressEvent {
  type: 'progress';
  category: MigrationCategory;
  progress: number;
  itemsCompleted: number;
  itemsTotal: number;
  bytesTransferred: number;
  bytesTotal: number;
  currentItem: string | null;
}

export interface MigrationLogEvent {
  type: 'log';
  timestamp: string;
  level: 'debug' | 'info' | 'warn' | 'error';
  category: MigrationCategory | null;
  message: string;
}

export interface MigrationCompleteEvent {
  type: 'complete';
  migrationId: string;
  duration: string;
  summary: MigrationSummary;
}

export interface MigrationSummary {
  categoriesCompleted: MigrationCategory[];
  totalItemsMigrated: number;
  totalBytesMigrated: number;
  errors: MigrationErrorSummary[];
  warnings: string[];
}

export interface MigrationErrorSummary {
  category: MigrationCategory;
  item: string;
  error: string;
}

export interface MigrationErrorEvent {
  type: 'error';
  migrationId: string;
  category: MigrationCategory | null;
  message: string;
  recoverable: boolean;
}

export type MigrationEvent =
  | MigrationPhaseEvent
  | MigrationProgressEvent
  | MigrationLogEvent
  | MigrationCompleteEvent
  | MigrationErrorEvent;

// Cancel request
export interface CancelMigrationRequest {
  migrationId: string;
  graceful: boolean;
}

export interface CancelMigrationResponse {
  success: boolean;
  message: string;
}

// Status request
export interface GetMigrationStatusRequest {
  migrationId: string;
}

export type GetMigrationStatusResponse = MigrationProgress;
