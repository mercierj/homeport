package messaging

import (
	"context"
	"testing"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
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
				if result.DockerService.Image != "rabbitmq:3.12-management-alpine" {
					t.Errorf("Expected RabbitMQ image, got %s", result.DockerService.Image)
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
					"name":                    "ordered-topic",
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

func TestPubSubMapper_generateRabbitMQDefinitions(t *testing.T) {
	m := NewPubSubMapper()

	res := &resource.AWSResource{
		ID:   "my-project/test-topic",
		Type: resource.TypePubSubTopic,
		Config: map[string]interface{}{
			"name": "test-topic",
		},
	}

	definitions := m.generateRabbitMQDefinitions(res, "test-topic")

	// Check that definitions contain expected content
	if definitions == "" {
		t.Error("generateRabbitMQDefinitions returned empty string")
	}
	if !containsStr(definitions, "test-topic") {
		t.Error("Definitions should contain topic name")
	}
	if !containsStr(definitions, "exchanges") {
		t.Error("Definitions should contain exchanges")
	}
}
