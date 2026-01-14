package networking

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
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
