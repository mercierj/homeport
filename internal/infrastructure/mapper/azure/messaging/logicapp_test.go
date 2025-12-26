package messaging

import (
	"context"
	"testing"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

func TestNewLogicAppMapper(t *testing.T) {
	m := NewLogicAppMapper()
	if m == nil {
		t.Fatal("NewLogicAppMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeLogicApp {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeLogicApp)
	}
}

func TestLogicAppMapper_ResourceType(t *testing.T) {
	m := NewLogicAppMapper()
	got := m.ResourceType()
	want := resource.TypeLogicApp

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestLogicAppMapper_Dependencies(t *testing.T) {
	m := NewLogicAppMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestLogicAppMapper_Validate(t *testing.T) {
	m := NewLogicAppMapper()

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
				Type: resource.TypeLogicApp,
				Name: "test-logicapp",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeLogicApp,
				Name: "test-logicapp",
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

func TestLogicAppMapper_Map(t *testing.T) {
	m := NewLogicAppMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Logic App",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Logic/workflows/my-workflow",
				Type: resource.TypeLogicApp,
				Name: "my-workflow",
				Config: map[string]interface{}{
					"name": "my-logic-app",
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
			name: "Logic App with workflow definition",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Logic/workflows/my-workflow",
				Type: resource.TypeLogicApp,
				Name: "my-workflow",
				Config: map[string]interface{}{
					"name": "my-logic-app",
					"workflow_definition": map[string]interface{}{
						"$schema": "https://schema.management.azure.com/schemas/...",
						"actions": map[string]interface{}{},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if !result.HasWarnings() {
					t.Error("Expected warnings for workflow definition")
				}
			},
		},
		{
			name: "Logic App with access control",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Logic/workflows/my-workflow",
				Type: resource.TypeLogicApp,
				Name: "my-workflow",
				Config: map[string]interface{}{
					"name": "my-logic-app",
					"access_control": map[string]interface{}{
						"trigger": map[string]interface{}{},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if !result.HasWarnings() {
					t.Error("Expected warnings for access control")
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
