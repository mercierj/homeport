package gcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	compute "cloud.google.com/go/compute/apiv1"
	"cloud.google.com/go/compute/apiv1/computepb"
	redis "cloud.google.com/go/redis/apiv1"
	"cloud.google.com/go/redis/apiv1/redispb"
	run "cloud.google.com/go/run/apiv2"
	"cloud.google.com/go/run/apiv2/runpb"
	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	sqladmin "google.golang.org/api/sqladmin/v1beta4"

	"github.com/cloudexit/cloudexit/internal/domain/parser"
	"github.com/cloudexit/cloudexit/internal/domain/resource"
)

// APIParser discovers GCP infrastructure via API calls.
type APIParser struct {
	credConfig  *CredentialConfig
	clientOpts  []option.ClientOption
	project     string
	identity    *CallerIdentity
}

// NewAPIParser creates a new GCP API parser.
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
	return resource.ProviderGCP
}

// SupportedFormats returns the supported formats.
func (p *APIParser) SupportedFormats() []parser.Format {
	return []parser.Format{parser.FormatAPI}
}

// Validate checks if the parser can connect to GCP.
func (p *APIParser) Validate(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	opts, err := p.credConfig.ClientOptions(ctx)
	if err != nil {
		return fmt.Errorf("failed to get client options: %w", err)
	}
	p.clientOpts = opts

	project, err := p.credConfig.GetProject(ctx)
	if err != nil {
		return fmt.Errorf("failed to get project: %w", err)
	}
	p.project = project

	p.identity = &CallerIdentity{
		Project: project,
	}

	return nil
}

// AutoDetect checks for GCP credentials availability.
func (p *APIParser) AutoDetect(path string) (bool, float64) {
	source := DetectCredentialSource()
	if source != CredentialSourceDefault {
		return true, 0.7
	}

	// Try to validate default credentials
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	opts, err := p.credConfig.ClientOptions(ctx)
	if err != nil {
		return false, 0
	}

	_, err = p.credConfig.GetProject(ctx)
	if err != nil {
		return false, 0
	}

	p.clientOpts = opts
	return true, 0.6
}

// Parse discovers GCP infrastructure via API.
func (p *APIParser) Parse(ctx context.Context, path string, opts *parser.ParseOptions) (*resource.Infrastructure, error) {
	// Initialize credential config from options
	if opts != nil && opts.APICredentials != nil {
		p.credConfig = FromParseOptions(opts.APICredentials, opts.Regions)
	}

	// Load client options
	clientOpts, err := p.credConfig.ClientOptions(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get client options: %w", err)
	}
	p.clientOpts = clientOpts

	// Get project
	project, err := p.credConfig.GetProject(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get project: %w", err)
	}
	p.project = project

	// Create infrastructure
	infra := resource.NewInfrastructure(resource.ProviderGCP)
	infra.Metadata["project"] = project

	// Determine regions to scan
	regions := []string{"us-central1"}
	if opts != nil && len(opts.Regions) > 0 {
		regions = opts.Regions
	}

	// Scan resources
	if err := p.scanComputeInstances(ctx, infra, regions, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Compute Engine: %w", err)
	}

	if err := p.scanGCSBuckets(ctx, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Cloud Storage: %w", err)
	}

	if err := p.scanCloudSQL(ctx, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Cloud SQL: %w", err)
	}

	if err := p.scanCloudRun(ctx, infra, regions, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Cloud Run: %w", err)
	}

	if err := p.scanMemorystore(ctx, infra, regions, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Memorystore: %w", err)
	}

	return infra, nil
}

// scanComputeInstances discovers GCE instances.
func (p *APIParser) scanComputeInstances(ctx context.Context, infra *resource.Infrastructure, regions []string, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeGCEInstance, opts) {
		return nil
	}

	client, err := compute.NewInstancesRESTClient(ctx, p.clientOpts...)
	if err != nil {
		return fmt.Errorf("failed to create compute client: %w", err)
	}
	defer client.Close()

	for _, region := range regions {
		// List all zones in the region
		zones, err := p.getZonesForRegion(ctx, region)
		if err != nil {
			continue
		}

		for _, zone := range zones {
			req := &computepb.ListInstancesRequest{
				Project: p.project,
				Zone:    zone,
			}

			it := client.List(ctx, req)
			for {
				instance, err := it.Next()
				if err == iterator.Done {
					break
				}
				if err != nil {
					continue
				}

				// Skip terminated instances
				if instance.GetStatus() == "TERMINATED" {
					continue
				}

				res := resource.NewAWSResource(
					instance.GetName(),
					instance.GetName(),
					resource.TypeGCEInstance,
				)
				res.Region = region
				res.ARN = instance.GetSelfLink()

				// Config
				res.Config["machine_type"] = extractMachineType(instance.GetMachineType())
				res.Config["zone"] = zone
				res.Config["status"] = instance.GetStatus()

				// Network interfaces
				if len(instance.GetNetworkInterfaces()) > 0 {
					ni := instance.GetNetworkInterfaces()[0]
					res.Config["network"] = extractResourceName(ni.GetNetwork())
					res.Config["subnetwork"] = extractResourceName(ni.GetSubnetwork())
					res.Config["internal_ip"] = ni.GetNetworkIP()
					if len(ni.GetAccessConfigs()) > 0 {
						res.Config["external_ip"] = ni.GetAccessConfigs()[0].GetNatIP()
					}
				}

				// Disks
				var disks []map[string]interface{}
				for _, disk := range instance.GetDisks() {
					disks = append(disks, map[string]interface{}{
						"source":      extractResourceName(disk.GetSource()),
						"device_name": disk.GetDeviceName(),
						"boot":        disk.GetBoot(),
						"auto_delete": disk.GetAutoDelete(),
					})
				}
				res.Config["disks"] = disks

				// Labels
				for k, v := range instance.GetLabels() {
					res.Tags[k] = v
				}

				infra.AddResource(res)
			}
		}
	}

	return nil
}

// scanGCSBuckets discovers Cloud Storage buckets.
func (p *APIParser) scanGCSBuckets(ctx context.Context, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeGCSBucket, opts) {
		return nil
	}

	client, err := storage.NewClient(ctx, p.clientOpts...)
	if err != nil {
		return fmt.Errorf("failed to create storage client: %w", err)
	}
	defer client.Close()

	it := client.Buckets(ctx, p.project)
	for {
		bucket, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			continue
		}

		res := resource.NewAWSResource(
			bucket.Name,
			bucket.Name,
			resource.TypeGCSBucket,
		)
		res.Region = bucket.Location
		res.CreatedAt = bucket.Created

		// Config
		res.Config["location"] = bucket.Location
		res.Config["location_type"] = bucket.LocationType
		res.Config["storage_class"] = bucket.StorageClass
		res.Config["versioning_enabled"] = bucket.VersioningEnabled
		res.Config["uniform_bucket_level_access"] = bucket.UniformBucketLevelAccess.Enabled

		if bucket.Encryption != nil {
			res.Config["default_kms_key"] = bucket.Encryption.DefaultKMSKeyName
		}

		// Labels
		for k, v := range bucket.Labels {
			res.Tags[k] = v
		}

		infra.AddResource(res)
	}

	return nil
}

// scanCloudSQL discovers Cloud SQL instances.
func (p *APIParser) scanCloudSQL(ctx context.Context, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeCloudSQL, opts) {
		return nil
	}

	// Create the SQL Admin service using google.golang.org/api
	service, err := sqladmin.NewService(ctx, p.clientOpts...)
	if err != nil {
		return fmt.Errorf("failed to create Cloud SQL service: %w", err)
	}

	// List instances
	resp, err := service.Instances.List(p.project).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("failed to list Cloud SQL instances: %w", err)
	}

	for _, instance := range resp.Items {
		res := resource.NewAWSResource(
			instance.Name,
			instance.Name,
			resource.TypeCloudSQL,
		)
		res.Region = instance.Region

		// Config
		res.Config["database_version"] = instance.DatabaseVersion
		res.Config["state"] = instance.State

		if instance.Settings != nil {
			res.Config["tier"] = instance.Settings.Tier
			res.Config["data_disk_size_gb"] = instance.Settings.DataDiskSizeGb
			res.Config["data_disk_type"] = instance.Settings.DataDiskType
			res.Config["availability_type"] = instance.Settings.AvailabilityType

			if instance.Settings.BackupConfiguration != nil {
				res.Config["backup_enabled"] = instance.Settings.BackupConfiguration.Enabled
			}

			if instance.Settings.IpConfiguration != nil {
				res.Config["ipv4_enabled"] = instance.Settings.IpConfiguration.Ipv4Enabled
				res.Config["require_ssl"] = instance.Settings.IpConfiguration.RequireSsl
			}

			// Labels
			for k, v := range instance.Settings.UserLabels {
				res.Tags[k] = v
			}
		}

		// IP addresses
		var ips []map[string]string
		for _, ip := range instance.IpAddresses {
			ips = append(ips, map[string]string{
				"ip_address": ip.IpAddress,
				"type":       ip.Type,
			})
		}
		res.Config["ip_addresses"] = ips

		infra.AddResource(res)
	}

	return nil
}

// scanCloudRun discovers Cloud Run services.
func (p *APIParser) scanCloudRun(ctx context.Context, infra *resource.Infrastructure, regions []string, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeCloudRun, opts) {
		return nil
	}

	client, err := run.NewServicesClient(ctx, p.clientOpts...)
	if err != nil {
		return fmt.Errorf("failed to create Cloud Run client: %w", err)
	}
	defer client.Close()

	for _, region := range regions {
		req := &runpb.ListServicesRequest{
			Parent: fmt.Sprintf("projects/%s/locations/%s", p.project, region),
		}

		it := client.ListServices(ctx, req)
		for {
			service, err := it.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				continue
			}

			name := extractResourceName(service.GetName())
			res := resource.NewAWSResource(
				name,
				name,
				resource.TypeCloudRun,
			)
			res.Region = region
			res.ARN = service.GetName()

			// Config
			res.Config["uri"] = service.GetUri()
			res.Config["generation"] = service.GetGeneration()

			template := service.GetTemplate()
			if template != nil {
				res.Config["max_instance_count"] = template.GetMaxInstanceRequestConcurrency()
				res.Config["timeout"] = template.GetTimeout().GetSeconds()

				if len(template.GetContainers()) > 0 {
					container := template.GetContainers()[0]
					res.Config["image"] = container.GetImage()

					// Resources
					if container.GetResources() != nil {
						res.Config["cpu_limit"] = container.GetResources().GetLimits()["cpu"]
						res.Config["memory_limit"] = container.GetResources().GetLimits()["memory"]
					}

					// Ports
					var ports []int32
					for _, port := range container.GetPorts() {
						ports = append(ports, port.GetContainerPort())
					}
					res.Config["ports"] = ports

					// Environment variables (keys only for security)
					if !opts.IncludeSensitive {
						var envKeys []string
						for _, env := range container.GetEnv() {
							envKeys = append(envKeys, env.GetName())
						}
						res.Config["environment_keys"] = envKeys
					}
				}
			}

			// Labels
			for k, v := range service.GetLabels() {
				res.Tags[k] = v
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanMemorystore discovers Memorystore (Redis) instances.
func (p *APIParser) scanMemorystore(ctx context.Context, infra *resource.Infrastructure, regions []string, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeMemorystore, opts) {
		return nil
	}

	client, err := redis.NewCloudRedisClient(ctx, p.clientOpts...)
	if err != nil {
		return fmt.Errorf("failed to create Memorystore client: %w", err)
	}
	defer client.Close()

	for _, region := range regions {
		req := &redispb.ListInstancesRequest{
			Parent: fmt.Sprintf("projects/%s/locations/%s", p.project, region),
		}

		it := client.ListInstances(ctx, req)
		for {
			instance, err := it.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				continue
			}

			name := extractResourceName(instance.GetName())
			res := resource.NewAWSResource(
				name,
				name,
				resource.TypeMemorystore,
			)
			res.Region = region
			res.ARN = instance.GetName()

			// Config
			res.Config["tier"] = instance.GetTier().String()
			res.Config["memory_size_gb"] = instance.GetMemorySizeGb()
			res.Config["redis_version"] = instance.GetRedisVersion()
			res.Config["host"] = instance.GetHost()
			res.Config["port"] = instance.GetPort()
			res.Config["state"] = instance.GetState().String()
			res.Config["auth_enabled"] = instance.GetAuthEnabled()
			res.Config["transit_encryption_mode"] = instance.GetTransitEncryptionMode().String()

			// Labels
			for k, v := range instance.GetLabels() {
				res.Tags[k] = v
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// getZonesForRegion returns all zones in a region.
func (p *APIParser) getZonesForRegion(ctx context.Context, region string) ([]string, error) {
	client, err := compute.NewZonesRESTClient(ctx, p.clientOpts...)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	req := &computepb.ListZonesRequest{
		Project: p.project,
		Filter:  stringPtr(fmt.Sprintf("name:%s-*", region)),
	}

	var zones []string
	it := client.List(ctx, req)
	for {
		zone, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		zones = append(zones, zone.GetName())
	}

	return zones, nil
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

// extractMachineType extracts the machine type name from a full URL.
func extractMachineType(url string) string {
	// URL format: .../machineTypes/n1-standard-1
	parts := strings.Split(url, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return url
}

// extractResourceName extracts the resource name from a full path.
func extractResourceName(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return path
}

// stringPtr returns a pointer to the string.
func stringPtr(s string) *string {
	return &s
}
