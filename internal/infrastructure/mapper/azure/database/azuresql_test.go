package database

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestNewAzureSQLMapper(t *testing.T) {
	m := NewAzureSQLMapper()
	if m == nil {
		t.Fatal("NewAzureSQLMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeAzureSQL {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeAzureSQL)
	}
}

func TestAzureSQLMapper_ResourceType(t *testing.T) {
	m := NewAzureSQLMapper()
	got := m.ResourceType()
	want := resource.TypeAzureSQL

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestAzureSQLConformanceManagedAToZ(t *testing.T) {
	result, err := NewAzureSQLMapper().Map(context.Background(), managedAzureSQLFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Azure SQL migration", result.ManualSteps)
	}
	if result.DockerService.Image != "mcr.microsoft.com/mssql/server:2022-latest" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA SQL target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/sql/credentials.env", "config/sql/app-change.env", "config/sql/replication.env", "config/sql/generated-client.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/sql/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_AZURE_SQL=checkoutdb", "DATABASE_URL=sqlserver://sa:"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"init_database.sh", "migrate_azuresql.sh", "validate_database.sh", "backup_database.sh", "cutover_database.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"generate-sql-credentials":      domainrunbook.StepTypeCommand,
		"dump-restore-sql":              domainrunbook.StepTypeCommand,
		"validate-sql-migration":        domainrunbook.StepTypeCommand,
		"backup-sql-target":             domainrunbook.StepTypeCommand,
		"validate-app-sql-connection":   domainrunbook.StepTypeCommand,
		"rollback-sql-source-authority": domainrunbook.StepTypeRollback,
	} {
		if !hasAzureSQLRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func managedAzureSQLFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "/subscriptions/demo/resourceGroups/rg/providers/Microsoft.Sql/servers/sql/databases/checkoutdb",
		Type: resource.TypeAzureSQL,
		Name: "checkoutdb",
		Config: map[string]interface{}{
			"name":        "checkoutdb",
			"server_name": "checkout-sql",
			"sku_name":    "S0",
		},
	}
}

func hasAzureSQLRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}

func TestAzureSQLMapper_Dependencies(t *testing.T) {
	m := NewAzureSQLMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestAzureSQLMapper_Validate(t *testing.T) {
	m := NewAzureSQLMapper()

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
				Type: resource.TypeAzureVM,
				Name: "test",
			},
			wantErr: true,
		},
		{
			name: "valid resource",
			res: &resource.AWSResource{
				ID:   "test-id",
				Type: resource.TypeAzureSQL,
				Name: "test-sql",
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

func TestAzureSQLMapper_Map(t *testing.T) {
	m := NewAzureSQLMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Azure SQL Database",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Sql/servers/myserver/databases/mydb",
				Type: resource.TypeAzureSQL,
				Name: "mydb",
				Config: map[string]interface{}{
					"name":           "mydb",
					"server_name":    "myserver",
					"sku_name":       "S0",
					"max_size_gb":    float64(250),
					"zone_redundant": false,
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
