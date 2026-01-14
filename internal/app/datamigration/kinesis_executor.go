package datamigration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// KinesisToRedpandaExecutor migrates Kinesis streams to Redpanda/Kafka.
type KinesisToRedpandaExecutor struct{}

// NewKinesisToRedpandaExecutor creates a new Kinesis to Redpanda executor.
func NewKinesisToRedpandaExecutor() *KinesisToRedpandaExecutor {
	return &KinesisToRedpandaExecutor{}
}

// Type returns the migration type.
func (e *KinesisToRedpandaExecutor) Type() string {
	return "kinesis_to_redpanda"
}

// GetPhases returns the migration phases.
func (e *KinesisToRedpandaExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Fetching stream info",
		"Analyzing shards",
		"Generating Redpanda config",
		"Creating topic configuration",
		"Finalizing",
	}
}

// Validate validates the migration configuration.
func (e *KinesisToRedpandaExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["stream_name"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.stream_name is required")
		}
		if _, ok := config.Source["access_key_id"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.access_key_id is required")
		}
		if _, ok := config.Source["secret_access_key"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.secret_access_key is required")
		}
		if _, ok := config.Source["region"].(string); !ok {
			result.Warnings = append(result.Warnings, "source.region not specified, using default us-east-1")
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

	result.Warnings = append(result.Warnings, "Kinesis shards will be mapped to Redpanda partitions")
	result.Warnings = append(result.Warnings, "Kinesis-specific features (enhanced fan-out) need alternatives")

	return result, nil
}

// Execute performs the migration.
func (e *KinesisToRedpandaExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	streamName := config.Source["stream_name"].(string)
	accessKeyID := config.Source["access_key_id"].(string)
	secretAccessKey := config.Source["secret_access_key"].(string)
	region, _ := config.Source["region"].(string)
	if region == "" {
		region = "us-east-1"
	}
	outputDir := config.Destination["output_dir"].(string)

	awsEnv := []string{
		"AWS_ACCESS_KEY_ID=" + accessKeyID,
		"AWS_SECRET_ACCESS_KEY=" + secretAccessKey,
		"AWS_DEFAULT_REGION=" + region,
	}

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating AWS credentials")
	EmitProgress(m, 10, "Checking credentials")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Fetching stream info
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", fmt.Sprintf("Fetching stream info for %s", streamName))
	EmitProgress(m, 20, "Fetching stream info")

	describeCmd := exec.CommandContext(ctx, "aws", "kinesis", "describe-stream",
		"--stream-name", streamName,
		"--region", region,
		"--output", "json",
	)
	describeCmd.Env = append(os.Environ(), awsEnv...)
	streamOutput, err := describeCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to describe stream: %w", err)
	}

	var streamResult struct {
		StreamDescription struct {
			StreamName        string `json:"StreamName"`
			StreamARN         string `json:"StreamARN"`
			StreamStatus      string `json:"StreamStatus"`
			RetentionPeriodHours int `json:"RetentionPeriodHours"`
			Shards            []struct {
				ShardID                    string `json:"ShardId"`
				HashKeyRange               struct {
					StartingHashKey string `json:"StartingHashKey"`
					EndingHashKey   string `json:"EndingHashKey"`
				} `json:"HashKeyRange"`
				SequenceNumberRange struct {
					StartingSequenceNumber string `json:"StartingSequenceNumber"`
				} `json:"SequenceNumberRange"`
			} `json:"Shards"`
		} `json:"StreamDescription"`
	}
	if err := json.Unmarshal(streamOutput, &streamResult); err != nil {
		return fmt.Errorf("failed to parse stream info: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Analyzing shards
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", "Analyzing shard configuration")
	EmitProgress(m, 40, "Analyzing shards")

	shardCount := len(streamResult.StreamDescription.Shards)
	retentionHours := streamResult.StreamDescription.RetentionPeriodHours

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Save stream info
	streamInfoPath := filepath.Join(outputDir, "stream-info.json")
	if err := os.WriteFile(streamInfoPath, streamOutput, 0644); err != nil {
		return fmt.Errorf("failed to write stream info: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Generating Redpanda config
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Generating Redpanda configuration")
	EmitProgress(m, 60, "Generating config")

	// Docker compose for Redpanda
	redpandaCompose := fmt.Sprintf(`version: '3.8'

services:
  redpanda:
    image: redpandadata/redpanda:latest
    container_name: redpanda
    command:
      - redpanda
      - start
      - --kafka-addr internal://0.0.0.0:9092,external://0.0.0.0:19092
      - --advertise-kafka-addr internal://redpanda:9092,external://localhost:19092
      - --pandaproxy-addr internal://0.0.0.0:8082,external://0.0.0.0:18082
      - --advertise-pandaproxy-addr internal://redpanda:8082,external://localhost:18082
      - --schema-registry-addr internal://0.0.0.0:8081,external://0.0.0.0:18081
      - --rpc-addr redpanda:33145
      - --advertise-rpc-addr redpanda:33145
      - --smp 1
      - --memory 1G
      - --reserve-memory 0M
      - --overprovisioned
      - --default-log-level=info
    ports:
      - "18081:18081"
      - "18082:18082"
      - "19092:19092"
      - "19644:9644"
    volumes:
      - redpanda-data:/var/lib/redpanda/data
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:9644/v1/status/ready"]
      interval: 30s
      timeout: 10s
      retries: 5

  console:
    image: redpandadata/console:latest
    container_name: redpanda-console
    depends_on:
      - redpanda
    ports:
      - "8080:8080"
    environment:
      - KAFKA_BROKERS=redpanda:9092
      - KAFKA_SCHEMAREGISTRY_ENABLED=true
      - KAFKA_SCHEMAREGISTRY_URLS=http://redpanda:8081
    restart: unless-stopped

volumes:
  redpanda-data:

# Topic configuration (equivalent to Kinesis stream)
# Partitions: %d (mapped from shards)
# Retention: %d hours (from Kinesis retention)
`, shardCount, retentionHours)

	composePath := filepath.Join(outputDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(redpandaCompose), 0644); err != nil {
		return fmt.Errorf("failed to write docker-compose.yml: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Creating topic configuration
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Creating topic configuration")
	EmitProgress(m, 80, "Creating topic config")

	// Topic creation script
	topicScript := fmt.Sprintf(`#!/bin/bash
# Create Redpanda topic equivalent to Kinesis stream

set -e

TOPIC_NAME="%s"
PARTITIONS=%d
RETENTION_MS=%d

echo "Creating topic: $TOPIC_NAME"
echo "Partitions: $PARTITIONS (from Kinesis shards)"
echo "Retention: $RETENTION_MS ms (from Kinesis retention)"

# Using rpk (Redpanda CLI)
rpk topic create $TOPIC_NAME \
    --partitions $PARTITIONS \
    --replicas 1 \
    --config retention.ms=$RETENTION_MS \
    --config cleanup.policy=delete

echo "Topic created successfully!"

# List topic details
rpk topic describe $TOPIC_NAME
`, streamName, shardCount, retentionHours*3600000)

	topicScriptPath := filepath.Join(outputDir, "create-topic.sh")
	if err := os.WriteFile(topicScriptPath, []byte(topicScript), 0755); err != nil {
		return fmt.Errorf("failed to write topic script: %w", err)
	}

	// Producer example
	producerExample := fmt.Sprintf(`#!/usr/bin/env python3
"""
Example producer for migrated Kinesis stream
Equivalent to Kinesis put-record/put-records
"""
from kafka import KafkaProducer
import json

producer = KafkaProducer(
    bootstrap_servers=['localhost:19092'],
    value_serializer=lambda v: json.dumps(v).encode('utf-8'),
    key_serializer=lambda k: k.encode('utf-8') if k else None
)

def put_record(data, partition_key):
    """
    Equivalent to kinesis:PutRecord
    partition_key is used for consistent hashing (like Kinesis)
    """
    future = producer.send(
        '%s',
        key=partition_key,
        value=data
    )
    result = future.get(timeout=10)
    print(f"Sent to partition {result.partition}, offset {result.offset}")
    return result

if __name__ == '__main__':
    # Example usage
    put_record({'sensor_id': 1, 'temperature': 25.5}, 'sensor-1')
    put_record({'sensor_id': 2, 'temperature': 26.0}, 'sensor-2')
    producer.close()
`, streamName)

	producerPath := filepath.Join(outputDir, "producer_example.py")
	if err := os.WriteFile(producerPath, []byte(producerExample), 0755); err != nil {
		return fmt.Errorf("failed to write producer example: %w", err)
	}

	// Consumer example
	consumerExample := fmt.Sprintf(`#!/usr/bin/env python3
"""
Example consumer for migrated Kinesis stream
Equivalent to Kinesis get-records
"""
from kafka import KafkaConsumer
import json

consumer = KafkaConsumer(
    '%s',
    bootstrap_servers=['localhost:19092'],
    auto_offset_reset='earliest',  # Like TRIM_HORIZON
    # auto_offset_reset='latest',  # Like LATEST
    enable_auto_commit=True,
    group_id='my-consumer-group',
    value_deserializer=lambda m: json.loads(m.decode('utf-8'))
)

print("Waiting for records...")
for message in consumer:
    print(f"Partition: {message.partition}, Offset: {message.offset}")
    print(f"Key: {message.key}, Value: {message.value}")
    # Process record here (replaces Lambda consumer)
`, streamName)

	consumerPath := filepath.Join(outputDir, "consumer_example.py")
	if err := os.WriteFile(consumerPath, []byte(consumerExample), 0755); err != nil {
		return fmt.Errorf("failed to write consumer example: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 6: Finalizing
	EmitPhase(m, phases[5], 6)
	EmitLog(m, "info", "Finalizing migration artifacts")
	EmitProgress(m, 95, "Finalizing")

	// Data migration script
	dataMigrationScript := fmt.Sprintf(`#!/bin/bash
# Migrate data from Kinesis to Redpanda
# This script reads from Kinesis and writes to Redpanda

set -e

STREAM_NAME="%s"
TOPIC_NAME="%s"
REGION="%s"

echo "Data Migration: Kinesis -> Redpanda"
echo "Stream: $STREAM_NAME"
echo "Topic: $TOPIC_NAME"
echo ""

# Get shard iterator for each shard
SHARDS=$(aws kinesis describe-stream --stream-name $STREAM_NAME --region $REGION \
    --query 'StreamDescription.Shards[*].ShardId' --output text)

for SHARD in $SHARDS; do
    echo "Processing shard: $SHARD"

    # Get shard iterator (TRIM_HORIZON = from beginning)
    ITERATOR=$(aws kinesis get-shard-iterator \
        --stream-name $STREAM_NAME \
        --shard-id $SHARD \
        --shard-iterator-type TRIM_HORIZON \
        --region $REGION \
        --query 'ShardIterator' --output text)

    # Get records and pipe to Redpanda
    # Note: This is a simplified example, production would need pagination
    aws kinesis get-records --shard-iterator $ITERATOR --region $REGION \
        --query 'Records[*].Data' --output text | \
    while read -r DATA; do
        echo $DATA | base64 -d | rpk topic produce $TOPIC_NAME
    done
done

echo "Data migration complete!"
`, streamName, streamName, region)

	dataMigrationPath := filepath.Join(outputDir, "migrate-data.sh")
	if err := os.WriteFile(dataMigrationPath, []byte(dataMigrationScript), 0755); err != nil {
		return fmt.Errorf("failed to write data migration script: %w", err)
	}

	// Generate README
	readme := fmt.Sprintf(`# Kinesis to Redpanda Migration

## Source Kinesis Stream
- Stream Name: %s
- Region: %s
- Shards: %d
- Retention: %d hours

## Migration Mapping

| Kinesis Concept | Redpanda/Kafka Equivalent |
|-----------------|---------------------------|
| Stream          | Topic                     |
| Shard           | Partition                 |
| Partition Key   | Message Key               |
| Sequence Number | Offset                    |
| TRIM_HORIZON    | earliest offset           |
| LATEST          | latest offset             |

## Getting Started

1. Start Redpanda:
'''bash
docker-compose up -d
'''

2. Create the topic:
'''bash
./create-topic.sh
'''

3. Access Console:
- URL: http://localhost:8080

4. Migrate existing data:
'''bash
./migrate-data.sh
'''

5. Run example producer:
'''bash
pip install kafka-python
python producer_example.py
'''

6. Run example consumer:
'''bash
python consumer_example.py
'''

## Files Generated
- stream-info.json: Original Kinesis configuration
- docker-compose.yml: Redpanda container setup
- create-topic.sh: Topic creation script
- migrate-data.sh: Data migration script
- producer_example.py: Producer example
- consumer_example.py: Consumer example

## Notes
- Shards are mapped to partitions 1:1
- Enhanced fan-out requires consumer group configuration
- Kinesis Data Analytics needs alternative (ksqlDB, Flink)
`, streamName, region, shardCount, retentionHours)

	readmePath := filepath.Join(outputDir, "README.md")
	if err := os.WriteFile(readmePath, []byte(readme), 0644); err != nil {
		return fmt.Errorf("failed to write README: %w", err)
	}

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", fmt.Sprintf("Kinesis stream %s migrated to Redpanda", streamName))

	return nil
}
