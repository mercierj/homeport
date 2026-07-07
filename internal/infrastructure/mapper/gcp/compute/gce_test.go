package compute

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestGCEConformanceManagedAToZ(t *testing.T) {
	result, err := NewGCEMapper().Map(context.Background(), managedGCEFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Compute Engine migration", result.ManualSteps)
	}
	if result.DockerService.Image != "ubuntu:22.04" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA container target: %#v", result.DockerService)
	}
	for _, file := range []string{"Dockerfile.web", "config/gce/app-change.env", "config/gce/instance-report.yaml"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/gce/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_GCE_INSTANCE=web", "TARGET_CONTAINER=web", "TARGET_RUNTIME=docker-compose"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"startup-script.sh", "deploy_gce_container.sh", "validate_gce_container.sh", "backup_gce_config.sh", "cutover_gce_container.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"resolve-app-image":                 domainrunbook.StepTypeCommand,
		"deploy-compose-app":                domainrunbook.StepTypeCommand,
		"validate-app-health":               domainrunbook.StepTypeCommand,
		"backup-gce-config":                 domainrunbook.StepTypeCommand,
		"cutover-gce-container":             domainrunbook.StepTypeAPICall,
		"rollback-compute-source-authority": domainrunbook.StepTypeRollback,
	} {
		if !hasGCERunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewGCEMapper(t *testing.T) {
	m := NewGCEMapper()
	if m == nil {
		t.Fatal("NewGCEMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeGCEInstance {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeGCEInstance)
	}
}

func managedGCEFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "projects/demo/zones/europe-west1-b/instances/web",
		Type: resource.TypeGCEInstance,
		Name: "web",
		Config: map[string]interface{}{
			"name":         "web",
			"machine_type": "n2-standard-2",
			"zone":         "europe-west1-b",
			"metadata":     map[string]interface{}{"startup-script": "#!/bin/sh\necho web\n"},
			"boot_disk": map[string]interface{}{
				"initialize_params": map[string]interface{}{"image": "ubuntu-2204-jammy-v20231002"},
			},
			"attached_disk": []interface{}{map[string]interface{}{"device_name": "data"}},
		},
	}
}

func hasGCERunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}

func TestGCEMapper_ResourceType(t *testing.T) {
	m := NewGCEMapper()
	got := m.ResourceType()
	want := resource.TypeGCEInstance

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestGCEMapper_Dependencies(t *testing.T) {
	m := NewGCEMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestGCEMapper_Validate(t *testing.T) {
	m := NewGCEMapper()

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
				Type: resource.TypeCloudRun,
				Name: "test",
			},
			wantErr: true,
		},
		{
			name: "valid resource",
			res: &resource.AWSResource{
				ID:   "test-id",
				Type: resource.TypeGCEInstance,
				Name: "test-instance",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeGCEInstance,
				Name: "test-instance",
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

func TestGCEMapper_Map(t *testing.T) {
	m := NewGCEMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic GCE instance",
			res: &resource.AWSResource{
				ID:   "projects/my-project/zones/us-central1-a/instances/my-instance",
				Type: resource.TypeGCEInstance,
				Name: "my-instance",
				Config: map[string]interface{}{
					"name":         "my-instance",
					"machine_type": "n2-standard-2",
					"zone":         "us-central1-a",
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
			name: "GCE with Ubuntu image",
			res: &resource.AWSResource{
				ID:   "projects/my-project/zones/us-central1-a/instances/ubuntu-instance",
				Type: resource.TypeGCEInstance,
				Name: "ubuntu-instance",
				Config: map[string]interface{}{
					"name":         "ubuntu-instance",
					"machine_type": "e2-medium",
					"boot_disk": map[string]interface{}{
						"initialize_params": map[string]interface{}{
							"image": "ubuntu-2204-jammy-v20231002",
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
		{
			name: "wrong resource type",
			res: &resource.AWSResource{
				ID:   "wrong-id",
				Type: resource.TypeCloudRun,
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
