// Package messaging provides mappers for Azure messaging services.
package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

// ServiceBusQueueMapper converts Azure Service Bus queues to RabbitMQ queues.
type ServiceBusQueueMapper struct {
	*mapper.BaseMapper
}

// NewServiceBusQueueMapper creates a new Service Bus Queue to RabbitMQ mapper.
func NewServiceBusQueueMapper() *ServiceBusQueueMapper {
	return &ServiceBusQueueMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeServiceBusQueue, nil),
	}
}

// Map converts a Service Bus queue to a RabbitMQ service.
func (m *ServiceBusQueueMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	queueName := res.GetConfigString("name")
	if queueName == "" {
		queueName = res.Name
	}

	result := mapper.NewMappingResult("rabbitmq")
	svc := result.DockerService

	svc.Image = "rabbitmq:3.12-management-alpine"
	svc.Environment = map[string]string{
		"RABBITMQ_DEFAULT_USER": "guest",
		"RABBITMQ_DEFAULT_PASS": "guest",
	}
	svc.Ports = []string{
		"5672:5672",
		"15672:15672",
	}
	svc.Volumes = []string{
		"./data/rabbitmq:/var/lib/rabbitmq",
		"./config/rabbitmq/definitions.json:/etc/rabbitmq/definitions.json",
		"./config/rabbitmq/rabbitmq.conf:/etc/rabbitmq/rabbitmq.conf",
	}
	svc.Networks = []string{"cloudexit"}
	svc.Labels = map[string]string{
		"cloudexit.source":                                           "azurerm_servicebus_queue",
		"cloudexit.queue_name":                                       queueName,
		"traefik.enable":                                             "true",
		"traefik.http.routers.rabbitmq.rule":                         "Host(`rabbitmq.localhost`)",
		"traefik.http.services.rabbitmq.loadbalancer.server.port":    "15672",
	}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "rabbitmq-diagnostics", "-q", "ping"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  5,
	}
	svc.Restart = "unless-stopped"

	// Handle dead lettering
	if res.GetConfigBool("dead_lettering_on_message_expiration") {
		result.AddWarning("Dead lettering enabled. Configure dead letter exchange in RabbitMQ.")
		result.AddManualStep("Set up dead letter exchange with x-dead-letter-exchange argument")
	}

	// Handle sessions
	if res.GetConfigBool("requires_session") {
		result.AddWarning("Sessions required. Implement session affinity in your application.")
		result.AddManualStep("Use consistent hashing exchange or implement session logic in consumer")
	}

	// Handle duplicate detection
	if res.GetConfigBool("requires_duplicate_detection") {
		result.AddWarning("Duplicate detection enabled. Implement idempotency in consumers.")
		result.AddManualStep("Implement message deduplication using message IDs in your application")
	}

	// Handle max delivery count
	maxDeliveryCount := res.GetConfigInt("max_delivery_count")
	if maxDeliveryCount > 0 {
		result.AddWarning(fmt.Sprintf("Max delivery count: %d. Configure delivery limit in RabbitMQ.", maxDeliveryCount))
	}

	// Handle lock duration
	if lockDuration := res.GetConfigString("lock_duration"); lockDuration != "" {
		result.AddWarning(fmt.Sprintf("Lock duration: %s. Configure consumer timeout in RabbitMQ.", lockDuration))
	}

	definitions := m.generateRabbitMQDefinitions(res, queueName)
	result.AddConfig("config/rabbitmq/definitions.json", []byte(definitions))

	config := m.generateRabbitMQConfig()
	result.AddConfig("config/rabbitmq/rabbitmq.conf", []byte(config))

	setupScript := m.generateSetupScript(queueName)
	result.AddScript("setup_rabbitmq_queue.sh", []byte(setupScript))

	result.AddManualStep("Access RabbitMQ management console at http://localhost:15672")
	result.AddManualStep("Default credentials: guest/guest")
	result.AddManualStep("Update application code to use AMQP instead of Azure Service Bus SDK")

	return result, nil
}

func (m *ServiceBusQueueMapper) generateRabbitMQDefinitions(res *resource.AWSResource, queueName string) string {
	queueArgs := map[string]interface{}{}

	// Handle message TTL
	if defaultTTL := res.GetConfigString("default_message_ttl"); defaultTTL != "" {
		queueArgs["x-message-ttl"] = 86400000 // 24 hours default
	}

	// Handle max size
	if maxSize := res.GetConfigInt("max_size_in_megabytes"); maxSize > 0 {
		queueArgs["x-max-length-bytes"] = maxSize * 1024 * 1024
	}

	definitions := map[string]interface{}{
		"rabbit_version":   "3.12.0",
		"rabbitmq_version": "3.12.0",
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
		"queues": []map[string]interface{}{
			{
				"name":        queueName,
				"vhost":       "/",
				"durable":     true,
				"auto_delete": false,
				"arguments":   queueArgs,
			},
		},
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

func (m *ServiceBusQueueMapper) generateRabbitMQConfig() string {
	return `# RabbitMQ Configuration
# Generated from Azure Service Bus queue

listeners.tcp.default = 5672
management.tcp.port = 15672
management.load_definitions = /etc/rabbitmq/definitions.json

default_user = guest
default_pass = guest
default_vhost = /

disk_free_limit.absolute = 2GB
vm_memory_high_watermark.relative = 0.6

heartbeat = 60
consumer_timeout = 1800000

log.console.level = info
log.file.level = info
`
}

func (m *ServiceBusQueueMapper) generateSetupScript(queueName string) string {
	return fmt.Sprintf(`#!/bin/bash
# RabbitMQ Setup Script for Service Bus queue: %s

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
echo "Queue '%s' created"
echo "Management UI: http://$RABBITMQ_HOST:$RABBITMQ_PORT"
`, queueName, queueName)
}
