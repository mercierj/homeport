package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/appservice/armappservice"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v5"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerinstance/armcontainerinstance"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v4"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cosmos/armcosmos/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/dns/armdns"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/eventgrid/armeventgrid/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/eventhub/armeventhub"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/frontdoor/armfrontdoor"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/keyvault/armkeyvault"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/logic/armlogic"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/mysql/armmysqlflexibleservers"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v4"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/postgresql/armpostgresqlflexibleservers"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/redis/armredis/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/servicebus/armservicebus"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/sql/armsql"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage"

	"github.com/homeport/homeport/internal/domain/parser"
	"github.com/homeport/homeport/internal/domain/resource"
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

	if err := p.scanBlobStorage(ctx, cred, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Blob Storage: %w", err)
	}

	if err := p.scanManagedDisk(ctx, cred, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Managed Disks: %w", err)
	}

	if err := p.scanAzureFiles(ctx, cred, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Azure Files: %w", err)
	}

	if err := p.scanSQLServers(ctx, cred, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan SQL Servers: %w", err)
	}

	if err := p.scanAzurePostgres(ctx, cred, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Azure PostgreSQL: %w", err)
	}

	if err := p.scanAzureMySQL(ctx, cred, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Azure MySQL: %w", err)
	}

	if err := p.scanCosmosDB(ctx, cred, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Cosmos DB: %w", err)
	}

	if err := p.scanRedisCache(ctx, cred, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Redis Cache: %w", err)
	}

	if err := p.scanDNS(ctx, cred, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan DNS zones: %w", err)
	}

	if err := p.scanAKS(ctx, cred, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan AKS: %w", err)
	}

	if err := p.scanContainerInstance(ctx, cred, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Container Instances: %w", err)
	}

	if err := p.scanAzureCDN(ctx, cred, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Azure CDN: %w", err)
	}

	if err := p.scanFrontDoor(ctx, cred, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Front Door: %w", err)
	}

	if err := p.scanAppGateway(ctx, cred, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Application Gateway: %w", err)
	}

	if err := p.scanServiceBus(ctx, cred, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Service Bus: %w", err)
	}

	if err := p.scanServiceBusQueue(ctx, cred, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Service Bus Queues: %w", err)
	}

	if err := p.scanEventHub(ctx, cred, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Event Hub: %w", err)
	}

	if err := p.scanEventGrid(ctx, cred, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Event Grid: %w", err)
	}

	if err := p.scanLogicApp(ctx, cred, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Logic Apps: %w", err)
	}

	if err := p.scanKeyVault(ctx, cred, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Key Vault: %w", err)
	}

	if err := p.scanAzureFirewall(ctx, cred, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Azure Firewall: %w", err)
	}

	if err := p.scanAzureFunction(ctx, cred, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Azure Functions: %w", err)
	}

	if err := p.scanAppService(ctx, cred, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan App Services: %w", err)
	}

	if err := p.scanAzureLB(ctx, cred, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Load Balancers: %w", err)
	}

	if err := p.scanAzureVNet(ctx, cred, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Virtual Networks: %w", err)
	}

	if err := p.scanADB2C(ctx, cred, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Azure AD B2C: %w", err)
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

// scanAzurePostgres discovers Azure PostgreSQL Flexible Servers.
func (p *APIParser) scanAzurePostgres(ctx context.Context, cred interface{}, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeAzurePostgres, opts) {
		return nil
	}

	azCred, err := p.credConfig.GetCredential(ctx)
	if err != nil {
		return err
	}

	client, err := armpostgresqlflexibleservers.NewServersClient(p.subscriptionID, azCred, nil)
	if err != nil {
		return fmt.Errorf("failed to create PostgreSQL client: %w", err)
	}

	pager := client.NewListPager(nil)

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list PostgreSQL servers: %w", err)
		}

		for _, server := range page.Value {
			name := *server.Name
			res := resource.NewAWSResource(name, name, resource.TypeAzurePostgres)
			res.Region = *server.Location
			res.ARN = *server.ID

			// Config
			if server.SKU != nil {
				res.Config["sku_name"] = server.SKU.Name
				res.Config["sku_tier"] = string(*server.SKU.Tier)
			}

			if server.Properties != nil {
				props := server.Properties
				res.Config["administrator_login"] = props.AdministratorLogin
				res.Config["version"] = string(*props.Version)
				res.Config["state"] = string(*props.State)
				res.Config["fully_qualified_domain_name"] = props.FullyQualifiedDomainName

				// Storage
				if props.Storage != nil {
					res.Config["storage_size_gb"] = props.Storage.StorageSizeGB
				}

				// Backup
				if props.Backup != nil {
					res.Config["backup_retention_days"] = props.Backup.BackupRetentionDays
					res.Config["geo_redundant_backup"] = string(*props.Backup.GeoRedundantBackup)
				}

				// High availability
				if props.HighAvailability != nil {
					res.Config["high_availability_mode"] = string(*props.HighAvailability.Mode)
					res.Config["high_availability_state"] = string(*props.HighAvailability.State)
					res.Config["standby_availability_zone"] = props.HighAvailability.StandbyAvailabilityZone
				}

				// Network
				if props.Network != nil {
					res.Config["public_network_access"] = string(*props.Network.PublicNetworkAccess)
				}

				// Maintenance window
				if props.MaintenanceWindow != nil {
					res.Config["maintenance_window_day"] = props.MaintenanceWindow.DayOfWeek
					res.Config["maintenance_window_start_hour"] = props.MaintenanceWindow.StartHour
				}

				res.Config["availability_zone"] = props.AvailabilityZone
			}

			// Tags
			for k, v := range server.Tags {
				if v != nil {
					res.Tags[k] = *v
				}
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanAzureMySQL discovers Azure MySQL Flexible Servers.
func (p *APIParser) scanAzureMySQL(ctx context.Context, cred interface{}, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeAzureMySQL, opts) {
		return nil
	}

	azCred, err := p.credConfig.GetCredential(ctx)
	if err != nil {
		return err
	}

	client, err := armmysqlflexibleservers.NewServersClient(p.subscriptionID, azCred, nil)
	if err != nil {
		return fmt.Errorf("failed to create MySQL client: %w", err)
	}

	pager := client.NewListPager(nil)

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list MySQL servers: %w", err)
		}

		for _, server := range page.Value {
			name := *server.Name
			res := resource.NewAWSResource(name, name, resource.TypeAzureMySQL)
			res.Region = *server.Location
			res.ARN = *server.ID

			// Config
			if server.SKU != nil {
				res.Config["sku_name"] = server.SKU.Name
				res.Config["sku_tier"] = string(*server.SKU.Tier)
			}

			if server.Properties != nil {
				props := server.Properties
				res.Config["administrator_login"] = props.AdministratorLogin
				res.Config["version"] = string(*props.Version)
				res.Config["state"] = string(*props.State)
				res.Config["fully_qualified_domain_name"] = props.FullyQualifiedDomainName

				// Storage
				if props.Storage != nil {
					res.Config["storage_size_gb"] = props.Storage.StorageSizeGB
					res.Config["storage_iops"] = props.Storage.Iops
					res.Config["auto_grow"] = string(*props.Storage.AutoGrow)
				}

				// Backup
				if props.Backup != nil {
					res.Config["backup_retention_days"] = props.Backup.BackupRetentionDays
					res.Config["geo_redundant_backup"] = string(*props.Backup.GeoRedundantBackup)
				}

				// High availability
				if props.HighAvailability != nil {
					res.Config["high_availability_mode"] = string(*props.HighAvailability.Mode)
					res.Config["high_availability_state"] = string(*props.HighAvailability.State)
					res.Config["standby_availability_zone"] = props.HighAvailability.StandbyAvailabilityZone
				}

				// Replication role
				res.Config["replication_role"] = string(*props.ReplicationRole)

				res.Config["availability_zone"] = props.AvailabilityZone
			}

			// Tags
			for k, v := range server.Tags {
				if v != nil {
					res.Tags[k] = *v
				}
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanCosmosDB discovers Azure Cosmos DB accounts.
func (p *APIParser) scanCosmosDB(ctx context.Context, cred interface{}, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeCosmosDB, opts) {
		return nil
	}

	azCred, err := p.credConfig.GetCredential(ctx)
	if err != nil {
		return err
	}

	clientFactory, err := armcosmos.NewClientFactory(p.subscriptionID, azCred, nil)
	if err != nil {
		return fmt.Errorf("failed to create Cosmos DB client: %w", err)
	}

	client := clientFactory.NewDatabaseAccountsClient()
	pager := client.NewListPager(nil)

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list Cosmos DB accounts: %w", err)
		}

		for _, account := range page.Value {
			name := *account.Name
			res := resource.NewAWSResource(name, name, resource.TypeCosmosDB)
			res.Region = *account.Location
			res.ARN = *account.ID

			// Config
			res.Config["kind"] = string(*account.Kind)

			if account.Properties != nil {
				props := account.Properties
				res.Config["document_endpoint"] = props.DocumentEndpoint
				res.Config["provisioning_state"] = props.ProvisioningState
				res.Config["database_account_offer_type"] = string(*props.DatabaseAccountOfferType)
				res.Config["enable_automatic_failover"] = props.EnableAutomaticFailover
				res.Config["enable_multiple_write_locations"] = props.EnableMultipleWriteLocations
				res.Config["is_virtual_network_filter_enabled"] = props.IsVirtualNetworkFilterEnabled
				res.Config["enable_free_tier"] = props.EnableFreeTier
				res.Config["enable_analytical_storage"] = props.EnableAnalyticalStorage
				res.Config["public_network_access"] = string(*props.PublicNetworkAccess)

				// Consistency policy
				if props.ConsistencyPolicy != nil {
					res.Config["consistency_level"] = string(*props.ConsistencyPolicy.DefaultConsistencyLevel)
					res.Config["max_staleness_prefix"] = props.ConsistencyPolicy.MaxStalenessPrefix
					res.Config["max_interval_in_seconds"] = props.ConsistencyPolicy.MaxIntervalInSeconds
				}

				// Geo locations (read regions)
				if props.ReadLocations != nil {
					var locations []map[string]interface{}
					for _, loc := range props.ReadLocations {
						locations = append(locations, map[string]interface{}{
							"location_name":     loc.LocationName,
							"failover_priority": loc.FailoverPriority,
							"is_zone_redundant": loc.IsZoneRedundant,
						})
					}
					res.Config["read_locations"] = locations
				}

				// Write locations
				if props.WriteLocations != nil {
					var locations []map[string]interface{}
					for _, loc := range props.WriteLocations {
						locations = append(locations, map[string]interface{}{
							"location_name":     loc.LocationName,
							"failover_priority": loc.FailoverPriority,
							"is_zone_redundant": loc.IsZoneRedundant,
						})
					}
					res.Config["write_locations"] = locations
				}

				// Capabilities (e.g., EnableCassandra, EnableTable, EnableGremlin)
				if props.Capabilities != nil {
					var capabilities []string
					for _, cap := range props.Capabilities {
						capabilities = append(capabilities, *cap.Name)
					}
					res.Config["capabilities"] = capabilities
				}

				// Backup policy
				if props.BackupPolicy != nil {
					// The backup policy is a union type, we need to check which kind it is
					switch bp := props.BackupPolicy.(type) {
					case *armcosmos.PeriodicModeBackupPolicy:
						res.Config["backup_policy_type"] = "Periodic"
						if bp.PeriodicModeProperties != nil {
							res.Config["backup_interval_in_minutes"] = bp.PeriodicModeProperties.BackupIntervalInMinutes
							res.Config["backup_retention_interval_in_hours"] = bp.PeriodicModeProperties.BackupRetentionIntervalInHours
						}
					case *armcosmos.ContinuousModeBackupPolicy:
						res.Config["backup_policy_type"] = "Continuous"
					}
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

// scanDNS discovers Azure DNS zones.
func (p *APIParser) scanDNS(ctx context.Context, cred interface{}, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeAzureDNS, opts) {
		return nil
	}

	azCred, err := p.credConfig.GetCredential(ctx)
	if err != nil {
		return err
	}

	clientFactory, err := armdns.NewClientFactory(p.subscriptionID, azCred, nil)
	if err != nil {
		return fmt.Errorf("failed to create DNS client: %w", err)
	}

	client := clientFactory.NewZonesClient()
	pager := client.NewListPager(nil)

	zoneCount := 0
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list DNS zones: %w", err)
		}

		for _, zone := range page.Value {
			name := *zone.Name
			res := resource.NewAWSResource(name, name, resource.TypeAzureDNS)
			res.ARN = *zone.ID

			// Location for DNS zones is typically "global"
			if zone.Location != nil {
				res.Region = *zone.Location
			} else {
				res.Region = "global"
			}

			// Extract resource group from ID
			resourceGroup := extractResourceGroup(*zone.ID)
			res.Config["resource_group"] = resourceGroup

			// Zone properties
			if zone.Properties != nil {
				// Zone type (Public or Private)
				if zone.Properties.ZoneType != nil {
					res.Config["zone_type"] = string(*zone.Properties.ZoneType)
				}

				// Name servers
				if zone.Properties.NameServers != nil {
					nameServers := make([]string, 0, len(zone.Properties.NameServers))
					for _, ns := range zone.Properties.NameServers {
						if ns != nil {
							nameServers = append(nameServers, *ns)
						}
					}
					res.Config["name_servers"] = nameServers
				}

				// Number of record sets
				if zone.Properties.NumberOfRecordSets != nil {
					res.Config["record_count"] = *zone.Properties.NumberOfRecordSets
				}

				// Max number of record sets
				if zone.Properties.MaxNumberOfRecordSets != nil {
					res.Config["max_record_count"] = *zone.Properties.MaxNumberOfRecordSets
				}
			}

			// Etag
			if zone.Etag != nil {
				res.Config["etag"] = *zone.Etag
			}

			// Tags
			for k, v := range zone.Tags {
				if v != nil {
					res.Tags[k] = *v
				}
			}

			infra.AddResource(res)
			zoneCount++

			// Report progress if callback is provided
			if opts != nil && opts.OnProgress != nil {
				opts.OnProgress(parser.ProgressEvent{
					Step:           "scanning",
					Message:        fmt.Sprintf("Discovered DNS zone: %s", name),
					Service:        "DNS",
					ResourcesFound: zoneCount,
				})
			}
		}
	}

	// Final progress report
	if opts != nil && opts.OnProgress != nil {
		opts.OnProgress(parser.ProgressEvent{
			Step:           "scanning",
			Message:        fmt.Sprintf("Completed DNS zone scan: %d zones found", zoneCount),
			Service:        "DNS",
			ResourcesFound: zoneCount,
		})
	}

	return nil
}

// scanAKS discovers Azure Kubernetes Service clusters.
func (p *APIParser) scanAKS(ctx context.Context, cred interface{}, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeAKS, opts) {
		return nil
	}

	azCred, err := p.credConfig.GetCredential(ctx)
	if err != nil {
		return err
	}

	clientFactory, err := armcontainerservice.NewClientFactory(p.subscriptionID, azCred, nil)
	if err != nil {
		return fmt.Errorf("failed to create AKS client: %w", err)
	}

	client := clientFactory.NewManagedClustersClient()
	pager := client.NewListPager(nil)

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list AKS clusters: %w", err)
		}

		for _, cluster := range page.Value {
			name := *cluster.Name
			res := resource.NewAWSResource(name, name, resource.TypeAKS)
			res.Region = *cluster.Location
			res.ARN = *cluster.ID

			// Config
			if cluster.Properties != nil {
				props := cluster.Properties
				res.Config["kubernetes_version"] = props.KubernetesVersion
				res.Config["dns_prefix"] = props.DNSPrefix
				res.Config["fqdn"] = props.Fqdn
				res.Config["provisioning_state"] = string(*props.ProvisioningState)
				res.Config["power_state"] = string(*props.PowerState.Code)

				// Node resource group
				res.Config["node_resource_group"] = props.NodeResourceGroup

				// Agent pool profiles (node pools)
				if props.AgentPoolProfiles != nil {
					var nodePools []map[string]interface{}
					for _, pool := range props.AgentPoolProfiles {
						nodePool := map[string]interface{}{
							"name":                 pool.Name,
							"count":                pool.Count,
							"vm_size":              pool.VMSize,
							"os_type":              string(*pool.OSType),
							"os_disk_size_gb":      pool.OSDiskSizeGB,
							"max_pods":             pool.MaxPods,
							"mode":                 string(*pool.Mode),
							"orchestrator_version": pool.OrchestratorVersion,
						}

						if pool.EnableAutoScaling != nil && *pool.EnableAutoScaling {
							nodePool["enable_auto_scaling"] = true
							nodePool["min_count"] = pool.MinCount
							nodePool["max_count"] = pool.MaxCount
						}

						if pool.AvailabilityZones != nil {
							nodePool["availability_zones"] = pool.AvailabilityZones
						}

						nodePools = append(nodePools, nodePool)
					}
					res.Config["agent_pool_profiles"] = nodePools
				}

				// Network profile
				if props.NetworkProfile != nil {
					networkProfile := map[string]interface{}{
						"network_plugin":    string(*props.NetworkProfile.NetworkPlugin),
						"network_policy":    props.NetworkProfile.NetworkPolicy,
						"service_cidr":      props.NetworkProfile.ServiceCidr,
						"dns_service_ip":    props.NetworkProfile.DNSServiceIP,
						"pod_cidr":          props.NetworkProfile.PodCidr,
						"load_balancer_sku": string(*props.NetworkProfile.LoadBalancerSKU),
					}
					res.Config["network_profile"] = networkProfile
				}

				// Identity
				if cluster.Identity != nil {
					res.Config["identity_type"] = string(*cluster.Identity.Type)
				}

				// Enable RBAC
				res.Config["enable_rbac"] = props.EnableRBAC

				// AAD profile
				if props.AADProfile != nil {
					res.Config["aad_profile_managed"] = props.AADProfile.Managed
					res.Config["aad_profile_enable_azure_rbac"] = props.AADProfile.EnableAzureRBAC
				}

				// Auto upgrade profile
				if props.AutoUpgradeProfile != nil {
					res.Config["auto_upgrade_channel"] = string(*props.AutoUpgradeProfile.UpgradeChannel)
				}
			}

			// SKU
			if cluster.SKU != nil {
				res.Config["sku_name"] = string(*cluster.SKU.Name)
				res.Config["sku_tier"] = string(*cluster.SKU.Tier)
			}

			// Tags
			for k, v := range cluster.Tags {
				if v != nil {
					res.Tags[k] = *v
				}
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanContainerInstance discovers Azure Container Instances.
func (p *APIParser) scanContainerInstance(ctx context.Context, cred interface{}, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeContainerInstance, opts) {
		return nil
	}

	azCred, err := p.credConfig.GetCredential(ctx)
	if err != nil {
		return err
	}

	client, err := armcontainerinstance.NewContainerGroupsClient(p.subscriptionID, azCred, nil)
	if err != nil {
		return fmt.Errorf("failed to create Container Instance client: %w", err)
	}
	pager := client.NewListPager(nil)

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list container groups: %w", err)
		}

		for _, cg := range page.Value {
			name := *cg.Name
			res := resource.NewAWSResource(name, name, resource.TypeContainerInstance)
			res.Region = *cg.Location
			res.ARN = *cg.ID

			// Config
			if cg.Properties != nil {
				props := cg.Properties
				res.Config["os_type"] = string(*props.OSType)
				res.Config["restart_policy"] = string(*props.RestartPolicy)
				res.Config["provisioning_state"] = props.ProvisioningState

				// Containers
				if props.Containers != nil {
					var containers []map[string]interface{}
					for _, c := range props.Containers {
						container := map[string]interface{}{
							"name":  c.Name,
							"image": c.Properties.Image,
						}

						if c.Properties.Resources != nil && c.Properties.Resources.Requests != nil {
							container["cpu"] = c.Properties.Resources.Requests.CPU
							container["memory_gb"] = c.Properties.Resources.Requests.MemoryInGB
						}

						if c.Properties.Ports != nil {
							var ports []int32
							for _, p := range c.Properties.Ports {
								ports = append(ports, *p.Port)
							}
							container["ports"] = ports
						}

						// Environment variables (keys only, no values for security)
						if c.Properties.EnvironmentVariables != nil {
							var envKeys []string
							for _, env := range c.Properties.EnvironmentVariables {
								envKeys = append(envKeys, *env.Name)
							}
							container["environment_keys"] = envKeys
						}

						containers = append(containers, container)
					}
					res.Config["containers"] = containers
				}

				// IP address
				if props.IPAddress != nil {
					ipConfig := map[string]interface{}{
						"type": string(*props.IPAddress.Type),
						"ip":   props.IPAddress.IP,
					}
					if props.IPAddress.DNSNameLabel != nil {
						ipConfig["dns_name_label"] = *props.IPAddress.DNSNameLabel
					}
					if props.IPAddress.Fqdn != nil {
						ipConfig["fqdn"] = *props.IPAddress.Fqdn
					}
					if props.IPAddress.Ports != nil {
						var ports []map[string]interface{}
						for _, p := range props.IPAddress.Ports {
							ports = append(ports, map[string]interface{}{
								"port":     p.Port,
								"protocol": string(*p.Protocol),
							})
						}
						ipConfig["ports"] = ports
					}
					res.Config["ip_address"] = ipConfig
				}

				// Volumes
				if props.Volumes != nil {
					var volumes []map[string]interface{}
					for _, v := range props.Volumes {
						volume := map[string]interface{}{
							"name": v.Name,
						}
						if v.AzureFile != nil {
							volume["azure_file_share_name"] = v.AzureFile.ShareName
							volume["azure_file_storage_account"] = v.AzureFile.StorageAccountName
						}
						if v.EmptyDir != nil {
							volume["empty_dir"] = true
						}
						if v.GitRepo != nil {
							volume["git_repo_url"] = v.GitRepo.Repository
						}
						volumes = append(volumes, volume)
					}
					res.Config["volumes"] = volumes
				}

				// Subnet IDs for VNet integration
				if props.SubnetIDs != nil {
					var subnetIDs []string
					for _, s := range props.SubnetIDs {
						subnetIDs = append(subnetIDs, *s.ID)
					}
					res.Config["subnet_ids"] = subnetIDs
				}

				// DNS config
				if props.DNSConfig != nil {
					res.Config["dns_name_servers"] = props.DNSConfig.NameServers
				}
			}

			// Tags
			for k, v := range cg.Tags {
				if v != nil {
					res.Tags[k] = *v
				}
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanAzureCDN discovers Azure CDN profiles and endpoints.
func (p *APIParser) scanAzureCDN(ctx context.Context, cred interface{}, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeAzureCDN, opts) {
		return nil
	}

	azCred, err := p.credConfig.GetCredential(ctx)
	if err != nil {
		return err
	}

	clientFactory, err := armcdn.NewClientFactory(p.subscriptionID, azCred, nil)
	if err != nil {
		return fmt.Errorf("failed to create CDN client: %w", err)
	}

	profilesClient := clientFactory.NewProfilesClient()
	endpointsClient := clientFactory.NewEndpointsClient()

	profilePager := profilesClient.NewListPager(nil)

	for profilePager.More() {
		page, err := profilePager.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list CDN profiles: %w", err)
		}

		for _, profile := range page.Value {
			profileName := *profile.Name
			resourceGroup := extractResourceGroup(*profile.ID)

			res := resource.NewAWSResource(profileName, profileName, resource.TypeAzureCDN)
			res.Region = *profile.Location
			res.ARN = *profile.ID

			// Config
			if profile.SKU != nil {
				res.Config["sku_name"] = string(*profile.SKU.Name)
			}

			if profile.Properties != nil {
				res.Config["provisioning_state"] = string(*profile.Properties.ProvisioningState)
				res.Config["resource_state"] = string(*profile.Properties.ResourceState)
			}

			res.Config["resource_group"] = resourceGroup

			// List endpoints for this profile
			endpointPager := endpointsClient.NewListByProfilePager(resourceGroup, profileName, nil)

			var endpoints []map[string]interface{}
			for endpointPager.More() {
				epPage, err := endpointPager.NextPage(ctx)
				if err != nil {
					break
				}

				for _, ep := range epPage.Value {
					endpoint := map[string]interface{}{
						"name":      ep.Name,
						"host_name": ep.Properties.HostName,
					}

					if ep.Properties != nil {
						endpoint["provisioning_state"] = string(*ep.Properties.ProvisioningState)
						endpoint["resource_state"] = string(*ep.Properties.ResourceState)
						endpoint["is_compression_enabled"] = ep.Properties.IsCompressionEnabled
						endpoint["is_http_allowed"] = ep.Properties.IsHTTPAllowed
						endpoint["is_https_allowed"] = ep.Properties.IsHTTPSAllowed

						// Origins
						if ep.Properties.Origins != nil {
							var origins []map[string]interface{}
							for _, o := range ep.Properties.Origins {
								origins = append(origins, map[string]interface{}{
									"name":      o.Name,
									"host_name": o.Properties.HostName,
								})
							}
							endpoint["origins"] = origins
						}

						// Custom domains
						if ep.Properties.CustomDomains != nil {
							var customDomains []string
							for _, cd := range ep.Properties.CustomDomains {
								customDomains = append(customDomains, *cd.Name)
							}
							endpoint["custom_domains"] = customDomains
						}
					}

					endpoints = append(endpoints, endpoint)
				}
			}
			res.Config["endpoints"] = endpoints

			// Tags
			for k, v := range profile.Tags {
				if v != nil {
					res.Tags[k] = *v
				}
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanFrontDoor discovers Azure Front Door instances.
func (p *APIParser) scanFrontDoor(ctx context.Context, cred interface{}, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeFrontDoor, opts) {
		return nil
	}

	azCred, err := p.credConfig.GetCredential(ctx)
	if err != nil {
		return err
	}

	clientFactory, err := armfrontdoor.NewClientFactory(p.subscriptionID, azCred, nil)
	if err != nil {
		return fmt.Errorf("failed to create Front Door client: %w", err)
	}

	client := clientFactory.NewFrontDoorsClient()
	pager := client.NewListPager(nil)

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list Front Doors: %w", err)
		}

		for _, fd := range page.Value {
			name := *fd.Name
			res := resource.NewAWSResource(name, name, resource.TypeFrontDoor)
			res.Region = *fd.Location
			res.ARN = *fd.ID

			// Config
			if fd.Properties != nil {
				props := fd.Properties
				res.Config["friendly_name"] = props.FriendlyName
				res.Config["provisioning_state"] = string(*props.ProvisioningState)
				res.Config["resource_state"] = string(*props.ResourceState)
				res.Config["cname"] = props.Cname
				res.Config["enabled_state"] = string(*props.EnabledState)

				// Frontend endpoints
				if props.FrontendEndpoints != nil {
					var frontendEndpoints []map[string]interface{}
					for _, fe := range props.FrontendEndpoints {
						feConfig := map[string]interface{}{
							"name":      fe.Name,
							"host_name": fe.Properties.HostName,
						}
						if fe.Properties.SessionAffinityEnabledState != nil {
							feConfig["session_affinity_enabled"] = string(*fe.Properties.SessionAffinityEnabledState)
						}
						frontendEndpoints = append(frontendEndpoints, feConfig)
					}
					res.Config["frontend_endpoints"] = frontendEndpoints
				}

				// Backend pools
				if props.BackendPools != nil {
					var backendPools []map[string]interface{}
					for _, bp := range props.BackendPools {
						bpConfig := map[string]interface{}{
							"name": bp.Name,
						}
						if bp.Properties != nil && bp.Properties.Backends != nil {
							var backends []map[string]interface{}
							for _, b := range bp.Properties.Backends {
								backends = append(backends, map[string]interface{}{
									"address":       b.Address,
									"http_port":     b.HTTPPort,
									"https_port":    b.HTTPSPort,
									"enabled_state": string(*b.EnabledState),
									"priority":      b.Priority,
									"weight":        b.Weight,
								})
							}
							bpConfig["backends"] = backends
						}
						backendPools = append(backendPools, bpConfig)
					}
					res.Config["backend_pools"] = backendPools
				}

				// Routing rules
				if props.RoutingRules != nil {
					var routingRules []map[string]interface{}
					for _, rr := range props.RoutingRules {
						rrConfig := map[string]interface{}{
							"name":               rr.Name,
							"accepted_protocols": rr.Properties.AcceptedProtocols,
							"patterns_to_match":  rr.Properties.PatternsToMatch,
							"enabled_state":      string(*rr.Properties.EnabledState),
						}
						routingRules = append(routingRules, rrConfig)
					}
					res.Config["routing_rules"] = routingRules
				}

				// Health probe settings
				if props.HealthProbeSettings != nil {
					var probeSettings []map[string]interface{}
					for _, hp := range props.HealthProbeSettings {
						if hp.Properties != nil {
							probeSettings = append(probeSettings, map[string]interface{}{
								"name":                hp.Name,
								"path":                hp.Properties.Path,
								"protocol":            string(*hp.Properties.Protocol),
								"interval_in_seconds": hp.Properties.IntervalInSeconds,
							})
						}
					}
					res.Config["health_probe_settings"] = probeSettings
				}
			}

			// Tags
			for k, v := range fd.Tags {
				if v != nil {
					res.Tags[k] = *v
				}
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanAppGateway discovers Azure Application Gateways.
func (p *APIParser) scanAppGateway(ctx context.Context, cred interface{}, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeAppGateway, opts) {
		return nil
	}

	azCred, err := p.credConfig.GetCredential(ctx)
	if err != nil {
		return err
	}

	clientFactory, err := armnetwork.NewClientFactory(p.subscriptionID, azCred, nil)
	if err != nil {
		return fmt.Errorf("failed to create network client: %w", err)
	}

	client := clientFactory.NewApplicationGatewaysClient()
	pager := client.NewListAllPager(nil)

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list Application Gateways: %w", err)
		}

		for _, ag := range page.Value {
			name := *ag.Name
			res := resource.NewAWSResource(name, name, resource.TypeAppGateway)
			res.Region = *ag.Location
			res.ARN = *ag.ID

			// Config
			if ag.Properties != nil {
				props := ag.Properties
				res.Config["provisioning_state"] = string(*props.ProvisioningState)
				res.Config["operational_state"] = string(*props.OperationalState)

				// SKU
				if props.SKU != nil {
					res.Config["sku_name"] = string(*props.SKU.Name)
					res.Config["sku_tier"] = string(*props.SKU.Tier)
					res.Config["sku_capacity"] = props.SKU.Capacity
				}

				// Frontend IP configurations
				if props.FrontendIPConfigurations != nil {
					var frontendIPs []map[string]interface{}
					for _, fip := range props.FrontendIPConfigurations {
						ipConfig := map[string]interface{}{
							"name": fip.Name,
						}
						if fip.Properties != nil {
							if fip.Properties.PrivateIPAddress != nil {
								ipConfig["private_ip"] = *fip.Properties.PrivateIPAddress
							}
							if fip.Properties.PublicIPAddress != nil && fip.Properties.PublicIPAddress.ID != nil {
								ipConfig["public_ip_id"] = extractResourceName(*fip.Properties.PublicIPAddress.ID)
							}
						}
						frontendIPs = append(frontendIPs, ipConfig)
					}
					res.Config["frontend_ip_configurations"] = frontendIPs
				}

				// Backend address pools
				if props.BackendAddressPools != nil {
					var backendPools []map[string]interface{}
					for _, bp := range props.BackendAddressPools {
						poolConfig := map[string]interface{}{
							"name": bp.Name,
						}
						if bp.Properties != nil && bp.Properties.BackendAddresses != nil {
							var addresses []string
							for _, addr := range bp.Properties.BackendAddresses {
								if addr.Fqdn != nil {
									addresses = append(addresses, *addr.Fqdn)
								} else if addr.IPAddress != nil {
									addresses = append(addresses, *addr.IPAddress)
								}
							}
							poolConfig["addresses"] = addresses
						}
						backendPools = append(backendPools, poolConfig)
					}
					res.Config["backend_address_pools"] = backendPools
				}

				// HTTP listeners
				if props.HTTPListeners != nil {
					var listeners []map[string]interface{}
					for _, l := range props.HTTPListeners {
						listenerConfig := map[string]interface{}{
							"name": l.Name,
						}
						if l.Properties != nil {
							listenerConfig["protocol"] = string(*l.Properties.Protocol)
							listenerConfig["host_name"] = l.Properties.HostName
							if l.Properties.FrontendPort != nil {
								listenerConfig["frontend_port"] = extractResourceName(*l.Properties.FrontendPort.ID)
							}
						}
						listeners = append(listeners, listenerConfig)
					}
					res.Config["http_listeners"] = listeners
				}

				// Request routing rules
				if props.RequestRoutingRules != nil {
					var rules []map[string]interface{}
					for _, r := range props.RequestRoutingRules {
						ruleConfig := map[string]interface{}{
							"name":      r.Name,
							"rule_type": string(*r.Properties.RuleType),
							"priority":  r.Properties.Priority,
						}
						rules = append(rules, ruleConfig)
					}
					res.Config["request_routing_rules"] = rules
				}

				// Probes
				if props.Probes != nil {
					res.Config["probes_count"] = len(props.Probes)
				}

				// WAF configuration
				if props.WebApplicationFirewallConfiguration != nil {
					wafConfig := props.WebApplicationFirewallConfiguration
					res.Config["waf_enabled"] = wafConfig.Enabled
					res.Config["waf_mode"] = string(*wafConfig.FirewallMode)
					res.Config["waf_rule_set_type"] = wafConfig.RuleSetType
					res.Config["waf_rule_set_version"] = wafConfig.RuleSetVersion
				}
			}

			// Tags
			for k, v := range ag.Tags {
				if v != nil {
					res.Tags[k] = *v
				}
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanServiceBus discovers Azure Service Bus namespaces.
func (p *APIParser) scanServiceBus(ctx context.Context, cred interface{}, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeServiceBus, opts) {
		return nil
	}

	azCred, err := p.credConfig.GetCredential(ctx)
	if err != nil {
		return err
	}

	clientFactory, err := armservicebus.NewClientFactory(p.subscriptionID, azCred, nil)
	if err != nil {
		return fmt.Errorf("failed to create Service Bus client: %w", err)
	}

	client := clientFactory.NewNamespacesClient()
	pager := client.NewListPager(nil)

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list Service Bus namespaces: %w", err)
		}

		for _, ns := range page.Value {
			name := *ns.Name
			res := resource.NewAWSResource(name, name, resource.TypeServiceBus)
			res.Region = *ns.Location
			res.ARN = *ns.ID

			// Config
			if ns.SKU != nil {
				res.Config["sku_name"] = string(*ns.SKU.Name)
				res.Config["sku_tier"] = string(*ns.SKU.Tier)
				res.Config["sku_capacity"] = ns.SKU.Capacity
			}

			if ns.Properties != nil {
				res.Config["provisioning_state"] = ns.Properties.ProvisioningState
				res.Config["status"] = ns.Properties.Status
				res.Config["service_bus_endpoint"] = ns.Properties.ServiceBusEndpoint
				res.Config["zone_redundant"] = ns.Properties.ZoneRedundant
				res.Config["disable_local_auth"] = ns.Properties.DisableLocalAuth
			}

			// Tags
			for k, v := range ns.Tags {
				if v != nil {
					res.Tags[k] = *v
				}
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanServiceBusQueue discovers Azure Service Bus queues.
func (p *APIParser) scanServiceBusQueue(ctx context.Context, cred interface{}, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeServiceBusQueue, opts) {
		return nil
	}

	azCred, err := p.credConfig.GetCredential(ctx)
	if err != nil {
		return err
	}

	clientFactory, err := armservicebus.NewClientFactory(p.subscriptionID, azCred, nil)
	if err != nil {
		return fmt.Errorf("failed to create Service Bus client: %w", err)
	}

	nsClient := clientFactory.NewNamespacesClient()
	queueClient := clientFactory.NewQueuesClient()

	nsPager := nsClient.NewListPager(nil)

	for nsPager.More() {
		nsPage, err := nsPager.NextPage(ctx)
		if err != nil {
			continue
		}

		for _, ns := range nsPage.Value {
			nsName := *ns.Name
			resourceGroup := extractResourceGroup(*ns.ID)

			queuePager := queueClient.NewListByNamespacePager(resourceGroup, nsName, nil)

			for queuePager.More() {
				queuePage, err := queuePager.NextPage(ctx)
				if err != nil {
					break
				}

				for _, queue := range queuePage.Value {
					queueName := *queue.Name
					fullName := fmt.Sprintf("%s/%s", nsName, queueName)

					res := resource.NewAWSResource(fullName, queueName, resource.TypeServiceBusQueue)
					res.Region = *ns.Location
					res.ARN = *queue.ID

					// Config
					res.Config["namespace_name"] = nsName
					res.Config["queue_name"] = queueName

					if queue.Properties != nil {
						props := queue.Properties
						res.Config["max_size_in_megabytes"] = props.MaxSizeInMegabytes
						res.Config["lock_duration"] = props.LockDuration
						res.Config["max_delivery_count"] = props.MaxDeliveryCount
						res.Config["requires_duplicate_detection"] = props.RequiresDuplicateDetection
						res.Config["requires_session"] = props.RequiresSession
						res.Config["dead_lettering_on_message_expiration"] = props.DeadLetteringOnMessageExpiration
						res.Config["enable_partitioning"] = props.EnablePartitioning
						res.Config["status"] = string(*props.Status)

						if props.DefaultMessageTimeToLive != nil {
							res.Config["default_message_ttl"] = *props.DefaultMessageTimeToLive
						}
					}

					infra.AddResource(res)
				}
			}
		}
	}

	return nil
}

// scanEventHub discovers Azure Event Hub namespaces and event hubs.
func (p *APIParser) scanEventHub(ctx context.Context, cred interface{}, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeEventHub, opts) {
		return nil
	}

	azCred, err := p.credConfig.GetCredential(ctx)
	if err != nil {
		return err
	}

	clientFactory, err := armeventhub.NewClientFactory(p.subscriptionID, azCred, nil)
	if err != nil {
		return fmt.Errorf("failed to create Event Hub client: %w", err)
	}

	nsClient := clientFactory.NewNamespacesClient()
	ehClient := clientFactory.NewEventHubsClient()

	nsPager := nsClient.NewListPager(nil)

	for nsPager.More() {
		nsPage, err := nsPager.NextPage(ctx)
		if err != nil {
			continue
		}

		for _, ns := range nsPage.Value {
			nsName := *ns.Name
			resourceGroup := extractResourceGroup(*ns.ID)

			ehPager := ehClient.NewListByNamespacePager(resourceGroup, nsName, nil)

			for ehPager.More() {
				ehPage, err := ehPager.NextPage(ctx)
				if err != nil {
					break
				}

				for _, eh := range ehPage.Value {
					ehName := *eh.Name
					fullName := fmt.Sprintf("%s/%s", nsName, ehName)

					res := resource.NewAWSResource(fullName, ehName, resource.TypeEventHub)
					res.Region = *ns.Location
					res.ARN = *eh.ID

					// Config
					res.Config["namespace_name"] = nsName
					res.Config["event_hub_name"] = ehName

					if eh.Properties != nil {
						props := eh.Properties
						res.Config["partition_count"] = props.PartitionCount
						res.Config["message_retention_in_days"] = props.MessageRetentionInDays
						res.Config["status"] = string(*props.Status)
						res.Config["partition_ids"] = props.PartitionIDs

						// Capture description
						if props.CaptureDescription != nil {
							capture := props.CaptureDescription
							captureConfig := map[string]interface{}{
								"enabled":             capture.Enabled,
								"encoding":            string(*capture.Encoding),
								"interval_in_seconds": capture.IntervalInSeconds,
								"size_limit_in_bytes": capture.SizeLimitInBytes,
								"skip_empty_archives": capture.SkipEmptyArchives,
							}
							if capture.Destination != nil {
								captureConfig["destination_name"] = capture.Destination.Name
							}
							res.Config["capture_description"] = captureConfig
						}
					}

					infra.AddResource(res)
				}
			}
		}
	}

	return nil
}

// scanEventGrid discovers Azure Event Grid topics.
func (p *APIParser) scanEventGrid(ctx context.Context, cred interface{}, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeEventGrid, opts) {
		return nil
	}

	azCred, err := p.credConfig.GetCredential(ctx)
	if err != nil {
		return err
	}

	clientFactory, err := armeventgrid.NewClientFactory(p.subscriptionID, azCred, nil)
	if err != nil {
		return fmt.Errorf("failed to create Event Grid client: %w", err)
	}

	client := clientFactory.NewTopicsClient()
	pager := client.NewListBySubscriptionPager(nil)

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list Event Grid topics: %w", err)
		}

		for _, topic := range page.Value {
			name := *topic.Name
			res := resource.NewAWSResource(name, name, resource.TypeEventGrid)
			res.Region = *topic.Location
			res.ARN = *topic.ID

			// Config
			if topic.Properties != nil {
				props := topic.Properties
				res.Config["provisioning_state"] = string(*props.ProvisioningState)
				res.Config["endpoint"] = props.Endpoint
				res.Config["input_schema"] = string(*props.InputSchema)
				res.Config["public_network_access"] = string(*props.PublicNetworkAccess)
				res.Config["disable_local_auth"] = props.DisableLocalAuth
			}

			// Tags
			for k, v := range topic.Tags {
				if v != nil {
					res.Tags[k] = *v
				}
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanLogicApp discovers Azure Logic App workflows.
func (p *APIParser) scanLogicApp(ctx context.Context, cred interface{}, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeLogicApp, opts) {
		return nil
	}

	azCred, err := p.credConfig.GetCredential(ctx)
	if err != nil {
		return err
	}

	clientFactory, err := armlogic.NewClientFactory(p.subscriptionID, azCred, nil)
	if err != nil {
		return fmt.Errorf("failed to create Logic App client: %w", err)
	}

	client := clientFactory.NewWorkflowsClient()
	pager := client.NewListBySubscriptionPager(nil)

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list Logic App workflows: %w", err)
		}

		for _, workflow := range page.Value {
			name := *workflow.Name
			res := resource.NewAWSResource(name, name, resource.TypeLogicApp)
			res.Region = *workflow.Location
			res.ARN = *workflow.ID

			// Config
			if workflow.Properties != nil {
				props := workflow.Properties
				res.Config["provisioning_state"] = string(*props.ProvisioningState)
				res.Config["state"] = string(*props.State)
				res.Config["version"] = props.Version
				res.Config["access_endpoint"] = props.AccessEndpoint
				res.Config["created_time"] = props.CreatedTime
				res.Config["changed_time"] = props.ChangedTime

				// Integration account if present
				if props.IntegrationAccount != nil {
					res.Config["integration_account_id"] = props.IntegrationAccount.ID
				}

				// Integration service environment if present
				if props.IntegrationServiceEnvironment != nil {
					res.Config["integration_service_environment_id"] = props.IntegrationServiceEnvironment.ID
				}

				// Note: we don't include the full definition as it can be very large
				// Instead, we just indicate if it exists
				if props.Definition != nil {
					res.Config["has_definition"] = true
				}
			}

			// Tags
			for k, v := range workflow.Tags {
				if v != nil {
					res.Tags[k] = *v
				}
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanKeyVault discovers Azure Key Vaults.
func (p *APIParser) scanKeyVault(ctx context.Context, cred interface{}, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeKeyVault, opts) {
		return nil
	}

	azCred, err := p.credConfig.GetCredential(ctx)
	if err != nil {
		return err
	}

	clientFactory, err := armkeyvault.NewClientFactory(p.subscriptionID, azCred, nil)
	if err != nil {
		return fmt.Errorf("failed to create Key Vault client: %w", err)
	}

	client := clientFactory.NewVaultsClient()
	pager := client.NewListBySubscriptionPager(nil)

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list Key Vaults: %w", err)
		}

		for _, vault := range page.Value {
			name := *vault.Name
			res := resource.NewAWSResource(name, name, resource.TypeKeyVault)
			res.Region = *vault.Location
			res.ARN = *vault.ID

			// Config
			if vault.Properties != nil {
				props := vault.Properties
				res.Config["vault_uri"] = props.VaultURI
				res.Config["tenant_id"] = props.TenantID
				res.Config["provisioning_state"] = string(*props.ProvisioningState)
				res.Config["enabled_for_deployment"] = props.EnabledForDeployment
				res.Config["enabled_for_disk_encryption"] = props.EnabledForDiskEncryption
				res.Config["enabled_for_template_deployment"] = props.EnabledForTemplateDeployment
				res.Config["enable_soft_delete"] = props.EnableSoftDelete
				res.Config["soft_delete_retention_in_days"] = props.SoftDeleteRetentionInDays
				res.Config["enable_purge_protection"] = props.EnablePurgeProtection
				res.Config["enable_rbac_authorization"] = props.EnableRbacAuthorization
				res.Config["public_network_access"] = props.PublicNetworkAccess

				// SKU
				if props.SKU != nil {
					res.Config["sku_family"] = string(*props.SKU.Family)
					res.Config["sku_name"] = string(*props.SKU.Name)
				}

				// Network ACLs
				if props.NetworkACLs != nil {
					networkConfig := map[string]interface{}{
						"default_action": string(*props.NetworkACLs.DefaultAction),
						"bypass":         string(*props.NetworkACLs.Bypass),
					}

					if props.NetworkACLs.IPRules != nil {
						var ipRules []string
						for _, rule := range props.NetworkACLs.IPRules {
							ipRules = append(ipRules, *rule.Value)
						}
						networkConfig["ip_rules"] = ipRules
					}

					if props.NetworkACLs.VirtualNetworkRules != nil {
						var vnetRules []string
						for _, rule := range props.NetworkACLs.VirtualNetworkRules {
							vnetRules = append(vnetRules, *rule.ID)
						}
						networkConfig["virtual_network_rules"] = vnetRules
					}

					res.Config["network_acls"] = networkConfig
				}

				// Access policies (simplified - just count and principals)
				if props.AccessPolicies != nil {
					res.Config["access_policy_count"] = len(props.AccessPolicies)
				}
			}

			// Tags
			for k, v := range vault.Tags {
				if v != nil {
					res.Tags[k] = *v
				}
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanAzureFirewall discovers Azure Firewalls.
func (p *APIParser) scanAzureFirewall(ctx context.Context, cred interface{}, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeAzureFirewall, opts) {
		return nil
	}

	azCred, err := p.credConfig.GetCredential(ctx)
	if err != nil {
		return err
	}

	clientFactory, err := armnetwork.NewClientFactory(p.subscriptionID, azCred, nil)
	if err != nil {
		return fmt.Errorf("failed to create network client: %w", err)
	}

	client := clientFactory.NewAzureFirewallsClient()
	pager := client.NewListAllPager(nil)

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list Azure Firewalls: %w", err)
		}

		for _, fw := range page.Value {
			name := *fw.Name
			res := resource.NewAWSResource(name, name, resource.TypeAzureFirewall)
			res.Region = *fw.Location
			res.ARN = *fw.ID

			// Config
			if fw.Properties != nil {
				props := fw.Properties
				res.Config["provisioning_state"] = string(*props.ProvisioningState)
				res.Config["threat_intel_mode"] = string(*props.ThreatIntelMode)

				// SKU
				if props.SKU != nil {
					res.Config["sku_name"] = string(*props.SKU.Name)
					res.Config["sku_tier"] = string(*props.SKU.Tier)
				}

				// Firewall policy
				if props.FirewallPolicy != nil {
					res.Config["firewall_policy_id"] = props.FirewallPolicy.ID
				}

				// IP configurations
				if props.IPConfigurations != nil {
					var ipConfigs []map[string]interface{}
					for _, ipConfig := range props.IPConfigurations {
						config := map[string]interface{}{
							"name": ipConfig.Name,
						}
						if ipConfig.Properties != nil {
							if ipConfig.Properties.PrivateIPAddress != nil {
								config["private_ip_address"] = *ipConfig.Properties.PrivateIPAddress
							}
							if ipConfig.Properties.PublicIPAddress != nil && ipConfig.Properties.PublicIPAddress.ID != nil {
								config["public_ip_address_id"] = extractResourceName(*ipConfig.Properties.PublicIPAddress.ID)
							}
							if ipConfig.Properties.Subnet != nil && ipConfig.Properties.Subnet.ID != nil {
								config["subnet_id"] = extractResourceName(*ipConfig.Properties.Subnet.ID)
							}
						}
						ipConfigs = append(ipConfigs, config)
					}
					res.Config["ip_configurations"] = ipConfigs
				}

				// Management IP configuration
				if props.ManagementIPConfiguration != nil {
					mgmtConfig := map[string]interface{}{
						"name": props.ManagementIPConfiguration.Name,
					}
					if props.ManagementIPConfiguration.Properties != nil {
						if props.ManagementIPConfiguration.Properties.PublicIPAddress != nil {
							mgmtConfig["public_ip_address_id"] = props.ManagementIPConfiguration.Properties.PublicIPAddress.ID
						}
					}
					res.Config["management_ip_configuration"] = mgmtConfig
				}

				// Hub IP addresses (for Virtual WAN scenario)
				if props.HubIPAddresses != nil {
					var publicIPs []string
					if props.HubIPAddresses.PublicIPs != nil {
						for _, pip := range props.HubIPAddresses.PublicIPs.Addresses {
							if pip.Address != nil {
								publicIPs = append(publicIPs, *pip.Address)
							}
						}
					}
					res.Config["hub_public_ip_addresses"] = publicIPs
					if props.HubIPAddresses.PrivateIPAddress != nil {
						res.Config["hub_private_ip_address"] = *props.HubIPAddresses.PrivateIPAddress
					}
				}

				// DNS settings
				if props.AdditionalProperties != nil {
					res.Config["additional_properties"] = props.AdditionalProperties
				}

				// Availability zones
				if fw.Zones != nil {
					res.Config["availability_zones"] = fw.Zones
				}
			}

			// Tags
			for k, v := range fw.Tags {
				if v != nil {
					res.Tags[k] = *v
				}
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanBlobStorage discovers Azure Blob containers.
func (p *APIParser) scanBlobStorage(ctx context.Context, cred interface{}, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeBlobStorage, opts) {
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

	accountsClient := clientFactory.NewAccountsClient()
	containersClient := clientFactory.NewBlobContainersClient()

	accountPager := accountsClient.NewListPager(nil)

	for accountPager.More() {
		accountPage, err := accountPager.NextPage(ctx)
		if err != nil {
			continue
		}

		for _, account := range accountPage.Value {
			accountName := *account.Name
			resourceGroup := extractResourceGroup(*account.ID)

			containerPager := containersClient.NewListPager(resourceGroup, accountName, nil)

			for containerPager.More() {
				containerPage, err := containerPager.NextPage(ctx)
				if err != nil {
					break
				}

				for _, container := range containerPage.Value {
					containerName := *container.Name
					fullName := fmt.Sprintf("%s/%s", accountName, containerName)

					res := resource.NewAWSResource(fullName, containerName, resource.TypeBlobStorage)
					res.Region = *account.Location
					res.ARN = *container.ID

					// Config
					res.Config["storage_account"] = accountName
					res.Config["container_name"] = containerName
					res.Config["resource_group"] = resourceGroup

					if container.Properties != nil {
						props := container.Properties
						if props.PublicAccess != nil {
							res.Config["public_access"] = string(*props.PublicAccess)
						}
						if props.LeaseStatus != nil {
							res.Config["lease_status"] = string(*props.LeaseStatus)
						}
						if props.LeaseState != nil {
							res.Config["lease_state"] = string(*props.LeaseState)
						}
						res.Config["last_modified_time"] = props.LastModifiedTime
						res.Config["has_immutability_policy"] = props.HasImmutabilityPolicy
						res.Config["has_legal_hold"] = props.HasLegalHold
						res.Config["default_encryption_scope"] = props.DefaultEncryptionScope
						res.Config["deny_encryption_scope_override"] = props.DenyEncryptionScopeOverride

						if props.ImmutableStorageWithVersioning != nil {
							res.Config["immutable_storage_enabled"] = props.ImmutableStorageWithVersioning.Enabled
						}

						// Container metadata
						if props.Metadata != nil {
							for k, v := range props.Metadata {
								if v != nil {
									res.Tags[k] = *v
								}
							}
						}
					}

					infra.AddResource(res)
				}
			}
		}
	}

	return nil
}

// scanManagedDisk discovers Azure Managed Disks.
func (p *APIParser) scanManagedDisk(ctx context.Context, cred interface{}, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeManagedDisk, opts) {
		return nil
	}

	azCred, err := p.credConfig.GetCredential(ctx)
	if err != nil {
		return err
	}

	clientFactory, err := armcompute.NewClientFactory(p.subscriptionID, azCred, nil)
	if err != nil {
		return fmt.Errorf("failed to create compute client: %w", err)
	}

	client := clientFactory.NewDisksClient()
	pager := client.NewListPager(nil)

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list managed disks: %w", err)
		}

		for _, disk := range page.Value {
			name := *disk.Name
			res := resource.NewAWSResource(name, name, resource.TypeManagedDisk)
			res.Region = *disk.Location
			res.ARN = *disk.ID

			// Config
			if disk.SKU != nil {
				if disk.SKU.Name != nil {
					res.Config["sku_name"] = string(*disk.SKU.Name)
				}
				res.Config["sku_tier"] = disk.SKU.Tier
			}

			if disk.Properties != nil {
				props := disk.Properties
				res.Config["disk_size_gb"] = props.DiskSizeGB
				if props.DiskState != nil {
					res.Config["disk_state"] = string(*props.DiskState)
				}
				res.Config["provisioning_state"] = props.ProvisioningState
				res.Config["time_created"] = props.TimeCreated
				res.Config["unique_id"] = props.UniqueID

				if props.OSType != nil {
					res.Config["os_type"] = string(*props.OSType)
				}

				// Disk IOPS and throughput
				res.Config["disk_iops_read_write"] = props.DiskIOPSReadWrite
				res.Config["disk_mbps_read_write"] = props.DiskMBpsReadWrite

				// Encryption
				if props.Encryption != nil {
					if props.Encryption.Type != nil {
						res.Config["encryption_type"] = string(*props.Encryption.Type)
					}
					if props.Encryption.DiskEncryptionSetID != nil {
						res.Config["disk_encryption_set_id"] = *props.Encryption.DiskEncryptionSetID
					}
				}

				// Network access
				if props.NetworkAccessPolicy != nil {
					res.Config["network_access_policy"] = string(*props.NetworkAccessPolicy)
				}
				if props.PublicNetworkAccess != nil {
					res.Config["public_network_access"] = string(*props.PublicNetworkAccess)
				}

				// Source info
				if props.CreationData != nil {
					if props.CreationData.CreateOption != nil {
						res.Config["create_option"] = string(*props.CreationData.CreateOption)
					}
					if props.CreationData.SourceResourceID != nil {
						res.Config["source_resource_id"] = *props.CreationData.SourceResourceID
					}
					if props.CreationData.SourceURI != nil {
						res.Config["source_uri"] = *props.CreationData.SourceURI
					}
					if props.CreationData.ImageReference != nil {
						res.Config["image_reference_id"] = props.CreationData.ImageReference.ID
					}
				}

				// Zones
				if disk.Zones != nil {
					res.Config["zones"] = disk.Zones
				}

				// Max shares (for shared disks)
				res.Config["max_shares"] = props.MaxShares
			}

			// Attached VMs (ManagedBy is on the disk, not properties)
			if disk.ManagedBy != nil {
				res.Config["managed_by"] = extractResourceName(*disk.ManagedBy)
			}

			// Tags
			for k, v := range disk.Tags {
				if v != nil {
					res.Tags[k] = *v
				}
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanAzureFiles discovers Azure File Shares.
func (p *APIParser) scanAzureFiles(ctx context.Context, cred interface{}, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeAzureFiles, opts) {
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

	accountsClient := clientFactory.NewAccountsClient()
	sharesClient := clientFactory.NewFileSharesClient()

	accountPager := accountsClient.NewListPager(nil)

	for accountPager.More() {
		accountPage, err := accountPager.NextPage(ctx)
		if err != nil {
			continue
		}

		for _, account := range accountPage.Value {
			accountName := *account.Name
			resourceGroup := extractResourceGroup(*account.ID)

			sharePager := sharesClient.NewListPager(resourceGroup, accountName, nil)

			for sharePager.More() {
				sharePage, err := sharePager.NextPage(ctx)
				if err != nil {
					break
				}

				for _, share := range sharePage.Value {
					shareName := *share.Name
					fullName := fmt.Sprintf("%s/%s", accountName, shareName)

					res := resource.NewAWSResource(fullName, shareName, resource.TypeAzureFiles)
					res.Region = *account.Location
					res.ARN = *share.ID

					// Config
					res.Config["storage_account"] = accountName
					res.Config["share_name"] = shareName
					res.Config["resource_group"] = resourceGroup

					if share.Properties != nil {
						props := share.Properties
						res.Config["share_quota"] = props.ShareQuota
						if props.AccessTier != nil {
							res.Config["access_tier"] = string(*props.AccessTier)
						}
						res.Config["access_tier_change_time"] = props.AccessTierChangeTime
						if props.EnabledProtocols != nil {
							res.Config["enabled_protocols"] = string(*props.EnabledProtocols)
						}
						res.Config["last_modified_time"] = props.LastModifiedTime
						res.Config["share_usage_bytes"] = props.ShareUsageBytes
						if props.LeaseStatus != nil {
							res.Config["lease_status"] = string(*props.LeaseStatus)
						}
						if props.LeaseState != nil {
							res.Config["lease_state"] = string(*props.LeaseState)
						}

						// Root squash for NFS
						if props.RootSquash != nil {
							res.Config["root_squash"] = string(*props.RootSquash)
						}

						// Snapshot info
						if props.SnapshotTime != nil {
							res.Config["snapshot_time"] = props.SnapshotTime
						}

						// Deleted info
						if props.Deleted != nil && *props.Deleted {
							res.Config["deleted"] = true
							res.Config["deleted_time"] = props.DeletedTime
							res.Config["remaining_retention_days"] = props.RemainingRetentionDays
						}

						// Metadata
						if props.Metadata != nil {
							for k, v := range props.Metadata {
								if v != nil {
									res.Tags[k] = *v
								}
							}
						}
					}

					infra.AddResource(res)
				}
			}
		}
	}

	return nil
}

// scanAzureFunction discovers Azure Functions.
func (p *APIParser) scanAzureFunction(ctx context.Context, cred interface{}, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeAzureFunction, opts) {
		return nil
	}

	azCred, err := p.credConfig.GetCredential(ctx)
	if err != nil {
		return err
	}

	client, err := armappservice.NewWebAppsClient(p.subscriptionID, azCred, nil)
	if err != nil {
		return fmt.Errorf("failed to create App Service client: %w", err)
	}

	pager := client.NewListPager(nil)

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list web apps: %w", err)
		}

		for _, app := range page.Value {
			// Filter for function apps only
			if app.Kind == nil || !strings.Contains(*app.Kind, "functionapp") {
				continue
			}

			name := *app.Name
			res := resource.NewAWSResource(name, name, resource.TypeAzureFunction)
			res.Region = *app.Location
			res.ARN = *app.ID

			// Config
			res.Config["kind"] = app.Kind

			if app.Properties != nil {
				props := app.Properties
				res.Config["state"] = props.State
				res.Config["enabled"] = props.Enabled
				res.Config["default_host_name"] = props.DefaultHostName
				res.Config["https_only"] = props.HTTPSOnly
				res.Config["client_cert_enabled"] = props.ClientCertEnabled
				res.Config["client_cert_mode"] = string(*props.ClientCertMode)
				res.Config["availability_state"] = string(*props.AvailabilityState)
				res.Config["usage_state"] = string(*props.UsageState)

				// Hosting environment
				if props.HostingEnvironmentProfile != nil {
					res.Config["hosting_environment_id"] = props.HostingEnvironmentProfile.ID
				}

				// Outbound IPs
				res.Config["outbound_ip_addresses"] = props.OutboundIPAddresses
				res.Config["possible_outbound_ip_addresses"] = props.PossibleOutboundIPAddresses

				// Managed identity
				if app.Identity != nil {
					res.Config["identity_type"] = string(*app.Identity.Type)
					res.Config["identity_principal_id"] = app.Identity.PrincipalID
				}

				// Resource group
				res.Config["resource_group"] = props.ResourceGroup
			}

			// Tags
			for k, v := range app.Tags {
				if v != nil {
					res.Tags[k] = *v
				}
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanAppService discovers Azure App Services.
func (p *APIParser) scanAppService(ctx context.Context, cred interface{}, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeAppService, opts) {
		return nil
	}

	azCred, err := p.credConfig.GetCredential(ctx)
	if err != nil {
		return err
	}

	client, err := armappservice.NewWebAppsClient(p.subscriptionID, azCred, nil)
	if err != nil {
		return fmt.Errorf("failed to create App Service client: %w", err)
	}

	pager := client.NewListPager(nil)

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list web apps: %w", err)
		}

		for _, app := range page.Value {
			// Filter out function apps (already handled separately)
			if app.Kind != nil && strings.Contains(*app.Kind, "functionapp") {
				continue
			}

			name := *app.Name
			res := resource.NewAWSResource(name, name, resource.TypeAppService)
			res.Region = *app.Location
			res.ARN = *app.ID

			// Config
			res.Config["kind"] = app.Kind

			if app.Properties != nil {
				props := app.Properties
				res.Config["state"] = props.State
				res.Config["enabled"] = props.Enabled
				res.Config["default_host_name"] = props.DefaultHostName
				res.Config["https_only"] = props.HTTPSOnly
				res.Config["client_cert_enabled"] = props.ClientCertEnabled
				res.Config["availability_state"] = string(*props.AvailabilityState)
				res.Config["usage_state"] = string(*props.UsageState)

				// Repository site name
				res.Config["repository_site_name"] = props.RepositorySiteName

				// Host names
				res.Config["host_names"] = props.HostNames
				res.Config["enabled_host_names"] = props.EnabledHostNames

				// SSL states
				if props.HostNameSSLStates != nil {
					var sslStates []map[string]interface{}
					for _, ssl := range props.HostNameSSLStates {
						sslStates = append(sslStates, map[string]interface{}{
							"name":      ssl.Name,
							"ssl_state": string(*ssl.SSLState),
						})
					}
					res.Config["host_name_ssl_states"] = sslStates
				}

				// Resource group
				res.Config["resource_group"] = props.ResourceGroup

				// Managed identity
				if app.Identity != nil {
					res.Config["identity_type"] = string(*app.Identity.Type)
				}
			}

			// Tags
			for k, v := range app.Tags {
				if v != nil {
					res.Tags[k] = *v
				}
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanAzureLB discovers Azure Load Balancers.
func (p *APIParser) scanAzureLB(ctx context.Context, cred interface{}, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeAzureLB, opts) {
		return nil
	}

	azCred, err := p.credConfig.GetCredential(ctx)
	if err != nil {
		return err
	}

	clientFactory, err := armnetwork.NewClientFactory(p.subscriptionID, azCred, nil)
	if err != nil {
		return fmt.Errorf("failed to create network client: %w", err)
	}

	client := clientFactory.NewLoadBalancersClient()
	pager := client.NewListAllPager(nil)

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list load balancers: %w", err)
		}

		for _, lb := range page.Value {
			name := *lb.Name
			res := resource.NewAWSResource(name, name, resource.TypeAzureLB)
			res.Region = *lb.Location
			res.ARN = *lb.ID

			// Config
			if lb.SKU != nil {
				res.Config["sku_name"] = string(*lb.SKU.Name)
				res.Config["sku_tier"] = string(*lb.SKU.Tier)
			}

			if lb.Properties != nil {
				props := lb.Properties
				res.Config["provisioning_state"] = string(*props.ProvisioningState)

				// Frontend IP configurations
				if props.FrontendIPConfigurations != nil {
					var frontends []map[string]interface{}
					for _, fe := range props.FrontendIPConfigurations {
						frontend := map[string]interface{}{
							"name": fe.Name,
						}
						if fe.Properties != nil {
							if fe.Properties.PrivateIPAddress != nil {
								frontend["private_ip"] = *fe.Properties.PrivateIPAddress
							}
							if fe.Properties.PrivateIPAllocationMethod != nil {
								frontend["private_ip_allocation"] = string(*fe.Properties.PrivateIPAllocationMethod)
							}
							if fe.Properties.PublicIPAddress != nil && fe.Properties.PublicIPAddress.ID != nil {
								frontend["public_ip_id"] = extractResourceName(*fe.Properties.PublicIPAddress.ID)
							}
						}
						if fe.Zones != nil {
							frontend["zones"] = fe.Zones
						}
						frontends = append(frontends, frontend)
					}
					res.Config["frontend_ip_configurations"] = frontends
				}

				// Backend address pools
				if props.BackendAddressPools != nil {
					var backends []map[string]interface{}
					for _, be := range props.BackendAddressPools {
						backends = append(backends, map[string]interface{}{
							"name": be.Name,
						})
					}
					res.Config["backend_address_pools"] = backends
				}

				// Load balancing rules
				if props.LoadBalancingRules != nil {
					var rules []map[string]interface{}
					for _, rule := range props.LoadBalancingRules {
						ruleConfig := map[string]interface{}{
							"name": rule.Name,
						}
						if rule.Properties != nil {
							ruleConfig["protocol"] = string(*rule.Properties.Protocol)
							ruleConfig["frontend_port"] = rule.Properties.FrontendPort
							ruleConfig["backend_port"] = rule.Properties.BackendPort
							ruleConfig["enable_floating_ip"] = rule.Properties.EnableFloatingIP
							ruleConfig["idle_timeout_in_minutes"] = rule.Properties.IdleTimeoutInMinutes
							ruleConfig["load_distribution"] = string(*rule.Properties.LoadDistribution)
						}
						rules = append(rules, ruleConfig)
					}
					res.Config["load_balancing_rules"] = rules
				}

				// Health probes
				if props.Probes != nil {
					var probes []map[string]interface{}
					for _, probe := range props.Probes {
						probeConfig := map[string]interface{}{
							"name": probe.Name,
						}
						if probe.Properties != nil {
							probeConfig["protocol"] = string(*probe.Properties.Protocol)
							probeConfig["port"] = probe.Properties.Port
							probeConfig["interval_in_seconds"] = probe.Properties.IntervalInSeconds
							probeConfig["number_of_probes"] = probe.Properties.NumberOfProbes
						}
						probes = append(probes, probeConfig)
					}
					res.Config["probes"] = probes
				}
			}

			// Tags
			for k, v := range lb.Tags {
				if v != nil {
					res.Tags[k] = *v
				}
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanAzureVNet discovers Azure Virtual Networks.
func (p *APIParser) scanAzureVNet(ctx context.Context, cred interface{}, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeAzureVNet, opts) {
		return nil
	}

	azCred, err := p.credConfig.GetCredential(ctx)
	if err != nil {
		return err
	}

	clientFactory, err := armnetwork.NewClientFactory(p.subscriptionID, azCred, nil)
	if err != nil {
		return fmt.Errorf("failed to create network client: %w", err)
	}

	client := clientFactory.NewVirtualNetworksClient()
	pager := client.NewListAllPager(nil)

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list virtual networks: %w", err)
		}

		for _, vnet := range page.Value {
			name := *vnet.Name
			res := resource.NewAWSResource(name, name, resource.TypeAzureVNet)
			res.Region = *vnet.Location
			res.ARN = *vnet.ID

			// Config
			if vnet.Properties != nil {
				props := vnet.Properties
				res.Config["provisioning_state"] = string(*props.ProvisioningState)
				res.Config["enable_ddos_protection"] = props.EnableDdosProtection
				res.Config["enable_vm_protection"] = props.EnableVMProtection

				// Address space
				if props.AddressSpace != nil && props.AddressSpace.AddressPrefixes != nil {
					res.Config["address_prefixes"] = props.AddressSpace.AddressPrefixes
				}

				// DNS servers
				if props.DhcpOptions != nil && props.DhcpOptions.DNSServers != nil {
					res.Config["dns_servers"] = props.DhcpOptions.DNSServers
				}

				// Subnets
				if props.Subnets != nil {
					var subnets []map[string]interface{}
					for _, subnet := range props.Subnets {
						subnetConfig := map[string]interface{}{
							"name": subnet.Name,
						}
						if subnet.Properties != nil {
							subnetConfig["address_prefix"] = subnet.Properties.AddressPrefix

							if subnet.Properties.NetworkSecurityGroup != nil {
								subnetConfig["network_security_group_id"] = extractResourceName(*subnet.Properties.NetworkSecurityGroup.ID)
							}

							if subnet.Properties.RouteTable != nil {
								subnetConfig["route_table_id"] = extractResourceName(*subnet.Properties.RouteTable.ID)
							}

							if subnet.Properties.NatGateway != nil {
								subnetConfig["nat_gateway_id"] = extractResourceName(*subnet.Properties.NatGateway.ID)
							}

							subnetConfig["private_endpoint_network_policies"] = string(*subnet.Properties.PrivateEndpointNetworkPolicies)
							subnetConfig["private_link_service_network_policies"] = string(*subnet.Properties.PrivateLinkServiceNetworkPolicies)

							// Service endpoints
							if subnet.Properties.ServiceEndpoints != nil {
								var endpoints []string
								for _, se := range subnet.Properties.ServiceEndpoints {
									endpoints = append(endpoints, *se.Service)
								}
								subnetConfig["service_endpoints"] = endpoints
							}
						}
						subnets = append(subnets, subnetConfig)
					}
					res.Config["subnets"] = subnets
				}

				// VNet peerings
				if props.VirtualNetworkPeerings != nil {
					var peerings []map[string]interface{}
					for _, peering := range props.VirtualNetworkPeerings {
						peeringConfig := map[string]interface{}{
							"name": peering.Name,
						}
						if peering.Properties != nil {
							peeringConfig["peering_state"] = string(*peering.Properties.PeeringState)
							peeringConfig["peering_sync_level"] = string(*peering.Properties.PeeringSyncLevel)
							peeringConfig["allow_virtual_network_access"] = peering.Properties.AllowVirtualNetworkAccess
							peeringConfig["allow_forwarded_traffic"] = peering.Properties.AllowForwardedTraffic
							peeringConfig["allow_gateway_transit"] = peering.Properties.AllowGatewayTransit
							peeringConfig["use_remote_gateways"] = peering.Properties.UseRemoteGateways

							if peering.Properties.RemoteVirtualNetwork != nil {
								peeringConfig["remote_vnet_id"] = *peering.Properties.RemoteVirtualNetwork.ID
							}
						}
						peerings = append(peerings, peeringConfig)
					}
					res.Config["virtual_network_peerings"] = peerings
				}

				// Flow timeout
				res.Config["flow_timeout_in_minutes"] = props.FlowTimeoutInMinutes
			}

			// Tags
			for k, v := range vnet.Tags {
				if v != nil {
					res.Tags[k] = *v
				}
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanADB2C discovers Azure AD B2C directories via REST API.
// Note: There is no ARM SDK package for Azure AD B2C, so we use direct REST API calls.
func (p *APIParser) scanADB2C(ctx context.Context, cred interface{}, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeAzureADB2C, opts) {
		return nil
	}

	// Get Azure credential for token
	azCred, err := p.credConfig.GetCredential(ctx)
	if err != nil {
		return err
	}

	// Get access token for Azure Resource Manager
	token, err := azCred.GetToken(ctx, policy.TokenRequestOptions{
		Scopes: []string{"https://management.azure.com/.default"},
	})
	if err != nil {
		return fmt.Errorf("failed to get access token: %w", err)
	}

	// Build REST API URL
	apiURL := fmt.Sprintf(
		"https://management.azure.com/subscriptions/%s/providers/Microsoft.AzureActiveDirectory/b2cDirectories?api-version=2021-04-01",
		p.subscriptionID,
	)

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token.Token)
	req.Header.Set("Content-Type", "application/json")

	// Execute request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to list B2C directories: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to list B2C directories: status %d, body: %s", resp.StatusCode, string(body))
	}

	// Parse response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	var result b2cDirectoryListResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("failed to parse B2C directories response: %w", err)
	}

	// Process each B2C directory
	for _, b2c := range result.Value {
		name := b2c.Name
		res := resource.NewAWSResource(name, name, resource.TypeAzureADB2C)
		res.Region = b2c.Location
		res.ARN = b2c.ID

		// Config - basic properties
		res.Config["tenant_name"] = name
		res.Config["domain_name"] = name // Domain name is the same as resource name (e.g., contoso.onmicrosoft.com)

		// SKU information
		if b2c.SKU != nil {
			res.Config["sku_name"] = b2c.SKU.Name
			res.Config["sku_tier"] = b2c.SKU.Tier
		}

		// Properties
		if b2c.Properties != nil {
			if b2c.Properties.TenantID != "" {
				res.Config["tenant_id"] = b2c.Properties.TenantID
			}

			// Billing configuration
			if b2c.Properties.BillingConfig != nil {
				res.Config["billing_type"] = b2c.Properties.BillingConfig.BillingType
				if b2c.Properties.BillingConfig.EffectiveStartDateUtc != "" {
					res.Config["effective_start_date"] = b2c.Properties.BillingConfig.EffectiveStartDateUtc
				}
			}

			// Country code if available
			if b2c.Properties.CountryCode != "" {
				res.Config["country_code"] = b2c.Properties.CountryCode
			}

			// Production tenant flag if available
			if b2c.Properties.IsProductionTenant != nil {
				res.Config["is_production_tenant"] = *b2c.Properties.IsProductionTenant
			}
		}

		// Location serves as region for B2C (e.g., "United States", "Europe")
		res.Config["location"] = b2c.Location

		// Tags
		for k, v := range b2c.Tags {
			res.Tags[k] = v
		}

		infra.AddResource(res)
	}

	return nil
}

// b2cDirectoryListResponse represents the response from listing B2C directories.
type b2cDirectoryListResponse struct {
	Value []b2cDirectory `json:"value"`
}

// b2cDirectory represents an Azure AD B2C directory resource.
type b2cDirectory struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags"`
	SKU        *b2cSKU           `json:"sku"`
	Properties *b2cProperties    `json:"properties"`
}

// b2cSKU represents the SKU of a B2C directory.
type b2cSKU struct {
	Name string `json:"name"`
	Tier string `json:"tier"`
}

// b2cProperties represents the properties of a B2C directory.
type b2cProperties struct {
	TenantID           string            `json:"tenantId"`
	BillingConfig      *b2cBillingConfig `json:"billingConfig"`
	CountryCode        string            `json:"countryCode,omitempty"`
	IsProductionTenant *bool             `json:"isProductionTenant,omitempty"`
}

// b2cBillingConfig represents the billing configuration of a B2C directory.
type b2cBillingConfig struct {
	BillingType           string `json:"billingType"`
	EffectiveStartDateUtc string `json:"effectiveStartDateUtc"`
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
