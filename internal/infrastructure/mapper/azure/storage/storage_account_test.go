package storage

import (
	"context"
	"testing"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
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
