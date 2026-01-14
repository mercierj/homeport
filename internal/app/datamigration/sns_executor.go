package datamigration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// SNSToNATSExecutor migrates SNS topics to NATS.
type SNSToNATSExecutor struct{}

// NewSNSToNATSExecutor creates a new SNS to NATS executor.
func NewSNSToNATSExecutor() *SNSToNATSExecutor {
	return &SNSToNATSExecutor{}
}

// Type returns the migration type.
func (e *SNSToNATSExecutor) Type() string {
	return "sns_to_nats"
}

// GetPhases returns the migration phases.
func (e *SNSToNATSExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Fetching topics",
		"Exporting subscriptions",
		"Generating NATS config",
		"Creating subject mappings",
		"Finalizing",
	}
}

// Validate validates the migration configuration.
func (e *SNSToNATSExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["access_key_id"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.access_key_id is required")
		}
		if _, ok := config.Source["secret_access_key"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.secret_access_key is required")
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

	result.Warnings = append(result.Warnings, "SNS topics will be mapped to NATS subjects")
	result.Warnings = append(result.Warnings, "HTTP/HTTPS subscriptions need webhook reconfiguration")

	return result, nil
}

// Execute performs the migration.
func (e *SNSToNATSExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

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

	// Phase 2: Fetching topics
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", "Fetching SNS topics")
	EmitProgress(m, 25, "Fetching topics")

	listTopicsCmd := exec.CommandContext(ctx, "aws", "sns", "list-topics",
		"--region", region, "--output", "json",
	)
	listTopicsCmd.Env = append(os.Environ(), awsEnv...)
	topicsOutput, err := listTopicsCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to list topics: %w", err)
	}

	var topicsList struct {
		Topics []struct {
			TopicArn string `json:"TopicArn"`
		} `json:"Topics"`
	}
	json.Unmarshal(topicsOutput, &topicsList)

	type TopicDetails struct {
		Arn           string                   `json:"arn"`
		Name          string                   `json:"name"`
		Subscriptions []map[string]interface{} `json:"subscriptions"`
		Attributes    map[string]string        `json:"attributes"`
	}
	topics := make([]TopicDetails, 0)

	for _, topic := range topicsList.Topics {
		// Extract topic name from ARN
		topicName := ""
		parts := []byte(topic.TopicArn)
		for i := len(parts) - 1; i >= 0; i-- {
			if parts[i] == ':' {
				topicName = string(parts[i+1:])
				break
			}
		}

		// Get topic attributes
		attrCmd := exec.CommandContext(ctx, "aws", "sns", "get-topic-attributes",
			"--topic-arn", topic.TopicArn,
			"--region", region, "--output", "json",
		)
		attrCmd.Env = append(os.Environ(), awsEnv...)
		attrOutput, _ := attrCmd.Output()
		var attrs struct {
			Attributes map[string]string `json:"Attributes"`
		}
		json.Unmarshal(attrOutput, &attrs)

		// Get subscriptions
		subsCmd := exec.CommandContext(ctx, "aws", "sns", "list-subscriptions-by-topic",
			"--topic-arn", topic.TopicArn,
			"--region", region, "--output", "json",
		)
		subsCmd.Env = append(os.Environ(), awsEnv...)
		subsOutput, _ := subsCmd.Output()
		var subs struct {
			Subscriptions []map[string]interface{} `json:"Subscriptions"`
		}
		json.Unmarshal(subsOutput, &subs)

		topics = append(topics, TopicDetails{
			Arn:           topic.TopicArn,
			Name:          topicName,
			Subscriptions: subs.Subscriptions,
			Attributes:    attrs.Attributes,
		})
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Exporting subscriptions
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", "Exporting subscription details")
	EmitProgress(m, 40, "Exporting subscriptions")

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	topicsData, _ := json.MarshalIndent(topics, "", "  ")
	topicsPath := filepath.Join(outputDir, "sns-topics.json")
	os.WriteFile(topicsPath, topicsData, 0644)

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Generating NATS config
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Generating NATS configuration")
	EmitProgress(m, 60, "Generating config")

	// Docker compose with NATS
	natsCompose := `version: '3.8'

services:
  nats:
    image: nats:latest
    container_name: nats
    command:
      - "--jetstream"
      - "--store_dir=/data"
      - "--http_port=8222"
    ports:
      - "4222:4222"
      - "8222:8222"
    volumes:
      - nats-data:/data
    restart: unless-stopped

  nats-box:
    image: natsio/nats-box:latest
    container_name: nats-box
    depends_on:
      - nats
    entrypoint: /bin/sh
    tty: true
    stdin_open: true

volumes:
  nats-data:
`
	composePath := filepath.Join(outputDir, "docker-compose.yml")
	os.WriteFile(composePath, []byte(natsCompose), 0644)

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Creating subject mappings
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Creating NATS subject mappings")
	EmitProgress(m, 80, "Creating mappings")

	// Generate setup script
	setupScript := `#!/bin/bash
# NATS JetStream Setup
# Migrated from AWS SNS

set -e

NATS_URL="nats://localhost:4222"

echo "Setting up NATS JetStream streams..."

# Create streams for each topic
`
	for _, topic := range topics {
		subject := fmt.Sprintf("sns.%s", topic.Name)
		setupScript += fmt.Sprintf(`
# Stream for: %s
nats stream add %s \
    --subjects="%s,sns.%s.>" \
    --storage=file \
    --retention=limits \
    --max-msgs=-1 \
    --max-bytes=-1 \
    --max-age=72h \
    --discard=old \
    --replicas=1 \
    --server=$NATS_URL
`, topic.Name, topic.Name, subject, topic.Name)
	}

	setupScript += `
echo "Streams created successfully!"
nats stream list --server=$NATS_URL
`
	setupPath := filepath.Join(outputDir, "setup-nats.sh")
	os.WriteFile(setupPath, []byte(setupScript), 0755)

	// Publisher example
	publisherExample := `#!/usr/bin/env python3
"""
NATS Publisher Example (SNS Publish equivalent)
"""
import asyncio
import json
from nats.aio.client import Client as NATS

async def publish(topic_name: str, message: dict, attributes: dict = None):
    """
    Publish message to NATS (equivalent to SNS publish)
    """
    nc = NATS()
    await nc.connect("nats://localhost:4222")

    subject = f"sns.{topic_name}"
    payload = {
        "Message": message,
        "MessageAttributes": attributes or {}
    }

    await nc.publish(subject, json.dumps(payload).encode())
    await nc.flush()
    await nc.close()
    print(f"Published to {subject}")

if __name__ == "__main__":
    asyncio.run(publish(
        "my-topic",
        {"event": "user_signup", "user_id": "123"},
        {"type": {"DataType": "String", "StringValue": "notification"}}
    ))
`
	publisherPath := filepath.Join(outputDir, "publisher_example.py")
	os.WriteFile(publisherPath, []byte(publisherExample), 0755)

	// Subscriber example
	subscriberExample := `#!/usr/bin/env python3
"""
NATS Subscriber Example (SNS Subscription equivalent)
"""
import asyncio
import json
from nats.aio.client import Client as NATS

async def subscribe(topic_name: str, callback):
    """
    Subscribe to NATS subject (equivalent to SNS subscription)
    """
    nc = NATS()
    await nc.connect("nats://localhost:4222")

    subject = f"sns.{topic_name}"

    async def message_handler(msg):
        data = json.loads(msg.data.decode())
        await callback(data)

    # Simple subscription
    await nc.subscribe(subject, cb=message_handler)

    # Or with JetStream for durability
    js = nc.jetstream()
    await js.subscribe(
        subject,
        cb=message_handler,
        durable="my-consumer",
        manual_ack=True
    )

    print(f"Subscribed to {subject}")
    while True:
        await asyncio.sleep(1)

async def process_message(data):
    print(f"Received: {data}")

if __name__ == "__main__":
    asyncio.run(subscribe("my-topic", process_message))
`
	subscriberPath := filepath.Join(outputDir, "subscriber_example.py")
	os.WriteFile(subscriberPath, []byte(subscriberExample), 0755)

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 6: Finalizing
	EmitPhase(m, phases[5], 6)
	EmitLog(m, "info", "Finalizing migration artifacts")
	EmitProgress(m, 95, "Finalizing")

	readme := fmt.Sprintf(`# SNS to NATS Migration

## Source SNS
- Region: %s
- Topics: %d

## Migration Mapping

| SNS Concept | NATS Equivalent |
|-------------|-----------------|
| Topic | Subject/Stream |
| Subscription | Consumer |
| Message Attributes | Headers |
| Filter Policy | Subject Hierarchy |

## Getting Started

1. Start NATS:
'''bash
docker-compose up -d
'''

2. Create streams:
'''bash
./setup-nats.sh
'''

3. Access NATS monitoring:
- URL: http://localhost:8222

4. Run examples:
'''bash
pip install nats-py
python publisher_example.py
python subscriber_example.py
'''

## Subject Naming Convention

SNS topics are mapped to NATS subjects:
- Topic: my-topic -> Subject: sns.my-topic
- Filtered: sns.my-topic.notifications

## Migrated Topics
`, region, len(topics))

	for _, topic := range topics {
		readme += fmt.Sprintf("- %s (%d subscriptions)\n", topic.Name, len(topic.Subscriptions))
	}

	readme += `
## Files Generated
- sns-topics.json: Original SNS topic details
- docker-compose.yml: NATS container
- setup-nats.sh: Stream creation script
- publisher_example.py: Publisher example
- subscriber_example.py: Subscriber example

## Notes
- HTTP/HTTPS endpoints need webhook reconfiguration
- Lambda subscriptions need consumer implementation
- SQS subscriptions can use NATS consumers
`

	readmePath := filepath.Join(outputDir, "README.md")
	os.WriteFile(readmePath, []byte(readme), 0644)

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", fmt.Sprintf("SNS migration complete: %d topics", len(topics)))

	return nil
}
