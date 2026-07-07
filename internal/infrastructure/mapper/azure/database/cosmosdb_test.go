package database

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestNewCosmosDBMapper(t *testing.T) {
	m := NewCosmosDBMapper()
	if m == nil {
		t.Fatal("NewCosmosDBMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeCosmosDB {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeCosmosDB)
	}
}

func TestCosmosDBConformanceManagedAToZ(t *testing.T) {
	result, err := NewCosmosDBMapper().Map(context.Background(), managedCosmosDBFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Cosmos DB migration", result.ManualSteps)
	}
	if result.DockerService.Image != "mongo:7" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA MongoDB target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/cosmosdb/app-change.env", "config/cosmosdb/generated-client.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/cosmosdb/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_COSMOSDB_ACCOUNT=orders-cosmos", "MONGODB_URI='mongodb://admin:changeme@mongodb:27017/cosmosdb"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"migrate_cosmosdb.sh", "validate_cosmosdb.sh", "backup_cosmosdb.sh", "cutover_cosmosdb.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"dump-restore-cosmosdb":              domainrunbook.StepTypeCommand,
		"validate-cosmosdb-migration":        domainrunbook.StepTypeCommand,
		"backup-cosmosdb-target":             domainrunbook.StepTypeCommand,
		"cutover-cosmosdb-clients":           domainrunbook.StepTypeAPICall,
		"rollback-cosmosdb-source-authority": domainrunbook.StepTypeRollback,
	} {
		if !hasCosmosDBRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func managedCosmosDBFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "/subscriptions/demo/resourceGroups/rg/providers/Microsoft.DocumentDB/databaseAccounts/orders-cosmos",
		Type: resource.TypeCosmosDB,
		Name: "orders-cosmos",
		Config: map[string]interface{}{
			"name": "orders-cosmos",
			"kind": "MongoDB",
		},
	}
}

func hasCosmosDBRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}

func TestCosmosDBMapper_ResourceType(t *testing.T) {
	m := NewCosmosDBMapper()
	got := m.ResourceType()
	want := resource.TypeCosmosDB

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestCosmosDBMapper_Dependencies(t *testing.T) {
	m := NewCosmosDBMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestCosmosDBMapper_Validate(t *testing.T) {
	m := NewCosmosDBMapper()

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
				Type: resource.TypeCosmosDB,
				Name: "test-cosmosdb",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeCosmosDB,
				Name: "test-cosmosdb",
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

func TestCosmosDBMapper_Map(t *testing.T) {
	m := NewCosmosDBMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "SQL API (default to MongoDB)",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.DocumentDB/databaseAccounts/my-cosmos",
				Type: resource.TypeCosmosDB,
				Name: "my-cosmos",
				Config: map[string]interface{}{
					"name": "my-cosmos-account",
					"kind": "GlobalDocumentDB",
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
			name: "MongoDB API",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.DocumentDB/databaseAccounts/my-cosmos",
				Type: resource.TypeCosmosDB,
				Name: "my-cosmos",
				Config: map[string]interface{}{
					"name": "my-cosmos-account",
					"kind": "MongoDB",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if result.DockerService.Image != "mongo:7" {
					t.Errorf("Expected mongo:7 image, got %s", result.DockerService.Image)
				}
			},
		},
		{
			name: "Cassandra API",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.DocumentDB/databaseAccounts/my-cosmos",
				Type: resource.TypeCosmosDB,
				Name: "my-cosmos",
				Config: map[string]interface{}{
					"name": "my-cosmos-account",
					"capabilities": []interface{}{
						map[string]interface{}{
							"name": "EnableCassandra",
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if result.DockerService.Image != "cassandra:4.1" {
					t.Errorf("Expected cassandra:4.1 image, got %s", result.DockerService.Image)
				}
			},
		},
		{
			name: "Gremlin API",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.DocumentDB/databaseAccounts/my-cosmos",
				Type: resource.TypeCosmosDB,
				Name: "my-cosmos",
				Config: map[string]interface{}{
					"name": "my-cosmos-account",
					"capabilities": []interface{}{
						map[string]interface{}{
							"name": "EnableGremlin",
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if result.DockerService.Image != "janusgraph/janusgraph:latest" {
					t.Errorf("Expected janusgraph image, got %s", result.DockerService.Image)
				}
			},
		},
		{
			name: "Table API",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.DocumentDB/databaseAccounts/my-cosmos",
				Type: resource.TypeCosmosDB,
				Name: "my-cosmos",
				Config: map[string]interface{}{
					"name": "my-cosmos-account",
					"capabilities": []interface{}{
						map[string]interface{}{
							"name": "EnableTable",
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if result.DockerService.Image != "mcr.microsoft.com/azure-storage/azurite" {
					t.Errorf("Expected azurite image, got %s", result.DockerService.Image)
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
