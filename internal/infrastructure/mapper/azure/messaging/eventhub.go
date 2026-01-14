// Package messaging provides mappers for Azure messaging services.
package messaging

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
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
		"homeport.source":                                           "azurerm_eventhub",
		"homeport.hub_name":                                         hubName,
		"homeport.partition_count":                                  fmt.Sprintf("%d", partitionCount),
		"traefik.enable":                                             "true",
		"traefik.http.routers.redpanda.rule":                         "Host(`redpanda.localhost`)",
		"traefik.http.services.redpanda.loadbalancer.server.port":    "8082",
	}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "rpk", "cluster", "health"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  5,
	}
	svc.Restart = "unless-stopped"

	// Handle message retention
	messageRetention := res.GetConfigInt("message_retention")
	if messageRetention == 0 {
		messageRetention = 1 // Default 1 day
	}
	result.AddWarning(fmt.Sprintf("Message retention: %d days. Configure topic retention.ms in Redpanda.", messageRetention))

	// Handle capture (archiving)
	if res.Config["capture_description"] != nil {
		result.AddWarning("Event capture enabled. Consider using Redpanda tiered storage for archiving.")
		result.AddManualStep("Configure Redpanda tiered storage for long-term archival")
	}

	topicConfig := m.generateTopicConfig(hubName, partitionCount)
	result.AddConfig("config/redpanda/topics.yaml", []byte(topicConfig))

	setupScript := m.generateSetupScript(hubName, partitionCount)
	result.AddScript("setup_redpanda_eventhub.sh", []byte(setupScript))

	consumerExample := m.generateConsumerExample(hubName)
	result.AddScript("eventhub_consumer.py", []byte(consumerExample))

	result.AddManualStep(fmt.Sprintf("Topic '%s' will be created with %d partitions", hubName, partitionCount))
	result.AddManualStep("Update application code to use Kafka client instead of Event Hubs SDK")
	result.AddManualStep("Use rpk CLI for topic management: rpk topic list")

	return result, nil
}

func (m *EventHubMapper) generateTopicConfig(hubName string, partitionCount int) string {
	return fmt.Sprintf(`# Redpanda Topic Configuration
# Generated from Azure Event Hub: %s

topics:
  - name: %s
    partitions: %d
    replication_factor: 1
    config:
      retention.ms: 86400000  # 24 hours
      cleanup.policy: delete
      segment.bytes: 1073741824  # 1GB
`, hubName, hubName, partitionCount)
}

func (m *EventHubMapper) generateSetupScript(hubName string, partitionCount int) string {
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
docker exec redpanda rpk topic create %s --partitions %d --replicas 1 2>/dev/null || echo "Topic may already exist"

echo ""
echo "Kafka Bootstrap: localhost:19092"
echo "REST Proxy: http://localhost:8082"
echo "Schema Registry: http://localhost:8081"
echo ""
echo "Topic: %s"
echo "Partitions: %d"
`, hubName, hubName, partitionCount, hubName, partitionCount, hubName, partitionCount)
}

func (m *EventHubMapper) generateConsumerExample(hubName string) string {
	return fmt.Sprintf(`#!/usr/bin/env python3
"""
Kafka Consumer Example
Migration from Azure Event Hubs to Redpanda
"""

from kafka import KafkaConsumer, KafkaProducer
import json

BOOTSTRAP_SERVERS = ['localhost:19092']
TOPIC = '%s'

def consume_events():
    """Consume events from Redpanda topic."""
    consumer = KafkaConsumer(
        TOPIC,
        bootstrap_servers=BOOTSTRAP_SERVERS,
        auto_offset_reset='earliest',
        enable_auto_commit=True,
        group_id='eventhub-consumer-group',
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
`, hubName)
}
