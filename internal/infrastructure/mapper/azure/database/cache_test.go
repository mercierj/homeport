package database

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestNewCacheMapper(t *testing.T) {
	m := NewCacheMapper()
	if m == nil {
		t.Fatal("NewCacheMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeAzureCache {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeAzureCache)
	}
}

func TestCacheMapper_ResourceType(t *testing.T) {
	m := NewCacheMapper()
	got := m.ResourceType()
	want := resource.TypeAzureCache

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestCacheConformanceManagedAToZ(t *testing.T) {
	result, err := NewCacheMapper().Map(context.Background(), managedCacheFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Azure Cache migration", result.ManualSteps)
	}
	if result.DockerService.Image != "redis:7-alpine" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Redis target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/redis/redis.conf", "config/redis/app-change.env", "config/redis/tls.env", "config/redis/generated-client.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/redis/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_AZURE_CACHE=checkout-cache", "REDIS_HOST=redis"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"migrate_azure_cache.sh", "validate_redis.sh", "backup_redis.sh", "cutover_redis_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"configure-redis-tls":             domainrunbook.StepTypeCommand,
		"generate-redis-auth":             domainrunbook.StepTypeCommand,
		"sync-redis-data":                 domainrunbook.StepTypeCommand,
		"validate-redis-migration":        domainrunbook.StepTypeCommand,
		"validate-redis-failover":         domainrunbook.StepTypeCommand,
		"backup-redis-target":             domainrunbook.StepTypeCommand,
		"cutover-redis-clients":           domainrunbook.StepTypeAPICall,
		"rollback-redis-source-authority": domainrunbook.StepTypeRollback,
	} {
		if !hasCacheRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func managedCacheFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "/subscriptions/demo/resourceGroups/rg/providers/Microsoft.Cache/Redis/checkout-cache",
		Type: resource.TypeAzureCache,
		Name: "checkout-cache",
		Config: map[string]interface{}{
			"name":                "checkout-cache",
			"capacity":            float64(1),
			"sku_name":            "Premium",
			"family":              "P",
			"redis_version":       "7",
			"enable_non_ssl_port": false,
		},
	}
}

func hasCacheRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}

func TestCacheMapper_Dependencies(t *testing.T) {
	m := NewCacheMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestCacheMapper_Validate(t *testing.T) {
	m := NewCacheMapper()

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
				Type: resource.TypeAzureCache,
				Name: "test-cache",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeAzureCache,
				Name: "test-cache",
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

func TestCacheMapper_Map(t *testing.T) {
	m := NewCacheMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Redis cache",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Cache/Redis/my-cache",
				Type: resource.TypeAzureCache,
				Name: "my-cache",
				Config: map[string]interface{}{
					"name":          "my-redis-cache",
					"capacity":      float64(2),
					"sku_name":      "Standard",
					"family":        "C",
					"redis_version": "6",
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
			name: "Premium tier cache",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Cache/Redis/premium-cache",
				Type: resource.TypeAzureCache,
				Name: "premium-cache",
				Config: map[string]interface{}{
					"name":     "premium-redis-cache",
					"capacity": float64(1),
					"sku_name": "Premium",
					"family":   "P",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if !result.HasWarnings() {
					t.Error("Expected warnings for Premium tier")
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
