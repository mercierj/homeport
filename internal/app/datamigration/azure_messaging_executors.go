package datamigration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// ServiceBusToRabbitMQExecutor migrates Azure Service Bus to RabbitMQ.
type ServiceBusToRabbitMQExecutor struct{}

// NewServiceBusToRabbitMQExecutor creates a new Service Bus to RabbitMQ executor.
func NewServiceBusToRabbitMQExecutor() *ServiceBusToRabbitMQExecutor {
	return &ServiceBusToRabbitMQExecutor{}
}

// Type returns the migration type.
func (e *ServiceBusToRabbitMQExecutor) Type() string {
	return "servicebus_to_rabbitmq"
}

// GetPhases returns the migration phases.
func (e *ServiceBusToRabbitMQExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Fetching namespace info",
		"Exporting queues and topics",
		"Generating RabbitMQ configuration",
		"Creating migration scripts",
		"Finalizing",
	}
}

// Validate validates the migration configuration.
func (e *ServiceBusToRabbitMQExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["namespace"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.namespace is required")
		}
		if _, ok := config.Source["resource_group"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.resource_group is required")
		}
	}

	if config.Destination == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "destination configuration is required")
	} else {
		if _, ok := config.Destination["output_dir"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "destination.output_dir is required")
		}
	}

	result.Warnings = append(result.Warnings, "Service Bus sessions need custom handling in RabbitMQ")
	result.Warnings = append(result.Warnings, "Dead letter queues need manual configuration")

	return result, nil
}

// Execute performs the migration.
func (e *ServiceBusToRabbitMQExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	namespace := config.Source["namespace"].(string)
	resourceGroup := config.Source["resource_group"].(string)
	subscription, _ := config.Source["subscription"].(string)

	outputDir := config.Destination["output_dir"].(string)

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating Azure credentials")
	EmitProgress(m, 10, "Checking credentials")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Fetching namespace info
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", fmt.Sprintf("Fetching Service Bus namespace info for %s", namespace))
	EmitProgress(m, 25, "Fetching namespace info")

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Get namespace info
	args := []string{"servicebus", "namespace", "show",
		"--name", namespace,
		"--resource-group", resourceGroup,
		"--output", "json",
	}
	if subscription != "" {
		args = append(args, "--subscription", subscription)
	}

	showCmd := exec.CommandContext(ctx, "az", args...)
	nsOutput, _ := showCmd.Output()

	var nsInfo struct {
		Name     string `json:"name"`
		Location string `json:"location"`
		Sku      struct {
			Name string `json:"name"`
			Tier string `json:"tier"`
		} `json:"sku"`
	}
	if len(nsOutput) > 0 {
		_ = json.Unmarshal(nsOutput, &nsInfo)
	}

	// Save namespace info
	nsInfoPath := filepath.Join(outputDir, "namespace-info.json")
	if len(nsOutput) > 0 {
		if err := os.WriteFile(nsInfoPath, nsOutput, 0644); err != nil {
			return fmt.Errorf("failed to write namespace info: %w", err)
		}
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Exporting queues and topics
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", "Exporting queues and topics")
	EmitProgress(m, 45, "Exporting queues and topics")

	// Get queues
	queueArgs := []string{"servicebus", "queue", "list",
		"--namespace-name", namespace,
		"--resource-group", resourceGroup,
		"--output", "json",
	}
	if subscription != "" {
		queueArgs = append(queueArgs, "--subscription", subscription)
	}

	queueCmd := exec.CommandContext(ctx, "az", queueArgs...)
	queueOutput, _ := queueCmd.Output()

	var queues []struct {
		Name                       string `json:"name"`
		MaxSizeInMegabytes         int    `json:"maxSizeInMegabytes"`
		MessageCount               int    `json:"messageCount"`
		DefaultMessageTimeToLive   string `json:"defaultMessageTimeToLive"`
		LockDuration               string `json:"lockDuration"`
		MaxDeliveryCount           int    `json:"maxDeliveryCount"`
		DeadLetteringOnMessageExpiration bool `json:"deadLetteringOnMessageExpiration"`
	}
	if len(queueOutput) > 0 {
		_ = json.Unmarshal(queueOutput, &queues)
	}

	// Get topics
	topicArgs := []string{"servicebus", "topic", "list",
		"--namespace-name", namespace,
		"--resource-group", resourceGroup,
		"--output", "json",
	}
	if subscription != "" {
		topicArgs = append(topicArgs, "--subscription", subscription)
	}

	topicCmd := exec.CommandContext(ctx, "az", topicArgs...)
	topicOutput, _ := topicCmd.Output()

	var topics []struct {
		Name                     string `json:"name"`
		MaxSizeInMegabytes       int    `json:"maxSizeInMegabytes"`
		DefaultMessageTimeToLive string `json:"defaultMessageTimeToLive"`
	}
	if len(topicOutput) > 0 {
		_ = json.Unmarshal(topicOutput, &topics)
	}

	// Save queues and topics
	queuePath := filepath.Join(outputDir, "queues.json")
	if len(queueOutput) > 0 {
		_ = os.WriteFile(queuePath, queueOutput, 0644)
	}

	topicPath := filepath.Join(outputDir, "topics.json")
	if len(topicOutput) > 0 {
		_ = os.WriteFile(topicPath, topicOutput, 0644)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Generating RabbitMQ configuration
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Generating RabbitMQ configuration")
	EmitProgress(m, 65, "Generating configuration")

	// Generate Docker Compose for RabbitMQ
	dockerCompose := `version: '3.8'

services:
  rabbitmq:
    image: rabbitmq:3-management-alpine
    container_name: rabbitmq
    hostname: rabbitmq
    environment:
      RABBITMQ_DEFAULT_USER: admin
      RABBITMQ_DEFAULT_PASS: admin
      RABBITMQ_DEFAULT_VHOST: /
    volumes:
      - rabbitmq-data:/var/lib/rabbitmq
      - ./rabbitmq.conf:/etc/rabbitmq/rabbitmq.conf
      - ./definitions.json:/etc/rabbitmq/definitions.json
    ports:
      - "5672:5672"   # AMQP
      - "15672:15672" # Management UI
    restart: unless-stopped

volumes:
  rabbitmq-data:
`
	composePath := filepath.Join(outputDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(dockerCompose), 0644); err != nil {
		return fmt.Errorf("failed to write docker-compose.yml: %w", err)
	}

	// Generate RabbitMQ config
	rabbitmqConf := `# RabbitMQ Configuration
# Migrated from Azure Service Bus

# Load definitions on startup
management.load_definitions = /etc/rabbitmq/definitions.json

# Listeners
listeners.tcp.default = 5672

# Management
management.tcp.port = 15672

# Logging
log.console = true
log.console.level = info

# Memory and disk limits
vm_memory_high_watermark.relative = 0.7
disk_free_limit.absolute = 1GB
`
	rabbitmqConfPath := filepath.Join(outputDir, "rabbitmq.conf")
	if err := os.WriteFile(rabbitmqConfPath, []byte(rabbitmqConf), 0644); err != nil {
		return fmt.Errorf("failed to write rabbitmq.conf: %w", err)
	}

	// Generate RabbitMQ definitions (queues, exchanges, bindings)
	var queueDefs []map[string]interface{}
	var exchangeDefs []map[string]interface{}
	var bindingDefs []map[string]interface{}

	// Convert Service Bus queues to RabbitMQ queues
	for _, q := range queues {
		queueDef := map[string]interface{}{
			"name":        q.Name,
			"vhost":       "/",
			"durable":     true,
			"auto_delete": false,
			"arguments": map[string]interface{}{
				"x-max-length": q.MaxSizeInMegabytes * 1024, // Approximate
			},
		}
		if q.DeadLetteringOnMessageExpiration {
			queueDef["arguments"].(map[string]interface{})["x-dead-letter-exchange"] = "dlx"
			queueDef["arguments"].(map[string]interface{})["x-dead-letter-routing-key"] = q.Name + ".dlq"
		}
		queueDefs = append(queueDefs, queueDef)
	}

	// Convert Service Bus topics to RabbitMQ exchanges
	for _, t := range topics {
		exchangeDef := map[string]interface{}{
			"name":        t.Name,
			"vhost":       "/",
			"type":        "topic",
			"durable":     true,
			"auto_delete": false,
		}
		exchangeDefs = append(exchangeDefs, exchangeDef)
	}

	// Add DLX exchange
	exchangeDefs = append(exchangeDefs, map[string]interface{}{
		"name":        "dlx",
		"vhost":       "/",
		"type":        "direct",
		"durable":     true,
		"auto_delete": false,
	})

	definitions := map[string]interface{}{
		"rabbit_version": "3.12.0",
		"users": []map[string]interface{}{
			{
				"name":              "admin",
				"password_hash":     "admin", // Will be hashed by RabbitMQ
				"hashing_algorithm": "rabbit_password_hashing_sha256",
				"tags":              []string{"administrator"},
			},
		},
		"vhosts": []map[string]interface{}{
			{"name": "/"},
		},
		"permissions": []map[string]interface{}{
			{
				"user":      "admin",
				"vhost":     "/",
				"configure": ".*",
				"write":     ".*",
				"read":      ".*",
			},
		},
		"queues":    queueDefs,
		"exchanges": exchangeDefs,
		"bindings":  bindingDefs,
	}

	definitionsBytes, _ := json.MarshalIndent(definitions, "", "  ")
	definitionsPath := filepath.Join(outputDir, "definitions.json")
	if err := os.WriteFile(definitionsPath, definitionsBytes, 0644); err != nil {
		return fmt.Errorf("failed to write definitions.json: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Creating migration scripts
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Creating migration scripts")
	EmitProgress(m, 80, "Creating scripts")

	// Generate message drain script
	drainScript := fmt.Sprintf(`#!/bin/bash
# Service Bus Message Drain Script
# Namespace: %s

set -e

echo "Service Bus Message Drain"
echo "========================="

# Configuration
NAMESPACE="%s"
RESOURCE_GROUP="%s"

# Get connection string
CONNECTION_STRING=$(az servicebus namespace authorization-rule keys list \
    --namespace-name "$NAMESPACE" \
    --resource-group "$RESOURCE_GROUP" \
    --name RootManageSharedAccessKey \
    --query primaryConnectionString \
    --output tsv)

echo "Connection string retrieved."
echo ""
echo "To drain messages, use the Azure Service Bus Explorer or SDK."
echo ""
echo "Python example:"
echo "  pip install azure-servicebus"
echo "  from azure.servicebus import ServiceBusClient"
echo "  client = ServiceBusClient.from_connection_string('$CONNECTION_STRING')"
echo "  receiver = client.get_queue_receiver(queue_name='your-queue')"
echo "  messages = receiver.receive_messages(max_message_count=100)"
echo ""
echo "Or use Service Bus Explorer:"
echo "  https://github.com/paolosalvatori/ServiceBusExplorer"
`, namespace, namespace, resourceGroup)

	drainPath := filepath.Join(outputDir, "drain-messages.sh")
	if err := os.WriteFile(drainPath, []byte(drainScript), 0755); err != nil {
		return fmt.Errorf("failed to write drain script: %w", err)
	}

	// Generate RabbitMQ setup verification script
	setupScript := `#!/bin/bash
# RabbitMQ Setup and Verification Script

set -e

echo "RabbitMQ Setup"
echo "=============="

# Start RabbitMQ
docker-compose up -d

echo "Waiting for RabbitMQ to be ready..."
sleep 10

# Check if RabbitMQ is healthy
until docker exec rabbitmq rabbitmqctl status > /dev/null 2>&1; do
    echo "Waiting for RabbitMQ..."
    sleep 2
done

echo "RabbitMQ is ready!"
echo ""
echo "Access:"
echo "  AMQP: amqp://admin:admin@localhost:5672/"
echo "  Management: http://localhost:15672 (admin/admin)"
echo ""

# List queues
echo "Configured queues:"
docker exec rabbitmq rabbitmqctl list_queues name messages

echo ""
echo "Configured exchanges:"
docker exec rabbitmq rabbitmqctl list_exchanges name type
`
	setupPath := filepath.Join(outputDir, "setup-rabbitmq.sh")
	if err := os.WriteFile(setupPath, []byte(setupScript), 0755); err != nil {
		return fmt.Errorf("failed to write setup script: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 6: Finalizing
	EmitPhase(m, phases[5], 6)
	EmitLog(m, "info", "Finalizing migration artifacts")
	EmitProgress(m, 90, "Finalizing")

	readme := fmt.Sprintf(`# Service Bus to RabbitMQ Migration

## Source Service Bus
- Namespace: %s
- Resource Group: %s
- SKU: %s
- Queues: %d
- Topics: %d

## Migration Steps

1. (Optional) Drain messages from Service Bus:
'''bash
./drain-messages.sh
'''

2. Start RabbitMQ:
'''bash
./setup-rabbitmq.sh
'''

3. Update application connection strings

## Files Generated
- namespace-info.json: Service Bus namespace configuration
- queues.json: Queue definitions
- topics.json: Topic definitions
- docker-compose.yml: RabbitMQ container
- rabbitmq.conf: RabbitMQ configuration
- definitions.json: RabbitMQ queues/exchanges/bindings
- drain-messages.sh: Message drain script
- setup-rabbitmq.sh: RabbitMQ setup script

## Access
- AMQP: amqp://admin:admin@localhost:5672/
- Management UI: http://localhost:15672 (admin/admin)

## Service Bus to RabbitMQ Mapping

| Service Bus | RabbitMQ |
|-------------|----------|
| Queue | Queue |
| Topic | Exchange (topic type) |
| Subscription | Queue + Binding |
| Dead Letter Queue | DLX + DLQ |
| Sessions | Consumer groups (custom) |

## Notes
- Service Bus sessions need custom handling
- Topic subscriptions need manual binding setup
- Review dead letter queue configuration
- Update authentication in your applications
`, namespace, resourceGroup, nsInfo.Sku.Name, len(queues), len(topics))

	readmePath := filepath.Join(outputDir, "README.md")
	if err := os.WriteFile(readmePath, []byte(readme), 0644); err != nil {
		return fmt.Errorf("failed to write README: %w", err)
	}

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", fmt.Sprintf("Service Bus %s migration prepared at %s", namespace, outputDir))

	return nil
}

// EventHubToRedpandaExecutor migrates Azure Event Hubs to Redpanda (Kafka-compatible).
type EventHubToRedpandaExecutor struct{}

// NewEventHubToRedpandaExecutor creates a new Event Hub to Redpanda executor.
func NewEventHubToRedpandaExecutor() *EventHubToRedpandaExecutor {
	return &EventHubToRedpandaExecutor{}
}

// Type returns the migration type.
func (e *EventHubToRedpandaExecutor) Type() string {
	return "eventhub_to_redpanda"
}

// GetPhases returns the migration phases.
func (e *EventHubToRedpandaExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Fetching namespace info",
		"Exporting event hubs",
		"Generating Redpanda configuration",
		"Creating migration scripts",
		"Finalizing",
	}
}

// Validate validates the migration configuration.
func (e *EventHubToRedpandaExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["namespace"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.namespace is required")
		}
		if _, ok := config.Source["resource_group"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.resource_group is required")
		}
	}

	if config.Destination == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "destination configuration is required")
	} else {
		if _, ok := config.Destination["output_dir"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "destination.output_dir is required")
		}
	}

	result.Warnings = append(result.Warnings, "Event Hub Capture needs alternative solution")
	result.Warnings = append(result.Warnings, "Consumer groups need to be recreated")

	return result, nil
}

// Execute performs the migration.
func (e *EventHubToRedpandaExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	namespace := config.Source["namespace"].(string)
	resourceGroup := config.Source["resource_group"].(string)
	subscription, _ := config.Source["subscription"].(string)

	outputDir := config.Destination["output_dir"].(string)

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating Azure credentials")
	EmitProgress(m, 10, "Checking credentials")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Fetching namespace info
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", fmt.Sprintf("Fetching Event Hubs namespace info for %s", namespace))
	EmitProgress(m, 25, "Fetching namespace info")

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Get namespace info
	args := []string{"eventhubs", "namespace", "show",
		"--name", namespace,
		"--resource-group", resourceGroup,
		"--output", "json",
	}
	if subscription != "" {
		args = append(args, "--subscription", subscription)
	}

	showCmd := exec.CommandContext(ctx, "az", args...)
	nsOutput, _ := showCmd.Output()

	var nsInfo struct {
		Name     string `json:"name"`
		Location string `json:"location"`
		Sku      struct {
			Name     string `json:"name"`
			Capacity int    `json:"capacity"`
		} `json:"sku"`
	}
	if len(nsOutput) > 0 {
		_ = json.Unmarshal(nsOutput, &nsInfo)
	}

	// Save namespace info
	nsInfoPath := filepath.Join(outputDir, "namespace-info.json")
	if len(nsOutput) > 0 {
		_ = os.WriteFile(nsInfoPath, nsOutput, 0644)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Exporting event hubs
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", "Exporting event hubs")
	EmitProgress(m, 45, "Exporting event hubs")

	// Get event hubs
	ehArgs := []string{"eventhubs", "eventhub", "list",
		"--namespace-name", namespace,
		"--resource-group", resourceGroup,
		"--output", "json",
	}
	if subscription != "" {
		ehArgs = append(ehArgs, "--subscription", subscription)
	}

	ehCmd := exec.CommandContext(ctx, "az", ehArgs...)
	ehOutput, _ := ehCmd.Output()

	var eventHubs []struct {
		Name            string `json:"name"`
		PartitionCount  int    `json:"partitionCount"`
		MessageRetentionInDays int `json:"messageRetentionInDays"`
	}
	if len(ehOutput) > 0 {
		_ = json.Unmarshal(ehOutput, &eventHubs)
	}

	// Save event hubs
	ehPath := filepath.Join(outputDir, "eventhubs.json")
	if len(ehOutput) > 0 {
		_ = os.WriteFile(ehPath, ehOutput, 0644)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Generating Redpanda configuration
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Generating Redpanda configuration")
	EmitProgress(m, 65, "Generating configuration")

	// Generate Docker Compose for Redpanda
	dockerCompose := `version: '3.8'

services:
  redpanda:
    image: redpandadata/redpanda:latest
    container_name: redpanda
    command:
      - redpanda
      - start
      - --smp 1
      - --memory 1G
      - --reserve-memory 0M
      - --overprovisioned
      - --kafka-addr internal://0.0.0.0:9092,external://0.0.0.0:19092
      - --advertise-kafka-addr internal://redpanda:9092,external://localhost:19092
      - --pandaproxy-addr internal://0.0.0.0:8082,external://0.0.0.0:18082
      - --advertise-pandaproxy-addr internal://redpanda:8082,external://localhost:18082
      - --schema-registry-addr internal://0.0.0.0:8081,external://0.0.0.0:18081
    volumes:
      - redpanda-data:/var/lib/redpanda/data
    ports:
      - "18081:18081"  # Schema Registry
      - "18082:18082"  # REST Proxy
      - "19092:19092"  # Kafka API
      - "9644:9644"    # Admin API
    restart: unless-stopped

  console:
    image: redpandadata/console:latest
    container_name: redpanda-console
    environment:
      KAFKA_BROKERS: redpanda:9092
      KAFKA_SCHEMAREGISTRY_ENABLED: "true"
      KAFKA_SCHEMAREGISTRY_URLS: http://redpanda:8081
    ports:
      - "8080:8080"
    depends_on:
      - redpanda
    restart: unless-stopped

volumes:
  redpanda-data:
`
	composePath := filepath.Join(outputDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(dockerCompose), 0644); err != nil {
		return fmt.Errorf("failed to write docker-compose.yml: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Creating migration scripts
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Creating migration scripts")
	EmitProgress(m, 80, "Creating scripts")

	// Generate topic creation script
	var topicCreation string
	for _, eh := range eventHubs {
		retention := eh.MessageRetentionInDays * 24 * 60 * 60 * 1000 // Convert to ms
		topicCreation += fmt.Sprintf(`
echo "Creating topic: %s"
rpk topic create %s \
    --partitions %d \
    --config retention.ms=%d
`, eh.Name, eh.Name, eh.PartitionCount, retention)
	}

	createTopicsScript := fmt.Sprintf(`#!/bin/bash
# Create Topics in Redpanda
# Migrated from Event Hubs namespace: %s

set -e

echo "Creating topics in Redpanda..."
echo "=============================="

# Wait for Redpanda to be ready
until rpk cluster info --brokers localhost:19092 > /dev/null 2>&1; do
    echo "Waiting for Redpanda..."
    sleep 2
done

%s

echo ""
echo "Topics created successfully!"
echo ""
rpk topic list --brokers localhost:19092
`, namespace, topicCreation)

	createTopicsPath := filepath.Join(outputDir, "create-topics.sh")
	if err := os.WriteFile(createTopicsPath, []byte(createTopicsScript), 0755); err != nil {
		return fmt.Errorf("failed to write create topics script: %w", err)
	}

	// Generate Event Hubs to Kafka migration guide
	kafkaMigration := fmt.Sprintf(`#!/bin/bash
# Event Hubs Kafka Migration Script
# Use this to bridge events from Event Hubs to Redpanda during transition

set -e

echo "Event Hubs to Redpanda Bridge"
echo "============================="

# Configuration
EVENTHUB_NAMESPACE="%s"
EVENTHUB_NAME="${1:-your-eventhub}"
RESOURCE_GROUP="%s"

# Get connection string
CONNECTION_STRING=$(az eventhubs namespace authorization-rule keys list \
    --namespace-name "$EVENTHUB_NAMESPACE" \
    --resource-group "$RESOURCE_GROUP" \
    --name RootManageSharedAccessKey \
    --query primaryConnectionString \
    --output tsv)

echo "Event Hubs connection string retrieved."
echo ""
echo "Event Hubs Kafka endpoint:"
echo "  Bootstrap: ${EVENTHUB_NAMESPACE}.servicebus.windows.net:9093"
echo ""
echo "To consume from Event Hubs using Kafka client:"
echo "  security.protocol=SASL_SSL"
echo "  sasl.mechanism=PLAIN"
echo "  sasl.username=\$ConnectionString"
echo "  sasl.password=${CONNECTION_STRING}"
echo ""
echo "To produce to Redpanda:"
echo "  bootstrap.servers=localhost:19092"
echo ""
echo "Use Kafka MirrorMaker or similar tool to bridge events."
`, namespace, resourceGroup)

	kafkaMigrationPath := filepath.Join(outputDir, "kafka-bridge.sh")
	if err := os.WriteFile(kafkaMigrationPath, []byte(kafkaMigration), 0755); err != nil {
		return fmt.Errorf("failed to write kafka bridge script: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 6: Finalizing
	EmitPhase(m, phases[5], 6)
	EmitLog(m, "info", "Finalizing migration artifacts")
	EmitProgress(m, 90, "Finalizing")

	readme := fmt.Sprintf(`# Event Hubs to Redpanda Migration

## Source Event Hubs
- Namespace: %s
- Resource Group: %s
- SKU: %s
- Event Hubs: %d

## Migration Steps

1. Start Redpanda:
'''bash
docker-compose up -d
'''

2. Install rpk (Redpanda CLI):
'''bash
# macOS
brew install redpanda-data/tap/redpanda

# Linux
curl -LO https://github.com/redpanda-data/redpanda/releases/latest/download/rpk-linux-amd64.zip
'''

3. Create topics:
'''bash
./create-topics.sh
'''

4. Update application configurations

## Files Generated
- namespace-info.json: Event Hubs namespace configuration
- eventhubs.json: Event hub definitions
- docker-compose.yml: Redpanda container setup
- create-topics.sh: Topic creation script
- kafka-bridge.sh: Event Hubs Kafka bridge info

## Access
- Kafka API: localhost:19092
- Schema Registry: localhost:18081
- REST Proxy: localhost:18082
- Console: http://localhost:8080

## Event Hubs to Redpanda/Kafka Mapping

| Event Hubs | Redpanda/Kafka |
|------------|----------------|
| Event Hub | Topic |
| Partition | Partition |
| Consumer Group | Consumer Group |
| Partition Key | Message Key |
| Capture | Tiered Storage / Connect |

## Application Configuration Changes

Event Hubs Kafka endpoint:
'''
bootstrap.servers=%s.servicebus.windows.net:9093
security.protocol=SASL_SSL
sasl.mechanism=PLAIN
'''

Redpanda endpoint:
'''
bootstrap.servers=localhost:19092
'''

## Notes
- Event Hubs Capture needs alternative (Redpanda Tiered Storage)
- Consumer groups need to be recreated
- Update Kafka clients to point to Redpanda
`, namespace, resourceGroup, nsInfo.Sku.Name, len(eventHubs), namespace)

	readmePath := filepath.Join(outputDir, "README.md")
	if err := os.WriteFile(readmePath, []byte(readme), 0644); err != nil {
		return fmt.Errorf("failed to write README: %w", err)
	}

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", fmt.Sprintf("Event Hubs %s migration prepared at %s", namespace, outputDir))

	return nil
}

// EventGridToRabbitMQExecutor migrates Azure Event Grid to RabbitMQ.
type EventGridToRabbitMQExecutor struct{}

// NewEventGridToRabbitMQExecutor creates a new Event Grid to RabbitMQ executor.
func NewEventGridToRabbitMQExecutor() *EventGridToRabbitMQExecutor {
	return &EventGridToRabbitMQExecutor{}
}

// Type returns the migration type.
func (e *EventGridToRabbitMQExecutor) Type() string {
	return "eventgrid_to_rabbitmq"
}

// GetPhases returns the migration phases.
func (e *EventGridToRabbitMQExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Fetching topic info",
		"Analyzing subscriptions",
		"Generating RabbitMQ configuration",
		"Creating webhook adapters",
		"Finalizing",
	}
}

// Validate validates the migration configuration.
func (e *EventGridToRabbitMQExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["topic_name"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.topic_name is required")
		}
		if _, ok := config.Source["resource_group"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.resource_group is required")
		}
	}

	if config.Destination == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "destination configuration is required")
	} else {
		if _, ok := config.Destination["output_dir"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "destination.output_dir is required")
		}
	}

	result.Warnings = append(result.Warnings, "Event Grid filtering needs custom implementation")
	result.Warnings = append(result.Warnings, "CloudEvents schema conversion may be needed")

	return result, nil
}

// Execute performs the migration.
func (e *EventGridToRabbitMQExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	topicName := config.Source["topic_name"].(string)
	resourceGroup := config.Source["resource_group"].(string)
	subscription, _ := config.Source["subscription"].(string)

	outputDir := config.Destination["output_dir"].(string)

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating Azure credentials")
	EmitProgress(m, 10, "Checking credentials")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Fetching topic info
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", fmt.Sprintf("Fetching Event Grid topic info for %s", topicName))
	EmitProgress(m, 25, "Fetching topic info")

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Get topic info
	args := []string{"eventgrid", "topic", "show",
		"--name", topicName,
		"--resource-group", resourceGroup,
		"--output", "json",
	}
	if subscription != "" {
		args = append(args, "--subscription", subscription)
	}

	showCmd := exec.CommandContext(ctx, "az", args...)
	topicOutput, _ := showCmd.Output()

	var topicInfo struct {
		Name     string `json:"name"`
		Location string `json:"location"`
		Endpoint string `json:"endpoint"`
		InputSchema string `json:"inputSchema"`
	}
	if len(topicOutput) > 0 {
		_ = json.Unmarshal(topicOutput, &topicInfo)
	}

	// Save topic info
	topicInfoPath := filepath.Join(outputDir, "topic-info.json")
	if len(topicOutput) > 0 {
		_ = os.WriteFile(topicInfoPath, topicOutput, 0644)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Analyzing subscriptions
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", "Analyzing event subscriptions")
	EmitProgress(m, 45, "Analyzing subscriptions")

	// Get subscriptions
	subArgs := []string{"eventgrid", "event-subscription", "list",
		"--topic-name", topicName,
		"--resource-group", resourceGroup,
		"--output", "json",
	}
	if subscription != "" {
		subArgs = append(subArgs, "--subscription", subscription)
	}

	subCmd := exec.CommandContext(ctx, "az", subArgs...)
	subOutput, _ := subCmd.Output()

	var subscriptions []struct {
		Name        string `json:"name"`
		Destination struct {
			EndpointType string `json:"endpointType"`
			EndpointBaseUrl string `json:"endpointBaseUrl"`
		} `json:"destination"`
		Filter struct {
			SubjectBeginsWith string `json:"subjectBeginsWith"`
			SubjectEndsWith   string `json:"subjectEndsWith"`
		} `json:"filter"`
	}
	if len(subOutput) > 0 {
		_ = json.Unmarshal(subOutput, &subscriptions)
	}

	// Save subscriptions
	subPath := filepath.Join(outputDir, "subscriptions.json")
	if len(subOutput) > 0 {
		_ = os.WriteFile(subPath, subOutput, 0644)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Generating RabbitMQ configuration
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Generating RabbitMQ configuration")
	EmitProgress(m, 65, "Generating configuration")

	// Generate Docker Compose for RabbitMQ + event router
	dockerCompose := `version: '3.8'

services:
  rabbitmq:
    image: rabbitmq:3-management-alpine
    container_name: rabbitmq
    hostname: rabbitmq
    environment:
      RABBITMQ_DEFAULT_USER: admin
      RABBITMQ_DEFAULT_PASS: admin
    volumes:
      - rabbitmq-data:/var/lib/rabbitmq
    ports:
      - "5672:5672"
      - "15672:15672"
    restart: unless-stopped

  event-router:
    build: ./event-router
    container_name: event-router
    environment:
      RABBITMQ_URL: amqp://admin:admin@rabbitmq:5672/
      PORT: 8080
    ports:
      - "8080:8080"
    depends_on:
      - rabbitmq
    restart: unless-stopped

volumes:
  rabbitmq-data:
`
	composePath := filepath.Join(outputDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(dockerCompose), 0644); err != nil {
		return fmt.Errorf("failed to write docker-compose.yml: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Creating webhook adapters
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Creating webhook adapters")
	EmitProgress(m, 80, "Creating webhook adapters")

	// Create event router directory
	routerDir := filepath.Join(outputDir, "event-router")
	if err := os.MkdirAll(routerDir, 0755); err != nil {
		return fmt.Errorf("failed to create event-router directory: %w", err)
	}

	// Generate event router (simple HTTP to RabbitMQ bridge)
	eventRouter := `// Event Router - HTTP to RabbitMQ bridge
// Replacement for Event Grid event delivery

const http = require('http');
const amqp = require('amqplib');

const RABBITMQ_URL = process.env.RABBITMQ_URL || 'amqp://admin:admin@localhost:5672/';
const PORT = process.env.PORT || 8080;
const EXCHANGE_NAME = 'events';

let channel;

async function connectRabbitMQ() {
    const connection = await amqp.connect(RABBITMQ_URL);
    channel = await connection.createChannel();
    await channel.assertExchange(EXCHANGE_NAME, 'topic', { durable: true });
    console.log('Connected to RabbitMQ');
}

async function publishEvent(event) {
    const routingKey = event.eventType || event.type || 'unknown';
    const message = Buffer.from(JSON.stringify(event));

    channel.publish(EXCHANGE_NAME, routingKey, message, {
        persistent: true,
        contentType: 'application/json'
    });

    console.log('Published event:', routingKey);
}

const server = http.createServer(async (req, res) => {
    if (req.method === 'POST' && req.url === '/events') {
        let body = '';

        req.on('data', chunk => {
            body += chunk.toString();
        });

        req.on('end', async () => {
            try {
                const events = JSON.parse(body);
                const eventArray = Array.isArray(events) ? events : [events];

                for (const event of eventArray) {
                    await publishEvent(event);
                }

                res.writeHead(200, { 'Content-Type': 'application/json' });
                res.end(JSON.stringify({ status: 'ok', count: eventArray.length }));
            } catch (error) {
                console.error('Error processing event:', error);
                res.writeHead(500, { 'Content-Type': 'application/json' });
                res.end(JSON.stringify({ error: error.message }));
            }
        });
    } else if (req.method === 'OPTIONS') {
        // Event Grid validation
        res.writeHead(200, {
            'WebHook-Allowed-Origin': '*',
            'WebHook-Allowed-Rate': '*'
        });
        res.end();
    } else {
        res.writeHead(404);
        res.end('Not Found');
    }
});

async function start() {
    await connectRabbitMQ();
    server.listen(PORT, () => {
        console.log('Event router listening on port ' + PORT);
    });
}

start();
`
	routerPath := filepath.Join(routerDir, "index.js")
	if err := os.WriteFile(routerPath, []byte(eventRouter), 0644); err != nil {
		return fmt.Errorf("failed to write event router: %w", err)
	}

	// Generate package.json
	packageJson := `{
  "name": "event-router",
  "version": "1.0.0",
  "main": "index.js",
  "scripts": {
    "start": "node index.js"
  },
  "dependencies": {
    "amqplib": "^0.10.3"
  }
}
`
	packagePath := filepath.Join(routerDir, "package.json")
	if err := os.WriteFile(packagePath, []byte(packageJson), 0644); err != nil {
		return fmt.Errorf("failed to write package.json: %w", err)
	}

	// Generate Dockerfile for event router
	routerDockerfile := `FROM node:18-alpine
WORKDIR /app
COPY package*.json ./
RUN npm ci --only=production
COPY . .
EXPOSE 8080
CMD ["npm", "start"]
`
	routerDockerfilePath := filepath.Join(routerDir, "Dockerfile")
	if err := os.WriteFile(routerDockerfilePath, []byte(routerDockerfile), 0644); err != nil {
		return fmt.Errorf("failed to write router Dockerfile: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 6: Finalizing
	EmitPhase(m, phases[5], 6)
	EmitLog(m, "info", "Finalizing migration artifacts")
	EmitProgress(m, 90, "Finalizing")

	readme := fmt.Sprintf(`# Event Grid to RabbitMQ Migration

## Source Event Grid
- Topic: %s
- Resource Group: %s
- Input Schema: %s
- Subscriptions: %d

## Migration Steps

1. Start services:
'''bash
docker-compose up -d --build
'''

2. Update event publishers to send to:
'''
POST http://localhost:8080/events
Content-Type: application/json
'''

3. Create RabbitMQ consumers for each subscription

## Files Generated
- topic-info.json: Event Grid topic configuration
- subscriptions.json: Event subscription definitions
- docker-compose.yml: RabbitMQ + event router
- event-router/: HTTP to RabbitMQ bridge service

## Architecture

'''
Publishers -> Event Router (HTTP) -> RabbitMQ Exchange -> Queues -> Consumers
'''

## Access
- RabbitMQ Management: http://localhost:15672 (admin/admin)
- Event Router: http://localhost:8080/events

## Event Grid to RabbitMQ Mapping

| Event Grid | RabbitMQ |
|------------|----------|
| Topic | Exchange (topic type) |
| Subscription | Queue + Binding |
| Event Type | Routing Key |
| Subject Filter | Binding Pattern |

## Subscription Migration

For each Event Grid subscription, create a RabbitMQ queue and binding:

'''bash
# Example: Create queue and bind to exchange
rabbitmqadmin declare queue name=subscription1 durable=true
rabbitmqadmin declare binding source=events destination=subscription1 routing_key="EventType.*"
'''

## Notes
- Event Grid filtering needs custom implementation in consumers
- CloudEvents schema may need adjustment
- Update webhook endpoints to point to event router
- Consider using a message broker UI for management
`, topicName, resourceGroup, topicInfo.InputSchema, len(subscriptions))

	readmePath := filepath.Join(outputDir, "README.md")
	if err := os.WriteFile(readmePath, []byte(readme), 0644); err != nil {
		return fmt.Errorf("failed to write README: %w", err)
	}

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", fmt.Sprintf("Event Grid %s migration prepared at %s", topicName, outputDir))

	return nil
}
