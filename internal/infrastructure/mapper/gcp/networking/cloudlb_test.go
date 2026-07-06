package networking

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestCloudLBConformanceManagedAToZ(t *testing.T) {
	result, err := NewCloudLBMapper().Map(context.Background(), managedCloudLBFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Cloud Load Balancing migration", result.ManualSteps)
	}
	if result.DockerService.Image != "traefik:v2.10" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Traefik target: %#v", result.DockerService)
	}
	for _, file := range []string{"traefik.yml", "dynamic-config.yml", "config/cloud-lb/app-change.env", "config/cloud-lb/backend-report.yaml"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/cloud-lb/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_CLOUD_LB_SERVICE=edge-lb", "TARGET_LB_ENDPOINT=http://edge-lb:80"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"backup_cloud_lb.sh", "validate_cloud_lb.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"discover-cloud-lb-service": domainrunbook.StepTypeCommand,
		"provision-traefik-lb":      domainrunbook.StepTypeCommand,
		"migrate-cloud-lb-backends": domainrunbook.StepTypeCommand,
		"validate-traefik-lb":       domainrunbook.StepTypeCommand,
		"backup-cloud-lb-config":    domainrunbook.StepTypeCommand,
		"cutover-cloud-lb-endpoint": domainrunbook.StepTypeAPICall,
		"rollback-cloud-lb-service": domainrunbook.StepTypeRollback,
	} {
		if !hasCloudLBRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewCloudLBMapper(t *testing.T) {
	m := NewCloudLBMapper()
	if m == nil {
		t.Fatal("NewCloudLBMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeCloudLB {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeCloudLB)
	}
}

func managedCloudLBFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "projects/demo/global/backendServices/edge-lb",
		Type: resource.TypeCloudLB,
		Name: "edge-lb",
		Config: map[string]interface{}{
			"name":               "edge-lb",
			"protocol":           "HTTP",
			"locality_lb_policy": "ROUND_ROBIN",
			"backend": []interface{}{
				map[string]interface{}{"group": "http://app:8080"},
				map[string]interface{}{"group": "http://app-2:8080"},
			},
		},
	}
}

func hasCloudLBRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}

func TestCloudLBMapper_ResourceType(t *testing.T) {
	m := NewCloudLBMapper()
	got := m.ResourceType()
	want := resource.TypeCloudLB

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestCloudLBMapper_Dependencies(t *testing.T) {
	m := NewCloudLBMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestCloudLBMapper_Validate(t *testing.T) {
	m := NewCloudLBMapper()

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
				Type: resource.TypeGCSBucket,
				Name: "test",
			},
			wantErr: true,
		},
		{
			name: "valid resource",
			res: &resource.AWSResource{
				ID:   "test-id",
				Type: resource.TypeCloudLB,
				Name: "test-lb",
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

func TestCloudLBMapper_Map(t *testing.T) {
	m := NewCloudLBMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Cloud Load Balancer",
			res: &resource.AWSResource{
				ID:   "my-project/my-lb",
				Type: resource.TypeCloudLB,
				Name: "my-lb",
				Config: map[string]interface{}{
					"name": "my-lb",
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
				if result.DockerService.Image != "traefik:v2.10" {
					t.Errorf("Expected Traefik image, got %s", result.DockerService.Image)
				}
			},
		},
		{
			name: "Cloud LB with session affinity",
			res: &resource.AWSResource{
				ID:   "my-project/sticky-lb",
				Type: resource.TypeCloudLB,
				Name: "sticky-lb",
				Config: map[string]interface{}{
					"name":             "sticky-lb",
					"session_affinity": "CLIENT_IP",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Session affinity should be enabled
				if result.DockerService.Labels == nil {
					t.Error("Labels should not be nil")
				}
			},
		},
		{
			name: "Cloud LB with backends",
			res: &resource.AWSResource{
				ID:   "my-project/backend-lb",
				Type: resource.TypeCloudLB,
				Name: "backend-lb",
				Config: map[string]interface{}{
					"name": "backend-lb",
					"backend": []interface{}{
						map[string]interface{}{
							"group": "https://www.googleapis.com/compute/v1/projects/my-project/zones/us-central1-a/instanceGroups/my-ig",
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about backends
				if len(result.Warnings) == 0 {
					t.Error("Expected warnings about backends")
				}
			},
		},
		{
			name: "Cloud LB with health checks",
			res: &resource.AWSResource{
				ID:   "my-project/healthy-lb",
				Type: resource.TypeCloudLB,
				Name: "healthy-lb",
				Config: map[string]interface{}{
					"name": "healthy-lb",
					"health_checks": []interface{}{
						map[string]interface{}{
							"request_path":       "/health",
							"port":               float64(8080),
							"check_interval_sec": float64(10),
							"timeout_sec":        float64(5),
						},
					},
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

func TestCloudLBMapper_extractLoadBalancingScheme(t *testing.T) {
	m := NewCloudLBMapper()

	tests := []struct {
		name   string
		res    *resource.AWSResource
		expect string
	}{
		{
			name: "with locality_lb_policy",
			res: &resource.AWSResource{
				Config: map[string]interface{}{
					"locality_lb_policy": "LEAST_REQUEST",
				},
			},
			expect: "LEAST_REQUEST",
		},
		{
			name: "without policy",
			res: &resource.AWSResource{
				Config: map[string]interface{}{},
			},
			expect: "ROUND_ROBIN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.extractLoadBalancingScheme(tt.res)
			if got != tt.expect {
				t.Errorf("extractLoadBalancingScheme() = %v, want %v", got, tt.expect)
			}
		})
	}
}

func TestCloudLBMapper_sanitizeName(t *testing.T) {
	m := NewCloudLBMapper()

	tests := []struct {
		input string
		want  string
	}{
		{"my-lb", "my-lb"},
		{"MY_LB", "my-lb"},
		{"my lb", "my-lb"},
		{"123lb", "lb"},
		{"", "loadbalancer"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := m.sanitizeName(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
