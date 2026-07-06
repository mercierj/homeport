package messaging

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/resource"
)

func TestPubSubFixtureMapsToRabbitMQGuidedPath(t *testing.T) {
	result, err := NewPubSubMapper().Map(context.Background(), &resource.AWSResource{
		ID:   "topic-1",
		Type: resource.TypePubSubTopic,
		Name: "events",
		Config: map[string]interface{}{
			"name":                     "events",
			"message_ordering_enabled": true,
		},
	})
	if err != nil {
		t.Fatalf("Map() error = %v", err)
	}
	if result.DockerService == nil || result.DockerService.Image != "rabbitmq:3.12-management-alpine" {
		t.Fatalf("DockerService = %#v", result.DockerService)
	}
	if len(result.ManualSteps) == 0 {
		t.Fatal("ManualSteps is empty, want guided Pub/Sub SDK migration note")
	}
}
