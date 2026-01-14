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

// ============================================================================
// PubSubToRabbitMQExecutor - Cloud Pub/Sub to RabbitMQ
// ============================================================================

// PubSubToRabbitMQExecutor migrates GCP Cloud Pub/Sub topics and subscriptions to RabbitMQ.
type PubSubToRabbitMQExecutor struct{}

// NewPubSubToRabbitMQExecutor creates a new Pub/Sub to RabbitMQ executor.
func NewPubSubToRabbitMQExecutor() *PubSubToRabbitMQExecutor {
	return &PubSubToRabbitMQExecutor{}
}

// Type returns the migration type.
func (e *PubSubToRabbitMQExecutor) Type() string {
	return "pubsub_to_rabbitmq"
}

// GetPhases returns the migration phases.
func (e *PubSubToRabbitMQExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Fetching Pub/Sub configuration",
		"Creating RabbitMQ resources",
		"Migrating messages",
		"Verifying transfer",
	}
}

// Validate validates the migration configuration.
func (e *PubSubToRabbitMQExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	// Validate source config
	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		// Check for topic or subscription
		topic, hasTopic := config.Source["topic"].(string)
		subscription, hasSubscription := config.Source["subscription"].(string)
		if (!hasTopic || topic == "") && (!hasSubscription || subscription == "") {
			result.Valid = false
			result.Errors = append(result.Errors, "source.topic or source.subscription is required")
		}

		if _, ok := config.Source["project_id"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.project_id is required")
		}

		// Service account JSON is optional - can use application default credentials
		if _, ok := config.Source["service_account_json"].(string); !ok {
			result.Warnings = append(result.Warnings, "source.service_account_json not specified, using application default credentials")
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
		// exchange and queue are optional - will default to source topic/subscription name
	}

	return result, nil
}

// Execute performs the migration.
func (e *PubSubToRabbitMQExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	// Extract source configuration
	projectID := config.Source["project_id"].(string)
	topic, _ := config.Source["topic"].(string)
	subscription, _ := config.Source["subscription"].(string)
	serviceAccountJSON, hasServiceAccount := config.Source["service_account_json"].(string)

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

	// Set up gcloud environment
	var gcloudEnv []string
	var tempCredFile string
	if hasServiceAccount && serviceAccountJSON != "" {
		// Write service account JSON to temp file
		tmpFile, err := os.CreateTemp("", "gcp-creds-*.json")
		if err != nil {
			return fmt.Errorf("failed to create temp credentials file: %w", err)
		}
		tempCredFile = tmpFile.Name()
		defer os.Remove(tempCredFile)

		if _, err := tmpFile.WriteString(serviceAccountJSON); err != nil {
			tmpFile.Close()
			return fmt.Errorf("failed to write credentials: %w", err)
		}
		tmpFile.Close()

		gcloudEnv = append(os.Environ(),
			"GOOGLE_APPLICATION_CREDENTIALS="+tempCredFile,
			"CLOUDSDK_CORE_PROJECT="+projectID,
		)
	} else {
		gcloudEnv = append(os.Environ(),
			"CLOUDSDK_CORE_PROJECT="+projectID,
		)
	}

	// =========================================================================
	// Phase 1: Validating credentials
	// =========================================================================
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating GCP credentials")
	EmitProgress(m, 5, "Checking GCP credentials")

	// Test GCP credentials by listing topics
	testCmd := exec.CommandContext(ctx, "gcloud", "pubsub", "topics", "list",
		"--project", projectID,
		"--limit", "1",
		"--format", "json",
	)
	testCmd.Env = gcloudEnv

	if output, err := testCmd.CombinedOutput(); err != nil {
		EmitLog(m, "error", fmt.Sprintf("GCP credential validation failed: %s", string(output)))
		return fmt.Errorf("failed to validate GCP credentials: %w", err)
	}
	EmitLog(m, "info", "GCP credentials validated successfully")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// =========================================================================
	// Phase 2: Fetching Pub/Sub configuration
	// =========================================================================
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", "Fetching Pub/Sub configuration")
	EmitProgress(m, 15, "Getting Pub/Sub information")

	// Determine the resource name for RabbitMQ
	resourceName := topic
	if resourceName == "" {
		resourceName = subscription
	}
	// Extract just the name part if it's a full path
	if strings.Contains(resourceName, "/") {
		parts := strings.Split(resourceName, "/")
		resourceName = parts[len(parts)-1]
	}

	// Get topic details if topic is specified
	if topic != "" {
		topicCmd := exec.CommandContext(ctx, "gcloud", "pubsub", "topics", "describe",
			topic,
			"--project", projectID,
			"--format", "json",
		)
		topicCmd.Env = gcloudEnv

		output, err := topicCmd.Output()
		if err != nil {
			EmitLog(m, "warn", fmt.Sprintf("Could not fetch topic details: %v", err))
		} else {
			var topicInfo struct {
				Name                 string            `json:"name"`
				Labels               map[string]string `json:"labels"`
				MessageStoragePolicy struct {
					AllowedPersistenceRegions []string `json:"allowedPersistenceRegions"`
				} `json:"messageStoragePolicy"`
			}
			if err := json.Unmarshal(output, &topicInfo); err == nil {
				EmitLog(m, "info", fmt.Sprintf("Topic: %s", topicInfo.Name))
				if len(topicInfo.Labels) > 0 {
					EmitLog(m, "info", fmt.Sprintf("Labels: %v", topicInfo.Labels))
				}
			}
		}

		// List subscriptions for the topic
		subsCmd := exec.CommandContext(ctx, "gcloud", "pubsub", "topics", "list-subscriptions",
			topic,
			"--project", projectID,
			"--format", "json",
		)
		subsCmd.Env = gcloudEnv

		if output, err := subsCmd.Output(); err == nil {
			var subs []string
			if err := json.Unmarshal(output, &subs); err == nil && len(subs) > 0 {
				EmitLog(m, "info", fmt.Sprintf("Found %d subscription(s) for topic", len(subs)))
			}
		}
	}

	// Get subscription details if specified
	if subscription != "" {
		subCmd := exec.CommandContext(ctx, "gcloud", "pubsub", "subscriptions", "describe",
			subscription,
			"--project", projectID,
			"--format", "json",
		)
		subCmd.Env = gcloudEnv

		output, err := subCmd.Output()
		if err != nil {
			EmitLog(m, "warn", fmt.Sprintf("Could not fetch subscription details: %v", err))
		} else {
			var subInfo struct {
				Name               string `json:"name"`
				Topic              string `json:"topic"`
				AckDeadlineSeconds int    `json:"ackDeadlineSeconds"`
				RetainAckedMessages bool  `json:"retainAckedMessages"`
				MessageRetentionDuration string `json:"messageRetentionDuration"`
				DeadLetterPolicy   *struct {
					DeadLetterTopic     string `json:"deadLetterTopic"`
					MaxDeliveryAttempts int    `json:"maxDeliveryAttempts"`
				} `json:"deadLetterPolicy"`
			}
			if err := json.Unmarshal(output, &subInfo); err == nil {
				EmitLog(m, "info", fmt.Sprintf("Subscription: %s", subInfo.Name))
				EmitLog(m, "info", fmt.Sprintf("Topic: %s", subInfo.Topic))
				EmitLog(m, "info", fmt.Sprintf("Ack deadline: %d seconds", subInfo.AckDeadlineSeconds))
				if subInfo.DeadLetterPolicy != nil {
					EmitLog(m, "info", fmt.Sprintf("Dead letter topic: %s", subInfo.DeadLetterPolicy.DeadLetterTopic))
				}
			}
		}
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
		destExchange = resourceName + "-exchange"
	}
	destQueue, _ := config.Destination["queue"].(string)
	if destQueue == "" {
		destQueue = resourceName
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
	} else if subscription == "" {
		EmitLog(m, "info", "No subscription specified, cannot pull messages")
		EmitLog(m, "info", "Specify source.subscription to enable message migration")
	} else {
		EmitLog(m, "info", "Starting message migration from Pub/Sub to RabbitMQ")

		messagesMigrated := 0
		emptyPulls := 0
		maxEmptyPulls := 3

		// RabbitMQ publish URL for the exchange
		publishURL := fmt.Sprintf("%s/exchanges/%s/%s/publish", rabbitAPIBase, encodedVHost, destExchange)

		for emptyPulls < maxEmptyPulls {
			if m.IsCancelled() {
				EmitLog(m, "warn", fmt.Sprintf("Migration cancelled after migrating %d messages", messagesMigrated))
				return fmt.Errorf("migration cancelled")
			}

			// Pull messages from Pub/Sub
			pullCmd := exec.CommandContext(ctx, "gcloud", "pubsub", "subscriptions", "pull",
				subscription,
				"--project", projectID,
				"--limit", fmt.Sprintf("%d", batchSize),
				"--auto-ack",
				"--format", "json",
			)
			pullCmd.Env = gcloudEnv

			output, err := pullCmd.Output()
			if err != nil {
				EmitLog(m, "warn", fmt.Sprintf("Error pulling messages: %v", err))
				emptyPulls++
				continue
			}

			var messages []struct {
				AckID   string `json:"ackId"`
				Message struct {
					Data        string            `json:"data"`
					Attributes  map[string]string `json:"attributes"`
					MessageID   string            `json:"messageId"`
					PublishTime string            `json:"publishTime"`
				} `json:"message"`
			}

			if err := json.Unmarshal(output, &messages); err != nil {
				// Check if output is empty or just whitespace
				if strings.TrimSpace(string(output)) == "" || strings.TrimSpace(string(output)) == "[]" {
					emptyPulls++
					EmitLog(m, "info", fmt.Sprintf("No messages received (attempt %d/%d)", emptyPulls, maxEmptyPulls))
					continue
				}
				EmitLog(m, "warn", fmt.Sprintf("Error parsing messages: %v", err))
				emptyPulls++
				continue
			}

			if len(messages) == 0 {
				emptyPulls++
				EmitLog(m, "info", fmt.Sprintf("No messages received (attempt %d/%d)", emptyPulls, maxEmptyPulls))
				continue
			}

			emptyPulls = 0

			// Process each message
			for _, msg := range messages {
				// Build headers from Pub/Sub attributes
				headers := map[string]interface{}{
					"x-pubsub-message-id":   msg.Message.MessageID,
					"x-pubsub-publish-time": msg.Message.PublishTime,
				}
				for k, v := range msg.Message.Attributes {
					headers["x-pubsub-attr-"+k] = v
				}

				// Publish to RabbitMQ
				publishBody := map[string]interface{}{
					"properties": map[string]interface{}{
						"delivery_mode": 2, // persistent
						"headers":       headers,
					},
					"routing_key":      destQueue,
					"payload":          msg.Message.Data,
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
					EmitLog(m, "error", fmt.Sprintf("Failed to publish message %s: %v", msg.Message.MessageID, err))
					continue
				}
				resp.Body.Close()

				if resp.StatusCode >= 200 && resp.StatusCode < 300 {
					messagesMigrated++
				} else {
					EmitLog(m, "error", fmt.Sprintf("Failed to publish message %s: HTTP %d", msg.Message.MessageID, resp.StatusCode))
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

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", "Pub/Sub to RabbitMQ migration completed successfully")
	EmitLog(m, "info", fmt.Sprintf("Exchange: %s, Queue: %s", destExchange, destQueue))
	EmitLog(m, "info", fmt.Sprintf("RabbitMQ connection: amqp://%s:%s%s", rabbitHost, rabbitPort, rabbitVHost))

	return nil
}

// ============================================================================
// CloudTasksToRabbitMQExecutor - Cloud Tasks to RabbitMQ
// ============================================================================

// CloudTasksToRabbitMQExecutor migrates GCP Cloud Tasks queues to RabbitMQ.
type CloudTasksToRabbitMQExecutor struct{}

// NewCloudTasksToRabbitMQExecutor creates a new Cloud Tasks to RabbitMQ executor.
func NewCloudTasksToRabbitMQExecutor() *CloudTasksToRabbitMQExecutor {
	return &CloudTasksToRabbitMQExecutor{}
}

// Type returns the migration type.
func (e *CloudTasksToRabbitMQExecutor) Type() string {
	return "cloudtasks_to_rabbitmq"
}

// GetPhases returns the migration phases.
func (e *CloudTasksToRabbitMQExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Fetching Cloud Tasks configuration",
		"Creating RabbitMQ resources",
		"Exporting task definitions",
		"Verifying transfer",
	}
}

// Validate validates the migration configuration.
func (e *CloudTasksToRabbitMQExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	// Validate source config
	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["queue"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.queue is required")
		}
		if _, ok := config.Source["project_id"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.project_id is required")
		}
		if _, ok := config.Source["location"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.location is required (e.g., us-central1)")
		}

		// Service account JSON is optional - can use application default credentials
		if _, ok := config.Source["service_account_json"].(string); !ok {
			result.Warnings = append(result.Warnings, "source.service_account_json not specified, using application default credentials")
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

	result.Warnings = append(result.Warnings, "Cloud Tasks migration exports queue configuration; pending tasks cannot be directly migrated")

	return result, nil
}

// Execute performs the migration.
func (e *CloudTasksToRabbitMQExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	// Extract source configuration
	projectID := config.Source["project_id"].(string)
	location := config.Source["location"].(string)
	queueName := config.Source["queue"].(string)
	serviceAccountJSON, hasServiceAccount := config.Source["service_account_json"].(string)

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

	// Set up gcloud environment
	var gcloudEnv []string
	var tempCredFile string
	if hasServiceAccount && serviceAccountJSON != "" {
		// Write service account JSON to temp file
		tmpFile, err := os.CreateTemp("", "gcp-creds-*.json")
		if err != nil {
			return fmt.Errorf("failed to create temp credentials file: %w", err)
		}
		tempCredFile = tmpFile.Name()
		defer os.Remove(tempCredFile)

		if _, err := tmpFile.WriteString(serviceAccountJSON); err != nil {
			tmpFile.Close()
			return fmt.Errorf("failed to write credentials: %w", err)
		}
		tmpFile.Close()

		gcloudEnv = append(os.Environ(),
			"GOOGLE_APPLICATION_CREDENTIALS="+tempCredFile,
			"CLOUDSDK_CORE_PROJECT="+projectID,
		)
	} else {
		gcloudEnv = append(os.Environ(),
			"CLOUDSDK_CORE_PROJECT="+projectID,
		)
	}

	// =========================================================================
	// Phase 1: Validating credentials
	// =========================================================================
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating GCP credentials")
	EmitProgress(m, 5, "Checking GCP credentials")

	// Test GCP credentials by listing queues
	testCmd := exec.CommandContext(ctx, "gcloud", "tasks", "queues", "list",
		"--project", projectID,
		"--location", location,
		"--limit", "1",
		"--format", "json",
	)
	testCmd.Env = gcloudEnv

	if output, err := testCmd.CombinedOutput(); err != nil {
		EmitLog(m, "error", fmt.Sprintf("GCP credential validation failed: %s", string(output)))
		return fmt.Errorf("failed to validate GCP credentials: %w", err)
	}
	EmitLog(m, "info", "GCP credentials validated successfully")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// =========================================================================
	// Phase 2: Fetching Cloud Tasks configuration
	// =========================================================================
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", "Fetching Cloud Tasks queue configuration")
	EmitProgress(m, 15, "Getting queue information")

	// Get queue details
	queueCmd := exec.CommandContext(ctx, "gcloud", "tasks", "queues", "describe",
		queueName,
		"--project", projectID,
		"--location", location,
		"--format", "json",
	)
	queueCmd.Env = gcloudEnv

	var queueConfig struct {
		Name         string `json:"name"`
		State        string `json:"state"`
		RateLimits   struct {
			MaxDispatchesPerSecond  float64 `json:"maxDispatchesPerSecond"`
			MaxConcurrentDispatches int     `json:"maxConcurrentDispatches"`
			MaxBurstSize            int     `json:"maxBurstSize"`
		} `json:"rateLimits"`
		RetryConfig struct {
			MaxAttempts       int    `json:"maxAttempts"`
			MaxRetryDuration  string `json:"maxRetryDuration"`
			MinBackoff        string `json:"minBackoff"`
			MaxBackoff        string `json:"maxBackoff"`
			MaxDoublings      int    `json:"maxDoublings"`
		} `json:"retryConfig"`
	}

	output, err := queueCmd.Output()
	if err != nil {
		EmitLog(m, "error", fmt.Sprintf("Failed to fetch queue details: %v", err))
		return fmt.Errorf("failed to describe Cloud Tasks queue: %w", err)
	}

	if err := json.Unmarshal(output, &queueConfig); err != nil {
		EmitLog(m, "warn", fmt.Sprintf("Could not parse queue config: %v", err))
	} else {
		EmitLog(m, "info", fmt.Sprintf("Queue: %s", queueConfig.Name))
		EmitLog(m, "info", fmt.Sprintf("State: %s", queueConfig.State))
		EmitLog(m, "info", fmt.Sprintf("Max dispatches per second: %.2f", queueConfig.RateLimits.MaxDispatchesPerSecond))
		EmitLog(m, "info", fmt.Sprintf("Max concurrent dispatches: %d", queueConfig.RateLimits.MaxConcurrentDispatches))
		EmitLog(m, "info", fmt.Sprintf("Retry max attempts: %d", queueConfig.RetryConfig.MaxAttempts))
	}

	// List pending tasks
	tasksCmd := exec.CommandContext(ctx, "gcloud", "tasks", "list",
		"--queue", queueName,
		"--project", projectID,
		"--location", location,
		"--format", "json",
	)
	tasksCmd.Env = gcloudEnv

	tasksOutput, err := tasksCmd.Output()
	if err != nil {
		EmitLog(m, "warn", fmt.Sprintf("Could not list tasks: %v", err))
	} else {
		var tasks []map[string]interface{}
		if err := json.Unmarshal(tasksOutput, &tasks); err == nil {
			EmitLog(m, "info", fmt.Sprintf("Found %d pending task(s) in queue", len(tasks)))
		}
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// =========================================================================
	// Phase 3: Creating RabbitMQ resources
	// =========================================================================
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", "Creating RabbitMQ resources")
	EmitProgress(m, 35, "Setting up RabbitMQ")

	// Extract just the queue name from full path if needed
	simpleQueueName := queueName
	if strings.Contains(queueName, "/") {
		parts := strings.Split(queueName, "/")
		simpleQueueName = parts[len(parts)-1]
	}

	// Determine exchange and queue names
	destExchange, _ := config.Destination["exchange"].(string)
	if destExchange == "" {
		destExchange = simpleQueueName + "-exchange"
	}
	destQueue, _ := config.Destination["queue"].(string)
	if destQueue == "" {
		destQueue = simpleQueueName
	}

	// Create exchange using RabbitMQ Management API
	rabbitAPIBase := fmt.Sprintf("http://%s:%s/api", rabbitHost, rabbitManagementPort)
	client := &http.Client{Timeout: 30 * time.Second}

	// URL encode vhost
	encodedVHost := strings.ReplaceAll(rabbitVHost, "/", "%2F")

	// Create exchange (direct type for task routing)
	exchangeURL := fmt.Sprintf("%s/exchanges/%s/%s", rabbitAPIBase, encodedVHost, destExchange)
	exchangeBody := map[string]interface{}{
		"type":        "direct",
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
			"type=direct",
			"durable=true",
		)
		if output, err := adminCmd.CombinedOutput(); err != nil {
			EmitLog(m, "warn", fmt.Sprintf("rabbitmqadmin exchange creation: %s", string(output)))
		}
	} else {
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			EmitLog(m, "info", fmt.Sprintf("Created exchange: %s (type: direct)", destExchange))
		} else if resp.StatusCode == 204 {
			EmitLog(m, "info", fmt.Sprintf("Exchange already exists: %s", destExchange))
		}
	}

	// Create queue with arguments for task-like behavior
	queueAPIURL := fmt.Sprintf("%s/queues/%s/%s", rabbitAPIBase, encodedVHost, destQueue)
	queueBody := map[string]interface{}{
		"durable":     true,
		"auto_delete": false,
		"arguments": map[string]interface{}{
			// Set message TTL based on Cloud Tasks config if available
			"x-message-ttl": 86400000, // 24 hours default
			// Set max retries similar to Cloud Tasks
			"x-delivery-limit": queueConfig.RetryConfig.MaxAttempts,
		},
	}

	// Create DLQ for failed tasks
	dlqName := destQueue + "-dlq"
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

		// Add DLQ configuration to main queue
		queueBody["arguments"].(map[string]interface{})["x-dead-letter-exchange"] = ""
		queueBody["arguments"].(map[string]interface{})["x-dead-letter-routing-key"] = dlqName
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
	// Phase 4: Exporting task definitions
	// =========================================================================
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Exporting Cloud Tasks configuration")
	EmitProgress(m, 70, "Exporting task definitions")

	// Export queue configuration as JSON for reference
	exportConfig := map[string]interface{}{
		"source": map[string]interface{}{
			"type":      "cloud_tasks",
			"project":   projectID,
			"location":  location,
			"queue":     queueName,
			"state":     queueConfig.State,
			"rateLimits": map[string]interface{}{
				"maxDispatchesPerSecond":  queueConfig.RateLimits.MaxDispatchesPerSecond,
				"maxConcurrentDispatches": queueConfig.RateLimits.MaxConcurrentDispatches,
				"maxBurstSize":            queueConfig.RateLimits.MaxBurstSize,
			},
			"retryConfig": map[string]interface{}{
				"maxAttempts":      queueConfig.RetryConfig.MaxAttempts,
				"maxRetryDuration": queueConfig.RetryConfig.MaxRetryDuration,
				"minBackoff":       queueConfig.RetryConfig.MinBackoff,
				"maxBackoff":       queueConfig.RetryConfig.MaxBackoff,
			},
		},
		"destination": map[string]interface{}{
			"type":     "rabbitmq",
			"host":     rabbitHost,
			"port":     rabbitPort,
			"vhost":    rabbitVHost,
			"exchange": destExchange,
			"queue":    destQueue,
			"dlq":      dlqName,
		},
		"migrationNotes": []string{
			"Cloud Tasks HTTP targets should be converted to RabbitMQ consumers",
			"Rate limiting should be implemented in the consumer application",
			"Retry logic is handled by RabbitMQ x-delivery-limit and DLQ",
			"Scheduled tasks require a delay exchange or scheduled task library",
		},
	}

	exportJSON, _ := json.MarshalIndent(exportConfig, "", "  ")
	EmitLog(m, "info", "Migration configuration exported:")
	EmitLog(m, "debug", string(exportJSON))

	EmitLog(m, "info", "Note: Pending tasks cannot be directly migrated from Cloud Tasks")
	EmitLog(m, "info", "Consider draining the queue before migration or handling them separately")

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
					Name      string                 `json:"name"`
					Messages  int                    `json:"messages"`
					State     string                 `json:"state"`
					Arguments map[string]interface{} `json:"arguments"`
				}
				if err := json.NewDecoder(resp.Body).Decode(&queueInfo); err == nil {
					EmitLog(m, "info", fmt.Sprintf("RabbitMQ queue verified: %s (state: %s)", queueInfo.Name, queueInfo.State))
					if dl, ok := queueInfo.Arguments["x-delivery-limit"]; ok {
						EmitLog(m, "info", fmt.Sprintf("Delivery limit: %v", dl))
					}
				}
			}
		}
	}

	// Verify DLQ
	dlqInfoURL := fmt.Sprintf("%s/queues/%s/%s", rabbitAPIBase, encodedVHost, dlqName)
	req, err = http.NewRequestWithContext(ctx, "GET", dlqInfoURL, nil)
	if err == nil {
		req.SetBasicAuth(rabbitUser, rabbitPass)
		resp, err = client.Do(req)
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == 200 {
				EmitLog(m, "info", fmt.Sprintf("Dead letter queue verified: %s", dlqName))
			}
		}
	}

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", "Cloud Tasks to RabbitMQ migration completed successfully")
	EmitLog(m, "info", fmt.Sprintf("Exchange: %s, Queue: %s, DLQ: %s", destExchange, destQueue, dlqName))
	EmitLog(m, "info", fmt.Sprintf("RabbitMQ connection: amqp://%s:%s%s", rabbitHost, rabbitPort, rabbitVHost))

	return nil
}
