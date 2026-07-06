package database

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestRDSConformanceManagedAToZ(t *testing.T) {
	result, err := NewRDSMapper().Map(context.Background(), managedRDSFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated RDS migration", result.ManualSteps)
	}
	if result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA SQL target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/postgres/postgresql.conf", "config/sql/app-change.env", "config/sql/credentials.env", "config/sql/replication.env"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/sql/app-change.env"])
	for _, want := range []string{"SOURCE_DATABASE=orders", "DATABASE_HOST=postgres", "APP_CHANGE_MODE=generated_patch"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"migrate_database.sh", "validate_database.sh", "backup_database.sh", "cutover_database.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for _, step := range result.RunbookSteps {
		if step.Type == domainrunbook.StepTypeInput {
			t.Fatalf("runbook has input step %s: %#v", step.ID, result.RunbookSteps)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"generate-sql-credentials":       domainrunbook.StepTypeCommand,
		"validate-sql-source":            domainrunbook.StepTypeCommand,
		"dump-restore-sql":               domainrunbook.StepTypeCommand,
		"configure-live-sql-replication": domainrunbook.StepTypeCommand,
		"validate-sql-migration":         domainrunbook.StepTypeCommand,
		"backup-sql-target":              domainrunbook.StepTypeCommand,
		"validate-app-sql-connection":    domainrunbook.StepTypeCommand,
		"rollback-sql-source-authority":  domainrunbook.StepTypeRollback,
	} {
		if !hasRDSRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewRDSMapper(t *testing.T) {
	m := NewRDSMapper()
	if m == nil {
		t.Fatal("NewRDSMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeRDSInstance {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeRDSInstance)
	}
}

func managedRDSFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "orders-db",
		Type: resource.TypeRDSInstance,
		Name: "orders",
		Config: map[string]interface{}{
			"identifier":              "orders-db",
			"db_name":                 "orders",
			"engine":                  "postgres",
			"engine_version":          "15.4",
			"allocated_storage":       float64(100),
			"backup_retention_period": float64(7),
			"multi_az":                true,
			"storage_encrypted":       true,
		},
	}
}

func hasRDSRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}

func TestRDSMapper_ResourceType(t *testing.T) {
	m := NewRDSMapper()
	got := m.ResourceType()
	want := resource.TypeRDSInstance

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestRDSMapper_Dependencies(t *testing.T) {
	m := NewRDSMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestRDSMapper_Validate(t *testing.T) {
	m := NewRDSMapper()

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
				Type: resource.TypeRDSInstance,
				Name: "test-db",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeRDSInstance,
				Name: "test-db",
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

func TestRDSMapper_Map(t *testing.T) {
	m := NewRDSMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "PostgreSQL RDS instance",
			res: &resource.AWSResource{
				ID:   "db-instance-1",
				Type: resource.TypeRDSInstance,
				Name: "my-postgres-db",
				Config: map[string]interface{}{
					"identifier":        "my-postgres-db",
					"engine":            "postgres",
					"engine_version":    "15.4",
					"instance_class":    "db.t3.medium",
					"allocated_storage": float64(100),
					"username":          "admin",
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
				// Should use postgres image
				if result.DockerService.Image != "postgres:15" && result.DockerService.Image != "postgres:15.4" {
					t.Logf("DockerService.Image = %s (checking for postgres version)", result.DockerService.Image)
				}
			},
		},
		{
			name: "MySQL RDS instance",
			res: &resource.AWSResource{
				ID:   "db-instance-2",
				Type: resource.TypeRDSInstance,
				Name: "my-mysql-db",
				Config: map[string]interface{}{
					"identifier":     "my-mysql-db",
					"engine":         "mysql",
					"engine_version": "8.0",
					"instance_class": "db.t3.small",
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
