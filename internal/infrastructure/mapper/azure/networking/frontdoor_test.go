package networking

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestNewFrontDoorMapper(t *testing.T) {
	m := NewFrontDoorMapper()
	if m == nil {
		t.Fatal("NewFrontDoorMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeFrontDoor {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeFrontDoor)
	}
}

func TestFrontDoorMapper_ResourceType(t *testing.T) {
	m := NewFrontDoorMapper()
	got := m.ResourceType()
	want := resource.TypeFrontDoor

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestFrontDoorMapper_Dependencies(t *testing.T) {
	m := NewFrontDoorMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestFrontDoorMapper_Validate(t *testing.T) {
	m := NewFrontDoorMapper()

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
				Type: resource.TypeFrontDoor,
				Name: "test-frontdoor",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeFrontDoor,
				Name: "test-frontdoor",
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

func TestFrontDoorMapper_Map(t *testing.T) {
	m := NewFrontDoorMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Front Door",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Network/frontDoors/my-fd",
				Type: resource.TypeFrontDoor,
				Name: "my-fd",
				Config: map[string]interface{}{
					"name": "my-front-door",
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
			name: "Front Door with frontend endpoints",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Network/frontDoors/my-fd",
				Type: resource.TypeFrontDoor,
				Name: "my-fd",
				Config: map[string]interface{}{
					"name": "my-front-door",
					"frontend_endpoint": []interface{}{
						map[string]interface{}{
							"name":      "default-frontend",
							"host_name": "myapp.azurefd.net",
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
					t.Error("Expected warnings for frontend endpoints")
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
