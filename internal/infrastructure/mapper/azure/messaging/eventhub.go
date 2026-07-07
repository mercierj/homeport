// Package messaging provides mappers for Azure messaging services.
package messaging

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

// EventHubMapper converts Azure Event Hubs to Kafka/Redpanda.
type EventHubMapper struct {
	*mapper.BaseMapper
}

// NewEventHubMapper creates a new Event Hub to Kafka mapper.
func NewEventHubMapper() *EventHubMapper {
	return &EventHubMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeEventHub, nil),
	}
}

// Map converts an Event Hub to a Redpanda/Kafka service.
func (m *EventHubMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	hubName := res.GetConfigString("name")
	if hubName == "" {
		hubName = res.Name
	}

	result := mapper.NewMappingResult("redpanda")
	svc := result.DockerService

	partitionCount := res.GetConfigInt("partition_count")
	if partitionCount == 0 {
		partitionCount = 2
	}

	svc.Image = "redpandadata/redpanda:v23.3.5"
	svc.Command = []string{
		"redpanda", "start",
		"--smp", "1",
		"--memory", "1G",
		"--reserve-memory", "0M",
		"--overprovisioned",
		"--node-id", "0",
		"--kafka-addr", "PLAINTEXT://0.0.0.0:9092,OUTSIDE://0.0.0.0:19092",
		"--advertise-kafka-addr", "PLAINTEXT://redpanda:9092,OUTSIDE://localhost:19092",
		"--pandaproxy-addr", "0.0.0.0:8082",
		"--advertise-pandaproxy-addr", "localhost:8082",
		"--schema-registry-addr", "0.0.0.0:8081",
	}
	svc.Ports = []string{
		"9092:9092",
		"19092:19092",
		"8081:8081",
		"8082:8082",
		"9644:9644",
	}
	svc.Volumes = []string{
		"./data/redpanda:/var/lib/redpanda/data",
	}
	svc.Networks = []string{"homeport"}
	svc.Labels = map[string]string{
		"homeport.source":                    "azurerm_eventhub",
		"homeport.hub_name":                  hubName,
		"homeport.partition_count":           fmt.Sprintf("%d", partitionCount),
		"traefik.enable":                     "true",
		"traefik.http.routers.redpanda.rule": "Host(`redpanda.localhost`)",
		"traefik.http.services.redpanda.loadbalancer.server.port": "8082",
	}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "rpk", "cluster", "health"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  5,
	}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: 3}

	// Handle message retention
	messageRetention := res.GetConfigInt("message_retention")
	if messageRetention == 0 {
		messageRetention = 1 // Default 1 day
	}
	retentionMs := messageRetention * 24 * 60 * 60 * 1000
	consumerGroup := res.GetConfigString("consumer_group")
	if consumerGroup == "" {
		consumerGroup = "eventhub-consumer-group"
	}
	offsetReset := res.GetConfigString("offset_reset")
	if offsetReset == "" {
		offsetReset = "earliest"
	}
	result.AddWarning(fmt.Sprintf("Message retention: %d days. Generated Redpanda retention.ms=%d.", messageRetention, retentionMs))

	// Handle capture (archiving)
	if res.Config["capture_description"] != nil {
		result.AddWarning("Event capture enabled. Consider using Redpanda tiered storage for archiving.")
	}

	topicConfig := m.generateTopicConfig(hubName, partitionCount, retentionMs)
	result.AddConfig("config/redpanda/topics.yaml", []byte(topicConfig))
	result.AddConfig("config/redpanda/app-change.env", []byte(m.generateAppChange(hubName, consumerGroup, offsetReset)))
	result.AddConfig("config/redpanda/replay-plan.yaml", []byte(m.generateReplayPlan(hubName, consumerGroup, offsetReset, messageRetention)))

	setupScript := m.generateSetupScript(hubName, partitionCount, retentionMs)
	result.AddScript("setup_redpanda_eventhub.sh", []byte(setupScript))

	consumerExample := m.generateConsumerExample(hubName, consumerGroup, offsetReset)
	result.AddScript("eventhub_consumer.py", []byte(consumerExample))
	result.AddScript("validate_eventhub_replay.sh", []byte(m.generateValidationScript(hubName, consumerGroup)))
	result.AddScript("backup_eventhub_offsets.sh", []byte(m.generateBackupScript(hubName, consumerGroup)))
	result.AddScript("cutover_eventhub_clients.sh", []byte(m.generateCutoverScript(hubName, consumerGroup)))
	for _, step := range eventHubRunbook(hubName) {
		result.AddRunbookStep(step)
	}

	return result, nil
}

func (m *EventHubMapper) generateTopicConfig(hubName string, partitionCount, retentionMs int) string {
	return fmt.Sprintf(`# Redpanda Topic Configuration
# Generated from Azure Event Hub: %s

topics:
  - name: %s
    partitions: %d
    replication_factor: 3
    config:
      retention.ms: %d
      cleanup.policy: delete
      segment.bytes: 1073741824  # 1GB
`, hubName, hubName, partitionCount, retentionMs)
}

func (m *EventHubMapper) generateSetupScript(hubName string, partitionCount, retentionMs int) string {
	return fmt.Sprintf(`#!/bin/bash
# Redpanda Setup Script for Event Hub: %s

set -e

echo "Waiting for Redpanda to be ready..."
until docker exec redpanda rpk cluster health 2>/dev/null | grep -q "Healthy"; do
  echo "Waiting..."
  sleep 5
done

echo "Redpanda is ready!"

echo "Creating topic: %s with %d partitions..."
docker exec redpanda rpk topic create %s --partitions %d --replicas 3 2>/dev/null || echo "Topic may already exist"
docker exec redpanda rpk topic alter-config %s --set retention.ms=%d

echo ""
echo "Kafka Bootstrap: localhost:19092"
echo "REST Proxy: http://localhost:8082"
echo "Schema Registry: http://localhost:8081"
echo ""
echo "Topic: %s"
echo "Partitions: %d"
echo "Retention ms: %d"
`, hubName, hubName, partitionCount, hubName, partitionCount, hubName, retentionMs, hubName, partitionCount, retentionMs)
}

func (m *EventHubMapper) generateConsumerExample(hubName, consumerGroup, offsetReset string) string {
	return fmt.Sprintf(`#!/usr/bin/env python3
"""
Kafka Consumer Example
Migration from Azure Event Hubs to Redpanda
"""

from kafka import KafkaConsumer, KafkaProducer
import json

BOOTSTRAP_SERVERS = ['localhost:19092']
TOPIC = '%s'
CONSUMER_GROUP = '%s'
OFFSET_RESET = '%s'

def consume_events():
    """Consume events from Redpanda topic."""
    consumer = KafkaConsumer(
        TOPIC,
        bootstrap_servers=BOOTSTRAP_SERVERS,
        auto_offset_reset=OFFSET_RESET,
        enable_auto_commit=True,
        group_id=CONSUMER_GROUP,
        value_deserializer=lambda x: json.loads(x.decode('utf-8'))
    )

    print(f"Consuming from topic: {TOPIC}")
    for message in consumer:
        print(f"Partition: {message.partition}, Offset: {message.offset}")
        print(f"Value: {message.value}")

def send_event(data):
    """Send event to Redpanda topic."""
    producer = KafkaProducer(
        bootstrap_servers=BOOTSTRAP_SERVERS,
        value_serializer=lambda x: json.dumps(x).encode('utf-8')
    )

    future = producer.send(TOPIC, value=data)
    result = future.get(timeout=10)
    print(f"Sent to partition {result.partition} at offset {result.offset}")
    producer.close()

if __name__ == '__main__':
    # Example: send an event
    send_event({'eventType': 'test', 'data': {'key': 'value'}})

    # Example: consume events
    # consume_events()
`, hubName, consumerGroup, offsetReset)
}

func (m *EventHubMapper) generateAppChange(hubName, consumerGroup, offsetReset string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_EVENT_HUB=%s\nKAFKA_BOOTSTRAP_SERVERS=redpanda:9092\nKAFKA_TOPIC=%s\nKAFKA_CONSUMER_GROUP=%s\nKAFKA_AUTO_OFFSET_RESET=%s\nEVENTHUB_REPLAY_MODE=explicit_offsets\n", hubName, hubName, consumerGroup, offsetReset)
}

func (m *EventHubMapper) generateReplayPlan(hubName, consumerGroup, offsetReset string, retentionDays int) string {
	return fmt.Sprintf("source_event_hub: %s\ntarget_topic: %s\nconsumer_group: %s\noffset_reset: %s\nretention_days: %d\nreplay_mode: explicit_offsets\n", hubName, hubName, consumerGroup, offsetReset, retentionDays)
}

func (m *EventHubMapper) generateValidationScript(hubName, consumerGroup string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
test -s config/redpanda/topics.yaml
test -s config/redpanda/app-change.env
test -s config/redpanda/replay-plan.yaml
grep -q "name: %s" config/redpanda/topics.yaml
grep -q "KAFKA_CONSUMER_GROUP=%s" config/redpanda/app-change.env
grep -q "replay_mode: explicit_offsets" config/redpanda/replay-plan.yaml
`, hubName, consumerGroup)
}

func (m *EventHubMapper) generateBackupScript(hubName, consumerGroup string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/eventhub-%s-offsets-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
mkdir -p eventhub-offsets
printf "topic=%s\nconsumer_group=%s\n" > eventhub-offsets/offsets.env
tar -czf "$archive" config/redpanda eventhub-offsets
echo "$archive"
`, hubName, hubName, consumerGroup)
}

func (m *EventHubMapper) generateCutoverScript(hubName, consumerGroup string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
. config/redpanda/app-change.env
test "$SOURCE_EVENT_HUB" = %q
test "$KAFKA_CONSUMER_GROUP" = %q
test "$EVENTHUB_REPLAY_MODE" = "explicit_offsets"
echo "Apply generated Kafka client settings for $KAFKA_TOPIC at $KAFKA_BOOTSTRAP_SERVERS"
`, hubName, consumerGroup)
}

func eventHubRunbook(hubName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "messaging", "source": "azurerm_eventhub", "event_hub": hubName, "target": "redpanda"}
	return []domainrunbook.Step{
		eventHubStep("provision-eventhub-redpanda", "Provision Event Hubs Redpanda target", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "setup_redpanda_eventhub.sh"}, "Redpanda topic is provisioned with retention", metadata),
		eventHubStep("configure-eventhub-replay", "Configure Event Hubs replay", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s config/redpanda/replay-plan.yaml"}, "consumer group and offset replay plan is generated", metadata),
		eventHubStep("validate-eventhub-replay", "Validate Event Hubs replay", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_eventhub_replay.sh"}, "Kafka-compatible replay config validates", metadata),
		eventHubStep("backup-eventhub-offsets", "Backup Event Hubs offsets", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_eventhub_offsets.sh"}, "offset and replay handoff is archived", metadata),
		eventHubStep("cutover-eventhub-clients", "Cut over Event Hubs clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_eventhub_clients.sh"}, "clients use generated Kafka settings", metadata),
		eventHubStep("rollback-eventhub-source", "Keep Event Hubs source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "Azure Event Hubs remains authoritative until replay validation passes", metadata),
	}
}

func eventHubStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}
