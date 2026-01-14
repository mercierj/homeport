package compute

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestNewFunctionMapper(t *testing.T) {
	m := NewFunctionMapper()
	if m == nil {
		t.Fatal("NewFunctionMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeAzureFunction {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeAzureFunction)
	}
}

func TestFunctionMapper_ResourceType(t *testing.T) {
	m := NewFunctionMapper()
	got := m.ResourceType()
	want := resource.TypeAzureFunction

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestFunctionMapper_Dependencies(t *testing.T) {
	m := NewFunctionMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestFunctionMapper_Validate(t *testing.T) {
	m := NewFunctionMapper()

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
				Type: resource.TypeAzureFunction,
				Name: "test-function",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeAzureFunction,
				Name: "test-function",
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

func TestFunctionMapper_Map(t *testing.T) {
	m := NewFunctionMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Node.js function",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Web/sites/my-function",
				Type: resource.TypeAzureFunction,
				Name: "my-function",
				Config: map[string]interface{}{
					"name": "my-function-app",
					"site_config": map[string]interface{}{
						"application_stack": map[string]interface{}{
							"node_version": "18",
						},
					},
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
			name: "Python function",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Web/sites/my-python-func",
				Type: resource.TypeAzureFunction,
				Name: "my-python-func",
				Config: map[string]interface{}{
					"name": "my-python-function",
					"site_config": map[string]interface{}{
						"application_stack": map[string]interface{}{
							"python_version": "3.11",
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if result.DockerService.Environment["FUNCTIONS_WORKER_RUNTIME"] != "python" {
					t.Error("Expected FUNCTIONS_WORKER_RUNTIME to be python")
				}
			},
		},
		{
			name: "function with storage account",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Web/sites/my-func",
				Type: resource.TypeAzureFunction,
				Name: "my-func",
				Config: map[string]interface{}{
					"name":                 "my-func",
					"storage_account_name": "mystorageaccount",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if !result.HasWarnings() {
					t.Error("Expected warnings for storage account")
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
