package database

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestCloudSQLConformanceManagedAToZ(t *testing.T) {
	result, err := NewCloudSQLMapper().Map(context.Background(), managedCloudSQLFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Cloud SQL migration", result.ManualSteps)
	}
	if result.DockerService.Image != "postgres:15-alpine" || (result.DockerService.Deploy != nil && result.DockerService.Deploy.Replicas > 1) {
		t.Fatalf("service must not run duplicate database writers: %#v", result.DockerService)
	}
	for _, file := range []string{"config/cloud-sql/app-change.env", "config/cloud-sql/database-report.yaml"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/cloud-sql/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_CLOUD_SQL_INSTANCE=orders-db", "TARGET_DATABASE_URL=postgres://postgres:changeme@postgres:5432/cloudsql_db"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"migrate_cloudsql.sh", "backup_cloud_sql.sh", "validate_cloud_sql.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"discover-cloud-sql-instance": domainrunbook.StepTypeCommand,
		"provision-cloud-sql-target":  domainrunbook.StepTypeCommand,
		"migrate-cloud-sql-data":      domainrunbook.StepTypeCommand,
		"validate-cloud-sql-target":   domainrunbook.StepTypeCommand,
		"backup-cloud-sql-target":     domainrunbook.StepTypeCommand,
		"cutover-cloud-sql-client":    domainrunbook.StepTypeAPICall,
		"rollback-cloud-sql-source":   domainrunbook.StepTypeRollback,
	} {
		if !hasCloudSQLRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewCloudSQLMapper(t *testing.T) {
	m := NewCloudSQLMapper()
	if m == nil {
		t.Fatal("NewCloudSQLMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeCloudSQL {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeCloudSQL)
	}
}

func managedCloudSQLFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "projects/demo/instances/orders-db",
		Type: resource.TypeCloudSQL,
		Name: "orders-db",
		Config: map[string]interface{}{
			"name":             "orders-db",
			"database_version": "POSTGRES_15",
			"region":           "europe-west1",
		},
	}
}

func hasCloudSQLRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}

func TestCloudSQLMapper_ResourceType(t *testing.T) {
	m := NewCloudSQLMapper()
	got := m.ResourceType()
	want := resource.TypeCloudSQL

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestCloudSQLMapper_Dependencies(t *testing.T) {
	m := NewCloudSQLMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestCloudSQLMapper_Validate(t *testing.T) {
	m := NewCloudSQLMapper()

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
				Type: resource.TypeCloudSQL,
				Name: "test-db",
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

func TestCloudSQLMapper_Map(t *testing.T) {
	m := NewCloudSQLMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "PostgreSQL Cloud SQL instance",
			res: &resource.AWSResource{
				ID:   "my-project:us-central1:my-postgres",
				Type: resource.TypeCloudSQL,
				Name: "my-postgres",
				Config: map[string]interface{}{
					"name":             "my-postgres",
					"database_version": "POSTGRES_15",
					"region":           "us-central1",
					"settings": map[string]interface{}{
						"tier": "db-f1-micro",
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
			name: "MySQL Cloud SQL instance",
			res: &resource.AWSResource{
				ID:   "my-project:us-central1:my-mysql",
				Type: resource.TypeCloudSQL,
				Name: "my-mysql",
				Config: map[string]interface{}{
					"name":             "my-mysql",
					"database_version": "MYSQL_8_0",
					"region":           "us-central1",
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
