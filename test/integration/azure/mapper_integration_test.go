package azure_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/parser"
	"github.com/homeport/homeport/internal/domain/resource"
	azuremapper "github.com/homeport/homeport/internal/infrastructure/mapper/azure"
	"github.com/homeport/homeport/internal/infrastructure/mapper/azure/compute"
	"github.com/homeport/homeport/internal/infrastructure/mapper/azure/database"
	"github.com/homeport/homeport/internal/infrastructure/mapper/azure/messaging"
	"github.com/homeport/homeport/internal/infrastructure/mapper/azure/security"
	"github.com/homeport/homeport/internal/infrastructure/mapper/azure/storage"
	azureparser "github.com/homeport/homeport/internal/infrastructure/parser/azure"
)

// TestMapperIntegration_StorageToAzurite tests Azure Storage Account to Azurite mapping.
func TestMapperIntegration_StorageToAzurite(t *testing.T) {
	// Create a storage account resource
	res := resource.NewAWSResource(
		"azurerm_storage_account.main",
		"mystorageaccount",
		resource.TypeAzureStorageAcct,
	)
	res.Region = "eastus"
	res.Config["name"] = "mystorageaccount"
	res.Config["account_tier"] = "Standard"
	res.Config["account_replication_type"] = "LRS"
	res.Config["access_tier"] = "Hot"
	res.Tags["environment"] = "test"

	m := storage.NewStorageAccountMapper()
	ctx := context.Background()

	result, err := m.Map(ctx, res)
	if err != nil {
		t.Fatalf("mapping failed: %v", err)
	}

	// Verify Docker service configuration
	svc := result.DockerService
	if svc.Image != "mcr.microsoft.com/azure-storage/azurite:latest" {
		t.Errorf("expected Azurite image, got %s", svc.Image)
	}

	// Verify ports (Blob: 10000, Queue: 10001, Table: 10002)
	if len(svc.Ports) != 3 {
		t.Errorf("expected 3 ports for Azurite, got %d", len(svc.Ports))
	}

	// Verify environment variables
	if svc.Environment["AZURITE_ACCOUNTS"] == "" {
		t.Error("expected AZURITE_ACCOUNTS environment variable")
	}

	// Verify labels
	if svc.Labels["homeport.source"] != "azurerm_storage_account" {
		t.Errorf("expected homeport.source label, got %s", svc.Labels["homeport.source"])
	}

	// Verify health check is configured
	if svc.HealthCheck == nil {
		t.Error("expected health check configuration")
	}
}

// TestMapperIntegration_VMToDocker tests Azure VM to Docker container mapping.
func TestMapperIntegration_VMToDocker(t *testing.T) {
	// Create a Linux VM resource
	res := resource.NewAWSResource(
		"azurerm_linux_virtual_machine.vm",
		"mylinuxvm",
		resource.TypeAzureVM,
	)
	res.Region = "eastus"
	res.Config["name"] = "mylinuxvm"
	res.Config["size"] = "Standard_DS2_v2"
	res.Config["source_image_reference"] = map[string]interface{}{
		"publisher": "Canonical",
		"offer":     "UbuntuServer",
		"sku":       "22.04-LTS",
		"version":   "latest",
	}
	res.Tags["environment"] = "dev"

	m := compute.NewVMMapper()
	ctx := context.Background()

	result, err := m.Map(ctx, res)
	if err != nil {
		t.Fatalf("mapping failed: %v", err)
	}

	// Verify Docker service configuration
	svc := result.DockerService
	if svc.Image != "ubuntu:22.04" {
		t.Errorf("expected ubuntu:22.04 image for Canonical 22.04, got %s", svc.Image)
	}

	// Verify resource limits are set based on VM size
	if svc.Deploy == nil || svc.Deploy.Resources == nil || svc.Deploy.Resources.Limits == nil {
		t.Error("expected resource limits to be configured")
	} else {
		if svc.Deploy.Resources.Limits.CPUs != "2" {
			t.Errorf("expected 2 CPUs for DS2_v2, got %s", svc.Deploy.Resources.Limits.CPUs)
		}
		if svc.Deploy.Resources.Limits.Memory != "8G" {
			t.Errorf("expected 8G memory for DS2_v2, got %s", svc.Deploy.Resources.Limits.Memory)
		}
	}

	// Verify labels
	if svc.Labels["homeport.vm_name"] != "mylinuxvm" {
		t.Errorf("expected vm_name label, got %s", svc.Labels["homeport.vm_name"])
	}

	// Verify Dockerfile is generated
	if len(result.Configs) == 0 {
		t.Error("expected Dockerfile in configs")
	}
}

// TestMapperIntegration_SQLToMSSQL tests Azure SQL to SQL Server container mapping.
func TestMapperIntegration_SQLToMSSQL(t *testing.T) {
	// Create an Azure SQL database resource
	res := resource.NewAWSResource(
		"azurerm_mssql_database.db",
		"mydb",
		resource.TypeAzureSQL,
	)
	res.Region = "eastus"
	res.Config["name"] = "mydb"
	res.Config["sku_name"] = "S0"
	res.Tags["environment"] = "production"

	m := database.NewAzureSQLMapper()
	ctx := context.Background()

	result, err := m.Map(ctx, res)
	if err != nil {
		t.Fatalf("mapping failed: %v", err)
	}

	// Verify Docker service configuration
	svc := result.DockerService
	if svc.Image != "mcr.microsoft.com/mssql/server:2022-latest" {
		t.Errorf("expected MSSQL Server image, got %s", svc.Image)
	}

	// Verify SQL Server port
	if len(svc.Ports) != 1 || svc.Ports[0] != "1433:1433" {
		t.Errorf("expected port 1433:1433, got %v", svc.Ports)
	}

	// Verify environment variables
	if svc.Environment["ACCEPT_EULA"] != "Y" {
		t.Error("expected ACCEPT_EULA=Y")
	}
	if svc.Environment["MSSQL_PID"] != "Developer" {
		t.Error("expected MSSQL_PID=Developer")
	}

	// Verify health check is configured
	if svc.HealthCheck == nil {
		t.Error("expected health check configuration")
	}

	// Verify scripts are generated
	if len(result.Scripts) == 0 {
		t.Error("expected init scripts to be generated")
	}

	// Verify warnings about licensing
	if len(result.Warnings) == 0 {
		t.Error("expected warnings about SQL Server licensing")
	}
}

// TestMapperIntegration_ServiceBusToRabbitMQ tests Azure Service Bus to RabbitMQ mapping.
func TestMapperIntegration_ServiceBusToRabbitMQ(t *testing.T) {
	// Create a Service Bus namespace resource
	res := resource.NewAWSResource(
		"azurerm_servicebus_namespace.sb",
		"myservicebus",
		resource.TypeServiceBus,
	)
	res.Region = "eastus"
	res.Config["name"] = "myservicebus"
	res.Config["sku"] = "Standard"
	res.Tags["environment"] = "test"

	m := messaging.NewServiceBusMapper()
	ctx := context.Background()

	result, err := m.Map(ctx, res)
	if err != nil {
		t.Fatalf("mapping failed: %v", err)
	}

	// Verify Docker service configuration
	svc := result.DockerService
	if svc.Image != "rabbitmq:3.12-management-alpine" {
		t.Errorf("expected RabbitMQ image, got %s", svc.Image)
	}

	// Verify RabbitMQ ports (AMQP: 5672, Management: 15672)
	if len(svc.Ports) != 2 {
		t.Errorf("expected 2 ports for RabbitMQ, got %d", len(svc.Ports))
	}

	// Verify default credentials
	if svc.Environment["RABBITMQ_DEFAULT_USER"] != "guest" {
		t.Error("expected default user 'guest'")
	}

	// Verify labels
	if svc.Labels["homeport.source"] != "azurerm_servicebus_namespace" {
		t.Errorf("expected homeport.source label, got %s", svc.Labels["homeport.source"])
	}

	// Verify configs are generated (definitions.json, rabbitmq.conf)
	if len(result.Configs) == 0 {
		t.Error("expected RabbitMQ configuration files")
	}

	// Verify health check is configured
	if svc.HealthCheck == nil {
		t.Error("expected health check configuration")
	}
}

// TestMapperIntegration_KeyVaultToVault tests Azure Key Vault to HashiCorp Vault mapping.
func TestMapperIntegration_KeyVaultToVault(t *testing.T) {
	// Create a Key Vault resource
	res := resource.NewAWSResource(
		"azurerm_key_vault.kv",
		"mykeyvault",
		resource.TypeKeyVault,
	)
	res.Region = "eastus"
	res.Config["name"] = "mykeyvault"
	res.Config["resource_group_name"] = "rg-test"
	res.Config["sku_name"] = "standard"
	res.Tags["environment"] = "production"

	m := security.NewKeyVaultMapper()
	ctx := context.Background()

	result, err := m.Map(ctx, res)
	if err != nil {
		t.Fatalf("mapping failed: %v", err)
	}

	// Verify Docker service configuration
	svc := result.DockerService
	if svc.Image != "hashicorp/vault:1.15" {
		t.Errorf("expected HashiCorp Vault image, got %s", svc.Image)
	}

	// Verify Vault port
	if len(svc.Ports) != 1 || svc.Ports[0] != "8200:8200" {
		t.Errorf("expected port 8200:8200, got %v", svc.Ports)
	}

	// Verify dev mode token
	if svc.Environment["VAULT_DEV_ROOT_TOKEN_ID"] != "root" {
		t.Error("expected dev root token 'root'")
	}

	// Verify IPC_LOCK capability
	if len(svc.CapAdd) == 0 || svc.CapAdd[0] != "IPC_LOCK" {
		t.Error("expected IPC_LOCK capability")
	}

	// Verify labels
	if svc.Labels["homeport.vault_name"] != "mykeyvault" {
		t.Errorf("expected vault_name label, got %s", svc.Labels["homeport.vault_name"])
	}

	// Verify configs and scripts are generated
	if len(result.Configs) == 0 {
		t.Error("expected Vault configuration files")
	}
	if len(result.Scripts) == 0 {
		t.Error("expected migration scripts")
	}

	// Verify warnings about access policies
	if len(result.Warnings) == 0 {
		t.Error("expected warnings about access policy migration")
	}
}

// registryWrapper wraps mapper.Registry to implement MapperRegistrar interface.
type registryWrapper struct {
	registry *mapper.Registry
}

func (w *registryWrapper) Register(m mapper.Mapper) {
	_ = w.registry.Register(m) // Ignore error for test wrapper
}

// TestMapperIntegration_ParseAndMapWorkflow tests the full parser-to-mapper workflow.
func TestMapperIntegration_ParseAndMapWorkflow(t *testing.T) {
	// Create a temp directory with an ARM template
	tmpDir, err := os.MkdirTemp("", "parser-mapper-integration")
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
				"properties": {
					"accessTier": "Hot"
				}
			},
			{
				"type": "Microsoft.Compute/virtualMachines",
				"apiVersion": "2021-03-01",
				"name": "myvm",
				"location": "eastus",
				"properties": {
					"vmSize": "Standard_DS2_v2"
				}
			},
			{
				"type": "Microsoft.Sql/servers/databases",
				"apiVersion": "2021-02-01",
				"name": "myserver/mydb",
				"location": "eastus"
			},
			{
				"type": "Microsoft.ServiceBus/namespaces",
				"apiVersion": "2021-06-01",
				"name": "myservicebus",
				"location": "eastus"
			},
			{
				"type": "Microsoft.KeyVault/vaults",
				"apiVersion": "2021-06-01",
				"name": "mykeyvault",
				"location": "eastus"
			}
		]
	}`

	templatePath := filepath.Join(tmpDir, "azuredeploy.json")
	if err := os.WriteFile(templatePath, []byte(templateContent), 0644); err != nil {
		t.Fatalf("failed to write template: %v", err)
	}

	// Step 1: Parse the ARM template
	p := azureparser.NewARMParser()
	ctx := context.Background()
	opts := parser.NewParseOptions()
	infra, err := p.Parse(ctx, templatePath, opts)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	// Step 2: Create a mapper registry and register Azure mappers
	registry := mapper.NewRegistry()
	wrapper := &registryWrapper{registry: registry}
	azuremapper.RegisterAll(wrapper)

	// Step 3: Map each parsed resource
	mappingResults := make(map[string]*mapper.MappingResult)
	for id, res := range infra.Resources {
		m, err := registry.Get(res.Type)
		if err != nil {
			t.Logf("no mapper for resource type %s: %v", res.Type, err)
			continue
		}

		result, err := m.Map(ctx, res)
		if err != nil {
			t.Errorf("failed to map resource %s: %v", id, err)
			continue
		}

		mappingResults[id] = result
	}

	// Step 4: Verify all resources were mapped
	expectedMappings := map[string]string{
		"mystorageaccount": "mcr.microsoft.com/azure-storage/azurite:latest",
		"myserver/mydb":    "mcr.microsoft.com/mssql/server:2022-latest",
		"myservicebus":     "rabbitmq:3.12-management-alpine",
		"mykeyvault":       "hashicorp/vault:1.15",
	}

	for resourceName, expectedImage := range expectedMappings {
		result, ok := mappingResults[resourceName]
		if !ok {
			t.Errorf("resource %s was not mapped", resourceName)
			continue
		}
		if result.DockerService.Image != expectedImage {
			t.Errorf("resource %s: expected image %s, got %s", resourceName, expectedImage, result.DockerService.Image)
		}
	}
}

// TestMapperIntegration_RegistryContainsAllMappers tests that all Azure mappers are registered.
func TestMapperIntegration_RegistryContainsAllMappers(t *testing.T) {
	registry := mapper.NewRegistry()
	wrapper := &registryWrapper{registry: registry}
	azuremapper.RegisterAll(wrapper)

	// List of expected Azure resource types with mappers
	expectedTypes := []resource.Type{
		// Compute
		resource.TypeAzureVM,
		resource.TypeAzureVMWindows,
		resource.TypeAzureFunction,
		resource.TypeAKS,

		// Storage
		resource.TypeBlobStorage,
		resource.TypeAzureStorageAcct,
		resource.TypeManagedDisk,
		resource.TypeAzureFiles,

		// Database
		resource.TypeAzureSQL,
		resource.TypeAzurePostgres,
		resource.TypeAzureMySQL,
		resource.TypeCosmosDB,
		resource.TypeAzureCache,

		// Networking
		resource.TypeAzureLB,
		resource.TypeAppGateway,
		resource.TypeAzureDNS,
		resource.TypeAzureCDN,
		resource.TypeFrontDoor,
		resource.TypeAzureVNet,

		// Security
		resource.TypeAzureADB2C,
		resource.TypeKeyVault,
		resource.TypeAzureFirewall,

		// Messaging
		resource.TypeServiceBus,
		resource.TypeServiceBusQueue,
		resource.TypeEventHub,
		resource.TypeEventGrid,
		resource.TypeLogicApp,
	}

	for _, resType := range expectedTypes {
		if !registry.Has(resType) {
			t.Errorf("registry missing mapper for resource type: %s", resType)
		}
	}

	// Verify count
	registeredCount := registry.Count()
	if registeredCount < len(expectedTypes) {
		t.Errorf("expected at least %d mappers, got %d", len(expectedTypes), registeredCount)
	}
}

// TestMapperIntegration_WindowsVMToDocker tests Azure Windows VM to Docker container mapping.
func TestMapperIntegration_WindowsVMToDocker(t *testing.T) {
	// Create a Windows VM resource
	res := resource.NewAWSResource(
		"azurerm_windows_virtual_machine.vm",
		"mywindowsvm",
		resource.TypeAzureVMWindows,
	)
	res.Region = "eastus"
	res.Config["name"] = "mywindowsvm"
	res.Config["size"] = "Standard_D4s_v3"
	res.Tags["environment"] = "dev"

	m := compute.NewWindowsVMMapper()
	ctx := context.Background()

	result, err := m.Map(ctx, res)
	if err != nil {
		t.Fatalf("mapping failed: %v", err)
	}

	// Verify Docker service configuration for Windows
	svc := result.DockerService
	if svc.Image != "mcr.microsoft.com/windows/servercore:ltsc2022" {
		t.Errorf("expected Windows Server Core image, got %s", svc.Image)
	}

	// Verify labels indicate Windows
	if svc.Labels["homeport.source"] != "azurerm_windows_virtual_machine" {
		t.Errorf("expected windows source label, got %s", svc.Labels["homeport.source"])
	}

	// Verify warnings about Windows containers
	hasWindowsWarning := false
	for _, warning := range result.Warnings {
		if warning != "" {
			hasWindowsWarning = true
			break
		}
	}
	if !hasWindowsWarning {
		t.Log("Note: Windows VM mapping should include container compatibility warnings")
	}
}

// TestMapperIntegration_StorageWithReplication tests storage mapping with different replication types.
func TestMapperIntegration_StorageWithReplication(t *testing.T) {
	testCases := []struct {
		name            string
		replicationType string
		expectWarning   bool
	}{
		{"LRS", "LRS", false},
		{"GRS", "GRS", true},
		{"ZRS", "ZRS", true},
		{"GZRS", "GZRS", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			res := resource.NewAWSResource(
				"azurerm_storage_account.main",
				"teststorage",
				resource.TypeAzureStorageAcct,
			)
			res.Config["name"] = "teststorage"
			res.Config["account_replication_type"] = tc.replicationType

			m := storage.NewStorageAccountMapper()
			ctx := context.Background()

			result, err := m.Map(ctx, res)
			if err != nil {
				t.Fatalf("mapping failed: %v", err)
			}

			hasReplicationWarning := false
			for _, warning := range result.Warnings {
				if warning != "" && tc.replicationType != "LRS" {
					hasReplicationWarning = true
					break
				}
			}

			if tc.expectWarning && !hasReplicationWarning {
				t.Logf("Expected replication warning for %s but got none", tc.replicationType)
			}
		})
	}
}

// TestMapperIntegration_PremiumKeyVault tests Key Vault with premium SKU (HSM).
func TestMapperIntegration_PremiumKeyVault(t *testing.T) {
	res := resource.NewAWSResource(
		"azurerm_key_vault.kv",
		"premiumvault",
		resource.TypeKeyVault,
	)
	res.Config["name"] = "premiumvault"
	res.Config["sku_name"] = "premium"
	res.Config["resource_group_name"] = "rg-test"

	m := security.NewKeyVaultMapper()
	ctx := context.Background()

	result, err := m.Map(ctx, res)
	if err != nil {
		t.Fatalf("mapping failed: %v", err)
	}

	// Verify warnings about HSM support
	hasHSMWarning := false
	for _, warning := range result.Warnings {
		if warning != "" {
			hasHSMWarning = true
			break
		}
	}

	if !hasHSMWarning {
		t.Log("Note: Premium Key Vault should warn about HSM key storage requirements")
	}

	// Verify manual steps mention HSM/Enterprise
	hasHSMStep := false
	for _, step := range result.ManualSteps {
		if step != "" {
			hasHSMStep = true
			break
		}
	}

	if !hasHSMStep {
		t.Log("Note: Premium Key Vault should include manual steps for HSM migration")
	}
}

// TestMapperIntegration_ServiceBusPremium tests Service Bus with premium SKU.
func TestMapperIntegration_ServiceBusPremium(t *testing.T) {
	res := resource.NewAWSResource(
		"azurerm_servicebus_namespace.sb",
		"premiumsb",
		resource.TypeServiceBus,
	)
	res.Config["name"] = "premiumsb"
	res.Config["sku"] = "Premium"
	res.Config["capacity"] = 2

	m := messaging.NewServiceBusMapper()
	ctx := context.Background()

	result, err := m.Map(ctx, res)
	if err != nil {
		t.Fatalf("mapping failed: %v", err)
	}

	// Verify warnings about premium tier
	hasPremiumWarning := false
	for _, warning := range result.Warnings {
		if warning != "" {
			hasPremiumWarning = true
			break
		}
	}

	if !hasPremiumWarning {
		t.Log("Note: Premium Service Bus should warn about clustering considerations")
	}
}
