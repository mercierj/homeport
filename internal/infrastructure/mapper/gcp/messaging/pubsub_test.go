package messaging

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestNewPubSubMapper(t *testing.T) {
	m := NewPubSubMapper()
	if m == nil {
		t.Fatal("NewPubSubMapper() returned nil")
	}
	if m.ResourceType() != resource.TypePubSubTopic {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypePubSubTopic)
	}
}

func TestPubSubConformanceGeneratedArtifacts(t *testing.T) {
	tests := []struct {
		name   string
		mapper interface {
			Map(context.Context, *resource.AWSResource) (*mapper.MappingResult, error)
		}
		resource *resource.AWSResource
		configs  []string
		scripts  []string
		runbook  string
	}{
		{
			name:   "topic",
			mapper: NewPubSubMapper(),
			resource: &resource.AWSResource{
				ID:   "projects/demo/topics/orders",
				Type: resource.TypePubSubTopic,
				Name: "orders",
				Config: map[string]interface{}{
					"name":                     "orders",
					"message_ordering_enabled": true,
					"dead_letter_topic":        "projects/demo/topics/orders-dlq",
				},
			},
			configs: []string{"config/nats/nats.conf", "config/nats/pubsub-stream.json", "config/pubsub/app-change.env"},
			scripts: []string{"setup_nats_pubsub.sh", "validate_pubsub_adapter.sh", "backup_pubsub_nats.sh", "cutover_pubsub_adapter.sh"},
			runbook: "validate-pubsub-adapter",
		},
		{
			name:   "subscription",
			mapper: NewPubSubSubscriptionMapper(),
			resource: &resource.AWSResource{
				ID:   "projects/demo/subscriptions/orders-worker",
				Type: resource.TypePubSubSubscription,
				Name: "orders-worker",
				Config: map[string]interface{}{
					"name":                         "orders-worker",
					"topic":                        "orders",
					"ack_deadline_seconds":         float64(30),
					"dead_letter_topic":            "projects/demo/topics/orders-dlq",
					"enable_message_ordering":      true,
					"enable_exactly_once_delivery": true,
					"filter":                       `attributes.kind="order"`,
				},
			},
			configs: []string{"config/nats/nats.conf", "config/nats/pubsub-consumer.json", "config/pubsub/app-change.env"},
			scripts: []string{"scripts/migrate-pubsub-subscription.sh", "scripts/validate-pubsub-subscription.sh", "scripts/backup-pubsub-subscription.sh", "scripts/cutover-pubsub-subscription.sh"},
			runbook: "validate-pubsub-subscription-adapter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.mapper.Map(context.Background(), tt.resource)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.ManualSteps) != 0 {
				t.Fatalf("manual steps = %#v, want generated Pub/Sub migration", result.ManualSteps)
			}
			if tt.name == "topic" && (result.DockerService.Image != "nats:2.10-alpine" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 3) {
				t.Fatalf("NATS JetStream service is not HA: %#v", result.DockerService)
			}
			if tt.name == "subscription" && (result.DockerService.Image != "nats:2.10-alpine" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 3) {
				t.Fatalf("subscription NATS JetStream service is not HA: %#v", result.DockerService)
			}
			for _, file := range tt.configs {
				content, ok := result.Configs[file]
				if !ok {
					t.Fatalf("missing config %s", file)
				}
				if strings.Contains(string(content), "TODO") {
					t.Fatalf("config %s contains TODO:\n%s", file, content)
				}
			}
			appEnv := string(result.Configs["config/pubsub/app-change.env"])
			if tt.name == "topic" && (!strings.Contains(appEnv, "APP_CHANGE_MODE=adapter") || !strings.Contains(appEnv, "HOMEPORT_COMPAT_BACKEND=nats-jetstream") || !strings.Contains(appEnv, "PUBSUB_EMULATOR_HOST=http://homeport:8080/api/v1/compat/gcp/pub-sub")) {
				t.Fatalf("app-change env missing NATS-backed Pub/Sub adapter target:\n%s", appEnv)
			}
			if tt.name == "subscription" && (!strings.Contains(appEnv, "APP_CHANGE_MODE=adapter") || !strings.Contains(appEnv, "HOMEPORT_COMPAT_BACKEND=nats-jetstream") || !strings.Contains(appEnv, "PUBSUB_EMULATOR_HOST=http://homeport:8080/api/v1/compat/gcp/pub-sub")) {
				t.Fatalf("app-change env missing NATS-backed Pub/Sub adapter target:\n%s", appEnv)
			}
			for _, file := range tt.scripts {
				if _, ok := result.Scripts[file]; !ok {
					t.Fatalf("missing script %s", file)
				}
			}
			if !hasPubSubRunbookStep(result, tt.runbook, domainrunbook.StepTypeCommand) {
				t.Fatalf("missing runbook step %s: %#v", tt.runbook, result.RunbookSteps)
			}
			for _, step := range result.RunbookSteps {
				if step.Type == domainrunbook.StepTypeInput || step.Status == domainrunbook.StepStatusBlocked {
					t.Fatalf("manual or blocked runbook step = %#v", step)
				}
			}
		})
	}
}

func hasPubSubRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}

func TestPubSubMapper_ResourceType(t *testing.T) {
	m := NewPubSubMapper()
	got := m.ResourceType()
	want := resource.TypePubSubTopic

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestPubSubMapper_Dependencies(t *testing.T) {
	m := NewPubSubMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestPubSubMapper_Validate(t *testing.T) {
	m := NewPubSubMapper()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
	}{
		{
			name:    "nil resource",
			res:     nil,
			wantErr: true,
		},
		{
			name: "wrong resource type",
			res: &resource.AWSResource{
				ID:   "test-id",
				Type: resource.TypeGCSBucket,
				Name: "test",
			},
			wantErr: true,
		},
		{
			name: "valid resource",
			res: &resource.AWSResource{
				ID:   "test-id",
				Type: resource.TypePubSubTopic,
				Name: "test-topic",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := m.Validate(tt.res)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPubSubMapper_Map(t *testing.T) {
	m := NewPubSubMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Pub/Sub topic",
			res: &resource.AWSResource{
				ID:   "my-project/my-topic",
				Type: resource.TypePubSubTopic,
				Name: "my-topic",
				Config: map[string]interface{}{
					"name": "my-topic",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if result.DockerService == nil {
					t.Fatal("DockerService is nil")
				}
				if result.DockerService.Image != "nats:2.10-alpine" {
					t.Errorf("Expected NATS image, got %s", result.DockerService.Image)
				}
			},
		},
		{
			name: "Pub/Sub topic with message ordering",
			res: &resource.AWSResource{
				ID:   "my-project/ordered-topic",
				Type: resource.TypePubSubTopic,
				Name: "ordered-topic",
				Config: map[string]interface{}{
					"name":                     "ordered-topic",
					"message_ordering_enabled": true,
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about message ordering
				hasOrderingWarning := false
				for _, w := range result.Warnings {
					if containsStr(w, "Message ordering") {
						hasOrderingWarning = true
						break
					}
				}
				if !hasOrderingWarning {
					t.Error("Expected warning about message ordering")
				}
			},
		},
		{
			name: "Pub/Sub topic with dead letter topic",
			res: &resource.AWSResource{
				ID:   "my-project/dlq-topic",
				Type: resource.TypePubSubTopic,
				Name: "dlq-topic",
				Config: map[string]interface{}{
					"name":              "dlq-topic",
					"dead_letter_topic": "projects/my-project/topics/dead-letter",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about dead letter topic
				hasDLQWarning := false
				for _, w := range result.Warnings {
					if containsStr(w, "Dead letter topic") {
						hasDLQWarning = true
						break
					}
				}
				if !hasDLQWarning {
					t.Error("Expected warning about dead letter topic")
				}
			},
		},
		{
			name:    "nil resource",
			res:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := m.Map(ctx, tt.res)
			if (err != nil) != tt.wantErr {
				t.Errorf("Map() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}

func TestPubSubMapper_generateNATSJetStreamConfig(t *testing.T) {
	m := NewPubSubMapper()

	res := &resource.AWSResource{
		ID:   "my-project/test-topic",
		Type: resource.TypePubSubTopic,
		Config: map[string]interface{}{
			"name": "test-topic",
		},
	}

	config := m.generateJetStreamConfig(res, "test-topic")

	if config == "" {
		t.Error("generateJetStreamConfig returned empty string")
	}
	if !containsStr(config, "test-topic") {
		t.Error("config should contain topic name")
	}
	if !containsStr(config, "pubsub.test-topic") {
		t.Error("config should contain Pub/Sub subject")
	}
}
