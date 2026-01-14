// Package scaleway generates Terraform configurations for Scaleway cloud platform.
package scaleway

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/generator"
	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/provider"
	"github.com/homeport/homeport/internal/domain/resource"
	"github.com/homeport/homeport/internal/domain/target"
)

// Generator generates Terraform configurations for Scaleway.
type Generator struct{}

// New creates a new Scaleway Terraform generator.
func New() *Generator {
	return &Generator{}
}

// Platform returns the target platform this generator handles.
func (g *Generator) Platform() target.Platform {
	return target.PlatformScaleway
}

// Name returns the name of this generator.
func (g *Generator) Name() string {
	return "scaleway-terraform"
}

// Description returns a human-readable description.
func (g *Generator) Description() string {
	return "Generates Terraform configurations for Scaleway cloud (France) - Instances, Kapsule, RDB, Redis, Object Storage"
}

// SupportedHALevels returns the HA levels this generator supports.
func (g *Generator) SupportedHALevels() []target.HALevel {
	return []target.HALevel{
		target.HALevelNone,
		target.HALevelBasic,
		target.HALevelMultiServer,
		target.HALevelCluster,
	}
}

// RequiresCredentials returns true if the platform needs cloud credentials.
func (g *Generator) RequiresCredentials() bool {
	return true
}

// RequiredCredentials returns the list of required credential keys.
func (g *Generator) RequiredCredentials() []string {
	return []string{
		"SCW_ACCESS_KEY",
		"SCW_SECRET_KEY",
		"SCW_PROJECT_ID",
	}
}

// Validate checks if the mapping results can be deployed to this platform.
func (g *Generator) Validate(results []*mapper.MappingResult, config *generator.TargetConfig) error {
	if len(results) == 0 {
		return fmt.Errorf("no mapping results provided")
	}
	return nil
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
	VPCs          []*mapper.MappingResult
	DNS           []*mapper.MappingResult
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
			// ECS, Cloud Run, Container Instances -> Compute or K8s
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

		case resource.CategoryDNS:
			categorized.DNS = append(categorized.DNS, r)

		case resource.CategoryCDN, resource.CategoryNetworking:
			if strings.Contains(resourceType, "vpc") ||
				strings.Contains(resourceType, "network") ||
				strings.Contains(resourceType, "subnet") {
				categorized.VPCs = append(categorized.VPCs, r)
			} else {
				categorized.Other = append(categorized.Other, r)
			}

		default:
			categorized.Other = append(categorized.Other, r)
		}
	}

	return categorized
}

// Generate produces output artifacts for the Scaleway platform.
func (g *Generator) Generate(ctx context.Context, results []*mapper.MappingResult, config *generator.TargetConfig) (*generator.TargetOutput, error) {
	if err := g.Validate(results, config); err != nil {
		return nil, err
	}

	output := generator.NewTargetOutput(target.PlatformScaleway)

	// Extract region/zone from config
	region := "fr-par"
	zone := "fr-par-1"
	if config.TargetConfig != nil && config.TargetConfig.Scaleway != nil {
		if config.TargetConfig.Scaleway.Region != "" {
			region = config.TargetConfig.Scaleway.Region
		}
		if config.TargetConfig.Scaleway.Zone != "" {
			zone = config.TargetConfig.Scaleway.Zone
		}
	}

	// Categorize resources
	categorized := g.categorizeResources(results)

	// Generate main.tf
	mainTf := g.generateMainTF(config, region, zone)
	output.AddTerraformFile("main.tf", []byte(mainTf))

	// Generate variables.tf
	varsTf := g.generateVariablesTF(config, categorized)
	output.AddTerraformFile("variables.tf", []byte(varsTf))

	// Generate compute.tf (instances + kubernetes)
	if len(categorized.Compute) > 0 || len(categorized.Kubernetes) > 0 {
		computeTf := GenerateComputeTF(categorized.Compute, categorized.Kubernetes, config, zone)
		output.AddTerraformFile("compute.tf", []byte(computeTf))
	}

	// Generate database.tf (RDB + Redis)
	if len(categorized.Databases) > 0 || len(categorized.Caches) > 0 {
		dbTf := GenerateDatabaseTF(categorized.Databases, categorized.Caches, config, region)
		output.AddTerraformFile("database.tf", []byte(dbTf))
	}

	// Generate storage.tf (Object Storage + Block Storage)
	if len(categorized.ObjectStorage) > 0 || len(categorized.BlockStorage) > 0 {
		storageTf := GenerateStorageTF(categorized.ObjectStorage, categorized.BlockStorage, config, region)
		output.AddTerraformFile("storage.tf", []byte(storageTf))
	}

	// Generate networking.tf (VPC, LB, DNS) - always generate VPC
	networkTf := GenerateNetworkingTF(categorized.VPCs, categorized.LoadBalancers, categorized.DNS, config, zone)
	output.AddTerraformFile("networking.tf", []byte(networkTf))

	// Generate outputs.tf
	outputsTf := g.generateOutputsTF(categorized)
	output.AddTerraformFile("outputs.tf", []byte(outputsTf))

	// Generate terraform.tfvars.example
	tfvarsExample := g.generateTFVarsExample(config, categorized)
	output.AddTerraformFile("terraform.tfvars.example", []byte(tfvarsExample))

	// Estimate costs
	costEstimate, err := g.EstimateCost(results, config)
	if err == nil {
		output.EstimatedCost = costEstimate
	}

	// Set metadata
	output.MainFile = "main.tf"
	output.Summary = g.generateSummary(categorized, config)
	output.GeneratedAt = time.Now()

	// Add manual steps
	output.AddManualStep("Set environment variables: export SCW_ACCESS_KEY=xxx SCW_SECRET_KEY=xxx SCW_PROJECT_ID=xxx")
	output.AddManualStep("Copy terraform.tfvars.example to terraform.tfvars and fill in values")
	output.AddManualStep("Run: terraform init")
	output.AddManualStep("Run: terraform plan")
	output.AddManualStep("Run: terraform apply")
	output.AddManualStep("Configure DNS records if using custom domains")

	return output, nil
}

// generateMainTF generates the main.tf file with provider configuration.
func (g *Generator) generateMainTF(config *generator.TargetConfig, region, zone string) string {
	var buf bytes.Buffer

	buf.WriteString("# Scaleway Terraform Configuration\n")
	buf.WriteString(fmt.Sprintf("# Generated by Homeport - %s\n", time.Now().Format(time.RFC3339)))
	buf.WriteString(fmt.Sprintf("# Project: %s\n", config.ProjectName))
	buf.WriteString(fmt.Sprintf("# HA Level: %s\n\n", config.HALevel.String()))

	// Terraform block
	buf.WriteString("terraform {\n")
	buf.WriteString("  required_version = \">= 1.0.0\"\n\n")
	buf.WriteString("  required_providers {\n")
	buf.WriteString("    scaleway = {\n")
	buf.WriteString("      source  = \"scaleway/scaleway\"\n")
	buf.WriteString("      version = \"~> 2.0\"\n")
	buf.WriteString("    }\n")
	buf.WriteString("  }\n")
	buf.WriteString("}\n\n")

	// Provider block
	buf.WriteString("provider \"scaleway\" {\n")
	buf.WriteString("  access_key = var.scw_access_key\n")
	buf.WriteString("  secret_key = var.scw_secret_key\n")
	buf.WriteString("  project_id = var.scw_project_id\n")
	buf.WriteString("  region     = var.scw_region\n")
	buf.WriteString("  zone       = var.scw_zone\n")
	buf.WriteString("}\n\n")

	// Local values
	buf.WriteString("locals {\n")
	buf.WriteString(fmt.Sprintf("  project_name = \"%s\"\n", SanitizeName(config.ProjectName)))
	buf.WriteString("  common_tags = [\n")
	buf.WriteString("    \"managed-by:homeport\",\n")
	buf.WriteString("    \"project:${local.project_name}\",\n")
	buf.WriteString(fmt.Sprintf("    \"ha-level:%s\",\n", config.HALevel.String()))
	buf.WriteString("    \"environment:${var.environment}\",\n")
	buf.WriteString("  ]\n")
	buf.WriteString("}\n")

	return buf.String()
}

// generateVariablesTF generates the variables.tf file.
func (g *Generator) generateVariablesTF(config *generator.TargetConfig, categorized *CategorizedResources) string {
	var buf bytes.Buffer

	buf.WriteString("# Scaleway Terraform Variables\n")
	buf.WriteString(fmt.Sprintf("# Generated by Homeport - %s\n\n", time.Now().Format(time.RFC3339)))

	// Authentication
	buf.WriteString("# ============================================\n")
	buf.WriteString("# Authentication\n")
	buf.WriteString("# ============================================\n\n")

	buf.WriteString("variable \"scw_access_key\" {\n")
	buf.WriteString("  description = \"Scaleway access key\"\n")
	buf.WriteString("  type        = string\n")
	buf.WriteString("  sensitive   = true\n")
	buf.WriteString("}\n\n")

	buf.WriteString("variable \"scw_secret_key\" {\n")
	buf.WriteString("  description = \"Scaleway secret key\"\n")
	buf.WriteString("  type        = string\n")
	buf.WriteString("  sensitive   = true\n")
	buf.WriteString("}\n\n")

	buf.WriteString("variable \"scw_project_id\" {\n")
	buf.WriteString("  description = \"Scaleway project ID\"\n")
	buf.WriteString("  type        = string\n")
	buf.WriteString("}\n\n")

	// Region and Zone
	buf.WriteString("# ============================================\n")
	buf.WriteString("# Region and Zone\n")
	buf.WriteString("# ============================================\n\n")

	buf.WriteString("variable \"scw_region\" {\n")
	buf.WriteString("  description = \"Scaleway region (fr-par, nl-ams, pl-waw)\"\n")
	buf.WriteString("  type        = string\n")
	buf.WriteString("  default     = \"fr-par\"\n")
	buf.WriteString("}\n\n")

	buf.WriteString("variable \"scw_zone\" {\n")
	buf.WriteString("  description = \"Scaleway zone (fr-par-1, fr-par-2, fr-par-3, nl-ams-1, nl-ams-2, pl-waw-1, pl-waw-2)\"\n")
	buf.WriteString("  type        = string\n")
	buf.WriteString("  default     = \"fr-par-1\"\n")
	buf.WriteString("}\n\n")

	// Environment
	buf.WriteString("variable \"environment\" {\n")
	buf.WriteString("  description = \"Environment (dev, staging, prod)\"\n")
	buf.WriteString("  type        = string\n")
	buf.WriteString("  default     = \"prod\"\n")
	buf.WriteString("}\n\n")

	// Instance Configuration
	if len(categorized.Compute) > 0 {
		buf.WriteString("# ============================================\n")
		buf.WriteString("# Instance Configuration\n")
		buf.WriteString("# ============================================\n\n")

		buf.WriteString("variable \"instance_type\" {\n")
		buf.WriteString("  description = \"Scaleway instance type (DEV1-S, DEV1-M, DEV1-L, GP1-XS, GP1-S, GP1-M, etc.)\"\n")
		buf.WriteString("  type        = string\n")
		buf.WriteString(fmt.Sprintf("  default     = \"%s\"\n", GetInstanceType(config.HALevel)))
		buf.WriteString("}\n\n")

		buf.WriteString("variable \"instance_image\" {\n")
		buf.WriteString("  description = \"Instance OS image\"\n")
		buf.WriteString("  type        = string\n")
		buf.WriteString("  default     = \"ubuntu_jammy\"\n")
		buf.WriteString("}\n\n")

		buf.WriteString("variable \"ssh_public_key\" {\n")
		buf.WriteString("  description = \"SSH public key for instance access\"\n")
		buf.WriteString("  type        = string\n")
		buf.WriteString("  default     = \"\"\n")
		buf.WriteString("}\n\n")
	}

	// Kubernetes Configuration
	if len(categorized.Kubernetes) > 0 {
		buf.WriteString("# ============================================\n")
		buf.WriteString("# Kubernetes (Kapsule) Configuration\n")
		buf.WriteString("# ============================================\n\n")

		buf.WriteString("variable \"k8s_version\" {\n")
		buf.WriteString("  description = \"Kubernetes version\"\n")
		buf.WriteString("  type        = string\n")
		buf.WriteString("  default     = \"1.28\"\n")
		buf.WriteString("}\n\n")

		buf.WriteString("variable \"k8s_node_type\" {\n")
		buf.WriteString("  description = \"Kubernetes node pool instance type\"\n")
		buf.WriteString("  type        = string\n")
		buf.WriteString(fmt.Sprintf("  default     = \"%s\"\n", GetK8sNodeType(config.HALevel)))
		buf.WriteString("}\n\n")

		buf.WriteString("variable \"k8s_node_count\" {\n")
		buf.WriteString("  description = \"Number of nodes in the default pool\"\n")
		buf.WriteString("  type        = number\n")
		buf.WriteString(fmt.Sprintf("  default     = %d\n", GetK8sNodeCount(config.HALevel)))
		buf.WriteString("}\n\n")

		buf.WriteString("variable \"k8s_autoscale\" {\n")
		buf.WriteString("  description = \"Enable cluster autoscaling\"\n")
		buf.WriteString("  type        = bool\n")
		buf.WriteString(fmt.Sprintf("  default     = %t\n", config.HALevel.RequiresMultiServer()))
		buf.WriteString("}\n\n")

		buf.WriteString("variable \"k8s_min_nodes\" {\n")
		buf.WriteString("  description = \"Minimum nodes when autoscaling\"\n")
		buf.WriteString("  type        = number\n")
		buf.WriteString(fmt.Sprintf("  default     = %d\n", GetK8sNodeCount(config.HALevel)))
		buf.WriteString("}\n\n")

		buf.WriteString("variable \"k8s_max_nodes\" {\n")
		buf.WriteString("  description = \"Maximum nodes when autoscaling\"\n")
		buf.WriteString("  type        = number\n")
		buf.WriteString(fmt.Sprintf("  default     = %d\n", GetK8sNodeCount(config.HALevel)*2))
		buf.WriteString("}\n\n")
	}

	// Database Configuration
	if len(categorized.Databases) > 0 {
		buf.WriteString("# ============================================\n")
		buf.WriteString("# Database (RDB) Configuration\n")
		buf.WriteString("# ============================================\n\n")

		buf.WriteString("variable \"db_node_type\" {\n")
		buf.WriteString("  description = \"Database node type (DB-DEV-S, DB-DEV-M, DB-GP-XS, etc.)\"\n")
		buf.WriteString("  type        = string\n")
		buf.WriteString(fmt.Sprintf("  default     = \"%s\"\n", GetDBNodeType(config.HALevel)))
		buf.WriteString("}\n\n")

		buf.WriteString("variable \"db_engine\" {\n")
		buf.WriteString("  description = \"Database engine (PostgreSQL-15, MySQL-8, etc.)\"\n")
		buf.WriteString("  type        = string\n")
		buf.WriteString("  default     = \"PostgreSQL-15\"\n")
		buf.WriteString("}\n\n")

		buf.WriteString("variable \"db_username\" {\n")
		buf.WriteString("  description = \"Database admin username\"\n")
		buf.WriteString("  type        = string\n")
		buf.WriteString("  default     = \"admin\"\n")
		buf.WriteString("}\n\n")

		buf.WriteString("variable \"db_password\" {\n")
		buf.WriteString("  description = \"Database admin password\"\n")
		buf.WriteString("  type        = string\n")
		buf.WriteString("  sensitive   = true\n")
		buf.WriteString("}\n\n")

		buf.WriteString("variable \"db_ha_enabled\" {\n")
		buf.WriteString("  description = \"Enable database HA cluster\"\n")
		buf.WriteString("  type        = bool\n")
		buf.WriteString(fmt.Sprintf("  default     = %t\n", config.HALevel.RequiresMultiServer()))
		buf.WriteString("}\n\n")
	}

	// Redis Configuration
	if len(categorized.Caches) > 0 {
		buf.WriteString("# ============================================\n")
		buf.WriteString("# Redis Configuration\n")
		buf.WriteString("# ============================================\n\n")

		buf.WriteString("variable \"redis_node_type\" {\n")
		buf.WriteString("  description = \"Redis node type (RED1-MICRO, RED1-XS, RED1-S, etc.)\"\n")
		buf.WriteString("  type        = string\n")
		buf.WriteString(fmt.Sprintf("  default     = \"%s\"\n", GetRedisNodeType(config.HALevel)))
		buf.WriteString("}\n\n")

		buf.WriteString("variable \"redis_version\" {\n")
		buf.WriteString("  description = \"Redis version\"\n")
		buf.WriteString("  type        = string\n")
		buf.WriteString("  default     = \"7.0.5\"\n")
		buf.WriteString("}\n\n")

		buf.WriteString("variable \"redis_cluster_size\" {\n")
		buf.WriteString("  description = \"Redis cluster size (1, 3, 5)\"\n")
		buf.WriteString("  type        = number\n")
		buf.WriteString(fmt.Sprintf("  default     = %d\n", GetRedisClusterSize(config.HALevel)))
		buf.WriteString("}\n\n")
	}

	// Storage Configuration
	if len(categorized.ObjectStorage) > 0 {
		buf.WriteString("# ============================================\n")
		buf.WriteString("# Object Storage Configuration\n")
		buf.WriteString("# ============================================\n\n")

		buf.WriteString("variable \"bucket_versioning\" {\n")
		buf.WriteString("  description = \"Enable bucket versioning\"\n")
		buf.WriteString("  type        = bool\n")
		buf.WriteString("  default     = true\n")
		buf.WriteString("}\n\n")

		buf.WriteString("variable \"bucket_lifecycle_days\" {\n")
		buf.WriteString("  description = \"Days before transitioning to infrequent access (0 to disable)\"\n")
		buf.WriteString("  type        = number\n")
		buf.WriteString("  default     = 90\n")
		buf.WriteString("}\n\n")
	}

	// Load Balancer Configuration
	if len(categorized.LoadBalancers) > 0 || config.HALevel.RequiresMultiServer() {
		buf.WriteString("# ============================================\n")
		buf.WriteString("# Load Balancer Configuration\n")
		buf.WriteString("# ============================================\n\n")

		buf.WriteString("variable \"lb_type\" {\n")
		buf.WriteString("  description = \"Load balancer type (LB-S, LB-GP-M, LB-GP-L)\"\n")
		buf.WriteString("  type        = string\n")
		buf.WriteString(fmt.Sprintf("  default     = \"%s\"\n", GetLBType(config.HALevel)))
		buf.WriteString("}\n\n")
	}

	// Domain Configuration
	buf.WriteString("# ============================================\n")
	buf.WriteString("# Domain Configuration\n")
	buf.WriteString("# ============================================\n\n")

	buf.WriteString("variable \"domain\" {\n")
	buf.WriteString("  description = \"Primary domain name (leave empty to skip DNS)\"\n")
	buf.WriteString("  type        = string\n")
	buf.WriteString("  default     = \"\"\n")
	buf.WriteString("}\n")

	return buf.String()
}

// generateOutputsTF generates the outputs.tf file.
func (g *Generator) generateOutputsTF(categorized *CategorizedResources) string {
	var buf bytes.Buffer

	buf.WriteString("# Scaleway Terraform Outputs\n")
	buf.WriteString(fmt.Sprintf("# Generated by Homeport - %s\n\n", time.Now().Format(time.RFC3339)))

	// Instance outputs
	if len(categorized.Compute) > 0 {
		buf.WriteString("# ============================================\n")
		buf.WriteString("# Compute Outputs\n")
		buf.WriteString("# ============================================\n\n")

		for _, r := range categorized.Compute {
			name := SanitizeTFName(r.SourceResourceName)
			if name == "" {
				continue
			}
			buf.WriteString(fmt.Sprintf("output \"instance_%s_ip\" {\n", name))
			buf.WriteString(fmt.Sprintf("  description = \"Public IP of instance %s\"\n", name))
			buf.WriteString(fmt.Sprintf("  value       = scaleway_instance_ip.%s.address\n", name))
			buf.WriteString("}\n\n")
		}
	}

	// Kubernetes outputs
	if len(categorized.Kubernetes) > 0 {
		buf.WriteString("# ============================================\n")
		buf.WriteString("# Kubernetes Outputs\n")
		buf.WriteString("# ============================================\n\n")

		for _, r := range categorized.Kubernetes {
			name := SanitizeTFName(r.SourceResourceName)
			if name == "" {
				continue
			}
			buf.WriteString(fmt.Sprintf("output \"k8s_%s_endpoint\" {\n", name))
			buf.WriteString(fmt.Sprintf("  description = \"Kubernetes API endpoint for %s\"\n", name))
			buf.WriteString(fmt.Sprintf("  value       = scaleway_k8s_cluster.%s.apiserver_url\n", name))
			buf.WriteString("  sensitive   = true\n")
			buf.WriteString("}\n\n")

			buf.WriteString(fmt.Sprintf("output \"k8s_%s_kubeconfig\" {\n", name))
			buf.WriteString(fmt.Sprintf("  description = \"Kubeconfig for %s\"\n", name))
			buf.WriteString(fmt.Sprintf("  value       = scaleway_k8s_cluster.%s.kubeconfig[0].config_file\n", name))
			buf.WriteString("  sensitive   = true\n")
			buf.WriteString("}\n\n")
		}
	}

	// Database outputs
	if len(categorized.Databases) > 0 {
		buf.WriteString("# ============================================\n")
		buf.WriteString("# Database Outputs\n")
		buf.WriteString("# ============================================\n\n")

		for _, r := range categorized.Databases {
			name := SanitizeTFName(r.SourceResourceName)
			if name == "" {
				continue
			}
			buf.WriteString(fmt.Sprintf("output \"db_%s_endpoint\" {\n", name))
			buf.WriteString(fmt.Sprintf("  description = \"Database endpoint for %s\"\n", name))
			buf.WriteString(fmt.Sprintf("  value       = scaleway_rdb_instance.%s.endpoint_ip\n", name))
			buf.WriteString("  sensitive   = true\n")
			buf.WriteString("}\n\n")

			buf.WriteString(fmt.Sprintf("output \"db_%s_port\" {\n", name))
			buf.WriteString(fmt.Sprintf("  description = \"Database port for %s\"\n", name))
			buf.WriteString(fmt.Sprintf("  value       = scaleway_rdb_instance.%s.endpoint_port\n", name))
			buf.WriteString("}\n\n")
		}
	}

	// Redis outputs
	if len(categorized.Caches) > 0 {
		buf.WriteString("# ============================================\n")
		buf.WriteString("# Redis Outputs\n")
		buf.WriteString("# ============================================\n\n")

		for _, r := range categorized.Caches {
			name := SanitizeTFName(r.SourceResourceName)
			if name == "" {
				continue
			}
			buf.WriteString(fmt.Sprintf("output \"redis_%s_endpoints\" {\n", name))
			buf.WriteString(fmt.Sprintf("  description = \"Redis endpoints for %s\"\n", name))
			buf.WriteString(fmt.Sprintf("  value       = scaleway_redis_cluster.%s.public_network[0].ips\n", name))
			buf.WriteString("  sensitive   = true\n")
			buf.WriteString("}\n\n")
		}
	}

	// Storage outputs
	if len(categorized.ObjectStorage) > 0 {
		buf.WriteString("# ============================================\n")
		buf.WriteString("# Storage Outputs\n")
		buf.WriteString("# ============================================\n\n")

		for _, r := range categorized.ObjectStorage {
			name := SanitizeTFName(r.SourceResourceName)
			if name == "" {
				continue
			}
			buf.WriteString(fmt.Sprintf("output \"bucket_%s_endpoint\" {\n", name))
			buf.WriteString(fmt.Sprintf("  description = \"Object storage endpoint for %s\"\n", name))
			buf.WriteString(fmt.Sprintf("  value       = scaleway_object_bucket.%s.endpoint\n", name))
			buf.WriteString("}\n\n")

			buf.WriteString(fmt.Sprintf("output \"bucket_%s_name\" {\n", name))
			buf.WriteString(fmt.Sprintf("  description = \"Bucket name for %s\"\n", name))
			buf.WriteString(fmt.Sprintf("  value       = scaleway_object_bucket.%s.name\n", name))
			buf.WriteString("}\n\n")
		}
	}

	// VPC output
	buf.WriteString("# ============================================\n")
	buf.WriteString("# Networking Outputs\n")
	buf.WriteString("# ============================================\n\n")

	buf.WriteString("output \"vpc_id\" {\n")
	buf.WriteString("  description = \"VPC Private Network ID\"\n")
	buf.WriteString("  value       = scaleway_vpc_private_network.main.id\n")
	buf.WriteString("}\n\n")

	// Load Balancer outputs
	if len(categorized.LoadBalancers) > 0 {
		for _, r := range categorized.LoadBalancers {
			name := SanitizeTFName(r.SourceResourceName)
			if name == "" {
				continue
			}
			buf.WriteString(fmt.Sprintf("output \"lb_%s_ip\" {\n", name))
			buf.WriteString(fmt.Sprintf("  description = \"Load balancer IP for %s\"\n", name))
			buf.WriteString(fmt.Sprintf("  value       = scaleway_lb_ip.%s.ip_address\n", name))
			buf.WriteString("}\n\n")
		}
	}

	return buf.String()
}

// generateTFVarsExample generates the terraform.tfvars.example file.
func (g *Generator) generateTFVarsExample(config *generator.TargetConfig, categorized *CategorizedResources) string {
	var buf bytes.Buffer

	buf.WriteString("# Scaleway Terraform Variables Example\n")
	buf.WriteString(fmt.Sprintf("# Generated by Homeport - %s\n", time.Now().Format(time.RFC3339)))
	buf.WriteString("# Copy this file to terraform.tfvars and fill in the values\n\n")

	// Authentication
	buf.WriteString("# ============================================\n")
	buf.WriteString("# Authentication (recommended: use environment variables)\n")
	buf.WriteString("# export TF_VAR_scw_access_key=\"SCWXXXXXXXXXX\"\n")
	buf.WriteString("# export TF_VAR_scw_secret_key=\"...\"\n")
	buf.WriteString("# ============================================\n\n")

	buf.WriteString("# scw_access_key = \"SCWXXXXXXXXXX\"  # Use TF_VAR_scw_access_key instead\n")
	buf.WriteString("# scw_secret_key = \"...\"           # Use TF_VAR_scw_secret_key instead\n")
	buf.WriteString("scw_project_id = \"your-project-id-here\"\n\n")

	// Region and Zone
	buf.WriteString("# Region and Zone\n")
	buf.WriteString("scw_region = \"fr-par\"\n")
	buf.WriteString("scw_zone   = \"fr-par-1\"\n\n")

	// Environment
	buf.WriteString("# Environment\n")
	buf.WriteString("environment = \"prod\"\n\n")

	// Instance Configuration
	if len(categorized.Compute) > 0 {
		buf.WriteString("# Instance Configuration\n")
		buf.WriteString(fmt.Sprintf("instance_type  = \"%s\"\n", GetInstanceType(config.HALevel)))
		buf.WriteString("instance_image = \"ubuntu_jammy\"\n")
		buf.WriteString("ssh_public_key = \"ssh-ed25519 AAAA... user@host\"\n\n")
	}

	// Kubernetes Configuration
	if len(categorized.Kubernetes) > 0 {
		buf.WriteString("# Kubernetes Configuration\n")
		buf.WriteString("k8s_version    = \"1.28\"\n")
		buf.WriteString(fmt.Sprintf("k8s_node_type  = \"%s\"\n", GetK8sNodeType(config.HALevel)))
		buf.WriteString(fmt.Sprintf("k8s_node_count = %d\n", GetK8sNodeCount(config.HALevel)))
		buf.WriteString(fmt.Sprintf("k8s_autoscale  = %t\n", config.HALevel.RequiresMultiServer()))
		buf.WriteString(fmt.Sprintf("k8s_min_nodes  = %d\n", GetK8sNodeCount(config.HALevel)))
		buf.WriteString(fmt.Sprintf("k8s_max_nodes  = %d\n\n", GetK8sNodeCount(config.HALevel)*2))
	}

	// Database Configuration
	if len(categorized.Databases) > 0 {
		buf.WriteString("# Database Configuration\n")
		buf.WriteString(fmt.Sprintf("db_node_type = \"%s\"\n", GetDBNodeType(config.HALevel)))
		buf.WriteString("db_engine    = \"PostgreSQL-15\"\n")
		buf.WriteString("db_username  = \"admin\"\n")
		buf.WriteString("# db_password = \"your-secure-password\"  # Use TF_VAR_db_password instead\n")
		buf.WriteString(fmt.Sprintf("db_ha_enabled = %t\n\n", config.HALevel.RequiresMultiServer()))
	}

	// Redis Configuration
	if len(categorized.Caches) > 0 {
		buf.WriteString("# Redis Configuration\n")
		buf.WriteString(fmt.Sprintf("redis_node_type   = \"%s\"\n", GetRedisNodeType(config.HALevel)))
		buf.WriteString("redis_version     = \"7.0.5\"\n")
		buf.WriteString(fmt.Sprintf("redis_cluster_size = %d\n\n", GetRedisClusterSize(config.HALevel)))
	}

	// Storage Configuration
	if len(categorized.ObjectStorage) > 0 {
		buf.WriteString("# Storage Configuration\n")
		buf.WriteString("bucket_versioning     = true\n")
		buf.WriteString("bucket_lifecycle_days = 90\n\n")
	}

	// Load Balancer Configuration
	if len(categorized.LoadBalancers) > 0 || config.HALevel.RequiresMultiServer() {
		buf.WriteString("# Load Balancer Configuration\n")
		buf.WriteString(fmt.Sprintf("lb_type = \"%s\"\n\n", GetLBType(config.HALevel)))
	}

	// Domain Configuration
	buf.WriteString("# Domain Configuration\n")
	buf.WriteString("domain = \"example.com\"\n")

	return buf.String()
}

// EstimateCost estimates the monthly cost for the deployment using the provider catalog.
func (g *Generator) EstimateCost(results []*mapper.MappingResult, config *generator.TargetConfig) (*generator.CostEstimate, error) {
	estimate := generator.NewCostEstimate("EUR")
	categorized := g.categorizeResources(results)

	// Get Scaleway pricing from catalog
	pricing := provider.GetProviderPricing(provider.ProviderScaleway)
	if pricing == nil {
		// Fallback to hardcoded values if catalog not available
		return g.estimateCostFallback(results, config)
	}

	// Extract requirements from mapping results and select instance type
	requirements := provider.ExtractRequirements(results)
	instanceType := provider.SelectInstance(provider.ProviderScaleway, requirements)

	// Fall back to HA-level based selection if no suitable instance found
	if instanceType == nil {
		instanceType = g.selectInstanceType(config.HALevel, pricing)
	}

	// Compute costs
	for range categorized.Compute {
		if instanceType != nil {
			estimate.Compute += instanceType.PricePerMonth
			estimate.AddDetail("instance_"+instanceType.Type, instanceType.PricePerMonth)
		} else {
			// Fallback
			estimate.Compute += GetInstanceCost(config.HALevel)
		}
	}

	// Kubernetes costs (nodes only, control plane is free)
	for range categorized.Kubernetes {
		nodeType := g.selectK8sNodeType(config.HALevel, pricing)
		nodeCount := GetK8sNodeCount(config.HALevel)
		if nodeType != nil {
			nodeCost := nodeType.PricePerMonth * float64(nodeCount)
			estimate.Compute += nodeCost
			estimate.AddDetail("k8s_nodes", nodeCost)
		} else {
			estimate.Compute += GetK8sNodeCost(config.HALevel) * float64(nodeCount)
		}
	}

	// Database costs (using catalog storage pricing for estimation)
	for range categorized.Databases {
		cost := GetDBCost(config.HALevel)
		estimate.Database += cost
		estimate.AddDetail("database", cost)
	}

	// Redis costs
	for range categorized.Caches {
		cost := GetRedisCost(config.HALevel)
		estimate.Database += cost
		estimate.AddDetail("redis", cost)
	}

	// Storage costs using catalog pricing (0.06 EUR/GB/month)
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

	// Load balancer costs
	for range categorized.LoadBalancers {
		lbCost := GetLBCost(config.HALevel)
		estimate.Network += lbCost
		estimate.AddDetail("load_balancer", lbCost)
	}

	// Network/bandwidth estimate using catalog pricing
	// Scaleway: 75GB free egress, then 0.01 EUR/GB
	estimatedEgressGB := 500 // Estimate 500GB monthly egress
	egressCost := pricing.Network.EstimateEgressCost(estimatedEgressGB)
	estimate.Network += egressCost
	if egressCost > 0 {
		estimate.AddDetail("egress", egressCost)
	}

	estimate.Calculate()

	estimate.AddNote("Prices from Scaleway catalog (last updated: December 2024)")
	estimate.AddNote(fmt.Sprintf("Storage: %.2f EUR/GB/month", pricing.Storage.PricePerGBMonth))
	estimate.AddNote(fmt.Sprintf("Egress: First %dGB free, then %.2f EUR/GB", pricing.Network.FreeEgressGB, pricing.Network.EgressPricePerGB))
	estimate.AddNote("Kapsule control plane is free")

	return estimate, nil
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
		minVCPUs = 2
		minMemoryGB = 2
	case target.HALevelBasic:
		minVCPUs = 3
		minMemoryGB = 4
	case target.HALevelMultiServer:
		minVCPUs = 4
		minMemoryGB = 8
	case target.HALevelCluster:
		minVCPUs = 4
		minMemoryGB = 16
	default:
		minVCPUs = 2
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

// selectK8sNodeType selects the best K8s node type from the catalog.
func (g *Generator) selectK8sNodeType(level target.HALevel, pricing *provider.ProviderPricing) *provider.InstancePricing {
	// K8s nodes need slightly more resources
	switch level {
	case target.HALevelCluster:
		return g.selectInstanceType(target.HALevelCluster, pricing)
	case target.HALevelMultiServer:
		return g.selectInstanceType(target.HALevelMultiServer, pricing)
	default:
		return g.selectInstanceType(target.HALevelBasic, pricing)
	}
}

// estimateCostFallback uses hardcoded values when catalog is unavailable.
func (g *Generator) estimateCostFallback(results []*mapper.MappingResult, config *generator.TargetConfig) (*generator.CostEstimate, error) {
	estimate := generator.NewCostEstimate("EUR")
	categorized := g.categorizeResources(results)

	for range categorized.Compute {
		estimate.Compute += GetInstanceCost(config.HALevel)
	}
	for range categorized.Kubernetes {
		estimate.Compute += GetK8sNodeCost(config.HALevel) * float64(GetK8sNodeCount(config.HALevel))
	}
	for range categorized.Databases {
		estimate.Database += GetDBCost(config.HALevel)
	}
	for range categorized.Caches {
		estimate.Database += GetRedisCost(config.HALevel)
	}
	for range categorized.ObjectStorage {
		estimate.Storage += 6.0 // 100GB at 0.06 EUR/GB
	}
	for range categorized.BlockStorage {
		estimate.Storage += 3.0 // 50GB at 0.06 EUR/GB
	}
	for range categorized.LoadBalancers {
		estimate.Network += GetLBCost(config.HALevel)
	}

	estimate.Calculate()
	estimate.AddNote("Fallback pricing (catalog unavailable)")
	return estimate, nil
}

// generateSummary generates a summary of what was created.
func (g *Generator) generateSummary(categorized *CategorizedResources, config *generator.TargetConfig) string {
	var buf bytes.Buffer

	buf.WriteString(fmt.Sprintf("Scaleway Terraform Configuration\n"))
	buf.WriteString(fmt.Sprintf("Project: %s\n", config.ProjectName))
	buf.WriteString(fmt.Sprintf("HA Level: %s\n", config.HALevel.String()))
	buf.WriteString("\nResources:\n")

	counts := make(map[string]int)
	counts["Instances"] = len(categorized.Compute)
	counts["Kapsule Clusters"] = len(categorized.Kubernetes)
	counts["RDB Instances"] = len(categorized.Databases)
	counts["Redis Clusters"] = len(categorized.Caches)
	counts["Object Buckets"] = len(categorized.ObjectStorage)
	counts["Block Volumes"] = len(categorized.BlockStorage)
	counts["Load Balancers"] = len(categorized.LoadBalancers)
	counts["DNS Zones"] = len(categorized.DNS)

	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		if counts[k] > 0 {
			buf.WriteString(fmt.Sprintf("  - %s: %d\n", k, counts[k]))
		}
	}

	return buf.String()
}

// Helper functions

// SanitizeName converts a name to a valid lowercase name with hyphens.
func SanitizeName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.ReplaceAll(name, "_", "-")
	var result strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			result.WriteRune(r)
		}
	}
	return strings.Trim(result.String(), "-")
}

// SanitizeTFName converts a name to a valid Terraform resource name.
func SanitizeTFName(name string) string {
	if name == "" {
		return ""
	}
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ReplaceAll(name, "-", "_")
	var result strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			result.WriteRune(r)
		}
	}
	clean := strings.Trim(result.String(), "_")
	// Ensure starts with letter
	if len(clean) > 0 && clean[0] >= '0' && clean[0] <= '9' {
		clean = "r_" + clean
	}
	return clean
}

// GetInstanceType returns instance type based on HA level.
func GetInstanceType(level target.HALevel) string {
	switch level {
	case target.HALevelNone:
		return "DEV1-S"
	case target.HALevelBasic:
		return "DEV1-M"
	case target.HALevelMultiServer:
		return "GP1-XS"
	case target.HALevelCluster:
		return "GP1-S"
	default:
		return "DEV1-S"
	}
}

// GetInstanceCost returns instance cost in EUR/month.
func GetInstanceCost(level target.HALevel) float64 {
	switch level {
	case target.HALevelNone:
		return 4.99
	case target.HALevelBasic:
		return 9.99
	case target.HALevelMultiServer:
		return 19.99
	case target.HALevelCluster:
		return 39.99
	default:
		return 4.99
	}
}

// GetK8sNodeType returns Kapsule node type.
func GetK8sNodeType(level target.HALevel) string {
	switch level {
	case target.HALevelNone:
		return "DEV1-M"
	case target.HALevelBasic:
		return "DEV1-L"
	case target.HALevelMultiServer:
		return "GP1-XS"
	case target.HALevelCluster:
		return "GP1-S"
	default:
		return "DEV1-M"
	}
}

// GetK8sNodeCount returns default node count.
func GetK8sNodeCount(level target.HALevel) int {
	switch level {
	case target.HALevelNone:
		return 1
	case target.HALevelBasic:
		return 2
	case target.HALevelMultiServer:
		return 3
	case target.HALevelCluster:
		return 3
	default:
		return 1
	}
}

// GetK8sNodeCost returns node cost in EUR/month.
func GetK8sNodeCost(level target.HALevel) float64 {
	switch level {
	case target.HALevelNone:
		return 9.99
	case target.HALevelBasic:
		return 17.99
	case target.HALevelMultiServer:
		return 19.99
	case target.HALevelCluster:
		return 39.99
	default:
		return 9.99
	}
}

// GetDBNodeType returns database node type.
func GetDBNodeType(level target.HALevel) string {
	switch level {
	case target.HALevelNone:
		return "DB-DEV-S"
	case target.HALevelBasic:
		return "DB-DEV-M"
	case target.HALevelMultiServer:
		return "DB-GP-XS"
	case target.HALevelCluster:
		return "DB-GP-S"
	default:
		return "DB-DEV-S"
	}
}

// GetDBCost returns database cost in EUR/month.
func GetDBCost(level target.HALevel) float64 {
	switch level {
	case target.HALevelNone:
		return 10.0
	case target.HALevelBasic:
		return 20.0
	case target.HALevelMultiServer:
		return 80.0 // with HA
	case target.HALevelCluster:
		return 160.0 // with HA
	default:
		return 10.0
	}
}

// GetRedisNodeType returns Redis node type.
func GetRedisNodeType(level target.HALevel) string {
	switch level {
	case target.HALevelNone:
		return "RED1-MICRO"
	case target.HALevelBasic:
		return "RED1-XS"
	case target.HALevelMultiServer:
		return "RED1-S"
	case target.HALevelCluster:
		return "RED1-M"
	default:
		return "RED1-MICRO"
	}
}

// GetRedisClusterSize returns Redis cluster size.
func GetRedisClusterSize(level target.HALevel) int {
	switch level {
	case target.HALevelNone:
		return 1
	case target.HALevelBasic:
		return 1
	case target.HALevelMultiServer:
		return 3
	case target.HALevelCluster:
		return 3
	default:
		return 1
	}
}

// GetRedisCost returns Redis cost in EUR/month.
func GetRedisCost(level target.HALevel) float64 {
	switch level {
	case target.HALevelNone:
		return 5.0
	case target.HALevelBasic:
		return 10.0
	case target.HALevelMultiServer:
		return 60.0 // 3 nodes
	case target.HALevelCluster:
		return 120.0 // 3 nodes larger
	default:
		return 5.0
	}
}

// GetLBType returns load balancer type.
func GetLBType(level target.HALevel) string {
	switch level {
	case target.HALevelNone, target.HALevelBasic:
		return "LB-S"
	case target.HALevelMultiServer:
		return "LB-S"
	case target.HALevelCluster:
		return "LB-GP-M"
	default:
		return "LB-S"
	}
}

// GetLBCost returns LB cost in EUR/month.
func GetLBCost(level target.HALevel) float64 {
	switch level {
	case target.HALevelNone, target.HALevelBasic, target.HALevelMultiServer:
		return 10.0
	case target.HALevelCluster:
		return 30.0
	default:
		return 10.0
	}
}

func init() {
	generator.RegisterGenerator(New())
}
