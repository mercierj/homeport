package networking

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestFrontDoorConformanceManagedAToZ(t *testing.T) {
	result, err := NewFrontDoorMapper().Map(context.Background(), managedFrontDoorFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Front Door migration", result.ManualSteps)
	}
	if result.DockerService.Image != "traefik:v2.10" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Traefik target: %#v", result.DockerService)
	}
	if !hasFrontDoorService(result, "varnish:7.4") {
		t.Fatalf("missing generated Varnish cache service: %#v", result.AdditionalServices)
	}
	for _, file := range []string{"config/traefik/frontdoor-config.yml", "config/varnish/default.vcl", "config/frontdoor/app-change.env", "config/frontdoor/generated-client.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	traefikConfig := string(result.Configs["config/traefik/frontdoor-config.yml"])
	if !strings.Contains(traefikConfig, "service: varnish-cache") {
		t.Fatalf("Traefik config does not route to Varnish:\n%s", traefikConfig)
	}
	appEnv := string(result.Configs["config/frontdoor/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_AZURE_FRONT_DOOR=checkout-frontdoor", "FRONTDOOR_ENDPOINT=http://checkout-frontdoor:80"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"setup_frontdoor.sh", "validate_frontdoor.sh", "backup_frontdoor_config.sh", "cutover_frontdoor_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"render-edge-cache-config":       domainrunbook.StepTypeCommand,
		"validate-cache-behavior":        domainrunbook.StepTypeCommand,
		"backup-frontdoor-config":        domainrunbook.StepTypeCommand,
		"cutover-frontdoor-clients":      domainrunbook.StepTypeAPICall,
		"rollback-edge-source-authority": domainrunbook.StepTypeRollback,
	} {
		if !hasFrontDoorRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewFrontDoorMapper(t *testing.T) {
	m := NewFrontDoorMapper()
	if m == nil {
		t.Fatal("NewFrontDoorMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeFrontDoor {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeFrontDoor)
	}
}

func managedFrontDoorFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "/subscriptions/demo/resourceGroups/rg/providers/Microsoft.Network/frontDoors/checkout-frontdoor",
		Type: resource.TypeFrontDoor,
		Name: "checkout-frontdoor",
		Config: map[string]interface{}{
			"name": "checkout-frontdoor",
			"frontend_endpoint": []interface{}{
				map[string]interface{}{
					"name":      "checkout",
					"host_name": "checkout.example.com",
				},
			},
			"backend_pool": []interface{}{
				map[string]interface{}{"name": "checkout-origin"},
			},
			"routing_rule": []interface{}{
				map[string]interface{}{
					"name":               "checkout",
					"accepted_protocols": []interface{}{"Http", "Https"},
					"patterns_to_match":  []interface{}{"/*"},
				},
			},
		},
	}
}

func hasFrontDoorRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}

func hasFrontDoorService(result *mapper.MappingResult, image string) bool {
	for _, svc := range result.AdditionalServices {
		if svc.Image == image {
			return true
		}
	}
	return false
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
