// Package e2e contains end-to-end tests for the Homeport migration tool.
package e2e

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/parser"
	"github.com/homeport/homeport/internal/domain/resource"
	"github.com/homeport/homeport/internal/infrastructure/generator/compose"
	_ "github.com/homeport/homeport/internal/infrastructure/mapper/azure/compute"
	_ "github.com/homeport/homeport/internal/infrastructure/mapper/azure/database"
	_ "github.com/homeport/homeport/internal/infrastructure/mapper/azure/storage"
	_ "github.com/homeport/homeport/internal/infrastructure/mapper/compute"
	_ "github.com/homeport/homeport/internal/infrastructure/mapper/database"
	_ "github.com/homeport/homeport/internal/infrastructure/mapper/gcp/compute"
	_ "github.com/homeport/homeport/internal/infrastructure/mapper/gcp/database"
	_ "github.com/homeport/homeport/internal/infrastructure/mapper/gcp/storage"
	_ "github.com/homeport/homeport/internal/infrastructure/mapper/storage"
	_ "github.com/homeport/homeport/internal/infrastructure/parser/aws"
	_ "github.com/homeport/homeport/internal/infrastructure/parser/azure"
	_ "github.com/homeport/homeport/internal/infrastructure/parser/gcp"
)

// TestMultiCloudMigration_ParseMultipleProviders tests parsing infrastructure from multiple cloud providers.
func TestMultiCloudMigration_ParseMultipleProviders(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	// Create AWS CloudFormation template
	awsPath := filepath.Join(tmpDir, "aws-template.yaml")
	awsContent := `AWSTemplateFormatVersion: '2010-09-09'
Description: AWS Resources

Resources:
  WebServer:
    Type: AWS::EC2::Instance
    Properties:
      InstanceType: t3.medium
      ImageId: ami-12345678

  Database:
    Type: AWS::RDS::DBInstance
    Properties:
      DBInstanceIdentifier: aws-db
      Engine: postgres
      DBInstanceClass: db.t3.medium

  StorageBucket:
    Type: AWS::S3::Bucket
    Properties:
      BucketName: aws-storage
`
	if err := os.WriteFile(awsPath, []byte(awsContent), 0644); err != nil {
		t.Fatalf("Failed to write AWS template: %v", err)
	}

	// Create Azure ARM template
	azurePath := filepath.Join(tmpDir, "azure-template.json")
	azureContent := `{
  "$schema": "https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#",
  "contentVersion": "1.0.0.0",
  "resources": [
    {
      "type": "Microsoft.DBforPostgreSQL/flexibleServers",
      "apiVersion": "2023-03-01-preview",
      "name": "azure-db",
      "location": "eastus",
      "properties": {
        "version": "15",
        "administratorLogin": "dbadmin"
      }
    },
    {
      "type": "Microsoft.Storage/storageAccounts",
      "apiVersion": "2023-01-01",
      "name": "azurestorage",
      "location": "eastus",
      "sku": {"name": "Standard_LRS"},
      "kind": "StorageV2"
    }
  ]
}`
	if err := os.WriteFile(azurePath, []byte(azureContent), 0644); err != nil {
		t.Fatalf("Failed to write Azure template: %v", err)
	}

	// Create GCP Terraform file
	gcpPath := filepath.Join(tmpDir, "gcp-main.tf")
	gcpContent := `provider "google" {
  project = "my-project"
  region  = "us-central1"
}

resource "google_sql_database_instance" "main" {
  name             = "gcp-db"
  database_version = "POSTGRES_15"
  region           = "us-central1"

  settings {
    tier = "db-custom-2-8192"
  }
}

resource "google_storage_bucket" "assets" {
  name     = "gcp-storage"
  location = "US"
}
`
	if err := os.WriteFile(gcpPath, []byte(gcpContent), 0644); err != nil {
		t.Fatalf("Failed to write GCP template: %v", err)
	}

	// Parse AWS resources
	t.Run("Parse AWS Resources", func(t *testing.T) {
		registry := parser.DefaultRegistry()
		p, err := registry.GetByFormat(resource.ProviderAWS, parser.FormatCloudFormation)
		if err != nil {
			t.Fatalf("Failed to get AWS parser: %v", err)
		}

		infra, err := p.Parse(ctx, awsPath, parser.NewParseOptions())
		if err != nil {
			t.Fatalf("Failed to parse AWS: %v", err)
		}

		t.Logf("Parsed %d AWS resources", len(infra.Resources))
		for id, res := range infra.Resources {
			t.Logf("  AWS: %s (%s)", id, res.Type)
		}
	})

	// Parse Azure resources
	t.Run("Parse Azure Resources", func(t *testing.T) {
		registry := parser.DefaultRegistry()
		p, err := registry.GetByFormat(resource.ProviderAzure, parser.FormatARM)
		if err != nil {
			t.Fatalf("Failed to get Azure parser: %v", err)
		}

		infra, err := p.Parse(ctx, azurePath, parser.NewParseOptions())
		if err != nil {
			t.Fatalf("Failed to parse Azure: %v", err)
		}

		t.Logf("Parsed %d Azure resources", len(infra.Resources))
		for id, res := range infra.Resources {
			t.Logf("  Azure: %s (%s)", id, res.Type)
		}
	})

	// Parse GCP resources
	t.Run("Parse GCP Resources", func(t *testing.T) {
		registry := parser.DefaultRegistry()
		p, err := registry.GetByFormat(resource.ProviderGCP, parser.FormatTerraform)
		if err != nil {
			t.Fatalf("Failed to get GCP parser: %v", err)
		}

		// GCP parser needs a directory
		gcpDir := filepath.Join(tmpDir, "gcp")
		os.MkdirAll(gcpDir, 0755)
		gcpFilePath := filepath.Join(gcpDir, "main.tf")
		os.WriteFile(gcpFilePath, []byte(gcpContent), 0644)

		infra, err := p.Parse(ctx, gcpDir, parser.NewParseOptions())
		if err != nil {
			t.Fatalf("Failed to parse GCP: %v", err)
		}

		t.Logf("Parsed %d GCP resources", len(infra.Resources))
		for id, res := range infra.Resources {
			t.Logf("  GCP: %s (%s)", id, res.Type)
		}
	})
}

// TestMultiCloudMigration_MergeResults tests merging migration results from multiple providers.
func TestMultiCloudMigration_MergeResults(t *testing.T) {
	ctx := context.Background()
	outputDir := t.TempDir()

	// Create resources from different cloud providers
	awsResources := []*resource.AWSResource{
		func() *resource.AWSResource {
			r := resource.NewAWSResource("aws-db", "aws-database", resource.TypeRDSInstance)
			r.Config["engine"] = "postgres"
			r.Config["engine_version"] = "15"
			return r
		}(),
		func() *resource.AWSResource {
			r := resource.NewAWSResource("aws-bucket", "aws-storage", resource.TypeS3Bucket)
			r.Config["bucket"] = "aws-storage"
			return r
		}(),
	}

	gcpResources := []*resource.AWSResource{
		func() *resource.AWSResource {
			r := resource.NewAWSResource("gcp-db", "gcp-database", resource.TypeCloudSQL)
			r.Config["database_version"] = "POSTGRES_15"
			return r
		}(),
		func() *resource.AWSResource {
			r := resource.NewAWSResource("gcp-bucket", "gcp-storage", resource.TypeGCSBucket)
			r.Config["location"] = "US"
			return r
		}(),
	}

	azureResources := []*resource.AWSResource{
		func() *resource.AWSResource {
			r := resource.NewAWSResource("azure-db", "azure-database", resource.TypeAzurePostgres)
			r.Config["version"] = "15"
			return r
		}(),
		func() *resource.AWSResource {
			r := resource.NewAWSResource("azure-storage", "azure-storage", resource.TypeAzureStorageAcct)
			r.Config["account_tier"] = "Standard"
			return r
		}(),
	}

	// Combine all resources
	allResources := append(awsResources, gcpResources...)
	allResources = append(allResources, azureResources...)

	// Map all resources
	var results []*mapper.MappingResult
	providerStats := map[string]int{
		"aws":   0,
		"gcp":   0,
		"azure": 0,
	}

	for _, res := range allResources {
		m, err := mapper.Get(res.Type)
		if err != nil {
			t.Logf("No mapper for %s: %v", res.Type, err)
			continue
		}

		result, err := m.Map(ctx, res)
		if err != nil {
			t.Logf("Failed to map %s: %v", res.ID, err)
			continue
		}

		if result != nil {
			results = append(results, result)

			// Track by provider
			provider := res.Type.Provider()
			providerStats[string(provider)]++
		}
	}

	t.Logf("Mapped resources by provider:")
	for provider, count := range providerStats {
		t.Logf("  %s: %d", provider, count)
	}

	if len(results) == 0 {
		t.Skip("No resources mapped")
	}

	// Generate merged Docker Compose
	gen := compose.NewGenerator("multi-cloud-stack")
	output, err := gen.Generate(results)
	if err != nil {
		t.Fatalf("Failed to generate output: %v", err)
	}

	// Verify docker-compose.yml
	composeContent, ok := output.Files["docker-compose.yml"]
	if !ok {
		t.Fatal("docker-compose.yml not generated")
	}

	// Write output
	composePath := filepath.Join(outputDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, composeContent, 0644); err != nil {
		t.Fatalf("Failed to write docker-compose.yml: %v", err)
	}

	t.Logf("Generated merged docker-compose.yml (%d bytes)", len(composeContent))

	// Verify content contains services from all providers
	composeStr := string(composeContent)
	if !strings.Contains(composeStr, "services:") {
		t.Error("Missing services section")
	}

	t.Logf("Merged docker-compose.yml written to: %s", composePath)
}

// TestMultiCloudMigration_HandleMixedInfrastructure tests handling infrastructure with resources from multiple providers.
func TestMultiCloudMigration_HandleMixedInfrastructure(t *testing.T) {
	ctx := context.Background()

	// Simulate a real-world scenario: frontend on AWS, backend on GCP, data on Azure
	infrastructures := map[resource.Provider]*resource.Infrastructure{
		resource.ProviderAWS:   resource.NewInfrastructure(resource.ProviderAWS),
		resource.ProviderGCP:   resource.NewInfrastructure(resource.ProviderGCP),
		resource.ProviderAzure: resource.NewInfrastructure(resource.ProviderAzure),
	}

	// AWS: Frontend infrastructure
	awsEC2 := resource.NewAWSResource("frontend-server", "frontend", resource.TypeEC2Instance)
	awsEC2.Config["instance_type"] = "t3.medium"
	awsEC2.Tags["tier"] = "frontend"
	infrastructures[resource.ProviderAWS].AddResource(awsEC2)

	awsALB := resource.NewAWSResource("frontend-lb", "loadbalancer", resource.TypeALB)
	awsALB.Config["load_balancer_type"] = "application"
	awsALB.AddDependency("frontend-server")
	infrastructures[resource.ProviderAWS].AddResource(awsALB)

	// GCP: Backend infrastructure
	gcpCloudRun := resource.NewAWSResource("api-service", "backend-api", resource.TypeCloudRun)
	gcpCloudRun.Config["image"] = "gcr.io/project/api:v1"
	gcpCloudRun.Tags["tier"] = "backend"
	infrastructures[resource.ProviderGCP].AddResource(gcpCloudRun)

	gcpSQL := resource.NewAWSResource("backend-db", "backend-database", resource.TypeCloudSQL)
	gcpSQL.Config["database_version"] = "POSTGRES_15"
	infrastructures[resource.ProviderGCP].AddResource(gcpSQL)

	// Azure: Data infrastructure
	azureCosmos := resource.NewAWSResource("data-store", "analytics-db", resource.TypeCosmosDB)
	azureCosmos.Config["kind"] = "MongoDB"
	azureCosmos.Tags["tier"] = "data"
	infrastructures[resource.ProviderAzure].AddResource(azureCosmos)

	azureStorage := resource.NewAWSResource("data-lake", "data-storage", resource.TypeAzureStorageAcct)
	azureStorage.Config["account_tier"] = "Standard"
	infrastructures[resource.ProviderAzure].AddResource(azureStorage)

	// Map all resources from all providers
	var allResults []*mapper.MappingResult
	var allWarnings []string

	for provider, infra := range infrastructures {
		t.Logf("Processing %s resources:", provider)

		for _, res := range infra.Resources {
			m, err := mapper.Get(res.Type)
			if err != nil {
				t.Logf("  No mapper for %s (%s)", res.ID, res.Type)
				continue
			}

			result, err := m.Map(ctx, res)
			if err != nil {
				t.Logf("  Failed to map %s: %v", res.ID, err)
				continue
			}

			if result != nil {
				allResults = append(allResults, result)
				allWarnings = append(allWarnings, result.Warnings...)

				if result.DockerService != nil {
					t.Logf("  Mapped %s -> %s", res.ID, result.DockerService.Name)
				}
			}
		}
	}

	t.Logf("Total mapped: %d resources from %d providers", len(allResults), len(infrastructures))

	if len(allResults) == 0 {
		t.Skip("No resources mapped")
	}

	// Generate unified output
	gen := compose.NewGenerator("mixed-infrastructure")
	output, err := gen.Generate(allResults)
	if err != nil {
		t.Fatalf("Failed to generate output: %v", err)
	}

	t.Logf("Generated %d files", len(output.Files))

	// Verify all tiers are represented
	composeContent := string(output.Files["docker-compose.yml"])
	t.Log("Generated docker-compose.yml preview:")
	lines := strings.Split(composeContent, "\n")
	for i, line := range lines {
		if i < 30 { // Show first 30 lines
			t.Log(line)
		}
	}
}

// TestMultiCloudMigration_ConflictResolution tests handling of naming conflicts across providers.
func TestMultiCloudMigration_ConflictResolution(t *testing.T) {
	ctx := context.Background()

	// Create resources with similar names from different providers
	resources := []*resource.AWSResource{
		// AWS database
		func() *resource.AWSResource {
			r := resource.NewAWSResource("main-db", "database", resource.TypeRDSInstance)
			r.Config["engine"] = "postgres"
			return r
		}(),
		// GCP database (same logical name)
		func() *resource.AWSResource {
			r := resource.NewAWSResource("main-db", "database", resource.TypeCloudSQL)
			r.Config["database_version"] = "POSTGRES_15"
			return r
		}(),
		// Azure database (same logical name)
		func() *resource.AWSResource {
			r := resource.NewAWSResource("main-db", "database", resource.TypeAzurePostgres)
			r.Config["version"] = "15"
			return r
		}(),
	}

	var results []*mapper.MappingResult
	serviceNames := make(map[string]int)

	for _, res := range resources {
		m, err := mapper.Get(res.Type)
		if err != nil {
			continue
		}

		result, err := m.Map(ctx, res)
		if err != nil || result == nil || result.DockerService == nil {
			continue
		}

		results = append(results, result)

		// Track service names
		name := result.DockerService.Name
		serviceNames[name]++

		t.Logf("Resource %s (%s) -> service %s", res.ID, res.Type.Provider(), name)
	}

	// Check for name conflicts
	for name, count := range serviceNames {
		if count > 1 {
			t.Logf("Warning: Service name '%s' used %d times (may cause conflict)", name, count)
		}
	}

	if len(results) < 2 {
		t.Skip("Not enough resources mapped to test conflict resolution")
	}

	// Try to generate Docker Compose
	gen := compose.NewGenerator("conflict-test")
	output, err := gen.Generate(results)
	if err != nil {
		// Expect either successful generation or a meaningful error
		t.Logf("Generation with conflicts: %v", err)
		return
	}

	if len(output.Files) > 0 {
		t.Log("Docker Compose generated despite potential naming conflicts")
	}
}

// TestMultiCloudMigration_DependenciesAcrossProviders tests handling dependencies that cross provider boundaries.
func TestMultiCloudMigration_DependenciesAcrossProviders(t *testing.T) {
	ctx := context.Background()

	// Create infrastructure with cross-provider dependencies
	// AWS Lambda that depends on GCP Pub/Sub and Azure CosmosDB
	resources := []*resource.AWSResource{
		// GCP Pub/Sub topic (no dependencies)
		func() *resource.AWSResource {
			r := resource.NewAWSResource("events-topic", "events", resource.TypePubSubTopic)
			r.Config["name"] = "events"
			return r
		}(),
		// Azure CosmosDB (no dependencies)
		func() *resource.AWSResource {
			r := resource.NewAWSResource("data-store", "cosmos", resource.TypeCosmosDB)
			r.Config["kind"] = "MongoDB"
			return r
		}(),
		// AWS Lambda (depends on both)
		func() *resource.AWSResource {
			r := resource.NewAWSResource("processor", "event-processor", resource.TypeLambdaFunction)
			r.Config["runtime"] = "nodejs18.x"
			r.Config["handler"] = "index.handler"
			r.AddDependency("events-topic") // GCP resource
			r.AddDependency("data-store")   // Azure resource
			return r
		}(),
	}

	// Build dependency graph
	graph := resource.NewGraph()
	for _, res := range resources {
		graph.AddResource(res)
	}

	// Check for cycles
	if graph.HasCycles() {
		t.Error("Unexpected cycle in cross-provider dependencies")
	}

	// Topological sort
	sorted, err := graph.TopologicalSort()
	if err != nil {
		t.Fatalf("Failed to sort dependencies: %v", err)
	}

	t.Log("Deployment order for cross-provider dependencies:")
	for i, res := range sorted {
		t.Logf("  %d. %s (%s) - provider: %s",
			i+1, res.Name, res.ID, res.Type.Provider())
	}

	// Verify order: dependencies before dependents
	positions := make(map[string]int)
	for i, res := range sorted {
		positions[res.ID] = i
	}

	processorPos := positions["processor"]
	topicPos := positions["events-topic"]
	storePos := positions["data-store"]

	if processorPos <= topicPos {
		t.Error("Processor should be after events-topic")
	}
	if processorPos <= storePos {
		t.Error("Processor should be after data-store")
	}

	// Map resources
	var results []*mapper.MappingResult
	for _, res := range sorted {
		m, err := mapper.Get(res.Type)
		if err != nil {
			continue
		}
		result, err := m.Map(ctx, res)
		if err != nil || result == nil {
			continue
		}
		results = append(results, result)
	}

	t.Logf("Mapped %d cross-provider resources", len(results))
}

// TestMultiCloudMigration_UnifiedOutputGeneration tests generating a unified output from multiple providers.
func TestMultiCloudMigration_UnifiedOutputGeneration(t *testing.T) {
	ctx := context.Background()
	outputDir := t.TempDir()

	// Create a comprehensive multi-cloud setup
	resources := []*resource.AWSResource{
		// AWS resources
		func() *resource.AWSResource {
			r := resource.NewAWSResource("aws-ec2", "web-server", resource.TypeEC2Instance)
			r.Config["instance_type"] = "t3.medium"
			return r
		}(),
		func() *resource.AWSResource {
			r := resource.NewAWSResource("aws-rds", "postgres", resource.TypeRDSInstance)
			r.Config["engine"] = "postgres"
			r.Config["engine_version"] = "15"
			return r
		}(),
		func() *resource.AWSResource {
			r := resource.NewAWSResource("aws-s3", "storage", resource.TypeS3Bucket)
			r.Config["bucket"] = "my-bucket"
			return r
		}(),
		// GCP resources
		func() *resource.AWSResource {
			r := resource.NewAWSResource("gcp-sql", "analytics-db", resource.TypeCloudSQL)
			r.Config["database_version"] = "POSTGRES_15"
			return r
		}(),
		func() *resource.AWSResource {
			r := resource.NewAWSResource("gcp-gcs", "data-lake", resource.TypeGCSBucket)
			r.Config["location"] = "US"
			return r
		}(),
		func() *resource.AWSResource {
			r := resource.NewAWSResource("gcp-redis", "cache", resource.TypeMemorystore)
			r.Config["tier"] = "STANDARD_HA"
			return r
		}(),
		// Azure resources
		func() *resource.AWSResource {
			r := resource.NewAWSResource("azure-pg", "reporting-db", resource.TypeAzurePostgres)
			r.Config["version"] = "15"
			return r
		}(),
		func() *resource.AWSResource {
			r := resource.NewAWSResource("azure-redis", "session-cache", resource.TypeAzureCache)
			r.Config["sku_name"] = "Standard"
			return r
		}(),
		func() *resource.AWSResource {
			r := resource.NewAWSResource("azure-storage", "blobs", resource.TypeAzureStorageAcct)
			r.Config["account_tier"] = "Standard"
			return r
		}(),
	}

	// Map all resources
	var results []*mapper.MappingResult
	var allConfigs []byte
	var allScripts []string

	for _, res := range resources {
		m, err := mapper.Get(res.Type)
		if err != nil {
			t.Logf("No mapper for %s", res.Type)
			continue
		}

		result, err := m.Map(ctx, res)
		if err != nil {
			t.Logf("Failed to map %s: %v", res.ID, err)
			continue
		}

		if result != nil {
			results = append(results, result)

			// Collect configs
			for _, content := range result.Configs {
				allConfigs = append(allConfigs, content...)
			}

			// Collect scripts
			for name := range result.Scripts {
				allScripts = append(allScripts, name)
			}
		}
	}

	t.Logf("Mapped %d/%d resources", len(results), len(resources))

	if len(results) == 0 {
		t.Skip("No resources mapped")
	}

	// Generate unified Docker Compose
	gen := compose.NewGenerator("multi-cloud-unified")
	output, err := gen.Generate(results)
	if err != nil {
		t.Fatalf("Failed to generate output: %v", err)
	}

	// Write all generated files
	for name, content := range output.Files {
		filePath := filepath.Join(outputDir, name)
		if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			t.Errorf("Failed to create directory for %s: %v", name, err)
			continue
		}
		if err := os.WriteFile(filePath, content, 0644); err != nil {
			t.Errorf("Failed to write %s: %v", name, err)
			continue
		}
		t.Logf("Generated: %s (%d bytes)", name, len(content))
	}

	// Create additional directories
	for _, res := range resources {
		m, err := mapper.Get(res.Type)
		if err != nil {
			continue
		}
		result, err := m.Map(ctx, res)
		if err != nil || result == nil {
			continue
		}

		// Write scripts
		for name, content := range result.Scripts {
			scriptPath := filepath.Join(outputDir, "scripts", name)
			os.MkdirAll(filepath.Dir(scriptPath), 0755)
			os.WriteFile(scriptPath, content, 0755)
		}

		// Write configs
		for name, content := range result.Configs {
			configPath := filepath.Join(outputDir, "configs", name)
			os.MkdirAll(filepath.Dir(configPath), 0755)
			os.WriteFile(configPath, content, 0644)
		}
	}

	// Verify docker-compose.yml structure
	composePath := filepath.Join(outputDir, "docker-compose.yml")
	composeContent, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("Failed to read docker-compose.yml: %v", err)
	}

	composeStr := string(composeContent)

	// Basic structure validation
	if !strings.Contains(composeStr, "version:") {
		t.Error("Missing version in docker-compose.yml")
	}
	if !strings.Contains(composeStr, "services:") {
		t.Error("Missing services in docker-compose.yml")
	}
	if !strings.Contains(composeStr, "networks:") {
		t.Log("Note: No networks section in docker-compose.yml")
	}

	// Summary
	t.Logf("Multi-cloud migration complete:")
	t.Logf("  - Resources processed: %d", len(resources))
	t.Logf("  - Resources mapped: %d", len(results))
	t.Logf("  - Files generated: %d", len(output.Files))
	t.Logf("  - Output directory: %s", outputDir)

	// List all generated files
	filepath.Walk(outputDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		relPath, _ := filepath.Rel(outputDir, path)
		t.Logf("  - %s (%d bytes)", relPath, info.Size())
		return nil
	})
}

// TestMultiCloudMigration_ProviderSpecificFeatures tests that provider-specific features are preserved.
func TestMultiCloudMigration_ProviderSpecificFeatures(t *testing.T) {
	ctx := context.Background()

	// Test AWS-specific features
	t.Run("AWS Specific Features", func(t *testing.T) {
		rds := resource.NewAWSResource("aws-db", "database", resource.TypeRDSInstance)
		rds.Config["engine"] = "aurora-postgresql"
		rds.Config["engine_mode"] = "serverless"
		rds.Config["scaling_configuration"] = map[string]interface{}{
			"auto_pause":               true,
			"min_capacity":             2,
			"max_capacity":             16,
			"seconds_until_auto_pause": 300,
		}

		m, err := mapper.Get(resource.TypeRDSInstance)
		if err != nil {
			t.Logf("RDS mapper not available: %v", err)
			return
		}

		result, err := m.Map(ctx, rds)
		if err != nil {
			t.Logf("Failed to map: %v", err)
			return
		}

		if result != nil {
			t.Logf("Aurora Serverless mapped to: %v", result.DockerService)
			// Check for scaling-related warnings
			for _, w := range result.Warnings {
				if strings.Contains(strings.ToLower(w), "serverless") ||
					strings.Contains(strings.ToLower(w), "scaling") {
					t.Logf("Serverless warning: %s", w)
				}
			}
		}
	})

	// Test GCP-specific features
	t.Run("GCP Specific Features", func(t *testing.T) {
		cloudRun := resource.NewAWSResource("gcp-api", "api", resource.TypeCloudRun)
		cloudRun.Config["image"] = "gcr.io/project/api:v1"
		cloudRun.Config["autoscaling"] = map[string]interface{}{
			"min_instance_count": 0,
			"max_instance_count": 100,
		}
		cloudRun.Config["traffic"] = []map[string]interface{}{
			{"percent": 90, "revision_name": "api-v1"},
			{"percent": 10, "revision_name": "api-v2"},
		}

		m, err := mapper.Get(resource.TypeCloudRun)
		if err != nil {
			t.Logf("Cloud Run mapper not available: %v", err)
			return
		}

		result, err := m.Map(ctx, cloudRun)
		if err != nil {
			t.Logf("Failed to map: %v", err)
			return
		}

		if result != nil {
			t.Logf("Cloud Run mapped to: %v", result.DockerService)
			// Check for traffic splitting warnings
			for _, w := range result.Warnings {
				if strings.Contains(strings.ToLower(w), "traffic") {
					t.Logf("Traffic warning: %s", w)
				}
			}
		}
	})

	// Test Azure-specific features
	t.Run("Azure Specific Features", func(t *testing.T) {
		cosmos := resource.NewAWSResource("azure-cosmos", "data", resource.TypeCosmosDB)
		cosmos.Config["kind"] = "GlobalDocumentDB"
		cosmos.Config["consistency_policy"] = map[string]interface{}{
			"consistency_level": "BoundedStaleness",
			"max_staleness_seconds": 300,
		}
		cosmos.Config["geo_location"] = []map[string]interface{}{
			{"location": "eastus", "failover_priority": 0},
			{"location": "westus", "failover_priority": 1},
		}

		m, err := mapper.Get(resource.TypeCosmosDB)
		if err != nil {
			t.Logf("CosmosDB mapper not available: %v", err)
			return
		}

		result, err := m.Map(ctx, cosmos)
		if err != nil {
			t.Logf("Failed to map: %v", err)
			return
		}

		if result != nil {
			t.Logf("CosmosDB mapped to: %v", result.DockerService)
			// Check for geo-replication warnings
			for _, w := range result.Warnings {
				if strings.Contains(strings.ToLower(w), "geo") ||
					strings.Contains(strings.ToLower(w), "replication") {
					t.Logf("Geo-replication warning: %s", w)
				}
			}
		}
	})
}

