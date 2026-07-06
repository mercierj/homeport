package messaging

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/resource"
)

func TestServiceBusFixtureMapsToRabbitMQGuidedPath(t *testing.T) {
	result, err := NewServiceBusQueueMapper().Map(context.Background(), &resource.AWSResource{
		ID:   "queue-1",
		Type: resource.TypeServiceBusQueue,
		Name: "orders",
		Config: map[string]interface{}{
			"name":                         "orders",
			"requires_duplicate_detection": true,
		},
	})
	if err != nil {
		t.Fatalf("Map() error = %v", err)
	}
	if result.DockerService == nil || result.DockerService.Image != "rabbitmq:3.12-management-alpine" {
		t.Fatalf("DockerService = %#v", result.DockerService)
	}
	if len(result.ManualSteps) == 0 {
		t.Fatal("ManualSteps is empty, want guided Service Bus SDK migration note")
	}
}
