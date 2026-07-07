package database

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestNewSpannerMapper(t *testing.T) {
	m := NewSpannerMapper()
	if m == nil {
		t.Fatal("NewSpannerMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeSpanner {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeSpanner)
	}
}

func TestSpannerConformanceManagedAToZ(t *testing.T) {
	result, err := NewSpannerMapper().Map(context.Background(), managedSpannerFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Spanner migration", result.ManualSteps)
	}
	if result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("CockroachDB service is not HA: %#v", result.DockerService)
	}
	for _, file := range []string{"config/cockroachdb/cluster-config.yml", "config/spanner/app-change.env"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/spanner/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_SPANNER_INSTANCE=orders-spanner", "TARGET_DRIVER=postgres"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"convert_schema.sh", "migrate_spanner.sh", "validate_spanner_cockroach.sh", "backup_spanner_cockroach.sh", "cutover_spanner_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"convert-spanner-schema":            domainrunbook.StepTypeCommand,
		"migrate-spanner-data":              domainrunbook.StepTypeCommand,
		"validate-spanner-cockroach":        domainrunbook.StepTypeCommand,
		"backup-spanner-cockroach":          domainrunbook.StepTypeCommand,
		"cutover-spanner-clients":           domainrunbook.StepTypeAPICall,
		"rollback-spanner-source-authority": domainrunbook.StepTypeRollback,
	} {
		if !hasSpannerRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func managedSpannerFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "projects/demo/instances/orders-spanner",
		Type: resource.TypeSpanner,
		Name: "orders-spanner",
		Config: map[string]interface{}{
			"name":         "orders-spanner",
			"display_name": "Orders Spanner",
			"num_nodes":    float64(3),
		},
	}
}

func hasSpannerRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}

func TestSpannerMapper_ResourceType(t *testing.T) {
	m := NewSpannerMapper()
	got := m.ResourceType()
	want := resource.TypeSpanner

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestSpannerMapper_Dependencies(t *testing.T) {
	m := NewSpannerMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestSpannerMapper_Validate(t *testing.T) {
	m := NewSpannerMapper()

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
				Type: resource.TypeSpanner,
				Name: "test-spanner",
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

func TestSpannerMapper_Map(t *testing.T) {
	m := NewSpannerMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Spanner instance",
			res: &resource.AWSResource{
				ID:   "my-project/my-spanner",
				Type: resource.TypeSpanner,
				Name: "my-spanner",
				Config: map[string]interface{}{
					"name": "my-spanner",
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
				if result.DockerService.Image != "cockroachdb/cockroach:v23.2.0" {
					t.Errorf("Expected CockroachDB image, got %s", result.DockerService.Image)
				}
			},
		},
		{
			name: "Spanner instance with config",
			res: &resource.AWSResource{
				ID:   "my-project/production-spanner",
				Type: resource.TypeSpanner,
				Name: "production-spanner",
				Config: map[string]interface{}{
					"name":         "production-spanner",
					"display_name": "Production Spanner Instance",
					"num_nodes":    float64(3),
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warnings about Spanner specifics
				if len(result.Warnings) == 0 {
					t.Error("Expected warnings about Spanner to CockroachDB migration")
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
