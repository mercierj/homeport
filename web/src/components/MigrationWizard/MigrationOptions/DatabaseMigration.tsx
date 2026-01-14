import { useState, useMemo } from 'react';
import { Database, Zap, AlertTriangle, Info } from 'lucide-react';
import { ServiceMigrationCard } from '../ServiceMigrationCard';
import { useMigrationConfigStore } from '@/stores/migration-config';
import type { DatabaseConfig, CacheConfig } from '../types';
import type { Resource } from '@/lib/migrate-api';

// Resource type matchers for filtering which cards to show
const RDS_TYPES = ['aws_db_instance', 'aws_rds_cluster', 'google_sql_database_instance', 'azurerm_postgresql_server', 'azurerm_mysql_server'];
const DYNAMODB_TYPES = ['aws_dynamodb_table', 'google_firestore_database', 'google_spanner_instance', 'azurerm_cosmosdb_account'];
const ELASTICACHE_TYPES = ['aws_elasticache_cluster', 'aws_elasticache_replication_group', 'google_redis_instance', 'azurerm_redis_cache'];
const DOCUMENTDB_TYPES = ['aws_docdb_cluster', 'azurerm_cosmosdb_mongo_database'];

// ============================================================================
// Helper Components
// ============================================================================

interface FormLabelProps {
  label: string;
  htmlFor?: string;
  required?: boolean;
  children: React.ReactNode;
}

function FormField({ label, htmlFor, required, children }: FormLabelProps) {
  return (
    <div className="space-y-1">
      <label
        htmlFor={htmlFor}
        className="block text-sm font-medium text-gray-700"
      >
        {label}
        {required && <span className="text-error ml-1">*</span>}
      </label>
      {children}
    </div>
  );
}

interface WarningBoxProps {
  children: React.ReactNode;
}

function WarningBox({ children }: WarningBoxProps) {
  return (
    <div className="flex items-start gap-2 p-3 bg-warning/10 border border-warning/50 rounded-md">
      <AlertTriangle className="w-4 h-4 text-warning flex-shrink-0 mt-0.5" />
      <span className="text-sm text-warning">{children}</span>
    </div>
  );
}

interface InfoBoxProps {
  children: React.ReactNode;
}

function InfoBox({ children }: InfoBoxProps) {
  return (
    <div className="flex items-start gap-2 p-3 bg-info/10 border border-info/50 rounded-md">
      <Info className="w-4 h-4 text-info flex-shrink-0 mt-0.5" />
      <span className="text-sm text-info">{children}</span>
    </div>
  );
}

// ============================================================================
// RDS Migration Section
// ============================================================================

interface RDSMigrationSectionProps {
  config: DatabaseConfig;
  onConfigChange: (config: Partial<DatabaseConfig>) => void;
}

function RDSMigrationSection({ config, onConfigChange }: RDSMigrationSectionProps) {
  const [allTables, setAllTables] = useState(config.tables.length === 0);
  const [tablesInput, setTablesInput] = useState(config.tables.join(', '));

  const handleAllTablesChange = (checked: boolean) => {
    setAllTables(checked);
    if (checked) {
      onConfigChange({ tables: [] });
      setTablesInput('');
    }
  };

  const handleTablesInputChange = (value: string) => {
    setTablesInput(value);
    const tables = value
      .split(',')
      .map((t) => t.trim())
      .filter((t) => t.length > 0);
    onConfigChange({ tables });
  };

  return (
    <div className="space-y-4">
      {/* Source Type Selector */}
      <FormField label="Source Database Type" htmlFor="rds-source-type" required>
        <select
          id="rds-source-type"
          value={config.sourceType === 'rds' || config.sourceType === 'aurora' ? 'postgresql' : 'mysql'}
          onChange={() => {
            // Source type in config is 'rds' | 'aurora' | 'dynamodb' | 'documentdb'
            // We use this field to track the engine type for display purposes
            // The actual source type remains 'rds'
          }}
          className="w-full px-3 py-2 border border-input rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
        >
          <option value="postgresql">PostgreSQL</option>
          <option value="mysql">MySQL</option>
        </select>
      </FormField>

      {/* Connection String */}
      <FormField label="Connection String" htmlFor="rds-connection" required>
        <input
          id="rds-connection"
          type="text"
          value={config.connectionString}
          onChange={(e) => onConfigChange({ connectionString: e.target.value })}
          placeholder="postgresql://user:password@host:5432/database"
          className="w-full px-3 py-2 border border-input rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
        />
      </FormField>

      {/* Database Name */}
      <FormField label="Database Name" htmlFor="rds-database" required>
        <input
          id="rds-database"
          type="text"
          value={config.database}
          onChange={(e) => onConfigChange({ database: e.target.value })}
          placeholder="my_database"
          className="w-full px-3 py-2 border border-input rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
        />
      </FormField>

      {/* Tables Selection */}
      <div className="space-y-2">
        <label className="flex items-center gap-2 cursor-pointer">
          <input
            type="checkbox"
            checked={allTables}
            onChange={(e) => handleAllTablesChange(e.target.checked)}
            className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
          />
          <span className="text-sm font-medium text-gray-700">
            Migrate all tables
          </span>
        </label>

        {!allTables && (
          <FormField label="Tables to Migrate" htmlFor="rds-tables">
            <input
              id="rds-tables"
              type="text"
              value={tablesInput}
              onChange={(e) => handleTablesInputChange(e.target.value)}
              placeholder="users, orders, products (comma-separated)"
              className="w-full px-3 py-2 border border-input rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
            />
            <p className="text-xs text-gray-500 mt-1">
              Enter table names separated by commas
            </p>
          </FormField>
        )}
      </div>

      {/* Performance Options */}
      <div className="grid grid-cols-2 gap-4">
        <FormField label="Batch Size" htmlFor="rds-batch-size">
          <input
            id="rds-batch-size"
            type="number"
            min={100}
            max={100000}
            value={config.batchSize}
            onChange={(e) =>
              onConfigChange({
                batchSize: Math.max(100, Math.min(100000, parseInt(e.target.value) || 1000)),
              })
            }
            className="w-full px-3 py-2 border border-input rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
          />
          <p className="text-xs text-gray-500 mt-1">
            Rows per batch (100-100,000)
          </p>
        </FormField>

        <FormField label="Parallel Workers" htmlFor="rds-workers">
          <input
            id="rds-workers"
            type="number"
            min={1}
            max={16}
            value={config.parallelWorkers}
            onChange={(e) =>
              onConfigChange({
                parallelWorkers: Math.max(1, Math.min(16, parseInt(e.target.value) || 4)),
              })
            }
            className="w-full px-3 py-2 border border-input rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
          />
          <p className="text-xs text-gray-500 mt-1">
            Concurrent workers (1-16)
          </p>
        </FormField>
      </div>

      {/* Schema and Data Options */}
      <div className="space-y-3 pt-2">
        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={config.includeSchema}
            onChange={(e) => onConfigChange({ includeSchema: e.target.checked })}
            className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
          />
          <div>
            <span className="text-sm font-medium text-gray-700">Include Schema</span>
            <p className="text-xs text-gray-500">
              Migrate table structures, indexes, and constraints
            </p>
          </div>
        </label>

        <div className="space-y-2">
          <label className="flex items-center gap-3 cursor-pointer">
            <input
              type="checkbox"
              checked={config.truncateBeforeImport}
              onChange={(e) => onConfigChange({ truncateBeforeImport: e.target.checked })}
              className="w-4 h-4 text-red-600 rounded border-input focus:ring-red-500"
            />
            <div>
              <span className="text-sm font-medium text-gray-700">
                Truncate Before Import
              </span>
              <p className="text-xs text-gray-500">
                Clear target tables before importing data
              </p>
            </div>
          </label>

          {config.truncateBeforeImport && (
            <WarningBox>
              This will permanently delete all existing data in the target tables before migration.
              Make sure you have a backup!
            </WarningBox>
          )}
        </div>
      </div>
    </div>
  );
}

// ============================================================================
// DynamoDB Migration Section
// ============================================================================

interface DynamoDBConfig {
  enabled: boolean;
  tables: string;
  targetKeyspace: string;
  batchSize: number;
}

interface DynamoDBMigrationSectionProps {
  config: DynamoDBConfig;
  onConfigChange: (config: Partial<DynamoDBConfig>) => void;
}

function DynamoDBMigrationSection({ config, onConfigChange }: DynamoDBMigrationSectionProps) {
  return (
    <div className="space-y-4">
      <InfoBox>
        DynamoDB tables will be converted to ScyllaDB data models. Some schema adjustments may be
        required due to differences between DynamoDB and ScyllaDB architectures.
      </InfoBox>

      {/* Tables Input */}
      <FormField label="DynamoDB Tables" htmlFor="dynamodb-tables" required>
        <input
          id="dynamodb-tables"
          type="text"
          value={config.tables}
          onChange={(e) => onConfigChange({ tables: e.target.value })}
          placeholder="users, orders, sessions (comma-separated)"
          className="w-full px-3 py-2 border border-input rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
        />
        <p className="text-xs text-gray-500 mt-1">
          Enter DynamoDB table names separated by commas
        </p>
      </FormField>

      {/* Target Keyspace */}
      <FormField label="Target Keyspace Name" htmlFor="dynamodb-keyspace" required>
        <input
          id="dynamodb-keyspace"
          type="text"
          value={config.targetKeyspace}
          onChange={(e) => onConfigChange({ targetKeyspace: e.target.value })}
          placeholder="my_keyspace"
          className="w-full px-3 py-2 border border-input rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
        />
      </FormField>

      {/* Batch Size */}
      <FormField label="Batch Size" htmlFor="dynamodb-batch-size">
        <input
          id="dynamodb-batch-size"
          type="number"
          min={100}
          max={10000}
          value={config.batchSize}
          onChange={(e) =>
            onConfigChange({
              batchSize: Math.max(100, Math.min(10000, parseInt(e.target.value) || 1000)),
            })
          }
          className="w-32 px-3 py-2 border border-input rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
        />
        <p className="text-xs text-gray-500 mt-1">
          Items per batch (100-10,000)
        </p>
      </FormField>
    </div>
  );
}

// ============================================================================
// ElastiCache Redis Migration Section
// ============================================================================

interface ElastiCacheMigrationSectionProps {
  config: CacheConfig;
  onConfigChange: (config: Partial<CacheConfig>) => void;
}

function ElastiCacheMigrationSection({ config, onConfigChange }: ElastiCacheMigrationSectionProps) {
  // Generate database options (0-15)
  const databaseOptions = Array.from({ length: 16 }, (_, i) => i);

  return (
    <div className="space-y-4">
      {/* Endpoint */}
      <FormField label="Redis Endpoint" htmlFor="elasticache-endpoint" required>
        <input
          id="elasticache-endpoint"
          type="text"
          value={config.endpoint}
          onChange={(e) => onConfigChange({ endpoint: e.target.value })}
          placeholder="my-cluster.cache.amazonaws.com:6379"
          className="w-full px-3 py-2 border border-input rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
        />
      </FormField>

      {/* Database Numbers Multi-Select */}
      <FormField label="Database Numbers" htmlFor="elasticache-databases">
        <div className="flex flex-wrap gap-2">
          {databaseOptions.map((db) => (
            <button
              key={db}
              type="button"
              onClick={() => {
                const newDatabases = config.databases.includes(db)
                  ? config.databases.filter((d) => d !== db)
                  : [...config.databases, db].sort((a, b) => a - b);
                onConfigChange({ databases: newDatabases.length > 0 ? newDatabases : [0] });
              }}
              className={`px-3 py-1.5 text-sm rounded-md border transition-colors ${
                config.databases.includes(db)
                  ? 'bg-primary/10 border-primary text-primary'
                  : 'bg-white border-input text-gray-700 hover:bg-muted'
              }`}
            >
              {db}
            </button>
          ))}
        </div>
        <p className="text-xs text-gray-500 mt-1">
          Selected: {config.databases.join(', ')} (click to toggle)
        </p>
      </FormField>

      {/* Key Pattern Filter */}
      <FormField label="Key Pattern Filter" htmlFor="elasticache-key-pattern">
        <input
          id="elasticache-key-pattern"
          type="text"
          value={config.keyPattern || ''}
          onChange={(e) => onConfigChange({ keyPattern: e.target.value || undefined })}
          placeholder="user:* (glob pattern, leave empty for all)"
          className="w-full px-3 py-2 border border-input rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
        />
        <p className="text-xs text-gray-500 mt-1">
          Only migrate keys matching this pattern
        </p>
      </FormField>

      {/* Exclude Pattern */}
      <FormField label="Exclude Pattern" htmlFor="elasticache-exclude-pattern">
        <input
          id="elasticache-exclude-pattern"
          type="text"
          value={config.excludePattern || ''}
          onChange={(e) => onConfigChange({ excludePattern: e.target.value || undefined })}
          placeholder="temp:* (glob pattern to exclude)"
          className="w-full px-3 py-2 border border-input rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
        />
        <p className="text-xs text-gray-500 mt-1">
          Skip keys matching this pattern
        </p>
      </FormField>

      {/* TTL Preservation */}
      <label className="flex items-center gap-3 cursor-pointer">
        <input
          type="checkbox"
          checked={config.ttlPreservation}
          onChange={(e) => onConfigChange({ ttlPreservation: e.target.checked })}
          className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
        />
        <div>
          <span className="text-sm font-medium text-gray-700">Preserve TTL</span>
          <p className="text-xs text-gray-500">
            Keep original expiration times on migrated keys
          </p>
        </div>
      </label>
    </div>
  );
}

// ============================================================================
// DocumentDB Migration Section (Placeholder)
// ============================================================================

function DocumentDBMigrationSection() {
  return (
    <div className="flex flex-col items-center justify-center py-8 text-center">
      <div className="w-16 h-16 rounded-full bg-muted flex items-center justify-center mb-4">
        <Database className="w-8 h-8 text-muted-foreground/60" />
      </div>
      <h4 className="text-lg font-medium text-gray-700 mb-2">Coming Soon</h4>
      <p className="text-sm text-gray-500 max-w-sm">
        DocumentDB to MongoDB migration support is under development and will be available in a
        future release.
      </p>
    </div>
  );
}

// ============================================================================
// Main DatabaseMigration Component
// ============================================================================

interface DatabaseMigrationProps {
  resources: Resource[];
}

export function DatabaseMigration({ resources = [] }: DatabaseMigrationProps) {
  const {
    database,
    cache,
    setDatabaseConfig,
    setCacheConfig,
  } = useMigrationConfigStore();

  // Determine which service cards to show based on discovered resources
  const { hasRDS, hasDynamoDB, hasElastiCache, hasDocumentDB } = useMemo(() => ({
    hasRDS: resources.some(r => RDS_TYPES.includes(r.type)),
    hasDynamoDB: resources.some(r => DYNAMODB_TYPES.includes(r.type)),
    hasElastiCache: resources.some(r => ELASTICACHE_TYPES.includes(r.type)),
    hasDocumentDB: resources.some(r => DOCUMENTDB_TYPES.includes(r.type)),
  }), [resources]);

  // Local state for DynamoDB config (since it's not part of main types)
  const [dynamoDBConfig, setDynamoDBConfig] = useState<DynamoDBConfig>({
    enabled: false,
    tables: '',
    targetKeyspace: '',
    batchSize: 1000,
  });

  // Local state for DocumentDB enabled
  const [documentDBEnabled, setDocumentDBEnabled] = useState(false);

  const handleDynamoDBChange = (config: Partial<DynamoDBConfig>) => {
    setDynamoDBConfig((prev) => ({ ...prev, ...config }));
  };

  // If no database resources discovered, show empty state
  if (!hasRDS && !hasDynamoDB && !hasElastiCache && !hasDocumentDB) {
    return (
      <div className="text-center py-8 text-muted-foreground">
        <Database className="w-12 h-12 mx-auto mb-3 opacity-50" />
        <p className="font-medium">No database resources discovered</p>
        <p className="text-sm mt-1">RDS, DynamoDB, ElastiCache, or DocumentDB resources will appear here when detected.</p>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {/* RDS PostgreSQL/MySQL Section - only show if RDS resources discovered */}
      {hasRDS && (
        <ServiceMigrationCard
          title="Amazon RDS → PostgreSQL/MySQL"
          description="Migrate RDS databases to self-hosted instances"
          icon={Database}
          enabled={database.enabled && (database.sourceType === 'rds' || database.sourceType === 'aurora')}
          onToggle={(enabled) =>
            setDatabaseConfig({ enabled, sourceType: enabled ? 'rds' : database.sourceType })
          }
          defaultExpanded={true}
        >
          <RDSMigrationSection
            config={database}
            onConfigChange={setDatabaseConfig}
          />
        </ServiceMigrationCard>
      )}

      {/* DynamoDB Section - only show if DynamoDB resources discovered */}
      {hasDynamoDB && (
        <ServiceMigrationCard
          title="Amazon DynamoDB → ScyllaDB"
          description="Migrate NoSQL tables to ScyllaDB"
          icon={Database}
          enabled={dynamoDBConfig.enabled}
          onToggle={(enabled) => handleDynamoDBChange({ enabled })}
          defaultExpanded={true}
        >
          <DynamoDBMigrationSection
            config={dynamoDBConfig}
            onConfigChange={handleDynamoDBChange}
          />
        </ServiceMigrationCard>
      )}

      {/* ElastiCache Redis Section - only show if ElastiCache resources discovered */}
      {hasElastiCache && (
        <ServiceMigrationCard
          title="ElastiCache Redis → Redis"
          description="Migrate Redis cache clusters"
          icon={Zap}
          enabled={cache.enabled}
          onToggle={(enabled) => setCacheConfig({ enabled })}
          defaultExpanded={true}
        >
          <ElastiCacheMigrationSection
            config={cache}
            onConfigChange={setCacheConfig}
          />
        </ServiceMigrationCard>
      )}

      {/* DocumentDB Section - only show if DocumentDB resources discovered */}
      {hasDocumentDB && (
        <ServiceMigrationCard
          title="DocumentDB → MongoDB"
          description="Migrate DocumentDB to MongoDB"
          icon={Database}
          enabled={documentDBEnabled}
          onToggle={setDocumentDBEnabled}
          defaultExpanded={true}
        >
          <DocumentDBMigrationSection />
        </ServiceMigrationCard>
      )}
    </div>
  );
}

export default DatabaseMigration;
