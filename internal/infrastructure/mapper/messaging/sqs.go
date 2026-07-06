// Package messaging provides mappers for AWS messaging services.
package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/policy"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

// SQSMapper converts AWS SQS queues to RabbitMQ.
type SQSMapper struct {
	*mapper.BaseMapper
}

// NewSQSMapper creates a new SQS to RabbitMQ mapper.
func NewSQSMapper() *SQSMapper {
	return &SQSMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeSQSQueue, nil),
	}
}

// Map converts an SQS queue to a RabbitMQ service.
func (m *SQSMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	queueName := res.GetConfigString("name")
	if queueName == "" {
		queueName = res.Name
	}

	// Create result using new API
	result := mapper.NewMappingResult("rabbitmq")
	svc := result.DockerService

	// Configure RabbitMQ service
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
		"homeport.source":                    "aws_sqs_queue",
		"homeport.queue_name":                queueName,
		"homeport.target":                    "rabbitmq",
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
	svc.Deploy = &mapper.DeployConfig{Replicas: 3}
	svc.Restart = "unless-stopped"

	// Generate RabbitMQ definitions (queues, exchanges, bindings)
	definitions := m.generateRabbitMQDefinitions(res, queueName)
	result.AddConfig("config/rabbitmq/definitions.json", []byte(definitions))

	// Generate queue configuration
	queueConfig := m.generateQueueConfig(queueName)
	result.AddConfig("config/rabbitmq/rabbitmq.conf", []byte(queueConfig))
	result.AddConfig("config/rabbitmq/app-change.env", []byte(m.generateAppChangeConfig(queueName)))

	// Handle FIFO queue
	if m.isFIFOQueue(queueName) || res.GetConfigBool("fifo_queue") {
		result.AddWarning("FIFO queue detected. Generated RabbitMQ single-active-consumer arguments for ordered delivery.")
		result.AddConfig("config/rabbitmq/fifo-policy.json", []byte(m.generateFIFOPolicy(queueName)))
	}

	// Handle dead letter queue
	if dlqArn := res.GetConfigString("redrive_policy"); dlqArn != "" || res.Config["redrive_policy"] != nil {
		m.handleDeadLetterQueue(result, queueName)
	}

	// Handle visibility timeout
	visibilityTimeout := res.GetConfigInt("visibility_timeout_seconds")
	if visibilityTimeout > 0 && visibilityTimeout != 30 {
		result.AddWarning(fmt.Sprintf("Visibility timeout is %d seconds. Configure message TTL in RabbitMQ.", visibilityTimeout))
	}

	// Handle message retention
	messageRetention := res.GetConfigInt("message_retention_seconds")
	if messageRetention > 0 {
		days := messageRetention / 86400
		result.AddWarning(fmt.Sprintf("Message retention is %d days. Configure queue TTL in RabbitMQ.", days))
	}

	// Handle delay queue
	delaySeconds := res.GetConfigInt("delay_seconds")
	if delaySeconds > 0 {
		result.AddWarning(fmt.Sprintf("Default delay is %d seconds. Generated RabbitMQ delayed exchange plugin config.", delaySeconds))
		result.AddConfig("config/rabbitmq/delay-policy.json", []byte(m.generateDelayPolicy(queueName, delaySeconds)))
	}

	// Generate setup script
	setupScript := m.generateSetupScript(queueName)
	result.AddScript("setup_rabbitmq.sh", []byte(setupScript))
	result.AddScript("export_sqs_queue.sh", []byte(m.generateExportScript(queueName, res.Region)))
	result.AddScript("migrate_sqs_messages.sh", []byte(m.generateMigrateScript(queueName)))
	result.AddScript("validate_sqs_adapter.sh", []byte(m.generateValidateScript(queueName)))
	result.AddScript("backup_sqs_config.sh", []byte(m.generateBackupScript(queueName)))
	result.AddScript("cutover_sqs_adapter.sh", []byte(m.generateCutoverScript(queueName)))
	for _, step := range sqsRunbook(queueName) {
		result.AddRunbookStep(step)
	}

	return result, nil
}

// generateRabbitMQDefinitions creates RabbitMQ definitions JSON.
func (m *SQSMapper) generateRabbitMQDefinitions(res *resource.AWSResource, queueName string) string {
	// Build queue definition
	queueArgs := map[string]interface{}{}

	// Add message TTL if message retention is set
	if messageRetention := res.GetConfigInt("message_retention_seconds"); messageRetention > 0 {
		queueArgs["x-message-ttl"] = messageRetention * 1000 // Convert to milliseconds
	}
	if m.isFIFOQueue(queueName) || res.GetConfigBool("fifo_queue") {
		queueArgs["x-single-active-consumer"] = true
	}
	if dlqArn := res.GetConfigString("redrive_policy"); dlqArn != "" || res.Config["redrive_policy"] != nil {
		queueArgs["x-dead-letter-exchange"] = "dlx"
		queueArgs["x-dead-letter-routing-key"] = queueName + "-dlq"
	}
	if delaySeconds := res.GetConfigInt("delay_seconds"); delaySeconds > 0 {
		queueArgs["x-message-ttl"] = delaySeconds * 1000
		queueArgs["x-dead-letter-exchange"] = "amq.direct"
		queueArgs["x-dead-letter-routing-key"] = queueName
	}

	queueDef := map[string]interface{}{
		"name":        queueName,
		"vhost":       "/",
		"durable":     true,
		"auto_delete": false,
		"arguments":   queueArgs,
	}

	queues := []map[string]interface{}{queueDef}
	exchanges := []map[string]interface{}{
		{
			"name":        "amq.direct",
			"vhost":       "/",
			"type":        "direct",
			"durable":     true,
			"auto_delete": false,
			"internal":    false,
		},
	}
	bindings := []map[string]interface{}{
		{
			"source":           "amq.direct",
			"vhost":            "/",
			"destination":      queueName,
			"destination_type": "queue",
			"routing_key":      queueName,
		},
	}
	if dlqArn := res.GetConfigString("redrive_policy"); dlqArn != "" || res.Config["redrive_policy"] != nil {
		dlqName := queueName + "-dlq"
		queues = append(queues, map[string]interface{}{
			"name":        dlqName,
			"vhost":       "/",
			"durable":     true,
			"auto_delete": false,
			"arguments":   map[string]interface{}{},
		})
		exchanges = append(exchanges, map[string]interface{}{
			"name":        "dlx",
			"vhost":       "/",
			"type":        "direct",
			"durable":     true,
			"auto_delete": false,
			"internal":    false,
		})
		bindings = append(bindings, map[string]interface{}{
			"source":           "dlx",
			"vhost":            "/",
			"destination":      dlqName,
			"destination_type": "queue",
			"routing_key":      dlqName,
		})
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
		"queues":    queues,
		"exchanges": exchanges,
		"bindings":  bindings,
	}

	content, _ := json.MarshalIndent(definitions, "", "  ")
	return string(content)
}

// generateQueueConfig creates a RabbitMQ configuration file.
func (m *SQSMapper) generateQueueConfig(queueName string) string {
	return `# RabbitMQ Configuration
# Generated from SQS queue settings

# Listeners
listeners.tcp.default = 5672

# Management plugin
management.tcp.port = 15672

# Load definitions on startup
management.load_definitions = /etc/rabbitmq/definitions.json

# Default user
default_user = guest
default_pass = guest

# Default vhost
default_vhost = /

# Disk free limit (warning threshold)
disk_free_limit.absolute = 2GB

# Memory threshold
vm_memory_high_watermark.relative = 0.6

# Heartbeat
heartbeat = 60

# Consumer timeout (milliseconds)
consumer_timeout = 1800000

# Log level
log.console.level = info
log.file.level = info
`
}

// handleDeadLetterQueue configures dead letter queue settings.
func (m *SQSMapper) handleDeadLetterQueue(result *mapper.MappingResult, queueName string) {
	dlqName := queueName + "-dlq"

	result.AddWarning(fmt.Sprintf("Dead letter queue configured. Creating DLQ: %s", dlqName))

	dlqNote := fmt.Sprintf(`{
  "source_queue": %q,
  "dead_letter_queue": %q,
  "dead_letter_exchange": "dlx",
  "routing_key": %q
}
`, queueName, dlqName, dlqName)

	result.AddConfig("config/rabbitmq/dlq-policy.json", []byte(dlqNote))
}

// generateSetupScript creates a setup script for RabbitMQ.
func (m *SQSMapper) generateSetupScript(queueName string) string {
	return fmt.Sprintf(`#!/bin/bash
# RabbitMQ Setup Script
# Configures RabbitMQ for SQS queue: %s

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

# Import definitions
echo "Importing queue definitions..."
curl -X POST -u $RABBITMQ_USER:$RABBITMQ_PASS \
  http://$RABBITMQ_HOST:$RABBITMQ_PORT/api/definitions \
  -H "Content-Type: application/json" \
  -d @config/rabbitmq/definitions.json

echo "Queue '%s' created successfully!"
echo "Management UI: http://$RABBITMQ_HOST:$RABBITMQ_PORT"
echo "Credentials: $RABBITMQ_USER / $RABBITMQ_PASS"
echo ""
echo "Connection string: amqp://$RABBITMQ_USER:$RABBITMQ_PASS@$RABBITMQ_HOST:5672/"
`, queueName, queueName)
}

func (m *SQSMapper) generateAppChangeConfig(queueName string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=adapter
SOURCE_QUEUE=%s
TARGET_QUEUE=%s
AWS_ENDPOINT_URL_SQS=http://homeport:8080/api/v1/compat/aws/sqs
HOMEPORT_COMPAT_BACKEND=rabbitmq
HOMEPORT_COMPAT_PROTOCOL=sqs
AMQP_URL=amqp://guest:guest@rabbitmq:5672/
`, queueName, queueName)
}

func (m *SQSMapper) generateFIFOPolicy(queueName string) string {
	return fmt.Sprintf(`{
  "queue": %q,
  "ordered_delivery": "single-active-consumer",
  "deduplication": "message-group-id"
}
`, queueName)
}

func (m *SQSMapper) generateDelayPolicy(queueName string, delaySeconds int) string {
	return fmt.Sprintf(`{
  "queue": %q,
  "delay_seconds": %d,
  "rabbitmq_strategy": "ttl-and-dead-letter-route"
}
`, queueName, delaySeconds)
}

func (m *SQSMapper) generateExportScript(queueName, region string) string {
	if region == "" {
		region = "us-east-1"
	}
	return fmt.Sprintf(`#!/bin/sh
set -eu
AWS_REGION="%s"
QUEUE_NAME="%s"
OUTPUT_DIR="./sqs-export"
mkdir -p "$OUTPUT_DIR"
queue_url=$(aws sqs get-queue-url --queue-name "$QUEUE_NAME" --region "$AWS_REGION" --output text --query QueueUrl)
test -n "$queue_url"
aws sqs get-queue-attributes --queue-url "$queue_url" --attribute-names All --region "$AWS_REGION" --output json > "$OUTPUT_DIR/queue-attributes.json"
aws sqs list-queue-tags --queue-url "$queue_url" --region "$AWS_REGION" --output json > "$OUTPUT_DIR/tags.json"
`, region, queueName)
}

func (m *SQSMapper) generateMigrateScript(queueName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
test -s config/rabbitmq/definitions.json
jq -e --arg q %q '.queues[] | select(.name == $q)' config/rabbitmq/definitions.json >/dev/null
echo "SQS queue %s mapped to RabbitMQ queue %s"
`, queueName, queueName, queueName)
}

func (m *SQSMapper) generateValidateScript(queueName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
curl -fsS -u guest:guest http://localhost:15672/api/overview >/tmp/homeport-rabbitmq-overview.json
test -s config/rabbitmq/definitions.json
grep -q %q config/rabbitmq/definitions.json
test -s config/rabbitmq/app-change.env
`, queueName)
}

func (m *SQSMapper) generateBackupScript(queueName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-rabbitmq-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/rabbitmq setup_rabbitmq.sh export_sqs_queue.sh migrate_sqs_messages.sh validate_sqs_adapter.sh cutover_sqs_adapter.sh
echo "$archive"
`, queueName)
}

func (m *SQSMapper) generateCutoverScript(queueName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
. config/rabbitmq/app-change.env
test "$SOURCE_QUEUE" = %q
test "$APP_CHANGE_MODE" = "adapter"
test "$AWS_ENDPOINT_URL_SQS" = "http://homeport:8080/api/v1/compat/aws/sqs"
echo "Use AWS_ENDPOINT_URL_SQS=$AWS_ENDPOINT_URL_SQS for SQS SDK clients"
`, queueName)
}

func sqsRunbook(queueName string) []domainrunbook.Step {
	metadata := map[string]string{
		"kind":                    "queue",
		"source":                  "aws_sqs_queue",
		"queue":                   queueName,
		"HOMEPORT_TARGET":         "rabbitmq",
		"HOMEPORT_APP_CHANGE":     "adapter",
		"AWS_ENDPOINT_URL_SQS":    "http://homeport:8080/api/v1/compat/aws/sqs",
		"HOMEPORT_COMPAT_BACKEND": "rabbitmq",
		"AMQP_QUEUE":              queueName,
	}
	return []domainrunbook.Step{
		sqsStep("export-sqs-queue", "Export SQS queue", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_sqs_queue.sh"}, "SQS queue attributes and tags are exported", metadata),
		sqsStep("provision-rabbitmq-queue", "Provision RabbitMQ queue", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "setup_rabbitmq.sh"}, "RabbitMQ queue definitions are imported", metadata),
		sqsStep("migrate-sqs-messages", "Migrate SQS message config", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_sqs_messages.sh"}, "SQS queue semantics are mapped to RabbitMQ definitions", metadata),
		sqsStep("validate-sqs-adapter", "Validate SQS compatibility adapter", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_sqs_adapter.sh"}, "RabbitMQ health and SQS adapter config validate", metadata),
		sqsStep("backup-sqs-config", "Backup SQS migration config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_sqs_config.sh"}, "SQS and RabbitMQ migration artifacts are archived", metadata),
		sqsStep("cutover-sqs-clients", "Cut over SQS clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_sqs_adapter.sh"}, "SQS SDK clients use HomePort compatibility endpoint", metadata),
		sqsStep("rollback-sqs-source", "Keep SQS source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "AWS SQS remains authoritative until RabbitMQ delivery validation passes", metadata),
	}
}

func sqsStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
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

// isFIFOQueue checks if the queue is a FIFO queue based on naming convention.
func (m *SQSMapper) isFIFOQueue(queueName string) bool {
	return len(queueName) > 5 && queueName[len(queueName)-5:] == ".fifo"
}

// ExtractPolicies extracts queue policies from the SQS queue.
func (m *SQSMapper) ExtractPolicies(ctx context.Context, res *resource.AWSResource) ([]*policy.Policy, error) {
	var policies []*policy.Policy

	queueName := res.GetConfigString("name")
	if queueName == "" {
		queueName = res.Name
	}

	// Extract queue policy
	if policyDoc := res.GetConfigString("policy"); policyDoc != "" {
		p := policy.NewPolicy(
			res.ID+"-queue-policy",
			queueName+" Queue Policy",
			policy.PolicyTypeResource,
			policy.ProviderAWS,
		)
		p.ResourceID = res.ID
		p.ResourceType = "aws_sqs_queue"
		p.ResourceName = queueName
		p.OriginalDocument = json.RawMessage(policyDoc)
		p.OriginalFormat = "json"
		p.NormalizedPolicy = m.normalizeQueuePolicy(policyDoc)

		// Check for public access
		if strings.Contains(policyDoc, "\"*\"") {
			p.AddWarning("Queue policy may allow public access")
		}

		policies = append(policies, p)
	}

	return policies, nil
}

// normalizeQueuePolicy converts an SQS queue policy to normalized format.
func (m *SQSMapper) normalizeQueuePolicy(policyDoc string) *policy.NormalizedPolicy {
	normalized := &policy.NormalizedPolicy{
		Statements: make([]policy.Statement, 0),
	}

	var awsPolicy struct {
		Version   string `json:"Version"`
		Statement []struct {
			Sid       string      `json:"Sid"`
			Effect    string      `json:"Effect"`
			Principal interface{} `json:"Principal"`
			Action    interface{} `json:"Action"`
			Resource  interface{} `json:"Resource"`
			Condition interface{} `json:"Condition"`
		} `json:"Statement"`
	}

	if err := json.Unmarshal([]byte(policyDoc), &awsPolicy); err != nil {
		return normalized
	}

	normalized.Version = awsPolicy.Version

	for _, stmt := range awsPolicy.Statement {
		normalizedStmt := policy.Statement{
			SID:    stmt.Sid,
			Effect: policy.Effect(stmt.Effect),
		}

		// Parse principals
		normalizedStmt.Principals = m.parsePrincipals(stmt.Principal)

		// Parse actions
		normalizedStmt.Actions = m.parseStringOrSlice(stmt.Action)

		// Parse resources
		normalizedStmt.Resources = m.parseStringOrSlice(stmt.Resource)

		// Parse conditions
		normalizedStmt.Conditions = m.parseConditions(stmt.Condition)

		normalized.Statements = append(normalized.Statements, normalizedStmt)
	}

	return normalized
}

// parsePrincipals converts AWS principal format to normalized principals.
func (m *SQSMapper) parsePrincipals(principal interface{}) []policy.Principal {
	var principals []policy.Principal

	if principal == nil {
		return principals
	}

	switch p := principal.(type) {
	case string:
		if p == "*" {
			principals = append(principals, policy.Principal{Type: "*", ID: "*"})
		} else {
			principals = append(principals, policy.Principal{Type: "AWS", ID: p})
		}
	case map[string]interface{}:
		for pType, pValue := range p {
			ids := m.parseStringOrSlice(pValue)
			for _, id := range ids {
				principals = append(principals, policy.Principal{Type: pType, ID: id})
			}
		}
	}

	return principals
}

// parseStringOrSlice handles AWS policy fields that can be string or array.
func (m *SQSMapper) parseStringOrSlice(value interface{}) []string {
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case string:
		return []string{v}
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}

	return nil
}

// parseConditions converts AWS conditions to normalized format.
func (m *SQSMapper) parseConditions(condition interface{}) []policy.Condition {
	var conditions []policy.Condition

	if condition == nil {
		return conditions
	}

	condMap, ok := condition.(map[string]interface{})
	if !ok {
		return conditions
	}

	for operator, keys := range condMap {
		keyMap, ok := keys.(map[string]interface{})
		if !ok {
			continue
		}

		for key, values := range keyMap {
			cond := policy.Condition{
				Operator: operator,
				Key:      key,
				Values:   m.parseStringOrSlice(values),
			}
			conditions = append(conditions, cond)
		}
	}

	return conditions
}
