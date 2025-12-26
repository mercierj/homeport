package messaging

import (
	"context"
	"testing"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

func TestNewServiceBusMapper(t *testing.T) {
	m := NewServiceBusMapper()
	if m == nil {
		t.Fatal("NewServiceBusMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeServiceBus {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeServiceBus)
	}
}

func TestServiceBusMapper_ResourceType(t *testing.T) {
	m := NewServiceBusMapper()
	got := m.ResourceType()
	want := resource.TypeServiceBus

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestServiceBusMapper_Dependencies(t *testing.T) {
	m := NewServiceBusMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestServiceBusMapper_Validate(t *testing.T) {
	m := NewServiceBusMapper()

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
				Type: resource.TypeServiceBus,
				Name: "test-servicebus",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeServiceBus,
				Name: "test-servicebus",
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

func TestServiceBusMapper_Map(t *testing.T) {
	m := NewServiceBusMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Service Bus namespace",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.ServiceBus/namespaces/my-ns",
				Type: resource.TypeServiceBus,
				Name: "my-ns",
				Config: map[string]interface{}{
					"name": "my-servicebus-ns",
					"sku":  "Standard",
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
			name: "Premium tier namespace",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.ServiceBus/namespaces/premium-ns",
				Type: resource.TypeServiceBus,
				Name: "premium-ns",
				Config: map[string]interface{}{
					"name":     "premium-servicebus-ns",
					"sku":      "Premium",
					"capacity": float64(2),
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if !result.HasWarnings() {
					t.Error("Expected warnings for Premium tier")
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
