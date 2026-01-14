// Package stacks provides specialized merger implementations for different stack types.
// Each merger handles the consolidation logic for a specific category of cloud resources.
package stacks

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	"github.com/homeport/homeport/internal/domain/stack"
	"github.com/homeport/homeport/internal/infrastructure/consolidator"
)

// MessagingMerger consolidates messaging resources into a single RabbitMQ stack.
// It handles SQS, SNS, EventBridge, Pub/Sub, ServiceBus, Kinesis and similar resources.
type MessagingMerger struct {
	*consolidator.BaseMerger
}

// NewMessagingMerger creates a new MessagingMerger instance.
func NewMessagingMerger() *MessagingMerger {
	return &MessagingMerger{
		BaseMerger: consolidator.NewBaseMerger(stack.StackTypeMessaging),
	}
}

// StackType returns the stack type this merger handles.
func (m *MessagingMerger) StackType() stack.StackType {
	return stack.StackTypeMessaging
}

// CanMerge checks if this merger can handle the given results.
// Returns true if there are messaging resources to consolidate.
func (m *MessagingMerger) CanMerge(results []*mapper.MappingResult) bool {
	if len(results) == 0 {
		return false
	}

	// Check if any result is a messaging resource
	for _, result := range results {
		if result == nil {
			continue
		}
		if isMessagingResource(result.SourceResourceType) {
			return true
		}
	}

	return false
}

// Merge creates a consolidated messaging stack with RabbitMQ.
// It maps:
// - SQS queues -> RabbitMQ queues
// - SNS topics -> RabbitMQ exchanges (topic type)
// - EventBridge -> RabbitMQ exchanges (headers type)
// - Kinesis streams -> RabbitMQ streams (x-queue-type: stream)
// - Pub/Sub topics -> RabbitMQ exchanges
// - ServiceBus queues/topics -> RabbitMQ queues/exchanges
func (m *MessagingMerger) Merge(ctx context.Context, results []*mapper.MappingResult, opts *consolidator.MergeOptions) (*stack.Stack, error) {
	if opts == nil {
		opts = consolidator.DefaultOptions()
	}

	// Create the stack
	name := "messaging"
	if opts.NamePrefix != "" {
		name = opts.NamePrefix + "-" + name
	}

	stk := stack.NewStack(stack.StackTypeMessaging, name)
	stk.Description = "Message queues, pub/sub, and event streaming (RabbitMQ)"

	// Create the RabbitMQ service
	rabbitmq := stack.NewService("rabbitmq", "rabbitmq:3-management")
	rabbitmq.Ports = []string{"5672:5672", "15672:15672"}
	rabbitmq.Environment = map[string]string{
		"RABBITMQ_DEFAULT_USER":       "${RABBITMQ_USER:-admin}",
		"RABBITMQ_DEFAULT_PASS":       "${RABBITMQ_PASS:-changeme}",
		"RABBITMQ_SERVER_ADDITIONAL_ERL_ARGS": "-rabbitmq_management load_definitions \"/etc/rabbitmq/definitions.json\"",
	}
	rabbitmq.Volumes = []string{
		"rabbitmq_data:/var/lib/rabbitmq",
		"./config/rabbitmq/definitions.json:/etc/rabbitmq/definitions.json:ro",
	}
	rabbitmq.HealthCheck = &stack.HealthCheck{
		Test:        []string{"CMD", "rabbitmq-diagnostics", "-q", "ping"},
		Interval:    "30s",
		Timeout:     "10s",
		Retries:     3,
		StartPeriod: "30s",
	}
	rabbitmq.Labels = map[string]string{
		"homeport.stack":    "messaging",
		"homeport.service":  "rabbitmq",
		"homeport.role":     "primary",
	}

	stk.AddService(rabbitmq)

	// Add volume for RabbitMQ data
	stk.AddVolume(stack.Volume{
		Name:   "rabbitmq_data",
		Driver: "local",
		Labels: map[string]string{
			"homeport.stack": "messaging",
		},
	})

	// Generate definitions.json from source resources
	definitions := m.generateDefinitions(results)
	definitionsJSON, err := json.MarshalIndent(definitions, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to generate RabbitMQ definitions: %w", err)
	}
	stk.AddConfig("rabbitmq/definitions.json", definitionsJSON)

	// Generate RabbitMQ configuration
	rabbitmqConfig := m.generateRabbitMQConfig()
	stk.AddConfig("rabbitmq/rabbitmq.conf", rabbitmqConfig)

	// Add source resources
	for _, result := range results {
		if result == nil {
			continue
		}

		// Track source resource
		res := &resource.Resource{
			Type: resource.Type(result.SourceResourceType),
			Name: result.SourceResourceName,
		}
		stk.AddSourceResource(res)
	}

	// Add metadata about consolidated resources
	stk.Metadata["source_queues"] = fmt.Sprintf("%d", countResourcesByType(results, "queue"))
	stk.Metadata["source_topics"] = fmt.Sprintf("%d", countResourcesByType(results, "topic"))
	stk.Metadata["source_streams"] = fmt.Sprintf("%d", countResourcesByType(results, "stream"))
	stk.Metadata["source_events"] = fmt.Sprintf("%d", countResourcesByType(results, "event"))

	// Add messaging-specific manual steps
	manualSteps := []string{
		"Update application connection strings to use RabbitMQ (amqp://localhost:5672)",
		"Review message format compatibility - RabbitMQ uses AMQP protocol",
		"Migrate existing messages from cloud queues before cutover",
		"Update dead-letter queue configurations to use RabbitMQ DLX",
		"Configure message TTL and retention policies in RabbitMQ",
		"Set up monitoring for queue depth and consumer lag",
	}

	// Add provider-specific migration steps
	for _, result := range results {
		if result == nil {
			continue
		}
		switch {
		case strings.Contains(result.SourceResourceType, "sqs"):
			manualSteps = append(manualSteps, "SQS: Review visibility timeout settings and map to RabbitMQ acknowledgment timeout")
		case strings.Contains(result.SourceResourceType, "sns"):
			manualSteps = append(manualSteps, "SNS: Migrate SNS subscriptions to RabbitMQ queue bindings")
		case strings.Contains(result.SourceResourceType, "kinesis"):
			manualSteps = append(manualSteps, "Kinesis: Consider using RabbitMQ streams for ordered message processing")
		case strings.Contains(result.SourceResourceType, "eventbridge"):
			manualSteps = append(manualSteps, "EventBridge: Map event patterns to RabbitMQ header routing")
		case strings.Contains(result.SourceResourceType, "pubsub"):
			manualSteps = append(manualSteps, "Pub/Sub: Migrate acknowledgment deadlines to RabbitMQ consumer settings")
		case strings.Contains(result.SourceResourceType, "servicebus"):
			manualSteps = append(manualSteps, "ServiceBus: Review session-based messaging requirements")
		}
	}

	// Store manual steps in metadata
	for i, step := range manualSteps {
		stk.Metadata[fmt.Sprintf("manual_step_%d", i)] = step
	}

	return stk, nil
}

// RabbitMQDefinitions represents the RabbitMQ definitions.json structure.
type RabbitMQDefinitions struct {
	RabbitVersion string                 `json:"rabbit_version"`
	Queues        []QueueDefinition      `json:"queues"`
	Exchanges     []ExchangeDefinition   `json:"exchanges"`
	Bindings      []BindingDefinition    `json:"bindings"`
	Users         []UserDefinition       `json:"users"`
	VHosts        []VHostDefinition      `json:"vhosts"`
	Permissions   []PermissionDefinition `json:"permissions"`
	Parameters    []interface{}          `json:"parameters"`
	Policies      []PolicyDefinition     `json:"policies"`
}

// QueueDefinition represents a RabbitMQ queue definition.
type QueueDefinition struct {
	Name       string                 `json:"name"`
	VHost      string                 `json:"vhost"`
	Durable    bool                   `json:"durable"`
	AutoDelete bool                   `json:"auto_delete"`
	Arguments  map[string]interface{} `json:"arguments"`
}

// ExchangeDefinition represents a RabbitMQ exchange definition.
type ExchangeDefinition struct {
	Name       string                 `json:"name"`
	VHost      string                 `json:"vhost"`
	Type       string                 `json:"type"`
	Durable    bool                   `json:"durable"`
	AutoDelete bool                   `json:"auto_delete"`
	Internal   bool                   `json:"internal"`
	Arguments  map[string]interface{} `json:"arguments"`
}

// BindingDefinition represents a RabbitMQ binding.
type BindingDefinition struct {
	Source          string                 `json:"source"`
	VHost           string                 `json:"vhost"`
	Destination     string                 `json:"destination"`
	DestinationType string                 `json:"destination_type"`
	RoutingKey      string                 `json:"routing_key"`
	Arguments       map[string]interface{} `json:"arguments"`
}

// UserDefinition represents a RabbitMQ user.
type UserDefinition struct {
	Name         string `json:"name"`
	PasswordHash string `json:"password_hash"`
	HashingAlgo  string `json:"hashing_algorithm"`
	Tags         string `json:"tags"`
}

// VHostDefinition represents a RabbitMQ virtual host.
type VHostDefinition struct {
	Name string `json:"name"`
}

// PermissionDefinition represents RabbitMQ permissions.
type PermissionDefinition struct {
	User      string `json:"user"`
	VHost     string `json:"vhost"`
	Configure string `json:"configure"`
	Write     string `json:"write"`
	Read      string `json:"read"`
}

// PolicyDefinition represents a RabbitMQ policy.
type PolicyDefinition struct {
	Name       string                 `json:"name"`
	VHost      string                 `json:"vhost"`
	Pattern    string                 `json:"pattern"`
	ApplyTo    string                 `json:"apply-to"`
	Definition map[string]interface{} `json:"definition"`
	Priority   int                    `json:"priority"`
}

// generateDefinitions creates RabbitMQ definitions from mapping results.
func (m *MessagingMerger) generateDefinitions(results []*mapper.MappingResult) *RabbitMQDefinitions {
	definitions := &RabbitMQDefinitions{
		RabbitVersion: "3.13.0",
		Queues:        make([]QueueDefinition, 0),
		Exchanges:     make([]ExchangeDefinition, 0),
		Bindings:      make([]BindingDefinition, 0),
		Users:         make([]UserDefinition, 0),
		VHosts: []VHostDefinition{
			{Name: "/"},
		},
		Permissions: make([]PermissionDefinition, 0),
		Parameters:  make([]interface{}, 0),
		Policies:    make([]PolicyDefinition, 0),
	}

	// Process each result
	for _, result := range results {
		if result == nil {
			continue
		}

		resourceType := strings.ToLower(result.SourceResourceType)
		resourceName := consolidator.NormalizeName(result.SourceResourceName)

		switch {
		case strings.Contains(resourceType, "sqs") || strings.Contains(resourceType, "queue"):
			queue := m.mapSQSToQueue(result)
			if queue != nil {
				definitions.Queues = append(definitions.Queues, *queue)
			}

		case strings.Contains(resourceType, "sns") || strings.Contains(resourceType, "topic"):
			exchange := m.mapSNSToExchange(result)
			if exchange != nil {
				definitions.Exchanges = append(definitions.Exchanges, *exchange)
				// Create a default queue for the topic
				queue := &QueueDefinition{
					Name:       resourceName + "-default",
					VHost:      "/",
					Durable:    true,
					AutoDelete: false,
					Arguments:  make(map[string]interface{}),
				}
				definitions.Queues = append(definitions.Queues, *queue)
				// Bind the queue to the exchange
				binding := &BindingDefinition{
					Source:          exchange.Name,
					VHost:           "/",
					Destination:     queue.Name,
					DestinationType: "queue",
					RoutingKey:      "#",
					Arguments:       make(map[string]interface{}),
				}
				definitions.Bindings = append(definitions.Bindings, *binding)
			}

		case strings.Contains(resourceType, "eventbridge") || strings.Contains(resourceType, "event"):
			exchange := m.mapEventBridgeToExchange(result)
			if exchange != nil {
				definitions.Exchanges = append(definitions.Exchanges, *exchange)
			}

		case strings.Contains(resourceType, "kinesis") || strings.Contains(resourceType, "stream"):
			queue := m.mapKinesisToStream(result)
			if queue != nil {
				definitions.Queues = append(definitions.Queues, *queue)
			}

		case strings.Contains(resourceType, "pubsub"):
			// GCP Pub/Sub - create both exchange and queue
			exchange := &ExchangeDefinition{
				Name:       resourceName,
				VHost:      "/",
				Type:       "topic",
				Durable:    true,
				AutoDelete: false,
				Arguments:  make(map[string]interface{}),
			}
			definitions.Exchanges = append(definitions.Exchanges, *exchange)

		case strings.Contains(resourceType, "servicebus"):
			// Azure ServiceBus - could be queue or topic
			if strings.Contains(resourceType, "topic") {
				exchange := &ExchangeDefinition{
					Name:       resourceName,
					VHost:      "/",
					Type:       "topic",
					Durable:    true,
					AutoDelete: false,
					Arguments:  make(map[string]interface{}),
				}
				definitions.Exchanges = append(definitions.Exchanges, *exchange)
			} else {
				queue := &QueueDefinition{
					Name:       resourceName,
					VHost:      "/",
					Durable:    true,
					AutoDelete: false,
					Arguments:  make(map[string]interface{}),
				}
				definitions.Queues = append(definitions.Queues, *queue)
			}
		}
	}

	// Add default DLX exchange and queue
	dlxExchange := ExchangeDefinition{
		Name:       "dlx",
		VHost:      "/",
		Type:       "direct",
		Durable:    true,
		AutoDelete: false,
		Arguments:  make(map[string]interface{}),
	}
	definitions.Exchanges = append(definitions.Exchanges, dlxExchange)

	dlxQueue := QueueDefinition{
		Name:       "dead-letters",
		VHost:      "/",
		Durable:    true,
		AutoDelete: false,
		Arguments:  make(map[string]interface{}),
	}
	definitions.Queues = append(definitions.Queues, dlxQueue)

	// Bind DLX queue
	dlxBinding := BindingDefinition{
		Source:          "dlx",
		VHost:           "/",
		Destination:     "dead-letters",
		DestinationType: "queue",
		RoutingKey:      "#",
		Arguments:       make(map[string]interface{}),
	}
	definitions.Bindings = append(definitions.Bindings, dlxBinding)

	// Add default DLX policy
	dlxPolicy := PolicyDefinition{
		Name:    "dlx-policy",
		VHost:   "/",
		Pattern: ".*",
		ApplyTo: "queues",
		Definition: map[string]interface{}{
			"dead-letter-exchange":    "dlx",
			"dead-letter-routing-key": "dead-letter",
		},
		Priority: 0,
	}
	definitions.Policies = append(definitions.Policies, dlxPolicy)

	return definitions
}

// mapSQSToQueue converts an SQS queue to a RabbitMQ queue definition.
func (m *MessagingMerger) mapSQSToQueue(result *mapper.MappingResult) *QueueDefinition {
	name := consolidator.NormalizeName(result.SourceResourceName)
	if name == "" {
		name = "sqs-queue"
	}

	queue := &QueueDefinition{
		Name:       name,
		VHost:      "/",
		Durable:    true,
		AutoDelete: false,
		Arguments:  make(map[string]interface{}),
	}

	// Extract SQS-specific settings from DockerService environment if available
	if result.DockerService != nil && result.DockerService.Environment != nil {
		if ttl, ok := result.DockerService.Environment["MESSAGE_RETENTION_PERIOD"]; ok {
			// Convert SQS retention (seconds) to RabbitMQ TTL (milliseconds)
			queue.Arguments["x-message-ttl"] = ttl + "000"
		}
	}

	return queue
}

// mapSNSToExchange converts an SNS topic to a RabbitMQ exchange.
func (m *MessagingMerger) mapSNSToExchange(result *mapper.MappingResult) *ExchangeDefinition {
	name := consolidator.NormalizeName(result.SourceResourceName)
	if name == "" {
		name = "sns-topic"
	}

	exchange := &ExchangeDefinition{
		Name:       name,
		VHost:      "/",
		Type:       "topic", // SNS is pub/sub, so topic exchange is appropriate
		Durable:    true,
		AutoDelete: false,
		Internal:   false,
		Arguments:  make(map[string]interface{}),
	}

	return exchange
}

// mapEventBridgeToExchange converts an EventBridge bus to a RabbitMQ headers exchange.
func (m *MessagingMerger) mapEventBridgeToExchange(result *mapper.MappingResult) *ExchangeDefinition {
	name := consolidator.NormalizeName(result.SourceResourceName)
	if name == "" {
		name = "eventbridge"
	}

	exchange := &ExchangeDefinition{
		Name:       name,
		VHost:      "/",
		Type:       "headers", // EventBridge uses pattern matching, headers exchange is closest
		Durable:    true,
		AutoDelete: false,
		Internal:   false,
		Arguments:  make(map[string]interface{}),
	}

	return exchange
}

// mapKinesisToStream converts a Kinesis stream to a RabbitMQ stream queue.
func (m *MessagingMerger) mapKinesisToStream(result *mapper.MappingResult) *QueueDefinition {
	name := consolidator.NormalizeName(result.SourceResourceName)
	if name == "" {
		name = "kinesis-stream"
	}

	queue := &QueueDefinition{
		Name:       name,
		VHost:      "/",
		Durable:    true,
		AutoDelete: false,
		Arguments: map[string]interface{}{
			"x-queue-type":       "stream",
			"x-max-length-bytes": 10737418240, // 10GB default
		},
	}

	// Extract shard count and map to stream segments
	if result.DockerService != nil && result.DockerService.Environment != nil {
		if shards, ok := result.DockerService.Environment["SHARD_COUNT"]; ok {
			queue.Arguments["x-stream-max-segment-size-bytes"] = shards
		}
	}

	return queue
}

// generateRabbitMQConfig generates the RabbitMQ configuration file.
func (m *MessagingMerger) generateRabbitMQConfig() []byte {
	config := `# RabbitMQ Configuration
# Generated by Homeport

# Load definitions on startup
management.load_definitions = /etc/rabbitmq/definitions.json

# Networking
listeners.tcp.default = 5672

# Management plugin
management.tcp.port = 15672

# Memory
vm_memory_high_watermark.relative = 0.7
vm_memory_high_watermark_paging_ratio = 0.5

# Disk free limit
disk_free_limit.absolute = 2GB

# Logging
log.console = true
log.console.level = info
log.file = false

# Clustering preparation (for future scaling)
cluster_formation.peer_discovery_backend = rabbit_peer_discovery_classic_config
cluster_formation.classic_config.nodes.1 = rabbit@rabbitmq

# Stream settings for Kinesis-like workloads
stream.initial_credits = 50000
stream.credits_required_for_unblock = 25000
`
	return []byte(config)
}

// isMessagingResource checks if a resource type is a messaging resource.
func isMessagingResource(resourceType string) bool {
	resourceType = strings.ToLower(resourceType)
	messagingPatterns := []string{
		"sqs", "sns", "kinesis", "eventbridge",
		"pubsub", "servicebus", "eventhub",
		"queue", "topic", "stream", "event",
	}

	for _, pattern := range messagingPatterns {
		if strings.Contains(resourceType, pattern) {
			return true
		}
	}

	return false
}

// countResourcesByType counts resources matching a type pattern.
func countResourcesByType(results []*mapper.MappingResult, typePattern string) int {
	count := 0
	for _, result := range results {
		if result == nil {
			continue
		}
		if strings.Contains(strings.ToLower(result.SourceResourceType), typePattern) {
			count++
		}
	}
	return count
}
