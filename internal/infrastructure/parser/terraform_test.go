package parser

import (
	"path/filepath"
	"testing"

	"github.com/homeport/homeport/internal/domain/resource"
)

func TestParseState(t *testing.T) {
	// Path to test fixture
	fixturePath := filepath.Join("..", "..", "..", "test", "fixtures", "simple-webapp", "terraform.tfstate")

	infra, err := ParseState(fixturePath)
	if err != nil {
		t.Fatalf("Failed to parse state: %v", err)
	}

	// Verify infrastructure is not nil
	if infra == nil {
		t.Fatal("Expected infrastructure to be non-nil")
	}

	// Verify resources were parsed
	if len(infra.Resources) == 0 {
		t.Fatal("Expected resources to be parsed, got 0")
	}

	t.Logf("Parsed %d resources", len(infra.Resources))

	// Verify specific resources
	expectedResources := map[string]resource.Type{
		"aws_instance.web":            resource.TypeEC2Instance,
		"aws_db_instance.postgres":    resource.TypeRDSInstance,
		"aws_s3_bucket.assets":        resource.TypeS3Bucket,
		"aws_lb.web":                  resource.TypeALB,
		"aws_security_group.web":      resource.Type("aws_security_group"),
		"aws_security_group.database": resource.Type("aws_security_group"),
		"aws_security_group.alb":      resource.Type("aws_security_group"),
	}

	for id, expectedType := range expectedResources {
		res, err := infra.GetResource(id)
		if err != nil {
			t.Errorf("Expected resource %s to exist, but got error: %v", id, err)
			continue
		}

		if res.Type != expectedType {
			t.Errorf("Expected resource %s to have type %s, got %s", id, expectedType, res.Type)
		}

		t.Logf("Resource %s: type=%s, name=%s", id, res.Type, res.Name)
	}

	// Verify EC2 instance details
	ec2, err := infra.GetResource("aws_instance.web")
	if err != nil {
		t.Fatalf("Failed to get EC2 instance: %v", err)
	}

	if ec2.GetConfigString("instance_type") != "t3.medium" {
		t.Errorf("Expected instance_type to be t3.medium, got %s", ec2.GetConfigString("instance_type"))
	}

	if ec2.Region != "us-east-1a" && ec2.Region != "us-east-1" {
		t.Errorf("Expected region to be us-east-1, got %s", ec2.Region)
	}

	// Verify RDS instance details
	rds, err := infra.GetResource("aws_db_instance.postgres")
	if err != nil {
		t.Fatalf("Failed to get RDS instance: %v", err)
	}

	if rds.GetConfigString("engine") != "postgres" {
		t.Errorf("Expected engine to be postgres, got %s", rds.GetConfigString("engine"))
	}

	// Verify S3 bucket details
	s3, err := infra.GetResource("aws_s3_bucket.assets")
	if err != nil {
		t.Fatalf("Failed to get S3 bucket: %v", err)
	}

	if s3.GetConfigString("bucket") != "webapp-assets-prod" {
		t.Errorf("Expected bucket name to be webapp-assets-prod, got %s", s3.GetConfigString("bucket"))
	}

	// Verify dependencies were parsed
	if len(ec2.Dependencies) == 0 {
		t.Error("Expected EC2 instance to have dependencies")
	}

	t.Logf("EC2 instance dependencies: %v", ec2.Dependencies)
}

func TestParseHCL(t *testing.T) {
	// Path to test fixture directory
	fixtureDir := filepath.Join("..", "..", "..", "test", "fixtures", "simple-webapp")

	infra, err := ParseHCL(fixtureDir)
	if err != nil {
		t.Fatalf("Failed to parse HCL: %v", err)
	}

	if infra == nil {
		t.Fatal("Expected infrastructure to be non-nil")
	}

	// Check that resources were parsed
	if len(infra.Resources) == 0 {
		t.Fatal("Expected resources to be parsed from HCL")
	}

	t.Logf("Parsed %d resources from HCL", len(infra.Resources))

	// Check for variables in metadata
	if len(infra.Metadata) > 0 {
		t.Logf("Metadata entries: %d", len(infra.Metadata))
		for k, v := range infra.Metadata {
			t.Logf("  %s = %s", k, v)
		}
	}
}

func TestBuildInfrastructure(t *testing.T) {
	// Path to test fixtures
	statePath := filepath.Join("..", "..", "..", "test", "fixtures", "simple-webapp", "terraform.tfstate")
	tfDir := filepath.Join("..", "..", "..", "test", "fixtures", "simple-webapp")

	infra, err := BuildInfrastructure(statePath, tfDir)
	if err != nil {
		t.Fatalf("Failed to build infrastructure: %v", err)
	}

	if infra == nil {
		t.Fatal("Expected infrastructure to be non-nil")
	}

	// Verify resources were parsed
	if len(infra.Resources) == 0 {
		t.Fatal("Expected resources to be parsed")
	}

	t.Logf("Built infrastructure with %d resources", len(infra.Resources))

	// Verify metadata from HCL was merged
	if len(infra.Metadata) > 0 {
		t.Logf("Metadata entries: %d", len(infra.Metadata))
	}

	// Verify infrastructure is valid
	if err := infra.Validate(); err != nil {
		t.Errorf("Infrastructure validation failed: %v", err)
	}
}

func TestParseTerraformProject(t *testing.T) {
	projectDir := filepath.Join("..", "..", "..", "test", "fixtures", "simple-webapp")

	infra, err := ParseTerraformProject(projectDir)
	if err != nil {
		t.Fatalf("Failed to parse Terraform project: %v", err)
	}

	if infra == nil {
		t.Fatal("Expected infrastructure to be non-nil")
	}

	if len(infra.Resources) == 0 {
		t.Fatal("Expected resources to be parsed")
	}

	t.Logf("Parsed Terraform project with %d resources", len(infra.Resources))
}

func TestMapTerraformTypeToResourceType(t *testing.T) {
	tests := []struct {
		tfType   string
		expected resource.Type
	}{
		{"aws_instance", resource.TypeEC2Instance},
		{"aws_lambda_function", resource.TypeLambdaFunction},
		{"aws_s3_bucket", resource.TypeS3Bucket},
		{"aws_db_instance", resource.TypeRDSInstance},
		{"aws_lb", resource.TypeALB},
		{"aws_alb", resource.TypeALB},
		{"aws_sqs_queue", resource.TypeSQSQueue},
		{"aws_unknown_type", resource.Type("aws_unknown_type")},
	}

	for _, tt := range tests {
		t.Run(tt.tfType, func(t *testing.T) {
			result := mapTerraformTypeToResourceType(tt.tfType)
			if result != tt.expected {
				t.Errorf("mapTerraformTypeToResourceType(%s) = %s, want %s",
					tt.tfType, result, tt.expected)
			}
		})
	}
}

func TestParseWithOptions(t *testing.T) {
	statePath := filepath.Join("..", "..", "..", "test", "fixtures", "simple-webapp", "terraform.tfstate")
	tfDir := filepath.Join("..", "..", "..", "test", "fixtures", "simple-webapp")

	opts := ParseOptions{
		StatePath:           statePath,
		TerraformDir:        tfDir,
		ExtractDependencies: true,
		ValidateResources:   true,
	}

	infra, err := ParseWithOptions(opts)
	if err != nil {
		t.Fatalf("Failed to parse with options: %v", err)
	}

	if infra == nil {
		t.Fatal("Expected infrastructure to be non-nil")
	}

	if len(infra.Resources) == 0 {
		t.Fatal("Expected resources to be parsed")
	}

	t.Logf("Parsed infrastructure with %d resources", len(infra.Resources))
}

func TestResourceDependencies(t *testing.T) {
	statePath := filepath.Join("..", "..", "..", "test", "fixtures", "simple-webapp", "terraform.tfstate")

	infra, err := ParseState(statePath)
	if err != nil {
		t.Fatalf("Failed to parse state: %v", err)
	}

	// Check dependencies for EC2 instance
	ec2, err := infra.GetResource("aws_instance.web")
	if err != nil {
		t.Fatalf("Failed to get EC2 instance: %v", err)
	}

	if len(ec2.Dependencies) == 0 {
		t.Log("Warning: EC2 instance has no dependencies (might be expected)")
	} else {
		t.Logf("EC2 instance dependencies: %v", ec2.Dependencies)

		// Try to get dependency resources
		deps, err := infra.GetDependencies("aws_instance.web")
		if err != nil {
			t.Errorf("Failed to get dependencies: %v", err)
		} else {
			t.Logf("Found %d dependency resources", len(deps))
		}
	}
}
