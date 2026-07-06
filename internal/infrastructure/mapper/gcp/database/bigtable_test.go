package database

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestBigtableConformanceManagedAToZ(t *testing.T) {
	result, err := NewBigtableMapper().Map(context.Background(), managedBigtableFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Bigtable migration", result.ManualSteps)
	}
	if result.DockerService.Image != "cassandra:4.1" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Cassandra target: %#v", result.DockerService)
	}
	for _, file := range []string{
		"config/cassandra/cassandra.yaml",
		"config/bigtable/app-change.env",
		"config/bigtable/bigtable-api-routes.yaml",
	} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/bigtable/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_BIGTABLE_INSTANCE=orders-bt", "TARGET_BIGTABLE_ENDPOINT=http://bigtable-adapter:8086"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	routes := string(result.Configs["config/bigtable/bigtable-api-routes.yaml"])
	for _, want := range []string{"google.bigtable.v2.Bigtable.ReadRows", "google.bigtable.v2.Bigtable.MutateRow", "google.bigtable.admin.v2.BigtableTableAdmin.GetTable"} {
		if !strings.Contains(routes, want) {
			t.Fatalf("API compatibility routes missing %q:\n%s", want, routes)
		}
	}
	for _, file := range []string{"export_bigtable.sh", "load_bigtable_cassandra.sh", "backup_bigtable.sh", "validate_bigtable.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"discover-bigtable-tables":       domainrunbook.StepTypeCommand,
		"provision-cassandra-bigtable":   domainrunbook.StepTypeCommand,
		"export-bigtable-tables":         domainrunbook.StepTypeCommand,
		"load-bigtable-cassandra":        domainrunbook.StepTypeCommand,
		"validate-bigtable-api-adapter":  domainrunbook.StepTypeCommand,
		"backup-bigtable-cassandra":      domainrunbook.StepTypeCommand,
		"cutover-bigtable-client-config": domainrunbook.StepTypeAPICall,
		"rollback-bigtable-source":       domainrunbook.StepTypeRollback,
	} {
		if !hasBigtableRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewBigtableMapper(t *testing.T) {
	m := NewBigtableMapper()
	if m == nil {
		t.Fatal("NewBigtableMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeBigtable {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeBigtable)
	}
}

func managedBigtableFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:     "projects/demo/instances/orders-bt",
		Type:   resource.TypeBigtable,
		Name:   "orders-bt",
		Region: "europe-west1",
		Config: map[string]interface{}{
			"name":         "orders-bt",
			"display_name": "Orders Bigtable",
		},
	}
}

func hasBigtableRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}

func TestBigtableMapper_ResourceType(t *testing.T) {
	m := NewBigtableMapper()
	got := m.ResourceType()
	want := resource.TypeBigtable

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestBigtableMapper_Dependencies(t *testing.T) {
	m := NewBigtableMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestBigtableMapper_Validate(t *testing.T) {
	m := NewBigtableMapper()

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
				Type: resource.TypeBigtable,
				Name: "test-bigtable",
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

func TestBigtableMapper_Map(t *testing.T) {
	m := NewBigtableMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Bigtable instance",
			res: &resource.AWSResource{
				ID:   "my-project/my-bigtable",
				Type: resource.TypeBigtable,
				Name: "my-bigtable",
				Config: map[string]interface{}{
					"name": "my-bigtable",
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
				if result.DockerService.Image != "cassandra:4.1" {
					t.Errorf("Expected Cassandra image, got %s", result.DockerService.Image)
				}
			},
		},
		{
			name: "Bigtable instance with display name",
			res: &resource.AWSResource{
				ID:   "my-project/prod-bigtable",
				Type: resource.TypeBigtable,
				Name: "prod-bigtable",
				Config: map[string]interface{}{
					"name":         "prod-bigtable",
					"display_name": "Production Bigtable",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Check environment variables are set
				if result.DockerService.Environment["CASSANDRA_CLUSTER_NAME"] != "homeport_cluster" {
					t.Error("Expected CASSANDRA_CLUSTER_NAME to be set")
				}
			},
		},
		{
			name: "Bigtable with warnings",
			res: &resource.AWSResource{
				ID:   "my-project/analytics-bigtable",
				Type: resource.TypeBigtable,
				Name: "analytics-bigtable",
				Config: map[string]interface{}{
					"name": "analytics-bigtable",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warnings about Bigtable to Cassandra migration
				if len(result.Warnings) == 0 {
					t.Error("Expected warnings about Bigtable to Cassandra migration")
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
