package compute

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
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

func TestFunctionConformanceManagedAToZ(t *testing.T) {
	result, err := NewFunctionMapper().Map(context.Background(), managedFunctionFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Azure Functions migration", result.ManualSteps)
	}
	if result.DockerService.Build == nil || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA function target: %#v", result.DockerService)
	}
	for _, file := range []string{"functions/checkout-func/Dockerfile", "functions/checkout-func/host.json", "config/functions/app-change.env", "config/functions/generated-client.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/functions/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_FUNCTION_APP=checkout-func", "FUNCTION_URL=http://checkout-func.localhost/api"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"deploy_function.sh", "validate_function.sh", "backup_function_config.sh", "cutover_function_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"build-function-image":               domainrunbook.StepTypeCommand,
		"validate-function-invoke":           domainrunbook.StepTypeCommand,
		"backup-function-config":             domainrunbook.StepTypeCommand,
		"cutover-function-clients":           domainrunbook.StepTypeAPICall,
		"rollback-function-source-authority": domainrunbook.StepTypeRollback,
	} {
		if !hasFunctionRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func managedFunctionFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "/subscriptions/demo/resourceGroups/rg/providers/Microsoft.Web/sites/checkout-func",
		Type: resource.TypeAzureFunction,
		Name: "checkout-func",
		Config: map[string]interface{}{
			"name":                 "checkout-func",
			"storage_account_name": "checkoutstorage",
			"site_config": map[string]interface{}{
				"application_stack": map[string]interface{}{
					"node_version": "18",
				},
			},
		},
	}
}

func hasFunctionRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
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
