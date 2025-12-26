package messaging

import (
	"context"
	"testing"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

func TestNewServiceBusQueueMapper(t *testing.T) {
	m := NewServiceBusQueueMapper()
	if m == nil {
		t.Fatal("NewServiceBusQueueMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeServiceBusQueue {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeServiceBusQueue)
	}
}

func TestServiceBusQueueMapper_ResourceType(t *testing.T) {
	m := NewServiceBusQueueMapper()
	got := m.ResourceType()
	want := resource.TypeServiceBusQueue

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestServiceBusQueueMapper_Dependencies(t *testing.T) {
	m := NewServiceBusQueueMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestServiceBusQueueMapper_Validate(t *testing.T) {
	m := NewServiceBusQueueMapper()

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
				Type: resource.TypeServiceBusQueue,
				Name: "test-queue",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeServiceBusQueue,
				Name: "test-queue",
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

func TestServiceBusQueueMapper_Map(t *testing.T) {
	m := NewServiceBusQueueMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Service Bus queue",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.ServiceBus/namespaces/ns/queues/my-queue",
				Type: resource.TypeServiceBusQueue,
				Name: "my-queue",
				Config: map[string]interface{}{
					"name": "my-sb-queue",
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
			name: "queue with dead lettering",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.ServiceBus/namespaces/ns/queues/my-queue",
				Type: resource.TypeServiceBusQueue,
				Name: "my-queue",
				Config: map[string]interface{}{
					"name":                                  "my-sb-queue",
					"dead_lettering_on_message_expiration":  true,
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if !result.HasWarnings() {
					t.Error("Expected warnings for dead lettering")
				}
			},
		},
		{
			name: "queue with sessions",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.ServiceBus/namespaces/ns/queues/session-queue",
				Type: resource.TypeServiceBusQueue,
				Name: "session-queue",
				Config: map[string]interface{}{
					"name":             "session-sb-queue",
					"requires_session": true,
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if !result.HasWarnings() {
					t.Error("Expected warnings for session requirement")
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
