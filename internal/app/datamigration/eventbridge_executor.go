package datamigration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// RuleWithTargets represents an EventBridge rule with its targets.
type RuleWithTargets struct {
	Name         string        `json:"name"`
	EventPattern interface{}   `json:"eventPattern"`
	State        string        `json:"state"`
	Description  string        `json:"description"`
	Targets      []interface{} `json:"targets"`
}

// EventBridgeToRabbitMQExecutor migrates EventBridge rules to RabbitMQ.
type EventBridgeToRabbitMQExecutor struct{}

// NewEventBridgeToRabbitMQExecutor creates a new EventBridge to RabbitMQ executor.
func NewEventBridgeToRabbitMQExecutor() *EventBridgeToRabbitMQExecutor {
	return &EventBridgeToRabbitMQExecutor{}
}

// Type returns the migration type.
func (e *EventBridgeToRabbitMQExecutor) Type() string {
	return "eventbridge_to_rabbitmq"
}

// GetPhases returns the migration phases.
func (e *EventBridgeToRabbitMQExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Fetching event buses",
		"Exporting rules",
		"Generating RabbitMQ config",
		"Creating exchange topology",
		"Finalizing",
	}
}

// Validate validates the migration configuration.
func (e *EventBridgeToRabbitMQExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
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

	result.Warnings = append(result.Warnings, "EventBridge patterns will be converted to RabbitMQ topic bindings")
	result.Warnings = append(result.Warnings, "Lambda targets will need to be replaced with consumers")

	return result, nil
}

// Execute performs the migration.
func (e *EventBridgeToRabbitMQExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	accessKeyID := config.Source["access_key_id"].(string)
	secretAccessKey := config.Source["secret_access_key"].(string)
	region, _ := config.Source["region"].(string)
	if region == "" {
		region = "us-east-1"
	}
	eventBusName, _ := config.Source["event_bus_name"].(string)
	if eventBusName == "" {
		eventBusName = "default"
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

	// Phase 2: Fetching event buses
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", "Fetching event buses")
	EmitProgress(m, 20, "Fetching event buses")

	listBusesCmd := exec.CommandContext(ctx, "aws", "events", "list-event-buses",
		"--region", region,
		"--output", "json",
	)
	listBusesCmd.Env = append(os.Environ(), awsEnv...)
	busesOutput, err := listBusesCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to list event buses: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Exporting rules
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", fmt.Sprintf("Exporting rules from event bus: %s", eventBusName))
	EmitProgress(m, 40, "Exporting rules")

	listRulesCmd := exec.CommandContext(ctx, "aws", "events", "list-rules",
		"--event-bus-name", eventBusName,
		"--region", region,
		"--output", "json",
	)
	listRulesCmd.Env = append(os.Environ(), awsEnv...)
	rulesOutput, err := listRulesCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to list rules: %w", err)
	}

	var rulesResult struct {
		Rules []struct {
			Name         string `json:"Name"`
			EventPattern string `json:"EventPattern"`
			State        string `json:"State"`
			Description  string `json:"Description"`
		} `json:"Rules"`
	}
	if err := json.Unmarshal(rulesOutput, &rulesResult); err != nil {
		return fmt.Errorf("failed to parse rules: %w", err)
	}

	// Get targets for each rule
	rulesWithTargets := make([]RuleWithTargets, 0)

	for _, rule := range rulesResult.Rules {
		targetsCmd := exec.CommandContext(ctx, "aws", "events", "list-targets-by-rule",
			"--rule", rule.Name,
			"--event-bus-name", eventBusName,
			"--region", region,
			"--output", "json",
		)
		targetsCmd.Env = append(os.Environ(), awsEnv...)
		targetsOutput, _ := targetsCmd.Output()

		var pattern interface{}
		json.Unmarshal([]byte(rule.EventPattern), &pattern)

		var targets struct {
			Targets []interface{} `json:"Targets"`
		}
		json.Unmarshal(targetsOutput, &targets)

		rulesWithTargets = append(rulesWithTargets, RuleWithTargets{
			Name:         rule.Name,
			EventPattern: pattern,
			State:        rule.State,
			Description:  rule.Description,
			Targets:      targets.Targets,
		})
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Generating RabbitMQ config
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Generating RabbitMQ configuration")
	EmitProgress(m, 60, "Generating config")

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Save raw AWS data
	busesPath := filepath.Join(outputDir, "event-buses.json")
	if err := os.WriteFile(busesPath, busesOutput, 0644); err != nil {
		return fmt.Errorf("failed to write event buses: %w", err)
	}

	rulesData, _ := json.MarshalIndent(rulesWithTargets, "", "  ")
	rulesPath := filepath.Join(outputDir, "rules-with-targets.json")
	if err := os.WriteFile(rulesPath, rulesData, 0644); err != nil {
		return fmt.Errorf("failed to write rules: %w", err)
	}

	// Generate RabbitMQ definitions
	definitions := e.generateRabbitMQDefinitions(eventBusName, rulesWithTargets)
	definitionsPath := filepath.Join(outputDir, "rabbitmq-definitions.json")
	defData, _ := json.MarshalIndent(definitions, "", "  ")
	if err := os.WriteFile(definitionsPath, defData, 0644); err != nil {
		return fmt.Errorf("failed to write RabbitMQ definitions: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Creating exchange topology
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Creating exchange topology configuration")
	EmitProgress(m, 80, "Creating topology")

	// Docker compose for RabbitMQ
	rabbitCompose := `version: '3.8'

services:
  rabbitmq:
    image: rabbitmq:3-management-alpine
    container_name: rabbitmq-eventbridge
    hostname: rabbitmq
    environment:
      RABBITMQ_DEFAULT_USER: admin
      RABBITMQ_DEFAULT_PASS: admin
    ports:
      - "5672:5672"
      - "15672:15672"
    volumes:
      - rabbitmq-data:/var/lib/rabbitmq
      - ./rabbitmq-definitions.json:/etc/rabbitmq/definitions.json
      - ./rabbitmq.conf:/etc/rabbitmq/rabbitmq.conf
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "rabbitmq-diagnostics", "check_running"]
      interval: 30s
      timeout: 10s
      retries: 5

volumes:
  rabbitmq-data:
`
	composePath := filepath.Join(outputDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(rabbitCompose), 0644); err != nil {
		return fmt.Errorf("failed to write docker-compose.yml: %w", err)
	}

	rabbitConf := `management.load_definitions = /etc/rabbitmq/definitions.json
`
	confPath := filepath.Join(outputDir, "rabbitmq.conf")
	if err := os.WriteFile(confPath, []byte(rabbitConf), 0644); err != nil {
		return fmt.Errorf("failed to write rabbitmq.conf: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 6: Finalizing
	EmitPhase(m, phases[5], 6)
	EmitLog(m, "info", "Finalizing migration artifacts")
	EmitProgress(m, 95, "Finalizing")

	// Generate publisher example
	publisherExample := `#!/usr/bin/env python3
"""
Example publisher for migrated EventBridge events
"""
import pika
import json
import sys

connection = pika.BlockingConnection(
    pika.ConnectionParameters('localhost', 5672, '/',
        pika.PlainCredentials('admin', 'admin'))
)
channel = connection.channel()

# Publish event (similar to EventBridge put-events)
def publish_event(source, detail_type, detail):
    event = {
        'source': source,
        'detail-type': detail_type,
        'detail': detail
    }

    # Use topic exchange with routing key based on source and detail-type
    routing_key = f"{source}.{detail_type}".replace('/', '.')

    channel.basic_publish(
        exchange='events',
        routing_key=routing_key,
        body=json.dumps(event),
        properties=pika.BasicProperties(content_type='application/json')
    )
    print(f"Published event: {routing_key}")

if __name__ == '__main__':
    publish_event('myapp', 'order.created', {'orderId': '12345', 'amount': 99.99})
    connection.close()
`
	publisherPath := filepath.Join(outputDir, "publisher_example.py")
	if err := os.WriteFile(publisherPath, []byte(publisherExample), 0755); err != nil {
		return fmt.Errorf("failed to write publisher example: %w", err)
	}

	// Generate consumer example
	consumerExample := `#!/usr/bin/env python3
"""
Example consumer for migrated EventBridge events
"""
import pika
import json

connection = pika.BlockingConnection(
    pika.ConnectionParameters('localhost', 5672, '/',
        pika.PlainCredentials('admin', 'admin'))
)
channel = connection.channel()

def callback(ch, method, properties, body):
    event = json.loads(body)
    print(f"Received event: {event}")
    # Process event here (replaces Lambda target)
    ch.basic_ack(delivery_tag=method.delivery_tag)

# Replace 'rule-queue' with your migrated queue name
channel.basic_consume(queue='rule-queue', on_message_callback=callback)

print('Waiting for events...')
channel.start_consuming()
`
	consumerPath := filepath.Join(outputDir, "consumer_example.py")
	if err := os.WriteFile(consumerPath, []byte(consumerExample), 0755); err != nil {
		return fmt.Errorf("failed to write consumer example: %w", err)
	}

	// Generate README
	readme := fmt.Sprintf(`# EventBridge to RabbitMQ Migration

## Source EventBridge
- Event Bus: %s
- Region: %s
- Rules: %d

## Migration Mapping

| EventBridge Concept | RabbitMQ Equivalent |
|---------------------|---------------------|
| Event Bus           | Virtual Host        |
| Rule                | Queue + Binding     |
| Event Pattern       | Routing Key Pattern |
| Target              | Consumer            |

## Getting Started

1. Start RabbitMQ:
'''bash
docker-compose up -d
'''

2. Access Management UI:
- URL: http://localhost:15672
- Username: admin
- Password: admin

3. Run example publisher:
'''bash
pip install pika
python publisher_example.py
'''

4. Run example consumer:
'''bash
python consumer_example.py
'''

## Files Generated
- event-buses.json: Original EventBridge buses
- rules-with-targets.json: Rules and targets
- rabbitmq-definitions.json: RabbitMQ import config
- docker-compose.yml: RabbitMQ container
- publisher_example.py: Event publisher
- consumer_example.py: Event consumer

## Notes
- Lambda targets need to be replaced with consumers
- Event patterns are converted to routing key patterns
- Complex patterns may need manual adjustment
`, eventBusName, region, len(rulesResult.Rules))

	readmePath := filepath.Join(outputDir, "README.md")
	if err := os.WriteFile(readmePath, []byte(readme), 0644); err != nil {
		return fmt.Errorf("failed to write README: %w", err)
	}

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", fmt.Sprintf("EventBridge migration completed: %d rules", len(rulesResult.Rules)))

	return nil
}

func (e *EventBridgeToRabbitMQExecutor) generateRabbitMQDefinitions(busName string, rules []RuleWithTargets) map[string]interface{} {
	exchanges := []map[string]interface{}{
		{
			"name":        "events",
			"vhost":       "/",
			"type":        "topic",
			"durable":     true,
			"auto_delete": false,
		},
	}

	queues := make([]map[string]interface{}, 0)
	bindings := make([]map[string]interface{}, 0)

	for _, rule := range rules {
		if rule.State != "ENABLED" {
			continue
		}

		queueName := fmt.Sprintf("eb-%s", rule.Name)
		queues = append(queues, map[string]interface{}{
			"name":        queueName,
			"vhost":       "/",
			"durable":     true,
			"auto_delete": false,
			"arguments": map[string]interface{}{
				"x-queue-type": "classic",
			},
		})

		// Convert event pattern to routing key
		routingKey := e.patternToRoutingKey(rule.EventPattern)
		bindings = append(bindings, map[string]interface{}{
			"source":           "events",
			"vhost":            "/",
			"destination":      queueName,
			"destination_type": "queue",
			"routing_key":      routingKey,
		})
	}

	return map[string]interface{}{
		"rabbit_version": "3.12.0",
		"users": []map[string]interface{}{
			{
				"name":          "admin",
				"password_hash": "admin",
				"hashing_algorithm": "rabbit_password_hashing_sha256",
				"tags":          "administrator",
			},
		},
		"vhosts": []map[string]interface{}{
			{"name": "/"},
		},
		"permissions": []map[string]interface{}{
			{
				"user":      "admin",
				"vhost":     "/",
				"configure": ".*",
				"write":     ".*",
				"read":      ".*",
			},
		},
		"exchanges": exchanges,
		"queues":    queues,
		"bindings":  bindings,
	}
}

func (e *EventBridgeToRabbitMQExecutor) patternToRoutingKey(pattern interface{}) string {
	if pattern == nil {
		return "#"
	}

	patternMap, ok := pattern.(map[string]interface{})
	if !ok {
		return "#"
	}

	var parts []string

	if source, ok := patternMap["source"].([]interface{}); ok && len(source) > 0 {
		parts = append(parts, fmt.Sprintf("%v", source[0]))
	} else {
		parts = append(parts, "*")
	}

	if detailType, ok := patternMap["detail-type"].([]interface{}); ok && len(detailType) > 0 {
		parts = append(parts, fmt.Sprintf("%v", detailType[0]))
	} else {
		parts = append(parts, "*")
	}

	if len(parts) == 0 {
		return "#"
	}

	return fmt.Sprintf("%s.%s", parts[0], parts[1])
}
