package storage

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestNewStorageAccountMapper(t *testing.T) {
	m := NewStorageAccountMapper()
	if m == nil {
		t.Fatal("NewStorageAccountMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeAzureStorageAcct {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeAzureStorageAcct)
	}
}

func TestStorageAccountMapper_ResourceType(t *testing.T) {
	m := NewStorageAccountMapper()
	got := m.ResourceType()
	want := resource.TypeAzureStorageAcct

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestStorageAccountConformanceManagedAToZ(t *testing.T) {
	result, err := NewStorageAccountMapper().Map(context.Background(), managedStorageAccountFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Azure Storage migration", result.ManualSteps)
	}
	if result.DockerService.Image != "mcr.microsoft.com/azure-storage/azurite:latest" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Azurite target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/checkoutstorage-connection.txt", "config/storage/app-change.env", "config/storage/generated-client.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/storage/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_AZURE_STORAGE=checkoutstorage", "AZURE_STORAGE_CONNECTION_STRING='DefaultEndpointsProtocol=http;AccountName=checkoutstorage"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"setup_checkoutstorage.sh", "validate_storage.sh", "backup_storage_manifest.sh", "cutover_storage_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"provision-azurite-account":         domainrunbook.StepTypeCommand,
		"validate-azure-storage-api":        domainrunbook.StepTypeCommand,
		"backup-storage-manifest":           domainrunbook.StepTypeCommand,
		"cutover-storage-clients":           domainrunbook.StepTypeAPICall,
		"rollback-storage-source-authority": domainrunbook.StepTypeRollback,
	} {
		if !hasStorageAccountRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func managedStorageAccountFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "/subscriptions/demo/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/checkoutstorage",
		Type: resource.TypeAzureStorageAcct,
		Name: "checkoutstorage",
		Config: map[string]interface{}{
			"name":                     "checkoutstorage",
			"account_tier":             "Standard",
			"account_replication_type": "LRS",
		},
	}
}

func hasStorageAccountRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}

func TestStorageAccountMapper_Dependencies(t *testing.T) {
	m := NewStorageAccountMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestStorageAccountMapper_Validate(t *testing.T) {
	m := NewStorageAccountMapper()

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
				Type: resource.TypeAzureStorageAcct,
				Name: "test-storage",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeAzureStorageAcct,
				Name: "test-storage",
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

func TestStorageAccountMapper_Map(t *testing.T) {
	m := NewStorageAccountMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Storage Account",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/mystorageacct",
				Type: resource.TypeAzureStorageAcct,
				Name: "mystorageacct",
				Config: map[string]interface{}{
					"name":                     "mystorageaccount",
					"account_tier":             "Standard",
					"account_replication_type": "LRS",
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
			name: "Storage Account with GRS replication",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/grsacct",
				Type: resource.TypeAzureStorageAcct,
				Name: "grsacct",
				Config: map[string]interface{}{
					"name":                     "grsstorageaccount",
					"account_tier":             "Standard",
					"account_replication_type": "GRS",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if !result.HasWarnings() {
					t.Error("Expected warnings for GRS replication")
				}
			},
		},
		{
			name: "Storage Account with network rules",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/secureacct",
				Type: resource.TypeAzureStorageAcct,
				Name: "secureacct",
				Config: map[string]interface{}{
					"name":                     "securestorageaccount",
					"account_tier":             "Premium",
					"account_replication_type": "LRS",
					"network_rules": map[string]interface{}{
						"default_action": "Deny",
						"ip_rules":       []interface{}{"10.0.0.0/24"},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if !result.HasWarnings() {
					t.Error("Expected warnings for network rules")
				}
			},
		},
		{
			name: "Storage Account with static website",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/webacct",
				Type: resource.TypeAzureStorageAcct,
				Name: "webacct",
				Config: map[string]interface{}{
					"name":                     "webstorageaccount",
					"account_tier":             "Standard",
					"account_replication_type": "LRS",
					"static_website": map[string]interface{}{
						"index_document":     "index.html",
						"error_404_document": "404.html",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if !result.HasWarnings() {
					t.Error("Expected warnings for static website")
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
