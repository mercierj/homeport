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

// TestHCLParser_Integration tests the Terraform HCL parser with real .tf files.
func TestHCLParser_Integration(t *testing.T) {
	t.Run("ParseExistingFixture", func(t *testing.T) {
		// Use the existing fixture directory
		fixtureDir := filepath.Join("..", "..", "fixtures", "simple-webapp")

		// Skip if fixture doesn't exist
		if _, err := os.Stat(fixtureDir); os.IsNotExist(err) {
			t.Skip("Fixture directory not found, skipping test")
		}

		p := awsparser.NewHCLParser()
		ctx := context.Background()
		opts := parser.NewParseOptions()

		infra, err := p.Parse(ctx, fixtureDir, opts)
		if err != nil {
			t.Fatalf("failed to parse HCL files: %v", err)
		}

		if infra == nil {
			t.Fatal("expected infrastructure to be non-nil")
		}

		if len(infra.Resources) == 0 {
			t.Log("No resources parsed from HCL - this may be expected if parsing only extracts planned resources")
		} else {
			t.Logf("Parsed %d resources from HCL files", len(infra.Resources))
		}
	})

	t.Run("ParseSingleTFFile", func(t *testing.T) {
		tmpDir := t.TempDir()
		tfPath := filepath.Join(tmpDir, "main.tf")

		tfContent := `
provider "aws" {
  region = "us-east-1"
}

resource "aws_instance" "web" {
  ami           = "ami-0c55b159cbfafe1f0"
  instance_type = "t3.medium"

  tags = {
    Name        = "web-server"
    Environment = "production"
  }
}

resource "aws_s3_bucket" "assets" {
  bucket = "my-assets-bucket"

  tags = {
    Name = "assets-bucket"
  }
}

resource "aws_db_instance" "main" {
  identifier        = "mydb"
  engine            = "postgres"
  engine_version    = "15.4"
  instance_class    = "db.t3.medium"
  allocated_storage = 100
  username          = "admin"
}
`
		if err := os.WriteFile(tfPath, []byte(tfContent), 0644); err != nil {
			t.Fatalf("failed to write tf file: %v", err)
		}

		p := awsparser.NewHCLParser()
		ctx := context.Background()
		opts := parser.NewParseOptions()

		infra, err := p.Parse(ctx, tfPath, opts)
		if err != nil {
			t.Fatalf("failed to parse HCL file: %v", err)
		}

		// Verify resources
		expectedResources := map[string]resource.Type{
			"aws_instance.web":     resource.TypeEC2Instance,
			"aws_s3_bucket.assets": resource.TypeS3Bucket,
			"aws_db_instance.main": resource.TypeRDSInstance,
		}

		for id, expectedType := range expectedResources {
			res, err := infra.GetResource(id)
			if err != nil {
				t.Errorf("resource %s not found: %v", id, err)
				continue
			}
			if res.Type != expectedType {
				t.Errorf("resource %s: expected type %s, got %s", id, expectedType, res.Type)
			}
		}

		t.Logf("Parsed %d resources from single TF file", len(infra.Resources))
	})

	t.Run("ParseMultipleTFFiles", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create multiple .tf files as in a typical Terraform project
		files := map[string]string{
			"provider.tf": `
provider "aws" {
  region = "us-west-2"
}
`,
			"compute.tf": `
resource "aws_instance" "app" {
  ami           = "ami-12345678"
  instance_type = "t3.large"
}

resource "aws_lambda_function" "processor" {
  function_name = "data-processor"
  runtime       = "python3.11"
  handler       = "main.handler"
  role          = "arn:aws:iam::123456789012:role/lambda-role"
}
`,
			"storage.tf": `
resource "aws_s3_bucket" "data" {
  bucket = "data-bucket"
}

resource "aws_s3_bucket" "logs" {
  bucket = "logs-bucket"
}
`,
			"database.tf": `
resource "aws_db_instance" "primary" {
  identifier        = "primary-db"
  engine            = "mysql"
  instance_class    = "db.t3.medium"
  allocated_storage = 50
  username          = "admin"
}

resource "aws_elasticache_cluster" "cache" {
  cluster_id      = "app-cache"
  engine          = "redis"
  node_type       = "cache.t3.micro"
  num_cache_nodes = 1
}
`,
		}

		for filename, content := range files {
			path := filepath.Join(tmpDir, filename)
			if err := os.WriteFile(path, []byte(content), 0644); err != nil {
				t.Fatalf("failed to write %s: %v", filename, err)
			}
		}

		p := awsparser.NewHCLParser()
		ctx := context.Background()
		opts := parser.NewParseOptions()

		infra, err := p.Parse(ctx, tmpDir, opts)
		if err != nil {
			t.Fatalf("failed to parse HCL directory: %v", err)
		}

		expectedResourceCount := 6 // 1 instance + 1 lambda + 2 buckets + 1 rds + 1 cache
		if len(infra.Resources) != expectedResourceCount {
			t.Errorf("expected %d resources, got %d", expectedResourceCount, len(infra.Resources))
		}

		t.Logf("Parsed %d resources from multiple TF files", len(infra.Resources))
	})

	t.Run("VariableHandling", func(t *testing.T) {
		tmpDir := t.TempDir()

		variablesTF := `
variable "environment" {
  description = "Environment name"
  type        = string
  default     = "production"
}

variable "instance_type" {
  description = "EC2 instance type"
  type        = string
  default     = "t3.medium"
}

variable "db_instance_class" {
  description = "RDS instance class"
  type        = string
  default     = "db.t3.medium"
}
`
		mainTF := `
resource "aws_instance" "web" {
  ami           = "ami-12345678"
  instance_type = var.instance_type

  tags = {
    Environment = var.environment
  }
}
`

		if err := os.WriteFile(filepath.Join(tmpDir, "variables.tf"), []byte(variablesTF), 0644); err != nil {
			t.Fatalf("failed to write variables.tf: %v", err)
		}
		if err := os.WriteFile(filepath.Join(tmpDir, "main.tf"), []byte(mainTF), 0644); err != nil {
			t.Fatalf("failed to write main.tf: %v", err)
		}

		p := awsparser.NewHCLParser()
		ctx := context.Background()
		opts := parser.NewParseOptions()

		infra, err := p.Parse(ctx, tmpDir, opts)
		if err != nil {
			t.Fatalf("failed to parse HCL with variables: %v", err)
		}

		// Verify the instance resource exists
		instance, err := infra.GetResource("aws_instance.web")
		if err != nil {
			t.Errorf("instance resource not found: %v", err)
		} else {
			t.Logf("Instance resource parsed with config: %v", instance.Config)
		}
	})

	t.Run("ModuleResolution", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create a simple module structure
		modulesDir := filepath.Join(tmpDir, "modules", "vpc")
		if err := os.MkdirAll(modulesDir, 0755); err != nil {
			t.Fatalf("failed to create modules directory: %v", err)
		}

		// Module definition
		moduleMain := `
resource "aws_vpc" "main" {
  cidr_block = "10.0.0.0/16"

  tags = {
    Name = "main-vpc"
  }
}
`
		if err := os.WriteFile(filepath.Join(modulesDir, "main.tf"), []byte(moduleMain), 0644); err != nil {
			t.Fatalf("failed to write module main.tf: %v", err)
		}

		// Root module calling the VPC module
		rootMain := `
module "vpc" {
  source = "./modules/vpc"
}

resource "aws_instance" "web" {
  ami           = "ami-12345678"
  instance_type = "t3.micro"
}
`
		if err := os.WriteFile(filepath.Join(tmpDir, "main.tf"), []byte(rootMain), 0644); err != nil {
			t.Fatalf("failed to write root main.tf: %v", err)
		}

		p := awsparser.NewHCLParser()
		ctx := context.Background()
		opts := parser.NewParseOptions().WithFollowModules(true)

		infra, err := p.Parse(ctx, tmpDir, opts)
		if err != nil {
			t.Fatalf("failed to parse HCL with modules: %v", err)
		}

		// At minimum, we should have the root resource
		if len(infra.Resources) == 0 {
			t.Error("expected at least one resource")
		}

		t.Logf("Parsed %d resources with module resolution", len(infra.Resources))
	})

	t.Run("LocalsAndExpressions", func(t *testing.T) {
		tmpDir := t.TempDir()
		tfPath := filepath.Join(tmpDir, "main.tf")

		tfContent := `
locals {
  environment = "production"
  app_name    = "myapp"
  common_tags = {
    Environment = local.environment
    Application = local.app_name
    ManagedBy   = "terraform"
  }
}

resource "aws_s3_bucket" "assets" {
  bucket = "${local.app_name}-${local.environment}-assets"
}

resource "aws_instance" "web" {
  ami           = "ami-12345678"
  instance_type = "t3.micro"
}
`
		if err := os.WriteFile(tfPath, []byte(tfContent), 0644); err != nil {
			t.Fatalf("failed to write tf file: %v", err)
		}

		p := awsparser.NewHCLParser()
		ctx := context.Background()
		opts := parser.NewParseOptions()

		infra, err := p.Parse(ctx, tfPath, opts)
		if err != nil {
			t.Fatalf("failed to parse HCL with locals: %v", err)
		}

		if len(infra.Resources) != 2 {
			t.Errorf("expected 2 resources, got %d", len(infra.Resources))
		}

		t.Logf("Parsed %d resources with locals and expressions", len(infra.Resources))
	})
}

// TestHCLParser_AutoDetect tests the auto-detection functionality.
func TestHCLParser_AutoDetect(t *testing.T) {
	t.Run("DetectTFFile", func(t *testing.T) {
		tmpDir := t.TempDir()
		tfPath := filepath.Join(tmpDir, "main.tf")

		tfContent := `
provider "aws" {
  region = "us-east-1"
}

resource "aws_instance" "web" {
  ami           = "ami-12345678"
  instance_type = "t3.micro"
}
`
		if err := os.WriteFile(tfPath, []byte(tfContent), 0644); err != nil {
			t.Fatalf("failed to write tf file: %v", err)
		}

		p := awsparser.NewHCLParser()
		canHandle, confidence := p.AutoDetect(tfPath)

		if !canHandle {
			t.Error("expected parser to detect TF file")
		}
		if confidence < 0.7 {
			t.Errorf("expected reasonable confidence, got %f", confidence)
		}

		t.Logf("Auto-detected TF file with confidence: %f", confidence)
	})

	t.Run("DetectDirectoryWithTF", func(t *testing.T) {
		tmpDir := t.TempDir()

		tfContent := `
provider "aws" {
  region = "us-east-1"
}

resource "aws_s3_bucket" "test" {
  bucket = "test-bucket"
}
`
		if err := os.WriteFile(filepath.Join(tmpDir, "main.tf"), []byte(tfContent), 0644); err != nil {
			t.Fatalf("failed to write tf file: %v", err)
		}

		p := awsparser.NewHCLParser()
		canHandle, confidence := p.AutoDetect(tmpDir)

		if !canHandle {
			t.Error("expected parser to detect directory with TF files")
		}

		t.Logf("Auto-detected directory with TF files, confidence: %f", confidence)
	})

	t.Run("RejectNonAWSTF", func(t *testing.T) {
		tmpDir := t.TempDir()
		tfPath := filepath.Join(tmpDir, "main.tf")

		// TF file with only GCP resources
		tfContent := `
provider "google" {
  project = "my-project"
  region  = "us-central1"
}

resource "google_compute_instance" "default" {
  name         = "test"
  machine_type = "e2-medium"
  zone         = "us-central1-a"
}
`
		if err := os.WriteFile(tfPath, []byte(tfContent), 0644); err != nil {
			t.Fatalf("failed to write tf file: %v", err)
		}

		p := awsparser.NewHCLParser()
		canHandle, _ := p.AutoDetect(tfPath)

		if canHandle {
			t.Error("expected parser to reject non-AWS TF file")
		}

		t.Log("Correctly rejected non-AWS TF file")
	})
}

// TestHCLParser_Validation tests the Validate method.
func TestHCLParser_Validation(t *testing.T) {
	t.Run("ValidTFFile", func(t *testing.T) {
		tmpDir := t.TempDir()
		tfPath := filepath.Join(tmpDir, "main.tf")

		tfContent := `
provider "aws" {
  region = "us-east-1"
}

resource "aws_instance" "web" {
  ami           = "ami-12345678"
  instance_type = "t3.micro"
}
`
		if err := os.WriteFile(tfPath, []byte(tfContent), 0644); err != nil {
			t.Fatalf("failed to write tf file: %v", err)
		}

		p := awsparser.NewHCLParser()
		err := p.Validate(tfPath)

		if err != nil {
			t.Errorf("expected valid TF file, got error: %v", err)
		}
	})

	t.Run("ValidDirectory", func(t *testing.T) {
		tmpDir := t.TempDir()

		tfContent := `
resource "aws_s3_bucket" "test" {
  bucket = "test-bucket"
}
`
		if err := os.WriteFile(filepath.Join(tmpDir, "main.tf"), []byte(tfContent), 0644); err != nil {
			t.Fatalf("failed to write tf file: %v", err)
		}

		p := awsparser.NewHCLParser()
		err := p.Validate(tmpDir)

		if err != nil {
			t.Errorf("expected valid directory, got error: %v", err)
		}
	})

	t.Run("InvalidPath", func(t *testing.T) {
		p := awsparser.NewHCLParser()
		err := p.Validate("/nonexistent/path")

		if err == nil {
			t.Error("expected error for invalid path")
		}
	})

	t.Run("NonTFFile", func(t *testing.T) {
		tmpDir := t.TempDir()
		jsonPath := filepath.Join(tmpDir, "config.json")

		if err := os.WriteFile(jsonPath, []byte("{}"), 0644); err != nil {
			t.Fatalf("failed to write file: %v", err)
		}

		p := awsparser.NewHCLParser()
		err := p.Validate(jsonPath)

		if err == nil {
			t.Error("expected error for non-TF file")
		}
	})
}

// TestHCLParser_ResourceTypeMapping tests resource type mapping.
func TestHCLParser_ResourceTypeMapping(t *testing.T) {
	testCases := []struct {
		terraformType string
		resourceName  string
		expectedType  resource.Type
	}{
		{"aws_instance", "web", resource.TypeEC2Instance},
		{"aws_s3_bucket", "data", resource.TypeS3Bucket},
		{"aws_db_instance", "main", resource.TypeRDSInstance},
		{"aws_lambda_function", "processor", resource.TypeLambdaFunction},
		{"aws_sqs_queue", "tasks", resource.TypeSQSQueue},
		{"aws_sns_topic", "alerts", resource.TypeSNSTopic},
		{"aws_dynamodb_table", "users", resource.TypeDynamoDBTable},
		{"aws_elasticache_cluster", "cache", resource.TypeElastiCache},
		{"aws_lb", "main", resource.TypeALB},
		{"aws_alb", "app", resource.TypeALB},
	}

	for _, tc := range testCases {
		t.Run(tc.terraformType, func(t *testing.T) {
			tmpDir := t.TempDir()
			tfPath := filepath.Join(tmpDir, "main.tf")

			// Build minimal valid resource
			var properties string
			switch tc.terraformType {
			case "aws_instance":
				properties = `ami = "ami-12345678"
  instance_type = "t3.micro"`
			case "aws_db_instance":
				properties = `identifier = "test"
  engine = "postgres"
  instance_class = "db.t3.micro"
  allocated_storage = 20
  username = "admin"`
			case "aws_lambda_function":
				properties = `function_name = "test"
  runtime = "python3.9"
  handler = "index.handler"
  role = "arn:aws:iam::123456789012:role/test"`
			case "aws_dynamodb_table":
				properties = `name = "test"
  billing_mode = "PAY_PER_REQUEST"
  hash_key = "id"
  attribute {
    name = "id"
    type = "S"
  }`
			case "aws_elasticache_cluster":
				properties = `cluster_id = "test"
  engine = "redis"
  node_type = "cache.t3.micro"
  num_cache_nodes = 1`
			case "aws_lb", "aws_alb":
				properties = `name = "test"
  load_balancer_type = "application"`
			default:
				properties = `name = "test"`
			}

			tfContent := `
resource "` + tc.terraformType + `" "` + tc.resourceName + `" {
  ` + properties + `
}
`
			if err := os.WriteFile(tfPath, []byte(tfContent), 0644); err != nil {
				t.Fatalf("failed to write tf file: %v", err)
			}

			p := awsparser.NewHCLParser()
			ctx := context.Background()
			opts := parser.NewParseOptions()

			infra, err := p.Parse(ctx, tfPath, opts)
			if err != nil {
				t.Fatalf("failed to parse HCL: %v", err)
			}

			resourceID := tc.terraformType + "." + tc.resourceName
			res, err := infra.GetResource(resourceID)
			if err != nil {
				t.Fatalf("resource %s not found: %v", resourceID, err)
			}

			if res.Type != tc.expectedType {
				t.Errorf("expected type %s, got %s", tc.expectedType, res.Type)
			}
		})
	}
}
