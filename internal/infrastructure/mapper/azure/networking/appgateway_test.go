package networking

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestNewAppGatewayMapper(t *testing.T) {
	m := NewAppGatewayMapper()
	if m == nil {
		t.Fatal("NewAppGatewayMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeAppGateway {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeAppGateway)
	}
}

func TestAppGatewayConformanceManagedAToZ(t *testing.T) {
	result, err := NewAppGatewayMapper().Map(context.Background(), managedAppGatewayFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated App Gateway migration", result.ManualSteps)
	}
	if result.DockerService.Image != "traefik:v2.10" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Traefik target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/traefik/appgateway-config.yml", "config/traefik/middleware.yml", "config/appgateway/app-change.env", "config/appgateway/generated-client.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/appgateway/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_APP_GATEWAY=checkout-gateway", "TRAEFIK_ENTRYPOINT=websecure"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"setup_appgateway.sh", "validate_appgateway_traefik.sh", "backup_appgateway_config.sh", "cutover_appgateway_routes.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"render-traefik-routes":             domainrunbook.StepTypeCommand,
		"validate-route-table":              domainrunbook.StepTypeCommand,
		"block-unsupported-route-features":  domainrunbook.StepTypeInput,
		"rollback-routing-source-authority": domainrunbook.StepTypeRollback,
	} {
		if !hasAppGatewayRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func managedAppGatewayFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "/subscriptions/demo/resourceGroups/rg/providers/Microsoft.Network/applicationGateways/checkout-gateway",
		Type: resource.TypeAppGateway,
		Name: "checkout-gateway",
		Config: map[string]interface{}{
			"name": "checkout-gateway",
		},
	}
}

func hasAppGatewayRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}

func TestAppGatewayMapper_ResourceType(t *testing.T) {
	m := NewAppGatewayMapper()
	got := m.ResourceType()
	want := resource.TypeAppGateway

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestAppGatewayMapper_Dependencies(t *testing.T) {
	m := NewAppGatewayMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestAppGatewayMapper_Validate(t *testing.T) {
	m := NewAppGatewayMapper()

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
				Type: resource.TypeAppGateway,
				Name: "test-appgw",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeAppGateway,
				Name: "test-appgw",
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

func TestAppGatewayMapper_Map(t *testing.T) {
	m := NewAppGatewayMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Application Gateway",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Network/applicationGateways/my-appgw",
				Type: resource.TypeAppGateway,
				Name: "my-appgw",
				Config: map[string]interface{}{
					"name": "my-application-gateway",
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
			name: "Application Gateway with WAF",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Network/applicationGateways/my-appgw-waf",
				Type: resource.TypeAppGateway,
				Name: "my-appgw-waf",
				Config: map[string]interface{}{
					"name": "my-waf-gateway",
					"sku": map[string]interface{}{
						"name":     "WAF_v2",
						"tier":     "WAF_v2",
						"capacity": float64(2),
					},
					"waf_configuration": map[string]interface{}{
						"enabled":          true,
						"firewall_mode":    "Prevention",
						"rule_set_type":    "OWASP",
						"rule_set_version": "3.2",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if !result.HasWarnings() {
					t.Error("Expected warnings for WAF configuration")
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
