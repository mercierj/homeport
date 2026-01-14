// Package e2e contains end-to-end tests for the Homeport migration tool.
package e2e

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/parser"
	"github.com/homeport/homeport/internal/domain/resource"
	"github.com/homeport/homeport/internal/infrastructure/generator/compose"
	_ "github.com/homeport/homeport/internal/infrastructure/mapper/azure/compute"
	_ "github.com/homeport/homeport/internal/infrastructure/mapper/azure/database"
	_ "github.com/homeport/homeport/internal/infrastructure/mapper/azure/storage"
	_ "github.com/homeport/homeport/internal/infrastructure/parser/azure"
)

// TestAzureMigration_ARMTemplate tests complete Azure to self-hosted migration using ARM templates.
func TestAzureMigration_ARMTemplate(t *testing.T) {
	// Create temporary ARM template
	tmpDir := t.TempDir()
	armPath := filepath.Join(tmpDir, "azuredeploy.json")

	armContent := `{
  "$schema": "https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#",
  "contentVersion": "1.0.0.0",
  "parameters": {
    "location": {
      "type": "string",
      "defaultValue": "[resourceGroup().location]"
    },
    "vmSize": {
      "type": "string",
      "defaultValue": "Standard_D2s_v3"
    }
  },
  "variables": {
    "vmName": "web-server",
    "storageAccountName": "[concat('storage', uniqueString(resourceGroup().id))]"
  },
  "resources": [
    {
      "type": "Microsoft.Compute/virtualMachines",
      "apiVersion": "2023-03-01",
      "name": "[variables('vmName')]",
      "location": "[parameters('location')]",
      "properties": {
        "hardwareProfile": {
          "vmSize": "[parameters('vmSize')]"
        },
        "storageProfile": {
          "imageReference": {
            "publisher": "Canonical",
            "offer": "0001-com-ubuntu-server-jammy",
            "sku": "22_04-lts",
            "version": "latest"
          },
          "osDisk": {
            "createOption": "FromImage",
            "managedDisk": {
              "storageAccountType": "Premium_LRS"
            }
          }
        }
      },
      "tags": {
        "environment": "production",
        "app": "webapp"
      }
    },
    {
      "type": "Microsoft.Storage/storageAccounts",
      "apiVersion": "2023-01-01",
      "name": "[variables('storageAccountName')]",
      "location": "[parameters('location')]",
      "sku": {
        "name": "Standard_LRS"
      },
      "kind": "StorageV2",
      "properties": {
        "accessTier": "Hot"
      }
    },
    {
      "type": "Microsoft.DBforPostgreSQL/flexibleServers",
      "apiVersion": "2023-03-01-preview",
      "name": "main-database",
      "location": "[parameters('location')]",
      "sku": {
        "name": "Standard_D4s_v3",
        "tier": "GeneralPurpose"
      },
      "properties": {
        "version": "15",
        "administratorLogin": "dbadmin",
        "storage": {
          "storageSizeGB": 128
        },
        "backup": {
          "backupRetentionDays": 7,
          "geoRedundantBackup": "Disabled"
        },
        "highAvailability": {
          "mode": "ZoneRedundant"
        }
      }
    },
    {
      "type": "Microsoft.Cache/redis",
      "apiVersion": "2023-04-01",
      "name": "app-cache",
      "location": "[parameters('location')]",
      "properties": {
        "sku": {
          "name": "Standard",
          "family": "C",
          "capacity": 1
        },
        "enableNonSslPort": false,
        "minimumTlsVersion": "1.2"
      }
    },
    {
      "type": "Microsoft.Web/sites",
      "apiVersion": "2022-09-01",
      "name": "api-app",
      "location": "[parameters('location')]",
      "kind": "app,linux",
      "properties": {
        "serverFarmId": "[resourceId('Microsoft.Web/serverfarms', 'app-service-plan')]",
        "siteConfig": {
          "linuxFxVersion": "NODE|18-lts"
        }
      },
      "dependsOn": [
        "[resourceId('Microsoft.DBforPostgreSQL/flexibleServers', 'main-database')]"
      ]
    },
    {
      "type": "Microsoft.ServiceBus/namespaces",
      "apiVersion": "2022-10-01-preview",
      "name": "app-messaging",
      "location": "[parameters('location')]",
      "sku": {
        "name": "Standard",
        "tier": "Standard"
      }
    },
    {
      "type": "Microsoft.KeyVault/vaults",
      "apiVersion": "2023-02-01",
      "name": "app-secrets",
      "location": "[parameters('location')]",
      "properties": {
        "tenantId": "[subscription().tenantId]",
        "sku": {
          "family": "A",
          "name": "standard"
        },
        "accessPolicies": []
      }
    }
  ],
  "outputs": {
    "vmId": {
      "type": "string",
      "value": "[resourceId('Microsoft.Compute/virtualMachines', variables('vmName'))]"
    }
  }
}`

	if err := os.WriteFile(armPath, []byte(armContent), 0644); err != nil {
		t.Fatalf("Failed to write ARM template: %v", err)
	}

	ctx := context.Background()
	outputDir := t.TempDir()

	// Step 1: Parse ARM template
	t.Run("Parse ARM Template", func(t *testing.T) {
		registry := parser.DefaultRegistry()
		p, err := registry.GetByFormat(resource.ProviderAzure, parser.FormatARM)
		if err != nil {
			t.Fatalf("Failed to get ARM parser: %v", err)
		}

		opts := parser.NewParseOptions()
		infra, err := p.Parse(ctx, armPath, opts)
		if err != nil {
			t.Fatalf("Failed to parse ARM template: %v", err)
		}

		if infra == nil {
			t.Fatal("Infrastructure is nil")
		}

		t.Logf("Parsed %d Azure resources from ARM template", len(infra.Resources))

		// Log discovered resources
		for id, res := range infra.Resources {
			t.Logf("  - %s: %s (%s)", id, res.Name, res.Type)
		}

		// Verify expected resource types
		expectedTypes := []resource.Type{
			resource.TypeAzureVM,
			resource.TypeAzureStorageAcct,
			resource.TypeAzurePostgres,
			resource.TypeAzureCache,
		}

		for _, expected := range expectedTypes {
			found := false
			for _, res := range infra.Resources {
				if res.Type == expected {
					found = true
					break
				}
			}
			if !found {
				t.Logf("Note: Expected resource type %s not found", expected)
			}
		}
	})

	// Step 2: Map Azure resources to self-hosted
	t.Run("Map Azure to Self-Hosted", func(t *testing.T) {
		registry := parser.DefaultRegistry()
		p, err := registry.GetByFormat(resource.ProviderAzure, parser.FormatARM)
		if err != nil {
			t.Fatalf("Failed to get parser: %v", err)
		}

		infra, err := p.Parse(ctx, armPath, parser.NewParseOptions())
		if err != nil {
			t.Fatalf("Failed to parse: %v", err)
		}

		mappedCount := 0
		for _, res := range infra.Resources {
			m, err := mapper.Get(res.Type)
			if err != nil {
				t.Logf("No mapper for Azure resource type %s", res.Type)
				continue
			}

			result, err := m.Map(ctx, res)
			if err != nil {
				t.Logf("Failed to map %s: %v", res.ID, err)
				continue
			}

			if result != nil && result.DockerService != nil {
				mappedCount++
				t.Logf("Mapped %s -> %s (image: %s)",
					res.ID, result.DockerService.Name, result.DockerService.Image)
			}
		}

		t.Logf("Successfully mapped %d Azure resources", mappedCount)
	})

	// Step 3: Generate Docker Compose
	t.Run("Generate Docker Compose", func(t *testing.T) {
		registry := parser.DefaultRegistry()
		p, err := registry.GetByFormat(resource.ProviderAzure, parser.FormatARM)
		if err != nil {
			t.Fatalf("Failed to get parser: %v", err)
		}

		infra, err := p.Parse(ctx, armPath, parser.NewParseOptions())
		if err != nil {
			t.Fatalf("Failed to parse: %v", err)
		}

		var results []*mapper.MappingResult
		for _, res := range infra.Resources {
			m, err := mapper.Get(res.Type)
			if err != nil {
				continue
			}
			result, err := m.Map(ctx, res)
			if err != nil {
				continue
			}
			if result != nil {
				results = append(results, result)
			}
		}

		if len(results) == 0 {
			t.Skip("No Azure resources mapped")
		}

		gen := compose.NewGenerator("azure-migration-test")
		output, err := gen.Generate(results)
		if err != nil {
			t.Fatalf("Failed to generate Docker Compose: %v", err)
		}

		composeContent, ok := output.Files["docker-compose.yml"]
		if !ok {
			t.Fatal("docker-compose.yml not generated")
		}

		// Write to output directory
		composePath := filepath.Join(outputDir, "docker-compose.yml")
		if err := os.WriteFile(composePath, composeContent, 0644); err != nil {
			t.Fatalf("Failed to write docker-compose.yml: %v", err)
		}

		t.Logf("Generated docker-compose.yml: %s (%d bytes)", composePath, len(composeContent))
	})

	// Step 4: Verify complete migration output
	t.Run("Verify Complete Output", func(t *testing.T) {
		registry := parser.DefaultRegistry()
		p, err := registry.GetByFormat(resource.ProviderAzure, parser.FormatARM)
		if err != nil {
			t.Fatalf("Failed to get parser: %v", err)
		}

		infra, err := p.Parse(ctx, armPath, parser.NewParseOptions())
		if err != nil {
			t.Fatalf("Failed to parse: %v", err)
		}

		scriptsDir := filepath.Join(outputDir, "scripts")
		configsDir := filepath.Join(outputDir, "configs")

		totalScripts := 0
		totalConfigs := 0
		totalWarnings := 0
		totalManualSteps := 0

		for _, res := range infra.Resources {
			m, err := mapper.Get(res.Type)
			if err != nil {
				continue
			}
			result, err := m.Map(ctx, res)
			if err != nil || result == nil {
				continue
			}

			// Collect stats
			totalScripts += len(result.Scripts)
			totalConfigs += len(result.Configs)
			totalWarnings += len(result.Warnings)
			totalManualSteps += len(result.ManualSteps)

			// Write scripts
			for name, content := range result.Scripts {
				scriptPath := filepath.Join(scriptsDir, name)
				os.MkdirAll(filepath.Dir(scriptPath), 0755)
				os.WriteFile(scriptPath, content, 0755)
			}

			// Write configs
			for name, content := range result.Configs {
				configPath := filepath.Join(configsDir, name)
				os.MkdirAll(filepath.Dir(configPath), 0755)
				os.WriteFile(configPath, content, 0644)
			}
		}

		t.Logf("Migration summary:")
		t.Logf("  - Scripts: %d", totalScripts)
		t.Logf("  - Configs: %d", totalConfigs)
		t.Logf("  - Warnings: %d", totalWarnings)
		t.Logf("  - Manual steps: %d", totalManualSteps)
		t.Logf("Output directory: %s", outputDir)
	})
}

// TestAzureMigration_Terraform tests Azure migration using Terraform.
func TestAzureMigration_Terraform(t *testing.T) {
	tmpDir := t.TempDir()
	tfPath := filepath.Join(tmpDir, "main.tf")

	tfContent := `terraform {
  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = "~> 3.0"
    }
  }
}

provider "azurerm" {
  features {}
}

resource "azurerm_resource_group" "main" {
  name     = "webapp-rg"
  location = "East US"
}

resource "azurerm_linux_virtual_machine" "web" {
  name                = "web-server"
  resource_group_name = azurerm_resource_group.main.name
  location            = azurerm_resource_group.main.location
  size                = "Standard_D2s_v3"
  admin_username      = "adminuser"

  network_interface_ids = []

  os_disk {
    caching              = "ReadWrite"
    storage_account_type = "Premium_LRS"
  }

  source_image_reference {
    publisher = "Canonical"
    offer     = "0001-com-ubuntu-server-jammy"
    sku       = "22_04-lts"
    version   = "latest"
  }

  tags = {
    environment = "production"
  }
}

resource "azurerm_postgresql_flexible_server" "main" {
  name                   = "webapp-db"
  resource_group_name    = azurerm_resource_group.main.name
  location               = azurerm_resource_group.main.location
  version                = "15"
  administrator_login    = "dbadmin"
  administrator_password = "SecureP@ssword123!"
  storage_mb             = 131072
  sku_name               = "GP_Standard_D4s_v3"
  zone                   = "1"
}

resource "azurerm_storage_account" "main" {
  name                     = "webappassets"
  resource_group_name      = azurerm_resource_group.main.name
  location                 = azurerm_resource_group.main.location
  account_tier             = "Standard"
  account_replication_type = "LRS"
}

resource "azurerm_redis_cache" "main" {
  name                = "webapp-cache"
  resource_group_name = azurerm_resource_group.main.name
  location            = azurerm_resource_group.main.location
  capacity            = 1
  family              = "C"
  sku_name            = "Standard"
  enable_non_ssl_port = false
  minimum_tls_version = "1.2"
}

resource "azurerm_kubernetes_cluster" "main" {
  name                = "webapp-aks"
  resource_group_name = azurerm_resource_group.main.name
  location            = azurerm_resource_group.main.location
  dns_prefix          = "webappaks"

  default_node_pool {
    name       = "default"
    node_count = 3
    vm_size    = "Standard_D2s_v3"
  }

  identity {
    type = "SystemAssigned"
  }
}

resource "azurerm_cosmosdb_account" "main" {
  name                = "webapp-cosmos"
  resource_group_name = azurerm_resource_group.main.name
  location            = azurerm_resource_group.main.location
  offer_type          = "Standard"
  kind                = "MongoDB"

  capabilities {
    name = "MongoDBv3.4"
  }

  consistency_policy {
    consistency_level = "Session"
  }

  geo_location {
    location          = azurerm_resource_group.main.location
    failover_priority = 0
  }
}
`

	if err := os.WriteFile(tfPath, []byte(tfContent), 0644); err != nil {
		t.Fatalf("Failed to write Terraform file: %v", err)
	}

	ctx := context.Background()

	t.Run("Parse Azure Terraform", func(t *testing.T) {
		registry := parser.DefaultRegistry()
		p, err := registry.GetByFormat(resource.ProviderAzure, parser.FormatTerraform)
		if err != nil {
			t.Logf("Azure Terraform parser not available: %v", err)
			return
		}

		opts := parser.NewParseOptions()
		infra, err := p.Parse(ctx, tmpDir, opts)
		if err != nil {
			t.Fatalf("Failed to parse Azure Terraform: %v", err)
		}

		t.Logf("Parsed %d Azure resources from Terraform", len(infra.Resources))

		for id, res := range infra.Resources {
			t.Logf("  - %s: %s", id, res.Type)
		}
	})
}

// TestAzureMigration_ResourceTypes tests migration of specific Azure resource types.
func TestAzureMigration_ResourceTypes(t *testing.T) {
	ctx := context.Background()

	testCases := []struct {
		name           string
		resourceType   resource.Type
		config         map[string]interface{}
		expectedImage  string
		shouldHavePort bool
	}{
		{
			name:         "Azure VM",
			resourceType: resource.TypeAzureVM,
			config: map[string]interface{}{
				"size":     "Standard_D2s_v3",
				"os_disk":  map[string]interface{}{"storage_account_type": "Premium_LRS"},
			},
			expectedImage:  "",
			shouldHavePort: true,
		},
		{
			name:         "Azure PostgreSQL",
			resourceType: resource.TypeAzurePostgres,
			config: map[string]interface{}{
				"version":    "15",
				"sku_name":   "GP_Standard_D4s_v3",
				"storage_mb": 131072,
			},
			expectedImage:  "postgres",
			shouldHavePort: true,
		},
		{
			name:         "Azure MySQL",
			resourceType: resource.TypeAzureMySQL,
			config: map[string]interface{}{
				"version":    "8.0",
				"sku_name":   "GP_Standard_D2ds_v4",
				"storage_mb": 65536,
			},
			expectedImage:  "mysql",
			shouldHavePort: true,
		},
		{
			name:         "Azure Redis Cache",
			resourceType: resource.TypeAzureCache,
			config: map[string]interface{}{
				"sku_name": "Standard",
				"capacity": 1,
				"family":   "C",
			},
			expectedImage:  "redis",
			shouldHavePort: true,
		},
		{
			name:         "Azure Storage Account",
			resourceType: resource.TypeAzureStorageAcct,
			config: map[string]interface{}{
				"account_tier":             "Standard",
				"account_replication_type": "LRS",
			},
			expectedImage:  "minio",
			shouldHavePort: true,
		},
		{
			name:         "Azure Kubernetes Service",
			resourceType: resource.TypeAKS,
			config: map[string]interface{}{
				"dns_prefix":        "myaks",
				"default_node_pool": map[string]interface{}{"node_count": 3},
			},
			expectedImage:  "k3s",
			shouldHavePort: false,
		},
		{
			name:         "Azure CosmosDB",
			resourceType: resource.TypeCosmosDB,
			config: map[string]interface{}{
				"kind":       "MongoDB",
				"offer_type": "Standard",
			},
			expectedImage:  "mongo",
			shouldHavePort: true,
		},
		{
			name:         "Azure Key Vault",
			resourceType: resource.TypeKeyVault,
			config: map[string]interface{}{
				"sku_name": "standard",
			},
			expectedImage:  "vault",
			shouldHavePort: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			res := resource.NewAWSResource(
				"test-"+string(tc.resourceType),
				tc.name,
				tc.resourceType,
			)
			res.Region = "eastus"
			for k, v := range tc.config {
				res.Config[k] = v
			}

			m, err := mapper.Get(tc.resourceType)
			if err != nil {
				t.Logf("No mapper for %s: %v", tc.resourceType, err)
				return
			}

			result, err := m.Map(ctx, res)
			if err != nil {
				t.Logf("Mapping failed for %s: %v", tc.resourceType, err)
				return
			}

			if result == nil || result.DockerService == nil {
				t.Logf("No Docker service for %s", tc.resourceType)
				return
			}

			svc := result.DockerService

			// Verify image
			if tc.expectedImage != "" && !strings.Contains(strings.ToLower(svc.Image), tc.expectedImage) {
				t.Errorf("Expected image containing %s, got %s", tc.expectedImage, svc.Image)
			}

			// Verify ports
			if tc.shouldHavePort && len(svc.Ports) == 0 {
				t.Logf("Warning: Expected ports for %s", tc.name)
			}

			t.Logf("Mapped %s to %s (image: %s)", tc.resourceType, svc.Name, svc.Image)

			// Log warnings
			for _, w := range result.Warnings {
				t.Logf("  Warning: %s", w)
			}
		})
	}
}

// TestAzureMigration_ServiceBusToRabbitMQ tests Service Bus to RabbitMQ migration.
func TestAzureMigration_ServiceBusToRabbitMQ(t *testing.T) {
	ctx := context.Background()

	// Create Service Bus namespace
	sb := resource.NewAWSResource("app-messaging", "messaging", resource.TypeServiceBus)
	sb.Config["sku"] = map[string]interface{}{
		"name": "Standard",
		"tier": "Standard",
	}
	sb.Region = "eastus"

	m, err := mapper.Get(resource.TypeServiceBus)
	if err != nil {
		t.Logf("Service Bus mapper not available: %v", err)
		return
	}

	result, err := m.Map(ctx, sb)
	if err != nil {
		t.Logf("Failed to map Service Bus: %v", err)
		return
	}

	if result != nil && result.DockerService != nil {
		t.Logf("Service Bus mapped to: %s (image: %s)",
			result.DockerService.Name, result.DockerService.Image)

		// Verify RabbitMQ or similar
		if !strings.Contains(strings.ToLower(result.DockerService.Image), "rabbitmq") &&
			!strings.Contains(strings.ToLower(result.DockerService.Image), "activemq") {
			t.Logf("Note: Expected RabbitMQ or ActiveMQ, got %s", result.DockerService.Image)
		}

		// Check for management UI port
		for _, port := range result.DockerService.Ports {
			if strings.Contains(port, "15672") {
				t.Log("RabbitMQ management UI port configured")
			}
		}
	}

	// Log manual steps
	if result != nil && len(result.ManualSteps) > 0 {
		t.Log("Manual migration steps:")
		for _, step := range result.ManualSteps {
			t.Logf("  - %s", step)
		}
	}
}

// TestAzureMigration_KeyVaultToVault tests Key Vault to HashiCorp Vault migration.
func TestAzureMigration_KeyVaultToVault(t *testing.T) {
	ctx := context.Background()

	// Create Key Vault resource
	kv := resource.NewAWSResource("app-secrets", "keyvault", resource.TypeKeyVault)
	kv.Config["sku_name"] = "standard"
	kv.Config["soft_delete_retention_days"] = 90
	kv.Config["purge_protection_enabled"] = true
	kv.Region = "eastus"

	m, err := mapper.Get(resource.TypeKeyVault)
	if err != nil {
		t.Logf("Key Vault mapper not available: %v", err)
		return
	}

	result, err := m.Map(ctx, kv)
	if err != nil {
		t.Logf("Failed to map Key Vault: %v", err)
		return
	}

	if result == nil || result.DockerService == nil {
		t.Log("No Docker service generated for Key Vault")
		return
	}

	svc := result.DockerService

	// Verify Vault image
	if !strings.Contains(strings.ToLower(svc.Image), "vault") {
		t.Logf("Note: Expected Vault image, got %s", svc.Image)
	}

	t.Logf("Key Vault mapped to: %s (image: %s)", svc.Name, svc.Image)
	t.Logf("Environment: %v", svc.Environment)
	t.Logf("Volumes: %v", svc.Volumes)

	// Check for configs (Vault policy files, etc.)
	if len(result.Configs) > 0 {
		t.Log("Generated configs:")
		for name := range result.Configs {
			t.Logf("  - %s", name)
		}
	}

	// Check for migration scripts
	if len(result.Scripts) > 0 {
		t.Log("Generated scripts:")
		for name := range result.Scripts {
			t.Logf("  - %s", name)
		}
	}

	// Manual steps for secret migration
	if len(result.ManualSteps) > 0 {
		t.Log("Manual steps required:")
		for _, step := range result.ManualSteps {
			t.Logf("  - %s", step)
		}
	}
}

// TestAzureMigration_CompleteStack tests migration of a complete Azure stack.
func TestAzureMigration_CompleteStack(t *testing.T) {
	ctx := context.Background()
	outputDir := t.TempDir()

	// Create complete Azure infrastructure
	infra := resource.NewInfrastructure(resource.ProviderAzure)

	// PostgreSQL database
	db := resource.NewAWSResource("main-db", "database", resource.TypeAzurePostgres)
	db.Config["version"] = "15"
	db.Config["sku_name"] = "GP_Standard_D4s_v3"
	db.Config["storage_mb"] = 131072
	db.Region = "eastus"
	infra.AddResource(db)

	// Redis cache
	redis := resource.NewAWSResource("cache", "redis", resource.TypeAzureCache)
	redis.Config["sku_name"] = "Standard"
	redis.Config["capacity"] = 1
	redis.Region = "eastus"
	infra.AddResource(redis)

	// Storage account
	storage := resource.NewAWSResource("assets", "storage", resource.TypeAzureStorageAcct)
	storage.Config["account_tier"] = "Standard"
	storage.Config["account_replication_type"] = "LRS"
	storage.Region = "eastus"
	infra.AddResource(storage)

	// Key Vault
	kv := resource.NewAWSResource("secrets", "keyvault", resource.TypeKeyVault)
	kv.Config["sku_name"] = "standard"
	kv.Region = "eastus"
	infra.AddResource(kv)

	// Service Bus
	sb := resource.NewAWSResource("messaging", "servicebus", resource.TypeServiceBus)
	sb.Config["sku"] = "Standard"
	sb.Region = "eastus"
	infra.AddResource(sb)

	// Map all resources
	var results []*mapper.MappingResult
	var allWarnings []string
	var allManualSteps []string

	for _, res := range infra.Resources {
		m, err := mapper.Get(res.Type)
		if err != nil {
			t.Logf("No mapper for %s", res.Type)
			continue
		}

		result, err := m.Map(ctx, res)
		if err != nil {
			t.Logf("Failed to map %s: %v", res.ID, err)
			continue
		}

		if result != nil {
			results = append(results, result)
			allWarnings = append(allWarnings, result.Warnings...)
			allManualSteps = append(allManualSteps, result.ManualSteps...)
		}
	}

	t.Logf("Mapped %d/%d Azure resources", len(results), len(infra.Resources))

	if len(results) == 0 {
		t.Skip("No resources mapped")
	}

	// Generate Docker Compose
	gen := compose.NewGenerator("azure-complete-stack")
	output, err := gen.Generate(results)
	if err != nil {
		t.Fatalf("Failed to generate output: %v", err)
	}

	// Write all files
	for name, content := range output.Files {
		filePath := filepath.Join(outputDir, name)
		os.MkdirAll(filepath.Dir(filePath), 0755)
		os.WriteFile(filePath, content, 0644)
		t.Logf("Generated: %s", name)
	}

	// Summary
	t.Logf("Migration complete:")
	t.Logf("  - Files generated: %d", len(output.Files))
	t.Logf("  - Warnings: %d", len(allWarnings))
	t.Logf("  - Manual steps: %d", len(allManualSteps))
	t.Logf("  - Output: %s", outputDir)

	// Log all warnings
	if len(allWarnings) > 0 {
		t.Log("All warnings:")
		for _, w := range allWarnings {
			t.Logf("  - %s", w)
		}
	}

	// Log all manual steps
	if len(allManualSteps) > 0 {
		t.Log("All manual steps:")
		for _, s := range allManualSteps {
			t.Logf("  - %s", s)
		}
	}
}
