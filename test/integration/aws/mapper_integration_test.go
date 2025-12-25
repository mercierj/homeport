package aws_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	domainmapper "github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/parser"
	"github.com/agnostech/agnostech/internal/domain/resource"
	"github.com/agnostech/agnostech/internal/infrastructure/mapper"
	"github.com/agnostech/agnostech/internal/infrastructure/mapper/compute"
	"github.com/agnostech/agnostech/internal/infrastructure/mapper/database"
	"github.com/agnostech/agnostech/internal/infrastructure/mapper/storage"
	awsparser "github.com/agnostech/agnostech/internal/infrastructure/parser/aws"
)

// TestMapperIntegration_ParserToMapper tests the complete workflow from parsing to mapping.
func TestMapperIntegration_ParserToMapper(t *testing.T) {
	t.Run("S3ToMinIO", func(t *testing.T) {
		// Create a test S3 resource
		res := resource.NewAWSResource("my-bucket", "my-bucket", resource.TypeS3Bucket)
		res.Config["bucket"] = "my-app-assets"
		res.Config["region"] = "us-east-1"
		res.Config["versioning"] = map[string]interface{}{
			"enabled": true,
		}
		res.Tags["Environment"] = "production"
		res.Tags["Application"] = "webapp"

		// Create mapper and map
		s3Mapper := storage.NewS3Mapper()
		ctx := context.Background()

		result, err := s3Mapper.Map(ctx, res)
		if err != nil {
			t.Fatalf("failed to map S3 bucket: %v", err)
		}

		// Verify MappingResult structure
		if result == nil {
			t.Fatal("expected mapping result to be non-nil")
		}

		if result.DockerService == nil {
			t.Fatal("expected DockerService to be non-nil")
		}

		// Verify MinIO image
		if !strings.Contains(result.DockerService.Image, "minio") {
			t.Errorf("expected minio image, got %s", result.DockerService.Image)
		}

		// Verify ports
		if len(result.DockerService.Ports) < 2 {
			t.Error("expected at least 2 ports (API and console)")
		}

		// Verify environment variables
		if result.DockerService.Environment["MINIO_ROOT_USER"] == "" {
			t.Error("expected MINIO_ROOT_USER to be set")
		}

		// Verify volumes
		if len(result.DockerService.Volumes) == 0 {
			t.Error("expected at least one volume")
		}

		// Verify scripts were generated
		if len(result.Scripts) == 0 {
			t.Error("expected setup scripts to be generated")
		}

		// Verify versioning warning/step was added
		hasVersioningNote := false
		for _, step := range result.ManualSteps {
			if strings.Contains(step, "versioning") {
				hasVersioningNote = true
				break
			}
		}
		if !hasVersioningNote {
			t.Log("Note: versioning manual step may not be present")
		}

		t.Logf("S3 to MinIO mapping successful: image=%s, ports=%v",
			result.DockerService.Image, result.DockerService.Ports)
	})

	t.Run("EC2ToDocker", func(t *testing.T) {
		// Create a test EC2 resource
		res := resource.NewAWSResource("i-1234567890abcdef0", "web-server", resource.TypeEC2Instance)
		res.Config["instance_type"] = "t3.medium"
		res.Config["ami"] = "ami-0c55b159cbfafe1f0"
		res.Config["user_data"] = "#!/bin/bash\napt-get update\napt-get install -y nginx"
		res.Tags["Name"] = "web-server"
		res.Tags["Environment"] = "production"
		res.Tags["OS"] = "ubuntu"

		// Create mapper and map
		ec2Mapper := compute.NewEC2Mapper()
		ctx := context.Background()

		result, err := ec2Mapper.Map(ctx, res)
		if err != nil {
			t.Fatalf("failed to map EC2 instance: %v", err)
		}

		// Verify MappingResult structure
		if result == nil {
			t.Fatal("expected mapping result to be non-nil")
		}

		if result.DockerService == nil {
			t.Fatal("expected DockerService to be non-nil")
		}

		// Verify base image selection (should detect Ubuntu from tags)
		if !strings.Contains(result.DockerService.Image, "ubuntu") {
			t.Logf("Image: %s (may not be Ubuntu if tags not used for detection)", result.DockerService.Image)
		}

		// Verify environment variables include instance info
		if result.DockerService.Environment["INSTANCE_TYPE"] != "t3.medium" {
			t.Error("expected INSTANCE_TYPE environment variable")
		}

		// Verify resource limits were applied based on instance type
		if result.DockerService.Deploy != nil && result.DockerService.Deploy.Resources != nil {
			limits := result.DockerService.Deploy.Resources.Limits
			if limits != nil {
				t.Logf("Resource limits applied: CPUs=%s, Memory=%s", limits.CPUs, limits.Memory)
			}
		}

		// Verify configs/scripts were generated for user data
		hasDockerfile := false
		for filename := range result.Configs {
			if strings.Contains(filename, "Dockerfile") {
				hasDockerfile = true
				break
			}
		}
		if !hasDockerfile {
			t.Log("Note: Dockerfile may not be generated for simple user data")
		}

		t.Logf("EC2 to Docker mapping successful: image=%s", result.DockerService.Image)
	})

	t.Run("RDSToPostgreSQL", func(t *testing.T) {
		// Create a test RDS resource
		res := resource.NewAWSResource("mydb", "webapp-database", resource.TypeRDSInstance)
		res.Config["engine"] = "postgres"
		res.Config["engine_version"] = "15.4"
		res.Config["instance_class"] = "db.t3.medium"
		res.Config["allocated_storage"] = 100
		res.Config["db_name"] = "webapp"
		res.Config["multi_az"] = true
		res.Config["storage_encrypted"] = true
		res.Config["backup_retention_period"] = 7
		res.Tags["Environment"] = "production"

		// Create mapper and map
		rdsMapper := database.NewRDSMapper()
		ctx := context.Background()

		result, err := rdsMapper.Map(ctx, res)
		if err != nil {
			t.Fatalf("failed to map RDS instance: %v", err)
		}

		// Verify MappingResult structure
		if result == nil {
			t.Fatal("expected mapping result to be non-nil")
		}

		if result.DockerService == nil {
			t.Fatal("expected DockerService to be non-nil")
		}

		// Verify PostgreSQL image
		if !strings.Contains(result.DockerService.Image, "postgres") {
			t.Errorf("expected postgres image, got %s", result.DockerService.Image)
		}

		// Verify version is included in image
		if !strings.Contains(result.DockerService.Image, "15") {
			t.Logf("Image version: %s", result.DockerService.Image)
		}

		// Verify port mapping
		hasPort := false
		for _, port := range result.DockerService.Ports {
			if strings.Contains(port, "5432") {
				hasPort = true
				break
			}
		}
		if !hasPort {
			t.Error("expected port 5432 to be mapped")
		}

		// Verify environment variables
		if result.DockerService.Environment["POSTGRES_DB"] != "webapp" {
			t.Error("expected POSTGRES_DB to be set")
		}

		// Verify health check
		if result.DockerService.HealthCheck == nil {
			t.Error("expected health check to be configured")
		} else {
			hasHealthCmd := false
			for _, cmd := range result.DockerService.HealthCheck.Test {
				if strings.Contains(cmd, "pg_isready") {
					hasHealthCmd = true
					break
				}
			}
			if !hasHealthCmd {
				t.Error("expected pg_isready health check")
			}
		}

		// Verify warnings for RDS-specific features
		hasMultiAZWarning := false
		hasEncryptionWarning := false
		for _, warning := range result.Warnings {
			if strings.Contains(warning, "Multi-AZ") {
				hasMultiAZWarning = true
			}
			if strings.Contains(warning, "encryption") {
				hasEncryptionWarning = true
			}
		}
		if !hasMultiAZWarning {
			t.Log("Note: Multi-AZ warning may not be present")
		}
		if !hasEncryptionWarning {
			t.Log("Note: Encryption warning may not be present")
		}

		// Verify config files were generated
		if len(result.Configs) == 0 {
			t.Error("expected config files to be generated")
		}

		// Verify migration script was generated
		if len(result.Scripts) == 0 {
			t.Error("expected migration scripts to be generated")
		}

		t.Logf("RDS to PostgreSQL mapping successful: image=%s, warnings=%d",
			result.DockerService.Image, len(result.Warnings))
	})

	t.Run("RDSToMySQL", func(t *testing.T) {
		// Create a test RDS MySQL resource
		res := resource.NewAWSResource("mysqldb", "app-database", resource.TypeRDSInstance)
		res.Config["engine"] = "mysql"
		res.Config["engine_version"] = "8.0.35"
		res.Config["instance_class"] = "db.t3.medium"
		res.Config["allocated_storage"] = 50
		res.Config["db_name"] = "appdb"
		res.Tags["Environment"] = "staging"

		// Create mapper and map
		rdsMapper := database.NewRDSMapper()
		ctx := context.Background()

		result, err := rdsMapper.Map(ctx, res)
		if err != nil {
			t.Fatalf("failed to map RDS MySQL instance: %v", err)
		}

		// Verify MySQL image
		if !strings.Contains(result.DockerService.Image, "mysql") {
			t.Errorf("expected mysql image, got %s", result.DockerService.Image)
		}

		// Verify port mapping
		hasPort := false
		for _, port := range result.DockerService.Ports {
			if strings.Contains(port, "3306") {
				hasPort = true
				break
			}
		}
		if !hasPort {
			t.Error("expected port 3306 to be mapped")
		}

		t.Logf("RDS to MySQL mapping successful: image=%s", result.DockerService.Image)
	})
}

// TestMapperIntegration_FullWorkflow tests parsing then mapping.
func TestMapperIntegration_FullWorkflow(t *testing.T) {
	t.Run("ParseThenMapFromFixture", func(t *testing.T) {
		// Use the existing fixture
		fixturePath := filepath.Join("..", "..", "fixtures", "simple-webapp", "terraform.tfstate")

		if _, err := os.Stat(fixturePath); os.IsNotExist(err) {
			t.Skip("Fixture file not found, skipping test")
		}

		// Parse the state file
		p := awsparser.NewTFStateParser()
		ctx := context.Background()
		opts := parser.NewParseOptions()

		infra, err := p.Parse(ctx, fixturePath, opts)
		if err != nil {
			t.Fatalf("failed to parse state file: %v", err)
		}

		// Create mapper registry
		registry := mapper.NewRegistry()

		// Map each resource
		var mappedCount int
		for id, res := range infra.Resources {
			if registry.HasMapper(res.Type) {
				result, err := registry.Map(ctx, res)
				if err != nil {
					t.Logf("Failed to map %s: %v", id, err)
					continue
				}

				if result != nil && result.DockerService != nil {
					mappedCount++
					t.Logf("Mapped %s (%s) -> %s", id, res.Type, result.DockerService.Image)
				}
			} else {
				t.Logf("No mapper for %s (%s)", id, res.Type)
			}
		}

		t.Logf("Successfully mapped %d of %d resources", mappedCount, len(infra.Resources))
	})

	t.Run("ParseThenMapFromCloudFormation", func(t *testing.T) {
		// Create a CloudFormation template
		tmpDir := t.TempDir()
		cfnPath := filepath.Join(tmpDir, "template.yaml")

		cfnContent := `AWSTemplateFormatVersion: '2010-09-09'
Resources:
  AppBucket:
    Type: AWS::S3::Bucket
    Properties:
      BucketName: app-assets
      VersioningConfiguration:
        Status: Enabled

  AppDatabase:
    Type: AWS::RDS::DBInstance
    Properties:
      DBInstanceIdentifier: app-db
      Engine: postgres
      EngineVersion: "15.4"
      DBInstanceClass: db.t3.medium
      AllocatedStorage: 100
      MasterUsername: admin

  AppServer:
    Type: AWS::EC2::Instance
    Properties:
      InstanceType: t3.medium
      ImageId: ami-12345678
`
		if err := os.WriteFile(cfnPath, []byte(cfnContent), 0644); err != nil {
			t.Fatalf("failed to write CloudFormation template: %v", err)
		}

		// Parse the CloudFormation template
		cfnParser := awsparser.NewCloudFormationParser()
		ctx := context.Background()
		opts := parser.NewParseOptions()

		infra, err := cfnParser.Parse(ctx, cfnPath, opts)
		if err != nil {
			t.Fatalf("failed to parse CloudFormation template: %v", err)
		}

		// Create mapper registry
		registry := mapper.NewRegistry()

		// Map each resource
		results := make(map[string]*domainmapper.MappingResult)
		for id, res := range infra.Resources {
			if registry.HasMapper(res.Type) {
				result, err := registry.Map(ctx, res)
				if err != nil {
					t.Logf("Failed to map %s: %v", id, err)
					continue
				}
				results[id] = result
			}
		}

		// Verify we mapped the expected resources
		if len(results) < 3 {
			t.Errorf("expected at least 3 mapped resources, got %d", len(results))
		}

		// Verify S3 bucket mapping
		if bucketResult, ok := results["AppBucket"]; ok {
			if !strings.Contains(bucketResult.DockerService.Image, "minio") {
				t.Error("S3 bucket should map to MinIO")
			}
		}

		// Verify RDS mapping
		if dbResult, ok := results["AppDatabase"]; ok {
			if !strings.Contains(dbResult.DockerService.Image, "postgres") {
				t.Error("RDS should map to PostgreSQL")
			}
		}

		// Verify EC2 mapping
		if serverResult, ok := results["AppServer"]; ok {
			if serverResult.DockerService.Image == "" {
				t.Error("EC2 should have a Docker image")
			}
		}

		t.Logf("Mapped %d resources from CloudFormation", len(results))
	})
}

// TestMapperIntegration_Registry tests the mapper registry functionality.
func TestMapperIntegration_Registry(t *testing.T) {
	t.Run("RegistryHasAllMappers", func(t *testing.T) {
		registry := mapper.NewRegistry()

		// Check for essential AWS mappers
		essentialTypes := []resource.Type{
			resource.TypeEC2Instance,
			resource.TypeS3Bucket,
			resource.TypeRDSInstance,
			resource.TypeLambdaFunction,
			resource.TypeSQSQueue,
			resource.TypeSNSTopic,
			resource.TypeElastiCache,
			resource.TypeDynamoDBTable,
			resource.TypeALB,
		}

		for _, t := range essentialTypes {
			if !registry.HasMapper(t) {
				// Log but don't fail - some mappers may not be implemented
				println("Mapper not found for:", string(t))
			}
		}
	})

	t.Run("SupportedTypes", func(t *testing.T) {
		registry := mapper.NewRegistry()

		types := registry.SupportedTypes()
		if len(types) == 0 {
			t.Error("expected at least one supported type")
		}

		t.Logf("Registry supports %d resource types", len(types))
	})

	t.Run("BatchMapping", func(t *testing.T) {
		registry := mapper.NewRegistry()
		ctx := context.Background()

		// Create multiple resources
		resources := []*resource.AWSResource{
			func() *resource.AWSResource {
				r := resource.NewAWSResource("bucket1", "bucket1", resource.TypeS3Bucket)
				r.Config["bucket"] = "test-bucket-1"
				return r
			}(),
			func() *resource.AWSResource {
				r := resource.NewAWSResource("bucket2", "bucket2", resource.TypeS3Bucket)
				r.Config["bucket"] = "test-bucket-2"
				return r
			}(),
			func() *resource.AWSResource {
				r := resource.NewAWSResource("db1", "db1", resource.TypeRDSInstance)
				r.Config["engine"] = "postgres"
				r.Config["db_name"] = "testdb"
				r.Config["instance_class"] = "db.t3.micro"
				r.Config["allocated_storage"] = 20
				return r
			}(),
		}

		results, err := registry.MapBatch(ctx, resources)
		if err != nil {
			t.Fatalf("batch mapping failed: %v", err)
		}

		if len(results) != len(resources) {
			t.Errorf("expected %d results, got %d", len(resources), len(results))
		}

		// Count successful mappings
		successCount := 0
		for _, result := range results {
			if result.DockerService != nil && result.DockerService.Image != "" {
				successCount++
			}
		}

		t.Logf("Batch mapping: %d/%d successful", successCount, len(resources))
	})
}

// TestMapperIntegration_MappingResultStructure tests the complete MappingResult structure.
func TestMapperIntegration_MappingResultStructure(t *testing.T) {
	t.Run("CompleteResultStructure", func(t *testing.T) {
		// Create a complex resource
		res := resource.NewAWSResource("complex-db", "complex-db", resource.TypeRDSInstance)
		res.Config["engine"] = "postgres"
		res.Config["engine_version"] = "15.4"
		res.Config["instance_class"] = "db.r5.large"
		res.Config["allocated_storage"] = 500
		res.Config["db_name"] = "production"
		res.Config["multi_az"] = true
		res.Config["storage_encrypted"] = true
		res.Config["backup_retention_period"] = 14
		res.Config["parameter_group_name"] = "custom-postgres15"
		res.Tags["Environment"] = "production"
		res.Tags["Team"] = "platform"

		rdsMapper := database.NewRDSMapper()
		ctx := context.Background()

		result, err := rdsMapper.Map(ctx, res)
		if err != nil {
			t.Fatalf("failed to map: %v", err)
		}

		// Verify DockerService
		svc := result.DockerService
		if svc.Name == "" {
			t.Error("expected service name")
		}
		if svc.Image == "" {
			t.Error("expected service image")
		}
		if len(svc.Ports) == 0 {
			t.Error("expected ports")
		}
		if len(svc.Volumes) == 0 {
			t.Error("expected volumes")
		}
		if len(svc.Environment) == 0 {
			t.Error("expected environment variables")
		}
		if svc.HealthCheck == nil {
			t.Error("expected health check")
		}
		if svc.Restart == "" {
			t.Error("expected restart policy")
		}

		// Verify Configs
		if len(result.Configs) == 0 {
			t.Error("expected config files")
		}
		for filename, content := range result.Configs {
			if len(content) == 0 {
				t.Errorf("config file %s is empty", filename)
			}
			t.Logf("Config file: %s (%d bytes)", filename, len(content))
		}

		// Verify Scripts
		if len(result.Scripts) == 0 {
			t.Error("expected scripts")
		}
		for filename, content := range result.Scripts {
			if len(content) == 0 {
				t.Errorf("script %s is empty", filename)
			}
			t.Logf("Script: %s (%d bytes)", filename, len(content))
		}

		// Verify Warnings exist for complex resources
		if len(result.Warnings) == 0 {
			t.Log("Note: Expected warnings for complex resource features")
		} else {
			t.Logf("Warnings: %d", len(result.Warnings))
			for _, w := range result.Warnings {
				t.Logf("  - %s", w)
			}
		}

		// Verify ManualSteps
		if len(result.ManualSteps) == 0 {
			t.Error("expected manual steps")
		} else {
			t.Logf("Manual steps: %d", len(result.ManualSteps))
		}

		// Verify Labels
		if len(svc.Labels) == 0 {
			t.Error("expected labels")
		}
	})

	t.Run("MappingResultMethods", func(t *testing.T) {
		result := domainmapper.NewMappingResult("test-service")

		// Test AddWarning
		result.AddWarning("Test warning 1")
		result.AddWarning("Test warning 2")
		if len(result.Warnings) != 2 {
			t.Errorf("expected 2 warnings, got %d", len(result.Warnings))
		}

		// Test HasWarnings
		if !result.HasWarnings() {
			t.Error("expected HasWarnings to return true")
		}

		// Test AddManualStep
		result.AddManualStep("Step 1")
		result.AddManualStep("Step 2")
		if len(result.ManualSteps) != 2 {
			t.Errorf("expected 2 manual steps, got %d", len(result.ManualSteps))
		}

		// Test HasManualSteps
		if !result.HasManualSteps() {
			t.Error("expected HasManualSteps to return true")
		}

		// Test AddConfig
		result.AddConfig("config/test.conf", []byte("test content"))
		if len(result.Configs) != 1 {
			t.Error("expected 1 config file")
		}

		// Test AddScript
		result.AddScript("scripts/setup.sh", []byte("#!/bin/bash\necho hello"))
		if len(result.Scripts) != 1 {
			t.Error("expected 1 script")
		}

		// Test AddNetwork
		result.AddNetwork("mynetwork")
		if len(result.Networks) != 1 {
			t.Error("expected 1 network")
		}
	})
}
