package azure

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v5"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/redis/armredis/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/sql/armsql"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage"

	"github.com/cloudexit/cloudexit/internal/domain/parser"
	"github.com/cloudexit/cloudexit/internal/domain/resource"
)

// APIParser discovers Azure infrastructure via API calls.
type APIParser struct {
	credConfig     *CredentialConfig
	subscriptionID string
	identity       *CallerIdentity
}

// NewAPIParser creates a new Azure API parser.
func NewAPIParser() *APIParser {
	return &APIParser{
		credConfig: NewCredentialConfig(),
	}
}

// WithCredentials sets the credential configuration.
func (p *APIParser) WithCredentials(cfg *CredentialConfig) *APIParser {
	p.credConfig = cfg
	return p
}

// Provider returns the cloud provider.
func (p *APIParser) Provider() resource.Provider {
	return resource.ProviderAzure
}

// SupportedFormats returns the supported formats.
func (p *APIParser) SupportedFormats() []parser.Format {
	return []parser.Format{parser.FormatAPI}
}

// Validate checks if the parser can connect to Azure.
func (p *APIParser) Validate(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	subID, err := p.credConfig.GetSubscriptionID()
	if err != nil {
		return fmt.Errorf("failed to get subscription ID: %w", err)
	}
	p.subscriptionID = subID

	_, err = p.credConfig.GetCredential(ctx)
	if err != nil {
		return fmt.Errorf("failed to get credentials: %w", err)
	}

	p.identity = &CallerIdentity{
		SubscriptionID: subID,
	}

	return nil
}

// AutoDetect checks for Azure credentials availability.
func (p *APIParser) AutoDetect(path string) (bool, float64) {
	source := DetectCredentialSource()
	if source != CredentialSourceDefault {
		return true, 0.7
	}

	// Try to validate default credentials
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := p.credConfig.GetCredential(ctx)
	if err != nil {
		return false, 0
	}

	_, err = p.credConfig.GetSubscriptionID()
	if err != nil {
		return false, 0
	}

	return true, 0.6
}

// Parse discovers Azure infrastructure via API.
func (p *APIParser) Parse(ctx context.Context, path string, opts *parser.ParseOptions) (*resource.Infrastructure, error) {
	// Initialize credential config from options
	if opts != nil && opts.APICredentials != nil {
		p.credConfig = FromParseOptions(opts.APICredentials, opts.Regions)
	}

	// Get subscription ID
	subID, err := p.credConfig.GetSubscriptionID()
	if err != nil {
		return nil, fmt.Errorf("failed to get subscription ID: %w", err)
	}
	p.subscriptionID = subID

	// Get credentials
	cred, err := p.credConfig.GetCredential(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get credentials: %w", err)
	}

	// Create infrastructure
	infra := resource.NewInfrastructure(resource.ProviderAzure)
	infra.Metadata["subscription_id"] = subID

	// Determine locations to scan
	locations := []string{"eastus"}
	if opts != nil && len(opts.Regions) > 0 {
		locations = opts.Regions
	}

	// Scan resources
	if err := p.scanVirtualMachines(ctx, cred, infra, locations, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Virtual Machines: %w", err)
	}

	if err := p.scanStorageAccounts(ctx, cred, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Storage Accounts: %w", err)
	}

	if err := p.scanSQLServers(ctx, cred, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan SQL Servers: %w", err)
	}

	if err := p.scanRedisCache(ctx, cred, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Redis Cache: %w", err)
	}

	return infra, nil
}

// scanVirtualMachines discovers Azure Virtual Machines.
func (p *APIParser) scanVirtualMachines(ctx context.Context, cred interface{}, infra *resource.Infrastructure, locations []string, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeAzureVM, opts) {
		return nil
	}

	// Get Azure credential
	azCred, err := p.credConfig.GetCredential(ctx)
	if err != nil {
		return err
	}

	clientFactory, err := armcompute.NewClientFactory(p.subscriptionID, azCred, nil)
	if err != nil {
		return fmt.Errorf("failed to create compute client: %w", err)
	}

	client := clientFactory.NewVirtualMachinesClient()
	pager := client.NewListAllPager(nil)

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list VMs: %w", err)
		}

		for _, vm := range page.Value {
			// Filter by location if specified
			if len(locations) > 0 && !containsIgnoreCase(locations, *vm.Location) {
				continue
			}

			name := *vm.Name
			resType := resource.TypeAzureVM
			if vm.Properties != nil && vm.Properties.OSProfile != nil {
				if vm.Properties.OSProfile.WindowsConfiguration != nil {
					resType = resource.TypeAzureVMWindows
				}
			}

			res := resource.NewAWSResource(name, name, resType)
			res.Region = *vm.Location
			res.ARN = *vm.ID

			// Config
			if vm.Properties != nil {
				if vm.Properties.HardwareProfile != nil {
					res.Config["vm_size"] = string(*vm.Properties.HardwareProfile.VMSize)
				}

				// OS disk
				if vm.Properties.StorageProfile != nil && vm.Properties.StorageProfile.OSDisk != nil {
					osDisk := vm.Properties.StorageProfile.OSDisk
					res.Config["os_disk_size_gb"] = osDisk.DiskSizeGB
					if osDisk.ManagedDisk != nil {
						res.Config["os_disk_type"] = string(*osDisk.ManagedDisk.StorageAccountType)
					}
				}

				// Data disks
				if vm.Properties.StorageProfile != nil {
					var dataDisks []map[string]interface{}
					for _, disk := range vm.Properties.StorageProfile.DataDisks {
						dataDisks = append(dataDisks, map[string]interface{}{
							"name":    disk.Name,
							"size_gb": disk.DiskSizeGB,
							"lun":     disk.Lun,
						})
					}
					res.Config["data_disks"] = dataDisks
				}

				// Network interfaces
				if vm.Properties.NetworkProfile != nil {
					var nics []string
					for _, nic := range vm.Properties.NetworkProfile.NetworkInterfaces {
						if nic.ID != nil {
							nics = append(nics, extractResourceName(*nic.ID))
						}
					}
					res.Config["network_interfaces"] = nics
				}
			}

			// Tags
			for k, v := range vm.Tags {
				if v != nil {
					res.Tags[k] = *v
				}
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanStorageAccounts discovers Azure Storage Accounts.
func (p *APIParser) scanStorageAccounts(ctx context.Context, cred interface{}, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeAzureStorageAcct, opts) {
		return nil
	}

	azCred, err := p.credConfig.GetCredential(ctx)
	if err != nil {
		return err
	}

	clientFactory, err := armstorage.NewClientFactory(p.subscriptionID, azCred, nil)
	if err != nil {
		return fmt.Errorf("failed to create storage client: %w", err)
	}

	client := clientFactory.NewAccountsClient()
	pager := client.NewListPager(nil)

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list storage accounts: %w", err)
		}

		for _, account := range page.Value {
			name := *account.Name
			res := resource.NewAWSResource(name, name, resource.TypeAzureStorageAcct)
			res.Region = *account.Location
			res.ARN = *account.ID

			// Config
			if account.SKU != nil {
				res.Config["sku_name"] = string(*account.SKU.Name)
				res.Config["sku_tier"] = string(*account.SKU.Tier)
			}
			res.Config["kind"] = string(*account.Kind)

			if account.Properties != nil {
				res.Config["access_tier"] = string(*account.Properties.AccessTier)
				res.Config["https_only"] = account.Properties.EnableHTTPSTrafficOnly

				if account.Properties.Encryption != nil {
					res.Config["blob_encryption_enabled"] = account.Properties.Encryption.Services.Blob.Enabled
					res.Config["file_encryption_enabled"] = account.Properties.Encryption.Services.File.Enabled
				}

				if account.Properties.PrimaryEndpoints != nil {
					res.Config["blob_endpoint"] = account.Properties.PrimaryEndpoints.Blob
					res.Config["file_endpoint"] = account.Properties.PrimaryEndpoints.File
				}
			}

			// Tags
			for k, v := range account.Tags {
				if v != nil {
					res.Tags[k] = *v
				}
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanSQLServers discovers Azure SQL Servers and Databases.
func (p *APIParser) scanSQLServers(ctx context.Context, cred interface{}, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeAzureSQL, opts) {
		return nil
	}

	azCred, err := p.credConfig.GetCredential(ctx)
	if err != nil {
		return err
	}

	clientFactory, err := armsql.NewClientFactory(p.subscriptionID, azCred, nil)
	if err != nil {
		return fmt.Errorf("failed to create SQL client: %w", err)
	}

	serversClient := clientFactory.NewServersClient()
	dbClient := clientFactory.NewDatabasesClient()

	serverPager := serversClient.NewListPager(nil)

	for serverPager.More() {
		page, err := serverPager.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list SQL servers: %w", err)
		}

		for _, server := range page.Value {
			serverName := *server.Name
			resourceGroup := extractResourceGroup(*server.ID)

			// List databases for this server
			dbPager := dbClient.NewListByServerPager(resourceGroup, serverName, nil)

			for dbPager.More() {
				dbPage, err := dbPager.NextPage(ctx)
				if err != nil {
					continue
				}

				for _, db := range dbPage.Value {
					// Skip master database
					if *db.Name == "master" {
						continue
					}

					name := fmt.Sprintf("%s/%s", serverName, *db.Name)
					res := resource.NewAWSResource(name, name, resource.TypeAzureSQL)
					res.Region = *server.Location
					res.ARN = *db.ID

					// Config
					res.Config["server_name"] = serverName
					res.Config["database_name"] = *db.Name

					if server.Properties != nil {
						res.Config["version"] = server.Properties.Version
						res.Config["fqdn"] = server.Properties.FullyQualifiedDomainName
					}

					if db.Properties != nil {
						res.Config["status"] = string(*db.Properties.Status)
						res.Config["max_size_bytes"] = db.Properties.MaxSizeBytes

						if db.SKU != nil {
							res.Config["sku_name"] = db.SKU.Name
							res.Config["sku_tier"] = db.SKU.Tier
							res.Config["sku_capacity"] = db.SKU.Capacity
						}
					}

					// Tags
					for k, v := range db.Tags {
						if v != nil {
							res.Tags[k] = *v
						}
					}

					infra.AddResource(res)
				}
			}
		}
	}

	return nil
}

// scanRedisCache discovers Azure Cache for Redis instances.
func (p *APIParser) scanRedisCache(ctx context.Context, cred interface{}, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeAzureCache, opts) {
		return nil
	}

	azCred, err := p.credConfig.GetCredential(ctx)
	if err != nil {
		return err
	}

	clientFactory, err := armredis.NewClientFactory(p.subscriptionID, azCred, nil)
	if err != nil {
		return fmt.Errorf("failed to create Redis client: %w", err)
	}

	client := clientFactory.NewClient()
	pager := client.NewListBySubscriptionPager(nil)

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list Redis caches: %w", err)
		}

		for _, cache := range page.Value {
			name := *cache.Name
			res := resource.NewAWSResource(name, name, resource.TypeAzureCache)
			res.Region = *cache.Location
			res.ARN = *cache.ID

			// Config
			if cache.Properties != nil {
				res.Config["redis_version"] = cache.Properties.RedisVersion
				res.Config["sku_name"] = string(*cache.Properties.SKU.Name)
				res.Config["sku_family"] = string(*cache.Properties.SKU.Family)
				res.Config["sku_capacity"] = cache.Properties.SKU.Capacity
				res.Config["host_name"] = cache.Properties.HostName
				res.Config["port"] = cache.Properties.Port
				res.Config["ssl_port"] = cache.Properties.SSLPort
				res.Config["enable_non_ssl_port"] = cache.Properties.EnableNonSSLPort
				res.Config["provisioning_state"] = string(*cache.Properties.ProvisioningState)
			}

			// Tags
			for k, v := range cache.Tags {
				if v != nil {
					res.Tags[k] = *v
				}
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// shouldScanType checks if a resource type should be scanned based on filters.
func (p *APIParser) shouldScanType(t resource.Type, opts *parser.ParseOptions) bool {
	if opts == nil {
		return true
	}

	// Check type filters
	if len(opts.FilterTypes) > 0 {
		for _, ft := range opts.FilterTypes {
			if ft == t {
				return true
			}
		}
		return false
	}

	// Check category filters
	if len(opts.FilterCategories) > 0 {
		category := t.GetCategory()
		for _, fc := range opts.FilterCategories {
			if fc == category {
				return true
			}
		}
		return false
	}

	return true
}

// extractResourceName extracts the resource name from an Azure resource ID.
func extractResourceName(resourceID string) string {
	parts := strings.Split(resourceID, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return resourceID
}

// extractResourceGroup extracts the resource group from an Azure resource ID.
func extractResourceGroup(resourceID string) string {
	parts := strings.Split(resourceID, "/")
	for i, part := range parts {
		if strings.EqualFold(part, "resourceGroups") && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

// containsIgnoreCase checks if a slice contains a string (case insensitive).
func containsIgnoreCase(slice []string, item string) bool {
	item = strings.ToLower(item)
	for _, s := range slice {
		if strings.ToLower(s) == item {
			return true
		}
	}
	return false
}
