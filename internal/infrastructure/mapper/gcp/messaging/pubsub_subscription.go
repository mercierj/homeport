// Package messaging provides mappers for GCP messaging services.
package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

// PubSubSubscriptionMapper converts GCP Pub/Sub subscriptions to RabbitMQ queues.
type PubSubSubscriptionMapper struct {
	*mapper.BaseMapper
}

// NewPubSubSubscriptionMapper creates a new Pub/Sub subscription to RabbitMQ mapper.
func NewPubSubSubscriptionMapper() *PubSubSubscriptionMapper {
	return &PubSubSubscriptionMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypePubSubSubscription, nil),
	}
}

// Map converts a Pub/Sub subscription to a RabbitMQ queue bound to a topic exchange.
func (m *PubSubSubscriptionMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	subscriptionName := res.GetConfigString("name")
	if subscriptionName == "" {
		subscriptionName = res.Name
	}

	topicName := res.GetConfigString("topic")
	ackDeadlineSec := res.GetConfigInt("ack_deadline_seconds")
	if ackDeadlineSec == 0 {
		ackDeadlineSec = 10 // Default Pub/Sub ack deadline
	}

	result := mapper.NewMappingResult("rabbitmq")
	svc := result.DockerService

	svc.Image = "rabbitmq:3.12-management-alpine"
	svc.Environment = map[string]string{
		"RABBITMQ_DEFAULT_USER": "${RABBITMQ_USER:-guest}",
		"RABBITMQ_DEFAULT_PASS": "${RABBITMQ_PASSWORD:-guest}",
	}
	svc.Ports = []string{
		"5672:5672",   // AMQP
		"15672:15672", // Management UI
	}
	svc.Volumes = []string{
		"./data/rabbitmq:/var/lib/rabbitmq",
		"./config/rabbitmq/definitions.json:/etc/rabbitmq/definitions.json",
		"./config/rabbitmq/rabbitmq.conf:/etc/rabbitmq/rabbitmq.conf",
	}
	svc.Networks = []string{"homeport"}
	svc.Labels = map[string]string{
		"homeport.source":                                        "google_pubsub_subscription",
		"homeport.subscription_name":                             subscriptionName,
		"homeport.topic_name":                                    topicName,
		"traefik.enable":                                         "true",
		"traefik.http.routers.rabbitmq.rule":                     "Host(`rabbitmq.localhost`)",
		"traefik.http.services.rabbitmq.loadbalancer.server.port": "15672",
	}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "rabbitmq-diagnostics", "-q", "ping"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  5,
	}
	svc.Restart = "unless-stopped"

	// Generate RabbitMQ definitions for the subscription queue
	definitions := m.generateDefinitions(res, subscriptionName, topicName, ackDeadlineSec)
	result.AddConfig("config/rabbitmq/definitions.json", []byte(definitions))

	// Generate RabbitMQ config
	rabbitConfig := m.generateRabbitConfig()
	result.AddConfig("config/rabbitmq/rabbitmq.conf", []byte(rabbitConfig))

	// Generate migration script
	migrationScript := m.generateMigrationScript(res, subscriptionName, topicName)
	result.AddScript("scripts/migrate-pubsub-subscription.sh", []byte(migrationScript))

	// Add warnings based on subscription configuration
	m.addMigrationWarnings(result, res, subscriptionName, topicName)

	return result, nil
}

func (m *PubSubSubscriptionMapper) generateDefinitions(res *resource.AWSResource, subscriptionName, topicName string, ackDeadlineSec int) string {
	// Convert ack deadline to RabbitMQ consumer timeout (milliseconds)
	consumerTimeout := ackDeadlineSec * 1000

	// Build queue arguments
	queueArgs := map[string]interface{}{
		"x-consumer-timeout": consumerTimeout,
	}

	// Handle dead letter configuration
	deadLetterTopic := res.GetConfigString("dead_letter_topic")
	if deadLetterTopic != "" {
		queueArgs["x-dead-letter-exchange"] = fmt.Sprintf("%s.dlx", topicName)
		queueArgs["x-dead-letter-routing-key"] = "dead-letter"
	}

	// Handle message retention
	retentionDuration := res.GetConfigString("message_retention_duration")
	if retentionDuration != "" {
		// Default to 7 days if we can't parse
		queueArgs["x-message-ttl"] = 604800000 // 7 days in ms
	}

	// Build definitions
	definitions := map[string]interface{}{
		"rabbit_version":   "3.12.0",
		"rabbitmq_version": "3.12.0",
		"users": []map[string]interface{}{
			{
				"name":              "guest",
				"password_hash":     "guest",
				"hashing_algorithm": "rabbit_password_hashing_sha256",
				"tags":              []string{"administrator"},
			},
		},
		"vhosts": []map[string]interface{}{
			{"name": "/"},
		},
		"permissions": []map[string]interface{}{
			{
				"user":      "guest",
				"vhost":     "/",
				"configure": ".*",
				"write":     ".*",
				"read":      ".*",
			},
		},
		"exchanges": []map[string]interface{}{
			{
				"name":        topicName,
				"vhost":       "/",
				"type":        "topic",
				"durable":     true,
				"auto_delete": false,
				"internal":    false,
				"arguments":   map[string]interface{}{},
			},
		},
		"queues": []map[string]interface{}{
			{
				"name":        subscriptionName,
				"vhost":       "/",
				"durable":     true,
				"auto_delete": false,
				"arguments":   queueArgs,
			},
		},
		"bindings": []map[string]interface{}{
			{
				"source":           topicName,
				"vhost":            "/",
				"destination":      subscriptionName,
				"destination_type": "queue",
				"routing_key":      "#", // Match all messages (Pub/Sub default behavior)
				"arguments":        map[string]interface{}{},
			},
		},
	}

	// Add dead letter exchange if configured
	if deadLetterTopic != "" {
		exchanges := definitions["exchanges"].([]map[string]interface{})
		exchanges = append(exchanges, map[string]interface{}{
			"name":        fmt.Sprintf("%s.dlx", topicName),
			"vhost":       "/",
			"type":        "direct",
			"durable":     true,
			"auto_delete": false,
			"internal":    false,
			"arguments":   map[string]interface{}{},
		})
		definitions["exchanges"] = exchanges

		queues := definitions["queues"].([]map[string]interface{})
		queues = append(queues, map[string]interface{}{
			"name":        fmt.Sprintf("%s.dlq", subscriptionName),
			"vhost":       "/",
			"durable":     true,
			"auto_delete": false,
			"arguments":   map[string]interface{}{},
		})
		definitions["queues"] = queues

		bindings := definitions["bindings"].([]map[string]interface{})
		bindings = append(bindings, map[string]interface{}{
			"source":           fmt.Sprintf("%s.dlx", topicName),
			"vhost":            "/",
			"destination":      fmt.Sprintf("%s.dlq", subscriptionName),
			"destination_type": "queue",
			"routing_key":      "dead-letter",
			"arguments":        map[string]interface{}{},
		})
		definitions["bindings"] = bindings
	}

	data, _ := json.MarshalIndent(definitions, "", "  ")
	return string(data)
}

func (m *PubSubSubscriptionMapper) generateRabbitConfig() string {
	return `# RabbitMQ Configuration
# Generated for Pub/Sub subscription migration

# Load definitions on startup
load_definitions = /etc/rabbitmq/definitions.json

# Management plugin settings
management.load_definitions = /etc/rabbitmq/definitions.json

# Consumer acknowledgement timeout (matches Pub/Sub ack deadline)
consumer_timeout = 1800000

# Enable message persistence
queue_master_locator = min-masters

# Logging
log.console = true
log.console.level = info

# Memory settings
vm_memory_high_watermark.relative = 0.6
vm_memory_high_watermark_paging_ratio = 0.8

# Disk space settings
disk_free_limit.relative = 1.0
`
}

func (m *PubSubSubscriptionMapper) generateMigrationScript(res *resource.AWSResource, subscriptionName, topicName string) string {
	project := res.GetConfigString("project")
	if project == "" {
		project = "<YOUR_PROJECT_ID>"
	}

	return fmt.Sprintf(`#!/bin/bash
# Pub/Sub Subscription Migration Script
# Subscription: %s
# Topic: %s

set -e

echo "============================================"
echo "Pub/Sub Subscription to RabbitMQ Migration"
echo "============================================"
echo ""

# Export subscription configuration from GCP
echo "Step 1: Exporting subscription configuration..."
mkdir -p ./pubsub-export

gcloud pubsub subscriptions describe %s \
  --project=%s \
  --format=json > ./pubsub-export/subscription-config.json

echo "Subscription configuration exported."

# Export pending messages (optional)
echo ""
echo "Step 2: Exporting pending messages (optional)..."
echo "Note: This pulls messages without acknowledging them."
echo "Run the following to export pending messages:"
echo ""
echo "  gcloud pubsub subscriptions pull %s \\"
echo "    --project=%s \\"
echo "    --limit=1000 \\"
echo "    --format=json > ./pubsub-export/pending-messages.json"
echo ""

# Start RabbitMQ
echo "Step 3: Starting RabbitMQ..."
docker-compose up -d rabbitmq

# Wait for RabbitMQ to be ready
echo "Waiting for RabbitMQ to be ready..."
until docker-compose exec rabbitmq rabbitmq-diagnostics -q ping 2>/dev/null; do
  sleep 2
done
echo "RabbitMQ is ready."

# Import messages (if exported)
if [ -f "./pubsub-export/pending-messages.json" ]; then
  echo ""
  echo "Step 4: Importing pending messages..."
  # Note: You'll need to transform the messages and publish them
  echo "Use the RabbitMQ management API or a client library to import messages."
  echo "See: http://localhost:15672"
fi

echo ""
echo "============================================"
echo "Migration Complete!"
echo "============================================"
echo ""
echo "Subscription: %s -> Queue: %s"
echo "Topic: %s -> Exchange: %s"
echo ""
echo "RabbitMQ Management: http://localhost:15672"
echo "AMQP Connection: amqp://guest:guest@localhost:5672/"
echo ""
echo "Update your application to:"
echo "  1. Connect to RabbitMQ instead of Pub/Sub"
echo "  2. Consume from queue: %s"
echo "  3. Use AMQP client library (e.g., amqplib, pika, bunny)"
echo ""
`, subscriptionName, topicName, subscriptionName, project, subscriptionName, project, subscriptionName, subscriptionName, topicName, topicName, subscriptionName)
}

func (m *PubSubSubscriptionMapper) addMigrationWarnings(result *mapper.MappingResult, res *resource.AWSResource, subscriptionName, topicName string) {
	// Push endpoint warning
	if pushEndpoint := res.GetConfigString("push_endpoint"); pushEndpoint != "" {
		result.AddWarning(fmt.Sprintf("Push subscription to %s detected. Configure RabbitMQ webhook plugin or consumer application.", pushEndpoint))
		result.AddManualStep("Set up a consumer application to poll the RabbitMQ queue and forward to your endpoint")
	}

	// Message ordering warning
	if res.GetConfigBool("enable_message_ordering") {
		result.AddWarning("Message ordering enabled. Use single active consumer pattern in RabbitMQ.")
		result.AddManualStep("Configure consumer with x-single-active-consumer=true")
	}

	// Exactly once delivery warning
	if res.GetConfigBool("enable_exactly_once_delivery") {
		result.AddWarning("Exactly-once delivery enabled. RabbitMQ provides at-least-once by default.")
		result.AddManualStep("Implement idempotent message processing in your consumer")
	}

	// Filter warning
	if filter := res.GetConfigString("filter"); filter != "" {
		result.AddWarning(fmt.Sprintf("Subscription filter detected: %s. Convert to RabbitMQ routing keys.", filter))
		result.AddManualStep("Update binding routing key to match your filter pattern")
	}

	// Retry policy warning
	if res.Config["retry_policy"] != nil {
		result.AddWarning("Retry policy configured. Configure RabbitMQ dead letter exchange for failed messages.")
	}

	// Dead letter topic warning
	if deadLetterTopic := res.GetConfigString("dead_letter_topic"); deadLetterTopic != "" {
		result.AddWarning(fmt.Sprintf("Dead letter topic %s configured. DLX has been set up in RabbitMQ definitions.", deadLetterTopic))
	}

	// Standard manual steps
	result.AddManualStep("Run scripts/migrate-pubsub-subscription.sh to export Pub/Sub configuration")
	result.AddManualStep("Access RabbitMQ management console at http://localhost:15672")
	result.AddManualStep("Update application code to use AMQP client instead of Pub/Sub SDK")
	result.AddManualStep(fmt.Sprintf("Consume from queue '%s' bound to exchange '%s'", subscriptionName, topicName))

	// Volumes
	result.AddVolume(mapper.Volume{
		Name:   "rabbitmq-data",
		Driver: "local",
	})
}
