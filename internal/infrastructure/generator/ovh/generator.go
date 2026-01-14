// Package ovh generates Terraform configurations for OVHcloud deployments.
// It converts mapping results from AWS/GCP/Azure to OVH-compatible Terraform configurations
// using the OVH and OpenStack providers.
package ovh

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/generator"
	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/provider"
	"github.com/homeport/homeport/internal/domain/resource"
	"github.com/homeport/homeport/internal/domain/target"
)

// Generator generates Terraform configurations for OVHcloud.
type Generator struct{}

// New creates a new OVH Terraform generator.
func New() *Generator {
	return &Generator{}
}

// Platform returns the target platform.
func (g *Generator) Platform() target.Platform {
	return target.PlatformOVH
}

// Name returns the generator name.
func (g *Generator) Name() string {
	return "ovh-terraform"
}

// Description returns description.
func (g *Generator) Description() string {
	return "Generates Terraform for OVHcloud (instances, databases, object storage via OpenStack)"
}

// SupportedHALevels returns supported levels.
func (g *Generator) SupportedHALevels() []target.HALevel {
	return []target.HALevel{
		target.HALevelNone,
		target.HALevelBasic,
		target.HALevelMultiServer,
		target.HALevelCluster,
	}
}

// RequiresCredentials returns true.
func (g *Generator) RequiresCredentials() bool {
	return true
}

// RequiredCredentials returns required creds.
func (g *Generator) RequiredCredentials() []string {
	return []string{"OVH_ENDPOINT", "OVH_APPLICATION_KEY", "OVH_APPLICATION_SECRET", "OVH_CONSUMER_KEY", "OS_AUTH_URL", "OS_USERNAME", "OS_PASSWORD", "OS_TENANT_NAME"}
}

// Validate validates inputs.
func (g *Generator) Validate(results []*mapper.MappingResult, config *generator.TargetConfig) error {
	if len(results) == 0 {
		return fmt.Errorf("no mapping results")
	}
	return nil
}

// Generate produces Terraform files.
func (g *Generator) Generate(ctx context.Context, results []*mapper.MappingResult, config *generator.TargetConfig) (*generator.TargetOutput, error) {
	if err := g.Validate(results, config); err != nil {
		return nil, err
	}

	output := generator.NewTargetOutput(target.PlatformOVH)
	region := "GRA11"
	if config.TargetConfig != nil && config.TargetConfig.OVH != nil && config.TargetConfig.OVH.Region != "" {
		region = config.TargetConfig.OVH.Region
	}

	// Collect services
	var services []*mapper.DockerService
	for _, r := range results {
		if r.DockerService != nil {
			services = append(services, r.DockerService)
		}
		services = append(services, r.AdditionalServices...)
	}

	// Generate main.tf
	mainTf := g.generateMain(region)
	output.AddTerraformFile("main.tf", []byte(mainTf))

	// Generate variables.tf
	varsTf := g.generateVariables(config)
	output.AddTerraformFile("variables.tf", []byte(varsTf))

	// Generate compute.tf
	computeTf := g.generateCompute(services, config, region)
	output.AddTerraformFile("compute.tf", []byte(computeTf))

	// Generate networking.tf
	networkTf := g.generateNetworking(config, region)
	output.AddTerraformFile("networking.tf", []byte(networkTf))

	// Generate outputs.tf
	outputsTf := g.generateOutputs()
	output.AddTerraformFile("outputs.tf", []byte(outputsTf))

	// Generate terraform.tfvars.example
	tfvars := g.generateTfvarsExample(config)
	output.AddTerraformFile("terraform.tfvars.example", []byte(tfvars))

	output.MainFile = "main.tf"
	output.Summary = fmt.Sprintf("Generated OVH Terraform with %d services", len(services))
	output.AddManualStep("Configure OVH API credentials in environment or tfvars")
	output.AddManualStep("terraform init && terraform plan && terraform apply")

	return output, nil
}

// EstimateCost estimates the monthly cost using the provider catalog.
func (g *Generator) EstimateCost(results []*mapper.MappingResult, config *generator.TargetConfig) (*generator.CostEstimate, error) {
	estimate := generator.NewCostEstimate("EUR")

	// Get OVH pricing from catalog
	pricing := provider.GetProviderPricing(provider.ProviderOVH)
	if pricing == nil {
		// Fallback to simple estimate if catalog unavailable
		estimate.Compute = float64(len(results)) * 8.0
		estimate.Calculate()
		estimate.AddNote("Fallback pricing (catalog unavailable)")
		return estimate, nil
	}

	// Categorize resources
	categorized := g.categorizeResources(results)

	// Extract requirements from mapping results and select instance type
	requirements := provider.ExtractRequirements(results)
	instanceType := provider.SelectInstance(provider.ProviderOVH, requirements)

	// Fall back to HA-level based selection if no suitable instance found
	if instanceType == nil {
		instanceType = g.selectInstanceType(config.HALevel, pricing)
	}

	instanceCount := g.getInstanceCount(config.HALevel)

	// Compute costs
	computeCount := len(categorized.Compute)
	if computeCount == 0 {
		computeCount = 1 // At least one instance for services
	}
	if instanceType != nil {
		instanceCost := instanceType.PricePerMonth * float64(computeCount)
		estimate.Compute += instanceCost
		estimate.AddDetail("instances_"+instanceType.Type, instanceCost)

		// Additional workers for HA
		if instanceCount > 1 {
			workerCost := instanceType.PricePerMonth * float64(instanceCount-1)
			estimate.Compute += workerCost
			estimate.AddDetail("ha_workers", workerCost)
		}
	} else {
		// Fallback
		estimate.Compute = float64(computeCount+instanceCount-1) * 8.0
	}

	// Database costs
	for range categorized.Databases {
		dbCost := g.getDBCost(config.HALevel)
		estimate.Database += dbCost
		estimate.AddDetail("database", dbCost)
	}

	// Cache costs
	for range categorized.Caches {
		cacheCost := g.getCacheCost(config.HALevel)
		estimate.Database += cacheCost
		estimate.AddDetail("cache", cacheCost)
	}

	// Storage costs using catalog pricing (0.04 EUR/GB/month for OVH)
	storageGB := 0
	for range categorized.ObjectStorage {
		storageGB += 100 // Estimate 100GB per bucket
	}
	for range categorized.BlockStorage {
		storageGB += 50 // Estimate 50GB per volume
	}
	if storageGB > 0 {
		storageCost := pricing.Storage.EstimateStorageCost(storageGB)
		estimate.Storage = storageCost
		estimate.AddDetail("storage", storageCost)
	}

	// Network costs - OVH has unlimited free egress!
	// No egress charges for OVH
	if config.HALevel.RequiresMultiServer() {
		// Load balancer cost estimate
		lbCost := 15.0 // Octavia LB estimate
		estimate.Network += lbCost
		estimate.AddDetail("load_balancer", lbCost)
	}

	estimate.Calculate()

	estimate.AddNote("Prices from OVH catalog (last updated: December 2024)")
	estimate.AddNote(fmt.Sprintf("Storage: %.2f EUR/GB/month", pricing.Storage.PricePerGBMonth))
	estimate.AddNote("Network egress: Unlimited FREE (OVH doesn't charge for egress)")
	estimate.AddNote("OpenStack-based infrastructure")

	return estimate, nil
}

// CategorizedResources holds resources grouped by type for generation.
type CategorizedResources struct {
	Compute       []*mapper.MappingResult
	Kubernetes    []*mapper.MappingResult
	Databases     []*mapper.MappingResult
	Caches        []*mapper.MappingResult
	ObjectStorage []*mapper.MappingResult
	BlockStorage  []*mapper.MappingResult
	LoadBalancers []*mapper.MappingResult
	Other         []*mapper.MappingResult
}

// categorizeResources groups mapping results by resource type.
func (g *Generator) categorizeResources(results []*mapper.MappingResult) *CategorizedResources {
	categorized := &CategorizedResources{}

	for _, r := range results {
		if r == nil {
			continue
		}

		category := r.SourceCategory
		resourceType := strings.ToLower(r.SourceResourceType)

		switch category {
		case resource.CategoryCompute, resource.CategoryServerless:
			categorized.Compute = append(categorized.Compute, r)

		case resource.CategoryContainer:
			if strings.Contains(resourceType, "kubernetes") ||
				strings.Contains(resourceType, "eks") ||
				strings.Contains(resourceType, "gke") ||
				strings.Contains(resourceType, "aks") {
				categorized.Kubernetes = append(categorized.Kubernetes, r)
			} else {
				categorized.Compute = append(categorized.Compute, r)
			}

		case resource.CategoryKubernetes:
			categorized.Kubernetes = append(categorized.Kubernetes, r)

		case resource.CategorySQLDatabase, resource.CategoryNoSQLDatabase:
			categorized.Databases = append(categorized.Databases, r)

		case resource.CategoryCache:
			categorized.Caches = append(categorized.Caches, r)

		case resource.CategoryObjectStorage:
			categorized.ObjectStorage = append(categorized.ObjectStorage, r)

		case resource.CategoryBlockStorage, resource.CategoryFileStorage:
			categorized.BlockStorage = append(categorized.BlockStorage, r)

		case resource.CategoryLoadBalancer:
			categorized.LoadBalancers = append(categorized.LoadBalancers, r)

		default:
			categorized.Other = append(categorized.Other, r)
		}
	}

	return categorized
}

// selectInstanceType selects the best instance type from the catalog based on HA level.
func (g *Generator) selectInstanceType(level target.HALevel, pricing *provider.ProviderPricing) *provider.InstancePricing {
	if pricing == nil || len(pricing.Instances) == 0 {
		return nil
	}

	// Map HA levels to desired specs
	var minVCPUs int
	var minMemoryGB float64

	switch level {
	case target.HALevelNone:
		minVCPUs = 1
		minMemoryGB = 2
	case target.HALevelBasic:
		minVCPUs = 1
		minMemoryGB = 4
	case target.HALevelMultiServer:
		minVCPUs = 2
		minMemoryGB = 7
	case target.HALevelCluster:
		minVCPUs = 4
		minMemoryGB = 15
	default:
		minVCPUs = 1
		minMemoryGB = 2
	}

	// Find the cheapest instance that meets requirements
	var bestMatch *provider.InstancePricing
	for i := range pricing.Instances {
		inst := &pricing.Instances[i]
		if inst.VCPUs >= minVCPUs && inst.MemoryGB >= minMemoryGB {
			if bestMatch == nil || inst.PricePerMonth < bestMatch.PricePerMonth {
				bestMatch = inst
			}
		}
	}

	// If no match found, return the largest available
	if bestMatch == nil && len(pricing.Instances) > 0 {
		bestMatch = &pricing.Instances[len(pricing.Instances)-1]
	}

	return bestMatch
}

// getDBCost returns database cost estimate based on HA level.
func (g *Generator) getDBCost(level target.HALevel) float64 {
	switch level {
	case target.HALevelNone:
		return 15.0
	case target.HALevelBasic:
		return 30.0
	case target.HALevelMultiServer:
		return 60.0
	case target.HALevelCluster:
		return 120.0
	default:
		return 15.0
	}
}

// getCacheCost returns cache cost estimate based on HA level.
func (g *Generator) getCacheCost(level target.HALevel) float64 {
	switch level {
	case target.HALevelNone:
		return 10.0
	case target.HALevelBasic:
		return 20.0
	case target.HALevelMultiServer:
		return 40.0
	case target.HALevelCluster:
		return 80.0
	default:
		return 10.0
	}
}

func (g *Generator) generateMain(region string) string {
	return fmt.Sprintf(`# OVHcloud Terraform Configuration
# Generated by Homeport - %s

terraform {
  required_version = ">= 1.0"
  required_providers {
    ovh = {
      source  = "ovh/ovh"
      version = "~> 0.36"
    }
    openstack = {
      source  = "terraform-provider-openstack/openstack"
      version = "~> 1.53"
    }
  }
}

provider "ovh" {
  endpoint           = var.ovh_endpoint
  application_key    = var.ovh_application_key
  application_secret = var.ovh_application_secret
  consumer_key       = var.ovh_consumer_key
}

provider "openstack" {
  auth_url    = var.os_auth_url
  user_name   = var.os_username
  password    = var.os_password
  tenant_name = var.os_tenant_name
  region      = "%s"
}
`, time.Now().Format(time.RFC3339), region)
}

func (g *Generator) generateVariables(config *generator.TargetConfig) string {
	return fmt.Sprintf(`variable "project_name" {
  description = "Project name"
  type        = string
  default     = "%s"
}

variable "ovh_endpoint" {
  description = "OVH API endpoint"
  type        = string
  default     = "ovh-eu"
}

variable "ovh_application_key" {
  description = "OVH application key"
  type        = string
  sensitive   = true
}

variable "ovh_application_secret" {
  description = "OVH application secret"
  type        = string
  sensitive   = true
}

variable "ovh_consumer_key" {
  description = "OVH consumer key"
  type        = string
  sensitive   = true
}

variable "os_auth_url" {
  description = "OpenStack auth URL"
  type        = string
  default     = "https://auth.cloud.ovh.net/v3"
}

variable "os_username" {
  description = "OpenStack username"
  type        = string
}

variable "os_password" {
  description = "OpenStack password"
  type        = string
  sensitive   = true
}

variable "os_tenant_name" {
  description = "OpenStack tenant/project name"
  type        = string
}

variable "instance_flavor" {
  description = "Instance flavor"
  type        = string
  default     = "s1-2"
}
`, config.ProjectName)
}

func (g *Generator) generateCompute(services []*mapper.DockerService, config *generator.TargetConfig, region string) string {
	var buf bytes.Buffer
	buf.WriteString("# Compute Resources (OpenStack)\n\n")

	buf.WriteString(`data "openstack_images_image_v2" "ubuntu" {
  name        = "Ubuntu 22.04"
  most_recent = true
}

resource "openstack_compute_instance_v2" "main" {
  name            = "${var.project_name}-main"
  image_id        = data.openstack_images_image_v2.ubuntu.id
  flavor_name     = var.instance_flavor
  security_groups = ["default"]

  network {
    name = "Ext-Net"
  }

  metadata = {
    managed_by = "homeport"
  }
}

`)

	instanceCount := g.getInstanceCount(config.HALevel)
	if instanceCount > 1 {
		buf.WriteString(fmt.Sprintf(`resource "openstack_compute_instance_v2" "workers" {
  count           = %d
  name            = "${var.project_name}-worker-${count.index}"
  image_id        = data.openstack_images_image_v2.ubuntu.id
  flavor_name     = var.instance_flavor
  security_groups = ["default"]

  network {
    name = "Ext-Net"
  }

  metadata = {
    managed_by = "homeport"
    role       = "worker"
  }
}
`, instanceCount-1))
	}

	return buf.String()
}

func (g *Generator) generateNetworking(config *generator.TargetConfig, region string) string {
	var buf bytes.Buffer
	buf.WriteString("# Networking Resources\n\n")

	buf.WriteString(`resource "openstack_networking_network_v2" "private" {
  name           = "${var.project_name}-network"
  admin_state_up = true
}

resource "openstack_networking_subnet_v2" "private" {
  name       = "${var.project_name}-subnet"
  network_id = openstack_networking_network_v2.private.id
  cidr       = "10.0.0.0/24"
  ip_version = 4
}

`)

	if config.HALevel.RequiresMultiServer() {
		buf.WriteString(`resource "openstack_lb_loadbalancer_v2" "main" {
  name          = "${var.project_name}-lb"
  vip_subnet_id = openstack_networking_subnet_v2.private.id
}

resource "openstack_lb_listener_v2" "http" {
  name            = "http"
  protocol        = "HTTP"
  protocol_port   = 80
  loadbalancer_id = openstack_lb_loadbalancer_v2.main.id
}

resource "openstack_lb_pool_v2" "main" {
  name        = "main-pool"
  protocol    = "HTTP"
  lb_method   = "ROUND_ROBIN"
  listener_id = openstack_lb_listener_v2.http.id
}
`)
	}

	return buf.String()
}

func (g *Generator) generateOutputs() string {
	return `# Outputs

output "main_instance_ip" {
  description = "Main instance IP"
  value       = openstack_compute_instance_v2.main.access_ip_v4
}
`
}

func (g *Generator) generateTfvarsExample(config *generator.TargetConfig) string {
	return fmt.Sprintf(`# OVH Terraform Variables
# Copy to terraform.tfvars and fill in values

project_name = "%s"

# OVH API credentials (from https://api.ovh.com/createToken/)
ovh_application_key    = ""
ovh_application_secret = ""
ovh_consumer_key       = ""

# OpenStack credentials (from OVH control panel)
os_username    = ""
os_password    = ""
os_tenant_name = ""
`, config.ProjectName)
}

func (g *Generator) getInstanceCount(level target.HALevel) int {
	switch level {
	case target.HALevelCluster:
		return 3
	case target.HALevelMultiServer:
		return 2
	default:
		return 1
	}
}

func init() {
	generator.RegisterGenerator(New())
}
