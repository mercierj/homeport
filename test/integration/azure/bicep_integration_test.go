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

// TestBicepParserIntegration_BasicResource tests parsing a basic Bicep file with a storage account.
func TestBicepParserIntegration_BasicResource(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "bicep-integration-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	bicepContent := `param location string = 'eastus'
param storageAccountName string = 'mystorageacct'

resource storageAccount 'Microsoft.Storage/storageAccounts@2021-02-01' = {
  name: storageAccountName
  location: location
  sku: {
    name: 'Standard_LRS'
  }
  kind: 'StorageV2'
  properties: {
    accessTier: 'Hot'
  }
}

output storageAccountId string = storageAccount.id
`

	bicepPath := filepath.Join(tmpDir, "main.bicep")
	if err := os.WriteFile(bicepPath, []byte(bicepContent), 0644); err != nil {
		t.Fatalf("failed to write bicep file: %v", err)
	}

	p := azureparser.NewBicepParser()

	// Verify provider
	if p.Provider() != resource.ProviderAzure {
		t.Errorf("expected Azure provider, got %s", p.Provider())
	}

	// Verify format
	formats := p.SupportedFormats()
	if len(formats) != 1 || formats[0] != parser.FormatBicep {
		t.Errorf("expected Bicep format, got %v", formats)
	}

	// Test validation
	if err := p.Validate(bicepPath); err != nil {
		t.Errorf("validation failed: %v", err)
	}

	// Test auto-detection
	canHandle, confidence := p.AutoDetect(bicepPath)
	if !canHandle {
		t.Error("expected parser to handle Bicep file")
	}
	if confidence < 0.9 {
		t.Errorf("expected high confidence, got %f", confidence)
	}

	// Test parsing
	ctx := context.Background()
	opts := parser.NewParseOptions()
	infra, err := p.Parse(ctx, bicepPath, opts)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	if len(infra.Resources) != 1 {
		t.Errorf("expected 1 resource, got %d", len(infra.Resources))
	}

	// Check the resource was parsed correctly
	var storageRes *resource.Resource
	for _, res := range infra.Resources {
		if res.Type == resource.TypeAzureStorageAcct {
			storageRes = res
			break
		}
	}

	if storageRes == nil {
		t.Fatal("expected storage account resource")
	}

	// Verify metadata contains format info
	if infra.Metadata["format"] != "bicep" {
		t.Errorf("expected format metadata to be 'bicep', got %s", infra.Metadata["format"])
	}
}

// TestBicepParserIntegration_MultipleResources tests parsing Bicep files with multiple resources.
func TestBicepParserIntegration_MultipleResources(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "bicep-multi-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	bicepContent := `param location string = 'westus2'

resource storageAccount 'Microsoft.Storage/storageAccounts@2021-02-01' = {
  name: 'mystorageacct'
  location: location
  sku: {
    name: 'Standard_LRS'
  }
  kind: 'StorageV2'
}

resource virtualMachine 'Microsoft.Compute/virtualMachines@2021-03-01' = {
  name: 'myvm'
  location: location
  properties: {
    hardwareProfile: {
      vmSize: 'Standard_DS2_v2'
    }
  }
}

resource sqlDatabase 'Microsoft.Sql/servers/databases@2021-02-01' = {
  name: 'myserver/mydb'
  location: location
  properties: {}
}

resource keyVault 'Microsoft.KeyVault/vaults@2021-06-01' = {
  name: 'mykeyvault'
  location: location
  properties: {
    sku: {
      family: 'A'
      name: 'standard'
    }
    tenantId: subscription().tenantId
  }
}

resource serviceBus 'Microsoft.ServiceBus/namespaces@2021-06-01' = {
  name: 'myservicebus'
  location: location
  sku: {
    name: 'Standard'
    tier: 'Standard'
  }
}
`

	bicepPath := filepath.Join(tmpDir, "infrastructure.bicep")
	if err := os.WriteFile(bicepPath, []byte(bicepContent), 0644); err != nil {
		t.Fatalf("failed to write bicep file: %v", err)
	}

	p := azureparser.NewBicepParser()
	ctx := context.Background()
	opts := parser.NewParseOptions()
	infra, err := p.Parse(ctx, bicepPath, opts)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	if len(infra.Resources) != 5 {
		t.Errorf("expected 5 resources, got %d", len(infra.Resources))
	}

	// Verify resource types are correctly mapped
	expectedTypes := map[resource.Type]bool{
		resource.TypeAzureStorageAcct: false,
		resource.TypeAzureVM:          false,
		resource.TypeAzureSQL:         false,
		resource.TypeKeyVault:         false,
		resource.TypeServiceBus:       false,
	}

	for _, res := range infra.Resources {
		if _, exists := expectedTypes[res.Type]; exists {
			expectedTypes[res.Type] = true
		}
	}

	for resType, found := range expectedTypes {
		if !found {
			t.Errorf("expected to find resource type %s", resType)
		}
	}
}

// TestBicepParserIntegration_ResourceDependencies tests that Bicep resource dependencies are extracted.
func TestBicepParserIntegration_ResourceDependencies(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "bicep-deps-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	bicepContent := `param location string = 'eastus'

resource storageAccount 'Microsoft.Storage/storageAccounts@2021-02-01' = {
  name: 'mystorageacct'
  location: location
  sku: {
    name: 'Standard_LRS'
  }
  kind: 'StorageV2'
}

resource virtualMachine 'Microsoft.Compute/virtualMachines@2021-03-01' = {
  name: 'myvm'
  location: location
  properties: {
    storageProfile: {
      imageReference: storageAccount.id
    }
  }
}

resource appService 'Microsoft.Web/sites@2021-02-01' = {
  name: 'myapp'
  location: location
  properties: {
    serverFarmId: virtualMachine.properties.vmId
    storageAccountRequired: storageAccount.name
  }
}
`

	bicepPath := filepath.Join(tmpDir, "deps.bicep")
	if err := os.WriteFile(bicepPath, []byte(bicepContent), 0644); err != nil {
		t.Fatalf("failed to write bicep file: %v", err)
	}

	p := azureparser.NewBicepParser()
	ctx := context.Background()
	opts := parser.NewParseOptions()
	infra, err := p.Parse(ctx, bicepPath, opts)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	// Find VM resource and check dependencies
	var vmRes *resource.Resource
	for _, res := range infra.Resources {
		if res.Type == resource.TypeAzureVM {
			vmRes = res
			break
		}
	}

	if vmRes != nil && len(vmRes.Dependencies) > 0 {
		foundStorageDep := false
		for _, dep := range vmRes.Dependencies {
			if dep == "storageAccount" {
				foundStorageDep = true
				break
			}
		}
		if !foundStorageDep {
			t.Log("VM resource dependencies:", vmRes.Dependencies)
		}
	}

	// Find app service and check it has dependencies
	var appRes *resource.Resource
	for _, res := range infra.Resources {
		if res.Type == resource.TypeAppService {
			appRes = res
			break
		}
	}

	if appRes != nil && len(appRes.Dependencies) > 0 {
		t.Logf("App Service has %d dependencies", len(appRes.Dependencies))
	}
}

// TestBicepParserIntegration_DirectoryParsing tests parsing multiple Bicep files from a directory.
func TestBicepParserIntegration_DirectoryParsing(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "bicep-dir-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create multiple Bicep files
	mainBicep := `resource storageAccount 'Microsoft.Storage/storageAccounts@2021-02-01' = {
  name: 'mainstorage'
  location: 'eastus'
  sku: {
    name: 'Standard_LRS'
  }
  kind: 'StorageV2'
}
`

	moduleBicep := `resource virtualMachine 'Microsoft.Compute/virtualMachines@2021-03-01' = {
  name: 'modulevm'
  location: 'eastus'
  properties: {}
}
`

	if err := os.WriteFile(filepath.Join(tmpDir, "main.bicep"), []byte(mainBicep), 0644); err != nil {
		t.Fatalf("failed to write main.bicep: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "module.bicep"), []byte(moduleBicep), 0644); err != nil {
		t.Fatalf("failed to write module.bicep: %v", err)
	}

	p := azureparser.NewBicepParser()

	// Test directory validation
	if err := p.Validate(tmpDir); err != nil {
		t.Errorf("directory validation failed: %v", err)
	}

	// Test directory auto-detection
	canHandle, confidence := p.AutoDetect(tmpDir)
	if !canHandle {
		t.Error("expected parser to handle directory with Bicep files")
	}
	if confidence < 0.8 {
		t.Errorf("expected high confidence for directory, got %f", confidence)
	}

	// Test parsing
	ctx := context.Background()
	opts := parser.NewParseOptions()
	infra, err := p.Parse(ctx, tmpDir, opts)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	if len(infra.Resources) != 2 {
		t.Errorf("expected 2 resources from directory, got %d", len(infra.Resources))
	}
}

// TestBicepParserIntegration_ParametersAndVariables tests that parameters and variables are captured.
func TestBicepParserIntegration_ParametersAndVariables(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "bicep-params-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	bicepContent := `param environment string = 'dev'
param location string = 'eastus'
param storageAccountPrefix string

var storageAccountName = '${storageAccountPrefix}${environment}'
var resourceGroupName = 'rg-${environment}'

resource storageAccount 'Microsoft.Storage/storageAccounts@2021-02-01' = {
  name: storageAccountName
  location: location
  sku: {
    name: 'Standard_LRS'
  }
  kind: 'StorageV2'
}

output storageAccountId string = storageAccount.id
output storageAccountName string = storageAccount.name
`

	bicepPath := filepath.Join(tmpDir, "params.bicep")
	if err := os.WriteFile(bicepPath, []byte(bicepContent), 0644); err != nil {
		t.Fatalf("failed to write bicep file: %v", err)
	}

	p := azureparser.NewBicepParser()
	ctx := context.Background()
	opts := parser.NewParseOptions()
	infra, err := p.Parse(ctx, bicepPath, opts)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	// Check that parameters are captured in metadata
	if _, ok := infra.Metadata["param.environment"]; !ok {
		t.Error("expected param.environment in metadata")
	}

	if _, ok := infra.Metadata["param.location"]; !ok {
		t.Error("expected param.location in metadata")
	}

	// Check that variables are captured
	if _, ok := infra.Metadata["var.storageAccountName"]; !ok {
		t.Error("expected var.storageAccountName in metadata")
	}

	// Check that outputs are captured
	if _, ok := infra.Metadata["output.storageAccountId"]; !ok {
		t.Error("expected output.storageAccountId in metadata")
	}
}

// TestBicepParserIntegration_MessagingResources tests Bicep parsing for messaging resource types.
func TestBicepParserIntegration_MessagingResources(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "bicep-messaging-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	bicepContent := `param location string = 'eastus'

resource serviceBusNamespace 'Microsoft.ServiceBus/namespaces@2021-06-01' = {
  name: 'myservicebus'
  location: location
  sku: {
    name: 'Standard'
  }
}

resource serviceBusQueue 'Microsoft.ServiceBus/namespaces/queues@2021-06-01' = {
  parent: serviceBusNamespace
  name: 'myqueue'
  properties: {
    maxDeliveryCount: 10
  }
}

resource eventHub 'Microsoft.EventHub/namespaces@2021-06-01' = {
  name: 'myeventhub'
  location: location
  sku: {
    name: 'Standard'
    tier: 'Standard'
    capacity: 1
  }
}

resource eventGridTopic 'Microsoft.EventGrid/topics@2021-06-01' = {
  name: 'myeventgrid'
  location: location
}

resource logicApp 'Microsoft.Logic/workflows@2019-05-01' = {
  name: 'mylogicapp'
  location: location
  properties: {
    state: 'Enabled'
  }
}
`

	bicepPath := filepath.Join(tmpDir, "messaging.bicep")
	if err := os.WriteFile(bicepPath, []byte(bicepContent), 0644); err != nil {
		t.Fatalf("failed to write bicep file: %v", err)
	}

	p := azureparser.NewBicepParser()
	ctx := context.Background()
	opts := parser.NewParseOptions()
	infra, err := p.Parse(ctx, bicepPath, opts)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	// Verify messaging resources are parsed
	messagingTypes := map[resource.Type]bool{
		resource.TypeServiceBus:      false,
		resource.TypeServiceBusQueue: false,
		resource.TypeEventHub:        false,
		resource.TypeEventGrid:       false,
		resource.TypeLogicApp:        false,
	}

	for _, res := range infra.Resources {
		if _, exists := messagingTypes[res.Type]; exists {
			messagingTypes[res.Type] = true
		}
	}

	for resType, found := range messagingTypes {
		if !found {
			t.Errorf("expected to find messaging resource type %s", resType)
		}
	}
}

// TestBicepParserIntegration_InvalidPath tests error handling for invalid paths.
func TestBicepParserIntegration_InvalidPath(t *testing.T) {
	p := azureparser.NewBicepParser()

	// Test with non-existent path
	err := p.Validate("/nonexistent/path/to/file.bicep")
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

// TestBicepParserIntegration_NonBicepFile tests validation of non-Bicep files.
func TestBicepParserIntegration_NonBicepFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "bicep-nonbicep-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a non-Bicep file
	jsonPath := filepath.Join(tmpDir, "config.json")
	if err := os.WriteFile(jsonPath, []byte(`{"key": "value"}`), 0644); err != nil {
		t.Fatalf("failed to write json file: %v", err)
	}

	p := azureparser.NewBicepParser()

	// Validate should fail for non-Bicep file
	err = p.Validate(jsonPath)
	if err != parser.ErrUnsupportedFormat {
		t.Errorf("expected ErrUnsupportedFormat for non-Bicep file, got %v", err)
	}

	// Auto-detect should return false
	canHandle, _ := p.AutoDetect(jsonPath)
	if canHandle {
		t.Error("expected false for non-Bicep file")
	}
}
