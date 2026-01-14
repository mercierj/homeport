import { useState, useMemo } from 'react';
import { MessageSquare, Bell, Zap, Plus, Trash2, AlertTriangle, Info } from 'lucide-react';
import { ServiceMigrationCard } from '../ServiceMigrationCard';
import { useMigrationConfigStore } from '@/stores/migration-config';
import type { QueueMigration } from '../types';
import type { Resource } from '@/lib/migrate-api';

// Resource type matchers for filtering which cards to show
const SQS_TYPES = ['aws_sqs_queue', 'azurerm_servicebus_queue'];
const SNS_TYPES = ['aws_sns_topic', 'google_pubsub_topic'];
const EVENTBRIDGE_TYPES = ['aws_cloudwatch_event_rule', 'aws_cloudwatch_event_bus'];

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
// Queue Mapping Table Component
// ============================================================================

interface QueueMappingTableProps {
  queues: QueueMigration[];
  onQueuesChange: (queues: QueueMigration[]) => void;
}

function QueueMappingTable({ queues, onQueuesChange }: QueueMappingTableProps) {
  const [newQueue, setNewQueue] = useState<QueueMigration>({
    sourceQueue: '',
    targetQueue: '',
    targetExchange: '',
  });

  const handleAddQueue = () => {
    if (newQueue.sourceQueue.trim() && newQueue.targetQueue.trim()) {
      onQueuesChange([...queues, { ...newQueue }]);
      setNewQueue({ sourceQueue: '', targetQueue: '', targetExchange: '' });
    }
  };

  const handleRemoveQueue = (index: number) => {
    onQueuesChange(queues.filter((_, i) => i !== index));
  };

  const handleUpdateQueue = (index: number, field: keyof QueueMigration, value: string) => {
    const updated = queues.map((queue, i) =>
      i === index ? { ...queue, [field]: value } : queue
    );
    onQueuesChange(updated);
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      handleAddQueue();
    }
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <label className="block text-sm font-medium text-gray-700">
          Queue Mappings
        </label>
        <span className="text-xs text-gray-500">
          {queues.length} queue{queues.length !== 1 ? 's' : ''} configured
        </span>
      </div>

      {/* Queue Table */}
      <div className="border border-gray-200 rounded-lg overflow-hidden">
        {/* Table Header */}
        <div className="bg-muted border-b border-border px-4 py-2">
          <div className="grid grid-cols-[1fr_1fr_1fr_auto] gap-3 text-xs font-medium text-gray-500 uppercase tracking-wider">
            <span>Source Queue</span>
            <span>Target Queue</span>
            <span>Target Exchange (Optional)</span>
            <span className="w-10"></span>
          </div>
        </div>

        {/* Existing Queues */}
        {queues.length > 0 && (
          <div className="divide-y divide-gray-100">
            {queues.map((queue, index) => (
              <div key={index} className="px-4 py-2 bg-white hover:bg-muted transition-colors">
                <div className="grid grid-cols-[1fr_1fr_1fr_auto] gap-3 items-center">
                  <input
                    type="text"
                    value={queue.sourceQueue}
                    onChange={(e) => handleUpdateQueue(index, 'sourceQueue', e.target.value)}
                    placeholder="my-sqs-queue"
                    className="w-full px-2 py-1.5 text-sm border border-gray-200 rounded focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                  />
                  <input
                    type="text"
                    value={queue.targetQueue}
                    onChange={(e) => handleUpdateQueue(index, 'targetQueue', e.target.value)}
                    placeholder="my-rabbitmq-queue"
                    className="w-full px-2 py-1.5 text-sm border border-gray-200 rounded focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                  />
                  <input
                    type="text"
                    value={queue.targetExchange || ''}
                    onChange={(e) => handleUpdateQueue(index, 'targetExchange', e.target.value)}
                    placeholder="amq.direct"
                    className="w-full px-2 py-1.5 text-sm border border-gray-200 rounded focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                  />
                  <button
                    type="button"
                    onClick={() => handleRemoveQueue(index)}
                    className="p-1.5 text-muted-foreground/60 hover:text-error hover:bg-error/10 rounded transition-colors"
                    aria-label="Remove queue"
                  >
                    <Trash2 className="w-4 h-4" />
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}

        {/* Add New Queue Row */}
        <div className="px-4 py-2 bg-muted border-t border-border">
          <div className="grid grid-cols-[1fr_1fr_1fr_auto] gap-3 items-center">
            <input
              type="text"
              value={newQueue.sourceQueue}
              onChange={(e) => setNewQueue({ ...newQueue, sourceQueue: e.target.value })}
              onKeyDown={handleKeyDown}
              placeholder="Source queue name"
              className="w-full px-2 py-1.5 text-sm border border-input rounded focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
            />
            <input
              type="text"
              value={newQueue.targetQueue}
              onChange={(e) => setNewQueue({ ...newQueue, targetQueue: e.target.value })}
              onKeyDown={handleKeyDown}
              placeholder="Target queue name"
              className="w-full px-2 py-1.5 text-sm border border-input rounded focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
            />
            <input
              type="text"
              value={newQueue.targetExchange || ''}
              onChange={(e) => setNewQueue({ ...newQueue, targetExchange: e.target.value })}
              onKeyDown={handleKeyDown}
              placeholder="Exchange (optional)"
              className="w-full px-2 py-1.5 text-sm border border-input rounded focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
            />
            <button
              type="button"
              onClick={handleAddQueue}
              disabled={!newQueue.sourceQueue.trim() || !newQueue.targetQueue.trim()}
              className="p-1.5 text-primary hover:bg-primary/10 rounded transition-colors disabled:text-muted-foreground/40 disabled:hover:bg-transparent disabled:cursor-not-allowed"
              aria-label="Add queue"
            >
              <Plus className="w-4 h-4" />
            </button>
          </div>
        </div>

        {/* Empty State */}
        {queues.length === 0 && (
          <div className="px-4 py-6 text-center text-sm text-gray-500">
            No queues configured. Add a queue mapping above.
          </div>
        )}
      </div>

      <p className="text-xs text-gray-500">
        Map SQS queues to RabbitMQ queues. Optionally specify a target exchange for routing.
      </p>
    </div>
  );
}

// ============================================================================
// SNS Configuration Interfaces
// ============================================================================

interface SNSConfig {
  enabled: boolean;
  topics: string;
  migrateSubscriptions: boolean;
  migrateFilterPolicies: boolean;
  targetSubjectPattern: string;
}

// ============================================================================
// EventBridge Configuration Interfaces
// ============================================================================

interface EventBridgeConfig {
  enabled: boolean;
  eventBuses: string;
  migrateRules: boolean;
  migrateEventPatterns: boolean;
}

// ============================================================================
// SQS Migration Section
// ============================================================================

interface SQSMigrationSectionProps {
  migratePendingMessages: boolean;
  onMigratePendingMessagesChange: (value: boolean) => void;
}

function SQSMigrationSection({ migratePendingMessages, onMigratePendingMessagesChange }: SQSMigrationSectionProps) {
  const { queue, setQueueConfig } = useMigrationConfigStore();

  const handleQueuesChange = (queues: QueueMigration[]) => {
    setQueueConfig({ queues });
  };

  return (
    <div className="space-y-6">
      {/* Queue Mapping Table */}
      <QueueMappingTable
        queues={queue.queues}
        onQueuesChange={handleQueuesChange}
      />

      {/* Options Grid */}
      <div className="grid grid-cols-2 gap-4">
        {/* Migrate Dead Letter Queues */}
        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={queue.migrateDeadLetterQueues}
            onChange={(e) => setQueueConfig({ migrateDeadLetterQueues: e.target.checked })}
            className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
          />
          <div>
            <span className="text-sm font-medium text-gray-700">Migrate Dead Letter Queues</span>
            <p className="text-xs text-gray-500">Include associated DLQ configurations</p>
          </div>
        </label>

        {/* Preserve Message Attributes */}
        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={queue.preserveMessageAttributes}
            onChange={(e) => setQueueConfig({ preserveMessageAttributes: e.target.checked })}
            className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
          />
          <div>
            <span className="text-sm font-medium text-gray-700">Preserve Message Attributes</span>
            <p className="text-xs text-gray-500">Keep message metadata and headers</p>
          </div>
        </label>
      </div>

      {/* Migrate Pending Messages Option */}
      <div className="space-y-3">
        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={migratePendingMessages}
            onChange={(e) => onMigratePendingMessagesChange(e.target.checked)}
            className="w-4 h-4 text-amber-600 rounded border-input focus:ring-amber-500"
          />
          <div>
            <span className="text-sm font-medium text-gray-700">Migrate Pending Messages</span>
            <p className="text-xs text-gray-500">Transfer in-flight messages to RabbitMQ</p>
          </div>
        </label>

        {migratePendingMessages && (
          <WarningBox>
            Migrating pending messages requires careful timing. Messages received after migration
            starts but before completion may be duplicated or lost. Consider draining the queue
            before migration for critical workloads.
          </WarningBox>
        )}
      </div>
    </div>
  );
}

// ============================================================================
// SNS Migration Section
// ============================================================================

interface SNSMigrationSectionProps {
  config: SNSConfig;
  onConfigChange: (config: Partial<SNSConfig>) => void;
}

function SNSMigrationSection({ config, onConfigChange }: SNSMigrationSectionProps) {
  return (
    <div className="space-y-4">
      {/* Topics Input */}
      <FormField label="SNS Topics" htmlFor="sns-topics" required>
        <input
          id="sns-topics"
          type="text"
          value={config.topics}
          onChange={(e) => onConfigChange({ topics: e.target.value })}
          placeholder="my-topic-1, my-topic-2 (comma-separated)"
          className="w-full px-3 py-2 border border-input rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
        />
        <p className="text-xs text-gray-500 mt-1">
          Enter SNS topic names or ARNs separated by commas
        </p>
      </FormField>

      {/* Target Subject Naming Pattern */}
      <FormField label="Target Subject Pattern" htmlFor="sns-subject-pattern">
        <input
          id="sns-subject-pattern"
          type="text"
          value={config.targetSubjectPattern}
          onChange={(e) => onConfigChange({ targetSubjectPattern: e.target.value })}
          placeholder="events.{topic_name}"
          className="w-full px-3 py-2 border border-input rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
        />
        <p className="text-xs text-gray-500 mt-1">
          NATS subject naming pattern. Use {'{topic_name}'} as placeholder for original topic name.
        </p>
      </FormField>

      {/* Options */}
      <div className="space-y-3 pt-2">
        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={config.migrateSubscriptions}
            onChange={(e) => onConfigChange({ migrateSubscriptions: e.target.checked })}
            className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
          />
          <div>
            <span className="text-sm font-medium text-gray-700">Migrate Subscriptions</span>
            <p className="text-xs text-gray-500">
              Recreate SNS subscriptions as NATS subscriptions
            </p>
          </div>
        </label>

        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={config.migrateFilterPolicies}
            onChange={(e) => onConfigChange({ migrateFilterPolicies: e.target.checked })}
            className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
          />
          <div>
            <span className="text-sm font-medium text-gray-700">Migrate Filter Policies</span>
            <p className="text-xs text-gray-500">
              Convert SNS filter policies to NATS subject-based filtering
            </p>
          </div>
        </label>
      </div>
    </div>
  );
}

// ============================================================================
// EventBridge Migration Section
// ============================================================================

interface EventBridgeMigrationSectionProps {
  config: EventBridgeConfig;
  onConfigChange: (config: Partial<EventBridgeConfig>) => void;
}

function EventBridgeMigrationSection({ config, onConfigChange }: EventBridgeMigrationSectionProps) {
  return (
    <div className="space-y-4">
      {/* Event Buses Input */}
      <FormField label="Event Buses" htmlFor="eventbridge-buses" required>
        <input
          id="eventbridge-buses"
          type="text"
          value={config.eventBuses}
          onChange={(e) => onConfigChange({ eventBuses: e.target.value })}
          placeholder="default, custom-bus-1 (comma-separated)"
          className="w-full px-3 py-2 border border-input rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
        />
        <p className="text-xs text-gray-500 mt-1">
          Enter EventBridge event bus names separated by commas
        </p>
      </FormField>

      {/* Options */}
      <div className="space-y-3 pt-2">
        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={config.migrateRules}
            onChange={(e) => onConfigChange({ migrateRules: e.target.checked })}
            className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
          />
          <div>
            <span className="text-sm font-medium text-gray-700">Migrate Rules</span>
            <p className="text-xs text-gray-500">
              Convert EventBridge rules to JetStream consumers
            </p>
          </div>
        </label>

        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={config.migrateEventPatterns}
            onChange={(e) => onConfigChange({ migrateEventPatterns: e.target.checked })}
            className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
          />
          <div>
            <span className="text-sm font-medium text-gray-700">Migrate Event Patterns</span>
            <p className="text-xs text-gray-500">
              Translate event patterns to NATS subject hierarchies
            </p>
          </div>
        </label>
      </div>

      {/* Pattern Translation Limitations Note */}
      <InfoBox>
        EventBridge event patterns use complex matching rules that may not translate directly to
        NATS subject-based routing. Complex patterns with content-based filtering, prefix matching,
        or numeric comparisons may require manual adjustment after migration.
      </InfoBox>
    </div>
  );
}

// ============================================================================
// Main MessagingMigration Component
// ============================================================================

interface MessagingMigrationProps {
  resources: Resource[];
}

export function MessagingMigration({ resources = [] }: MessagingMigrationProps) {
  const { queue, setQueueConfig } = useMigrationConfigStore();

  // Determine which service cards to show based on discovered resources
  const { hasSQS, hasSNS, hasEventBridge } = useMemo(() => ({
    hasSQS: resources.some(r => SQS_TYPES.includes(r.type)),
    hasSNS: resources.some(r => SNS_TYPES.includes(r.type)),
    hasEventBridge: resources.some(r => EVENTBRIDGE_TYPES.includes(r.type)),
  }), [resources]);

  // Local state for SQS pending messages option
  const [migratePendingMessages, setMigratePendingMessages] = useState(false);

  // Local state for SNS configuration
  const [snsConfig, setSnsConfig] = useState<SNSConfig>({
    enabled: false,
    topics: '',
    migrateSubscriptions: true,
    migrateFilterPolicies: false,
    targetSubjectPattern: 'events.{topic_name}',
  });

  // Local state for EventBridge configuration
  const [eventBridgeConfig, setEventBridgeConfig] = useState<EventBridgeConfig>({
    enabled: false,
    eventBuses: '',
    migrateRules: true,
    migrateEventPatterns: true,
  });

  const handleSQSToggle = (enabled: boolean) => {
    setQueueConfig({ enabled, sourceType: 'sqs' });
  };

  const handleSNSToggle = (enabled: boolean) => {
    setSnsConfig((prev) => ({ ...prev, enabled }));
  };

  const handleEventBridgeToggle = (enabled: boolean) => {
    setEventBridgeConfig((prev) => ({ ...prev, enabled }));
  };

  const handleSNSConfigChange = (config: Partial<SNSConfig>) => {
    setSnsConfig((prev) => ({ ...prev, ...config }));
  };

  const handleEventBridgeConfigChange = (config: Partial<EventBridgeConfig>) => {
    setEventBridgeConfig((prev) => ({ ...prev, ...config }));
  };

  // If no messaging resources discovered, show empty state
  if (!hasSQS && !hasSNS && !hasEventBridge) {
    return (
      <div className="text-center py-8 text-muted-foreground">
        <MessageSquare className="w-12 h-12 mx-auto mb-3 opacity-50" />
        <p className="font-medium">No messaging resources discovered</p>
        <p className="text-sm mt-1">SQS, SNS, or EventBridge resources will appear here when detected.</p>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {/* SQS to RabbitMQ Section - only show if SQS resources discovered */}
      {hasSQS && (
        <ServiceMigrationCard
          title="Amazon SQS → RabbitMQ"
          description="Migrate message queues to RabbitMQ"
          icon={MessageSquare}
          enabled={queue.enabled && queue.sourceType === 'sqs'}
          onToggle={handleSQSToggle}
          defaultExpanded={true}
        >
          <SQSMigrationSection
            migratePendingMessages={migratePendingMessages}
            onMigratePendingMessagesChange={setMigratePendingMessages}
          />
        </ServiceMigrationCard>
      )}

      {/* SNS to NATS Section - only show if SNS resources discovered */}
      {hasSNS && (
        <ServiceMigrationCard
          title="Amazon SNS → NATS"
          description="Migrate pub/sub topics to NATS"
          icon={Bell}
          enabled={snsConfig.enabled}
          onToggle={handleSNSToggle}
          defaultExpanded={true}
        >
          <SNSMigrationSection
            config={snsConfig}
            onConfigChange={handleSNSConfigChange}
          />
        </ServiceMigrationCard>
      )}

      {/* EventBridge to NATS JetStream Section - only show if EventBridge resources discovered */}
      {hasEventBridge && (
        <ServiceMigrationCard
          title="Amazon EventBridge → NATS JetStream"
          description="Migrate event buses and rules"
          icon={Zap}
          enabled={eventBridgeConfig.enabled}
          onToggle={handleEventBridgeToggle}
          defaultExpanded={true}
        >
          <EventBridgeMigrationSection
            config={eventBridgeConfig}
            onConfigChange={handleEventBridgeConfigChange}
          />
        </ServiceMigrationCard>
      )}
    </div>
  );
}

export default MessagingMigration;
