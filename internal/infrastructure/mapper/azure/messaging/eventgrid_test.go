package messaging

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestEventGridConformanceManagedAToZ(t *testing.T) {
	result, err := NewEventGridMapper().Map(context.Background(), managedEventGridFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Event Grid migration", result.ManualSteps)
	}
	if result.DockerService.Image != "n8nio/n8n:latest" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA n8n target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/n8n/workflows/eventgrid_workflow.json", "config/eventgrid/app-change.env", "config/eventgrid/generated-client.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/eventgrid/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_EVENT_GRID_TOPIC=orders-events", "TARGET_EVENTGRID_WEBHOOK=http://n8n:5678/webhook/orders-events"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"setup_n8n_eventgrid.sh", "validate_eventgrid_delivery.sh", "backup_eventgrid_config.sh", "cutover_eventgrid_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"provision-eventgrid-target":  domainrunbook.StepTypeCommand,
		"validate-eventgrid-delivery": domainrunbook.StepTypeCommand,
		"backup-eventgrid-config":     domainrunbook.StepTypeCommand,
		"cutover-eventgrid-clients":   domainrunbook.StepTypeAPICall,
		"rollback-eventgrid-source":   domainrunbook.StepTypeRollback,
	} {
		if !hasEventGridRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewEventGridMapper(t *testing.T) {
	m := NewEventGridMapper()
	if m == nil {
		t.Fatal("NewEventGridMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeEventGrid {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeEventGrid)
	}
}

func managedEventGridFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "/subscriptions/demo/resourceGroups/rg/providers/Microsoft.EventGrid/topics/orders-events",
		Type: resource.TypeEventGrid,
		Name: "orders-events",
		Config: map[string]interface{}{
			"name": "orders-events",
		},
	}
}

func hasEventGridRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}

func TestEventGridMapper_ResourceType(t *testing.T) {
	m := NewEventGridMapper()
	got := m.ResourceType()
	want := resource.TypeEventGrid

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestEventGridMapper_Dependencies(t *testing.T) {
	m := NewEventGridMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestEventGridMapper_Validate(t *testing.T) {
	m := NewEventGridMapper()

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
				Type: resource.TypeEventGrid,
				Name: "test-eventgrid",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeEventGrid,
				Name: "test-eventgrid",
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

func TestEventGridMapper_Map(t *testing.T) {
	m := NewEventGridMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Event Grid topic",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.EventGrid/topics/my-topic",
				Type: resource.TypeEventGrid,
				Name: "my-topic",
				Config: map[string]interface{}{
					"name": "my-eventgrid-topic",
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
			name: "Event Grid with input schema",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.EventGrid/topics/my-topic",
				Type: resource.TypeEventGrid,
				Name: "my-topic",
				Config: map[string]interface{}{
					"name":         "my-eventgrid-topic",
					"input_schema": "CustomEventSchema",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if !result.HasWarnings() {
					t.Error("Expected warnings for custom input schema")
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
