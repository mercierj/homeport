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

// ServiceBusMapper converts Azure Service Bus namespaces to RabbitMQ.
type ServiceBusMapper struct {
	*mapper.BaseMapper
}

// NewServiceBusMapper creates a new Service Bus to RabbitMQ mapper.
func NewServiceBusMapper() *ServiceBusMapper {
	return &ServiceBusMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeServiceBus, nil),
	}
}

// Map converts a Service Bus namespace to a RabbitMQ service.
func (m *ServiceBusMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	namespaceName := res.GetConfigString("name")
	if namespaceName == "" {
		namespaceName = res.Name
	}

	result := mapper.NewMappingResult("rabbitmq")
	svc := result.DockerService

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
		"cloudexit.source":                                           "azurerm_servicebus_namespace",
		"cloudexit.namespace_name":                                   namespaceName,
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

	// Handle SKU tier
	sku := res.GetConfigString("sku")
	if sku == "Premium" {
		result.AddWarning("Premium tier detected. RabbitMQ supports clustering for high availability.")
		result.AddManualStep("Consider setting up RabbitMQ clustering for production workloads")
	}

	// Handle capacity units
	if capacity := res.GetConfigInt("capacity"); capacity > 1 {
		result.AddWarning(fmt.Sprintf("Capacity units: %d. Scale RabbitMQ resources accordingly.", capacity))
	}

	definitions := m.generateRabbitMQDefinitions(namespaceName)
	result.AddConfig("config/rabbitmq/definitions.json", []byte(definitions))

	config := m.generateRabbitMQConfig()
	result.AddConfig("config/rabbitmq/rabbitmq.conf", []byte(config))

	setupScript := m.generateSetupScript(namespaceName)
	result.AddScript("setup_rabbitmq_servicebus.sh", []byte(setupScript))

	result.AddManualStep("Access RabbitMQ management console at http://localhost:15672")
	result.AddManualStep("Default credentials: guest/guest")
	result.AddManualStep("Update application code to use AMQP instead of Azure Service Bus SDK")

	return result, nil
}

func (m *ServiceBusMapper) generateRabbitMQDefinitions(namespaceName string) string {
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
			{"name": namespaceName},
		},
		"permissions": []map[string]interface{}{
			{
				"user":      "guest",
				"vhost":     "/",
				"configure": ".*",
				"write":     ".*",
				"read":      ".*",
			},
			{
				"user":      "guest",
				"vhost":     namespaceName,
				"configure": ".*",
				"write":     ".*",
				"read":      ".*",
			},
		},
		"queues":    []map[string]interface{}{},
		"exchanges": []map[string]interface{}{},
		"bindings":  []map[string]interface{}{},
	}
	content, _ := json.MarshalIndent(definitions, "", "  ")
	return string(content)
}

func (m *ServiceBusMapper) generateRabbitMQConfig() string {
	return `# RabbitMQ Configuration
# Generated from Azure Service Bus namespace

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

func (m *ServiceBusMapper) generateSetupScript(namespaceName string) string {
	return fmt.Sprintf(`#!/bin/bash
# RabbitMQ Setup Script for Service Bus namespace: %s

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
echo "Management UI: http://$RABBITMQ_HOST:$RABBITMQ_PORT"
echo "Namespace vhost: %s"
echo "Connection string: amqp://$RABBITMQ_USER:$RABBITMQ_PASS@$RABBITMQ_HOST:5672/%s"
`, namespaceName, namespaceName, namespaceName)
}
