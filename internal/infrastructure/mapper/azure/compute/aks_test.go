package compute

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestNewAKSMapper(t *testing.T) {
	m := NewAKSMapper()
	if m == nil {
		t.Fatal("NewAKSMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeAKS {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeAKS)
	}
}

func TestAKSConformanceManagedAToZ(t *testing.T) {
	result, err := NewAKSMapper().Map(context.Background(), managedAKSFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated AKS migration", result.ManualSteps)
	}
	if result.DockerService.Image != "rancher/k3s:v1.29.0-k3s1" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA K3s target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/k3s/agent-compose.yml", "config/aks/app-change.env"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/aks/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_AKS_CLUSTER=checkout-aks", "KUBECONFIG=./kubeconfig/kubeconfig.yaml"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"setup_k3s.sh", "validate_aks_k3s.sh", "backup_aks_config.sh", "cutover_aks_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-kubernetes-workloads":          domainrunbook.StepTypeCommand,
		"provision-k3s-cluster":                domainrunbook.StepTypeCommand,
		"apply-kubernetes-workloads":           domainrunbook.StepTypeCommand,
		"validate-kubernetes-workloads":        domainrunbook.StepTypeCommand,
		"rollback-kubernetes-source-authority": domainrunbook.StepTypeRollback,
	} {
		if !hasAKSRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func managedAKSFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "/subscriptions/demo/resourceGroups/rg/providers/Microsoft.ContainerService/managedClusters/checkout-aks",
		Type: resource.TypeAKS,
		Name: "checkout-aks",
		Config: map[string]interface{}{
			"name":               "checkout-aks",
			"kubernetes_version": "1.29",
		},
	}
}

func hasAKSRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}

func TestAKSMapper_ResourceType(t *testing.T) {
	m := NewAKSMapper()
	got := m.ResourceType()
	want := resource.TypeAKS

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestAKSMapper_Dependencies(t *testing.T) {
	m := NewAKSMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestAKSMapper_Validate(t *testing.T) {
	m := NewAKSMapper()

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
				Type: resource.TypeAKS,
				Name: "test-aks",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeAKS,
				Name: "test-aks",
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

func TestAKSMapper_Map(t *testing.T) {
	m := NewAKSMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic AKS cluster",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.ContainerService/managedClusters/my-aks",
				Type: resource.TypeAKS,
				Name: "my-aks",
				Config: map[string]interface{}{
					"name":               "my-aks-cluster",
					"kubernetes_version": "1.28",
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
				if len(result.DockerService.Ports) == 0 {
					t.Error("DockerService.Ports is empty")
				}
			},
		},
		{
			name: "AKS with node pool",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.ContainerService/managedClusters/my-aks",
				Type: resource.TypeAKS,
				Name: "my-aks",
				Config: map[string]interface{}{
					"name":               "my-aks-cluster",
					"kubernetes_version": "1.29",
					"default_node_pool": map[string]interface{}{
						"name":       "default",
						"node_count": float64(3),
						"vm_size":    "Standard_D2s_v3",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if !result.HasWarnings() {
					t.Error("Expected warnings for node pool configuration")
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
