package networking

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestNewCDNMapper(t *testing.T) {
	m := NewCDNMapper()
	if m == nil {
		t.Fatal("NewCDNMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeAzureCDN {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeAzureCDN)
	}
}

func TestCDNMapper_ResourceType(t *testing.T) {
	m := NewCDNMapper()
	got := m.ResourceType()
	want := resource.TypeAzureCDN

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestCDNConformanceManagedAToZ(t *testing.T) {
	result, err := NewCDNMapper().Map(context.Background(), managedCDNFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Azure CDN migration", result.ManualSteps)
	}
	if result.DockerService.Image != "varnish:7.4" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Varnish target: %#v", result.DockerService)
	}
	if !hasCDNService(result, "caddy:2.8-alpine") {
		t.Fatalf("missing generated Caddy edge service: %#v", result.AdditionalServices)
	}
	for _, file := range []string{"config/varnish/default.vcl", "config/caddy/Caddyfile", "config/cdn/app-change.env", "config/cdn/generated-client.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	vcl := string(result.Configs["config/varnish/default.vcl"])
	if !strings.Contains(vcl, `.host = "origin.example.com"`) {
		t.Fatalf("VCL does not include generated origin:\n%s", vcl)
	}
	appEnv := string(result.Configs["config/cdn/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_AZURE_CDN=checkout-cdn", "CDN_ENDPOINT=http://caddy:80"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"setup_cdn.sh", "validate_cdn.sh", "backup_cdn_config.sh", "cutover_cdn_clients.sh", "purge_cdn_cache.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"render-edge-cache-config":       domainrunbook.StepTypeCommand,
		"validate-cache-behavior":        domainrunbook.StepTypeCommand,
		"backup-cdn-config":              domainrunbook.StepTypeCommand,
		"cutover-cdn-clients":            domainrunbook.StepTypeAPICall,
		"rollback-edge-source-authority": domainrunbook.StepTypeRollback,
	} {
		if !hasCDNRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func managedCDNFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "/subscriptions/demo/resourceGroups/rg/providers/Microsoft.Cdn/profiles/checkout-cdn",
		Type: resource.TypeAzureCDN,
		Name: "checkout-cdn",
		Config: map[string]interface{}{
			"name": "checkout-cdn",
			"sku":  "Standard_Microsoft",
			"endpoint": []interface{}{
				map[string]interface{}{
					"name":             "checkout",
					"origin_host_name": "origin.example.com",
					"origin_path":      "/assets",
					"custom_domain":    "cdn.example.com",
				},
			},
		},
	}
}

func hasCDNRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}

func hasCDNService(result *mapper.MappingResult, image string) bool {
	for _, svc := range result.AdditionalServices {
		if svc.Image == image {
			return true
		}
	}
	return false
}

func TestCDNMapper_Dependencies(t *testing.T) {
	m := NewCDNMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestCDNMapper_Validate(t *testing.T) {
	m := NewCDNMapper()

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
				Type: resource.TypeAzureCDN,
				Name: "test-cdn",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeAzureCDN,
				Name: "test-cdn",
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

func TestCDNMapper_Map(t *testing.T) {
	m := NewCDNMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic CDN profile",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Cdn/profiles/my-cdn",
				Type: resource.TypeAzureCDN,
				Name: "my-cdn",
				Config: map[string]interface{}{
					"name": "my-cdn-profile",
					"sku":  "Standard_Microsoft",
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
			name: "CDN with endpoint",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Cdn/profiles/my-cdn",
				Type: resource.TypeAzureCDN,
				Name: "my-cdn",
				Config: map[string]interface{}{
					"name": "my-cdn-profile",
					"sku":  "Standard_Verizon",
					"endpoint": []interface{}{
						map[string]interface{}{
							"name":             "my-endpoint",
							"origin_host_name": "origin.example.com",
							"origin_path":      "/content",
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
					t.Error("Expected warnings for CDN endpoint")
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
