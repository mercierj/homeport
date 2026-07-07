package compute

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestNewVMMapper(t *testing.T) {
	m := NewVMMapper()
	if m == nil {
		t.Fatal("NewVMMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeAzureVM {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeAzureVM)
	}
}

func TestVMConformanceManagedAToZ(t *testing.T) {
	result, err := NewVMMapper().Map(context.Background(), managedVMFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Azure VM container migration", result.ManualSteps)
	}
	if result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA VM container target: %#v", result.DockerService.Deploy)
	}
	for _, file := range []string{"Dockerfile.checkout-vm", "config/vm/app-change.env", "config/vm/generated-client.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/vm/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_AZURE_VM=checkout-vm", "TARGET_SERVICE=checkout-vm", "TARGET_IMAGE=ubuntu:22.04"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"setup_checkout-vm.sh", "validate_checkout-vm.sh", "backup_checkout-vm.sh", "cutover_checkout-vm.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"resolve-app-image":                 domainrunbook.StepTypeCommand,
		"deploy-compose-app":                domainrunbook.StepTypeCommand,
		"validate-app-health":               domainrunbook.StepTypeCommand,
		"backup-azure-vm-container":         domainrunbook.StepTypeCommand,
		"cutover-azure-vm-container":        domainrunbook.StepTypeAPICall,
		"rollback-compute-source-authority": domainrunbook.StepTypeRollback,
	} {
		if !hasVMRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func managedVMFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "/subscriptions/demo/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/checkout-vm",
		Type: resource.TypeAzureVM,
		Name: "checkout-vm",
		Config: map[string]interface{}{
			"name": "checkout-vm",
			"size": "Standard_D2s_v3",
			"source_image_reference": map[string]interface{}{
				"publisher": "Canonical",
				"offer":     "UbuntuServer",
				"sku":       "22_04-lts",
			},
			"custom_data": "#!/bin/sh\necho boot\n",
		},
	}
}

func hasVMRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}

func TestVMMapper_ResourceType(t *testing.T) {
	m := NewVMMapper()
	got := m.ResourceType()
	want := resource.TypeAzureVM

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestVMMapper_Dependencies(t *testing.T) {
	m := NewVMMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestVMMapper_Validate(t *testing.T) {
	m := NewVMMapper()

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
				Type: resource.TypeAzureVM,
				Name: "test-vm",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeAzureVM,
				Name: "test-vm",
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

func TestVMMapper_Map(t *testing.T) {
	m := NewVMMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Linux VM",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/my-vm",
				Type: resource.TypeAzureVM,
				Name: "my-vm",
				Config: map[string]interface{}{
					"name": "my-linux-vm",
					"size": "Standard_D2s_v3",
					"source_image_reference": map[string]interface{}{
						"publisher": "Canonical",
						"offer":     "UbuntuServer",
						"sku":       "22_04-lts",
					},
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
