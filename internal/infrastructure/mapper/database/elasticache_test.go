package database

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestNewElastiCacheMapper(t *testing.T) {
	m := NewElastiCacheMapper()
	if m == nil {
		t.Fatal("NewElastiCacheMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeElastiCache {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeElastiCache)
	}
}

func TestElastiCacheConformanceManagedAToZ(t *testing.T) {
	result, err := NewElastiCacheMapper().Map(context.Background(), managedElastiCacheFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated ElastiCache migration", result.ManualSteps)
	}
	if result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Redis target: %#v", result.DockerService.Deploy)
	}
	for _, file := range []string{"config/redis/redis.conf", "config/redis/app-change.env", "config/redis/tls.env", "config/redis/cluster.env"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/redis/app-change.env"])
	for _, want := range []string{"SOURCE_CLUSTER=orders-cache", "TARGET_ENDPOINT=redis:6379", "APP_CHANGE_MODE=generated_patch"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"migrate_redis.sh", "setup_redis_cluster.sh", "backup_redis_config.sh", "validate_redis.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"generate-redis-auth":             domainrunbook.StepTypeCommand,
		"configure-redis-tls":             domainrunbook.StepTypeCommand,
		"sync-redis-data":                 domainrunbook.StepTypeCommand,
		"validate-redis-migration":        domainrunbook.StepTypeCommand,
		"validate-redis-failover":         domainrunbook.StepTypeCommand,
		"backup-elasticache-config":       domainrunbook.StepTypeCommand,
		"cutover-elasticache-endpoint":    domainrunbook.StepTypeAPICall,
		"rollback-redis-source-authority": domainrunbook.StepTypeRollback,
	} {
		if !hasElastiCacheRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
	for _, step := range result.RunbookSteps {
		if step.Type == domainrunbook.StepTypeInput {
			t.Fatalf("manual input runbook step = %#v, want executable conformance", step)
		}
	}
}

func managedElastiCacheFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "orders-cache",
		Type: resource.TypeElastiCache,
		Name: "orders-cache",
		Config: map[string]interface{}{
			"cluster_id":                 "orders-cache",
			"engine":                     "redis",
			"engine_version":             "7.0",
			"num_cache_nodes":            float64(3),
			"cluster_mode_enabled":       true,
			"auth_token":                 "managed",
			"transit_encryption_enabled": true,
			"at_rest_encryption_enabled": true,
			"snapshot_retention_limit":   float64(7),
		},
	}
}

func hasElastiCacheRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}

func TestElastiCacheMapper_ResourceType(t *testing.T) {
	m := NewElastiCacheMapper()
	got := m.ResourceType()
	want := resource.TypeElastiCache

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestElastiCacheMapper_Dependencies(t *testing.T) {
	m := NewElastiCacheMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestElastiCacheMapper_Validate(t *testing.T) {
	m := NewElastiCacheMapper()

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
				Type: resource.TypeElastiCache,
				Name: "test-cache",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeElastiCache,
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

func TestElastiCacheMapper_Map(t *testing.T) {
	m := NewElastiCacheMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "Redis ElastiCache cluster",
			res: &resource.AWSResource{
				ID:   "my-redis-cluster",
				Type: resource.TypeElastiCache,
				Name: "my-redis-cluster",
				Config: map[string]interface{}{
					"cluster_id":     "my-redis-cluster",
					"engine":         "redis",
					"engine_version": "7.0",
					"node_type":      "cache.t3.micro",
					"port":           float64(6379),
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
			name: "Memcached ElastiCache cluster",
			res: &resource.AWSResource{
				ID:   "my-memcached-cluster",
				Type: resource.TypeElastiCache,
				Name: "my-memcached-cluster",
				Config: map[string]interface{}{
					"cluster_id":     "my-memcached-cluster",
					"engine":         "memcached",
					"engine_version": "1.6.17",
					"node_type":      "cache.t3.small",
					"port":           float64(11211),
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
			},
		},
		{
			name: "Redis with cluster mode enabled",
			res: &resource.AWSResource{
				ID:   "redis-cluster",
				Type: resource.TypeElastiCache,
				Name: "redis-cluster",
				Config: map[string]interface{}{
					"cluster_id":           "redis-cluster",
					"engine":               "redis",
					"engine_version":       "7.0",
					"num_cache_nodes":      float64(3),
					"cluster_mode_enabled": true,
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about cluster mode
				if len(result.Warnings) == 0 {
					t.Log("Expected warning about cluster mode")
				}
			},
		},
		{
			name: "Redis with persistence (snapshot)",
			res: &resource.AWSResource{
				ID:   "redis-persistent",
				Type: resource.TypeElastiCache,
				Name: "redis-persistent",
				Config: map[string]interface{}{
					"cluster_id":               "redis-persistent",
					"engine":                   "redis",
					"snapshot_retention_limit": float64(7),
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about persistence
				if len(result.Warnings) == 0 {
					t.Log("Expected warning about persistence")
				}
			},
		},
		{
			name: "unsupported engine",
			res: &resource.AWSResource{
				ID:   "unknown-engine",
				Type: resource.TypeElastiCache,
				Name: "unknown-engine",
				Config: map[string]interface{}{
					"cluster_id": "unknown-engine",
					"engine":     "unknown",
				},
			},
			wantErr: true,
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
				Type: resource.TypeS3Bucket,
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
