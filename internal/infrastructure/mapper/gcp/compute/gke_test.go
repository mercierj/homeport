package compute

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestGKEConformanceManagedAToZ(t *testing.T) {
	result, err := NewGKEMapper().Map(context.Background(), managedGKEFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated GKE migration", result.ManualSteps)
	}
	if result.DockerService.Image != "rancher/k3s:v1.29.0-k3s1" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA K3s target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/k3s/agent-compose.yml", "config/gke/app-change.env", "config/gke/migration.env", "config/gke/workload-export.env"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/gke/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_GKE_CLUSTER=orders-cluster", "TARGET_KUBECONFIG=./kubeconfig/kubeconfig.yaml"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"setup_k3s.sh", "export_gke_workloads.sh", "apply_k3s_workloads.sh", "validate_k3s_cluster.sh", "backup_gke_config.sh", "cutover_gke_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-kubernetes-workloads":          domainrunbook.StepTypeCommand,
		"provision-k3s-cluster":                domainrunbook.StepTypeCommand,
		"apply-kubernetes-workloads":           domainrunbook.StepTypeCommand,
		"validate-kubernetes-workloads":        domainrunbook.StepTypeCommand,
		"backup-gke-config":                    domainrunbook.StepTypeCommand,
		"cutover-gke-clients":                  domainrunbook.StepTypeAPICall,
		"rollback-kubernetes-source-authority": domainrunbook.StepTypeRollback,
	} {
		if !hasGKERunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewGKEMapper(t *testing.T) {
	m := NewGKEMapper()
	if m == nil {
		t.Fatal("NewGKEMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeGKE {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeGKE)
	}
}

func TestGKEMapper_ResourceType(t *testing.T) {
	m := NewGKEMapper()
	got := m.ResourceType()
	want := resource.TypeGKE

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestGKEMapper_Dependencies(t *testing.T) {
	m := NewGKEMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestGKEMapper_Validate(t *testing.T) {
	m := NewGKEMapper()

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
				Type: resource.TypeGKE,
				Name: "test-cluster",
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

func TestGKEMapper_Map(t *testing.T) {
	m := NewGKEMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic GKE cluster",
			res: &resource.AWSResource{
				ID:   "my-project/us-central1/my-cluster",
				Type: resource.TypeGKE,
				Name: "my-cluster",
				Config: map[string]interface{}{
					"name":               "my-cluster",
					"min_master_version": "1.28",
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
				// Check K3s is used
				if result.DockerService.Image != "rancher/k3s:v1.28.5-k3s1" {
					t.Errorf("Expected K3s image for 1.28, got %s", result.DockerService.Image)
				}
			},
		},
		{
			name: "GKE cluster with node pools",
			res: &resource.AWSResource{
				ID:   "my-project/us-central1/cluster-with-pools",
				Type: resource.TypeGKE,
				Name: "cluster-with-pools",
				Config: map[string]interface{}{
					"name":               "cluster-with-pools",
					"min_master_version": "1.29",
					"node_pool": []interface{}{
						map[string]interface{}{
							"name":       "default-pool",
							"node_count": float64(3),
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warnings about node pools
				if len(result.Warnings) == 0 {
					t.Error("Expected warnings about node pools")
				}
			},
		},
		{
			name: "GKE cluster with network policy",
			res: &resource.AWSResource{
				ID:   "my-project/us-central1/cluster-network",
				Type: resource.TypeGKE,
				Name: "cluster-network",
				Config: map[string]interface{}{
					"name":           "cluster-network",
					"network_policy": map[string]interface{}{"enabled": true},
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

func TestGKEMapper_getK3sImage(t *testing.T) {
	m := NewGKEMapper()

	tests := []struct {
		version string
		want    string
	}{
		{"1.29.0", "rancher/k3s:v1.29.0-k3s1"},
		{"1.28.5", "rancher/k3s:v1.28.5-k3s1"},
		{"1.27.9", "rancher/k3s:v1.27.9-k3s1"},
		{"1.26.0", "rancher/k3s:latest"},
		{"", "rancher/k3s:latest"},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			got := m.getK3sImage(tt.version)
			if got != tt.want {
				t.Errorf("getK3sImage(%q) = %q, want %q", tt.version, got, tt.want)
			}
		})
	}
}

func managedGKEFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "projects/demo/locations/europe-west1/clusters/orders-cluster",
		Type: resource.TypeGKE,
		Name: "orders-cluster",
		Config: map[string]interface{}{
			"name":               "orders-cluster",
			"location":           "europe-west1",
			"min_master_version": "1.29.0",
			"node_pool": []interface{}{
				map[string]interface{}{"name": "default-pool", "node_count": float64(3)},
			},
		},
	}
}

func hasGKERunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
