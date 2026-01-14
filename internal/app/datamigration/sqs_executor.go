package datamigration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// SQSToRabbitMQExecutor migrates SQS queues to RabbitMQ.
type SQSToRabbitMQExecutor struct{}

// NewSQSToRabbitMQExecutor creates a new SQS to RabbitMQ executor.
func NewSQSToRabbitMQExecutor() *SQSToRabbitMQExecutor {
	return &SQSToRabbitMQExecutor{}
}

// Type returns the migration type.
func (e *SQSToRabbitMQExecutor) Type() string {
	return "sqs_to_rabbitmq"
}

// GetPhases returns the migration phases.
func (e *SQSToRabbitMQExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Fetching queue attributes",
		"Creating RabbitMQ resources",
		"Migrating messages",
		"Verifying transfer",
	}
}

// Validate validates the migration configuration.
func (e *SQSToRabbitMQExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	// Validate source config
	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		// Check for queue_url or queue_name
		queueURL, hasURL := config.Source["queue_url"].(string)
		queueName, hasName := config.Source["queue_name"].(string)
		if (!hasURL || queueURL == "") && (!hasName || queueName == "") {
			result.Valid = false
			result.Errors = append(result.Errors, "source.queue_url or source.queue_name is required")
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

	// Validate destination config
	if config.Destination == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "destination configuration is required")
	} else {
		if _, ok := config.Destination["host"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "destination.host is required")
		}
		// exchange and queue are optional - will default to source queue name
	}

	return result, nil
}

// Execute performs the migration.
func (e *SQSToRabbitMQExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	// Extract source configuration
	queueURL, _ := config.Source["queue_url"].(string)
	queueName, _ := config.Source["queue_name"].(string)
	accessKeyID := config.Source["access_key_id"].(string)
	secretAccessKey := config.Source["secret_access_key"].(string)
	region, _ := config.Source["region"].(string)
	if region == "" {
		region = "us-east-1"
	}

	// Extract destination configuration
	rabbitHost := config.Destination["host"].(string)
	rabbitPort := "5672"
	if p, ok := config.Destination["port"]; ok {
		rabbitPort = fmt.Sprintf("%v", p)
	}
	rabbitManagementPort := "15672"
	if p, ok := config.Destination["management_port"]; ok {
		rabbitManagementPort = fmt.Sprintf("%v", p)
	}
	rabbitUser := "guest"
	if u, ok := config.Destination["username"].(string); ok && u != "" {
		rabbitUser = u
	}
	rabbitPass := "guest"
	if p, ok := config.Destination["password"].(string); ok && p != "" {
		rabbitPass = p
	}
	rabbitVHost := "/"
	if v, ok := config.Destination["vhost"].(string); ok && v != "" {
		rabbitVHost = v
	}

	// Determine exchange type
	exchangeType := "fanout"
	if et, ok := config.Destination["exchange_type"].(string); ok && et != "" {
		exchangeType = et
	}

	// Check if we should migrate messages
	migrateMessages := false
	if mm, ok := config.Options["migrate_messages"].(bool); ok {
		migrateMessages = mm
	}

	// Batch size for message migration
	batchSize := 10
	if bs, ok := config.Options["batch_size"].(float64); ok {
		batchSize = int(bs)
	}

	// =========================================================================
	// Phase 1: Validating credentials
	// =========================================================================
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating AWS credentials")
	EmitProgress(m, 5, "Checking AWS credentials")

	// Test AWS credentials by listing queues
	testCmd := exec.CommandContext(ctx, "aws", "sqs", "list-queues",
		"--region", region,
		"--max-results", "1",
	)
	testCmd.Env = append(os.Environ(),
		"AWS_ACCESS_KEY_ID="+accessKeyID,
		"AWS_SECRET_ACCESS_KEY="+secretAccessKey,
		"AWS_DEFAULT_REGION="+region,
	)

	if output, err := testCmd.CombinedOutput(); err != nil {
		EmitLog(m, "error", fmt.Sprintf("AWS credential validation failed: %s", string(output)))
		return fmt.Errorf("failed to validate AWS credentials: %w", err)
	}
	EmitLog(m, "info", "AWS credentials validated successfully")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// =========================================================================
	// Phase 2: Fetching queue attributes
	// =========================================================================
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", "Fetching SQS queue attributes")
	EmitProgress(m, 15, "Getting queue information")

	// If only queue name is provided, get the queue URL
	if queueURL == "" && queueName != "" {
		getURLCmd := exec.CommandContext(ctx, "aws", "sqs", "get-queue-url",
			"--queue-name", queueName,
			"--region", region,
		)
		getURLCmd.Env = append(os.Environ(),
			"AWS_ACCESS_KEY_ID="+accessKeyID,
			"AWS_SECRET_ACCESS_KEY="+secretAccessKey,
			"AWS_DEFAULT_REGION="+region,
		)

		output, err := getURLCmd.Output()
		if err != nil {
			return fmt.Errorf("failed to get queue URL for %s: %w", queueName, err)
		}

		var urlResult struct {
			QueueUrl string `json:"QueueUrl"`
		}
		if err := json.Unmarshal(output, &urlResult); err != nil {
			return fmt.Errorf("failed to parse queue URL response: %w", err)
		}
		queueURL = urlResult.QueueUrl
		EmitLog(m, "info", fmt.Sprintf("Resolved queue URL: %s", queueURL))
	}

	// Extract queue name from URL if not provided
	if queueName == "" {
		parts := strings.Split(queueURL, "/")
		queueName = parts[len(parts)-1]
	}

	// Get queue attributes
	attrCmd := exec.CommandContext(ctx, "aws", "sqs", "get-queue-attributes",
		"--queue-url", queueURL,
		"--attribute-names", "All",
		"--region", region,
	)
	attrCmd.Env = append(os.Environ(),
		"AWS_ACCESS_KEY_ID="+accessKeyID,
		"AWS_SECRET_ACCESS_KEY="+secretAccessKey,
		"AWS_DEFAULT_REGION="+region,
	)

	attrOutput, err := attrCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get queue attributes: %w", err)
	}

	var queueAttrs struct {
		Attributes map[string]string `json:"Attributes"`
	}
	if err := json.Unmarshal(attrOutput, &queueAttrs); err != nil {
		return fmt.Errorf("failed to parse queue attributes: %w", err)
	}

	EmitLog(m, "info", fmt.Sprintf("Queue: %s", queueName))
	if msgCount, ok := queueAttrs.Attributes["ApproximateNumberOfMessages"]; ok {
		EmitLog(m, "info", fmt.Sprintf("Approximate message count: %s", msgCount))
	}
	if visTimeout, ok := queueAttrs.Attributes["VisibilityTimeout"]; ok {
		EmitLog(m, "info", fmt.Sprintf("Visibility timeout: %s seconds", visTimeout))
	}
	if dlqArn, ok := queueAttrs.Attributes["RedrivePolicy"]; ok {
		EmitLog(m, "info", fmt.Sprintf("Dead letter queue configured: %s", dlqArn))
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// =========================================================================
	// Phase 3: Creating RabbitMQ resources
	// =========================================================================
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", "Creating RabbitMQ resources")
	EmitProgress(m, 30, "Setting up RabbitMQ")

	// Determine exchange and queue names
	destExchange, _ := config.Destination["exchange"].(string)
	if destExchange == "" {
		destExchange = queueName + "-exchange"
	}
	destQueue, _ := config.Destination["queue"].(string)
	if destQueue == "" {
		destQueue = queueName
	}

	// Create exchange using RabbitMQ Management API
	rabbitAPIBase := fmt.Sprintf("http://%s:%s/api", rabbitHost, rabbitManagementPort)
	client := &http.Client{Timeout: 30 * time.Second}

	// URL encode vhost
	encodedVHost := strings.ReplaceAll(rabbitVHost, "/", "%2F")

	// Create exchange
	exchangeURL := fmt.Sprintf("%s/exchanges/%s/%s", rabbitAPIBase, encodedVHost, destExchange)
	exchangeBody := map[string]interface{}{
		"type":        exchangeType,
		"durable":     true,
		"auto_delete": false,
	}
	exchangeJSON, _ := json.Marshal(exchangeBody)

	req, err := http.NewRequestWithContext(ctx, "PUT", exchangeURL, bytes.NewReader(exchangeJSON))
	if err != nil {
		return fmt.Errorf("failed to create exchange request: %w", err)
	}
	req.SetBasicAuth(rabbitUser, rabbitPass)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		EmitLog(m, "warn", fmt.Sprintf("Failed to create exchange via API: %v, trying rabbitmqadmin", err))
		// Fallback to rabbitmqadmin
		adminCmd := exec.CommandContext(ctx, "rabbitmqadmin",
			"-H", rabbitHost,
			"-P", rabbitManagementPort,
			"-u", rabbitUser,
			"-p", rabbitPass,
			"-V", rabbitVHost,
			"declare", "exchange",
			fmt.Sprintf("name=%s", destExchange),
			fmt.Sprintf("type=%s", exchangeType),
			"durable=true",
		)
		if output, err := adminCmd.CombinedOutput(); err != nil {
			EmitLog(m, "warn", fmt.Sprintf("rabbitmqadmin exchange creation: %s", string(output)))
		}
	} else {
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			EmitLog(m, "info", fmt.Sprintf("Created exchange: %s (type: %s)", destExchange, exchangeType))
		} else if resp.StatusCode == 204 {
			EmitLog(m, "info", fmt.Sprintf("Exchange already exists: %s", destExchange))
		}
	}

	// Create queue
	queueAPIURL := fmt.Sprintf("%s/queues/%s/%s", rabbitAPIBase, encodedVHost, destQueue)
	queueBody := map[string]interface{}{
		"durable":     true,
		"auto_delete": false,
	}

	// Add DLQ settings if present in source
	if _, hasDLQ := queueAttrs.Attributes["RedrivePolicy"]; hasDLQ {
		dlqName := destQueue + "-dlq"
		queueBody["arguments"] = map[string]interface{}{
			"x-dead-letter-exchange":    "",
			"x-dead-letter-routing-key": dlqName,
		}
		EmitLog(m, "info", fmt.Sprintf("Configuring dead letter queue: %s", dlqName))

		// Create the DLQ first
		dlqURL := fmt.Sprintf("%s/queues/%s/%s", rabbitAPIBase, encodedVHost, dlqName)
		dlqBody, _ := json.Marshal(map[string]interface{}{
			"durable":     true,
			"auto_delete": false,
		})
		dlqReq, _ := http.NewRequestWithContext(ctx, "PUT", dlqURL, bytes.NewReader(dlqBody))
		dlqReq.SetBasicAuth(rabbitUser, rabbitPass)
		dlqReq.Header.Set("Content-Type", "application/json")
		if dlqResp, err := client.Do(dlqReq); err == nil {
			dlqResp.Body.Close()
			EmitLog(m, "info", fmt.Sprintf("Created dead letter queue: %s", dlqName))
		}
	}

	queueJSON, _ := json.Marshal(queueBody)
	req, err = http.NewRequestWithContext(ctx, "PUT", queueAPIURL, bytes.NewReader(queueJSON))
	if err != nil {
		return fmt.Errorf("failed to create queue request: %w", err)
	}
	req.SetBasicAuth(rabbitUser, rabbitPass)
	req.Header.Set("Content-Type", "application/json")

	resp, err = client.Do(req)
	if err != nil {
		EmitLog(m, "warn", fmt.Sprintf("Failed to create queue via API: %v, trying rabbitmqadmin", err))
		// Fallback to rabbitmqadmin
		adminCmd := exec.CommandContext(ctx, "rabbitmqadmin",
			"-H", rabbitHost,
			"-P", rabbitManagementPort,
			"-u", rabbitUser,
			"-p", rabbitPass,
			"-V", rabbitVHost,
			"declare", "queue",
			fmt.Sprintf("name=%s", destQueue),
			"durable=true",
		)
		if output, err := adminCmd.CombinedOutput(); err != nil {
			EmitLog(m, "warn", fmt.Sprintf("rabbitmqadmin queue creation: %s", string(output)))
		}
	} else {
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			EmitLog(m, "info", fmt.Sprintf("Created queue: %s", destQueue))
		}
	}

	// Create binding between exchange and queue
	bindingURL := fmt.Sprintf("%s/bindings/%s/e/%s/q/%s", rabbitAPIBase, encodedVHost, destExchange, destQueue)
	bindingBody := map[string]interface{}{
		"routing_key": destQueue,
	}
	bindingJSON, _ := json.Marshal(bindingBody)

	req, err = http.NewRequestWithContext(ctx, "POST", bindingURL, bytes.NewReader(bindingJSON))
	if err != nil {
		return fmt.Errorf("failed to create binding request: %w", err)
	}
	req.SetBasicAuth(rabbitUser, rabbitPass)
	req.Header.Set("Content-Type", "application/json")

	resp, err = client.Do(req)
	if err != nil {
		EmitLog(m, "warn", fmt.Sprintf("Failed to create binding via API: %v, trying rabbitmqadmin", err))
		// Fallback to rabbitmqadmin
		adminCmd := exec.CommandContext(ctx, "rabbitmqadmin",
			"-H", rabbitHost,
			"-P", rabbitManagementPort,
			"-u", rabbitUser,
			"-p", rabbitPass,
			"-V", rabbitVHost,
			"declare", "binding",
			fmt.Sprintf("source=%s", destExchange),
			fmt.Sprintf("destination=%s", destQueue),
			fmt.Sprintf("routing_key=%s", destQueue),
		)
		if output, err := adminCmd.CombinedOutput(); err != nil {
			EmitLog(m, "warn", fmt.Sprintf("rabbitmqadmin binding creation: %s", string(output)))
		}
	} else {
		resp.Body.Close()
		EmitLog(m, "info", fmt.Sprintf("Created binding: %s -> %s", destExchange, destQueue))
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// =========================================================================
	// Phase 4: Migrating messages
	// =========================================================================
	EmitPhase(m, phases[3], 4)
	EmitProgress(m, 50, "Migrating messages")

	if !migrateMessages {
		EmitLog(m, "info", "Message migration disabled, skipping message transfer")
		EmitLog(m, "info", "Set options.migrate_messages to true to migrate existing messages")
	} else {
		EmitLog(m, "info", "Starting message migration from SQS to RabbitMQ")

		// Get visibility timeout from queue attributes
		visibilityTimeout := "30"
		if vt, ok := queueAttrs.Attributes["VisibilityTimeout"]; ok {
			visibilityTimeout = vt
		}

		messagesMigrated := 0
		emptyReceives := 0
		maxEmptyReceives := 3

		// RabbitMQ publish URL for the queue
		publishURL := fmt.Sprintf("%s/exchanges/%s/%s/publish", rabbitAPIBase, encodedVHost, destExchange)

		for emptyReceives < maxEmptyReceives {
			if m.IsCancelled() {
				EmitLog(m, "warn", fmt.Sprintf("Migration cancelled after migrating %d messages", messagesMigrated))
				return fmt.Errorf("migration cancelled")
			}

			// Receive messages from SQS
			receiveCmd := exec.CommandContext(ctx, "aws", "sqs", "receive-message",
				"--queue-url", queueURL,
				"--max-number-of-messages", fmt.Sprintf("%d", batchSize),
				"--visibility-timeout", visibilityTimeout,
				"--wait-time-seconds", "5",
				"--region", region,
			)
			receiveCmd.Env = append(os.Environ(),
				"AWS_ACCESS_KEY_ID="+accessKeyID,
				"AWS_SECRET_ACCESS_KEY="+secretAccessKey,
				"AWS_DEFAULT_REGION="+region,
			)

			output, err := receiveCmd.Output()
			if err != nil {
				EmitLog(m, "warn", fmt.Sprintf("Error receiving messages: %v", err))
				emptyReceives++
				continue
			}

			var receiveResult struct {
				Messages []struct {
					MessageId     string            `json:"MessageId"`
					ReceiptHandle string            `json:"ReceiptHandle"`
					Body          string            `json:"Body"`
					Attributes    map[string]string `json:"Attributes"`
					MD5OfBody     string            `json:"MD5OfBody"`
				} `json:"Messages"`
			}

			if err := json.Unmarshal(output, &receiveResult); err != nil {
				EmitLog(m, "warn", fmt.Sprintf("Error parsing messages: %v", err))
				emptyReceives++
				continue
			}

			if len(receiveResult.Messages) == 0 {
				emptyReceives++
				EmitLog(m, "info", fmt.Sprintf("No messages received (attempt %d/%d)", emptyReceives, maxEmptyReceives))
				continue
			}

			emptyReceives = 0

			// Process each message
			for _, msg := range receiveResult.Messages {
				// Publish to RabbitMQ
				publishBody := map[string]interface{}{
					"properties": map[string]interface{}{
						"delivery_mode": 2, // persistent
						"headers": map[string]interface{}{
							"x-sqs-message-id": msg.MessageId,
							"x-sqs-md5":        msg.MD5OfBody,
						},
					},
					"routing_key":      destQueue,
					"payload":          msg.Body,
					"payload_encoding": "string",
				}
				publishJSON, _ := json.Marshal(publishBody)

				req, err := http.NewRequestWithContext(ctx, "POST", publishURL, bytes.NewReader(publishJSON))
				if err != nil {
					EmitLog(m, "error", fmt.Sprintf("Failed to create publish request: %v", err))
					continue
				}
				req.SetBasicAuth(rabbitUser, rabbitPass)
				req.Header.Set("Content-Type", "application/json")

				resp, err := client.Do(req)
				if err != nil {
					EmitLog(m, "error", fmt.Sprintf("Failed to publish message %s: %v", msg.MessageId, err))
					continue
				}
				resp.Body.Close()

				if resp.StatusCode >= 200 && resp.StatusCode < 300 {
					// Delete from SQS after successful publish
					deleteCmd := exec.CommandContext(ctx, "aws", "sqs", "delete-message",
						"--queue-url", queueURL,
						"--receipt-handle", msg.ReceiptHandle,
						"--region", region,
					)
					deleteCmd.Env = append(os.Environ(),
						"AWS_ACCESS_KEY_ID="+accessKeyID,
						"AWS_SECRET_ACCESS_KEY="+secretAccessKey,
						"AWS_DEFAULT_REGION="+region,
					)

					if err := deleteCmd.Run(); err != nil {
						EmitLog(m, "warn", fmt.Sprintf("Failed to delete message %s from SQS: %v", msg.MessageId, err))
					}

					messagesMigrated++
				} else {
					EmitLog(m, "error", fmt.Sprintf("Failed to publish message %s: HTTP %d", msg.MessageId, resp.StatusCode))
				}
			}

			progress := 50 + (40 * messagesMigrated / (messagesMigrated + 10)) // Dynamic progress
			if progress > 90 {
				progress = 90
			}
			EmitProgress(m, progress, fmt.Sprintf("Migrated %d messages", messagesMigrated))
		}

		EmitLog(m, "info", fmt.Sprintf("Message migration complete: %d messages transferred", messagesMigrated))
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// =========================================================================
	// Phase 5: Verifying transfer
	// =========================================================================
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Verifying RabbitMQ setup")
	EmitProgress(m, 95, "Verifying transfer")

	// Check queue exists and get message count
	queueInfoURL := fmt.Sprintf("%s/queues/%s/%s", rabbitAPIBase, encodedVHost, destQueue)
	req, err = http.NewRequestWithContext(ctx, "GET", queueInfoURL, nil)
	if err != nil {
		EmitLog(m, "warn", fmt.Sprintf("Failed to verify queue: %v", err))
	} else {
		req.SetBasicAuth(rabbitUser, rabbitPass)
		resp, err = client.Do(req)
		if err != nil {
			EmitLog(m, "warn", fmt.Sprintf("Failed to verify queue: %v", err))
		} else {
			defer resp.Body.Close()
			if resp.StatusCode == 200 {
				var queueInfo struct {
					Name     string `json:"name"`
					Messages int    `json:"messages"`
					State    string `json:"state"`
				}
				if err := json.NewDecoder(resp.Body).Decode(&queueInfo); err == nil {
					EmitLog(m, "info", fmt.Sprintf("RabbitMQ queue verified: %s (messages: %d, state: %s)",
						queueInfo.Name, queueInfo.Messages, queueInfo.State))
				}
			}
		}
	}

	// Check exchange exists
	exchangeInfoURL := fmt.Sprintf("%s/exchanges/%s/%s", rabbitAPIBase, encodedVHost, destExchange)
	req, err = http.NewRequestWithContext(ctx, "GET", exchangeInfoURL, nil)
	if err != nil {
		EmitLog(m, "warn", fmt.Sprintf("Failed to verify exchange: %v", err))
	} else {
		req.SetBasicAuth(rabbitUser, rabbitPass)
		resp, err = client.Do(req)
		if err != nil {
			EmitLog(m, "warn", fmt.Sprintf("Failed to verify exchange: %v", err))
		} else {
			defer resp.Body.Close()
			if resp.StatusCode == 200 {
				var exchangeInfo struct {
					Name string `json:"name"`
					Type string `json:"type"`
				}
				if err := json.NewDecoder(resp.Body).Decode(&exchangeInfo); err == nil {
					EmitLog(m, "info", fmt.Sprintf("RabbitMQ exchange verified: %s (type: %s)",
						exchangeInfo.Name, exchangeInfo.Type))
				}
			}
		}
	}

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", "SQS to RabbitMQ migration completed successfully")
	EmitLog(m, "info", fmt.Sprintf("Exchange: %s, Queue: %s", destExchange, destQueue))
	EmitLog(m, "info", fmt.Sprintf("RabbitMQ connection: amqp://%s:%s%s", rabbitHost, rabbitPort, rabbitVHost))

	return nil
}
