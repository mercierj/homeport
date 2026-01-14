package gcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/bigtable"
	cloudtasks "cloud.google.com/go/cloudtasks/apiv2"
	"cloud.google.com/go/cloudtasks/apiv2/cloudtaskspb"
	compute "cloud.google.com/go/compute/apiv1"
	"cloud.google.com/go/compute/apiv1/computepb"
	container "cloud.google.com/go/container/apiv1"
	"cloud.google.com/go/container/apiv1/containerpb"
	filestore "cloud.google.com/go/filestore/apiv1"
	"cloud.google.com/go/filestore/apiv1/filestorepb"
	firestoreadmin "cloud.google.com/go/firestore/apiv1/admin"
	"cloud.google.com/go/firestore/apiv1/admin/adminpb"
	functions "cloud.google.com/go/functions/apiv2"
	"cloud.google.com/go/functions/apiv2/functionspb"
	"cloud.google.com/go/pubsub"
	redis "cloud.google.com/go/redis/apiv1"
	"cloud.google.com/go/redis/apiv1/redispb"
	run "cloud.google.com/go/run/apiv2"
	"cloud.google.com/go/run/apiv2/runpb"
	scheduler "cloud.google.com/go/scheduler/apiv1"
	"cloud.google.com/go/scheduler/apiv1/schedulerpb"
	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	spanner "cloud.google.com/go/spanner/admin/instance/apiv1"
	"cloud.google.com/go/spanner/admin/instance/apiv1/instancepb"
	"cloud.google.com/go/storage"
	appengine "google.golang.org/api/appengine/v1"
	cloudresourcemanager "google.golang.org/api/cloudresourcemanager/v1"
	dns "google.golang.org/api/dns/v1"
	identitytoolkit "google.golang.org/api/identitytoolkit/v2"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	sqladmin "google.golang.org/api/sqladmin/v1beta4"

	"github.com/homeport/homeport/internal/domain/parser"
	"github.com/homeport/homeport/internal/domain/resource"
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

	if err := p.scanFirestore(ctx, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Firestore: %w", err)
	}

	if err := p.scanBigtable(ctx, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Bigtable: %w", err)
	}

	if err := p.scanSpanner(ctx, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Spanner: %w", err)
	}

	if err := p.scanGKE(ctx, infra, regions, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan GKE: %w", err)
	}

	if err := p.scanCloudCDN(ctx, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Cloud CDN: %w", err)
	}

	if err := p.scanCloudLB(ctx, infra, regions, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Cloud LB: %w", err)
	}

	if err := p.scanPubSub(ctx, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Pub/Sub: %w", err)
	}

	if err := p.scanCloudTasks(ctx, infra, regions, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Cloud Tasks: %w", err)
	}

	if err := p.scanCloudDNS(ctx, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Cloud DNS: %w", err)
	}

	if err := p.scanIdentityPlatform(ctx, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Identity Platform: %w", err)
	}

	if err := p.scanSecretManager(ctx, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Secret Manager: %w", err)
	}

	if err := p.scanPersistentDisk(ctx, infra, regions, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Persistent Disks: %w", err)
	}

	if err := p.scanFilestore(ctx, infra, regions, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Filestore: %w", err)
	}

	if err := p.scanCloudFunctions(ctx, infra, regions, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Cloud Functions: %w", err)
	}

	if err := p.scanCloudArmor(ctx, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Cloud Armor: %w", err)
	}

	if err := p.scanGCPIAM(ctx, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan GCP IAM: %w", err)
	}

	if err := p.scanGCPVPCNetwork(ctx, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan VPC Networks: %w", err)
	}

	if err := p.scanAppEngine(ctx, infra, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan App Engine: %w", err)
	}

	if err := p.scanCloudScheduler(ctx, infra, regions, opts); err != nil && !opts.IgnoreErrors {
		return nil, fmt.Errorf("failed to scan Cloud Scheduler: %w", err)
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

// scanGKE discovers GKE clusters.
func (p *APIParser) scanGKE(ctx context.Context, infra *resource.Infrastructure, regions []string, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeGKE, opts) {
		return nil
	}

	client, err := container.NewClusterManagerClient(ctx, p.clientOpts...)
	if err != nil {
		return fmt.Errorf("failed to create GKE client: %w", err)
	}
	defer client.Close()

	for _, region := range regions {
		// List clusters in this region (using "-" to get all zones in region)
		req := &containerpb.ListClustersRequest{
			Parent: fmt.Sprintf("projects/%s/locations/%s", p.project, region),
		}

		resp, err := client.ListClusters(ctx, req)
		if err != nil {
			// Try with "-" for all locations if region-specific fails
			req.Parent = fmt.Sprintf("projects/%s/locations/-", p.project)
			resp, err = client.ListClusters(ctx, req)
			if err != nil {
				continue
			}
		}

		for _, cluster := range resp.Clusters {
			res := resource.NewAWSResource(
				cluster.Name,
				cluster.Name,
				resource.TypeGKE,
			)
			res.Region = cluster.Location
			res.ARN = cluster.SelfLink

			// Config
			res.Config["name"] = cluster.Name
			res.Config["location"] = cluster.Location
			res.Config["status"] = cluster.Status.String()
			res.Config["current_master_version"] = cluster.CurrentMasterVersion
			res.Config["current_node_version"] = cluster.CurrentNodeVersion
			res.Config["endpoint"] = cluster.Endpoint
			res.Config["services_ipv4_cidr"] = cluster.ServicesIpv4Cidr
			res.Config["cluster_ipv4_cidr"] = cluster.ClusterIpv4Cidr
			res.Config["initial_cluster_version"] = cluster.InitialClusterVersion
			res.Config["node_count"] = cluster.CurrentNodeCount
			res.Config["network"] = cluster.Network
			res.Config["subnetwork"] = cluster.Subnetwork

			// Node pools
			var nodePools []map[string]interface{}
			for _, np := range cluster.NodePools {
				nodePool := map[string]interface{}{
					"name":               np.Name,
					"status":             np.Status.String(),
					"initial_node_count": np.InitialNodeCount,
					"version":            np.Version,
				}

				if np.Config != nil {
					nodePool["machine_type"] = np.Config.MachineType
					nodePool["disk_size_gb"] = np.Config.DiskSizeGb
					nodePool["disk_type"] = np.Config.DiskType
					nodePool["preemptible"] = np.Config.Preemptible
					nodePool["spot"] = np.Config.Spot
				}

				if np.Autoscaling != nil && np.Autoscaling.Enabled {
					nodePool["autoscaling_enabled"] = true
					nodePool["min_node_count"] = np.Autoscaling.MinNodeCount
					nodePool["max_node_count"] = np.Autoscaling.MaxNodeCount
				}

				nodePools = append(nodePools, nodePool)
			}
			res.Config["node_pools"] = nodePools

			// Network policy
			if cluster.NetworkPolicy != nil {
				res.Config["network_policy_enabled"] = cluster.NetworkPolicy.Enabled
				res.Config["network_policy_provider"] = cluster.NetworkPolicy.Provider.String()
			}

			// Addons config
			if cluster.AddonsConfig != nil {
				addons := make(map[string]bool)
				if cluster.AddonsConfig.HttpLoadBalancing != nil {
					addons["http_load_balancing"] = !cluster.AddonsConfig.HttpLoadBalancing.Disabled
				}
				if cluster.AddonsConfig.HorizontalPodAutoscaling != nil {
					addons["horizontal_pod_autoscaling"] = !cluster.AddonsConfig.HorizontalPodAutoscaling.Disabled
				}
				if cluster.AddonsConfig.NetworkPolicyConfig != nil {
					addons["network_policy_config"] = !cluster.AddonsConfig.NetworkPolicyConfig.Disabled
				}
				res.Config["addons_config"] = addons
			}

			// Master auth
			if cluster.MasterAuth != nil {
				res.Config["master_auth_client_certificate"] = cluster.MasterAuth.ClientCertificate != ""
			}

			// Private cluster config
			if cluster.PrivateClusterConfig != nil {
				res.Config["enable_private_nodes"] = cluster.PrivateClusterConfig.EnablePrivateNodes
				res.Config["enable_private_endpoint"] = cluster.PrivateClusterConfig.EnablePrivateEndpoint
				res.Config["master_ipv4_cidr_block"] = cluster.PrivateClusterConfig.MasterIpv4CidrBlock
			}

			// Workload identity config
			if cluster.WorkloadIdentityConfig != nil {
				res.Config["workload_identity_pool"] = cluster.WorkloadIdentityConfig.WorkloadPool
			}

			// Labels
			for k, v := range cluster.ResourceLabels {
				res.Tags[k] = v
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanCloudCDN discovers Cloud CDN enabled backend services.
func (p *APIParser) scanCloudCDN(ctx context.Context, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeCloudCDN, opts) {
		return nil
	}

	client, err := compute.NewBackendServicesRESTClient(ctx, p.clientOpts...)
	if err != nil {
		return fmt.Errorf("failed to create backend services client: %w", err)
	}
	defer client.Close()

	// List all backend services (global)
	req := &computepb.ListBackendServicesRequest{
		Project: p.project,
	}

	it := client.List(ctx, req)
	for {
		backend, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			continue
		}

		// Only include backends with CDN enabled
		if !backend.GetEnableCDN() {
			continue
		}

		res := resource.NewAWSResource(
			backend.GetName(),
			backend.GetName(),
			resource.TypeCloudCDN,
		)
		res.Region = "global"
		res.ARN = backend.GetSelfLink()

		// Config
		res.Config["name"] = backend.GetName()
		res.Config["description"] = backend.GetDescription()
		res.Config["protocol"] = backend.GetProtocol()
		res.Config["port_name"] = backend.GetPortName()
		res.Config["timeout_sec"] = backend.GetTimeoutSec()
		res.Config["enable_cdn"] = backend.GetEnableCDN()
		res.Config["load_balancing_scheme"] = backend.GetLoadBalancingScheme()

		// CDN policy
		if backend.GetCdnPolicy() != nil {
			cdnPolicy := backend.GetCdnPolicy()
			policyConfig := map[string]interface{}{
				"cache_mode":                   cdnPolicy.GetCacheMode(),
				"client_ttl":                   cdnPolicy.GetClientTtl(),
				"default_ttl":                  cdnPolicy.GetDefaultTtl(),
				"max_ttl":                      cdnPolicy.GetMaxTtl(),
				"negative_caching":             cdnPolicy.GetNegativeCaching(),
				"serve_while_stale":            cdnPolicy.GetServeWhileStale(),
				"signed_url_cache_max_age_sec": cdnPolicy.GetSignedUrlCacheMaxAgeSec(),
			}

			if cdnPolicy.GetSignedUrlKeyNames() != nil {
				policyConfig["signed_url_key_names"] = cdnPolicy.GetSignedUrlKeyNames()
			}

			if cdnPolicy.GetCacheKeyPolicy() != nil {
				ckp := cdnPolicy.GetCacheKeyPolicy()
				policyConfig["cache_key_policy"] = map[string]interface{}{
					"include_host":         ckp.GetIncludeHost(),
					"include_protocol":     ckp.GetIncludeProtocol(),
					"include_query_string": ckp.GetIncludeQueryString(),
				}
			}

			res.Config["cdn_policy"] = policyConfig
		}

		// Backends
		if backend.GetBackends() != nil {
			var backends []map[string]interface{}
			for _, b := range backend.GetBackends() {
				backends = append(backends, map[string]interface{}{
					"group":           extractResourceName(b.GetGroup()),
					"balancing_mode":  b.GetBalancingMode(),
					"capacity_scaler": b.GetCapacityScaler(),
					"max_utilization": b.GetMaxUtilization(),
				})
			}
			res.Config["backends"] = backends
		}

		// Health checks
		if backend.GetHealthChecks() != nil {
			var healthChecks []string
			for _, hc := range backend.GetHealthChecks() {
				healthChecks = append(healthChecks, extractResourceName(hc))
			}
			res.Config["health_checks"] = healthChecks
		}

		infra.AddResource(res)
	}

	return nil
}

// scanCloudLB discovers Cloud Load Balancers (URL maps).
func (p *APIParser) scanCloudLB(ctx context.Context, infra *resource.Infrastructure, regions []string, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeCloudLB, opts) {
		return nil
	}

	// Scan global URL maps
	globalClient, err := compute.NewUrlMapsRESTClient(ctx, p.clientOpts...)
	if err != nil {
		return fmt.Errorf("failed to create URL maps client: %w", err)
	}
	defer globalClient.Close()

	req := &computepb.ListUrlMapsRequest{
		Project: p.project,
	}

	it := globalClient.List(ctx, req)
	for {
		urlMap, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			continue
		}

		res := resource.NewAWSResource(
			urlMap.GetName(),
			urlMap.GetName(),
			resource.TypeCloudLB,
		)
		res.Region = "global"
		res.ARN = urlMap.GetSelfLink()

		// Config
		res.Config["name"] = urlMap.GetName()
		res.Config["description"] = urlMap.GetDescription()
		res.Config["default_service"] = extractResourceName(urlMap.GetDefaultService())
		res.Config["fingerprint"] = urlMap.GetFingerprint()

		// Host rules
		if urlMap.GetHostRules() != nil {
			var hostRules []map[string]interface{}
			for _, hr := range urlMap.GetHostRules() {
				hostRules = append(hostRules, map[string]interface{}{
					"hosts":        hr.GetHosts(),
					"path_matcher": hr.GetPathMatcher(),
				})
			}
			res.Config["host_rules"] = hostRules
		}

		// Path matchers
		if urlMap.GetPathMatchers() != nil {
			var pathMatchers []map[string]interface{}
			for _, pm := range urlMap.GetPathMatchers() {
				pathMatcher := map[string]interface{}{
					"name":            pm.GetName(),
					"default_service": extractResourceName(pm.GetDefaultService()),
				}

				if pm.GetPathRules() != nil {
					var pathRules []map[string]interface{}
					for _, pr := range pm.GetPathRules() {
						pathRules = append(pathRules, map[string]interface{}{
							"paths":   pr.GetPaths(),
							"service": extractResourceName(pr.GetService()),
						})
					}
					pathMatcher["path_rules"] = pathRules
				}

				pathMatchers = append(pathMatchers, pathMatcher)
			}
			res.Config["path_matchers"] = pathMatchers
		}

		// Tests
		if urlMap.GetTests() != nil {
			res.Config["tests_count"] = len(urlMap.GetTests())
		}

		infra.AddResource(res)
	}

	// Scan regional URL maps
	regionalClient, err := compute.NewRegionUrlMapsRESTClient(ctx, p.clientOpts...)
	if err != nil {
		// Non-fatal, just skip regional
		return nil
	}
	defer regionalClient.Close()

	for _, region := range regions {
		regionalReq := &computepb.ListRegionUrlMapsRequest{
			Project: p.project,
			Region:  region,
		}

		regionalIt := regionalClient.List(ctx, regionalReq)
		for {
			urlMap, err := regionalIt.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				continue
			}

			res := resource.NewAWSResource(
				urlMap.GetName(),
				urlMap.GetName(),
				resource.TypeCloudLB,
			)
			res.Region = region
			res.ARN = urlMap.GetSelfLink()

			// Config - same structure as global
			res.Config["name"] = urlMap.GetName()
			res.Config["description"] = urlMap.GetDescription()
			res.Config["default_service"] = extractResourceName(urlMap.GetDefaultService())
			res.Config["scope"] = "regional"

			if urlMap.GetHostRules() != nil {
				var hostRules []map[string]interface{}
				for _, hr := range urlMap.GetHostRules() {
					hostRules = append(hostRules, map[string]interface{}{
						"hosts":        hr.GetHosts(),
						"path_matcher": hr.GetPathMatcher(),
					})
				}
				res.Config["host_rules"] = hostRules
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanFirestore discovers Firestore databases.
func (p *APIParser) scanFirestore(ctx context.Context, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeFirestore, opts) {
		return nil
	}

	client, err := firestoreadmin.NewFirestoreAdminClient(ctx, p.clientOpts...)
	if err != nil {
		return fmt.Errorf("failed to create Firestore admin client: %w", err)
	}
	defer client.Close()

	req := &adminpb.ListDatabasesRequest{
		Parent: fmt.Sprintf("projects/%s", p.project),
	}

	resp, err := client.ListDatabases(ctx, req)
	if err != nil {
		// Firestore might not be enabled
		return nil
	}

	for _, db := range resp.Databases {
		// Extract database name from full path
		dbName := extractResourceName(db.Name)
		if dbName == "" {
			dbName = "(default)"
		}

		res := resource.NewAWSResource(
			dbName,
			dbName,
			resource.TypeFirestore,
		)
		res.Region = db.LocationId
		res.ARN = db.Name

		// Config
		res.Config["name"] = dbName
		res.Config["location_id"] = db.LocationId
		res.Config["type"] = db.Type.String()
		res.Config["concurrency_mode"] = db.ConcurrencyMode.String()
		res.Config["app_engine_integration_mode"] = db.AppEngineIntegrationMode.String()
		res.Config["uid"] = db.Uid
		res.Config["etag"] = db.Etag

		if db.KeyPrefix != "" {
			res.Config["key_prefix"] = db.KeyPrefix
		}

		infra.AddResource(res)
	}

	return nil
}

// scanBigtable discovers Bigtable instances.
func (p *APIParser) scanBigtable(ctx context.Context, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeBigtable, opts) {
		return nil
	}

	client, err := bigtable.NewInstanceAdminClient(ctx, p.project, p.clientOpts...)
	if err != nil {
		return fmt.Errorf("failed to create Bigtable admin client: %w", err)
	}
	defer client.Close()

	instances, err := client.Instances(ctx)
	if err != nil {
		// Bigtable might not be enabled
		return nil
	}

	for _, instance := range instances {
		res := resource.NewAWSResource(
			instance.Name,
			instance.DisplayName,
			resource.TypeBigtable,
		)
		res.Region = "global"
		res.ARN = fmt.Sprintf("projects/%s/instances/%s", p.project, instance.Name)

		// Config
		res.Config["name"] = instance.Name
		res.Config["display_name"] = instance.DisplayName
		res.Config["instance_type"] = int(instance.InstanceType)
		res.Config["instance_state"] = int(instance.InstanceState)

		// Labels
		for k, v := range instance.Labels {
			res.Tags[k] = v
		}

		// Get cluster info
		clusters, err := client.Clusters(ctx, instance.Name)
		if err == nil {
			var clusterInfos []map[string]interface{}
			for _, cluster := range clusters {
				clusterInfo := map[string]interface{}{
					"name":         cluster.Name,
					"zone":         cluster.Zone,
					"serve_nodes":  cluster.ServeNodes,
					"state":        cluster.State,
					"storage_type": int(cluster.StorageType),
				}

				// Autoscaling config if present
				if cluster.AutoscalingConfig != nil {
					clusterInfo["autoscaling_min_nodes"] = cluster.AutoscalingConfig.MinNodes
					clusterInfo["autoscaling_max_nodes"] = cluster.AutoscalingConfig.MaxNodes
					clusterInfo["autoscaling_cpu_target"] = cluster.AutoscalingConfig.CPUTargetPercent
				}

				clusterInfos = append(clusterInfos, clusterInfo)
			}
			res.Config["clusters"] = clusterInfos
		}

		infra.AddResource(res)
	}

	return nil
}

// scanSpanner discovers Spanner instances.
func (p *APIParser) scanSpanner(ctx context.Context, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeSpanner, opts) {
		return nil
	}

	client, err := spanner.NewInstanceAdminClient(ctx, p.clientOpts...)
	if err != nil {
		return fmt.Errorf("failed to create Spanner admin client: %w", err)
	}
	defer client.Close()

	req := &instancepb.ListInstancesRequest{
		Parent: fmt.Sprintf("projects/%s", p.project),
	}

	it := client.ListInstances(ctx, req)
	for {
		instance, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			// Spanner might not be enabled
			return nil
		}

		instanceName := extractResourceName(instance.Name)
		res := resource.NewAWSResource(
			instanceName,
			instance.DisplayName,
			resource.TypeSpanner,
		)
		res.Region = instance.Config
		res.ARN = instance.Name

		// Config
		res.Config["name"] = instanceName
		res.Config["display_name"] = instance.DisplayName
		res.Config["config"] = instance.Config
		res.Config["node_count"] = instance.NodeCount
		res.Config["processing_units"] = instance.ProcessingUnits
		res.Config["state"] = instance.State.String()
		res.Config["endpoint_uris"] = instance.EndpointUris

		// Labels
		for k, v := range instance.Labels {
			res.Tags[k] = v
		}

		infra.AddResource(res)
	}

	return nil
}

// scanCloudDNS discovers Cloud DNS managed zones.
func (p *APIParser) scanCloudDNS(ctx context.Context, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeCloudDNS, opts) {
		return nil
	}

	// Report progress
	if opts != nil && opts.OnProgress != nil {
		opts.OnProgress(parser.ProgressEvent{
			Step:    "scanning",
			Message: "Scanning Cloud DNS managed zones...",
			Service: "Cloud DNS",
		})
	}

	// Create the DNS service
	service, err := dns.NewService(ctx, p.clientOpts...)
	if err != nil {
		return fmt.Errorf("failed to create Cloud DNS service: %w", err)
	}

	// List managed zones with pagination
	pageToken := ""
	for {
		call := service.ManagedZones.List(p.project)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		resp, err := call.Context(ctx).Do()
		if err != nil {
			return fmt.Errorf("failed to list Cloud DNS managed zones: %w", err)
		}

		for _, zone := range resp.ManagedZones {
			res := resource.NewAWSResource(
				zone.Name,
				zone.Name,
				resource.TypeCloudDNS,
			)
			res.ARN = fmt.Sprintf("projects/%s/managedZones/%s", p.project, zone.Name)

			// Config
			res.Config["dns_name"] = zone.DnsName
			res.Config["description"] = zone.Description
			res.Config["visibility"] = zone.Visibility
			res.Config["name_servers"] = zone.NameServers

			// DNSSEC configuration
			if zone.DnssecConfig != nil {
				res.Config["dnssec_state"] = zone.DnssecConfig.State
				res.Config["dnssec_kind"] = zone.DnssecConfig.Kind
			}

			// Private visibility config (for private zones)
			if zone.PrivateVisibilityConfig != nil {
				var networks []string
				for _, network := range zone.PrivateVisibilityConfig.Networks {
					networks = append(networks, extractResourceName(network.NetworkUrl))
				}
				res.Config["private_visibility_networks"] = networks
			}

			// Forwarding config (if present)
			if zone.ForwardingConfig != nil {
				var targets []string
				for _, target := range zone.ForwardingConfig.TargetNameServers {
					targets = append(targets, target.Ipv4Address)
				}
				res.Config["forwarding_targets"] = targets
			}

			// Peering config (if present)
			if zone.PeeringConfig != nil && zone.PeeringConfig.TargetNetwork != nil {
				res.Config["peering_network"] = extractResourceName(zone.PeeringConfig.TargetNetwork.NetworkUrl)
			}

			// Labels
			for k, v := range zone.Labels {
				res.Tags[k] = v
			}

			// Report progress for each zone
			if opts != nil && opts.OnProgress != nil {
				opts.OnProgress(parser.ProgressEvent{
					Step:           "scanning",
					Message:        fmt.Sprintf("Found Cloud DNS zone: %s (%s)", zone.Name, zone.DnsName),
					Service:        "Cloud DNS",
					ResourcesFound: len(infra.Resources) + 1,
				})
			}

			infra.AddResource(res)
		}

		// Check for more pages
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	return nil
}

// scanPubSub discovers Pub/Sub topics and subscriptions.
func (p *APIParser) scanPubSub(ctx context.Context, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypePubSubTopic, opts) {
		return nil
	}

	client, err := pubsub.NewClient(ctx, p.project, p.clientOpts...)
	if err != nil {
		return fmt.Errorf("failed to create Pub/Sub client: %w", err)
	}
	defer client.Close()

	// List topics
	topicIt := client.Topics(ctx)
	for {
		topic, err := topicIt.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			continue
		}

		topicName := topic.ID()
		res := resource.NewAWSResource(
			topicName,
			topicName,
			resource.TypePubSubTopic,
		)
		res.Region = "global"
		res.ARN = fmt.Sprintf("projects/%s/topics/%s", p.project, topicName)

		// Get topic config
		config, err := topic.Config(ctx)
		if err == nil {
			res.Config["name"] = topicName
			res.Config["message_storage_policy_regions"] = config.MessageStoragePolicy.AllowedPersistenceRegions
			res.Config["kms_key_name"] = config.KMSKeyName
			if config.RetentionDuration != nil {
				if dur, ok := config.RetentionDuration.(time.Duration); ok {
					res.Config["message_retention_duration"] = dur.String()
				}
			}

			// Labels
			for k, v := range config.Labels {
				res.Tags[k] = v
			}

			// Schema settings
			if config.SchemaSettings != nil {
				res.Config["schema"] = config.SchemaSettings.Schema
				res.Config["schema_encoding"] = int(config.SchemaSettings.Encoding)
			}
		}

		// List subscriptions for this topic
		var subscriptions []map[string]interface{}
		subIt := topic.Subscriptions(ctx)
		for {
			sub, err := subIt.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				continue
			}

			subConfig, err := sub.Config(ctx)
			if err != nil {
				continue
			}

			subInfo := map[string]interface{}{
				"name":                       sub.ID(),
				"ack_deadline_seconds":       subConfig.AckDeadline.Seconds(),
				"retain_acked_messages":      subConfig.RetainAckedMessages,
				"message_retention_duration": subConfig.RetentionDuration.String(),
				"enable_message_ordering":    subConfig.EnableMessageOrdering,
			}

			if subConfig.PushConfig.Endpoint != "" {
				subInfo["push_endpoint"] = subConfig.PushConfig.Endpoint
			}

			if subConfig.DeadLetterPolicy != nil {
				subInfo["dead_letter_topic"] = subConfig.DeadLetterPolicy.DeadLetterTopic
				subInfo["max_delivery_attempts"] = subConfig.DeadLetterPolicy.MaxDeliveryAttempts
			}

			subscriptions = append(subscriptions, subInfo)
		}
		res.Config["subscriptions"] = subscriptions

		infra.AddResource(res)
	}

	return nil
}

// scanCloudTasks discovers Cloud Tasks queues.
func (p *APIParser) scanCloudTasks(ctx context.Context, infra *resource.Infrastructure, regions []string, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeCloudTasks, opts) {
		return nil
	}

	client, err := cloudtasks.NewClient(ctx, p.clientOpts...)
	if err != nil {
		return fmt.Errorf("failed to create Cloud Tasks client: %w", err)
	}
	defer client.Close()

	for _, region := range regions {
		req := &cloudtaskspb.ListQueuesRequest{
			Parent: fmt.Sprintf("projects/%s/locations/%s", p.project, region),
		}

		it := client.ListQueues(ctx, req)
		for {
			queue, err := it.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				continue
			}

			// Extract queue name from full path
			queueName := extractResourceName(queue.Name)

			res := resource.NewAWSResource(
				queueName,
				queueName,
				resource.TypeCloudTasks,
			)
			res.Region = region
			res.ARN = queue.Name

			// Config
			res.Config["name"] = queueName
			res.Config["state"] = queue.State.String()

			// Rate limits
			if queue.RateLimits != nil {
				res.Config["rate_limits"] = map[string]interface{}{
					"max_dispatches_per_second": queue.RateLimits.MaxDispatchesPerSecond,
					"max_burst_size":            queue.RateLimits.MaxBurstSize,
					"max_concurrent_dispatches": queue.RateLimits.MaxConcurrentDispatches,
				}
			}

			// Retry config
			if queue.RetryConfig != nil {
				retryConfig := map[string]interface{}{
					"max_attempts": queue.RetryConfig.MaxAttempts,
				}
				if queue.RetryConfig.MaxRetryDuration != nil {
					retryConfig["max_retry_duration"] = queue.RetryConfig.MaxRetryDuration.AsDuration().String()
				}
				if queue.RetryConfig.MinBackoff != nil {
					retryConfig["min_backoff"] = queue.RetryConfig.MinBackoff.AsDuration().String()
				}
				if queue.RetryConfig.MaxBackoff != nil {
					retryConfig["max_backoff"] = queue.RetryConfig.MaxBackoff.AsDuration().String()
				}
				res.Config["retry_config"] = retryConfig
			}

			// Stackdriver logging config
			if queue.StackdriverLoggingConfig != nil {
				res.Config["logging_ratio"] = queue.StackdriverLoggingConfig.SamplingRatio
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanIdentityPlatform discovers Identity Platform configuration.
func (p *APIParser) scanIdentityPlatform(ctx context.Context, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeIdentityPlatform, opts) {
		return nil
	}

	// Create Identity Toolkit service
	service, err := identitytoolkit.NewService(ctx, p.clientOpts...)
	if err != nil {
		// Identity Platform might not be enabled
		return nil
	}

	// Get project config
	configName := fmt.Sprintf("projects/%s/config", p.project)
	config, err := service.Projects.GetConfig(configName).Context(ctx).Do()
	if err != nil {
		// Identity Platform might not be enabled
		return nil
	}

	res := resource.NewAWSResource(
		p.project,
		fmt.Sprintf("%s-identity-platform", p.project),
		resource.TypeIdentityPlatform,
	)
	res.Region = "global"
	res.ARN = configName

	// Config
	res.Config["name"] = configName

	// Authorized domains
	if config.AuthorizedDomains != nil {
		res.Config["authorized_domains"] = config.AuthorizedDomains
	}

	// Sign-in config
	if config.SignIn != nil {
		signInConfig := map[string]interface{}{
			"allow_duplicate_emails": config.SignIn.AllowDuplicateEmails,
		}

		// Email sign-in
		if config.SignIn.Email != nil {
			signInConfig["email_enabled"] = config.SignIn.Email.Enabled
			signInConfig["email_password_required"] = config.SignIn.Email.PasswordRequired
		}

		// Phone sign-in
		if config.SignIn.PhoneNumber != nil {
			signInConfig["phone_enabled"] = config.SignIn.PhoneNumber.Enabled
		}

		// Anonymous sign-in
		if config.SignIn.Anonymous != nil {
			signInConfig["anonymous_enabled"] = config.SignIn.Anonymous.Enabled
		}

		res.Config["sign_in_config"] = signInConfig
	}

	// MFA config
	if config.Mfa != nil {
		mfaConfig := map[string]interface{}{
			"state": config.Mfa.State,
		}
		if config.Mfa.ProviderConfigs != nil {
			var providers []string
			for _, pc := range config.Mfa.ProviderConfigs {
				providers = append(providers, pc.State)
			}
			mfaConfig["providers"] = providers
		}
		res.Config["mfa_config"] = mfaConfig
	}

	// Notification config
	if config.Notification != nil && config.Notification.SendEmail != nil {
		res.Config["send_email_method"] = config.Notification.SendEmail.Method
	}

	// Quota config
	if config.Quota != nil && config.Quota.SignUpQuotaConfig != nil {
		res.Config["sign_up_quota_duration"] = config.Quota.SignUpQuotaConfig.QuotaDuration
		res.Config["sign_up_quota"] = config.Quota.SignUpQuotaConfig.Quota
	}

	// Blocking functions
	if config.BlockingFunctions != nil {
		var triggers []string
		for trigger := range config.BlockingFunctions.Triggers {
			triggers = append(triggers, trigger)
		}
		res.Config["blocking_function_triggers"] = triggers
	}

	infra.AddResource(res)

	return nil
}

// scanSecretManager discovers Secret Manager secrets (metadata only).
func (p *APIParser) scanSecretManager(ctx context.Context, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeSecretManager, opts) {
		return nil
	}

	client, err := secretmanager.NewClient(ctx, p.clientOpts...)
	if err != nil {
		return fmt.Errorf("failed to create Secret Manager client: %w", err)
	}
	defer client.Close()

	req := &secretmanagerpb.ListSecretsRequest{
		Parent: fmt.Sprintf("projects/%s", p.project),
	}

	it := client.ListSecrets(ctx, req)
	for {
		secret, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			// Secret Manager might not be enabled
			return nil
		}

		secretName := extractResourceName(secret.Name)

		res := resource.NewAWSResource(
			secretName,
			secretName,
			resource.TypeSecretManager,
		)
		res.Region = "global"
		res.ARN = secret.Name

		// Config - NEVER include secret values
		res.Config["name"] = secretName
		res.Config["create_time"] = secret.CreateTime.AsTime()
		res.Config["etag"] = secret.Etag

		// Replication config
		if secret.Replication != nil {
			switch r := secret.Replication.Replication.(type) {
			case *secretmanagerpb.Replication_Automatic_:
				res.Config["replication_type"] = "automatic"
			case *secretmanagerpb.Replication_UserManaged_:
				res.Config["replication_type"] = "user_managed"
				if r.UserManaged != nil {
					var replicas []string
					for _, replica := range r.UserManaged.Replicas {
						replicas = append(replicas, replica.Location)
					}
					res.Config["replication_locations"] = replicas
				}
			}
		}

		// Rotation config
		if secret.Rotation != nil {
			rotationConfig := map[string]interface{}{}
			if secret.Rotation.NextRotationTime != nil {
				rotationConfig["next_rotation_time"] = secret.Rotation.NextRotationTime.AsTime()
			}
			if secret.Rotation.RotationPeriod != nil {
				rotationConfig["rotation_period"] = secret.Rotation.RotationPeriod.AsDuration().String()
			}
			res.Config["rotation_config"] = rotationConfig
		}

		// Expiration
		if exp, ok := secret.Expiration.(*secretmanagerpb.Secret_ExpireTime); ok {
			res.Config["expire_time"] = exp.ExpireTime.AsTime()
		}
		if ttl, ok := secret.Expiration.(*secretmanagerpb.Secret_Ttl); ok {
			res.Config["ttl"] = ttl.Ttl.AsDuration().String()
		}

		// Version aliases (not the versions themselves)
		if secret.VersionAliases != nil {
			res.Config["version_aliases"] = secret.VersionAliases
		}

		// Labels
		for k, v := range secret.Labels {
			res.Tags[k] = v
		}

		// Topics for notifications
		if secret.Topics != nil {
			var topics []string
			for _, topic := range secret.Topics {
				topics = append(topics, topic.Name)
			}
			res.Config["notification_topics"] = topics
		}

		infra.AddResource(res)
	}

	return nil
}

// scanPersistentDisk discovers GCE Persistent Disks.
func (p *APIParser) scanPersistentDisk(ctx context.Context, infra *resource.Infrastructure, regions []string, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypePersistentDisk, opts) {
		return nil
	}

	client, err := compute.NewDisksRESTClient(ctx, p.clientOpts...)
	if err != nil {
		return fmt.Errorf("failed to create disks client: %w", err)
	}
	defer client.Close()

	// Use aggregated list to get disks across all zones
	req := &computepb.AggregatedListDisksRequest{
		Project: p.project,
	}

	it := client.AggregatedList(ctx, req)
	for {
		resp, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			continue
		}

		// resp is a map entry: key is zone/region, value is DisksScopedList
		for _, disk := range resp.Value.Disks {
			diskName := disk.GetName()

			res := resource.NewAWSResource(
				diskName,
				diskName,
				resource.TypePersistentDisk,
			)

			// Extract zone from the zone URL
			zone := extractResourceName(disk.GetZone())
			res.Region = zone
			res.ARN = disk.GetSelfLink()

			// Config
			res.Config["name"] = diskName
			res.Config["size_gb"] = disk.GetSizeGb()
			res.Config["type"] = extractResourceName(disk.GetType())
			res.Config["zone"] = zone
			res.Config["status"] = disk.GetStatus()
			res.Config["physical_block_size_bytes"] = disk.GetPhysicalBlockSizeBytes()

			// Source image/snapshot
			if disk.GetSourceImage() != "" {
				res.Config["source_image"] = extractResourceName(disk.GetSourceImage())
			}
			if disk.GetSourceSnapshot() != "" {
				res.Config["source_snapshot"] = extractResourceName(disk.GetSourceSnapshot())
			}

			// Encryption
			if disk.GetDiskEncryptionKey() != nil {
				res.Config["encryption_key_type"] = "customer_managed"
				res.Config["kms_key_name"] = disk.GetDiskEncryptionKey().GetKmsKeyName()
			}

			// Users (attached instances)
			if disk.GetUsers() != nil {
				var users []string
				for _, user := range disk.GetUsers() {
					users = append(users, extractResourceName(user))
				}
				res.Config["attached_instances"] = users
			}

			// Replication
			if disk.GetReplicaZones() != nil {
				var replicaZones []string
				for _, rz := range disk.GetReplicaZones() {
					replicaZones = append(replicaZones, extractResourceName(rz))
				}
				res.Config["replica_zones"] = replicaZones
			}

			// Provisioned IOPS and throughput
			if disk.GetProvisionedIops() > 0 {
				res.Config["provisioned_iops"] = disk.GetProvisionedIops()
			}
			if disk.GetProvisionedThroughput() > 0 {
				res.Config["provisioned_throughput"] = disk.GetProvisionedThroughput()
			}

			// Labels
			for k, v := range disk.GetLabels() {
				res.Tags[k] = v
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanFilestore discovers Filestore instances.
func (p *APIParser) scanFilestore(ctx context.Context, infra *resource.Infrastructure, regions []string, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeFilestore, opts) {
		return nil
	}

	client, err := filestore.NewCloudFilestoreManagerClient(ctx, p.clientOpts...)
	if err != nil {
		return fmt.Errorf("failed to create Filestore client: %w", err)
	}
	defer client.Close()

	for _, region := range regions {
		// List instances in this location (region includes all zones)
		req := &filestorepb.ListInstancesRequest{
			Parent: fmt.Sprintf("projects/%s/locations/%s", p.project, region),
		}

		it := client.ListInstances(ctx, req)
		for {
			instance, err := it.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				// Try with "-" for all locations if region-specific fails
				break
			}

			instanceName := extractResourceName(instance.Name)

			res := resource.NewAWSResource(
				instanceName,
				instanceName,
				resource.TypeFilestore,
			)
			res.Region = region
			res.ARN = instance.Name

			// Config
			res.Config["name"] = instanceName
			res.Config["tier"] = instance.Tier.String()
			res.Config["state"] = instance.State.String()
			res.Config["status_message"] = instance.StatusMessage
			res.Config["create_time"] = instance.CreateTime.AsTime()
			res.Config["description"] = instance.Description

			// File shares
			if instance.FileShares != nil {
				var fileShares []map[string]interface{}
				for _, fs := range instance.FileShares {
					share := map[string]interface{}{
						"name":        fs.Name,
						"capacity_gb": fs.CapacityGb,
					}
					if fs.GetSourceBackup() != "" {
						share["source_backup"] = fs.GetSourceBackup()
					}

					// NFS export options
					if fs.NfsExportOptions != nil {
						var exportOptions []map[string]interface{}
						for _, opt := range fs.NfsExportOptions {
							exportOptions = append(exportOptions, map[string]interface{}{
								"ip_ranges":   opt.IpRanges,
								"access_mode": opt.AccessMode.String(),
								"squash_mode": opt.SquashMode.String(),
							})
						}
						share["nfs_export_options"] = exportOptions
					}

					fileShares = append(fileShares, share)
				}
				res.Config["file_shares"] = fileShares
			}

			// Networks
			if instance.Networks != nil {
				var networks []map[string]interface{}
				for _, net := range instance.Networks {
					networks = append(networks, map[string]interface{}{
						"network":           net.Network,
						"modes":             net.Modes,
						"reserved_ip_range": net.ReservedIpRange,
						"ip_addresses":      net.IpAddresses,
						"connect_mode":      net.ConnectMode.String(),
					})
				}
				res.Config["networks"] = networks
			}

			// Suspension reasons
			if instance.SuspensionReasons != nil {
				var reasons []string
				for _, r := range instance.SuspensionReasons {
					reasons = append(reasons, r.String())
				}
				res.Config["suspension_reasons"] = reasons
			}

			// KMS key
			if instance.KmsKeyName != "" {
				res.Config["kms_key_name"] = instance.KmsKeyName
			}

			// Labels
			for k, v := range instance.Labels {
				res.Tags[k] = v
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanCloudFunctions discovers Cloud Functions.
func (p *APIParser) scanCloudFunctions(ctx context.Context, infra *resource.Infrastructure, regions []string, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeCloudFunction, opts) {
		return nil
	}

	client, err := functions.NewFunctionClient(ctx, p.clientOpts...)
	if err != nil {
		return fmt.Errorf("failed to create Cloud Functions client: %w", err)
	}
	defer client.Close()

	for _, region := range regions {
		req := &functionspb.ListFunctionsRequest{
			Parent: fmt.Sprintf("projects/%s/locations/%s", p.project, region),
		}

		it := client.ListFunctions(ctx, req)
		for {
			fn, err := it.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				// Region might not support Cloud Functions
				break
			}

			fnName := extractResourceName(fn.Name)

			res := resource.NewAWSResource(
				fnName,
				fnName,
				resource.TypeCloudFunction,
			)
			res.Region = region
			res.ARN = fn.Name

			// Config
			res.Config["name"] = fnName
			res.Config["state"] = fn.State.String()
			res.Config["environment"] = fn.Environment.String()
			res.Config["description"] = fn.Description
			res.Config["update_time"] = fn.UpdateTime.AsTime()
			res.Config["url"] = fn.Url

			// Build config
			if fn.BuildConfig != nil {
				buildConfig := map[string]interface{}{
					"runtime":           fn.BuildConfig.Runtime,
					"entry_point":       fn.BuildConfig.EntryPoint,
					"docker_repository": fn.BuildConfig.DockerRepository,
				}

				if fn.BuildConfig.Source != nil {
					switch src := fn.BuildConfig.Source.Source.(type) {
					case *functionspb.Source_StorageSource:
						buildConfig["source_type"] = "storage"
						buildConfig["source_bucket"] = src.StorageSource.Bucket
						buildConfig["source_object"] = src.StorageSource.Object
					case *functionspb.Source_RepoSource:
						buildConfig["source_type"] = "repo"
						buildConfig["source_repo"] = src.RepoSource.ProjectId
						buildConfig["source_branch"] = src.RepoSource.GetBranchName()
					case *functionspb.Source_GitUri:
						buildConfig["source_type"] = "git"
						buildConfig["source_git_uri"] = src.GitUri
					}
				}

				// Environment variables (keys only for security)
				if fn.BuildConfig.EnvironmentVariables != nil {
					var envKeys []string
					for k := range fn.BuildConfig.EnvironmentVariables {
						envKeys = append(envKeys, k)
					}
					buildConfig["environment_variable_keys"] = envKeys
				}

				res.Config["build_config"] = buildConfig
			}

			// Service config
			if fn.ServiceConfig != nil {
				serviceConfig := map[string]interface{}{
					"service":                           fn.ServiceConfig.Service,
					"timeout_seconds":                   fn.ServiceConfig.TimeoutSeconds,
					"available_memory":                  fn.ServiceConfig.AvailableMemory,
					"available_cpu":                     fn.ServiceConfig.AvailableCpu,
					"max_instance_count":                fn.ServiceConfig.MaxInstanceCount,
					"min_instance_count":                fn.ServiceConfig.MinInstanceCount,
					"max_instance_request_concurrency":  fn.ServiceConfig.MaxInstanceRequestConcurrency,
					"ingress_settings":                  fn.ServiceConfig.IngressSettings.String(),
					"all_traffic_on_latest_revision":    fn.ServiceConfig.AllTrafficOnLatestRevision,
					"service_account_email":             fn.ServiceConfig.ServiceAccountEmail,
				}

				if fn.ServiceConfig.VpcConnector != "" {
					serviceConfig["vpc_connector"] = fn.ServiceConfig.VpcConnector
					serviceConfig["vpc_connector_egress_settings"] = fn.ServiceConfig.VpcConnectorEgressSettings.String()
				}

				// Secret environment variables (names only)
				if fn.ServiceConfig.SecretEnvironmentVariables != nil {
					var secretNames []string
					for _, sev := range fn.ServiceConfig.SecretEnvironmentVariables {
						secretNames = append(secretNames, sev.Key)
					}
					serviceConfig["secret_environment_variable_keys"] = secretNames
				}

				// Secret volumes (names only)
				if fn.ServiceConfig.SecretVolumes != nil {
					var secretVolumes []string
					for _, sv := range fn.ServiceConfig.SecretVolumes {
						secretVolumes = append(secretVolumes, sv.MountPath)
					}
					serviceConfig["secret_volume_mount_paths"] = secretVolumes
				}

				res.Config["service_config"] = serviceConfig
			}

			// Event trigger
			if fn.EventTrigger != nil {
				eventTrigger := map[string]interface{}{
					"trigger":               fn.EventTrigger.Trigger,
					"trigger_region":        fn.EventTrigger.TriggerRegion,
					"event_type":            fn.EventTrigger.EventType,
					"retry_policy":          fn.EventTrigger.RetryPolicy.String(),
					"service_account_email": fn.EventTrigger.ServiceAccountEmail,
					"channel":               fn.EventTrigger.Channel,
				}

				if fn.EventTrigger.PubsubTopic != "" {
					eventTrigger["pubsub_topic"] = fn.EventTrigger.PubsubTopic
				}

				if fn.EventTrigger.EventFilters != nil {
					var filters []map[string]string
					for _, ef := range fn.EventTrigger.EventFilters {
						filters = append(filters, map[string]string{
							"attribute": ef.Attribute,
							"value":     ef.Value,
							"operator":  ef.Operator,
						})
					}
					eventTrigger["event_filters"] = filters
				}

				res.Config["event_trigger"] = eventTrigger
			}

			// Labels
			for k, v := range fn.Labels {
				res.Tags[k] = v
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanCloudArmor discovers Cloud Armor security policies.
func (p *APIParser) scanCloudArmor(ctx context.Context, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeCloudArmor, opts) {
		return nil
	}

	client, err := compute.NewSecurityPoliciesRESTClient(ctx, p.clientOpts...)
	if err != nil {
		return fmt.Errorf("failed to create security policies client: %w", err)
	}
	defer client.Close()

	// List all security policies (global)
	req := &computepb.ListSecurityPoliciesRequest{
		Project: p.project,
	}

	it := client.List(ctx, req)
	for {
		policy, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			continue
		}

		res := resource.NewAWSResource(
			policy.GetName(),
			policy.GetName(),
			resource.TypeCloudArmor,
		)
		res.Region = "global"
		res.ARN = policy.GetSelfLink()

		// Config
		res.Config["name"] = policy.GetName()
		res.Config["description"] = policy.GetDescription()
		res.Config["type"] = policy.GetType()
		res.Config["fingerprint"] = policy.GetFingerprint()

		// Rules count
		if policy.GetRules() != nil {
			res.Config["rules_count"] = len(policy.GetRules())

			// Extract rule summaries (not full rule details for brevity)
			var ruleSummaries []map[string]interface{}
			for _, rule := range policy.GetRules() {
				ruleSummary := map[string]interface{}{
					"priority":    rule.GetPriority(),
					"action":      rule.GetAction(),
					"description": rule.GetDescription(),
					"preview":     rule.GetPreview(),
				}

				// Match expression
				if rule.GetMatch() != nil {
					match := rule.GetMatch()
					if match.GetExpr() != nil {
						ruleSummary["match_expression"] = match.GetExpr().GetExpression()
					}
					if match.GetVersionedExpr() != "" {
						ruleSummary["versioned_expr"] = match.GetVersionedExpr()
					}
					if match.GetConfig() != nil {
						ruleSummary["src_ip_ranges"] = match.GetConfig().GetSrcIpRanges()
					}
				}

				// Rate limit options
				if rule.GetRateLimitOptions() != nil {
					rateLimit := rule.GetRateLimitOptions()
					ruleSummary["rate_limit_threshold_count"] = rateLimit.GetRateLimitThreshold().GetCount()
					ruleSummary["rate_limit_threshold_interval_sec"] = rateLimit.GetRateLimitThreshold().GetIntervalSec()
					ruleSummary["conform_action"] = rateLimit.GetConformAction()
					ruleSummary["exceed_action"] = rateLimit.GetExceedAction()
					ruleSummary["enforce_on_key"] = rateLimit.GetEnforceOnKey()
				}

				ruleSummaries = append(ruleSummaries, ruleSummary)
			}
			res.Config["rules"] = ruleSummaries
		}

		// Adaptive protection config
		if policy.GetAdaptiveProtectionConfig() != nil {
			apc := policy.GetAdaptiveProtectionConfig()
			adaptiveConfig := map[string]interface{}{}

			if apc.GetLayer7DdosDefenseConfig() != nil {
				l7Config := apc.GetLayer7DdosDefenseConfig()
				adaptiveConfig["layer7_ddos_defense_enable"] = l7Config.GetEnable()
				adaptiveConfig["layer7_ddos_defense_rule_visibility"] = l7Config.GetRuleVisibility()
			}

			res.Config["adaptive_protection_config"] = adaptiveConfig
		}

		// Advanced options config
		if policy.GetAdvancedOptionsConfig() != nil {
			aoc := policy.GetAdvancedOptionsConfig()
			advancedConfig := map[string]interface{}{
				"json_parsing": aoc.GetJsonParsing(),
				"log_level":    aoc.GetLogLevel(),
			}

			if aoc.GetJsonCustomConfig() != nil {
				advancedConfig["json_custom_content_types"] = aoc.GetJsonCustomConfig().GetContentTypes()
			}

			res.Config["advanced_options_config"] = advancedConfig
		}

		// DDoS protection config
		if policy.GetDdosProtectionConfig() != nil {
			ddosConfig := map[string]interface{}{
				"ddos_protection": policy.GetDdosProtectionConfig().GetDdosProtection(),
			}
			res.Config["ddos_protection_config"] = ddosConfig
		}

		// Recaptcha options config
		if policy.GetRecaptchaOptionsConfig() != nil {
			recaptchaConfig := map[string]interface{}{
				"redirect_site_key": policy.GetRecaptchaOptionsConfig().GetRedirectSiteKey(),
			}
			res.Config["recaptcha_options_config"] = recaptchaConfig
		}

		// Labels
		for k, v := range policy.GetLabels() {
			res.Tags[k] = v
		}

		infra.AddResource(res)
	}

	return nil
}

// scanGCPIAM discovers GCP IAM policy bindings.
func (p *APIParser) scanGCPIAM(ctx context.Context, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeGCPIAM, opts) {
		return nil
	}

	// Create Cloud Resource Manager service
	service, err := cloudresourcemanager.NewService(ctx, p.clientOpts...)
	if err != nil {
		return fmt.Errorf("failed to create Cloud Resource Manager service: %w", err)
	}

	// Get IAM policy for the project
	policy, err := service.Projects.GetIamPolicy(p.project, &cloudresourcemanager.GetIamPolicyRequest{}).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("failed to get IAM policy: %w", err)
	}

	// Create one resource per role+member combination
	for _, binding := range policy.Bindings {
		role := binding.Role

		for _, member := range binding.Members {
			// Create a unique ID for this binding
			// Use a hash-like approach for the ID to avoid special characters
			bindingID := fmt.Sprintf("%s/%s", strings.ReplaceAll(role, "/", "_"), strings.ReplaceAll(member, ":", "_"))

			res := resource.NewAWSResource(
				bindingID,
				fmt.Sprintf("%s -> %s", role, member),
				resource.TypeGCPIAM,
			)
			res.Region = "global"
			res.ARN = fmt.Sprintf("projects/%s/iamPolicies/%s", p.project, bindingID)

			// Config
			res.Config["project"] = p.project
			res.Config["role"] = role
			res.Config["member"] = member

			// Parse member type (user, serviceAccount, group, domain, etc.)
			memberParts := strings.SplitN(member, ":", 2)
			if len(memberParts) == 2 {
				res.Config["member_type"] = memberParts[0]
				res.Config["member_id"] = memberParts[1]
			}

			// Condition (if present)
			if binding.Condition != nil {
				conditionConfig := map[string]interface{}{
					"title":       binding.Condition.Title,
					"description": binding.Condition.Description,
					"expression":  binding.Condition.Expression,
				}
				res.Config["condition"] = conditionConfig
			}

			// Extract role type (basic, predefined, custom)
			if strings.HasPrefix(role, "roles/") {
				res.Config["role_type"] = "predefined"
			} else if strings.HasPrefix(role, "organizations/") || strings.HasPrefix(role, "projects/") {
				res.Config["role_type"] = "custom"
			} else {
				res.Config["role_type"] = "basic"
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanGCPVPCNetwork discovers GCP VPC networks.
func (p *APIParser) scanGCPVPCNetwork(ctx context.Context, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeGCPVPCNetwork, opts) {
		return nil
	}

	client, err := compute.NewNetworksRESTClient(ctx, p.clientOpts...)
	if err != nil {
		return fmt.Errorf("failed to create networks client: %w", err)
	}
	defer client.Close()

	req := &computepb.ListNetworksRequest{
		Project: p.project,
	}

	it := client.List(ctx, req)
	for {
		network, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			continue
		}

		networkName := network.GetName()
		res := resource.NewAWSResource(
			networkName,
			networkName,
			resource.TypeGCPVPCNetwork,
		)
		res.Region = "global" // VPC networks are global in GCP
		res.ARN = network.GetSelfLink()

		// Config
		res.Config["name"] = networkName
		res.Config["description"] = network.GetDescription()
		res.Config["auto_create_subnetworks"] = network.GetAutoCreateSubnetworks()
		res.Config["mtu"] = network.GetMtu()
		res.Config["gateway_ipv4"] = network.GetGatewayIPv4()

		// Routing mode from routing config
		if network.GetRoutingConfig() != nil {
			res.Config["routing_mode"] = network.GetRoutingConfig().GetRoutingMode()
		}

		// Subnetworks (list of subnetwork names)
		if network.GetSubnetworks() != nil {
			var subnetworks []string
			for _, subnet := range network.GetSubnetworks() {
				subnetworks = append(subnetworks, extractResourceName(subnet))
			}
			res.Config["subnetworks"] = subnetworks
		}

		// Peerings (list of peering details)
		if network.GetPeerings() != nil {
			var peerings []map[string]interface{}
			for _, peering := range network.GetPeerings() {
				peeringInfo := map[string]interface{}{
					"name":                                peering.GetName(),
					"network":                             extractResourceName(peering.GetNetwork()),
					"state":                               peering.GetState(),
					"state_details":                       peering.GetStateDetails(),
					"auto_create_routes":                  peering.GetAutoCreateRoutes(),
					"export_custom_routes":                peering.GetExportCustomRoutes(),
					"import_custom_routes":                peering.GetImportCustomRoutes(),
					"exchange_subnet_routes":              peering.GetExchangeSubnetRoutes(),
					"export_subnet_routes_with_public_ip": peering.GetExportSubnetRoutesWithPublicIp(),
					"import_subnet_routes_with_public_ip": peering.GetImportSubnetRoutesWithPublicIp(),
				}
				peerings = append(peerings, peeringInfo)
			}
			res.Config["peerings"] = peerings
		}

		// Internal IPv6 range (if present)
		if network.GetInternalIpv6Range() != "" {
			res.Config["internal_ipv6_range"] = network.GetInternalIpv6Range()
		}

		// Network firewall policy enforcement order
		res.Config["network_firewall_policy_enforcement_order"] = network.GetNetworkFirewallPolicyEnforcementOrder()

		infra.AddResource(res)
	}

	return nil
}

// scanAppEngine discovers App Engine application.
func (p *APIParser) scanAppEngine(ctx context.Context, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeAppEngine, opts) {
		return nil
	}

	// Report progress
	if opts != nil && opts.OnProgress != nil {
		opts.OnProgress(parser.ProgressEvent{
			Step:    "scanning",
			Message: "Scanning App Engine application...",
			Service: "App Engine",
		})
	}

	// Create App Engine service
	service, err := appengine.NewService(ctx, p.clientOpts...)
	if err != nil {
		return fmt.Errorf("failed to create App Engine service: %w", err)
	}

	// Get the App Engine application
	app, err := service.Apps.Get(p.project).Context(ctx).Do()
	if err != nil {
		// App Engine might not be enabled for this project
		return nil
	}

	res := resource.NewAWSResource(
		app.Id,
		app.Id,
		resource.TypeAppEngine,
	)
	res.Region = app.LocationId
	res.ARN = app.Name

	// Config
	res.Config["id"] = app.Id
	res.Config["name"] = app.Name
	res.Config["location_id"] = app.LocationId
	res.Config["serving_status"] = app.ServingStatus
	res.Config["default_hostname"] = app.DefaultHostname
	res.Config["auth_domain"] = app.AuthDomain
	res.Config["code_bucket"] = app.CodeBucket
	res.Config["default_bucket"] = app.DefaultBucket
	res.Config["gcr_domain"] = app.GcrDomain

	// Feature settings
	if app.FeatureSettings != nil {
		featureSettings := map[string]interface{}{
			"split_health_checks":        app.FeatureSettings.SplitHealthChecks,
			"use_container_optimized_os": app.FeatureSettings.UseContainerOptimizedOs,
		}
		res.Config["feature_settings"] = featureSettings
	}

	// Database type
	if app.DatabaseType != "" {
		res.Config["database_type"] = app.DatabaseType
	}

	// IAP (Identity-Aware Proxy) configuration
	if app.Iap != nil {
		iapConfig := map[string]interface{}{
			"enabled": app.Iap.Enabled,
		}
		if app.Iap.Oauth2ClientId != "" {
			iapConfig["oauth2_client_id"] = app.Iap.Oauth2ClientId
		}
		res.Config["iap"] = iapConfig
	}

	// Dispatch rules
	if app.DispatchRules != nil {
		var dispatchRules []map[string]interface{}
		for _, rule := range app.DispatchRules {
			dispatchRules = append(dispatchRules, map[string]interface{}{
				"domain":  rule.Domain,
				"path":    rule.Path,
				"service": rule.Service,
			})
		}
		res.Config["dispatch_rules"] = dispatchRules
	}

	// Report progress
	if opts != nil && opts.OnProgress != nil {
		opts.OnProgress(parser.ProgressEvent{
			Step:           "scanning",
			Message:        fmt.Sprintf("Found App Engine application: %s in %s", app.Id, app.LocationId),
			Service:        "App Engine",
			ResourcesFound: len(infra.Resources) + 1,
		})
	}

	infra.AddResource(res)

	return nil
}

// scanCloudScheduler discovers Cloud Scheduler jobs.
func (p *APIParser) scanCloudScheduler(ctx context.Context, infra *resource.Infrastructure, regions []string, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeCloudScheduler, opts) {
		return nil
	}

	// Report progress
	if opts != nil && opts.OnProgress != nil {
		opts.OnProgress(parser.ProgressEvent{
			Step:    "scanning",
			Message: "Scanning Cloud Scheduler jobs...",
			Service: "Cloud Scheduler",
		})
	}

	client, err := scheduler.NewCloudSchedulerClient(ctx, p.clientOpts...)
	if err != nil {
		return fmt.Errorf("failed to create Cloud Scheduler client: %w", err)
	}
	defer client.Close()

	for _, region := range regions {
		req := &schedulerpb.ListJobsRequest{
			Parent: fmt.Sprintf("projects/%s/locations/%s", p.project, region),
		}

		it := client.ListJobs(ctx, req)
		for {
			job, err := it.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				// Region might not support Cloud Scheduler or no jobs exist
				break
			}

			// Extract job name from full path
			jobName := extractResourceName(job.Name)

			res := resource.NewAWSResource(
				jobName,
				jobName,
				resource.TypeCloudScheduler,
			)
			res.Region = region
			res.ARN = job.Name

			// Config
			res.Config["name"] = jobName
			res.Config["description"] = job.Description
			res.Config["schedule"] = job.Schedule
			res.Config["time_zone"] = job.TimeZone
			res.Config["state"] = job.State.String()

			// Timestamps
			if job.LastAttemptTime != nil {
				res.Config["last_attempt_time"] = job.LastAttemptTime.AsTime()
			}
			if job.ScheduleTime != nil {
				res.Config["schedule_time"] = job.ScheduleTime.AsTime()
			}
			if job.UserUpdateTime != nil {
				res.Config["user_update_time"] = job.UserUpdateTime.AsTime()
			}

			// Retry config
			if job.RetryConfig != nil {
				retryConfig := map[string]interface{}{
					"retry_count": job.RetryConfig.RetryCount,
				}
				if job.RetryConfig.MaxRetryDuration != nil {
					retryConfig["max_retry_duration"] = job.RetryConfig.MaxRetryDuration.AsDuration().String()
				}
				if job.RetryConfig.MinBackoffDuration != nil {
					retryConfig["min_backoff_duration"] = job.RetryConfig.MinBackoffDuration.AsDuration().String()
				}
				if job.RetryConfig.MaxBackoffDuration != nil {
					retryConfig["max_backoff_duration"] = job.RetryConfig.MaxBackoffDuration.AsDuration().String()
				}
				if job.RetryConfig.MaxDoublings != 0 {
					retryConfig["max_doublings"] = job.RetryConfig.MaxDoublings
				}
				res.Config["retry_config"] = retryConfig
			}

			// Attempt deadline
			if job.AttemptDeadline != nil {
				res.Config["attempt_deadline"] = job.AttemptDeadline.AsDuration().String()
			}

			// Target type - determine which type of target is configured
			switch target := job.Target.(type) {
			case *schedulerpb.Job_HttpTarget:
				res.Config["target_type"] = "http"
				httpConfig := map[string]interface{}{
					"uri":         target.HttpTarget.Uri,
					"http_method": target.HttpTarget.HttpMethod.String(),
				}
				// Headers (keys only for security)
				if target.HttpTarget.Headers != nil {
					var headerKeys []string
					for k := range target.HttpTarget.Headers {
						headerKeys = append(headerKeys, k)
					}
					httpConfig["header_keys"] = headerKeys
				}
				// Auth type
				switch target.HttpTarget.AuthorizationHeader.(type) {
				case *schedulerpb.HttpTarget_OauthToken:
					httpConfig["auth_type"] = "oauth"
				case *schedulerpb.HttpTarget_OidcToken:
					httpConfig["auth_type"] = "oidc"
				}
				res.Config["http_target"] = httpConfig

			case *schedulerpb.Job_PubsubTarget:
				res.Config["target_type"] = "pubsub"
				pubsubConfig := map[string]interface{}{
					"topic_name": target.PubsubTarget.TopicName,
				}
				// Attributes (keys only for security)
				if target.PubsubTarget.Attributes != nil {
					var attrKeys []string
					for k := range target.PubsubTarget.Attributes {
						attrKeys = append(attrKeys, k)
					}
					pubsubConfig["attribute_keys"] = attrKeys
				}
				res.Config["pubsub_target"] = pubsubConfig

			case *schedulerpb.Job_AppEngineHttpTarget:
				res.Config["target_type"] = "appengine"
				appEngineConfig := map[string]interface{}{
					"http_method":  target.AppEngineHttpTarget.HttpMethod.String(),
					"relative_uri": target.AppEngineHttpTarget.RelativeUri,
				}
				if target.AppEngineHttpTarget.AppEngineRouting != nil {
					appEngineConfig["service"] = target.AppEngineHttpTarget.AppEngineRouting.Service
					appEngineConfig["version"] = target.AppEngineHttpTarget.AppEngineRouting.Version
					appEngineConfig["instance"] = target.AppEngineHttpTarget.AppEngineRouting.Instance
				}
				// Headers (keys only for security)
				if target.AppEngineHttpTarget.Headers != nil {
					var headerKeys []string
					for k := range target.AppEngineHttpTarget.Headers {
						headerKeys = append(headerKeys, k)
					}
					appEngineConfig["header_keys"] = headerKeys
				}
				res.Config["appengine_http_target"] = appEngineConfig
			}

			// Status
			if job.Status != nil {
				res.Config["status_code"] = job.Status.Code
				res.Config["status_message"] = job.Status.Message
			}

			// Report progress
			if opts != nil && opts.OnProgress != nil {
				opts.OnProgress(parser.ProgressEvent{
					Step:           "scanning",
					Message:        fmt.Sprintf("Found Cloud Scheduler job: %s", jobName),
					Service:        "Cloud Scheduler",
					ResourcesFound: len(infra.Resources) + 1,
				})
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
