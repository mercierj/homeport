import { useMemo } from 'react';
import { Layers } from 'lucide-react';
import type { Resource } from '@/lib/migrate-api';

// Resource types that are NOT services (config, networking, IAM, etc.)
// These are excluded from consolidation preview since they don't become containers
const EXCLUDED_TYPES = [
  // Secrets - just configuration values
  'aws_secretsmanager_secret', 'google_secret_manager_secret', 'azurerm_key_vault', 'azurerm_key_vault_secret',
  // IAM - permissions, not containers
  'aws_iam_role', 'aws_iam_policy', 'aws_iam_user', 'google_project_iam_member', 'azurerm_role_assignment',
  // Networking - infrastructure, not containers
  'aws_vpc', 'aws_subnet', 'aws_security_group', 'aws_route_table', 'aws_internet_gateway',
  'google_compute_network', 'google_compute_subnetwork', 'google_compute_firewall',
  'azurerm_virtual_network', 'azurerm_subnet', 'azurerm_network_security_group',
  // DNS - just records
  'aws_route53_zone', 'aws_route53_record', 'google_dns_managed_zone', 'google_dns_record_set', 'azurerm_dns_zone',
  // Certificates
  'aws_acm_certificate', 'google_compute_managed_ssl_certificate', 'azurerm_app_service_certificate',
  // Observability - logging/monitoring config, not services
  'aws_cloudwatch_log_group', 'aws_cloudwatch_metric_alarm', 'google_logging_log_sink', 'google_monitoring_alert_policy',
];

// Stack type mapping for consolidation preview
export const STACK_TYPE_MAP: Record<string, { displayName: string; types: string[] }> = {
  database: {
    displayName: 'Database',
    types: ['aws_db_instance', 'aws_rds_cluster', 'google_sql_database_instance', 'azurerm_postgresql_server', 'azurerm_mysql_server', 'azurerm_mssql_server'],
  },
  cache: {
    displayName: 'Cache',
    types: ['aws_elasticache_cluster', 'aws_elasticache_replication_group', 'google_redis_instance', 'azurerm_redis_cache'],
  },
  messaging: {
    displayName: 'Messaging',
    types: ['aws_sqs_queue', 'aws_sns_topic', 'aws_kinesis_stream', 'google_pubsub_topic', 'google_pubsub_subscription', 'azurerm_servicebus_namespace', 'azurerm_servicebus_queue', 'azurerm_eventhub'],
  },
  storage: {
    displayName: 'Storage',
    types: ['aws_s3_bucket', 'google_storage_bucket', 'azurerm_storage_account', 'azurerm_storage_container'],
  },
  auth: {
    displayName: 'Authentication',
    types: ['aws_cognito_user_pool', 'google_identity_platform_config', 'azurerm_aadb2c_directory'],
  },
  compute: {
    displayName: 'Compute',
    types: ['aws_lambda_function', 'google_cloudfunctions_function', 'google_cloud_run_service', 'azurerm_function_app'],
  },
};

interface ConsolidationPreviewPanelProps {
  resources: Resource[];
}

export function ConsolidationPreviewPanel({ resources }: ConsolidationPreviewPanelProps) {
  // Calculate stack preview
  const preview = useMemo(() => {
    const stackCounts: Record<string, { count: number; resources: string[] }> = {};
    let passthroughCount = 0;
    const passthroughResources: string[] = [];
    let excludedCount = 0;

    for (const res of resources) {
      // Skip non-service resources (secrets, IAM, networking, etc.)
      if (EXCLUDED_TYPES.includes(res.type)) {
        excludedCount++;
        continue;
      }

      let foundStack = false;
      for (const [stackType, config] of Object.entries(STACK_TYPE_MAP)) {
        if (config.types.includes(res.type)) {
          if (!stackCounts[stackType]) {
            stackCounts[stackType] = { count: 0, resources: [] };
          }
          stackCounts[stackType].count++;
          stackCounts[stackType].resources.push(res.name);
          foundStack = true;
          break;
        }
      }
      if (!foundStack) {
        passthroughCount++;
        passthroughResources.push(res.name);
      }
    }

    // Calculate totals
    const stackEntries = Object.entries(stackCounts);
    const consolidatedServiceCount = stackEntries.length; // Each stack type becomes 1 service
    const totalServiceCount = consolidatedServiceCount + passthroughCount;
    const serviceResourceCount = resources.length - excludedCount;

    return {
      stacks: stackEntries.map(([type, data]) => ({
        type,
        displayName: STACK_TYPE_MAP[type]?.displayName || type,
        resourceCount: data.count,
        resources: data.resources,
      })),
      passthroughCount,
      passthroughResources,
      sourceCount: serviceResourceCount,
      excludedCount,
      serviceCount: totalServiceCount,
      reductionRatio: totalServiceCount > 0 ? serviceResourceCount / totalServiceCount : 1,
    };
  }, [resources]);

  return (
    <div className="border-t p-4 space-y-3">
      <div className="flex items-center gap-2">
        <Layers className="h-4 w-4 text-accent" />
        <span className="text-sm font-medium text-foreground">Consolidation Preview</span>
      </div>

      {/* Summary stats */}
      <div className="grid grid-cols-2 gap-2 text-center">
        <div className="bg-muted rounded-lg p-2">
          <div className="text-lg font-bold text-foreground">{preview.sourceCount}</div>
          <div className="text-xs text-muted-foreground">Service Resources</div>
        </div>
        <div className="bg-accent/10 rounded-lg p-2">
          <div className="text-lg font-bold text-accent">{preview.serviceCount}</div>
          <div className="text-xs text-muted-foreground">Containers</div>
        </div>
      </div>

      {preview.reductionRatio > 1 && (
        <div className="text-xs text-center text-accent font-medium">
          {preview.reductionRatio.toFixed(1)}x reduction in container count
        </div>
      )}

      {preview.excludedCount > 0 && (
        <div className="text-xs text-center text-muted-foreground">
          +{preview.excludedCount} config resources (secrets, IAM, networking)
        </div>
      )}

      {/* Stack breakdown */}
      <div className="space-y-2">
        <div className="text-xs font-medium text-muted-foreground uppercase">Stack Breakdown</div>
        {preview.stacks.map((stack) => (
          <div
            key={stack.type}
            className="flex items-center justify-between px-2 py-1.5 bg-accent/5 rounded text-sm"
          >
            <span className="text-foreground">{stack.displayName}</span>
            <span className="text-muted-foreground">
              {stack.resourceCount} {'->'} 1
            </span>
          </div>
        ))}
        {preview.passthroughCount > 0 && (
          <div className="flex items-center justify-between px-2 py-1.5 bg-muted rounded text-sm">
            <span className="text-muted-foreground">Passthrough</span>
            <span className="text-muted-foreground">
              {preview.passthroughCount} {'->'} {preview.passthroughCount}
            </span>
          </div>
        )}
      </div>
    </div>
  );
}
