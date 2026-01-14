// Package storage provides mappers for Azure storage services.
package storage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

// StorageAccountMapper converts Azure Storage Accounts to Azurite.
type StorageAccountMapper struct {
	*mapper.BaseMapper
}

// NewStorageAccountMapper creates a new Azure Storage Account to Azurite mapper.
func NewStorageAccountMapper() *StorageAccountMapper {
	return &StorageAccountMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeAzureStorageAcct, nil),
	}
}

// Map converts an Azure Storage Account to an Azurite service.
func (m *StorageAccountMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	accountName := res.GetConfigString("name")
	if accountName == "" {
		accountName = res.Name
	}

	result := mapper.NewMappingResult(m.sanitizeName(accountName))
	svc := result.DockerService

	// Configure Azurite service - Full Azure Storage emulator
	svc.Image = "mcr.microsoft.com/azure-storage/azurite:latest"
	svc.Command = []string{
		"azurite",
		"--blobHost", "0.0.0.0",
		"--queueHost", "0.0.0.0",
		"--tableHost", "0.0.0.0",
		"--loose",
		"--skipApiVersionCheck",
	}
	svc.Environment = map[string]string{
		"AZURITE_ACCOUNTS": fmt.Sprintf("%s:Eby8vdM09T0v9L3gP8Z0VGBKw5RZFV3Z", accountName),
	}
	svc.Ports = []string{
		"10000:10000", // Blob service
		"10001:10001", // Queue service
		"10002:10002", // Table service
	}
	svc.Volumes = []string{
		fmt.Sprintf("./data/azurite/%s:/data", accountName),
	}
	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "nc", "-z", "localhost", "10000"},
		Interval: 30 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  3,
	}
	svc.Labels = map[string]string{
		"homeport.source":         "azurerm_storage_account",
		"homeport.account_name":   accountName,
		"homeport.service_type":   "azurite",
		"traefik.enable":           "false",
	}

	// Handle account tier
	accountTier := res.GetConfigString("account_tier")
	if accountTier == "" {
		accountTier = "Standard"
	}
	svc.Labels["homeport.account_tier"] = accountTier

	// Handle replication type
	replicationType := res.GetConfigString("account_replication_type")
	if replicationType == "" {
		replicationType = "LRS"
	}
	svc.Labels["homeport.replication_type"] = replicationType

	if replicationType != "LRS" {
		result.AddWarning(fmt.Sprintf("Replication type '%s' is configured. Azurite is single-instance only (LRS equivalent).", replicationType))
	}

	// Handle access tier
	accessTier := res.GetConfigString("access_tier")
	if accessTier != "" {
		svc.Labels["homeport.access_tier"] = accessTier
		result.AddWarning(fmt.Sprintf("Access tier '%s' is configured. Azurite doesn't differentiate between Hot/Cool/Archive tiers.", accessTier))
	}

	// Handle account kind
	accountKind := res.GetConfigString("account_kind")
	if accountKind == "" {
		accountKind = "StorageV2"
	}
	svc.Labels["homeport.account_kind"] = accountKind

	if accountKind == "BlobStorage" {
		result.AddWarning("BlobStorage account kind detected. Azurite supports all storage types (blob, queue, table).")
	}

	// Handle HTTPS-only traffic
	enableHTTPSOnly := res.GetConfigBool("enable_https_traffic_only")
	if enableHTTPSOnly {
		result.AddWarning("HTTPS-only traffic is enabled. Azurite by default uses HTTP. Consider using a reverse proxy for HTTPS.")
		result.AddManualStep("Configure HTTPS using a reverse proxy (nginx, Traefik) for production-like testing")
	}

	// Handle minimum TLS version
	minTLSVersion := res.GetConfigString("min_tls_version")
	if minTLSVersion != "" {
		result.AddWarning(fmt.Sprintf("Minimum TLS version '%s' is configured. Configure this on your reverse proxy.", minTLSVersion))
	}

	// Handle blob properties
	if blobProps := res.Config["blob_properties"]; blobProps != nil {
		m.handleBlobProperties(blobProps, result)
	}

	// Handle network rules
	if networkRules := res.Config["network_rules"]; networkRules != nil {
		m.handleNetworkRules(networkRules, result)
	}

	// Handle static website
	if staticWebsite := res.Config["static_website"]; staticWebsite != nil {
		m.handleStaticWebsite(staticWebsite, result)
	}

	// Handle identity
	if identity := res.Config["identity"]; identity != nil {
		result.AddWarning("Managed identity is configured. Configure equivalent service credentials for local testing.")
	}

	// Generate setup script
	setupScript := m.generateSetupScript(accountName)
	result.AddScript(fmt.Sprintf("setup_%s.sh", accountName), []byte(setupScript))

	// Generate connection string documentation
	connectionDoc := m.generateConnectionDoc(accountName)
	result.AddConfig(fmt.Sprintf("config/%s-connection.txt", accountName), []byte(connectionDoc))

	result.AddManualStep(fmt.Sprintf("Access blob storage at: http://localhost:10000/%s", accountName))
	result.AddManualStep("Use Azure Storage Explorer or Azure CLI with Azurite connection string")
	result.AddManualStep("Default account key: Eby8vdM09T0v9L3gP8Z0VGBKw5RZFV3Z")

	return result, nil
}

// handleBlobProperties processes blob properties configuration.
func (m *StorageAccountMapper) handleBlobProperties(props interface{}, result *mapper.MappingResult) {
	if propsMap, ok := props.(map[string]interface{}); ok {
		// Handle versioning
		if versioning, ok := propsMap["versioning_enabled"].(bool); ok && versioning {
			result.AddWarning("Blob versioning is enabled. Azurite has limited versioning support.")
		}

		// Handle change feed
		if changeFeed, ok := propsMap["change_feed_enabled"].(bool); ok && changeFeed {
			result.AddWarning("Change feed is enabled. Azurite doesn't support change feed.")
		}

		// Handle soft delete
		if deleteRetention := propsMap["delete_retention_policy"]; deleteRetention != nil {
			result.AddWarning("Blob soft delete is configured. Azurite has limited soft delete support.")
		}

		// Handle container soft delete
		if containerRetention := propsMap["container_delete_retention_policy"]; containerRetention != nil {
			result.AddWarning("Container soft delete is configured. Azurite has limited soft delete support.")
		}
	}
}

// handleNetworkRules processes network rules configuration.
func (m *StorageAccountMapper) handleNetworkRules(rules interface{}, result *mapper.MappingResult) {
	if rulesMap, ok := rules.(map[string]interface{}); ok {
		defaultAction, _ := rulesMap["default_action"].(string)
		if defaultAction == "Deny" {
			result.AddWarning("Network rules with default action 'Deny' are configured. Azurite doesn't enforce network rules - use Docker network policies if needed.")
		}

		if ipRules := rulesMap["ip_rules"]; ipRules != nil {
			result.AddWarning("IP rules are configured. Implement using Docker network policies or firewall rules if needed.")
		}

		if vnetRules := rulesMap["virtual_network_subnet_ids"]; vnetRules != nil {
			result.AddWarning("Virtual network rules are configured. Map to Docker networks as appropriate.")
		}
	}
}

// handleStaticWebsite processes static website configuration.
func (m *StorageAccountMapper) handleStaticWebsite(website interface{}, result *mapper.MappingResult) {
	if websiteMap, ok := website.(map[string]interface{}); ok {
		indexDoc, _ := websiteMap["index_document"].(string)
		errorDoc, _ := websiteMap["error_404_document"].(string)

		result.AddWarning("Static website hosting is configured. Azurite doesn't support static website hosting - consider using nginx or Caddy to serve the $web container.")

		if indexDoc != "" || errorDoc != "" {
			staticWebsiteDoc := fmt.Sprintf(`# Static Website Configuration
Index document: %s
Error document: %s

To serve this as a static website with Docker:
1. Use nginx or Caddy to serve the $web container
2. Configure index and error documents in the web server
`, indexDoc, errorDoc)
			result.AddConfig("config/static-website.txt", []byte(staticWebsiteDoc))
		}
	}
}

// generateSetupScript creates an Azurite setup script.
func (m *StorageAccountMapper) generateSetupScript(accountName string) string {
	return fmt.Sprintf(`#!/bin/bash
# Azurite Setup Script for Azure Storage Account: %s

set -e

echo "Starting Azurite storage emulator..."
echo ""
echo "Storage account name: %s"
echo "Default account key: Eby8vdM09T0v9L3gP8Z0VGBKw5RZFV3Z"
echo ""
echo "Services available at:"
echo "  Blob:  http://localhost:10000/%s"
echo "  Queue: http://localhost:10001/%s"
echo "  Table: http://localhost:10002/%s"
echo ""
echo "Connection string:"
echo "DefaultEndpointsProtocol=http;AccountName=%s;AccountKey=Eby8vdM09T0v9L3gP8Z0VGBKw5RZFV3Z;BlobEndpoint=http://localhost:10000/%s;QueueEndpoint=http://localhost:10001/%s;TableEndpoint=http://localhost:10002/%s;"
echo ""
echo "Use Azure Storage Explorer or Azure CLI to interact with Azurite"
echo "Example: az storage container list --connection-string '<connection-string>'"
`, accountName, accountName, accountName, accountName, accountName, accountName, accountName, accountName, accountName)
}

// generateConnectionDoc creates connection string documentation.
func (m *StorageAccountMapper) generateConnectionDoc(accountName string) string {
	return fmt.Sprintf(`Azure Storage Account: %s
Azurite Connection Information

Connection String:
DefaultEndpointsProtocol=http;AccountName=%s;AccountKey=Eby8vdM09T0v9L3gP8Z0VGBKw5RZFV3Z;BlobEndpoint=http://localhost:10000/%s;QueueEndpoint=http://localhost:10001/%s;TableEndpoint=http://localhost:10002/%s;

Endpoints:
- Blob Service:  http://localhost:10000/%s
- Queue Service: http://localhost:10001/%s
- Table Service: http://localhost:10002/%s

Account Name: %s
Account Key: Eby8vdM09T0v9L3gP8Z0VGBKw5RZFV3Z

Usage with Azure SDK:
- Set the connection string in your application configuration
- The SDK will automatically use Azurite endpoints
- All Azure Storage operations are supported (with some limitations)

Usage with Azure CLI:
az storage container list --connection-string "DefaultEndpointsProtocol=http;AccountName=%s;AccountKey=Eby8vdM09T0v9L3gP8Z0VGBKw5RZFV3Z;BlobEndpoint=http://localhost:10000/%s;"

Usage with Azure Storage Explorer:
1. Open Azure Storage Explorer
2. Connect to local emulator
3. Use the connection string above
`, accountName, accountName, accountName, accountName, accountName, accountName, accountName, accountName, accountName, accountName, accountName)
}

// sanitizeName ensures the name is valid for Docker service names.
func (m *StorageAccountMapper) sanitizeName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "_", "-")
	validName := ""
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' {
			validName += string(ch)
		}
	}
	validName = strings.TrimLeft(validName, "-0123456789")
	if validName == "" {
		validName = "azurite"
	}
	return validName
}
