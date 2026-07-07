package database

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestFirestoreConformanceManagedAToZ(t *testing.T) {
	result, err := NewFirestoreMapper().Map(context.Background(), managedFirestoreFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Firestore migration", result.ManualSteps)
	}
	if result.DockerService.Image != "mongo:7" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA MongoDB target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/firestore/app-change.env", "config/firestore/migration.env", "config/mongodb/init-firestore.js"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/firestore/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_FIRESTORE_DATABASE=orders-db", "TARGET_MONGODB_URI=mongodb://admin:changeme@mongodb:27017/firestore_db?authSource=admin"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"migrate_firestore.sh", "export_firestore_data.sh", "transform_firestore_export.sh", "import_firestore_mongodb.sh", "validate_firestore_mongodb.sh", "backup_firestore_config.sh", "cutover_firestore_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-firestore-data":      domainrunbook.StepTypeCommand,
		"provision-mongodb-target":   domainrunbook.StepTypeCommand,
		"transform-firestore-export": domainrunbook.StepTypeCommand,
		"import-firestore-mongodb":   domainrunbook.StepTypeCommand,
		"validate-firestore-mongodb": domainrunbook.StepTypeCommand,
		"backup-firestore-config":    domainrunbook.StepTypeCommand,
		"cutover-firestore-clients":  domainrunbook.StepTypeAPICall,
		"rollback-firestore-source":  domainrunbook.StepTypeRollback,
	} {
		if !hasFirestoreRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewFirestoreMapper(t *testing.T) {
	m := NewFirestoreMapper()
	if m == nil {
		t.Fatal("NewFirestoreMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeFirestore {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeFirestore)
	}
}

func managedFirestoreFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "projects/demo/databases/orders-db",
		Type: resource.TypeFirestore,
		Name: "orders-db",
		Config: map[string]interface{}{
			"name":    "orders-db",
			"project": "demo",
		},
	}
}

func hasFirestoreRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}

func TestFirestoreMapper_ResourceType(t *testing.T) {
	m := NewFirestoreMapper()
	got := m.ResourceType()
	want := resource.TypeFirestore

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestFirestoreMapper_Dependencies(t *testing.T) {
	m := NewFirestoreMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestFirestoreMapper_Validate(t *testing.T) {
	m := NewFirestoreMapper()

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
				Type: resource.TypeFirestore,
				Name: "test-firestore",
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

func TestFirestoreMapper_Map(t *testing.T) {
	m := NewFirestoreMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Firestore database",
			res: &resource.AWSResource{
				ID:   "my-project/default",
				Type: resource.TypeFirestore,
				Name: "default",
				Config: map[string]interface{}{
					"name": "default",
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
				if result.DockerService.Image != "mongo:7" {
					t.Errorf("Expected MongoDB image, got %s", result.DockerService.Image)
				}
			},
		},
		{
			name: "Firestore with custom name",
			res: &resource.AWSResource{
				ID:   "my-project/app-database",
				Type: resource.TypeFirestore,
				Name: "app-database",
				Config: map[string]interface{}{
					"name": "app-database",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Check MongoDB environment is set
				if result.DockerService.Environment["MONGO_INITDB_ROOT_USERNAME"] != "admin" {
					t.Error("Expected MONGO_INITDB_ROOT_USERNAME to be admin")
				}
			},
		},
		{
			name: "Firestore with warnings",
			res: &resource.AWSResource{
				ID:   "my-project/users-db",
				Type: resource.TypeFirestore,
				Name: "users-db",
				Config: map[string]interface{}{
					"name": "users-db",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warnings about Firestore to MongoDB migration
				if len(result.Warnings) == 0 {
					t.Error("Expected warnings about Firestore to MongoDB migration")
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
