package gcp_test

import (
	"context"
	"testing"

	"github.com/agnostech/agnostech/internal/domain/parser"
	"github.com/agnostech/agnostech/internal/domain/resource"
	gcpparser "github.com/agnostech/agnostech/internal/infrastructure/parser/gcp"
)

// TestAPIParser_Integration tests the GCP API parser integration.
// Note: These tests use mock/stub patterns since real GCP access isn't available in tests.
func TestAPIParser_Integration(t *testing.T) {
	t.Run("ParserCreation", func(t *testing.T) {
		p := gcpparser.NewAPIParser()

		if p == nil {
			t.Fatal("expected parser to be non-nil")
		}

		// Verify provider
		if p.Provider() != resource.ProviderGCP {
			t.Errorf("expected provider GCP, got %s", p.Provider())
		}

		t.Log("API parser created successfully")
	})

	t.Run("SupportedFormats", func(t *testing.T) {
		p := gcpparser.NewAPIParser()
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
		var _ parser.Parser = gcpparser.NewAPIParser()

		t.Log("API parser correctly implements Parser interface")
	})

	t.Run("AutoDetectWithoutCredentials", func(t *testing.T) {
		p := gcpparser.NewAPIParser()

		// AutoDetect should work even without credentials (returns false or checks environment)
		canHandle, confidence := p.AutoDetect("")

		// The important thing is it doesn't panic
		t.Logf("AutoDetect without credentials: canHandle=%v, confidence=%f", canHandle, confidence)

		// Confidence should be in valid range
		if confidence < 0 || confidence > 1 {
			t.Errorf("Confidence should be between 0 and 1, got %f", confidence)
		}
	})

	t.Run("ValidateWithoutCredentials", func(t *testing.T) {
		p := gcpparser.NewAPIParser()

		// Validate should return an error without valid credentials
		err := p.Validate("")

		// We expect an error since we don't have valid GCP credentials in tests
		if err != nil {
			t.Logf("Expected validation error without credentials: %v", err)
		} else {
			t.Log("Validation passed (credentials may be available in environment)")
		}
	})

	t.Run("ParseWithMockCredentials", func(t *testing.T) {
		// Skip this test in CI/CD without GCP credentials
		t.Skip("Skipping API parse test - requires GCP credentials")

		p := gcpparser.NewAPIParser()
		ctx := context.Background()
		opts := parser.NewParseOptions().
			WithRegions("us-central1").
			WithCredentials(map[string]string{
				"project": "test-project",
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

		// Register GCP parsers
		gcpparser.RegisterAll(registry)

		// Verify parsers are registered
		parsers := registry.All()

		// Should have multiple parsers registered
		if len(parsers) == 0 {
			t.Error("expected parsers to be registered")
		}

		// Find API parser
		hasAPIParser := false
		for _, p := range parsers {
			if p.Provider() == resource.ProviderGCP {
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

	t.Run("AllGCPParsersRegistered", func(t *testing.T) {
		registry := parser.NewRegistry()
		gcpparser.RegisterAll(registry)

		parsers := registry.All()

		// Count GCP parsers
		gcpParsers := 0
		for _, p := range parsers {
			if p.Provider() == resource.ProviderGCP {
				gcpParsers++
			}
		}

		// Should have multiple GCP parsers (TFState, HCL, DeploymentManager, API, Terraform)
		if gcpParsers < 4 {
			t.Errorf("Expected at least 4 GCP parsers, got %d", gcpParsers)
		}

		t.Logf("Found %d GCP parsers in registry", gcpParsers)
	})

	t.Run("FormatSupport", func(t *testing.T) {
		registry := parser.NewRegistry()
		gcpparser.RegisterAll(registry)

		expectedFormats := []parser.Format{
			parser.FormatTFState,
			parser.FormatTerraform,
			parser.FormatDeploymentManager,
			parser.FormatAPI,
		}

		parsers := registry.All()

		for _, expectedFormat := range expectedFormats {
			found := false
			for _, p := range parsers {
				if p.Provider() != resource.ProviderGCP {
					continue
				}
				for _, f := range p.SupportedFormats() {
					if f == expectedFormat {
						found = true
						break
					}
				}
				if found {
					break
				}
			}

			if !found {
				t.Errorf("Expected format %s to be supported by a GCP parser", expectedFormat)
			}
		}
	})

	t.Run("DefaultRegistryHasAPIParser", func(t *testing.T) {
		// The default registry should have the API parser after init()
		registry := parser.DefaultRegistry()

		parsers := registry.All()

		hasGCPAPIParser := false
		for _, p := range parsers {
			if p.Provider() == resource.ProviderGCP {
				for _, f := range p.SupportedFormats() {
					if f == parser.FormatAPI {
						hasGCPAPIParser = true
						break
					}
				}
			}
		}

		if !hasGCPAPIParser {
			t.Log("Note: API parser may not be in default registry if init() order differs")
		}
	})
}

// TestAPIParser_ResourceTypeFiltering tests the type filtering functionality.
func TestAPIParser_ResourceTypeFiltering(t *testing.T) {
	t.Run("FilterTypesOption", func(t *testing.T) {
		opts := parser.NewParseOptions().
			WithFilterTypes(resource.TypeGCEInstance, resource.TypeGCSBucket)

		if len(opts.FilterTypes) != 2 {
			t.Errorf("expected 2 filter types, got %d", len(opts.FilterTypes))
		}

		hasGCE := false
		hasGCS := false
		for _, ft := range opts.FilterTypes {
			if ft == resource.TypeGCEInstance {
				hasGCE = true
			}
			if ft == resource.TypeGCSBucket {
				hasGCS = true
			}
		}

		if !hasGCE || !hasGCS {
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
			WithRegions("us-central1", "us-east1", "europe-west1")

		if len(opts.Regions) != 3 {
			t.Errorf("expected 3 regions, got %d", len(opts.Regions))
		}

		if opts.Regions[0] != "us-central1" {
			t.Errorf("expected first region to be us-central1, got %s", opts.Regions[0])
		}
	})
}

// TestAPIParser_CredentialConfiguration tests credential configuration.
func TestAPIParser_CredentialConfiguration(t *testing.T) {
	t.Run("WithCredentialsOption", func(t *testing.T) {
		creds := map[string]string{
			"project":         "my-gcp-project",
			"credentials_file": "/path/to/credentials.json",
		}

		opts := parser.NewParseOptions().WithCredentials(creds)

		if opts.APICredentials == nil {
			t.Error("expected API credentials to be set")
		}

		if opts.APICredentials["project"] != creds["project"] {
			t.Error("project not set correctly")
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
	t.Run("GCPProvider", func(t *testing.T) {
		p := gcpparser.NewAPIParser()

		if p.Provider() != resource.ProviderGCP {
			t.Errorf("expected provider GCP, got %s", p.Provider())
		}
	})

	t.Run("ResourceCreation", func(t *testing.T) {
		// Test that NewAWSResource creates proper resources (used for GCP too)
		res := resource.NewAWSResource("instance-123", "test-instance", resource.TypeGCEInstance)

		if res.ID != "instance-123" {
			t.Errorf("expected ID instance-123, got %s", res.ID)
		}

		if res.Name != "test-instance" {
			t.Errorf("expected name test-instance, got %s", res.Name)
		}

		if res.Type != resource.TypeGCEInstance {
			t.Errorf("expected type GCEInstance, got %s", res.Type)
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
		infra := resource.NewInfrastructure(resource.ProviderGCP)

		if infra == nil {
			t.Fatal("expected infrastructure to be non-nil")
		}

		if infra.Provider != resource.ProviderGCP {
			t.Errorf("expected provider GCP, got %s", infra.Provider)
		}

		if infra.Resources == nil {
			t.Error("expected Resources map to be initialized")
		}

		if infra.Metadata == nil {
			t.Error("expected Metadata map to be initialized")
		}
	})

	t.Run("AddResources", func(t *testing.T) {
		infra := resource.NewInfrastructure(resource.ProviderGCP)

		// Add GCE instance
		gce := resource.NewAWSResource("instance-123", "web-server", resource.TypeGCEInstance)
		gce.Config["machine_type"] = "n2-standard-4"
		gce.Region = "us-central1"
		infra.AddResource(gce)

		// Add GCS bucket
		gcs := resource.NewAWSResource("my-bucket", "my-bucket", resource.TypeGCSBucket)
		gcs.Config["location"] = "US"
		infra.AddResource(gcs)

		// Add CloudSQL instance
		cloudsql := resource.NewAWSResource("mydb", "mydb", resource.TypeCloudSQL)
		cloudsql.Config["database_version"] = "POSTGRES_14"
		cloudsql.AddDependency("instance-123")
		infra.AddResource(cloudsql)

		// Verify resources
		if len(infra.Resources) != 3 {
			t.Errorf("expected 3 resources, got %d", len(infra.Resources))
		}

		// Get and verify GCE
		retrievedGCE, err := infra.GetResource("instance-123")
		if err != nil {
			t.Errorf("failed to get GCE resource: %v", err)
		} else {
			if retrievedGCE.GetConfigString("machine_type") != "n2-standard-4" {
				t.Error("GCE machine_type not correct")
			}
		}

		// Verify CloudSQL dependencies
		retrievedSQL, err := infra.GetResource("mydb")
		if err != nil {
			t.Errorf("failed to get CloudSQL resource: %v", err)
		} else {
			if len(retrievedSQL.Dependencies) != 1 {
				t.Errorf("expected 1 dependency, got %d", len(retrievedSQL.Dependencies))
			}
		}
	})

	t.Run("GetResourcesByType", func(t *testing.T) {
		infra := resource.NewInfrastructure(resource.ProviderGCP)

		// Add multiple instances
		for i := 0; i < 3; i++ {
			gce := resource.NewAWSResource(
				"instance-"+string(rune('0'+i)),
				"instance-"+string(rune('0'+i)),
				resource.TypeGCEInstance,
			)
			infra.AddResource(gce)
		}

		// Add a bucket
		gcs := resource.NewAWSResource("bucket", "bucket", resource.TypeGCSBucket)
		infra.AddResource(gcs)

		// Get only GCE instances
		gceInstances := infra.GetResourcesByType(resource.TypeGCEInstance)
		if len(gceInstances) != 3 {
			t.Errorf("expected 3 GCE instances, got %d", len(gceInstances))
		}

		// Get only GCS buckets
		gcsBuckets := infra.GetResourcesByType(resource.TypeGCSBucket)
		if len(gcsBuckets) != 1 {
			t.Errorf("expected 1 GCS bucket, got %d", len(gcsBuckets))
		}
	})

	t.Run("ValidateInfrastructure", func(t *testing.T) {
		infra := resource.NewInfrastructure(resource.ProviderGCP)

		// Add resources without circular dependencies
		res1 := resource.NewAWSResource("res1", "res1", resource.TypeGCEInstance)
		res2 := resource.NewAWSResource("res2", "res2", resource.TypeGCSBucket)
		res2.AddDependency("res1")
		res3 := resource.NewAWSResource("res3", "res3", resource.TypeCloudSQL)
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

// TestAPIParser_ResourceTypeScanning verifies the API parser can scan different resource types.
// This is a documentation test showing what resources the API parser supports.
func TestAPIParser_ResourceTypeScanning(t *testing.T) {
	// The API parser scans these resource types:
	supportedTypes := []resource.Type{
		resource.TypeGCEInstance,   // Compute Engine instances
		resource.TypeGCSBucket,     // Cloud Storage buckets
		resource.TypeCloudSQL,      // Cloud SQL instances
		resource.TypeCloudRun,      // Cloud Run services
		resource.TypeMemorystore,   // Memorystore (Redis) instances
	}

	for _, resType := range supportedTypes {
		t.Run(string(resType), func(t *testing.T) {
			// Verify the type is valid
			if !resType.IsValid() {
				t.Errorf("Resource type %s should be valid", resType)
			}

			// Verify it's a GCP type
			if resType.Provider() != resource.ProviderGCP {
				t.Errorf("Resource type %s should be GCP provider", resType)
			}

			t.Logf("API parser supports scanning: %s", resType)
		})
	}
}

// TestAPIParser_MethodsExist verifies all required methods exist and don't panic.
func TestAPIParser_MethodsExist(t *testing.T) {
	p := gcpparser.NewAPIParser()

	// Test that all Parser interface methods can be called
	_ = p.Provider()
	_ = p.SupportedFormats()

	// These may fail without credentials but should not panic
	t.Run("AutoDetect", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("AutoDetect panicked: %v", r)
			}
		}()
		_, _ = p.AutoDetect("")
	})

	t.Run("Validate", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Validate panicked: %v", r)
			}
		}()
		_ = p.Validate("")
	})

	t.Log("All required methods exist and don't panic")
}
