package messaging

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestEventHubConformanceManagedAToZ(t *testing.T) {
	result, err := NewEventHubMapper().Map(context.Background(), managedEventHubFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Event Hubs migration", result.ManualSteps)
	}
	if result.DockerService.Image != "redpandadata/redpanda:v23.3.5" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 3 {
		t.Fatalf("service does not provision HA Redpanda target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/redpanda/topics.yaml", "config/redpanda/app-change.env", "config/redpanda/replay-plan.yaml"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	topicConfig := string(result.Configs["config/redpanda/topics.yaml"])
	for _, want := range []string{"name: orders-hub", "partitions: 4", "retention.ms: 259200000"} {
		if !strings.Contains(topicConfig, want) {
			t.Fatalf("topic config missing %q:\n%s", want, topicConfig)
		}
	}
	appEnv := string(result.Configs["config/redpanda/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_EVENT_HUB=orders-hub", "KAFKA_BOOTSTRAP_SERVERS=redpanda:9092", "KAFKA_CONSUMER_GROUP=orders-replay", "KAFKA_AUTO_OFFSET_RESET=earliest", "EVENTHUB_REPLAY_MODE=explicit_offsets"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	replayPlan := string(result.Configs["config/redpanda/replay-plan.yaml"])
	for _, want := range []string{"consumer_group: orders-replay", "offset_reset: earliest", "retention_days: 3"} {
		if !strings.Contains(replayPlan, want) {
			t.Fatalf("replay plan missing %q:\n%s", want, replayPlan)
		}
	}
	for _, file := range []string{"setup_redpanda_eventhub.sh", "eventhub_consumer.py", "validate_eventhub_replay.sh", "backup_eventhub_offsets.sh", "cutover_eventhub_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"provision-eventhub-redpanda": domainrunbook.StepTypeCommand,
		"configure-eventhub-replay":   domainrunbook.StepTypeCommand,
		"validate-eventhub-replay":    domainrunbook.StepTypeCommand,
		"backup-eventhub-offsets":     domainrunbook.StepTypeCommand,
		"cutover-eventhub-clients":    domainrunbook.StepTypeAPICall,
		"rollback-eventhub-source":    domainrunbook.StepTypeRollback,
	} {
		if !hasEventHubRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewEventHubMapper(t *testing.T) {
	m := NewEventHubMapper()
	if m == nil {
		t.Fatal("NewEventHubMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeEventHub {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeEventHub)
	}
}

func managedEventHubFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "/subscriptions/demo/resourceGroups/rg/providers/Microsoft.EventHub/namespaces/orders/eventhubs/orders-hub",
		Type: resource.TypeEventHub,
		Name: "orders-hub",
		Config: map[string]interface{}{
			"name":              "orders-hub",
			"partition_count":   4,
			"message_retention": 3,
			"consumer_group":    "orders-replay",
			"offset_reset":      "earliest",
		},
	}
}

func hasEventHubRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}

func TestEventHubMapper_ResourceType(t *testing.T) {
	m := NewEventHubMapper()
	got := m.ResourceType()
	want := resource.TypeEventHub

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestEventHubMapper_Dependencies(t *testing.T) {
	m := NewEventHubMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestEventHubMapper_Validate(t *testing.T) {
	m := NewEventHubMapper()

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
				Type: resource.TypeEC2Instance,
				Name: "test",
			},
			wantErr: true,
		},
		{
			name: "valid resource",
			res: &resource.AWSResource{
				ID:   "test-id",
				Type: resource.TypeEventHub,
				Name: "test-eventhub",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeEventHub,
				Name: "test-eventhub",
			},
			wantErr: true,
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

func TestEventHubMapper_Map(t *testing.T) {
	m := NewEventHubMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Event Hub",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.EventHub/namespaces/ns/eventhubs/my-hub",
				Type: resource.TypeEventHub,
				Name: "my-hub",
				Config: map[string]interface{}{
					"name":            "my-event-hub",
					"partition_count": float64(4),
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
				if result.DockerService.Image == "" {
					t.Error("DockerService.Image is empty")
				}
				if result.DockerService.HealthCheck == nil {
					t.Error("HealthCheck is nil")
				}
			},
		},
		{
			name: "Event Hub with capture",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.EventHub/namespaces/ns/eventhubs/my-hub",
				Type: resource.TypeEventHub,
				Name: "my-hub",
				Config: map[string]interface{}{
					"name":            "my-event-hub",
					"partition_count": float64(2),
					"capture_description": map[string]interface{}{
						"enabled": true,
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if !result.HasWarnings() {
					t.Error("Expected warnings for capture configuration")
				}
			},
		},
		{
			name:    "nil resource",
			res:     nil,
			wantErr: true,
		},
		{
			name: "wrong resource type",
			res: &resource.AWSResource{
				ID:   "wrong-id",
				Type: resource.TypeEC2Instance,
				Name: "wrong",
			},
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
