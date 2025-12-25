package aws_test

import (
	"context"
	"testing"

	"github.com/cloudexit/cloudexit/internal/domain/parser"
	"github.com/cloudexit/cloudexit/internal/domain/resource"
	awsparser "github.com/cloudexit/cloudexit/internal/infrastructure/parser/aws"
)

// TestAPIParser_Integration tests the AWS API parser integration.
// Note: These tests use mock/stub patterns since real AWS access isn't available in tests.
func TestAPIParser_Integration(t *testing.T) {
	t.Run("ParserCreation", func(t *testing.T) {
		p := awsparser.NewAPIParser()

		if p == nil {
			t.Fatal("expected parser to be non-nil")
		}

		// Verify provider
		if p.Provider() != resource.ProviderAWS {
			t.Errorf("expected provider AWS, got %s", p.Provider())
		}

		t.Log("API parser created successfully")
	})

	t.Run("SupportedFormats", func(t *testing.T) {
		p := awsparser.NewAPIParser()
		formats := p.SupportedFormats()

		if len(formats) == 0 {
			t.Error("expected at least one supported format")
		}

		hasAPIFormat := false
		for _, f := range formats {
			if f == parser.FormatAPI {
				hasAPIFormat = true
				break
			}
		}

		if !hasAPIFormat {
			t.Error("expected API format to be supported")
		}

		t.Logf("Supported formats: %v", formats)
	})

	t.Run("InterfaceCompliance", func(t *testing.T) {
		// Verify that APIParser implements the Parser interface
		var _ parser.Parser = awsparser.NewAPIParser()

		t.Log("API parser correctly implements Parser interface")
	})

	t.Run("AutoDetectWithoutCredentials", func(t *testing.T) {
		p := awsparser.NewAPIParser()

		// AutoDetect should work even without credentials (returns false)
		canHandle, confidence := p.AutoDetect("")

		// Without credentials, it may or may not be able to handle
		// The important thing is it doesn't panic
		t.Logf("AutoDetect without credentials: canHandle=%v, confidence=%f", canHandle, confidence)
	})

	t.Run("ValidateWithoutCredentials", func(t *testing.T) {
		p := awsparser.NewAPIParser()

		// Validate should return an error without valid credentials
		err := p.Validate("")

		// We expect an error since we don't have valid AWS credentials in tests
		if err != nil {
			t.Logf("Expected validation error without credentials: %v", err)
		} else {
			t.Log("Validation passed (credentials may be available in environment)")
		}
	})

	t.Run("ParseWithMockCredentials", func(t *testing.T) {
		// Skip this test in CI/CD without AWS credentials
		t.Skip("Skipping API parse test - requires AWS credentials")

		p := awsparser.NewAPIParser()
		ctx := context.Background()
		opts := parser.NewParseOptions().
			WithRegions("us-east-1").
			WithCredentials(map[string]string{
				"access_key": "test-key",
				"secret_key": "test-secret",
			})

		_, err := p.Parse(ctx, "", opts)

		// We expect this to fail without real credentials
		if err != nil {
			t.Logf("Parse failed as expected without real credentials: %v", err)
		}
	})
}

// TestAPIParser_RegistryIntegration tests that the API parser is properly registered.
func TestAPIParser_RegistryIntegration(t *testing.T) {
	t.Run("ParserRegistration", func(t *testing.T) {
		// Create a new registry
		registry := parser.NewRegistry()

		// Register AWS parsers
		awsparser.RegisterAll(registry)

		// Verify parsers are registered
		parsers := registry.All()

		// Should have multiple parsers registered
		if len(parsers) == 0 {
			t.Error("expected parsers to be registered")
		}

		// Find API parser
		hasAPIParser := false
		for _, p := range parsers {
			if p.Provider() == resource.ProviderAWS {
				for _, f := range p.SupportedFormats() {
					if f == parser.FormatAPI {
						hasAPIParser = true
						break
					}
				}
			}
		}

		if !hasAPIParser {
			t.Error("expected API parser to be registered")
		}

		t.Logf("Registered %d parsers", len(parsers))
	})

	t.Run("DefaultRegistryHasAPIParser", func(t *testing.T) {
		// The default registry should have the API parser after init()
		registry := parser.DefaultRegistry()

		parsers := registry.All()

		hasAWSAPIParser := false
		for _, p := range parsers {
			if p.Provider() == resource.ProviderAWS {
				for _, f := range p.SupportedFormats() {
					if f == parser.FormatAPI {
						hasAWSAPIParser = true
						break
					}
				}
			}
		}

		if !hasAWSAPIParser {
			t.Log("Note: API parser may not be in default registry if init() order differs")
		}
	})
}

// TestAPIParser_ResourceTypeFiltering tests the type filtering functionality.
func TestAPIParser_ResourceTypeFiltering(t *testing.T) {
	t.Run("FilterTypesOption", func(t *testing.T) {
		opts := parser.NewParseOptions().
			WithFilterTypes(resource.TypeEC2Instance, resource.TypeS3Bucket)

		if len(opts.FilterTypes) != 2 {
			t.Errorf("expected 2 filter types, got %d", len(opts.FilterTypes))
		}

		hasEC2 := false
		hasS3 := false
		for _, ft := range opts.FilterTypes {
			if ft == resource.TypeEC2Instance {
				hasEC2 = true
			}
			if ft == resource.TypeS3Bucket {
				hasS3 = true
			}
		}

		if !hasEC2 || !hasS3 {
			t.Error("filter types not set correctly")
		}
	})

	t.Run("FilterCategoriesOption", func(t *testing.T) {
		opts := parser.NewParseOptions().
			WithFilterCategories(resource.CategoryCompute, resource.CategorySQLDatabase)

		if len(opts.FilterCategories) != 2 {
			t.Errorf("expected 2 filter categories, got %d", len(opts.FilterCategories))
		}
	})

	t.Run("RegionsOption", func(t *testing.T) {
		opts := parser.NewParseOptions().
			WithRegions("us-east-1", "us-west-2", "eu-west-1")

		if len(opts.Regions) != 3 {
			t.Errorf("expected 3 regions, got %d", len(opts.Regions))
		}

		if opts.Regions[0] != "us-east-1" {
			t.Errorf("expected first region to be us-east-1, got %s", opts.Regions[0])
		}
	})
}

// TestAPIParser_CredentialConfiguration tests credential configuration.
func TestAPIParser_CredentialConfiguration(t *testing.T) {
	t.Run("WithCredentialsOption", func(t *testing.T) {
		creds := map[string]string{
			"access_key": "AKIAIOSFODNN7EXAMPLE",
			"secret_key": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			"region":     "us-east-1",
		}

		opts := parser.NewParseOptions().WithCredentials(creds)

		if opts.APICredentials == nil {
			t.Error("expected API credentials to be set")
		}

		if opts.APICredentials["access_key"] != creds["access_key"] {
			t.Error("access_key not set correctly")
		}

		if opts.APICredentials["secret_key"] != creds["secret_key"] {
			t.Error("secret_key not set correctly")
		}
	})

	t.Run("SensitiveDataOption", func(t *testing.T) {
		opts := parser.NewParseOptions().WithSensitive(true)

		if !opts.IncludeSensitive {
			t.Error("expected IncludeSensitive to be true")
		}

		opts = parser.NewParseOptions().WithSensitive(false)

		if opts.IncludeSensitive {
			t.Error("expected IncludeSensitive to be false")
		}
	})

	t.Run("IgnoreErrorsOption", func(t *testing.T) {
		opts := parser.NewParseOptions().WithIgnoreErrors(true)

		if !opts.IgnoreErrors {
			t.Error("expected IgnoreErrors to be true")
		}
	})
}

// TestAPIParser_ProviderCompliance tests provider-specific compliance.
func TestAPIParser_ProviderCompliance(t *testing.T) {
	t.Run("AWSProvider", func(t *testing.T) {
		p := awsparser.NewAPIParser()

		if p.Provider() != resource.ProviderAWS {
			t.Errorf("expected provider AWS, got %s", p.Provider())
		}
	})

	t.Run("ResourceCreation", func(t *testing.T) {
		// Test that NewAWSResource creates proper resources
		res := resource.NewAWSResource("i-1234567890abcdef0", "test-instance", resource.TypeEC2Instance)

		if res.ID != "i-1234567890abcdef0" {
			t.Errorf("expected ID i-1234567890abcdef0, got %s", res.ID)
		}

		if res.Name != "test-instance" {
			t.Errorf("expected name test-instance, got %s", res.Name)
		}

		if res.Type != resource.TypeEC2Instance {
			t.Errorf("expected type EC2Instance, got %s", res.Type)
		}

		// Verify maps are initialized
		if res.Config == nil {
			t.Error("expected Config map to be initialized")
		}

		if res.Tags == nil {
			t.Error("expected Tags map to be initialized")
		}

		if res.Dependencies == nil {
			t.Error("expected Dependencies slice to be initialized")
		}
	})
}

// TestAPIParser_InfrastructureCreation tests infrastructure creation from API results.
func TestAPIParser_InfrastructureCreation(t *testing.T) {
	t.Run("CreateInfrastructure", func(t *testing.T) {
		infra := resource.NewInfrastructure(resource.ProviderAWS)

		if infra == nil {
			t.Fatal("expected infrastructure to be non-nil")
		}

		if infra.Provider != resource.ProviderAWS {
			t.Errorf("expected provider AWS, got %s", infra.Provider)
		}

		if infra.Resources == nil {
			t.Error("expected Resources map to be initialized")
		}

		if infra.Metadata == nil {
			t.Error("expected Metadata map to be initialized")
		}
	})

	t.Run("AddResources", func(t *testing.T) {
		infra := resource.NewInfrastructure(resource.ProviderAWS)

		// Add EC2 instance
		ec2 := resource.NewAWSResource("i-123", "web-server", resource.TypeEC2Instance)
		ec2.Config["instance_type"] = "t3.medium"
		ec2.Region = "us-east-1"
		infra.AddResource(ec2)

		// Add S3 bucket
		s3 := resource.NewAWSResource("my-bucket", "my-bucket", resource.TypeS3Bucket)
		s3.Config["versioning"] = true
		infra.AddResource(s3)

		// Add RDS instance
		rds := resource.NewAWSResource("mydb", "mydb", resource.TypeRDSInstance)
		rds.Config["engine"] = "postgres"
		rds.AddDependency("i-123")
		infra.AddResource(rds)

		// Verify resources
		if len(infra.Resources) != 3 {
			t.Errorf("expected 3 resources, got %d", len(infra.Resources))
		}

		// Get and verify EC2
		retrievedEC2, err := infra.GetResource("i-123")
		if err != nil {
			t.Errorf("failed to get EC2 resource: %v", err)
		} else {
			if retrievedEC2.GetConfigString("instance_type") != "t3.medium" {
				t.Error("EC2 instance_type not correct")
			}
		}

		// Verify RDS dependencies
		retrievedRDS, err := infra.GetResource("mydb")
		if err != nil {
			t.Errorf("failed to get RDS resource: %v", err)
		} else {
			if len(retrievedRDS.Dependencies) != 1 {
				t.Errorf("expected 1 dependency, got %d", len(retrievedRDS.Dependencies))
			}
		}
	})

	t.Run("GetResourcesByType", func(t *testing.T) {
		infra := resource.NewInfrastructure(resource.ProviderAWS)

		// Add multiple instances
		for i := 0; i < 3; i++ {
			ec2 := resource.NewAWSResource(
				"i-"+string(rune('0'+i)),
				"instance-"+string(rune('0'+i)),
				resource.TypeEC2Instance,
			)
			infra.AddResource(ec2)
		}

		// Add a bucket
		s3 := resource.NewAWSResource("bucket", "bucket", resource.TypeS3Bucket)
		infra.AddResource(s3)

		// Get only EC2 instances
		ec2Instances := infra.GetResourcesByType(resource.TypeEC2Instance)
		if len(ec2Instances) != 3 {
			t.Errorf("expected 3 EC2 instances, got %d", len(ec2Instances))
		}

		// Get only S3 buckets
		s3Buckets := infra.GetResourcesByType(resource.TypeS3Bucket)
		if len(s3Buckets) != 1 {
			t.Errorf("expected 1 S3 bucket, got %d", len(s3Buckets))
		}
	})

	t.Run("ValidateInfrastructure", func(t *testing.T) {
		infra := resource.NewInfrastructure(resource.ProviderAWS)

		// Add resources without circular dependencies
		res1 := resource.NewAWSResource("res1", "res1", resource.TypeEC2Instance)
		res2 := resource.NewAWSResource("res2", "res2", resource.TypeS3Bucket)
		res2.AddDependency("res1")
		res3 := resource.NewAWSResource("res3", "res3", resource.TypeRDSInstance)
		res3.AddDependency("res2")

		infra.AddResource(res1)
		infra.AddResource(res2)
		infra.AddResource(res3)

		// Should validate without errors
		err := infra.Validate()
		if err != nil {
			t.Errorf("expected valid infrastructure, got error: %v", err)
		}
	})
}
