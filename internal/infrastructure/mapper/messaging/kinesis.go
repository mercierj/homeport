// Package messaging provides mappers for AWS messaging services.
package messaging

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
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
	svc.Networks = []string{"homeport"}
	svc.Labels = map[string]string{
		"homeport.source":                    "aws_kinesis_stream",
		"homeport.stream_name":               streamName,
		"homeport.shard_count":               fmt.Sprintf("%d", shardCount),
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
	svc.Deploy = &mapper.DeployConfig{Replicas: 3}
	svc.Restart = "unless-stopped"

	retentionHours := res.GetConfigInt("retention_period")
	if retentionHours == 0 {
		retentionHours = 24
	}

	// Create topic configuration
	topicConfig := m.generateTopicConfig(streamName, shardCount, retentionHours)
	result.AddConfig("config/redpanda/topics.yaml", []byte(topicConfig))
	result.AddConfig("config/kinesis/stream-map.yaml", []byte(m.generateStreamMap(streamName, shardCount, retentionHours)))
	result.AddConfig("config/kinesis/app-change.env", []byte(m.generateAppChangeConfig(streamName)))
	result.AddConfig("config/kinesis/consumer-groups.yaml", []byte(m.generateConsumerGroups(streamName)))

	// Handle retention
	result.AddWarning(fmt.Sprintf("Kinesis retention: %d hours. Configure topic retention.ms in Redpanda.", retentionHours))

	// Handle encryption
	if res.GetConfigString("encryption_type") != "" {
		result.AddWarning("Kinesis encryption enabled. Generated Redpanda target should run behind TLS termination for client traffic.")
	}

	// Handle enhanced monitoring
	if res.GetConfigString("shard_level_metrics") != "" {
		result.AddWarning("Enhanced monitoring enabled. Redpanda metrics are exposed on :9644/metrics for Prometheus.")
	}

	setupScript := m.generateSetupScript(streamName, shardCount)
	result.AddScript("setup_redpanda.sh", []byte(setupScript))
	result.AddScript("export_kinesis_records.sh", []byte(m.generateExportScript(streamName, shardCount, res.Region)))
	result.AddScript("migrate_kinesis_records.sh", []byte(m.generateMigrateScript(streamName)))
	result.AddScript("validate_kinesis_replay.sh", []byte(m.generateValidateScript(streamName)))
	result.AddScript("backup_kinesis_stream.sh", []byte(m.generateBackupScript(streamName)))
	result.AddScript("cutover_kinesis_adapter.sh", []byte(m.generateCutoverScript(streamName)))

	consumerExample := m.generateConsumerExample(streamName)
	result.AddScript("consumer_example.py", []byte(consumerExample))

	for _, step := range kinesisRunbook(streamName) {
		result.AddRunbookStep(step)
	}

	return result, nil
}

func (m *KinesisMapper) generateTopicConfig(streamName string, shardCount int, retentionHours int) string {
	retentionMillis := retentionHours * 60 * 60 * 1000
	return fmt.Sprintf(`# Redpanda Topic Configuration
# Generated from Kinesis stream: %s

topics:
  - name: %s
    partitions: %d
    replication_factor: 3
    config:
      retention.ms: %d
      cleanup.policy: delete
      segment.bytes: 1073741824  # 1GB
`, streamName, streamName, shardCount, retentionMillis)
}

func (m *KinesisMapper) generateStreamMap(streamName string, shardCount int, retentionHours int) string {
	return fmt.Sprintf(`stream: %s
target_topic: %s
shards: %d
retention_hours: %d
adapter: homeport-kinesis
`, streamName, streamName, shardCount, retentionHours)
}

func (m *KinesisMapper) generateAppChangeConfig(streamName string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=adapter
SOURCE_STREAM=%s
TARGET_TOPIC=%s
AWS_ENDPOINT_URL_KINESIS=http://homeport:8080/api/v1/compat/aws/kinesis
KINESIS_COMPAT_BACKEND=redpanda
KAFKA_BOOTSTRAP_SERVERS=redpanda:9092
`, streamName, streamName)
}

func (m *KinesisMapper) generateConsumerGroups(streamName string) string {
	return fmt.Sprintf(`stream: %s
groups:
  - name: homeport-replay-validator
    start_position: TRIM_HORIZON
    target_topic: %s
`, streamName, streamName)
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

func (m *KinesisMapper) generateExportScript(streamName string, shardCount int, region string) string {
	if region == "" {
		region = "us-east-1"
	}
	return fmt.Sprintf(`#!/bin/sh
set -eu
AWS_REGION="${AWS_REGION:-%s}"
STREAM_NAME="${KINESIS_STREAM:-%s}"
OUTPUT_DIR="${KINESIS_EXPORT_DIR:-kinesis-export}"
mkdir -p "$OUTPUT_DIR"

i=0
while [ "$i" -lt %d ]; do
  shard_id=$(printf 'shardId-%%012d' "$i")
  iterator=$(aws kinesis get-shard-iterator \
    --region "$AWS_REGION" \
    --stream-name "$STREAM_NAME" \
    --shard-id "$shard_id" \
    --shard-iterator-type TRIM_HORIZON \
    --query ShardIterator \
    --output text)
  aws kinesis get-records \
    --region "$AWS_REGION" \
    --shard-iterator "$iterator" \
    --output json > "$OUTPUT_DIR/$shard_id.json"
  i=$((i + 1))
done

echo "Exported %d shard snapshots for $STREAM_NAME into $OUTPUT_DIR"
`, region, streamName, shardCount, shardCount)
}

func (m *KinesisMapper) generateMigrateScript(streamName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
STREAM_NAME="${KINESIS_STREAM:-%s}"
TOPIC="${REDPANDA_TOPIC:-%s}"
EXPORT_DIR="${KINESIS_EXPORT_DIR:-kinesis-export}"

test -d "$EXPORT_DIR"
sh setup_redpanda.sh

for file in "$EXPORT_DIR"/*.json; do
  [ -e "$file" ] || continue
  jq -c '.Records[]?' "$file" | while read -r record; do
    partition_key=$(printf '%%s' "$record" | jq -r '.PartitionKey // "homeport"')
    data=$(printf '%%s' "$record" | jq -r '.Data')
    printf '%%s\n' "$data" | base64 -d | rpk topic produce "$TOPIC" -k "$partition_key" >/dev/null
  done
done

echo "Migrated exported Kinesis records from $STREAM_NAME into Redpanda topic $TOPIC"
`, streamName, streamName)
}

func (m *KinesisMapper) generateValidateScript(streamName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
TOPIC="${REDPANDA_TOPIC:-%s}"
test -s config/kinesis/stream-map.yaml
test -s config/kinesis/consumer-groups.yaml
rpk topic describe "$TOPIC" >/dev/null
rpk topic consume "$TOPIC" --num 1 --offset start >/tmp/homeport-kinesis-replay.txt 2>/dev/null || true
test -s /tmp/homeport-kinesis-replay.txt || test ! -d "${KINESIS_EXPORT_DIR:-kinesis-export}"
echo "Validated Kinesis replay path for $TOPIC"
`, streamName)
}

func (m *KinesisMapper) generateBackupScript(streamName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-kinesis-redpanda-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/kinesis config/redpanda setup_redpanda.sh export_kinesis_records.sh migrate_kinesis_records.sh validate_kinesis_replay.sh
echo "$archive"
`, streamName)
}

func (m *KinesisMapper) generateCutoverScript(streamName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
test -s config/kinesis/app-change.env
. config/kinesis/app-change.env
test "$SOURCE_STREAM" = %q
test "$TARGET_TOPIC" = %q
test "$APP_CHANGE_MODE" = "adapter"
echo "Use AWS_ENDPOINT_URL_KINESIS=$AWS_ENDPOINT_URL_KINESIS for Kinesis SDK clients backed by Redpanda."
`, streamName, streamName)
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

func kinesisRunbook(streamName string) []domainrunbook.Step {
	metadata := map[string]string{
		"kind":                       "stream",
		"source":                     "aws_kinesis_stream",
		"stream":                     streamName,
		"AWS_ENDPOINT_URL_KINESIS":   "http://homeport:8080/api/v1/compat/aws/kinesis",
		"HOMEPORT_COMPAT_BACKEND":    "redpanda",
		"HOMEPORT_COMPAT_PROTOCOL":   "kinesis",
		"KAFKA_BOOTSTRAP_SERVERS":    "redpanda:9092",
		"REDPANDA_REPLAY_VALIDATION": "consumer-group-offsets",
	}
	return []domainrunbook.Step{
		kinesisStep("export-kinesis-stream", "Export Kinesis stream records", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_kinesis_records.sh"}, "Kinesis shard snapshots are exported", metadata),
		kinesisStep("provision-redpanda-topic", "Provision Redpanda topic", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "setup_redpanda.sh"}, "Redpanda topic exists with mapped partitions and retention", metadata),
		kinesisStep("migrate-kinesis-records", "Migrate Kinesis records", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_kinesis_records.sh"}, "exported records are produced to Redpanda", metadata),
		kinesisStep("validate-kinesis-replay", "Validate Kinesis replay", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_kinesis_replay.sh"}, "consumer group can replay the migrated topic from the beginning", metadata),
		kinesisStep("backup-kinesis-config", "Backup Kinesis migration config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_kinesis_stream.sh"}, "Kinesis and Redpanda migration artifacts are archived", metadata),
		kinesisStep("cutover-kinesis-adapter", "Cut over Kinesis clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_kinesis_adapter.sh"}, "Kinesis SDK clients use the HomePort compatibility endpoint", metadata),
		kinesisStep("rollback-kinesis-source", "Keep Kinesis source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "AWS Kinesis remains authoritative until replay validation passes", metadata),
	}
}

func kinesisStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{
		ID:               id,
		Name:             name,
		Group:            group,
		Type:             stepType,
		Status:           domainrunbook.StepStatusPending,
		Executor:         executor,
		Command:          command,
		SuccessCondition: success,
		Metadata:         metadata,
	}
}
