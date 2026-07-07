// Package messaging provides mappers for GCP messaging services.
package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

// PubSubMapper converts GCP Pub/Sub topics to RabbitMQ with topic exchange pattern.
type PubSubMapper struct {
	*mapper.BaseMapper
}

// NewPubSubMapper creates a new Pub/Sub to RabbitMQ mapper.
func NewPubSubMapper() *PubSubMapper {
	return &PubSubMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypePubSubTopic, nil),
	}
}

// Map converts a Pub/Sub topic to a RabbitMQ service with topic exchange.
func (m *PubSubMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	topicName := res.GetConfigString("name")
	if topicName == "" {
		topicName = res.Name
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
	svc.Networks = []string{"homeport"}
	svc.Labels = map[string]string{
		"homeport.source":                    "google_pubsub_topic",
		"homeport.topic_name":                topicName,
		"traefik.enable":                     "true",
		"traefik.http.routers.rabbitmq.rule": "Host(`rabbitmq.localhost`)",
		"traefik.http.services.rabbitmq.loadbalancer.server.port": "15672",
	}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "rabbitmq-diagnostics", "-q", "ping"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  5,
	}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"

	definitions := m.generateRabbitMQDefinitions(res, topicName)
	result.AddConfig("config/rabbitmq/definitions.json", []byte(definitions))

	topicConfig := m.generateTopicConfig()
	result.AddConfig("config/rabbitmq/rabbitmq.conf", []byte(topicConfig))

	if res.GetConfigBool("message_ordering_enabled") {
		result.AddWarning("Message ordering enabled. Consider using single active consumer pattern.")
	}

	if deadLetterTopic := res.GetConfigString("dead_letter_topic"); deadLetterTopic != "" {
		result.AddWarning(fmt.Sprintf("Dead letter topic configured: %s", deadLetterTopic))
	}

	setupScript := m.generateSetupScript(topicName)
	result.AddScript("setup_rabbitmq_pubsub.sh", []byte(setupScript))
	result.AddScript("validate_pubsub_rabbitmq.sh", []byte(m.generateValidateScript(topicName)))
	result.AddScript("backup_pubsub_rabbitmq.sh", []byte(m.generateBackupScript(topicName)))
	result.AddConfig("config/pubsub/app-change.env", []byte(m.generateAppChangeConfig(topicName, topicName+"-default-subscription")))
	for _, step := range pubSubRunbook(topicName) {
		result.AddRunbookStep(step)
	}

	return result, nil
}

func (m *PubSubMapper) generateRabbitMQDefinitions(res *resource.AWSResource, topicName string) string {
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
		"exchanges": []map[string]interface{}{
			{
				"name":        topicName,
				"vhost":       "/",
				"type":        "topic",
				"durable":     true,
				"auto_delete": false,
				"internal":    false,
			},
		},
		"queues": []map[string]interface{}{
			{
				"name":        topicName + "-default-subscription",
				"vhost":       "/",
				"durable":     true,
				"auto_delete": false,
				"arguments":   map[string]interface{}{},
			},
		},
		"bindings": []map[string]interface{}{
			{
				"source":           topicName,
				"vhost":            "/",
				"destination":      topicName + "-default-subscription",
				"destination_type": "queue",
				"routing_key":      "#",
			},
		},
	}
	content, _ := json.MarshalIndent(definitions, "", "  ")
	return string(content)
}

func (m *PubSubMapper) generateTopicConfig() string {
	return `# RabbitMQ Configuration
# Generated from GCP Pub/Sub topic settings

listeners.tcp.default = 5672
management.tcp.port = 15672
management.load_definitions = /etc/rabbitmq/definitions.json

default_user = guest
default_pass = guest
default_vhost = /

disk_free_limit.absolute = 2GB
vm_memory_high_watermark.relative = 0.6

heartbeat = 60
consumer_timeout = 600000

log.console.level = info
log.file.level = info

queue_master_locator = min-masters
`
}

func (m *PubSubMapper) generateSetupScript(topicName string) string {
	return fmt.Sprintf(`#!/bin/bash
# RabbitMQ Setup Script for Pub/Sub topic: %s

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
echo "Credentials: $RABBITMQ_USER / $RABBITMQ_PASS"
echo "Connection string: amqp://$RABBITMQ_USER:$RABBITMQ_PASS@$RABBITMQ_HOST:5672/"
`, topicName)
}

func (m *PubSubMapper) generateAppChangeConfig(topicName, queueName string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_PUBSUB_TOPIC=%s\nTARGET_AMQP_URL=amqp://guest:guest@rabbitmq:5672/\nTARGET_EXCHANGE=%s\nTARGET_QUEUE=%s\n", topicName, topicName, queueName)
}

func (m *PubSubMapper) generateValidateScript(topicName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/pubsub/app-change.env\nrabbitmq-diagnostics -q ping\necho \"Pub/Sub topic %s RabbitMQ target validated\"\n", topicName)
}

func (m *PubSubMapper) generateBackupScript(topicName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/pubsub-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/rabbitmq config/pubsub data/rabbitmq\necho \"$archive\"\n", topicName)
}

func pubSubRunbook(topicName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "messaging", "source": "google_pubsub_topic", "topic": topicName, "target": "rabbitmq"}
	return []domainrunbook.Step{
		pubSubStep("provision-pubsub-rabbitmq", "Provision RabbitMQ Pub/Sub target", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "setup_rabbitmq_pubsub.sh"}, "RabbitMQ exchange and queue are configured", metadata),
		pubSubStep("validate-pubsub-rabbitmq", "Validate RabbitMQ Pub/Sub target", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_pubsub_rabbitmq.sh"}, "RabbitMQ health and app-change config validate", metadata),
		pubSubStep("backup-pubsub-rabbitmq", "Backup RabbitMQ Pub/Sub config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_pubsub_rabbitmq.sh"}, "RabbitMQ Pub/Sub config is archived", metadata),
		pubSubStep("cutover-pubsub-clients", "Cut over Pub/Sub clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "-c", "test -s config/pubsub/app-change.env"}, "applications use generated AMQP target", metadata),
		pubSubStep("rollback-pubsub-source-authority", "Keep Pub/Sub source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "Pub/Sub remains authoritative until cutover passes", metadata),
	}
}

func pubSubStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}
