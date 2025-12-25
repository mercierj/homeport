// Package messaging provides mappers for AWS messaging services.
package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

// SQSMapper converts AWS SQS queues to RabbitMQ.
type SQSMapper struct {
	*mapper.BaseMapper
}

// NewSQSMapper creates a new SQS to RabbitMQ mapper.
func NewSQSMapper() *SQSMapper {
	return &SQSMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeSQSQueue, nil),
	}
}

// Map converts an SQS queue to a RabbitMQ service.
func (m *SQSMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	queueName := res.GetConfigString("name")
	if queueName == "" {
		queueName = res.Name
	}

	// Create result using new API
	result := mapper.NewMappingResult("rabbitmq")
	svc := result.DockerService

	// Configure RabbitMQ service
	svc.Image = "rabbitmq:3.12-management-alpine"
	svc.Environment = map[string]string{
		"RABBITMQ_DEFAULT_USER": "guest",
		"RABBITMQ_DEFAULT_PASS": "guest",
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
	svc.Networks = []string{"cloudexit"}
	svc.Labels = map[string]string{
		"cloudexit.source":     "aws_sqs_queue",
		"cloudexit.queue_name": queueName,
		"traefik.enable":       "true",
		"traefik.http.routers.rabbitmq.rule":                      "Host(`rabbitmq.localhost`)",
		"traefik.http.services.rabbitmq.loadbalancer.server.port": "15672",
	}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "rabbitmq-diagnostics", "-q", "ping"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  5,
	}

	// Generate RabbitMQ definitions (queues, exchanges, bindings)
	definitions := m.generateRabbitMQDefinitions(res, queueName)
	result.AddConfig("config/rabbitmq/definitions.json", []byte(definitions))

	// Generate queue configuration
	queueConfig := m.generateQueueConfig(queueName)
	result.AddConfig("config/rabbitmq/rabbitmq.conf", []byte(queueConfig))

	// Handle FIFO queue
	if m.isFIFOQueue(queueName) || res.GetConfigBool("fifo_queue") {
		result.AddWarning("FIFO queue detected. RabbitMQ doesn't guarantee strict FIFO ordering across multiple consumers. Consider using single active consumer pattern.")
		result.AddManualStep("Review FIFO requirements and configure single active consumer if strict ordering is needed")
	}

	// Handle dead letter queue
	if dlqArn := res.GetConfigString("redrive_policy"); dlqArn != "" || res.Config["redrive_policy"] != nil {
		m.handleDeadLetterQueue(res, result, queueName)
	}

	// Handle visibility timeout
	visibilityTimeout := res.GetConfigInt("visibility_timeout_seconds")
	if visibilityTimeout > 0 && visibilityTimeout != 30 {
		result.AddWarning(fmt.Sprintf("Visibility timeout is %d seconds. Configure message TTL in RabbitMQ.", visibilityTimeout))
	}

	// Handle message retention
	messageRetention := res.GetConfigInt("message_retention_seconds")
	if messageRetention > 0 {
		days := messageRetention / 86400
		result.AddWarning(fmt.Sprintf("Message retention is %d days. Configure queue TTL in RabbitMQ.", days))
	}

	// Handle delay queue
	delaySeconds := res.GetConfigInt("delay_seconds")
	if delaySeconds > 0 {
		result.AddWarning(fmt.Sprintf("Default delay is %d seconds. Use RabbitMQ delayed message plugin or TTL.", delaySeconds))
		result.AddManualStep("Install RabbitMQ delayed message plugin: rabbitmq-plugins enable rabbitmq_delayed_message_exchange")
	}

	// Generate setup script
	setupScript := m.generateSetupScript(queueName)
	result.AddScript("setup_rabbitmq.sh", []byte(setupScript))

	result.AddManualStep("Access RabbitMQ management console at http://localhost:15672")
	result.AddManualStep("Default credentials: guest/guest")
	result.AddManualStep("Import queue definitions from config/rabbitmq/definitions.json")
	result.AddManualStep("Update application code to use AMQP instead of SQS SDK")

	return result, nil
}

// generateRabbitMQDefinitions creates RabbitMQ definitions JSON.
func (m *SQSMapper) generateRabbitMQDefinitions(res *resource.AWSResource, queueName string) string {
	// Build queue definition
	queueArgs := map[string]interface{}{}

	// Add message TTL if message retention is set
	if messageRetention := res.GetConfigInt("message_retention_seconds"); messageRetention > 0 {
		queueArgs["x-message-ttl"] = messageRetention * 1000 // Convert to milliseconds
	}

	queueDef := map[string]interface{}{
		"name":        queueName,
		"vhost":       "/",
		"durable":     true,
		"auto_delete": false,
		"arguments":   queueArgs,
	}

	definitions := map[string]interface{}{
		"rabbit_version":    "3.12.0",
		"rabbitmq_version":  "3.12.0",
		"users": []map[string]interface{}{
			{
				"name":              "guest",
				"password_hash":     "guest",
				"hashing_algorithm": "rabbit_password_hashing_sha256",
				"tags":              "administrator",
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
		"queues": []map[string]interface{}{queueDef},
		"exchanges": []map[string]interface{}{
			{
				"name":        "amq.direct",
				"vhost":       "/",
				"type":        "direct",
				"durable":     true,
				"auto_delete": false,
				"internal":    false,
			},
		},
		"bindings": []map[string]interface{}{
			{
				"source":           "amq.direct",
				"vhost":            "/",
				"destination":      queueName,
				"destination_type": "queue",
				"routing_key":      queueName,
			},
		},
	}

	content, _ := json.MarshalIndent(definitions, "", "  ")
	return string(content)
}

// generateQueueConfig creates a RabbitMQ configuration file.
func (m *SQSMapper) generateQueueConfig(queueName string) string {
	return `# RabbitMQ Configuration
# Generated from SQS queue settings

# Listeners
listeners.tcp.default = 5672

# Management plugin
management.tcp.port = 15672

# Load definitions on startup
management.load_definitions = /etc/rabbitmq/definitions.json

# Default user
default_user = guest
default_pass = guest

# Default vhost
default_vhost = /

# Disk free limit (warning threshold)
disk_free_limit.absolute = 2GB

# Memory threshold
vm_memory_high_watermark.relative = 0.6

# Heartbeat
heartbeat = 60

# Consumer timeout (milliseconds)
consumer_timeout = 1800000

# Log level
log.console.level = info
log.file.level = info
`
}

// handleDeadLetterQueue configures dead letter queue settings.
func (m *SQSMapper) handleDeadLetterQueue(res *resource.AWSResource, result *mapper.MappingResult, queueName string) {
	dlqName := queueName + "-dlq"

	result.AddWarning(fmt.Sprintf("Dead letter queue configured. Creating DLQ: %s", dlqName))
	result.AddManualStep("Configure dead letter exchange in RabbitMQ definitions")
	result.AddManualStep("Set x-dead-letter-exchange and x-dead-letter-routing-key on main queue")

	dlqNote := fmt.Sprintf(`
# Dead Letter Queue Configuration
# Add these arguments to the main queue in definitions.json:
# "x-dead-letter-exchange": "dlx",
# "x-dead-letter-routing-key": "%s"
#
# Also create a DLQ queue and DLX exchange in definitions.json
`, dlqName)

	result.AddConfig("config/rabbitmq/dlq-setup.txt", []byte(dlqNote))
}

// generateSetupScript creates a setup script for RabbitMQ.
func (m *SQSMapper) generateSetupScript(queueName string) string {
	return fmt.Sprintf(`#!/bin/bash
# RabbitMQ Setup Script
# Configures RabbitMQ for SQS queue: %s

set -e

RABBITMQ_HOST="${RABBITMQ_HOST:-localhost}"
RABBITMQ_PORT="${RABBITMQ_PORT:-15672}"
RABBITMQ_USER="${RABBITMQ_USER:-guest}"
RABBITMQ_PASS="${RABBITMQ_PASS:-guest}"

echo "Waiting for RabbitMQ to be ready..."
until curl -sf http://$RABBITMQ_HOST:$RABBITMQ_PORT/api/overview -u $RABBITMQ_USER:$RABBITMQ_PASS > /dev/null; do
  echo "Waiting..."
  sleep 5
done

echo "RabbitMQ is ready!"

# Import definitions
echo "Importing queue definitions..."
curl -X POST -u $RABBITMQ_USER:$RABBITMQ_PASS \
  http://$RABBITMQ_HOST:$RABBITMQ_PORT/api/definitions \
  -H "Content-Type: application/json" \
  -d @config/rabbitmq/definitions.json

echo "Queue '%s' created successfully!"
echo "Management UI: http://$RABBITMQ_HOST:$RABBITMQ_PORT"
echo "Credentials: $RABBITMQ_USER / $RABBITMQ_PASS"
echo ""
echo "Connection string: amqp://$RABBITMQ_USER:$RABBITMQ_PASS@$RABBITMQ_HOST:5672/"
`, queueName, queueName)
}

// isFIFOQueue checks if the queue is a FIFO queue based on naming convention.
func (m *SQSMapper) isFIFOQueue(queueName string) bool {
	return len(queueName) > 5 && queueName[len(queueName)-5:] == ".fifo"
}
