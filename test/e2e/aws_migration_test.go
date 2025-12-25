// Package e2e contains end-to-end tests for the CloudExit migration tool.
package e2e

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudexit/cloudexit/internal/domain/mapper"
	"github.com/cloudexit/cloudexit/internal/domain/parser"
	"github.com/cloudexit/cloudexit/internal/domain/resource"
	"github.com/cloudexit/cloudexit/internal/infrastructure/generator/compose"
	_ "github.com/cloudexit/cloudexit/internal/infrastructure/mapper/compute"
	_ "github.com/cloudexit/cloudexit/internal/infrastructure/mapper/database"
	_ "github.com/cloudexit/cloudexit/internal/infrastructure/mapper/storage"
	_ "github.com/cloudexit/cloudexit/internal/infrastructure/parser/aws"
)

// TestAWSMigration_Terraform tests complete AWS to self-hosted migration using Terraform state.
func TestAWSMigration_Terraform(t *testing.T) {
	// Setup test fixtures
	fixturePath := filepath.Join("..", "fixtures", "simple-webapp", "terraform.tfstate")
	if _, err := os.Stat(fixturePath); os.IsNotExist(err) {
		t.Skip("Test fixture not found, skipping test")
	}

	outputDir := t.TempDir()
	ctx := context.Background()

	// Step 1: Parse AWS infrastructure from Terraform state
	t.Run("Parse AWS Infrastructure", func(t *testing.T) {
		registry := parser.DefaultRegistry()
		p, err := registry.GetByFormat(resource.ProviderAWS, parser.FormatTFState)
		if err != nil {
			t.Fatalf("Failed to get Terraform state parser: %v", err)
		}

		opts := parser.NewParseOptions()
		infra, err := p.Parse(ctx, fixturePath, opts)
		if err != nil {
			t.Fatalf("Failed to parse infrastructure: %v", err)
		}

		if infra == nil {
			t.Fatal("Infrastructure is nil")
		}

		if len(infra.Resources) == 0 {
			t.Fatal("No resources parsed from Terraform state")
		}

		t.Logf("Parsed %d resources from AWS Terraform state", len(infra.Resources))

		// Verify expected AWS resource types
		expectedTypes := map[resource.Type]bool{
			resource.TypeEC2Instance: false,
			resource.TypeRDSInstance: false,
			resource.TypeS3Bucket:    false,
			resource.TypeALB:         false,
		}

		for _, res := range infra.Resources {
			if _, ok := expectedTypes[res.Type]; ok {
				expectedTypes[res.Type] = true
			}
		}

		for resType, found := range expectedTypes {
			if !found {
				t.Logf("Warning: Expected resource type %s not found in fixtures", resType)
			}
		}
	})

	// Step 2: Map resources to self-hosted alternatives
	t.Run("Map to Self-Hosted", func(t *testing.T) {
		registry := parser.DefaultRegistry()
		p, err := registry.GetByFormat(resource.ProviderAWS, parser.FormatTFState)
		if err != nil {
			t.Fatalf("Failed to get parser: %v", err)
		}

		infra, err := p.Parse(ctx, fixturePath, parser.NewParseOptions())
		if err != nil {
			t.Fatalf("Failed to parse: %v", err)
		}

		mappedCount := 0
		warningsCount := 0
		manualStepsCount := 0

		for _, res := range infra.Resources {
			m, err := mapper.Get(res.Type)
			if err != nil {
				t.Logf("No mapper for resource type %s: %v", res.Type, err)
				continue
			}

			result, err := m.Map(ctx, res)
			if err != nil {
				t.Logf("Failed to map resource %s: %v", res.ID, err)
				continue
			}

			if result != nil && result.DockerService != nil {
				mappedCount++
				warningsCount += len(result.Warnings)
				manualStepsCount += len(result.ManualSteps)

				t.Logf("Mapped %s -> %s (image: %s)", res.ID, result.DockerService.Name, result.DockerService.Image)

				// Verify Docker service has required fields
				if result.DockerService.Image == "" {
					t.Errorf("Docker service %s has no image", result.DockerService.Name)
				}
			}
		}

		t.Logf("Mapped %d resources, %d warnings, %d manual steps", mappedCount, warningsCount, manualStepsCount)
	})

	// Step 3: Generate Docker Compose output
	t.Run("Generate Docker Compose", func(t *testing.T) {
		registry := parser.DefaultRegistry()
		p, err := registry.GetByFormat(resource.ProviderAWS, parser.FormatTFState)
		if err != nil {
			t.Fatalf("Failed to get parser: %v", err)
		}

		infra, err := p.Parse(ctx, fixturePath, parser.NewParseOptions())
		if err != nil {
			t.Fatalf("Failed to parse: %v", err)
		}

		var results []*mapper.MappingResult
		for _, res := range infra.Resources {
			m, err := mapper.Get(res.Type)
			if err != nil {
				continue
			}
			result, err := m.Map(ctx, res)
			if err != nil {
				continue
			}
			if result != nil {
				results = append(results, result)
			}
		}

		if len(results) == 0 {
			t.Skip("No resources mapped, skipping Docker Compose generation")
		}

		gen := compose.NewGenerator("aws-migration-test")
		output, err := gen.Generate(results)
		if err != nil {
			t.Fatalf("Failed to generate Docker Compose: %v", err)
		}

		// Verify output contains docker-compose.yml
		composeContent, ok := output.Files["docker-compose.yml"]
		if !ok {
			t.Fatal("docker-compose.yml not generated")
		}

		// Validate docker-compose.yml structure
		composeStr := string(composeContent)
		if !strings.Contains(composeStr, "version:") {
			t.Error("docker-compose.yml missing version field")
		}
		if !strings.Contains(composeStr, "services:") {
			t.Error("docker-compose.yml missing services field")
		}

		t.Logf("Generated docker-compose.yml with %d bytes", len(composeContent))

		// Write output to temp directory for inspection
		composePath := filepath.Join(outputDir, "docker-compose.yml")
		if err := os.WriteFile(composePath, composeContent, 0644); err != nil {
			t.Fatalf("Failed to write docker-compose.yml: %v", err)
		}

		t.Logf("Docker Compose written to: %s", composePath)
	})

	// Step 4: Verify migration scripts generation
	t.Run("Verify Migration Scripts", func(t *testing.T) {
		registry := parser.DefaultRegistry()
		p, err := registry.GetByFormat(resource.ProviderAWS, parser.FormatTFState)
		if err != nil {
			t.Fatalf("Failed to get parser: %v", err)
		}

		infra, err := p.Parse(ctx, fixturePath, parser.NewParseOptions())
		if err != nil {
			t.Fatalf("Failed to parse: %v", err)
		}

		scriptsGenerated := 0
		for _, res := range infra.Resources {
			m, err := mapper.Get(res.Type)
			if err != nil {
				continue
			}
			result, err := m.Map(ctx, res)
			if err != nil || result == nil {
				continue
			}

			if len(result.Scripts) > 0 {
				scriptsGenerated += len(result.Scripts)
				for name, content := range result.Scripts {
					t.Logf("Generated script: %s (%d bytes)", name, len(content))

					// Write scripts to output dir
					scriptPath := filepath.Join(outputDir, "scripts", name)
					if err := os.MkdirAll(filepath.Dir(scriptPath), 0755); err != nil {
						t.Errorf("Failed to create scripts directory: %v", err)
					}
					if err := os.WriteFile(scriptPath, content, 0755); err != nil {
						t.Errorf("Failed to write script %s: %v", name, err)
					}
				}
			}
		}

		t.Logf("Total migration scripts generated: %d", scriptsGenerated)
	})

	// Step 5: Verify warnings and manual steps
	t.Run("Verify Warnings and Manual Steps", func(t *testing.T) {
		registry := parser.DefaultRegistry()
		p, err := registry.GetByFormat(resource.ProviderAWS, parser.FormatTFState)
		if err != nil {
			t.Fatalf("Failed to get parser: %v", err)
		}

		infra, err := p.Parse(ctx, fixturePath, parser.NewParseOptions())
		if err != nil {
			t.Fatalf("Failed to parse: %v", err)
		}

		var allWarnings []string
		var allManualSteps []string

		for _, res := range infra.Resources {
			m, err := mapper.Get(res.Type)
			if err != nil {
				continue
			}
			result, err := m.Map(ctx, res)
			if err != nil || result == nil {
				continue
			}

			allWarnings = append(allWarnings, result.Warnings...)
			allManualSteps = append(allManualSteps, result.ManualSteps...)
		}

		if len(allWarnings) > 0 {
			t.Logf("Migration warnings (%d):", len(allWarnings))
			for _, w := range allWarnings {
				t.Logf("  - %s", w)
			}
		}

		if len(allManualSteps) > 0 {
			t.Logf("Manual steps required (%d):", len(allManualSteps))
			for _, s := range allManualSteps {
				t.Logf("  - %s", s)
			}
		}
	})
}

// TestAWSMigration_CloudFormation tests AWS migration using CloudFormation templates.
func TestAWSMigration_CloudFormation(t *testing.T) {
	// Create temporary CloudFormation template
	tmpDir := t.TempDir()
	cfnPath := filepath.Join(tmpDir, "template.yaml")

	cfnContent := `AWSTemplateFormatVersion: '2010-09-09'
Description: E2E Test CloudFormation Stack

Parameters:
  Environment:
    Type: String
    Default: production

Resources:
  WebServer:
    Type: AWS::EC2::Instance
    Properties:
      InstanceType: t3.medium
      ImageId: ami-12345678
      Tags:
        - Key: Name
          Value: web-server
        - Key: Environment
          Value: !Ref Environment

  Database:
    Type: AWS::RDS::DBInstance
    Properties:
      DBInstanceIdentifier: mydb
      Engine: postgres
      EngineVersion: "15.4"
      DBInstanceClass: db.t3.medium
      AllocatedStorage: 100
      MasterUsername: admin
      Tags:
        - Key: Name
          Value: main-database
    DependsOn: WebServer

  StorageBucket:
    Type: AWS::S3::Bucket
    Properties:
      BucketName: my-app-storage
      VersioningConfiguration:
        Status: Enabled
      Tags:
        - Key: Name
          Value: app-storage

  MessageQueue:
    Type: AWS::SQS::Queue
    Properties:
      QueueName: app-queue
      VisibilityTimeout: 30

  CacheCluster:
    Type: AWS::ElastiCache::CacheCluster
    Properties:
      CacheClusterId: app-cache
      Engine: redis
      CacheNodeType: cache.t3.micro
      NumCacheNodes: 1
`

	if err := os.WriteFile(cfnPath, []byte(cfnContent), 0644); err != nil {
		t.Fatalf("Failed to write CloudFormation template: %v", err)
	}

	ctx := context.Background()
	outputDir := t.TempDir()

	// Step 1: Parse CloudFormation template
	t.Run("Parse CloudFormation Template", func(t *testing.T) {
		registry := parser.DefaultRegistry()
		p, err := registry.GetByFormat(resource.ProviderAWS, parser.FormatCloudFormation)
		if err != nil {
			t.Fatalf("Failed to get CloudFormation parser: %v", err)
		}

		opts := parser.NewParseOptions()
		infra, err := p.Parse(ctx, cfnPath, opts)
		if err != nil {
			t.Fatalf("Failed to parse CloudFormation template: %v", err)
		}

		expectedCount := 5 // EC2, RDS, S3, SQS, ElastiCache
		if len(infra.Resources) < expectedCount {
			t.Errorf("Expected at least %d resources, got %d", expectedCount, len(infra.Resources))
		}

		t.Logf("Parsed %d resources from CloudFormation", len(infra.Resources))

		// Verify dependencies
		db, exists := infra.Resources["Database"]
		if exists && len(db.Dependencies) > 0 {
			t.Logf("Database dependencies: %v", db.Dependencies)
		}
	})

	// Step 2: Full migration workflow
	t.Run("Complete Migration Workflow", func(t *testing.T) {
		registry := parser.DefaultRegistry()
		p, err := registry.GetByFormat(resource.ProviderAWS, parser.FormatCloudFormation)
		if err != nil {
			t.Fatalf("Failed to get CloudFormation parser: %v", err)
		}

		infra, err := p.Parse(ctx, cfnPath, parser.NewParseOptions())
		if err != nil {
			t.Fatalf("Failed to parse: %v", err)
		}

		// Map all resources
		var results []*mapper.MappingResult
		for _, res := range infra.Resources {
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
			}
		}

		if len(results) == 0 {
			t.Skip("No resources mapped")
		}

		// Generate Docker Compose
		gen := compose.NewGenerator("aws-cfn-migration")
		output, err := gen.Generate(results)
		if err != nil {
			t.Fatalf("Failed to generate output: %v", err)
		}

		// Write all generated files
		for name, content := range output.Files {
			filePath := filepath.Join(outputDir, name)
			if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
				t.Errorf("Failed to create directory for %s: %v", name, err)
			}
			if err := os.WriteFile(filePath, content, 0644); err != nil {
				t.Errorf("Failed to write %s: %v", name, err)
			}
			t.Logf("Generated: %s (%d bytes)", name, len(content))
		}

		// Verify docker-compose.yml exists and is valid
		composePath := filepath.Join(outputDir, "docker-compose.yml")
		content, err := os.ReadFile(composePath)
		if err != nil {
			t.Fatalf("Failed to read generated docker-compose.yml: %v", err)
		}

		if len(content) == 0 {
			t.Fatal("docker-compose.yml is empty")
		}

		t.Logf("Migration output written to: %s", outputDir)
	})
}

// TestAWSMigration_ResourceValidation tests resource validation during migration.
func TestAWSMigration_ResourceValidation(t *testing.T) {
	ctx := context.Background()

	// Create test resources with various configurations
	testCases := []struct {
		name        string
		resource    *resource.AWSResource
		expectError bool
	}{
		{
			name: "Valid EC2 Instance",
			resource: func() *resource.AWSResource {
				res := resource.NewAWSResource("i-123456", "web-server", resource.TypeEC2Instance)
				res.Config["instance_type"] = "t3.medium"
				res.Config["ami"] = "ami-12345678"
				return res
			}(),
			expectError: false,
		},
		{
			name: "Valid RDS Instance",
			resource: func() *resource.AWSResource {
				res := resource.NewAWSResource("mydb", "main-database", resource.TypeRDSInstance)
				res.Config["engine"] = "postgres"
				res.Config["engine_version"] = "15.4"
				res.Config["instance_class"] = "db.t3.medium"
				res.Config["allocated_storage"] = 100
				return res
			}(),
			expectError: false,
		},
		{
			name: "Valid S3 Bucket",
			resource: func() *resource.AWSResource {
				res := resource.NewAWSResource("my-bucket", "storage-bucket", resource.TypeS3Bucket)
				res.Config["bucket"] = "my-bucket"
				res.Region = "us-east-1"
				return res
			}(),
			expectError: false,
		},
		{
			name:        "Nil Resource",
			resource:    nil,
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.resource == nil {
				// Test nil resource handling
				_, err := mapper.Get(resource.TypeEC2Instance)
				if err == nil {
					m, _ := mapper.Get(resource.TypeEC2Instance)
					err = m.Validate(nil)
					if err == nil {
						t.Error("Expected error for nil resource")
					}
				}
				return
			}

			m, err := mapper.Get(tc.resource.Type)
			if err != nil {
				t.Logf("Mapper not found for %s: %v", tc.resource.Type, err)
				return
			}

			err = m.Validate(tc.resource)
			if tc.expectError && err == nil {
				t.Error("Expected validation error but got none")
			}
			if !tc.expectError && err != nil {
				t.Errorf("Unexpected validation error: %v", err)
			}

			// If validation passes, try mapping
			if err == nil {
				result, mapErr := m.Map(ctx, tc.resource)
				if mapErr != nil {
					t.Logf("Mapping warning: %v", mapErr)
				}
				if result != nil && result.DockerService != nil {
					t.Logf("Mapped to: %s (image: %s)", result.DockerService.Name, result.DockerService.Image)
				}
			}
		})
	}
}

// TestAWSMigration_DependencyOrder tests that resources are migrated in correct dependency order.
func TestAWSMigration_DependencyOrder(t *testing.T) {
	// Create infrastructure with dependencies
	infra := resource.NewInfrastructure(resource.ProviderAWS)

	// Database (no dependencies)
	db := resource.NewAWSResource("db-1", "database", resource.TypeRDSInstance)
	db.Config["engine"] = "postgres"
	infra.AddResource(db)

	// Cache (no dependencies)
	cache := resource.NewAWSResource("cache-1", "cache", resource.TypeElastiCache)
	cache.Config["engine"] = "redis"
	infra.AddResource(cache)

	// Web server (depends on database and cache)
	web := resource.NewAWSResource("web-1", "webserver", resource.TypeEC2Instance)
	web.Config["instance_type"] = "t3.medium"
	web.AddDependency("db-1")
	web.AddDependency("cache-1")
	infra.AddResource(web)

	// Load balancer (depends on web server)
	lb := resource.NewAWSResource("lb-1", "loadbalancer", resource.TypeALB)
	lb.Config["load_balancer_type"] = "application"
	lb.AddDependency("web-1")
	infra.AddResource(lb)

	// Build dependency graph
	graph := resource.NewGraph()
	for _, res := range infra.Resources {
		graph.AddResource(res)
	}

	// Topological sort
	sorted, err := graph.TopologicalSort()
	if err != nil {
		t.Fatalf("Failed to sort dependencies: %v", err)
	}

	t.Log("Deployment order:")
	for i, res := range sorted {
		t.Logf("  %d. %s (%s)", i+1, res.Name, res.ID)
	}

	// Verify order: database and cache before web, web before lb
	positions := make(map[string]int)
	for i, res := range sorted {
		positions[res.ID] = i
	}

	if positions["web-1"] <= positions["db-1"] {
		t.Error("Web server should be deployed after database")
	}
	if positions["web-1"] <= positions["cache-1"] {
		t.Error("Web server should be deployed after cache")
	}
	if positions["lb-1"] <= positions["web-1"] {
		t.Error("Load balancer should be deployed after web server")
	}
}
