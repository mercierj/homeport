package azure_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/agnostech/agnostech/internal/domain/parser"
	"github.com/agnostech/agnostech/internal/domain/resource"
	azureparser "github.com/agnostech/agnostech/internal/infrastructure/parser/azure"
)

// TestHCLParserIntegration_BasicAzureProvider tests parsing a basic Terraform HCL file with Azure provider.
func TestHCLParserIntegration_BasicAzureProvider(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hcl-azure-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	hclContent := `provider "azurerm" {
  features {}
}

resource "azurerm_resource_group" "example" {
  name     = "example-resources"
  location = "eastus"
}

resource "azurerm_storage_account" "main" {
  name                     = "mystorageaccount"
  resource_group_name      = azurerm_resource_group.example.name
  location                 = azurerm_resource_group.example.location
  account_tier             = "Standard"
  account_replication_type = "LRS"

  tags = {
    environment = "test"
    project     = "cloudexit"
  }
}
`

	hclPath := filepath.Join(tmpDir, "main.tf")
	if err := os.WriteFile(hclPath, []byte(hclContent), 0644); err != nil {
		t.Fatalf("failed to write HCL file: %v", err)
	}

	p := azureparser.NewHCLParser()

	// Verify provider
	if p.Provider() != resource.ProviderAzure {
		t.Errorf("expected Azure provider, got %s", p.Provider())
	}

	// Verify format
	formats := p.SupportedFormats()
	if len(formats) != 1 || formats[0] != parser.FormatTerraform {
		t.Errorf("expected Terraform format, got %v", formats)
	}

	// Test validation
	if err := p.Validate(hclPath); err != nil {
		t.Errorf("validation failed: %v", err)
	}

	// Test auto-detection
	canHandle, confidence := p.AutoDetect(hclPath)
	if !canHandle {
		t.Error("expected parser to handle HCL file with Azure resources")
	}
	if confidence < 0.8 {
		t.Errorf("expected high confidence, got %f", confidence)
	}

	// Test parsing
	ctx := context.Background()
	opts := parser.NewParseOptions()
	infra, err := p.Parse(ctx, hclPath, opts)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	// Should only include azurerm_ resources (not resource groups typically)
	if len(infra.Resources) < 1 {
		t.Errorf("expected at least 1 resource, got %d", len(infra.Resources))
	}
}

// TestHCLParserIntegration_AzureResourceExtraction tests extraction of various Azure resource types.
func TestHCLParserIntegration_AzureResourceExtraction(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hcl-azure-resources-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	hclContent := `provider "azurerm" {
  features {}
}

resource "azurerm_linux_virtual_machine" "vm" {
  name                = "my-linux-vm"
  resource_group_name = "rg-test"
  location            = "eastus"
  size                = "Standard_DS2_v2"
  admin_username      = "adminuser"

  network_interface_ids = [
    "/subscriptions/xxx/resourceGroups/rg-test/providers/Microsoft.Network/networkInterfaces/nic1"
  ]

  os_disk {
    caching              = "ReadWrite"
    storage_account_type = "Premium_LRS"
  }

  source_image_reference {
    publisher = "Canonical"
    offer     = "UbuntuServer"
    sku       = "22.04-LTS"
    version   = "latest"
  }

  tags = {
    environment = "dev"
  }
}

resource "azurerm_mssql_database" "db" {
  name      = "mydb"
  server_id = "/subscriptions/xxx/resourceGroups/rg-test/providers/Microsoft.Sql/servers/myserver"
  sku_name  = "S0"
}

resource "azurerm_redis_cache" "cache" {
  name                = "myredis"
  location            = "eastus"
  resource_group_name = "rg-test"
  capacity            = 2
  family              = "C"
  sku_name            = "Standard"
}

resource "azurerm_key_vault" "vault" {
  name                = "mykeyvault"
  location            = "eastus"
  resource_group_name = "rg-test"
  tenant_id           = "xxx-tenant-id"
  sku_name            = "standard"
}

resource "azurerm_servicebus_namespace" "sb" {
  name                = "myservicebus"
  location            = "eastus"
  resource_group_name = "rg-test"
  sku                 = "Standard"
}

resource "azurerm_lb" "lb" {
  name                = "mylb"
  location            = "eastus"
  resource_group_name = "rg-test"
}
`

	hclPath := filepath.Join(tmpDir, "resources.tf")
	if err := os.WriteFile(hclPath, []byte(hclContent), 0644); err != nil {
		t.Fatalf("failed to write HCL file: %v", err)
	}

	p := azureparser.NewHCLParser()
	ctx := context.Background()
	opts := parser.NewParseOptions()
	infra, err := p.Parse(ctx, hclPath, opts)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	if len(infra.Resources) != 6 {
		t.Errorf("expected 6 resources, got %d", len(infra.Resources))
	}

	// Verify resource types are correctly mapped
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

// TestHCLParserIntegration_StorageResources tests parsing of Azure storage resources.
func TestHCLParserIntegration_StorageResources(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hcl-storage-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	hclContent := `resource "azurerm_storage_account" "main" {
  name                     = "mystorageaccount"
  resource_group_name      = "rg-test"
  location                 = "eastus"
  account_tier             = "Standard"
  account_replication_type = "GRS"
  account_kind             = "StorageV2"

  blob_properties {
    versioning_enabled = true
    delete_retention_policy {
      days = 7
    }
  }

  network_rules {
    default_action = "Deny"
    ip_rules       = ["100.0.0.1"]
  }
}

resource "azurerm_storage_container" "blob" {
  name                  = "mycontainer"
  storage_account_name  = azurerm_storage_account.main.name
  container_access_type = "private"
}

resource "azurerm_managed_disk" "disk" {
  name                 = "mydisk"
  location             = "eastus"
  resource_group_name  = "rg-test"
  storage_account_type = "Premium_LRS"
  disk_size_gb         = 128
  create_option        = "Empty"
}

resource "azurerm_storage_share" "files" {
  name                 = "myfileshare"
  storage_account_name = azurerm_storage_account.main.name
  quota                = 50
}
`

	hclPath := filepath.Join(tmpDir, "storage.tf")
	if err := os.WriteFile(hclPath, []byte(hclContent), 0644); err != nil {
		t.Fatalf("failed to write HCL file: %v", err)
	}

	p := azureparser.NewHCLParser()
	ctx := context.Background()
	opts := parser.NewParseOptions()
	infra, err := p.Parse(ctx, hclPath, opts)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	// Verify storage resources
	expectedMappings := map[string]resource.Type{
		"azurerm_storage_account.main":   resource.TypeAzureStorageAcct,
		"azurerm_storage_container.blob": resource.TypeBlobStorage,
		"azurerm_managed_disk.disk":      resource.TypeManagedDisk,
		"azurerm_storage_share.files":    resource.TypeAzureFiles,
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

// TestHCLParserIntegration_DatabaseResources tests parsing of Azure database resources.
func TestHCLParserIntegration_DatabaseResources(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hcl-database-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	hclContent := `resource "azurerm_mssql_database" "sql" {
  name      = "mysqldb"
  server_id = "/subscriptions/xxx/resourceGroups/rg-test/providers/Microsoft.Sql/servers/myserver"
  sku_name  = "S0"
}

resource "azurerm_postgresql_flexible_server" "postgres" {
  name                   = "mypostgres"
  resource_group_name    = "rg-test"
  location               = "eastus"
  version                = "14"
  administrator_login    = "psqladmin"
  administrator_password = "H@Sh1CoR3!"
  storage_mb             = 32768
  sku_name               = "GP_Standard_D4s_v3"
}

resource "azurerm_mysql_flexible_server" "mysql" {
  name                   = "mymysql"
  resource_group_name    = "rg-test"
  location               = "eastus"
  administrator_login    = "mysqladmin"
  administrator_password = "H@Sh1CoR3!"
  backup_retention_days  = 7
  sku_name               = "GP_Standard_D2ds_v4"
}

resource "azurerm_cosmosdb_account" "cosmos" {
  name                = "mycosmosdb"
  location            = "eastus"
  resource_group_name = "rg-test"
  offer_type          = "Standard"
  kind                = "GlobalDocumentDB"

  consistency_policy {
    consistency_level = "Session"
  }

  geo_location {
    location          = "eastus"
    failover_priority = 0
  }
}

resource "azurerm_redis_cache" "redis" {
  name                = "myredis"
  location            = "eastus"
  resource_group_name = "rg-test"
  capacity            = 2
  family              = "C"
  sku_name            = "Standard"
  enable_non_ssl_port = false
  minimum_tls_version = "1.2"
}
`

	hclPath := filepath.Join(tmpDir, "databases.tf")
	if err := os.WriteFile(hclPath, []byte(hclContent), 0644); err != nil {
		t.Fatalf("failed to write HCL file: %v", err)
	}

	p := azureparser.NewHCLParser()
	ctx := context.Background()
	opts := parser.NewParseOptions()
	infra, err := p.Parse(ctx, hclPath, opts)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	expectedMappings := map[string]resource.Type{
		"azurerm_mssql_database.sql":                resource.TypeAzureSQL,
		"azurerm_postgresql_flexible_server.postgres": resource.TypeAzurePostgres,
		"azurerm_mysql_flexible_server.mysql":       resource.TypeAzureMySQL,
		"azurerm_cosmosdb_account.cosmos":           resource.TypeCosmosDB,
		"azurerm_redis_cache.redis":                 resource.TypeAzureCache,
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

// TestHCLParserIntegration_NetworkingResources tests parsing of Azure networking resources.
func TestHCLParserIntegration_NetworkingResources(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hcl-networking-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	hclContent := `resource "azurerm_virtual_network" "vnet" {
  name                = "myvnet"
  address_space       = ["10.0.0.0/16"]
  location            = "eastus"
  resource_group_name = "rg-test"
}

resource "azurerm_lb" "lb" {
  name                = "mylb"
  location            = "eastus"
  resource_group_name = "rg-test"
  sku                 = "Standard"
}

resource "azurerm_application_gateway" "appgw" {
  name                = "myappgateway"
  resource_group_name = "rg-test"
  location            = "eastus"

  sku {
    name     = "Standard_v2"
    tier     = "Standard_v2"
    capacity = 2
  }

  gateway_ip_configuration {
    name      = "my-gateway-ip-configuration"
    subnet_id = "/subscriptions/xxx/resourceGroups/rg-test/providers/Microsoft.Network/virtualNetworks/vnet/subnets/subnet1"
  }

  frontend_port {
    name = "http"
    port = 80
  }

  frontend_ip_configuration {
    name                 = "frontend"
    public_ip_address_id = "/subscriptions/xxx/resourceGroups/rg-test/providers/Microsoft.Network/publicIPAddresses/pip1"
  }

  backend_address_pool {
    name = "backend"
  }

  backend_http_settings {
    name                  = "http-settings"
    cookie_based_affinity = "Disabled"
    port                  = 80
    protocol              = "Http"
    request_timeout       = 60
  }

  http_listener {
    name                           = "listener"
    frontend_ip_configuration_name = "frontend"
    frontend_port_name             = "http"
    protocol                       = "Http"
  }

  request_routing_rule {
    name                       = "rule1"
    rule_type                  = "Basic"
    http_listener_name         = "listener"
    backend_address_pool_name  = "backend"
    backend_http_settings_name = "http-settings"
    priority                   = 100
  }
}

resource "azurerm_dns_zone" "dns" {
  name                = "example.com"
  resource_group_name = "rg-test"
}

resource "azurerm_cdn_profile" "cdn" {
  name                = "mycdnprofile"
  location            = "eastus"
  resource_group_name = "rg-test"
  sku                 = "Standard_Microsoft"
}

resource "azurerm_frontdoor" "fd" {
  name                = "myfrontdoor"
  resource_group_name = "rg-test"

  routing_rule {
    name               = "routingrule1"
    accepted_protocols = ["Http", "Https"]
    patterns_to_match  = ["/*"]
    frontend_endpoints = ["frontend1"]
    forwarding_configuration {
      forwarding_protocol = "MatchRequest"
      backend_pool_name   = "backend1"
    }
  }

  backend_pool_load_balancing {
    name = "loadbalancing1"
  }

  backend_pool_health_probe {
    name = "healthprobe1"
  }

  backend_pool {
    name = "backend1"
    backend {
      host_header = "www.example.com"
      address     = "www.example.com"
      http_port   = 80
      https_port  = 443
    }
    load_balancing_name = "loadbalancing1"
    health_probe_name   = "healthprobe1"
  }

  frontend_endpoint {
    name      = "frontend1"
    host_name = "myfrontdoor.azurefd.net"
  }
}
`

	hclPath := filepath.Join(tmpDir, "networking.tf")
	if err := os.WriteFile(hclPath, []byte(hclContent), 0644); err != nil {
		t.Fatalf("failed to write HCL file: %v", err)
	}

	p := azureparser.NewHCLParser()
	ctx := context.Background()
	opts := parser.NewParseOptions()
	infra, err := p.Parse(ctx, hclPath, opts)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	expectedMappings := map[string]resource.Type{
		"azurerm_virtual_network.vnet":      resource.TypeAzureVNet,
		"azurerm_lb.lb":                     resource.TypeAzureLB,
		"azurerm_application_gateway.appgw": resource.TypeAppGateway,
		"azurerm_dns_zone.dns":              resource.TypeAzureDNS,
		"azurerm_cdn_profile.cdn":           resource.TypeAzureCDN,
		"azurerm_frontdoor.fd":              resource.TypeFrontDoor,
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

// TestHCLParserIntegration_DirectoryParsing tests parsing multiple HCL files from a directory.
func TestHCLParserIntegration_DirectoryParsing(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hcl-dir-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create provider file
	providerTf := `provider "azurerm" {
  features {}
}
`

	// Create storage file
	storageTf := `resource "azurerm_storage_account" "main" {
  name                = "mystorageaccount"
  resource_group_name = "rg-test"
  location            = "eastus"
  account_tier        = "Standard"
  account_replication_type = "LRS"
}
`

	// Create compute file
	computeTf := `resource "azurerm_linux_virtual_machine" "vm" {
  name                = "myvm"
  resource_group_name = "rg-test"
  location            = "eastus"
  size                = "Standard_DS2_v2"
  admin_username      = "adminuser"

  network_interface_ids = ["/subscriptions/xxx/resourceGroups/rg-test/providers/Microsoft.Network/networkInterfaces/nic1"]

  os_disk {
    caching              = "ReadWrite"
    storage_account_type = "Premium_LRS"
  }

  source_image_reference {
    publisher = "Canonical"
    offer     = "UbuntuServer"
    sku       = "22.04-LTS"
    version   = "latest"
  }
}
`

	if err := os.WriteFile(filepath.Join(tmpDir, "provider.tf"), []byte(providerTf), 0644); err != nil {
		t.Fatalf("failed to write provider.tf: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "storage.tf"), []byte(storageTf), 0644); err != nil {
		t.Fatalf("failed to write storage.tf: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "compute.tf"), []byte(computeTf), 0644); err != nil {
		t.Fatalf("failed to write compute.tf: %v", err)
	}

	p := azureparser.NewHCLParser()

	// Test directory validation
	if err := p.Validate(tmpDir); err != nil {
		t.Errorf("directory validation failed: %v", err)
	}

	// Test directory auto-detection
	canHandle, confidence := p.AutoDetect(tmpDir)
	if !canHandle {
		t.Error("expected parser to handle directory with Azure TF files")
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

// TestHCLParserIntegration_IgnoresNonAzureResources tests that non-Azure resources are filtered out.
func TestHCLParserIntegration_IgnoresNonAzureResources(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hcl-mixed-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	hclContent := `provider "azurerm" {
  features {}
}

provider "aws" {
  region = "us-east-1"
}

resource "azurerm_storage_account" "azure_storage" {
  name                = "mystorageaccount"
  resource_group_name = "rg-test"
  location            = "eastus"
  account_tier        = "Standard"
  account_replication_type = "LRS"
}

resource "aws_s3_bucket" "aws_bucket" {
  bucket = "my-aws-bucket"
}

resource "azurerm_linux_virtual_machine" "azure_vm" {
  name                = "myvm"
  resource_group_name = "rg-test"
  location            = "eastus"
  size                = "Standard_DS2_v2"
  admin_username      = "adminuser"

  network_interface_ids = ["/subscriptions/xxx/resourceGroups/rg-test/providers/Microsoft.Network/networkInterfaces/nic1"]

  os_disk {
    caching              = "ReadWrite"
    storage_account_type = "Premium_LRS"
  }

  source_image_reference {
    publisher = "Canonical"
    offer     = "UbuntuServer"
    sku       = "22.04-LTS"
    version   = "latest"
  }
}

resource "google_storage_bucket" "gcp_bucket" {
  name     = "my-gcp-bucket"
  location = "US"
}
`

	hclPath := filepath.Join(tmpDir, "mixed.tf")
	if err := os.WriteFile(hclPath, []byte(hclContent), 0644); err != nil {
		t.Fatalf("failed to write HCL file: %v", err)
	}

	p := azureparser.NewHCLParser()
	ctx := context.Background()
	opts := parser.NewParseOptions()
	infra, err := p.Parse(ctx, hclPath, opts)
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

// TestHCLParserIntegration_InvalidPath tests error handling for invalid paths.
func TestHCLParserIntegration_InvalidPath(t *testing.T) {
	p := azureparser.NewHCLParser()

	// Test with non-existent path
	err := p.Validate("/nonexistent/path/main.tf")
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

// TestHCLParserIntegration_NonAzureFile tests validation of TF files without Azure resources.
func TestHCLParserIntegration_NonAzureFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hcl-nonazure-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a TF file with only AWS resources
	awsContent := `provider "aws" {
  region = "us-east-1"
}

resource "aws_s3_bucket" "bucket" {
  bucket = "my-bucket"
}
`

	hclPath := filepath.Join(tmpDir, "aws.tf")
	if err := os.WriteFile(hclPath, []byte(awsContent), 0644); err != nil {
		t.Fatalf("failed to write HCL file: %v", err)
	}

	p := azureparser.NewHCLParser()

	// Validate should fail for non-Azure TF file
	err = p.Validate(hclPath)
	if err != parser.ErrUnsupportedFormat {
		t.Errorf("expected ErrUnsupportedFormat for non-Azure TF file, got %v", err)
	}

	// Auto-detect should return false
	canHandle, _ := p.AutoDetect(hclPath)
	if canHandle {
		t.Error("expected false for non-Azure TF file")
	}
}

// TestHCLParserIntegration_MessagingResources tests parsing of Azure messaging resources.
func TestHCLParserIntegration_MessagingResources(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hcl-messaging-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	hclContent := `resource "azurerm_servicebus_namespace" "sb" {
  name                = "myservicebus"
  location            = "eastus"
  resource_group_name = "rg-test"
  sku                 = "Standard"
}

resource "azurerm_servicebus_queue" "queue" {
  name         = "myqueue"
  namespace_id = azurerm_servicebus_namespace.sb.id
}

resource "azurerm_eventhub_namespace" "eventhub" {
  name                = "myeventhub"
  location            = "eastus"
  resource_group_name = "rg-test"
  sku                 = "Standard"
  capacity            = 1
}

resource "azurerm_eventgrid_topic" "eventgrid" {
  name                = "myeventgrid"
  location            = "eastus"
  resource_group_name = "rg-test"
}

resource "azurerm_logic_app_workflow" "logicapp" {
  name                = "mylogicapp"
  location            = "eastus"
  resource_group_name = "rg-test"
}
`

	hclPath := filepath.Join(tmpDir, "messaging.tf")
	if err := os.WriteFile(hclPath, []byte(hclContent), 0644); err != nil {
		t.Fatalf("failed to write HCL file: %v", err)
	}

	p := azureparser.NewHCLParser()
	ctx := context.Background()
	opts := parser.NewParseOptions()
	infra, err := p.Parse(ctx, hclPath, opts)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	expectedMappings := map[string]resource.Type{
		"azurerm_servicebus_namespace.sb":  resource.TypeServiceBus,
		"azurerm_servicebus_queue.queue":   resource.TypeServiceBusQueue,
		"azurerm_eventhub_namespace.eventhub": resource.TypeEventHub,
		"azurerm_eventgrid_topic.eventgrid":   resource.TypeEventGrid,
		"azurerm_logic_app_workflow.logicapp": resource.TypeLogicApp,
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
