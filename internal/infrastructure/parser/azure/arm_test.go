package azure

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudexit/cloudexit/internal/domain/parser"
	"github.com/cloudexit/cloudexit/internal/domain/resource"
)

func TestNewARMParser(t *testing.T) {
	p := NewARMParser()
	if p == nil {
		t.Fatal("expected non-nil parser")
	}
}

func TestARMParser_Provider(t *testing.T) {
	p := NewARMParser()
	if p.Provider() != resource.ProviderAzure {
		t.Errorf("expected azure provider, got %s", p.Provider())
	}
}

func TestARMParser_SupportedFormats(t *testing.T) {
	p := NewARMParser()
	formats := p.SupportedFormats()
	if len(formats) != 1 {
		t.Errorf("expected 1 format, got %d", len(formats))
	}
	if formats[0] != parser.FormatARM {
		t.Errorf("expected arm format, got %s", formats[0])
	}
}

func TestARMParser_Validate_InvalidPath(t *testing.T) {
	p := NewARMParser()
	err := p.Validate("/nonexistent/path")
	if err != parser.ErrInvalidPath {
		t.Errorf("expected ErrInvalidPath, got %v", err)
	}
}

func TestARMParser_AutoDetect_InvalidPath(t *testing.T) {
	p := NewARMParser()
	canHandle, confidence := p.AutoDetect("/nonexistent/path")
	if canHandle {
		t.Error("expected false for nonexistent path")
	}
	if confidence != 0 {
		t.Errorf("expected 0 confidence, got %f", confidence)
	}
}

func TestARMParser_Parse_ValidTemplate(t *testing.T) {
	// Create a temporary directory with a valid ARM template
	tmpDir, err := os.MkdirTemp("", "arm-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	templateContent := `{
		"$schema": "https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#",
		"contentVersion": "1.0.0.0",
		"parameters": {},
		"resources": [
			{
				"type": "Microsoft.Storage/storageAccounts",
				"apiVersion": "2021-02-01",
				"name": "mystorageaccount",
				"location": "eastus",
				"properties": {
					"accessTier": "Hot"
				},
				"tags": {
					"environment": "test"
				}
			},
			{
				"type": "Microsoft.Compute/virtualMachines",
				"apiVersion": "2021-03-01",
				"name": "myvm",
				"location": "eastus",
				"properties": {
					"vmSize": "Standard_DS1_v2"
				},
				"dependsOn": [
					"mystorageaccount"
				]
			}
		]
	}`

	templatePath := filepath.Join(tmpDir, "template.json")
	if err := os.WriteFile(templatePath, []byte(templateContent), 0644); err != nil {
		t.Fatalf("failed to write template: %v", err)
	}

	p := NewARMParser()

	// Test AutoDetect
	canHandle, confidence := p.AutoDetect(templatePath)
	if !canHandle {
		t.Error("expected parser to handle ARM template")
	}
	if confidence < 0.5 {
		t.Errorf("expected high confidence, got %f", confidence)
	}

	// Test Parse
	ctx := context.Background()
	opts := parser.NewParseOptions()
	infra, err := p.Parse(ctx, templatePath, opts)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	if len(infra.Resources) != 2 {
		t.Errorf("expected 2 resources, got %d", len(infra.Resources))
	}

	// Check storage account
	storage, ok := infra.Resources["mystorageaccount"]
	if !ok {
		t.Error("expected mystorageaccount resource")
	} else {
		if storage.Type != resource.TypeAzureStorageAcct {
			t.Errorf("expected storage account type, got %s", storage.Type)
		}
		if storage.Region != "eastus" {
			t.Errorf("expected eastus region, got %s", storage.Region)
		}
	}

	// Check VM
	vm, ok := infra.Resources["myvm"]
	if !ok {
		t.Error("expected myvm resource")
	} else {
		if vm.Type != resource.TypeAzureVM {
			t.Errorf("expected VM type, got %s", vm.Type)
		}
		if len(vm.Dependencies) != 1 {
			t.Errorf("expected 1 dependency, got %d", len(vm.Dependencies))
		}
	}
}

func TestARMParser_Parse_Directory(t *testing.T) {
	// Create a temporary directory with ARM templates
	tmpDir, err := os.MkdirTemp("", "arm-test-dir")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	template1 := `{
		"$schema": "https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#",
		"contentVersion": "1.0.0.0",
		"resources": [
			{
				"type": "Microsoft.Storage/storageAccounts",
				"apiVersion": "2021-02-01",
				"name": "storage1",
				"location": "eastus"
			}
		]
	}`

	template2 := `{
		"$schema": "https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#",
		"contentVersion": "1.0.0.0",
		"resources": [
			{
				"type": "Microsoft.Storage/storageAccounts",
				"apiVersion": "2021-02-01",
				"name": "storage2",
				"location": "westus"
			}
		]
	}`

	if err := os.WriteFile(filepath.Join(tmpDir, "main.json"), []byte(template1), 0644); err != nil {
		t.Fatalf("failed to write template1: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "other.json"), []byte(template2), 0644); err != nil {
		t.Fatalf("failed to write template2: %v", err)
	}

	p := NewARMParser()
	ctx := context.Background()
	opts := parser.NewParseOptions()
	infra, err := p.Parse(ctx, tmpDir, opts)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	if len(infra.Resources) != 2 {
		t.Errorf("expected 2 resources, got %d", len(infra.Resources))
	}
}

func TestMapARMTypeToResourceType(t *testing.T) {
	tests := []struct {
		armType      string
		expectedType resource.Type
	}{
		{"Microsoft.Compute/virtualMachines", resource.TypeAzureVM},
		{"Microsoft.Storage/storageAccounts", resource.TypeAzureStorageAcct},
		{"Microsoft.Sql/servers/databases", resource.TypeAzureSQL},
		{"Microsoft.Cache/redis", resource.TypeAzureCache},
		{"Microsoft.Network/loadBalancers", resource.TypeAzureLB},
		{"Microsoft.KeyVault/vaults", resource.TypeKeyVault},
		{"microsoft.compute/virtualmachines", resource.TypeAzureVM}, // Case insensitive
		{"unknown/type", ""},
	}

	for _, tt := range tests {
		t.Run(tt.armType, func(t *testing.T) {
			result := mapARMTypeToResourceType(tt.armType)
			if result != tt.expectedType {
				t.Errorf("expected %s, got %s", tt.expectedType, result)
			}
		})
	}
}
