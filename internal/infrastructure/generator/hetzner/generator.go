// Package hetzner generates Terraform configurations for Hetzner Cloud deployments.
// This generator converts mapping results to Terraform HCL that provisions
// infrastructure on Hetzner Cloud using the hcloud provider.
package hetzner

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

// Generator generates Terraform configurations for Hetzner Cloud.
type Generator struct {
	projectName string
}

// New creates a new Hetzner Terraform generator.
func New() *Generator {
	return &Generator{
		projectName: "homeport",
	}
}

// NewWithProject creates a new Hetzner Terraform generator with a project name.
func NewWithProject(projectName string) *Generator {
	return &Generator{
		projectName: projectName,
	}
}

// Platform returns the target platform this generator handles.
func (g *Generator) Platform() target.Platform {
	return target.PlatformHetzner
}

// Name returns the name of this generator.
func (g *Generator) Name() string {
	return "hetzner-terraform"
}

// Description returns a human-readable description.
func (g *Generator) Description() string {
	return "Generates Terraform configurations for Hetzner Cloud (EU-based, cost-effective)"
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
	return []string{"HCLOUD_TOKEN"}
}

// Validate checks if the mapping results can be deployed to this platform.
func (g *Generator) Validate(results []*mapper.MappingResult, config *generator.TargetConfig) error {
	if len(results) == 0 {
		return fmt.Errorf("no mapping results provided")
	}

	// Check for unsupported HA levels
	supported := false
	for _, level := range g.SupportedHALevels() {
		if config.HALevel == level {
			supported = true
			break
		}
	}
	if !supported {
		return fmt.Errorf("HA level %s is not supported by Hetzner generator", config.HALevel)
	}

	return nil
}

// CategorizedResults holds resources grouped by category.
type CategorizedResults struct {
	Compute   []*mapper.MappingResult
	Storage   []*mapper.MappingResult
	Database  []*mapper.MappingResult
	Messaging []*mapper.MappingResult
	Network   []*mapper.MappingResult
	Security  []*mapper.MappingResult
}

// CategorizeResults groups mapping results by resource category.
func CategorizeResults(results []*mapper.MappingResult) *CategorizedResults {
	categorized := &CategorizedResults{
		Compute:   make([]*mapper.MappingResult, 0),
		Storage:   make([]*mapper.MappingResult, 0),
		Database:  make([]*mapper.MappingResult, 0),
		Messaging: make([]*mapper.MappingResult, 0),
		Network:   make([]*mapper.MappingResult, 0),
		Security:  make([]*mapper.MappingResult, 0),
	}

	for _, result := range results {
		if result == nil {
			continue
		}

		category := result.SourceCategory
		switch category {
		case resource.CategoryCompute, resource.CategoryContainer, resource.CategoryServerless, resource.CategoryKubernetes:
			categorized.Compute = append(categorized.Compute, result)
		case resource.CategoryObjectStorage, resource.CategoryBlockStorage, resource.CategoryFileStorage:
			categorized.Storage = append(categorized.Storage, result)
		case resource.CategorySQLDatabase, resource.CategoryNoSQLDatabase, resource.CategoryCache:
			categorized.Database = append(categorized.Database, result)
		case resource.CategoryMessaging:
			categorized.Messaging = append(categorized.Messaging, result)
		case resource.CategoryNetworking, resource.CategoryCDN, resource.CategoryDNS:
			categorized.Network = append(categorized.Network, result)
		case resource.CategorySecurity, resource.CategoryIdentity:
			categorized.Security = append(categorized.Security, result)
		default:
			// Default to compute for unknown categories
			categorized.Compute = append(categorized.Compute, result)
		}
	}

	return categorized
}

// GetServerCount returns the number of servers based on HA level.
func GetServerCount(haLevel target.HALevel) int {
	switch haLevel {
	case target.HALevelNone, target.HALevelBasic:
		return 1
	case target.HALevelMultiServer:
		return 2
	case target.HALevelCluster:
		return 3
	default:
		return 1
	}
}

// GetLocation returns the Hetzner location from config.
func GetLocation(config *generator.TargetConfig) string {
	if config.TargetConfig != nil && config.TargetConfig.Hetzner != nil && config.TargetConfig.Hetzner.Location != "" {
		return config.TargetConfig.Hetzner.Location
	}
	return "fsn1" // Default to Falkenstein, Germany
}

// Generate produces output artifacts for Hetzner Cloud.
func (g *Generator) Generate(ctx context.Context, results []*mapper.MappingResult, config *generator.TargetConfig) (*generator.TargetOutput, error) {
	if err := g.Validate(results, config); err != nil {
		return nil, err
	}

	output := generator.NewTargetOutput(target.PlatformHetzner)
	output.GeneratedAt = time.Now()

	// Get location
	location := GetLocation(config)

	// Categorize resources
	categorized := CategorizeResults(results)

	// Generate main.tf - provider configuration
	mainTF := g.generateMainTF(config, location)
	output.AddTerraformFile("main.tf", []byte(mainTF))
	output.MainFile = "main.tf"

	// Generate variables.tf
	variablesTF := g.generateVariablesTF(config, results)
	output.AddTerraformFile("variables.tf", []byte(variablesTF))

	// Generate terraform.tfvars.example
	tfvarsExample := g.generateTFVarsExample(config, location, results)
	output.AddTerraformFile("terraform.tfvars.example", []byte(tfvarsExample))

	// Generate networking.tf
	networkingTF := GenerateNetworkingTF(categorized, config, location)
	output.AddTerraformFile("networking.tf", []byte(networkingTF))

	// Generate compute.tf
	computeTF := GenerateComputeTF(categorized, config, location)
	output.AddTerraformFile("compute.tf", []byte(computeTF))

	// Generate storage.tf
	storageTF := GenerateStorageTF(categorized, config, location)
	output.AddTerraformFile("storage.tf", []byte(storageTF))

	// Generate database.tf (cloud-init based)
	databaseTF := GenerateDatabaseTF(categorized, config)
	output.AddTerraformFile("database.tf", []byte(databaseTF))

	// Generate cloud-init.tf
	cloudInitTF := GenerateCloudInitTF(categorized, config)
	output.AddTerraformFile("cloud-init.tf", []byte(cloudInitTF))

	// Generate outputs.tf
	outputsTF := g.generateOutputsTF(config)
	output.AddTerraformFile("outputs.tf", []byte(outputsTF))

	// Add deployment script
	deployScript := g.generateDeployScript(config)
	output.AddScript("deploy.sh", []byte(deployScript))

	// Add warnings and manual steps
	g.addWarningsAndManualSteps(output, categorized, config)

	// Estimate costs
	costEstimate, err := g.EstimateCost(results, config)
	if err == nil {
		output.EstimatedCost = costEstimate
	}

	// Generate summary
	output.Summary = g.generateSummary(categorized, config, costEstimate)

	return output, nil
}

// EstimateCost estimates the monthly cost for the deployment.
func (g *Generator) EstimateCost(results []*mapper.MappingResult, config *generator.TargetConfig) (*generator.CostEstimate, error) {
	estimate := generator.NewCostEstimate("EUR")

	categorized := CategorizeResults(results)
	serverCount := GetServerCount(config.HALevel)

	// Get pricing data from the catalog
	pricing := provider.GetProviderPricing(provider.ProviderHetzner)
	if pricing == nil {
		return nil, fmt.Errorf("hetzner pricing data not available")
	}

	// Extract resource requirements from mapping results
	requirements := provider.ExtractRequirements(results)

	// Select appropriate instance type based on requirements
	selectedInstance := provider.SelectInstance(provider.ProviderHetzner, requirements)
	if selectedInstance == nil {
		// Fallback to CX21 if no suitable instance found
		selectedInstance = provider.FindInstance(provider.ProviderHetzner, "cx21")
	}

	// Calculate compute costs using catalog pricing
	var serverCost float64
	if selectedInstance != nil {
		serverCost = selectedInstance.PricePerMonth * float64(serverCount)
		estimate.Compute = serverCost
		estimate.AddDetail("servers", serverCost)
		estimate.AddNote(fmt.Sprintf("%d x %s server(s) @ %.2f EUR each (%d vCPU, %.0fGB RAM)",
			serverCount, selectedInstance.Type, selectedInstance.PricePerMonth,
			selectedInstance.VCPUs, selectedInstance.MemoryGB))
	}

	// Calculate storage costs using catalog rate
	storageCost := 0.0
	additionalStorageGB := 0

	// Storage from mapped resources
	for range categorized.Storage {
		additionalStorageGB += 50 // Estimate 50GB per storage resource
	}

	// Additional storage per server for system/app data
	if serverCount > 0 {
		additionalStorageGB += serverCount * 20 // 20GB per server
	}

	// Database backup storage
	for range categorized.Database {
		additionalStorageGB += 10 // 10GB backup storage per DB
	}

	// Use catalog pricing for storage
	storageCost = pricing.Storage.EstimateStorageCost(additionalStorageGB)
	estimate.Storage = storageCost
	estimate.AddDetail("volumes", storageCost)
	estimate.AddNote(fmt.Sprintf("%dGB additional storage @ %.3f EUR/GB/mo", additionalStorageGB, pricing.Storage.PricePerGBMonth))

	// Calculate database costs (included in compute, but track separately for breakdown)
	dbCost := 0.0
	if len(categorized.Database) > 0 {
		// Database container overhead (memory/CPU already accounted in instance selection)
		dbCost = 0 // Containers don't have separate cost
	}
	estimate.Database = dbCost
	if len(categorized.Database) > 0 {
		estimate.AddNote(fmt.Sprintf("%d database(s) running as Docker containers (included in compute)", len(categorized.Database)))
	}

	// Calculate networking costs
	networkCost := 0.0

	// Load balancer: LB11 pricing
	if config.HALevel.RequiresMultiServer() {
		lbCost := 5.49 // LB11 price
		networkCost += lbCost
		estimate.AddNote("Load balancer LB11 @ 5.49 EUR/mo")
	}

	// Floating IP for HA
	if config.HALevel.RequiresMultiServer() {
		fipCost := 4.00
		networkCost += fipCost
		estimate.AddNote("Floating IP @ 4.00 EUR/mo")
	}

	// Estimate egress - assume moderate traffic, calculate using catalog pricing
	estimatedEgressGB := 100 * serverCount // 100GB per server estimate
	egressCost := pricing.Network.EstimateEgressCost(estimatedEgressGB)
	if egressCost > 0 {
		networkCost += egressCost
		estimate.AddNote(fmt.Sprintf("Estimated egress: %dGB (first %dGB free)", estimatedEgressGB, pricing.Network.FreeEgressGB))
	} else {
		estimate.AddNote(fmt.Sprintf("Network: %dGB free egress included", pricing.Network.FreeEgressGB))
	}

	estimate.Network = networkCost
	estimate.AddDetail("networking", networkCost)

	// Calculate total
	estimate.Calculate()

	// Add summary notes
	estimate.AddNote("Prices based on Hetzner Cloud catalog pricing (EU region)")
	estimate.AddNote(fmt.Sprintf("Resource requirements: %s", requirements.Description))

	return estimate, nil
}

// generateMainTF generates the main.tf with provider configuration.
func (g *Generator) generateMainTF(config *generator.TargetConfig, location string) string {
	var buf bytes.Buffer

	buf.WriteString("# Terraform configuration for Hetzner Cloud\n")
	buf.WriteString(fmt.Sprintf("# Generated by Homeport - %s\n", time.Now().Format(time.RFC3339)))
	buf.WriteString(fmt.Sprintf("# Project: %s\n\n", config.ProjectName))

	buf.WriteString("terraform {\n")
	buf.WriteString("  required_version = \">= 1.0.0\"\n\n")
	buf.WriteString("  required_providers {\n")
	buf.WriteString("    hcloud = {\n")
	buf.WriteString("      source  = \"hetznercloud/hcloud\"\n")
	buf.WriteString("      version = \"~> 1.45\"\n")
	buf.WriteString("    }\n")
	buf.WriteString("    template = {\n")
	buf.WriteString("      source  = \"hashicorp/template\"\n")
	buf.WriteString("      version = \"~> 2.2\"\n")
	buf.WriteString("    }\n")
	buf.WriteString("  }\n\n")
	buf.WriteString("  # Uncomment to use remote state\n")
	buf.WriteString("  # backend \"s3\" {\n")
	buf.WriteString("  #   bucket                      = \"your-terraform-state-bucket\"\n")
	buf.WriteString(fmt.Sprintf("  #   key                         = \"%s/terraform.tfstate\"\n", config.ProjectName))
	buf.WriteString("  #   region                      = \"eu-central-1\"\n")
	buf.WriteString("  #   endpoint                    = \"https://fsn1.your-objectstorage.com\"\n")
	buf.WriteString("  #   skip_credentials_validation = true\n")
	buf.WriteString("  #   skip_metadata_api_check     = true\n")
	buf.WriteString("  # }\n")
	buf.WriteString("}\n\n")

	buf.WriteString("provider \"hcloud\" {\n")
	buf.WriteString("  token = var.hcloud_token\n")
	buf.WriteString("}\n\n")

	buf.WriteString("# Local variables\n")
	buf.WriteString("locals {\n")
	buf.WriteString(fmt.Sprintf("  project_name = \"%s\"\n", config.ProjectName))
	buf.WriteString(fmt.Sprintf("  location     = \"%s\"\n", location))
	buf.WriteString("  common_labels = {\n")
	buf.WriteString("    project     = local.project_name\n")
	buf.WriteString("    managed_by  = \"terraform\"\n")
	buf.WriteString("    environment = var.environment\n")
	buf.WriteString("  }\n")
	buf.WriteString("}\n\n")

	buf.WriteString("# SSH Key resource\n")
	buf.WriteString("resource \"hcloud_ssh_key\" \"default\" {\n")
	buf.WriteString("  name       = \"${local.project_name}-key\"\n")
	buf.WriteString("  public_key = var.ssh_public_key\n")
	buf.WriteString("}\n")

	return buf.String()
}

// generateVariablesTF generates the variables.tf file.
func (g *Generator) generateVariablesTF(config *generator.TargetConfig, results []*mapper.MappingResult) string {
	var buf bytes.Buffer

	// Extract requirements and select appropriate instance type
	requirements := provider.ExtractRequirements(results)
	selectedInstance := provider.SelectInstance(provider.ProviderHetzner, requirements)
	defaultServerType := "cx21" // Fallback default
	if selectedInstance != nil {
		defaultServerType = selectedInstance.Type
	}

	buf.WriteString("# Terraform variables for Hetzner Cloud deployment\n\n")

	buf.WriteString("variable \"hcloud_token\" {\n")
	buf.WriteString("  description = \"Hetzner Cloud API token\"\n")
	buf.WriteString("  type        = string\n")
	buf.WriteString("  sensitive   = true\n")
	buf.WriteString("}\n\n")

	buf.WriteString("variable \"location\" {\n")
	buf.WriteString("  description = \"Hetzner datacenter location (fsn1, nbg1, hel1, ash)\"\n")
	buf.WriteString("  type        = string\n")
	buf.WriteString("  default     = \"fsn1\"\n\n")
	buf.WriteString("  validation {\n")
	buf.WriteString("    condition     = contains([\"fsn1\", \"nbg1\", \"hel1\", \"ash\"], var.location)\n")
	buf.WriteString("    error_message = \"Location must be one of: fsn1, nbg1, hel1, ash.\"\n")
	buf.WriteString("  }\n")
	buf.WriteString("}\n\n")

	buf.WriteString("variable \"environment\" {\n")
	buf.WriteString("  description = \"Environment name (dev, staging, prod)\"\n")
	buf.WriteString("  type        = string\n")
	buf.WriteString("  default     = \"prod\"\n")
	buf.WriteString("}\n\n")

	buf.WriteString("variable \"server_type\" {\n")
	buf.WriteString("  description = \"Hetzner server type (auto-selected based on resource requirements)\"\n")
	buf.WriteString("  type        = string\n")
	buf.WriteString(fmt.Sprintf("  default     = \"%s\"\n", defaultServerType))
	buf.WriteString("}\n\n")

	buf.WriteString("variable \"ssh_public_key\" {\n")
	buf.WriteString("  description = \"SSH public key for server access\"\n")
	buf.WriteString("  type        = string\n")
	buf.WriteString("}\n\n")

	buf.WriteString("variable \"ssh_private_key_path\" {\n")
	buf.WriteString("  description = \"Path to SSH private key for provisioning\"\n")
	buf.WriteString("  type        = string\n")
	buf.WriteString("  default     = \"~/.ssh/id_rsa\"\n")
	buf.WriteString("}\n\n")

	buf.WriteString("variable \"domain\" {\n")
	buf.WriteString("  description = \"Domain name for the deployment\"\n")
	buf.WriteString("  type        = string\n")
	buf.WriteString("  default     = \"\"\n")
	buf.WriteString("}\n\n")

	buf.WriteString("variable \"email\" {\n")
	buf.WriteString("  description = \"Email for Let's Encrypt certificates\"\n")
	buf.WriteString("  type        = string\n")
	buf.WriteString("  default     = \"\"\n")
	buf.WriteString("}\n\n")

	// Database variables
	buf.WriteString("# Database configuration\n")
	buf.WriteString("variable \"db_password\" {\n")
	buf.WriteString("  description = \"Database password\"\n")
	buf.WriteString("  type        = string\n")
	buf.WriteString("  sensitive   = true\n")
	buf.WriteString("  default     = \"\"\n")
	buf.WriteString("}\n\n")

	buf.WriteString("variable \"redis_password\" {\n")
	buf.WriteString("  description = \"Redis password\"\n")
	buf.WriteString("  type        = string\n")
	buf.WriteString("  sensitive   = true\n")
	buf.WriteString("  default     = \"\"\n")
	buf.WriteString("}\n\n")

	// HA configuration
	serverCount := GetServerCount(config.HALevel)
	buf.WriteString("# High Availability configuration\n")
	buf.WriteString("variable \"server_count\" {\n")
	buf.WriteString("  description = \"Number of servers (1 for single, 2+ for HA)\"\n")
	buf.WriteString("  type        = number\n")
	buf.WriteString(fmt.Sprintf("  default     = %d\n", serverCount))
	buf.WriteString("}\n\n")

	buf.WriteString("variable \"enable_load_balancer\" {\n")
	buf.WriteString("  description = \"Enable Hetzner load balancer\"\n")
	buf.WriteString("  type        = bool\n")
	buf.WriteString(fmt.Sprintf("  default     = %t\n", config.HALevel.RequiresMultiServer()))
	buf.WriteString("}\n\n")

	buf.WriteString("variable \"enable_floating_ip\" {\n")
	buf.WriteString("  description = \"Enable floating IP for failover\"\n")
	buf.WriteString("  type        = bool\n")
	buf.WriteString(fmt.Sprintf("  default     = %t\n", config.HALevel.RequiresMultiServer()))
	buf.WriteString("}\n")

	return buf.String()
}

// generateTFVarsExample generates the terraform.tfvars.example file.
func (g *Generator) generateTFVarsExample(config *generator.TargetConfig, location string, results []*mapper.MappingResult) string {
	var buf bytes.Buffer

	// Extract requirements and select appropriate instance type
	requirements := provider.ExtractRequirements(results)
	selectedInstance := provider.SelectInstance(provider.ProviderHetzner, requirements)
	defaultServerType := "cx21" // Fallback default
	if selectedInstance != nil {
		defaultServerType = selectedInstance.Type
	}

	buf.WriteString("# Hetzner Cloud Terraform variables\n")
	buf.WriteString("# Copy this file to terraform.tfvars and fill in your values\n\n")

	buf.WriteString("# Required: Hetzner Cloud API token\n")
	buf.WriteString("# Get it from: https://console.hetzner.cloud/projects/*/security/tokens\n")
	buf.WriteString("hcloud_token = \"your-hcloud-api-token\"\n\n")

	buf.WriteString("# Datacenter location\n")
	buf.WriteString("# fsn1 = Falkenstein, Germany\n")
	buf.WriteString("# nbg1 = Nuremberg, Germany\n")
	buf.WriteString("# hel1 = Helsinki, Finland\n")
	buf.WriteString("# ash  = Ashburn, USA\n")
	buf.WriteString(fmt.Sprintf("location = \"%s\"\n\n", location))

	buf.WriteString("# Environment\n")
	buf.WriteString("environment = \"prod\"\n\n")

	// Generate server type options with pricing from catalog
	pricing := provider.GetProviderPricing(provider.ProviderHetzner)
	buf.WriteString("# Server type (https://www.hetzner.com/cloud)\n")
	buf.WriteString("# Auto-selected based on resource requirements\n")
	if pricing != nil {
		for _, inst := range pricing.Instances {
			marker := ""
			if inst.Type == defaultServerType {
				marker = " <-- RECOMMENDED"
			}
			buf.WriteString(fmt.Sprintf("# %s = %d vCPU, %.0fGB RAM (~%.2f EUR/month)%s\n",
				inst.Type, inst.VCPUs, inst.MemoryGB, inst.PricePerMonth, marker))
		}
	}
	buf.WriteString(fmt.Sprintf("server_type = \"%s\"\n\n", defaultServerType))

	buf.WriteString("# SSH public key for server access\n")
	buf.WriteString("ssh_public_key = \"ssh-ed25519 AAAA... your-key\"\n\n")

	buf.WriteString("# Path to SSH private key (for provisioning)\n")
	buf.WriteString("ssh_private_key_path = \"~/.ssh/id_ed25519\"\n\n")

	buf.WriteString("# Domain name (optional, for Traefik SSL)\n")
	buf.WriteString(fmt.Sprintf("domain = \"%s.example.com\"\n\n", config.ProjectName))

	buf.WriteString("# Email for Let's Encrypt (required if domain is set)\n")
	buf.WriteString("email = \"admin@example.com\"\n\n")

	buf.WriteString("# Database password (auto-generated if empty)\n")
	buf.WriteString("db_password = \"\"\n\n")

	buf.WriteString("# Redis password (auto-generated if empty)\n")
	buf.WriteString("redis_password = \"\"\n\n")

	serverCount := GetServerCount(config.HALevel)
	buf.WriteString(fmt.Sprintf("# Number of servers (default: %d)\n", serverCount))
	buf.WriteString(fmt.Sprintf("server_count = %d\n\n", serverCount))

	buf.WriteString("# High Availability options\n")
	buf.WriteString(fmt.Sprintf("enable_load_balancer = %t\n", config.HALevel.RequiresMultiServer()))
	buf.WriteString(fmt.Sprintf("enable_floating_ip = %t\n", config.HALevel.RequiresMultiServer()))

	return buf.String()
}

// generateOutputsTF generates the outputs.tf file.
func (g *Generator) generateOutputsTF(config *generator.TargetConfig) string {
	var buf bytes.Buffer

	buf.WriteString("# Terraform outputs for Hetzner Cloud deployment\n\n")

	buf.WriteString("output \"server_ips\" {\n")
	buf.WriteString("  description = \"Public IP addresses of all servers\"\n")
	buf.WriteString("  value       = hcloud_server.app[*].ipv4_address\n")
	buf.WriteString("}\n\n")

	buf.WriteString("output \"server_names\" {\n")
	buf.WriteString("  description = \"Names of all servers\"\n")
	buf.WriteString("  value       = hcloud_server.app[*].name\n")
	buf.WriteString("}\n\n")

	buf.WriteString("output \"private_network_ips\" {\n")
	buf.WriteString("  description = \"Private network IPs\"\n")
	buf.WriteString("  value       = [for s in hcloud_server_network.app : s.ip]\n")
	buf.WriteString("}\n\n")

	if config.HALevel.RequiresMultiServer() {
		buf.WriteString("output \"load_balancer_ip\" {\n")
		buf.WriteString("  description = \"Load balancer public IP\"\n")
		buf.WriteString("  value       = var.enable_load_balancer ? hcloud_load_balancer.main[0].ipv4 : null\n")
		buf.WriteString("}\n\n")

		buf.WriteString("output \"floating_ip\" {\n")
		buf.WriteString("  description = \"Floating IP for failover\"\n")
		buf.WriteString("  value       = var.enable_floating_ip ? hcloud_floating_ip.main[0].ip_address : null\n")
		buf.WriteString("}\n\n")
	}

	buf.WriteString("output \"ssh_command\" {\n")
	buf.WriteString("  description = \"SSH command to connect to first server\"\n")
	buf.WriteString("  value       = \"ssh root@${hcloud_server.app[0].ipv4_address}\"\n")
	buf.WriteString("}\n\n")

	buf.WriteString("output \"app_url\" {\n")
	buf.WriteString("  description = \"Application URL\"\n")
	buf.WriteString("  value       = var.domain != \"\" ? \"https://${var.domain}\" : \"http://${hcloud_server.app[0].ipv4_address}\"\n")
	buf.WriteString("}\n\n")

	buf.WriteString("output \"network_id\" {\n")
	buf.WriteString("  description = \"Private network ID\"\n")
	buf.WriteString("  value       = hcloud_network.main.id\n")
	buf.WriteString("}\n")

	return buf.String()
}

// generateDeployScript generates the deploy.sh script.
func (g *Generator) generateDeployScript(config *generator.TargetConfig) string {
	var buf bytes.Buffer

	buf.WriteString("#!/bin/bash\n")
	buf.WriteString("# Deployment script for Hetzner Cloud\n")
	buf.WriteString(fmt.Sprintf("# Generated by Homeport - %s\n\n", time.Now().Format(time.RFC3339)))

	buf.WriteString("set -euo pipefail\n\n")

	buf.WriteString("# Colors for output\n")
	buf.WriteString("RED='\\033[0;31m'\n")
	buf.WriteString("GREEN='\\033[0;32m'\n")
	buf.WriteString("YELLOW='\\033[1;33m'\n")
	buf.WriteString("NC='\\033[0m' # No Color\n\n")

	buf.WriteString("echo -e \"${GREEN}Hetzner Cloud Deployment Script${NC}\"\n")
	buf.WriteString("echo \"=================================\"\n\n")

	buf.WriteString("# Check prerequisites\n")
	buf.WriteString("check_prerequisites() {\n")
	buf.WriteString("    echo -e \"${YELLOW}Checking prerequisites...${NC}\"\n")
	buf.WriteString("    \n")
	buf.WriteString("    if ! command -v terraform &> /dev/null; then\n")
	buf.WriteString("        echo -e \"${RED}Error: terraform is not installed${NC}\"\n")
	buf.WriteString("        echo \"Install from: https://www.terraform.io/downloads\"\n")
	buf.WriteString("        exit 1\n")
	buf.WriteString("    fi\n")
	buf.WriteString("    \n")
	buf.WriteString("    if [ ! -f terraform.tfvars ]; then\n")
	buf.WriteString("        echo -e \"${YELLOW}Warning: terraform.tfvars not found${NC}\"\n")
	buf.WriteString("        echo \"Copy terraform.tfvars.example to terraform.tfvars and configure it.\"\n")
	buf.WriteString("        exit 1\n")
	buf.WriteString("    fi\n")
	buf.WriteString("    \n")
	buf.WriteString("    echo -e \"${GREEN}All prerequisites met!${NC}\"\n")
	buf.WriteString("}\n\n")

	buf.WriteString("# Initialize Terraform\n")
	buf.WriteString("init() {\n")
	buf.WriteString("    echo -e \"${YELLOW}Initializing Terraform...${NC}\"\n")
	buf.WriteString("    terraform init\n")
	buf.WriteString("}\n\n")

	buf.WriteString("# Plan deployment\n")
	buf.WriteString("plan() {\n")
	buf.WriteString("    echo -e \"${YELLOW}Planning deployment...${NC}\"\n")
	buf.WriteString("    terraform plan -out=tfplan\n")
	buf.WriteString("}\n\n")

	buf.WriteString("# Apply deployment\n")
	buf.WriteString("apply() {\n")
	buf.WriteString("    echo -e \"${YELLOW}Applying deployment...${NC}\"\n")
	buf.WriteString("    terraform apply tfplan\n")
	buf.WriteString("    rm -f tfplan\n")
	buf.WriteString("}\n\n")

	buf.WriteString("# Destroy infrastructure\n")
	buf.WriteString("destroy() {\n")
	buf.WriteString("    echo -e \"${RED}WARNING: This will destroy all infrastructure!${NC}\"\n")
	buf.WriteString("    read -p \"Are you sure? (yes/no): \" confirm\n")
	buf.WriteString("    if [ \"$confirm\" = \"yes\" ]; then\n")
	buf.WriteString("        terraform destroy -auto-approve\n")
	buf.WriteString("    else\n")
	buf.WriteString("        echo \"Cancelled.\"\n")
	buf.WriteString("    fi\n")
	buf.WriteString("}\n\n")

	buf.WriteString("# Show outputs\n")
	buf.WriteString("outputs() {\n")
	buf.WriteString("    terraform output\n")
	buf.WriteString("}\n\n")

	buf.WriteString("# Main\n")
	buf.WriteString("case \"${1:-deploy}\" in\n")
	buf.WriteString("    init)\n")
	buf.WriteString("        check_prerequisites\n")
	buf.WriteString("        init\n")
	buf.WriteString("        ;;\n")
	buf.WriteString("    plan)\n")
	buf.WriteString("        check_prerequisites\n")
	buf.WriteString("        plan\n")
	buf.WriteString("        ;;\n")
	buf.WriteString("    apply)\n")
	buf.WriteString("        apply\n")
	buf.WriteString("        ;;\n")
	buf.WriteString("    deploy)\n")
	buf.WriteString("        check_prerequisites\n")
	buf.WriteString("        init\n")
	buf.WriteString("        plan\n")
	buf.WriteString("        apply\n")
	buf.WriteString("        echo -e \"\\n${GREEN}Deployment complete!${NC}\"\n")
	buf.WriteString("        outputs\n")
	buf.WriteString("        ;;\n")
	buf.WriteString("    destroy)\n")
	buf.WriteString("        destroy\n")
	buf.WriteString("        ;;\n")
	buf.WriteString("    outputs)\n")
	buf.WriteString("        outputs\n")
	buf.WriteString("        ;;\n")
	buf.WriteString("    *)\n")
	buf.WriteString("        echo \"Usage: $0 {init|plan|apply|deploy|destroy|outputs}\"\n")
	buf.WriteString("        exit 1\n")
	buf.WriteString("        ;;\n")
	buf.WriteString("esac\n")

	return buf.String()
}

// addWarningsAndManualSteps adds warnings and manual steps to the output.
func (g *Generator) addWarningsAndManualSteps(output *generator.TargetOutput, categorized *CategorizedResults, config *generator.TargetConfig) {
	// Add warnings for unsupported features
	if len(categorized.Messaging) > 0 {
		output.AddWarning("Messaging services (SQS, SNS, etc.) have no direct Hetzner equivalent. Using RabbitMQ or Redis Pub/Sub containers.")
	}

	if len(categorized.Security) > 0 {
		output.AddWarning("Security services (IAM, KMS, etc.) are not directly available. Use container-based alternatives like Vault.")
	}

	// Add manual steps
	output.AddManualStep("1. Copy terraform.tfvars.example to terraform.tfvars and configure your values")
	output.AddManualStep("2. Ensure your SSH public key is added to terraform.tfvars")
	output.AddManualStep("3. Run: chmod +x deploy.sh && ./deploy.sh")

	if config.SSLEnabled && config.BaseURL != "" {
		output.AddManualStep("4. Configure DNS to point your domain to the server/load balancer IP")
		output.AddManualStep("5. Traefik will automatically obtain SSL certificates from Let's Encrypt")
	}

	if config.HALevel.RequiresMultiServer() {
		output.AddManualStep("6. Verify load balancer health checks are passing")
		output.AddManualStep("7. Test failover by stopping one server")
	}

	if config.IncludeBackups {
		output.AddManualStep("8. Configure offsite backup destination (S3-compatible storage recommended)")
	}
}

// generateSummary generates a human-readable summary.
func (g *Generator) generateSummary(categorized *CategorizedResults, config *generator.TargetConfig, cost *generator.CostEstimate) string {
	var buf bytes.Buffer

	buf.WriteString(fmt.Sprintf("Hetzner Cloud Terraform Configuration for %s\n", config.ProjectName))
	buf.WriteString(strings.Repeat("=", len(config.ProjectName)+46) + "\n\n")

	buf.WriteString("Resources:\n")
	buf.WriteString(fmt.Sprintf("  - Compute: %d resources\n", len(categorized.Compute)))
	buf.WriteString(fmt.Sprintf("  - Storage: %d resources\n", len(categorized.Storage)))
	buf.WriteString(fmt.Sprintf("  - Database: %d resources\n", len(categorized.Database)))
	buf.WriteString(fmt.Sprintf("  - Network: %d resources\n", len(categorized.Network)))
	buf.WriteString(fmt.Sprintf("  - Security: %d resources\n", len(categorized.Security)))
	buf.WriteString(fmt.Sprintf("  - Messaging: %d resources\n\n", len(categorized.Messaging)))

	buf.WriteString(fmt.Sprintf("HA Level: %s\n", config.HALevel))
	buf.WriteString(fmt.Sprintf("Server Count: %d\n\n", GetServerCount(config.HALevel)))

	if cost != nil {
		buf.WriteString(fmt.Sprintf("Estimated Monthly Cost: %.2f %s\n", cost.Total, cost.Currency))
		buf.WriteString("  Breakdown:\n")
		// Sort details for consistent output
		keys := make([]string, 0, len(cost.Details))
		for k := range cost.Details {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			buf.WriteString(fmt.Sprintf("    - %s: %.2f %s\n", k, cost.Details[k], cost.Currency))
		}
	}

	return buf.String()
}

// SanitizeName sanitizes a resource name for Terraform use.
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

// init registers the generator when the package is imported.
func init() {
	generator.RegisterGenerator(New())
}
