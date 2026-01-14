package aws_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeport/homeport/internal/domain/parser"
	"github.com/homeport/homeport/internal/domain/resource"
	awsparser "github.com/homeport/homeport/internal/infrastructure/parser/aws"
)

// TestTFStateParser_Integration tests the Terraform state file parser with real fixtures.
func TestTFStateParser_Integration(t *testing.T) {
	t.Run("ParseExistingFixture", func(t *testing.T) {
		// Use the existing fixture file
		fixturePath := filepath.Join("..", "..", "fixtures", "simple-webapp", "terraform.tfstate")

		// Skip if fixture doesn't exist
		if _, err := os.Stat(fixturePath); os.IsNotExist(err) {
			t.Skip("Fixture file not found, skipping test")
		}

		p := awsparser.NewTFStateParser()
		ctx := context.Background()
		opts := parser.NewParseOptions()

		infra, err := p.Parse(ctx, fixturePath, opts)
		if err != nil {
			t.Fatalf("failed to parse state file: %v", err)
		}

		// Verify infrastructure is not nil
		if infra == nil {
			t.Fatal("expected infrastructure to be non-nil")
		}

		// Verify resources were parsed
		if len(infra.Resources) == 0 {
			t.Fatal("expected resources to be parsed, got 0")
		}

		t.Logf("Parsed %d resources from terraform.tfstate", len(infra.Resources))

		// Verify expected resource types
		expectedResources := []struct {
			id           string
			expectedType resource.Type
		}{
			{"aws_instance.web", resource.TypeEC2Instance},
			{"aws_db_instance.postgres", resource.TypeRDSInstance},
			{"aws_s3_bucket.assets", resource.TypeS3Bucket},
			{"aws_lb.web", resource.TypeALB},
		}

		for _, expected := range expectedResources {
			res, err := infra.GetResource(expected.id)
			if err != nil {
				t.Errorf("expected resource %s to exist: %v", expected.id, err)
				continue
			}

			if res.Type != expected.expectedType {
				t.Errorf("resource %s: expected type %s, got %s", expected.id, expected.expectedType, res.Type)
			}

			t.Logf("Resource %s: type=%s, name=%s", expected.id, res.Type, res.Name)
		}
	})

	t.Run("ExtractMetadataAndAttributes", func(t *testing.T) {
		fixturePath := filepath.Join("..", "..", "fixtures", "simple-webapp", "terraform.tfstate")

		if _, err := os.Stat(fixturePath); os.IsNotExist(err) {
			t.Skip("Fixture file not found, skipping test")
		}

		p := awsparser.NewTFStateParser()
		ctx := context.Background()
		opts := parser.NewParseOptions()

		infra, err := p.Parse(ctx, fixturePath, opts)
		if err != nil {
			t.Fatalf("failed to parse state file: %v", err)
		}

		// Verify metadata extraction
		if infra.Metadata["terraform_version"] == "" {
			t.Log("terraform_version metadata not set")
		} else {
			t.Logf("Terraform version: %s", infra.Metadata["terraform_version"])
		}

		// Verify EC2 instance attributes
		ec2, err := infra.GetResource("aws_instance.web")
		if err != nil {
			t.Fatalf("failed to get EC2 instance: %v", err)
		}

		instanceType := ec2.GetConfigString("instance_type")
		if instanceType == "" {
			t.Error("expected instance_type to be set")
		} else {
			t.Logf("EC2 instance_type: %s", instanceType)
		}

		// Verify RDS instance attributes
		rds, err := infra.GetResource("aws_db_instance.postgres")
		if err != nil {
			t.Fatalf("failed to get RDS instance: %v", err)
		}

		engine := rds.GetConfigString("engine")
		if engine != "postgres" {
			t.Errorf("expected engine to be postgres, got %s", engine)
		}

		allocatedStorage := rds.GetConfigInt("allocated_storage")
		if allocatedStorage == 0 {
			t.Error("expected allocated_storage to be set")
		} else {
			t.Logf("RDS allocated_storage: %d", allocatedStorage)
		}

		// Verify S3 bucket attributes
		s3, err := infra.GetResource("aws_s3_bucket.assets")
		if err != nil {
			t.Fatalf("failed to get S3 bucket: %v", err)
		}

		bucketName := s3.GetConfigString("bucket")
		if bucketName == "" {
			t.Error("expected bucket name to be set")
		} else {
			t.Logf("S3 bucket name: %s", bucketName)
		}
	})

	t.Run("ExtractDependencies", func(t *testing.T) {
		fixturePath := filepath.Join("..", "..", "fixtures", "simple-webapp", "terraform.tfstate")

		if _, err := os.Stat(fixturePath); os.IsNotExist(err) {
			t.Skip("Fixture file not found, skipping test")
		}

		p := awsparser.NewTFStateParser()
		ctx := context.Background()
		opts := parser.NewParseOptions()

		infra, err := p.Parse(ctx, fixturePath, opts)
		if err != nil {
			t.Fatalf("failed to parse state file: %v", err)
		}

		// Check dependencies for EC2 instance
		ec2, err := infra.GetResource("aws_instance.web")
		if err != nil {
			t.Fatalf("failed to get EC2 instance: %v", err)
		}

		t.Logf("EC2 instance has %d dependencies: %v", len(ec2.Dependencies), ec2.Dependencies)

		// Check dependencies for RDS instance
		rds, err := infra.GetResource("aws_db_instance.postgres")
		if err != nil {
			t.Fatalf("failed to get RDS instance: %v", err)
		}

		t.Logf("RDS instance has %d dependencies: %v", len(rds.Dependencies), rds.Dependencies)
	})

	t.Run("ParseWithTypeFilter", func(t *testing.T) {
		fixturePath := filepath.Join("..", "..", "fixtures", "simple-webapp", "terraform.tfstate")

		if _, err := os.Stat(fixturePath); os.IsNotExist(err) {
			t.Skip("Fixture file not found, skipping test")
		}

		p := awsparser.NewTFStateParser()
		ctx := context.Background()
		opts := parser.NewParseOptions().WithFilterTypes(resource.TypeEC2Instance, resource.TypeS3Bucket)

		infra, err := p.Parse(ctx, fixturePath, opts)
		if err != nil {
			t.Fatalf("failed to parse state file: %v", err)
		}

		// Should only have EC2 and S3 resources
		for id, res := range infra.Resources {
			if res.Type != resource.TypeEC2Instance && res.Type != resource.TypeS3Bucket {
				t.Errorf("unexpected resource type %s for %s (filter should have excluded it)", res.Type, id)
			}
		}

		t.Logf("Filtered parsing returned %d resources", len(infra.Resources))
	})

	t.Run("ParseSyntheticState", func(t *testing.T) {
		// Create a synthetic state file for testing
		tmpDir := t.TempDir()
		statePath := filepath.Join(tmpDir, "terraform.tfstate")

		stateContent := `{
  "version": 4,
  "terraform_version": "1.6.0",
  "resources": [
    {
      "mode": "managed",
      "type": "aws_instance",
      "name": "test",
      "provider": "provider[\"registry.terraform.io/hashicorp/aws\"]",
      "instances": [
        {
          "schema_version": 1,
          "attributes": {
            "id": "i-1234567890abcdef0",
            "ami": "ami-12345678",
            "instance_type": "t3.large",
            "availability_zone": "us-west-2a",
            "tags": {
              "Name": "test-instance",
              "Environment": "test"
            }
          },
          "dependencies": []
        }
      ]
    },
    {
      "mode": "managed",
      "type": "aws_s3_bucket",
      "name": "data",
      "provider": "provider[\"registry.terraform.io/hashicorp/aws\"]",
      "instances": [
        {
          "schema_version": 0,
          "attributes": {
            "id": "test-data-bucket",
            "bucket": "test-data-bucket",
            "region": "us-west-2",
            "versioning": [{"enabled": true}]
          },
          "dependencies": []
        }
      ]
    },
    {
      "mode": "managed",
      "type": "aws_lambda_function",
      "name": "processor",
      "provider": "provider[\"registry.terraform.io/hashicorp/aws\"]",
      "instances": [
        {
          "schema_version": 0,
          "attributes": {
            "function_name": "data-processor",
            "runtime": "python3.11",
            "handler": "main.handler",
            "memory_size": 256,
            "timeout": 30
          },
          "dependencies": ["aws_s3_bucket.data"]
        }
      ]
    }
  ]
}`

		if err := os.WriteFile(statePath, []byte(stateContent), 0644); err != nil {
			t.Fatalf("failed to write state file: %v", err)
		}

		p := awsparser.NewTFStateParser()
		ctx := context.Background()
		opts := parser.NewParseOptions()

		infra, err := p.Parse(ctx, statePath, opts)
		if err != nil {
			t.Fatalf("failed to parse state file: %v", err)
		}

		// Verify resources
		if len(infra.Resources) != 3 {
			t.Errorf("expected 3 resources, got %d", len(infra.Resources))
		}

		// Verify EC2 instance
		ec2, err := infra.GetResource("aws_instance.test")
		if err != nil {
			t.Errorf("EC2 instance not found: %v", err)
		} else {
			if ec2.Type != resource.TypeEC2Instance {
				t.Errorf("expected EC2Instance type, got %s", ec2.Type)
			}
			if ec2.GetConfigString("instance_type") != "t3.large" {
				t.Errorf("expected instance_type t3.large, got %s", ec2.GetConfigString("instance_type"))
			}
		}

		// Verify Lambda function dependencies
		lambda, err := infra.GetResource("aws_lambda_function.processor")
		if err != nil {
			t.Errorf("Lambda function not found: %v", err)
		} else {
			if lambda.Type != resource.TypeLambdaFunction {
				t.Errorf("expected LambdaFunction type, got %s", lambda.Type)
			}
			if len(lambda.Dependencies) == 0 {
				t.Log("Lambda function dependencies not extracted")
			}
		}

		t.Logf("Successfully parsed synthetic state with %d resources", len(infra.Resources))
	})
}

// TestTFStateParser_AutoDetect tests the auto-detection functionality.
func TestTFStateParser_AutoDetect(t *testing.T) {
	t.Run("DetectStateFile", func(t *testing.T) {
		tmpDir := t.TempDir()
		statePath := filepath.Join(tmpDir, "terraform.tfstate")

		stateContent := `{
  "version": 4,
  "terraform_version": "1.6.0",
  "resources": [
    {
      "mode": "managed",
      "type": "aws_instance",
      "name": "test",
      "provider": "provider[\"registry.terraform.io/hashicorp/aws\"]",
      "instances": []
    }
  ]
}`

		if err := os.WriteFile(statePath, []byte(stateContent), 0644); err != nil {
			t.Fatalf("failed to write state file: %v", err)
		}

		p := awsparser.NewTFStateParser()
		canHandle, confidence := p.AutoDetect(statePath)

		if !canHandle {
			t.Error("expected parser to detect state file")
		}
		if confidence < 0.8 {
			t.Errorf("expected high confidence, got %f", confidence)
		}

		t.Logf("Auto-detected state file with confidence: %f", confidence)
	})

	t.Run("DetectDirectoryWithState", func(t *testing.T) {
		tmpDir := t.TempDir()
		statePath := filepath.Join(tmpDir, "terraform.tfstate")

		stateContent := `{
  "version": 4,
  "terraform_version": "1.6.0",
  "resources": [
    {
      "mode": "managed",
      "type": "aws_s3_bucket",
      "name": "test",
      "provider": "provider[\"registry.terraform.io/hashicorp/aws\"]",
      "instances": []
    }
  ]
}`

		if err := os.WriteFile(statePath, []byte(stateContent), 0644); err != nil {
			t.Fatalf("failed to write state file: %v", err)
		}

		p := awsparser.NewTFStateParser()
		canHandle, confidence := p.AutoDetect(tmpDir)

		if !canHandle {
			t.Error("expected parser to detect directory with state file")
		}

		t.Logf("Auto-detected directory with state file, confidence: %f", confidence)
	})

	t.Run("RejectNonAWSState", func(t *testing.T) {
		tmpDir := t.TempDir()
		statePath := filepath.Join(tmpDir, "terraform.tfstate")

		// State file with only GCP resources
		stateContent := `{
  "version": 4,
  "terraform_version": "1.6.0",
  "resources": [
    {
      "mode": "managed",
      "type": "google_compute_instance",
      "name": "test",
      "provider": "provider[\"registry.terraform.io/hashicorp/google\"]",
      "instances": []
    }
  ]
}`

		if err := os.WriteFile(statePath, []byte(stateContent), 0644); err != nil {
			t.Fatalf("failed to write state file: %v", err)
		}

		p := awsparser.NewTFStateParser()
		canHandle, _ := p.AutoDetect(statePath)

		if canHandle {
			t.Error("expected parser to reject non-AWS state file")
		}

		t.Log("Correctly rejected non-AWS state file")
	})
}

// TestTFStateParser_Validation tests the Validate method.
func TestTFStateParser_Validation(t *testing.T) {
	t.Run("ValidStateFile", func(t *testing.T) {
		fixturePath := filepath.Join("..", "..", "fixtures", "simple-webapp", "terraform.tfstate")

		if _, err := os.Stat(fixturePath); os.IsNotExist(err) {
			t.Skip("Fixture file not found, skipping test")
		}

		p := awsparser.NewTFStateParser()
		err := p.Validate(fixturePath)

		if err != nil {
			t.Errorf("expected valid state file, got error: %v", err)
		}
	})

	t.Run("InvalidPath", func(t *testing.T) {
		p := awsparser.NewTFStateParser()
		err := p.Validate("/nonexistent/path/terraform.tfstate")

		if err == nil {
			t.Error("expected error for invalid path")
		}
	})

	t.Run("InvalidExtension", func(t *testing.T) {
		tmpDir := t.TempDir()
		wrongFile := filepath.Join(tmpDir, "config.json")

		if err := os.WriteFile(wrongFile, []byte("{}"), 0644); err != nil {
			t.Fatalf("failed to write file: %v", err)
		}

		p := awsparser.NewTFStateParser()
		err := p.Validate(wrongFile)

		if err == nil {
			t.Error("expected error for wrong file extension")
		}
	})
}
