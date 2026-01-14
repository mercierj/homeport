import { useEffect, useRef } from 'react';
import { Settings2, ChevronLeft, ChevronRight, AlertCircle } from 'lucide-react';
import { useMigrationConfigStore } from '@/stores/migration-config';
import { ServiceCategoryTabs } from './ServiceCategoryTabs';
import type { MigrationCategory, MigrationOptions, BucketMigration, QueueMigration, FunctionMigration } from './types';
import { CATEGORY_LABELS, CATEGORY_DESCRIPTIONS } from './types';
import { Button } from '@/components/ui/button';
import type { Resource } from '@/lib/migrate-api';
import { StorageMigration } from './MigrationOptions/StorageMigration';
import { DatabaseMigration } from './MigrationOptions/DatabaseMigration';
import { ComputeMigration } from './MigrationOptions/ComputeMigration';
import { MessagingMigration } from './MigrationOptions/MessagingMigration';
import { NetworkingMigration } from './MigrationOptions/NetworkingMigration';
import { SecurityMigration } from './MigrationOptions/SecurityMigration';

// ============================================================================
// Props Interface
// ============================================================================

interface MigrationConfigStepProps {
  onBack: () => void;
  onNext: () => void;
  resources: Resource[];
}

// ============================================================================
// Global Options Panel Component
// ============================================================================

interface GlobalOptionsPanelOptions {
  dryRun: boolean;
  continueOnError: boolean;
  maxConcurrentTasks: number;
  verifyAfterMigration: boolean;
  createBackup: boolean;
}

interface GlobalOptionsPanelProps {
  options: GlobalOptionsPanelOptions;
  onOptionsChange: (options: Partial<MigrationOptions>) => void;
}

function GlobalOptionsPanel({ options, onOptionsChange }: GlobalOptionsPanelProps) {
  return (
    <div className="mt-6 p-4 bg-muted border border-border rounded-lg">
      <div className="flex items-center gap-2 mb-4">
        <Settings2 className="w-5 h-5 text-gray-600" />
        <h3 className="font-medium text-gray-900">Global Options</h3>
      </div>

      <div className="grid grid-cols-2 gap-4">
        {/* Dry Run */}
        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={options.dryRun}
            onChange={(e) => onOptionsChange({ dryRun: e.target.checked })}
            className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
          />
          <div>
            <span className="text-sm font-medium text-gray-700">Dry Run</span>
            <p className="text-xs text-gray-500">Preview changes without executing</p>
          </div>
        </label>

        {/* Continue on Error */}
        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={options.continueOnError}
            onChange={(e) => onOptionsChange({ continueOnError: e.target.checked })}
            className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
          />
          <div>
            <span className="text-sm font-medium text-gray-700">Continue on Error</span>
            <p className="text-xs text-gray-500">Skip failed items and continue</p>
          </div>
        </label>

        {/* Verify After Migration */}
        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={options.verifyAfterMigration}
            onChange={(e) => onOptionsChange({ verifyAfterMigration: e.target.checked })}
            className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
          />
          <div>
            <span className="text-sm font-medium text-gray-700">Verify After Migration</span>
            <p className="text-xs text-gray-500">Validate migrated data integrity</p>
          </div>
        </label>

        {/* Create Backup */}
        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={options.createBackup}
            onChange={(e) => onOptionsChange({ createBackup: e.target.checked })}
            className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
          />
          <div>
            <span className="text-sm font-medium text-gray-700">Create Backup</span>
            <p className="text-xs text-gray-500">Backup before migration</p>
          </div>
        </label>

        {/* Max Concurrent Tasks */}
        <div className="col-span-2">
          <label className="block text-sm font-medium text-gray-700 mb-1">
            Max Concurrent Tasks
          </label>
          <input
            type="number"
            min={1}
            max={16}
            value={options.maxConcurrentTasks}
            onChange={(e) =>
              onOptionsChange({
                maxConcurrentTasks: Math.max(1, Math.min(16, parseInt(e.target.value) || 1)),
              })
            }
            className="w-24 px-3 py-2 border border-input rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
          />
          <p className="text-xs text-gray-500 mt-1">
            Number of parallel migration workers (1-16)
          </p>
        </div>
      </div>
    </div>
  );
}

// ============================================================================
// Category Content Panel Component
// ============================================================================

interface CategoryContentPanelProps {
  category: MigrationCategory;
  resources: Resource[];
}

function CategoryContentPanel({ category, resources }: CategoryContentPanelProps) {
  const label = CATEGORY_LABELS[category];
  const description = CATEGORY_DESCRIPTIONS[category];

  const renderCategoryContent = () => {
    switch (category) {
      case 'database':
        return <DatabaseMigration resources={resources} />;
      case 'storage':
        return <StorageMigration resources={resources} />;
      case 'queue':
        return <MessagingMigration resources={resources} />;
      case 'cache':
        return <DatabaseMigration resources={resources} />; // Cache is part of DatabaseMigration
      case 'auth':
        return <SecurityMigration resources={resources} filter="auth" />;
      case 'secrets':
        return <SecurityMigration resources={resources} filter="secrets" />;
      case 'dns':
        return <NetworkingMigration resources={resources} />;
      case 'functions':
        return <ComputeMigration resources={resources} />;
      default:
        return null;
    }
  };

  return (
    <div className="bg-white border border-gray-200 rounded-lg p-6">
      <div className="mb-4">
        <h3 className="text-lg font-semibold text-gray-900">{label}</h3>
        <p className="text-sm text-gray-600 mt-1">{description}</p>
      </div>

      {renderCategoryContent()}
    </div>
  );
}

// ============================================================================
// Main Component
// ============================================================================

export function MigrationConfigStep({
  onBack,
  onNext,
  resources,
}: MigrationConfigStepProps) {
  const {
    activeCategory,
    options,
    storage,
    queue,
    functions,
    dns,
    auth,
    secrets,
    setActiveCategory,
    setOptions,
    setStorageConfig,
    setQueueConfig,
    setDatabaseConfig,
    setCacheConfig,
    setFunctionsConfig,
    setDNSConfig,
    setAuthConfig,
    setSecretsConfig,
    getEnabledCategories,
  } = useMigrationConfigStore();

  // Track if we've already populated from these resources
  const populatedRef = useRef(false);
  const resourcesLengthRef = useRef(0);

  // Auto-populate migration configs from discovered resources
  useEffect(() => {
    // Only populate once per resource set (avoid re-populating on every render)
    if (populatedRef.current && resourcesLengthRef.current === resources.length) {
      return;
    }
    populatedRef.current = true;
    resourcesLengthRef.current = resources.length;

    if (resources.length === 0) return;

    // =========================================================================
    // STORAGE: S3 Buckets → MinIO
    // =========================================================================
    const s3Buckets = resources.filter(r =>
      r.type === 'aws_s3_bucket' ||
      r.type === 'google_storage_bucket' ||
      r.type === 'azurerm_storage_account'
    );
    if (s3Buckets.length > 0 && storage.buckets.length === 0) {
      const bucketMappings: BucketMigration[] = s3Buckets.map(bucket => ({
        sourceBucket: bucket.name,
        targetBucket: bucket.name, // Same name by default
        prefix: '',
      }));
      setStorageConfig({ buckets: bucketMappings, enabled: true });
    }

    // =========================================================================
    // QUEUES: SQS → RabbitMQ
    // =========================================================================
    const sqsQueues = resources.filter(r =>
      r.type === 'aws_sqs_queue' ||
      r.type === 'google_pubsub_topic' ||
      r.type === 'google_pubsub_subscription' ||
      r.type === 'azurerm_servicebus_queue'
    );
    if (sqsQueues.length > 0 && queue.queues.length === 0) {
      const queueMappings: QueueMigration[] = sqsQueues.map(q => ({
        sourceQueue: q.name,
        targetQueue: q.name, // Same name by default
      }));
      setQueueConfig({ queues: queueMappings, enabled: true });
    }

    // =========================================================================
    // DATABASES: RDS/Aurora → PostgreSQL/MySQL
    // =========================================================================
    const databases = resources.filter(r =>
      r.type === 'aws_db_instance' ||
      r.type === 'aws_rds_cluster' ||
      r.type === 'aws_dynamodb_table' ||
      r.type === 'google_sql_database_instance' ||
      r.type === 'google_spanner_instance' ||
      r.type === 'google_firestore_database' ||
      r.type === 'azurerm_postgresql_server' ||
      r.type === 'azurerm_mysql_server' ||
      r.type === 'azurerm_cosmosdb_account'
    );
    if (databases.length > 0) {
      setDatabaseConfig({ enabled: true });
    }

    // =========================================================================
    // CACHE: ElastiCache → Redis
    // =========================================================================
    const caches = resources.filter(r =>
      r.type === 'aws_elasticache_cluster' ||
      r.type === 'aws_elasticache_replication_group' ||
      r.type === 'google_redis_instance' ||
      r.type === 'azurerm_redis_cache'
    );
    if (caches.length > 0) {
      setCacheConfig({ enabled: true });
    }

    // =========================================================================
    // FUNCTIONS: Lambda → Docker Containers
    // =========================================================================
    const lambdas = resources.filter(r =>
      r.type === 'aws_lambda_function' ||
      r.type === 'google_cloudfunctions_function' ||
      r.type === 'google_cloudfunctions2_function' ||
      r.type === 'azurerm_function_app'
    );
    if (lambdas.length > 0 && functions.functions.length === 0) {
      const functionMappings: FunctionMigration[] = lambdas.map(fn => ({
        functionArn: fn.arn || '',
        functionName: fn.name,
        targetContainerName: fn.name.toLowerCase().replace(/[^a-z0-9-]/g, '-'),
        runtime: 'nodejs18.x', // Default, will be overridden by actual runtime
      }));
      setFunctionsConfig({ functions: functionMappings, enabled: true });
    }

    // =========================================================================
    // DNS: Route53 → CoreDNS
    // =========================================================================
    const dnsZones = resources.filter(r =>
      r.type === 'aws_route53_zone' ||
      r.type === 'google_dns_managed_zone' ||
      r.type === 'azurerm_dns_zone'
    );
    if (dnsZones.length > 0 && dns.hostedZones.length === 0) {
      setDNSConfig({
        hostedZones: dnsZones.map(z => z.name),
        enabled: true,
      });
    }

    // =========================================================================
    // AUTH: Cognito → Keycloak
    // =========================================================================
    const authResources = resources.filter(r =>
      r.type === 'aws_cognito_user_pool' ||
      r.type === 'google_identity_platform_config' ||
      r.type === 'azurerm_active_directory_b2c'
    );
    if (authResources.length > 0 && !auth.userPoolId) {
      const firstPool = authResources[0];
      setAuthConfig({
        userPoolId: firstPool.arn || firstPool.name,
        enabled: true,
      });
    }

    // =========================================================================
    // SECRETS: Secrets Manager → Vault
    // =========================================================================
    const secretResources = resources.filter(r =>
      r.type === 'aws_secretsmanager_secret' ||
      r.type === 'aws_ssm_parameter' ||
      r.type === 'google_secret_manager_secret' ||
      r.type === 'azurerm_key_vault_secret'
    );
    if (secretResources.length > 0 && secrets.secretPaths.length === 0) {
      setSecretsConfig({
        secretPaths: secretResources.map(s => s.name),
        enabled: true,
      });
    }
  }, [
    resources,
    storage.buckets.length,
    queue.queues.length,
    functions.functions.length,
    dns.hostedZones.length,
    auth.userPoolId,
    secrets.secretPaths.length,
    setStorageConfig,
    setQueueConfig,
    setDatabaseConfig,
    setCacheConfig,
    setFunctionsConfig,
    setDNSConfig,
    setAuthConfig,
    setSecretsConfig,
  ]);

  const enabledCategories = getEnabledCategories();
  const enabledCount = enabledCategories.length;
  const hasEnabledCategories = enabledCount > 0;

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="mb-6">
        <h2 className="text-2xl font-bold text-gray-900">Data Migration Configuration</h2>
        <p className="text-sm text-gray-600 mt-1">
          Configure migration settings for each service category. Enable categories and customize
          options to control how your data is migrated to self-hosted alternatives.
        </p>
      </div>

      {/* Main Content - Two Column Layout */}
      <div className="flex gap-6 flex-1 min-h-0">
        {/* Left Sidebar - Category Tabs */}
        <ServiceCategoryTabs
          activeCategory={activeCategory}
          onCategoryChange={setActiveCategory}
          enabledCategories={enabledCategories}
        />

        {/* Right Content Area */}
        <div className="flex-1 flex flex-col min-h-0 overflow-auto">
          {/* Category Configuration Panel */}
          <CategoryContentPanel category={activeCategory} resources={resources} />

          {/* Global Options Panel */}
          <GlobalOptionsPanel
            options={{
              dryRun: options.dryRun,
              continueOnError: options.continueOnError,
              maxConcurrentTasks: options.maxConcurrentTasks,
              verifyAfterMigration: options.verifyAfterMigration,
              createBackup: options.createBackup,
            }}
            onOptionsChange={setOptions}
          />
        </div>
      </div>

      {/* Footer Navigation */}
      <div className="mt-6 pt-4 border-t border-gray-200 flex items-center justify-between">
        {/* Left side - Status info */}
        <div className="flex items-center gap-2 text-sm text-gray-600">
          {hasEnabledCategories ? (
            <span>
              {enabledCount} {enabledCount === 1 ? 'category' : 'categories'} enabled
            </span>
          ) : (
            <div className="flex items-center gap-2 text-amber-600">
              <AlertCircle className="w-4 h-4" />
              <span>No categories enabled</span>
            </div>
          )}
        </div>

        {/* Right side - Navigation buttons */}
        <div className="flex items-center gap-3">
          <Button
            variant="outline"
            onClick={onBack}
            className="gap-2"
          >
            <ChevronLeft className="w-4 h-4" />
            Back to Review
          </Button>

          <Button
            onClick={onNext}
            disabled={!hasEnabledCategories}
            className="gap-2"
          >
            Validate Configuration
            <ChevronRight className="w-4 h-4" />
          </Button>
        </div>
      </div>
    </div>
  );
}

export default MigrationConfigStep;
