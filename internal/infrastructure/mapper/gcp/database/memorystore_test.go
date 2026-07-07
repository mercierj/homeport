package database

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestNewMemorystoreMapper(t *testing.T) {
	m := NewMemorystoreMapper()
	if m == nil {
		t.Fatal("NewMemorystoreMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeMemorystore {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeMemorystore)
	}
}

func TestMemorystoreConformanceManagedAToZ(t *testing.T) {
	result, err := NewMemorystoreMapper().Map(context.Background(), managedMemorystoreFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Memorystore migration", result.ManualSteps)
	}
	if result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Redis target: %#v", result.DockerService.Deploy)
	}
	if result.DockerService.Image != "redis:7.0-alpine" {
		t.Fatalf("image = %s, want normalized Redis image", result.DockerService.Image)
	}
	for _, file := range []string{"config/redis/redis.conf", "config/redis/app-change.env", "config/redis/ha.env"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/redis/app-change.env"])
	for _, want := range []string{"SOURCE_INSTANCE=orders-cache", "TARGET_ENDPOINT=redis:6379", "APP_CHANGE_MODE=generated_patch"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"migrate_memorystore.sh", "backup_memorystore_config.sh", "validate_redis.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"generate-redis-auth":             domainrunbook.StepTypeCommand,
		"sync-redis-data":                 domainrunbook.StepTypeCommand,
		"validate-redis-migration":        domainrunbook.StepTypeCommand,
		"validate-redis-failover":         domainrunbook.StepTypeCommand,
		"backup-memorystore-config":       domainrunbook.StepTypeCommand,
		"cutover-memorystore-endpoint":    domainrunbook.StepTypeAPICall,
		"rollback-redis-source-authority": domainrunbook.StepTypeRollback,
	} {
		if !hasMemorystoreRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
	for _, step := range result.RunbookSteps {
		if step.Type == domainrunbook.StepTypeInput {
			t.Fatalf("manual input runbook step = %#v, want executable conformance", step)
		}
	}
}

func managedMemorystoreFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "projects/demo/locations/us-central1/instances/orders-cache",
		Type: resource.TypeMemorystore,
		Name: "orders-cache",
		Config: map[string]interface{}{
			"name":           "orders-cache",
			"memory_size_gb": float64(4),
			"redis_version":  "REDIS_7_0",
			"tier":           "STANDARD_HA",
		},
	}
}

func hasMemorystoreRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}

func TestMemorystoreMapper_ResourceType(t *testing.T) {
	m := NewMemorystoreMapper()
	got := m.ResourceType()
	want := resource.TypeMemorystore

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestMemorystoreMapper_Dependencies(t *testing.T) {
	m := NewMemorystoreMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestMemorystoreMapper_Validate(t *testing.T) {
	m := NewMemorystoreMapper()

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
				Type: resource.TypeMemorystore,
				Name: "test-redis",
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

func TestMemorystoreMapper_Map(t *testing.T) {
	m := NewMemorystoreMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Memorystore instance",
			res: &resource.AWSResource{
				ID:   "my-project/us-central1/my-redis",
				Type: resource.TypeMemorystore,
				Name: "my-redis",
				Config: map[string]interface{}{
					"name":           "my-redis",
					"memory_size_gb": float64(2),
					"redis_version":  "7.0",
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
				if result.DockerService.Image != "redis:7.0-alpine" {
					t.Errorf("Expected redis:7.0-alpine image, got %s", result.DockerService.Image)
				}
			},
		},
		{
			name: "Memorystore with STANDARD_HA tier",
			res: &resource.AWSResource{
				ID:   "my-project/us-central1/ha-redis",
				Type: resource.TypeMemorystore,
				Name: "ha-redis",
				Config: map[string]interface{}{
					"name":           "ha-redis",
					"memory_size_gb": float64(5),
					"tier":           "STANDARD_HA",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				hasHAWarning := false
				for _, w := range result.Warnings {
					if strings.Contains(w, "Standard HA tier") {
						hasHAWarning = true
						break
					}
				}
				if !hasHAWarning {
					t.Error("Expected warning about Standard HA tier")
				}
			},
		},
		{
			name: "Memorystore with default settings",
			res: &resource.AWSResource{
				ID:   "my-project/us-central1/default-redis",
				Type: resource.TypeMemorystore,
				Name: "default-redis",
				Config: map[string]interface{}{
					"name": "default-redis",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should use default values
				if result.DockerService.Image == "" {
					t.Error("Expected Docker image to be set")
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
