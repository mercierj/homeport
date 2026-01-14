package azure_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeport/homeport/internal/domain/parser"
	"github.com/homeport/homeport/internal/domain/resource"
	azureparser "github.com/homeport/homeport/internal/infrastructure/parser/azure"
)

// TestTFStateParserIntegration_BasicAzureResources tests parsing a TFState file with basic Azure resources.
func TestTFStateParserIntegration_BasicAzureResources(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tfstate-azure-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tfstateContent := `{
		"version": 4,
		"terraform_version": "1.5.0",
		"serial": 1,
		"lineage": "azure-test-lineage",
		"outputs": {},
		"resources": [
			{
				"mode": "managed",
				"type": "azurerm_storage_account",
				"name": "main",
				"provider": "provider[\"registry.terraform.io/hashicorp/azurerm\"]",
				"instances": [
					{
						"schema_version": 3,
						"attributes": {
							"id": "/subscriptions/xxx/resourceGroups/rg-test/providers/Microsoft.Storage/storageAccounts/mystorageacct",
							"name": "mystorageacct",
							"location": "eastus",
							"account_tier": "Standard",
							"account_replication_type": "LRS",
							"tags": {
								"environment": "test",
								"project": "homeport"
							}
						}
					}
				]
			}
		]
	}`

	statePath := filepath.Join(tmpDir, "terraform.tfstate")
	if err := os.WriteFile(statePath, []byte(tfstateContent), 0644); err != nil {
		t.Fatalf("failed to write tfstate: %v", err)
	}

	p := azureparser.NewTFStateParser()

	// Verify provider
	if p.Provider() != resource.ProviderAzure {
		t.Errorf("expected Azure provider, got %s", p.Provider())
	}

	// Verify format
	formats := p.SupportedFormats()
	if len(formats) != 1 || formats[0] != parser.FormatTFState {
		t.Errorf("expected TFState format, got %v", formats)
	}

	// Test validation
	if err := p.Validate(statePath); err != nil {
		t.Errorf("validation failed: %v", err)
	}

	// Test auto-detection
	canHandle, confidence := p.AutoDetect(statePath)
	if !canHandle {
		t.Error("expected parser to handle tfstate file")
	}
	if confidence < 0.8 {
		t.Errorf("expected high confidence, got %f", confidence)
	}

	// Test parsing
	ctx := context.Background()
	opts := parser.NewParseOptions()
	infra, err := p.Parse(ctx, statePath, opts)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	if len(infra.Resources) != 1 {
		t.Errorf("expected 1 resource, got %d", len(infra.Resources))
	}

	// Verify metadata
	if infra.Metadata["terraform_version"] != "1.5.0" {
		t.Errorf("expected terraform_version 1.5.0, got %s", infra.Metadata["terraform_version"])
	}
}

// TestTFStateParserIntegration_AzurermResourceTypes tests mapping of azurerm_* resource types.
func TestTFStateParserIntegration_AzurermResourceTypes(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tfstate-azurerm-types-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tfstateContent := `{
		"version": 4,
		"terraform_version": "1.5.0",
		"serial": 1,
		"lineage": "azure-types-test",
		"resources": [
			{
				"mode": "managed",
				"type": "azurerm_linux_virtual_machine",
				"name": "vm",
				"provider": "provider[\"registry.terraform.io/hashicorp/azurerm\"]",
				"instances": [
					{
						"schema_version": 0,
						"attributes": {
							"id": "/subscriptions/xxx/resourceGroups/rg-test/providers/Microsoft.Compute/virtualMachines/myvm",
							"name": "myvm",
							"location": "eastus",
							"size": "Standard_DS2_v2"
						}
					}
				]
			},
			{
				"mode": "managed",
				"type": "azurerm_mssql_database",
				"name": "db",
				"provider": "provider[\"registry.terraform.io/hashicorp/azurerm\"]",
				"instances": [
					{
						"schema_version": 1,
						"attributes": {
							"id": "/subscriptions/xxx/resourceGroups/rg-test/providers/Microsoft.Sql/servers/myserver/databases/mydb",
							"name": "mydb",
							"location": "eastus"
						}
					}
				]
			},
			{
				"mode": "managed",
				"type": "azurerm_redis_cache",
				"name": "cache",
				"provider": "provider[\"registry.terraform.io/hashicorp/azurerm\"]",
				"instances": [
					{
						"schema_version": 1,
						"attributes": {
							"id": "/subscriptions/xxx/resourceGroups/rg-test/providers/Microsoft.Cache/Redis/myredis",
							"name": "myredis",
							"location": "eastus",
							"capacity": 1,
							"family": "C",
							"sku_name": "Standard"
						}
					}
				]
			},
			{
				"mode": "managed",
				"type": "azurerm_key_vault",
				"name": "vault",
				"provider": "provider[\"registry.terraform.io/hashicorp/azurerm\"]",
				"instances": [
					{
						"schema_version": 2,
						"attributes": {
							"id": "/subscriptions/xxx/resourceGroups/rg-test/providers/Microsoft.KeyVault/vaults/mykeyvault",
							"name": "mykeyvault",
							"location": "eastus",
							"sku_name": "standard"
						}
					}
				]
			},
			{
				"mode": "managed",
				"type": "azurerm_servicebus_namespace",
				"name": "sb",
				"provider": "provider[\"registry.terraform.io/hashicorp/azurerm\"]",
				"instances": [
					{
						"schema_version": 1,
						"attributes": {
							"id": "/subscriptions/xxx/resourceGroups/rg-test/providers/Microsoft.ServiceBus/namespaces/myservicebus",
							"name": "myservicebus",
							"location": "eastus",
							"sku": "Standard"
						}
					}
				]
			},
			{
				"mode": "managed",
				"type": "azurerm_lb",
				"name": "lb",
				"provider": "provider[\"registry.terraform.io/hashicorp/azurerm\"]",
				"instances": [
					{
						"schema_version": 0,
						"attributes": {
							"id": "/subscriptions/xxx/resourceGroups/rg-test/providers/Microsoft.Network/loadBalancers/mylb",
							"name": "mylb",
							"location": "eastus"
						}
					}
				]
			}
		]
	}`

	statePath := filepath.Join(tmpDir, "terraform.tfstate")
	if err := os.WriteFile(statePath, []byte(tfstateContent), 0644); err != nil {
		t.Fatalf("failed to write tfstate: %v", err)
	}

	p := azureparser.NewTFStateParser()
	ctx := context.Background()
	opts := parser.NewParseOptions()
	infra, err := p.Parse(ctx, statePath, opts)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	if len(infra.Resources) != 6 {
		t.Errorf("expected 6 resources, got %d", len(infra.Resources))
	}

	// Verify each resource type is correctly mapped
	expectedMappings := map[string]resource.Type{
		"azurerm_linux_virtual_machine.vm": resource.TypeAzureVM,
		"azurerm_mssql_database.db":        resource.TypeAzureSQL,
		"azurerm_redis_cache.cache":        resource.TypeAzureCache,
		"azurerm_key_vault.vault":          resource.TypeKeyVault,
		"azurerm_servicebus_namespace.sb":  resource.TypeServiceBus,
		"azurerm_lb.lb":                    resource.TypeAzureLB,
	}

	for id, expectedType := range expectedMappings {
		res, ok := infra.Resources[id]
		if !ok {
			t.Errorf("resource %s not found", id)
			continue
		}
		if res.Type != expectedType {
			t.Errorf("resource %s: expected type %s, got %s", id, expectedType, res.Type)
		}
	}
}

// TestTFStateParserIntegration_ComputeResources tests parsing of Azure compute resources.
func TestTFStateParserIntegration_ComputeResources(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tfstate-compute-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tfstateContent := `{
		"version": 4,
		"terraform_version": "1.5.0",
		"serial": 1,
		"lineage": "compute-test",
		"resources": [
			{
				"mode": "managed",
				"type": "azurerm_linux_virtual_machine",
				"name": "linux_vm",
				"provider": "provider[\"registry.terraform.io/hashicorp/azurerm\"]",
				"instances": [
					{
						"schema_version": 0,
						"attributes": {
							"id": "/subscriptions/xxx/resourceGroups/rg-test/providers/Microsoft.Compute/virtualMachines/linuxvm",
							"name": "linuxvm",
							"location": "eastus",
							"size": "Standard_DS2_v2",
							"tags": {
								"os": "linux"
							}
						}
					}
				]
			},
			{
				"mode": "managed",
				"type": "azurerm_windows_virtual_machine",
				"name": "windows_vm",
				"provider": "provider[\"registry.terraform.io/hashicorp/azurerm\"]",
				"instances": [
					{
						"schema_version": 0,
						"attributes": {
							"id": "/subscriptions/xxx/resourceGroups/rg-test/providers/Microsoft.Compute/virtualMachines/windowsvm",
							"name": "windowsvm",
							"location": "westus",
							"size": "Standard_D4s_v3",
							"tags": {
								"os": "windows"
							}
						}
					}
				]
			},
			{
				"mode": "managed",
				"type": "azurerm_kubernetes_cluster",
				"name": "aks",
				"provider": "provider[\"registry.terraform.io/hashicorp/azurerm\"]",
				"instances": [
					{
						"schema_version": 2,
						"attributes": {
							"id": "/subscriptions/xxx/resourceGroups/rg-test/providers/Microsoft.ContainerService/managedClusters/myaks",
							"name": "myaks",
							"location": "eastus",
							"kubernetes_version": "1.27.0"
						}
					}
				]
			},
			{
				"mode": "managed",
				"type": "azurerm_function_app",
				"name": "func",
				"provider": "provider[\"registry.terraform.io/hashicorp/azurerm\"]",
				"instances": [
					{
						"schema_version": 1,
						"attributes": {
							"id": "/subscriptions/xxx/resourceGroups/rg-test/providers/Microsoft.Web/sites/myfunc",
							"name": "myfunc",
							"location": "eastus"
						}
					}
				]
			}
		]
	}`

	statePath := filepath.Join(tmpDir, "terraform.tfstate")
	if err := os.WriteFile(statePath, []byte(tfstateContent), 0644); err != nil {
		t.Fatalf("failed to write tfstate: %v", err)
	}

	p := azureparser.NewTFStateParser()
	ctx := context.Background()
	opts := parser.NewParseOptions()
	infra, err := p.Parse(ctx, statePath, opts)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	// Verify Linux VM
	linuxVM, ok := infra.Resources["azurerm_linux_virtual_machine.linux_vm"]
	if !ok {
		t.Fatal("expected Linux VM resource")
	}
	if linuxVM.Type != resource.TypeAzureVM {
		t.Errorf("expected TypeAzureVM, got %s", linuxVM.Type)
	}
	if linuxVM.Tags["os"] != "linux" {
		t.Errorf("expected os tag 'linux', got %s", linuxVM.Tags["os"])
	}

	// Verify Windows VM
	windowsVM, ok := infra.Resources["azurerm_windows_virtual_machine.windows_vm"]
	if !ok {
		t.Fatal("expected Windows VM resource")
	}
	if windowsVM.Type != resource.TypeAzureVMWindows {
		t.Errorf("expected TypeAzureVMWindows, got %s", windowsVM.Type)
	}

	// Verify AKS
	aks, ok := infra.Resources["azurerm_kubernetes_cluster.aks"]
	if !ok {
		t.Fatal("expected AKS resource")
	}
	if aks.Type != resource.TypeAKS {
		t.Errorf("expected TypeAKS, got %s", aks.Type)
	}

	// Verify Function App
	funcApp, ok := infra.Resources["azurerm_function_app.func"]
	if !ok {
		t.Fatal("expected Function App resource")
	}
	if funcApp.Type != resource.TypeAzureFunction {
		t.Errorf("expected TypeAzureFunction, got %s", funcApp.Type)
	}
}

// TestTFStateParserIntegration_Dependencies tests that resource dependencies are extracted from TFState.
func TestTFStateParserIntegration_Dependencies(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tfstate-deps-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tfstateContent := `{
		"version": 4,
		"terraform_version": "1.5.0",
		"serial": 1,
		"lineage": "deps-test",
		"resources": [
			{
				"mode": "managed",
				"type": "azurerm_storage_account",
				"name": "storage",
				"provider": "provider[\"registry.terraform.io/hashicorp/azurerm\"]",
				"instances": [
					{
						"schema_version": 3,
						"attributes": {
							"id": "/subscriptions/xxx/resourceGroups/rg-test/providers/Microsoft.Storage/storageAccounts/mystorage",
							"name": "mystorage",
							"location": "eastus"
						}
					}
				]
			},
			{
				"mode": "managed",
				"type": "azurerm_linux_virtual_machine",
				"name": "vm",
				"provider": "provider[\"registry.terraform.io/hashicorp/azurerm\"]",
				"instances": [
					{
						"schema_version": 0,
						"attributes": {
							"id": "/subscriptions/xxx/resourceGroups/rg-test/providers/Microsoft.Compute/virtualMachines/myvm",
							"name": "myvm",
							"location": "eastus"
						},
						"dependencies": [
							"azurerm_storage_account.storage"
						]
					}
				]
			},
			{
				"mode": "managed",
				"type": "azurerm_mssql_database",
				"name": "db",
				"provider": "provider[\"registry.terraform.io/hashicorp/azurerm\"]",
				"instances": [
					{
						"schema_version": 1,
						"attributes": {
							"id": "/subscriptions/xxx/resourceGroups/rg-test/providers/Microsoft.Sql/servers/myserver/databases/mydb",
							"name": "mydb",
							"location": "eastus"
						},
						"dependencies": [
							"azurerm_linux_virtual_machine.vm",
							"azurerm_storage_account.storage"
						]
					}
				]
			}
		]
	}`

	statePath := filepath.Join(tmpDir, "terraform.tfstate")
	if err := os.WriteFile(statePath, []byte(tfstateContent), 0644); err != nil {
		t.Fatalf("failed to write tfstate: %v", err)
	}

	p := azureparser.NewTFStateParser()
	ctx := context.Background()
	opts := parser.NewParseOptions()
	infra, err := p.Parse(ctx, statePath, opts)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	// Check VM has dependency on storage
	vm, ok := infra.Resources["azurerm_linux_virtual_machine.vm"]
	if !ok {
		t.Fatal("expected VM resource")
	}
	if len(vm.Dependencies) != 1 {
		t.Errorf("expected 1 dependency for VM, got %d", len(vm.Dependencies))
	}

	// Check DB has dependencies
	db, ok := infra.Resources["azurerm_mssql_database.db"]
	if !ok {
		t.Fatal("expected DB resource")
	}
	if len(db.Dependencies) != 2 {
		t.Errorf("expected 2 dependencies for DB, got %d", len(db.Dependencies))
	}
}

// TestTFStateParserIntegration_DirectoryParsing tests parsing TFState from a directory.
func TestTFStateParserIntegration_DirectoryParsing(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tfstate-dir-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tfstateContent := `{
		"version": 4,
		"terraform_version": "1.5.0",
		"serial": 1,
		"lineage": "dir-test",
		"resources": [
			{
				"mode": "managed",
				"type": "azurerm_storage_account",
				"name": "main",
				"provider": "provider[\"registry.terraform.io/hashicorp/azurerm\"]",
				"instances": [
					{
						"schema_version": 3,
						"attributes": {
							"id": "/subscriptions/xxx/resourceGroups/rg-test/providers/Microsoft.Storage/storageAccounts/mystorage",
							"name": "mystorage",
							"location": "eastus"
						}
					}
				]
			}
		]
	}`

	statePath := filepath.Join(tmpDir, "terraform.tfstate")
	if err := os.WriteFile(statePath, []byte(tfstateContent), 0644); err != nil {
		t.Fatalf("failed to write tfstate: %v", err)
	}

	p := azureparser.NewTFStateParser()

	// Test directory validation
	if err := p.Validate(tmpDir); err != nil {
		t.Errorf("directory validation failed: %v", err)
	}

	// Test directory auto-detection
	canHandle, confidence := p.AutoDetect(tmpDir)
	if !canHandle {
		t.Error("expected parser to handle directory with tfstate")
	}
	if confidence < 0.8 {
		t.Errorf("expected high confidence for directory, got %f", confidence)
	}

	// Test parsing from directory
	ctx := context.Background()
	opts := parser.NewParseOptions()
	infra, err := p.Parse(ctx, tmpDir, opts)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	if len(infra.Resources) != 1 {
		t.Errorf("expected 1 resource, got %d", len(infra.Resources))
	}
}

// TestTFStateParserIntegration_MixedProviders tests that non-Azure resources are filtered out.
func TestTFStateParserIntegration_MixedProviders(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tfstate-mixed-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tfstateContent := `{
		"version": 4,
		"terraform_version": "1.5.0",
		"serial": 1,
		"lineage": "mixed-test",
		"resources": [
			{
				"mode": "managed",
				"type": "azurerm_storage_account",
				"name": "azure_storage",
				"provider": "provider[\"registry.terraform.io/hashicorp/azurerm\"]",
				"instances": [
					{
						"schema_version": 3,
						"attributes": {
							"id": "/subscriptions/xxx/resourceGroups/rg-test/providers/Microsoft.Storage/storageAccounts/mystorage",
							"name": "mystorage",
							"location": "eastus"
						}
					}
				]
			},
			{
				"mode": "managed",
				"type": "aws_s3_bucket",
				"name": "aws_bucket",
				"provider": "provider[\"registry.terraform.io/hashicorp/aws\"]",
				"instances": [
					{
						"schema_version": 0,
						"attributes": {
							"id": "my-aws-bucket",
							"bucket": "my-aws-bucket",
							"region": "us-east-1"
						}
					}
				]
			},
			{
				"mode": "managed",
				"type": "google_storage_bucket",
				"name": "gcp_bucket",
				"provider": "provider[\"registry.terraform.io/hashicorp/google\"]",
				"instances": [
					{
						"schema_version": 0,
						"attributes": {
							"id": "my-gcp-bucket",
							"name": "my-gcp-bucket",
							"location": "US"
						}
					}
				]
			},
			{
				"mode": "managed",
				"type": "azurerm_linux_virtual_machine",
				"name": "azure_vm",
				"provider": "provider[\"registry.terraform.io/hashicorp/azurerm\"]",
				"instances": [
					{
						"schema_version": 0,
						"attributes": {
							"id": "/subscriptions/xxx/resourceGroups/rg-test/providers/Microsoft.Compute/virtualMachines/myvm",
							"name": "myvm",
							"location": "eastus"
						}
					}
				]
			}
		]
	}`

	statePath := filepath.Join(tmpDir, "terraform.tfstate")
	if err := os.WriteFile(statePath, []byte(tfstateContent), 0644); err != nil {
		t.Fatalf("failed to write tfstate: %v", err)
	}

	p := azureparser.NewTFStateParser()
	ctx := context.Background()
	opts := parser.NewParseOptions()
	infra, err := p.Parse(ctx, statePath, opts)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	// Should only include Azure resources
	if len(infra.Resources) != 2 {
		t.Errorf("expected 2 Azure resources, got %d", len(infra.Resources))
	}

	// Verify only Azure resources are present
	for id := range infra.Resources {
		if id == "aws_s3_bucket.aws_bucket" || id == "google_storage_bucket.gcp_bucket" {
			t.Errorf("non-Azure resource %s should have been filtered out", id)
		}
	}
}

// TestTFStateParserIntegration_IndexedResources tests parsing resources with index keys (count/for_each).
func TestTFStateParserIntegration_IndexedResources(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tfstate-indexed-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tfstateContent := `{
		"version": 4,
		"terraform_version": "1.5.0",
		"serial": 1,
		"lineage": "indexed-test",
		"resources": [
			{
				"mode": "managed",
				"type": "azurerm_storage_account",
				"name": "storage",
				"provider": "provider[\"registry.terraform.io/hashicorp/azurerm\"]",
				"instances": [
					{
						"index_key": 0,
						"schema_version": 3,
						"attributes": {
							"id": "/subscriptions/xxx/resourceGroups/rg-test/providers/Microsoft.Storage/storageAccounts/storage0",
							"name": "storage0",
							"location": "eastus"
						}
					},
					{
						"index_key": 1,
						"schema_version": 3,
						"attributes": {
							"id": "/subscriptions/xxx/resourceGroups/rg-test/providers/Microsoft.Storage/storageAccounts/storage1",
							"name": "storage1",
							"location": "westus"
						}
					},
					{
						"index_key": "prod",
						"schema_version": 3,
						"attributes": {
							"id": "/subscriptions/xxx/resourceGroups/rg-test/providers/Microsoft.Storage/storageAccounts/storageprod",
							"name": "storageprod",
							"location": "centralus"
						}
					}
				]
			}
		]
	}`

	statePath := filepath.Join(tmpDir, "terraform.tfstate")
	if err := os.WriteFile(statePath, []byte(tfstateContent), 0644); err != nil {
		t.Fatalf("failed to write tfstate: %v", err)
	}

	p := azureparser.NewTFStateParser()
	ctx := context.Background()
	opts := parser.NewParseOptions()
	infra, err := p.Parse(ctx, statePath, opts)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	// Should have 3 resources (one for each instance)
	if len(infra.Resources) != 3 {
		t.Errorf("expected 3 indexed resources, got %d", len(infra.Resources))
	}

	// Verify indexed resources have correct names
	for _, res := range infra.Resources {
		if res.Name != "storage0" && res.Name != "storage1" && res.Name != "storageprod" {
			t.Errorf("unexpected resource name: %s", res.Name)
		}
	}
}

// TestTFStateParserIntegration_UnsupportedVersion tests handling of unsupported state versions.
func TestTFStateParserIntegration_UnsupportedVersion(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tfstate-version-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Version 2 is not supported
	tfstateContent := `{
		"version": 2,
		"terraform_version": "0.12.0",
		"serial": 1,
		"modules": []
	}`

	statePath := filepath.Join(tmpDir, "terraform.tfstate")
	if err := os.WriteFile(statePath, []byte(tfstateContent), 0644); err != nil {
		t.Fatalf("failed to write tfstate: %v", err)
	}

	p := azureparser.NewTFStateParser()
	ctx := context.Background()
	opts := parser.NewParseOptions()
	_, err = p.Parse(ctx, statePath, opts)
	if err == nil {
		t.Error("expected error for unsupported state version")
	}
}

// TestTFStateParserIntegration_InvalidPath tests error handling for invalid paths.
func TestTFStateParserIntegration_InvalidPath(t *testing.T) {
	p := azureparser.NewTFStateParser()

	// Test with non-existent path
	err := p.Validate("/nonexistent/path/terraform.tfstate")
	if err != parser.ErrInvalidPath {
		t.Errorf("expected ErrInvalidPath, got %v", err)
	}

	// Test auto-detect with non-existent path
	canHandle, confidence := p.AutoDetect("/nonexistent/path")
	if canHandle {
		t.Error("expected false for non-existent path")
	}
	if confidence != 0 {
		t.Errorf("expected 0 confidence, got %f", confidence)
	}
}
