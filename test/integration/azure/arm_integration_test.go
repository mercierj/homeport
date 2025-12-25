package azure_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudexit/cloudexit/internal/domain/parser"
	"github.com/cloudexit/cloudexit/internal/domain/resource"
	azureparser "github.com/cloudexit/cloudexit/internal/infrastructure/parser/azure"
)

// TestARMParserIntegration_BasicTemplate tests parsing a basic ARM template with storage account.
func TestARMParserIntegration_BasicTemplate(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "arm-integration-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	templateContent := `{
		"$schema": "https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#",
		"contentVersion": "1.0.0.0",
		"parameters": {
			"storageAccountName": {
				"type": "string",
				"defaultValue": "mystorageacct"
			}
		},
		"resources": [
			{
				"type": "Microsoft.Storage/storageAccounts",
				"apiVersion": "2021-02-01",
				"name": "mystorageaccount",
				"location": "eastus",
				"sku": {
					"name": "Standard_LRS"
				},
				"kind": "StorageV2",
				"properties": {
					"accessTier": "Hot",
					"supportsHttpsTrafficOnly": true
				},
				"tags": {
					"environment": "test",
					"project": "cloudexit"
				}
			}
		]
	}`

	templatePath := filepath.Join(tmpDir, "azuredeploy.json")
	if err := os.WriteFile(templatePath, []byte(templateContent), 0644); err != nil {
		t.Fatalf("failed to write template: %v", err)
	}

	p := azureparser.NewARMParser()

	// Verify provider
	if p.Provider() != resource.ProviderAzure {
		t.Errorf("expected Azure provider, got %s", p.Provider())
	}

	// Verify format
	formats := p.SupportedFormats()
	if len(formats) != 1 || formats[0] != parser.FormatARM {
		t.Errorf("expected ARM format, got %v", formats)
	}

	// Test validation
	if err := p.Validate(templatePath); err != nil {
		t.Errorf("validation failed: %v", err)
	}

	// Test auto-detection
	canHandle, confidence := p.AutoDetect(templatePath)
	if !canHandle {
		t.Error("expected parser to handle ARM template")
	}
	if confidence < 0.8 {
		t.Errorf("expected high confidence, got %f", confidence)
	}

	// Test parsing
	ctx := context.Background()
	opts := parser.NewParseOptions()
	infra, err := p.Parse(ctx, templatePath, opts)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	if len(infra.Resources) != 1 {
		t.Errorf("expected 1 resource, got %d", len(infra.Resources))
	}

	storage, ok := infra.Resources["mystorageaccount"]
	if !ok {
		t.Fatal("expected mystorageaccount resource")
	}

	if storage.Type != resource.TypeAzureStorageAcct {
		t.Errorf("expected storage account type, got %s", storage.Type)
	}

	if storage.Region != "eastus" {
		t.Errorf("expected eastus region, got %s", storage.Region)
	}

	if storage.Tags["environment"] != "test" {
		t.Errorf("expected environment tag 'test', got %s", storage.Tags["environment"])
	}
}

// TestARMParserIntegration_MicrosoftResourceTypes tests mapping of various Microsoft.* resource types.
func TestARMParserIntegration_MicrosoftResourceTypes(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "arm-resource-types-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	templateContent := `{
		"$schema": "https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#",
		"contentVersion": "1.0.0.0",
		"resources": [
			{
				"type": "Microsoft.Compute/virtualMachines",
				"apiVersion": "2021-03-01",
				"name": "myvm",
				"location": "westus2",
				"properties": {
					"vmSize": "Standard_DS2_v2"
				}
			},
			{
				"type": "Microsoft.Sql/servers/databases",
				"apiVersion": "2021-02-01",
				"name": "myserver/mydb",
				"location": "westus2",
				"properties": {}
			},
			{
				"type": "Microsoft.Cache/redis",
				"apiVersion": "2021-06-01",
				"name": "myredis",
				"location": "westus2",
				"properties": {}
			},
			{
				"type": "Microsoft.Network/loadBalancers",
				"apiVersion": "2021-05-01",
				"name": "mylb",
				"location": "westus2",
				"properties": {}
			},
			{
				"type": "Microsoft.KeyVault/vaults",
				"apiVersion": "2021-06-01",
				"name": "mykeyvault",
				"location": "westus2",
				"properties": {}
			},
			{
				"type": "Microsoft.ServiceBus/namespaces",
				"apiVersion": "2021-06-01",
				"name": "myservicebus",
				"location": "westus2",
				"properties": {}
			}
		]
	}`

	templatePath := filepath.Join(tmpDir, "multi-resource.json")
	if err := os.WriteFile(templatePath, []byte(templateContent), 0644); err != nil {
		t.Fatalf("failed to write template: %v", err)
	}

	p := azureparser.NewARMParser()
	ctx := context.Background()
	opts := parser.NewParseOptions()
	infra, err := p.Parse(ctx, templatePath, opts)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	tests := []struct {
		name         string
		expectedType resource.Type
	}{
		{"myvm", resource.TypeAzureVM},
		{"myserver/mydb", resource.TypeAzureSQL},
		{"myredis", resource.TypeAzureCache},
		{"mylb", resource.TypeAzureLB},
		{"mykeyvault", resource.TypeKeyVault},
		{"myservicebus", resource.TypeServiceBus},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, ok := infra.Resources[tt.name]
			if !ok {
				t.Errorf("resource %s not found", tt.name)
				return
			}
			if res.Type != tt.expectedType {
				t.Errorf("expected type %s, got %s", tt.expectedType, res.Type)
			}
		})
	}
}

// TestARMParserIntegration_DependencyResolution tests that resource dependencies are properly resolved.
func TestARMParserIntegration_DependencyResolution(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "arm-deps-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	templateContent := `{
		"$schema": "https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#",
		"contentVersion": "1.0.0.0",
		"resources": [
			{
				"type": "Microsoft.Storage/storageAccounts",
				"apiVersion": "2021-02-01",
				"name": "mystorageaccount",
				"location": "eastus",
				"properties": {}
			},
			{
				"type": "Microsoft.Compute/virtualMachines",
				"apiVersion": "2021-03-01",
				"name": "myvm",
				"location": "eastus",
				"dependsOn": [
					"mystorageaccount"
				],
				"properties": {}
			},
			{
				"type": "Microsoft.Sql/servers/databases",
				"apiVersion": "2021-02-01",
				"name": "myserver/mydb",
				"location": "eastus",
				"dependsOn": [
					"myvm",
					"mystorageaccount"
				],
				"properties": {}
			}
		]
	}`

	templatePath := filepath.Join(tmpDir, "deps.json")
	if err := os.WriteFile(templatePath, []byte(templateContent), 0644); err != nil {
		t.Fatalf("failed to write template: %v", err)
	}

	p := azureparser.NewARMParser()
	ctx := context.Background()
	opts := parser.NewParseOptions()
	infra, err := p.Parse(ctx, templatePath, opts)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	// Check VM has dependency on storage
	vm, ok := infra.Resources["myvm"]
	if !ok {
		t.Fatal("expected myvm resource")
	}
	if len(vm.Dependencies) != 1 {
		t.Errorf("expected 1 dependency for VM, got %d", len(vm.Dependencies))
	}
	if len(vm.Dependencies) > 0 && vm.Dependencies[0] != "mystorageaccount" {
		t.Errorf("expected dependency on mystorageaccount, got %s", vm.Dependencies[0])
	}

	// Check SQL DB has dependencies on VM and storage
	db, ok := infra.Resources["myserver/mydb"]
	if !ok {
		t.Fatal("expected myserver/mydb resource")
	}
	if len(db.Dependencies) != 2 {
		t.Errorf("expected 2 dependencies for DB, got %d", len(db.Dependencies))
	}
}

// TestARMParserIntegration_DirectoryParsing tests parsing multiple ARM templates from a directory.
func TestARMParserIntegration_DirectoryParsing(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "arm-dir-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create multiple template files
	template1 := `{
		"$schema": "https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#",
		"contentVersion": "1.0.0.0",
		"resources": [
			{
				"type": "Microsoft.Storage/storageAccounts",
				"apiVersion": "2021-02-01",
				"name": "storage1",
				"location": "eastus",
				"properties": {}
			}
		]
	}`

	template2 := `{
		"$schema": "https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#",
		"contentVersion": "1.0.0.0",
		"resources": [
			{
				"type": "Microsoft.Compute/virtualMachines",
				"apiVersion": "2021-03-01",
				"name": "vm1",
				"location": "westus",
				"properties": {}
			}
		]
	}`

	if err := os.WriteFile(filepath.Join(tmpDir, "storage.json"), []byte(template1), 0644); err != nil {
		t.Fatalf("failed to write template1: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "compute.json"), []byte(template2), 0644); err != nil {
		t.Fatalf("failed to write template2: %v", err)
	}

	// Also write a non-ARM JSON file to ensure it's ignored
	nonARMFile := `{"key": "value"}`
	if err := os.WriteFile(filepath.Join(tmpDir, "config.json"), []byte(nonARMFile), 0644); err != nil {
		t.Fatalf("failed to write non-ARM file: %v", err)
	}

	p := azureparser.NewARMParser()
	ctx := context.Background()
	opts := parser.NewParseOptions()
	infra, err := p.Parse(ctx, tmpDir, opts)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	if len(infra.Resources) != 2 {
		t.Errorf("expected 2 resources, got %d", len(infra.Resources))
	}

	if _, ok := infra.Resources["storage1"]; !ok {
		t.Error("expected storage1 resource")
	}

	if _, ok := infra.Resources["vm1"]; !ok {
		t.Error("expected vm1 resource")
	}
}

// TestARMParserIntegration_FilterByType tests filtering resources by type.
func TestARMParserIntegration_FilterByType(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "arm-filter-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	templateContent := `{
		"$schema": "https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#",
		"contentVersion": "1.0.0.0",
		"resources": [
			{
				"type": "Microsoft.Storage/storageAccounts",
				"apiVersion": "2021-02-01",
				"name": "mystorageaccount",
				"location": "eastus",
				"properties": {}
			},
			{
				"type": "Microsoft.Compute/virtualMachines",
				"apiVersion": "2021-03-01",
				"name": "myvm",
				"location": "eastus",
				"properties": {}
			}
		]
	}`

	templatePath := filepath.Join(tmpDir, "template.json")
	if err := os.WriteFile(templatePath, []byte(templateContent), 0644); err != nil {
		t.Fatalf("failed to write template: %v", err)
	}

	p := azureparser.NewARMParser()
	ctx := context.Background()
	opts := parser.NewParseOptions().WithFilterTypes(resource.TypeAzureStorageAcct)
	infra, err := p.Parse(ctx, templatePath, opts)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	if len(infra.Resources) != 1 {
		t.Errorf("expected 1 filtered resource, got %d", len(infra.Resources))
	}

	if _, ok := infra.Resources["mystorageaccount"]; !ok {
		t.Error("expected mystorageaccount resource")
	}

	if _, ok := infra.Resources["myvm"]; ok {
		t.Error("VM should have been filtered out")
	}
}

// TestARMParserIntegration_CaseInsensitiveTypes tests that ARM type matching is case-insensitive.
func TestARMParserIntegration_CaseInsensitiveTypes(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "arm-case-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Use mixed case resource types
	templateContent := `{
		"$schema": "https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#",
		"contentVersion": "1.0.0.0",
		"resources": [
			{
				"type": "MICROSOFT.COMPUTE/VIRTUALMACHINES",
				"apiVersion": "2021-03-01",
				"name": "myvm-upper",
				"location": "eastus",
				"properties": {}
			},
			{
				"type": "microsoft.storage/storageaccounts",
				"apiVersion": "2021-02-01",
				"name": "mystorage-lower",
				"location": "eastus",
				"properties": {}
			}
		]
	}`

	templatePath := filepath.Join(tmpDir, "case-test.json")
	if err := os.WriteFile(templatePath, []byte(templateContent), 0644); err != nil {
		t.Fatalf("failed to write template: %v", err)
	}

	p := azureparser.NewARMParser()
	ctx := context.Background()
	opts := parser.NewParseOptions()
	infra, err := p.Parse(ctx, templatePath, opts)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	if len(infra.Resources) != 2 {
		t.Errorf("expected 2 resources, got %d", len(infra.Resources))
	}

	vmRes, ok := infra.Resources["myvm-upper"]
	if !ok {
		t.Fatal("expected myvm-upper resource")
	}
	if vmRes.Type != resource.TypeAzureVM {
		t.Errorf("expected VM type for uppercase, got %s", vmRes.Type)
	}

	storageRes, ok := infra.Resources["mystorage-lower"]
	if !ok {
		t.Fatal("expected mystorage-lower resource")
	}
	if storageRes.Type != resource.TypeAzureStorageAcct {
		t.Errorf("expected storage type for lowercase, got %s", storageRes.Type)
	}
}
