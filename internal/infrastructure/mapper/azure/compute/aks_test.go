package compute

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
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
