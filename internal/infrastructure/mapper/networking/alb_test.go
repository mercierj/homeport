package networking

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestNewALBMapper(t *testing.T) {
	m := NewALBMapper()
	if m == nil {
		t.Fatal("NewALBMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeALB {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeALB)
	}
}

func TestALBMapper_ResourceType(t *testing.T) {
	m := NewALBMapper()
	got := m.ResourceType()
	want := resource.TypeALB

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestALBMapper_Dependencies(t *testing.T) {
	m := NewALBMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestALBMapper_Validate(t *testing.T) {
	m := NewALBMapper()

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
				Type: resource.TypeS3Bucket,
				Name: "test",
			},
			wantErr: true,
		},
		{
			name: "valid resource",
			res: &resource.AWSResource{
				ID:   "test-id",
				Type: resource.TypeALB,
				Name: "test-alb",
			},
			wantErr: false,
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

func TestALBMapper_Map(t *testing.T) {
	m := NewALBMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic ALB",
			res: &resource.AWSResource{
				ID:   "arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/my-alb/50dc6c495c0c9188",
				Type: resource.TypeALB,
				Name: "my-alb",
				Config: map[string]interface{}{
					"name":               "my-alb",
					"load_balancer_type": "application",
					"internal":           false,
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
				// Should have ports configured
				if len(result.DockerService.Ports) == 0 {
					t.Log("Expected ports to be configured")
				}
			},
		},
		{
			name: "internal ALB",
			res: &resource.AWSResource{
				ID:   "arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/internal-alb/abc123",
				Type: resource.TypeALB,
				Name: "internal-alb",
				Config: map[string]interface{}{
					"name":     "internal-alb",
					"internal": true,
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
			},
		},
		{
			name:    "nil resource",
			res:     nil,
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

func TestALBMapper_MapBuildsDeterministicRoutesWhenListenersAndTargetsAreKnown(t *testing.T) {
	res := &resource.AWSResource{
		ID:   "arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/shop/50dc6c495c0c9188",
		Type: resource.TypeALB,
		Name: "shop",
		Config: map[string]interface{}{
			"name": "shop",
			"listeners": []interface{}{
				map[string]interface{}{
					"port":     80,
					"protocol": "HTTP",
					"rules": []interface{}{
						map[string]interface{}{
							"host":               "shop.example.com",
							"path":               "/api",
							"target_group_name":  "api",
							"health_check_path":  "/ready",
							"target_group_port":  8080,
							"target_group_hosts": []interface{}{"api-1", "api-2"},
						},
					},
				},
			},
		},
	}

	result, err := NewALBMapper().Map(context.Background(), res)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want none when listeners and targets are known", result.ManualSteps)
	}
	dynamic := string(result.Configs["config/traefik/dynamic/config.yml"])
	for _, want := range []string{
		"shop-api-router:",
		"rule: \"Host(`shop.example.com`) && PathPrefix(`/api`)\"",
		"service: shop-api-service",
		"url: \"http://api-1:8080\"",
		"url: \"http://api-2:8080\"",
		"path: \"/ready\"",
	} {
		if !strings.Contains(dynamic, want) {
			t.Fatalf("dynamic config missing %q:\n%s", want, dynamic)
		}
	}
	if strings.Contains(dynamic, "TODO") {
		t.Fatalf("dynamic config still contains TODO:\n%s", dynamic)
	}
}

func TestALBConformanceManagedAToZ(t *testing.T) {
	result, err := NewALBMapper().Map(context.Background(), managedALBFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want fully generated migration", result.ManualSteps)
	}
	if result.DockerService.Image != "traefik:v3.0" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Traefik: %#v", result.DockerService)
	}
	if result.DockerService.HealthCheck == nil {
		t.Fatal("missing Traefik health check")
	}
	if _, ok := result.Configs["config/traefik/traefik.yml"]; !ok {
		t.Fatal("missing static Traefik config")
	}
	dynamic := string(result.Configs["config/traefik/dynamic/config.yml"])
	for _, want := range []string{"shop-api-router", "Host(`shop.example.com`)", "http://api-1:8080", "healthCheck"} {
		if !strings.Contains(dynamic, want) {
			t.Fatalf("dynamic config missing %q:\n%s", want, dynamic)
		}
	}
	if _, ok := result.Scripts["backup_alb_config.sh"]; !ok {
		t.Fatal("missing backup script")
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"render-traefik-routes":             domainrunbook.StepTypeCommand,
		"validate-route-table":              domainrunbook.StepTypeCommand,
		"backup-traefik-config":             domainrunbook.StepTypeCommand,
		"cutover-dns-to-traefik":            domainrunbook.StepTypeDNSCheck,
		"rollback-routing-source-authority": domainrunbook.StepTypeRollback,
	} {
		if !hasRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func managedALBFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/shop/50dc6c495c0c9188",
		Type: resource.TypeALB,
		Name: "shop",
		Config: map[string]interface{}{
			"name": "shop",
			"listeners": []interface{}{
				map[string]interface{}{
					"port":     80,
					"protocol": "HTTP",
					"rules": []interface{}{
						map[string]interface{}{
							"host":               "shop.example.com",
							"path":               "/api",
							"target_group_name":  "api",
							"health_check_path":  "/ready",
							"target_group_port":  8080,
							"target_group_hosts": []interface{}{"api-1", "api-2"},
						},
					},
				},
			},
		},
	}
}

func hasRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
