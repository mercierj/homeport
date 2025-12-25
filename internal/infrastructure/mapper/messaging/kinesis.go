// Package messaging provides mappers for AWS messaging services.
package messaging

import (
	"context"
	"fmt"
	"time"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

// KinesisMapper converts AWS Kinesis streams to Redpanda.
type KinesisMapper struct {
	*mapper.BaseMapper
}

// NewKinesisMapper creates a new Kinesis to Redpanda mapper.
func NewKinesisMapper() *KinesisMapper {
	return &KinesisMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeKinesis, nil),
	}
}

// Map converts a Kinesis stream to a Redpanda service.
func (m *KinesisMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	streamName := res.GetConfigString("name")
	if streamName == "" {
		streamName = res.Name
	}

	result := mapper.NewMappingResult("redpanda")
	svc := result.DockerService

	shardCount := res.GetConfigInt("shard_count")
	if shardCount == 0 {
		shardCount = 1
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
		"9092:9092",   // Kafka API (internal)
		"19092:19092", // Kafka API (external)
		"8081:8081",   // Schema Registry
		"8082:8082",   // Pandaproxy (REST)
		"9644:9644",   // Admin API
	}
	svc.Volumes = []string{
		"./data/redpanda:/var/lib/redpanda/data",
	}
	svc.Networks = []string{"cloudexit"}
	svc.Labels = map[string]string{
		"cloudexit.source":                                           "aws_kinesis_stream",
		"cloudexit.stream_name":                                      streamName,
		"cloudexit.shard_count":                                      fmt.Sprintf("%d", shardCount),
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

	// Create topic configuration
	topicConfig := m.generateTopicConfig(streamName, shardCount)
	result.AddConfig("config/redpanda/topics.yaml", []byte(topicConfig))

	// Handle retention
	retentionHours := res.GetConfigInt("retention_period")
	if retentionHours == 0 {
		retentionHours = 24
	}
	result.AddWarning(fmt.Sprintf("Kinesis retention: %d hours. Configure topic retention.ms in Redpanda.", retentionHours))

	// Handle encryption
	if res.GetConfigString("encryption_type") != "" {
		result.AddWarning("Kinesis encryption enabled. Configure TLS for Redpanda.")
		result.AddManualStep("Enable TLS in Redpanda configuration for encryption in transit")
	}

	// Handle enhanced monitoring
	if res.GetConfigString("shard_level_metrics") != "" {
		result.AddWarning("Enhanced monitoring enabled. Configure Prometheus metrics export.")
		result.AddManualStep("Set up Prometheus to scrape Redpanda metrics at :9644/metrics")
	}

	setupScript := m.generateSetupScript(streamName, shardCount)
	result.AddScript("setup_redpanda.sh", []byte(setupScript))

	consumerExample := m.generateConsumerExample(streamName)
	result.AddScript("consumer_example.py", []byte(consumerExample))

	result.AddManualStep("Access Redpanda Console at http://localhost:8080 (add console container)")
	result.AddManualStep(fmt.Sprintf("Topic '%s' will be created with %d partitions (shards)", streamName, shardCount))
	result.AddManualStep("Update application code to use Kafka client instead of Kinesis SDK")
	result.AddManualStep("Use rpk CLI for topic management: rpk topic list")

	return result, nil
}

func (m *KinesisMapper) generateTopicConfig(streamName string, shardCount int) string {
	return fmt.Sprintf(`# Redpanda Topic Configuration
# Generated from Kinesis stream: %s

topics:
  - name: %s
    partitions: %d
    replication_factor: 1
    config:
      retention.ms: 86400000  # 24 hours
      cleanup.policy: delete
      segment.bytes: 1073741824  # 1GB
`, streamName, streamName, shardCount)
}

func (m *KinesisMapper) generateSetupScript(streamName string, shardCount int) string {
	return fmt.Sprintf(`#!/bin/bash
# Redpanda Setup Script for Kinesis stream: %s

set -e

REDPANDA_HOST="${REDPANDA_HOST:-localhost}"
REDPANDA_PORT="${REDPANDA_PORT:-9092}"

echo "Waiting for Redpanda to be ready..."
until docker exec redpanda rpk cluster health 2>/dev/null | grep -q "Healthy"; do
  echo "Waiting..."
  sleep 5
done

echo "Redpanda is ready!"

# Create topic
echo "Creating topic: %s with %d partitions..."
docker exec redpanda rpk topic create %s --partitions %d --replicas 1 2>/dev/null || echo "Topic may already exist"

echo ""
echo "Redpanda Bootstrap: $REDPANDA_HOST:$REDPANDA_PORT"
echo "REST Proxy: http://$REDPANDA_HOST:8082"
echo "Schema Registry: http://$REDPANDA_HOST:8081"
echo "Admin API: http://$REDPANDA_HOST:9644"
echo ""
echo "Topic: %s"
echo "Partitions: %d"
`, streamName, streamName, shardCount, streamName, shardCount, streamName, shardCount)
}

func (m *KinesisMapper) generateConsumerExample(streamName string) string {
	return fmt.Sprintf(`#!/usr/bin/env python3
"""
Kafka Consumer Example
Migration from AWS Kinesis to Redpanda
"""

from kafka import KafkaConsumer, KafkaProducer
import json

BOOTSTRAP_SERVERS = ['localhost:19092']
TOPIC = '%s'

def consume_messages():
    """Consume messages from Redpanda topic."""
    consumer = KafkaConsumer(
        TOPIC,
        bootstrap_servers=BOOTSTRAP_SERVERS,
        auto_offset_reset='earliest',
        enable_auto_commit=True,
        group_id='my-consumer-group',
        value_deserializer=lambda x: json.loads(x.decode('utf-8'))
    )

    print(f"Consuming from topic: {TOPIC}")
    for message in consumer:
        print(f"Partition: {message.partition}, Offset: {message.offset}")
        print(f"Value: {message.value}")

def produce_message(data):
    """Produce message to Redpanda topic."""
    producer = KafkaProducer(
        bootstrap_servers=BOOTSTRAP_SERVERS,
        value_serializer=lambda x: json.dumps(x).encode('utf-8')
    )

    future = producer.send(TOPIC, value=data)
    result = future.get(timeout=10)
    print(f"Sent to partition {result.partition} at offset {result.offset}")
    producer.close()

if __name__ == '__main__':
    # Example: produce a message
    produce_message({'key': 'value', 'timestamp': '2024-01-01T00:00:00Z'})

    # Example: consume messages
    # consume_messages()
`, streamName)
}
