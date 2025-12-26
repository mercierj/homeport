package networking

import (
	"context"
	"testing"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

func TestNewLBMapper(t *testing.T) {
	m := NewLBMapper()
	if m == nil {
		t.Fatal("NewLBMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeAzureLB {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeAzureLB)
	}
}

func TestLBMapper_ResourceType(t *testing.T) {
	m := NewLBMapper()
	got := m.ResourceType()
	want := resource.TypeAzureLB

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestLBMapper_Dependencies(t *testing.T) {
	m := NewLBMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestLBMapper_Validate(t *testing.T) {
	m := NewLBMapper()

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
				Type: resource.TypeAzureLB,
				Name: "test-lb",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeAzureLB,
				Name: "test-lb",
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

func TestLBMapper_Map(t *testing.T) {
	m := NewLBMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Load Balancer",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Network/loadBalancers/my-lb",
				Type: resource.TypeAzureLB,
				Name: "my-lb",
				Config: map[string]interface{}{
					"name": "my-load-balancer",
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
			name: "Load Balancer with frontend IPs",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Network/loadBalancers/my-lb",
				Type: resource.TypeAzureLB,
				Name: "my-lb",
				Config: map[string]interface{}{
					"name": "my-load-balancer",
					"frontend_ip_configuration": []interface{}{
						map[string]interface{}{
							"name":               "primary",
							"private_ip_address": "10.0.1.5",
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if !result.HasWarnings() {
					t.Error("Expected warnings for frontend IP configuration")
				}
			},
		},
		{
			name: "Load Balancer with backend pools",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Network/loadBalancers/my-lb",
				Type: resource.TypeAzureLB,
				Name: "my-lb",
				Config: map[string]interface{}{
					"name": "my-load-balancer",
					"backend_address_pool": []interface{}{
						map[string]interface{}{
							"name": "web-servers",
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if !result.HasWarnings() {
					t.Error("Expected warnings for backend pool")
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
