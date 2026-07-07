package messaging

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestLogicAppConformanceManagedAToZ(t *testing.T) {
	result, err := NewLogicAppMapper().Map(context.Background(), managedLogicAppFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Logic Apps migration", result.ManualSteps)
	}
	if result.DockerService.Image != "n8nio/n8n:latest" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA n8n target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/n8n/workflows/logicapp_workflow.json", "config/logicapp/app-change.env", "config/logicapp/generated-client.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/logicapp/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_LOGIC_APP=orders-flow", "TARGET_LOGICAPP_WEBHOOK=http://n8n:5678/webhook/orders-flow"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"setup_n8n_logicapp.sh", "validate_logicapp_workflow.sh", "backup_logicapp_config.sh", "cutover_logicapp_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-logicapp-definition": domainrunbook.StepTypeCommand,
		"provision-logicapp-target":  domainrunbook.StepTypeCommand,
		"validate-logicapp-workflow": domainrunbook.StepTypeCommand,
		"backup-logicapp-config":     domainrunbook.StepTypeCommand,
		"cutover-logicapp-clients":   domainrunbook.StepTypeAPICall,
		"rollback-logicapp-source":   domainrunbook.StepTypeRollback,
	} {
		if !hasLogicAppRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewLogicAppMapper(t *testing.T) {
	m := NewLogicAppMapper()
	if m == nil {
		t.Fatal("NewLogicAppMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeLogicApp {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeLogicApp)
	}
}

func managedLogicAppFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "/subscriptions/demo/resourceGroups/rg/providers/Microsoft.Logic/workflows/orders-flow",
		Type: resource.TypeLogicApp,
		Name: "orders-flow",
		Config: map[string]interface{}{
			"name": "orders-flow",
			"workflow_definition": map[string]interface{}{
				"triggers": map[string]interface{}{
					"manual": map[string]interface{}{"type": "Request"},
				},
				"actions": map[string]interface{}{},
			},
		},
	}
}

func hasLogicAppRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
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
