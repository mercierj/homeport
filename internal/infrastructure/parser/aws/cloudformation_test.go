package aws

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeport/homeport/internal/domain/parser"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestCloudFormationParser_Provider(t *testing.T) {
	p := NewCloudFormationParser()
	if p.Provider() != resource.ProviderAWS {
		t.Errorf("expected provider AWS, got %s", p.Provider())
	}
}

func TestCloudFormationParser_SupportedFormats(t *testing.T) {
	p := NewCloudFormationParser()
	formats := p.SupportedFormats()
	if len(formats) != 1 || formats[0] != parser.FormatCloudFormation {
		t.Errorf("expected CloudFormation format, got %v", formats)
	}
}

func TestCloudFormationParser_AutoDetect(t *testing.T) {
	// Create a temporary CloudFormation template
	tmpDir := t.TempDir()
	cfnPath := filepath.Join(tmpDir, "template.yaml")

	cfnContent := `AWSTemplateFormatVersion: '2010-09-09'
Description: Test CloudFormation template

Resources:
  MyBucket:
    Type: AWS::S3::Bucket
    Properties:
      BucketName: my-test-bucket

  MyInstance:
    Type: AWS::EC2::Instance
    Properties:
      InstanceType: t3.micro
      ImageId: ami-12345678
`
	if err := os.WriteFile(cfnPath, []byte(cfnContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	p := NewCloudFormationParser()
	canHandle, confidence := p.AutoDetect(cfnPath)

	if !canHandle {
		t.Error("expected parser to handle CloudFormation template")
	}
	if confidence < 0.8 {
		t.Errorf("expected high confidence, got %f", confidence)
	}
}

func TestCloudFormationParser_AutoDetect_NonCFN(t *testing.T) {
	// Create a non-CloudFormation YAML file
	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "config.yaml")

	yamlContent := `name: test
version: 1.0
settings:
  debug: true
`
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	p := NewCloudFormationParser()
	canHandle, _ := p.AutoDetect(yamlPath)

	if canHandle {
		t.Error("expected parser to not handle non-CloudFormation YAML")
	}
}

func TestCloudFormationParser_Parse(t *testing.T) {
	// Create a temporary CloudFormation template
	tmpDir := t.TempDir()
	cfnPath := filepath.Join(tmpDir, "template.yaml")

	cfnContent := `AWSTemplateFormatVersion: '2010-09-09'
Description: Test CloudFormation template

Parameters:
  Environment:
    Type: String
    Default: production

Resources:
  MyBucket:
    Type: AWS::S3::Bucket
    Properties:
      BucketName: my-test-bucket
      VersioningConfiguration:
        Status: Enabled
      Tags:
        - Key: Environment
          Value: !Ref Environment

  MyDatabase:
    Type: AWS::RDS::DBInstance
    Properties:
      DBInstanceIdentifier: my-db
      Engine: postgres
      DBInstanceClass: db.t3.micro
      AllocatedStorage: 20
      MasterUsername: admin
    DependsOn: MyBucket

  MyQueue:
    Type: AWS::SQS::Queue
    Properties:
      QueueName: my-queue
      VisibilityTimeout: 30

  MyFunction:
    Type: AWS::Lambda::Function
    Properties:
      FunctionName: my-function
      Runtime: nodejs18.x
      Handler: index.handler
      Role: arn:aws:iam::123456789012:role/lambda-role
`
	if err := os.WriteFile(cfnPath, []byte(cfnContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	p := NewCloudFormationParser()
	ctx := context.Background()
	opts := parser.NewParseOptions()

	infra, err := p.Parse(ctx, cfnPath, opts)
	if err != nil {
		t.Fatalf("failed to parse CloudFormation template: %v", err)
	}

	// Check that we got the expected resources
	if len(infra.Resources) != 4 {
		t.Errorf("expected 4 resources, got %d", len(infra.Resources))
	}

	// Check S3 bucket
	bucket, exists := infra.Resources["MyBucket"]
	if !exists {
		t.Error("expected MyBucket resource")
	} else {
		if bucket.Type != resource.TypeS3Bucket {
			t.Errorf("expected S3Bucket type, got %s", bucket.Type)
		}
	}

	// Check RDS instance
	db, exists := infra.Resources["MyDatabase"]
	if !exists {
		t.Error("expected MyDatabase resource")
	} else {
		if db.Type != resource.TypeRDSInstance {
			t.Errorf("expected RDSInstance type, got %s", db.Type)
		}
		// Check dependency
		hasDep := false
		for _, dep := range db.Dependencies {
			if dep == "MyBucket" {
				hasDep = true
				break
			}
		}
		if !hasDep {
			t.Error("expected MyDatabase to depend on MyBucket")
		}
	}

	// Check SQS queue
	queue, exists := infra.Resources["MyQueue"]
	if !exists {
		t.Error("expected MyQueue resource")
	} else {
		if queue.Type != resource.TypeSQSQueue {
			t.Errorf("expected SQSQueue type, got %s", queue.Type)
		}
	}

	// Check Lambda function
	fn, exists := infra.Resources["MyFunction"]
	if !exists {
		t.Error("expected MyFunction resource")
	} else {
		if fn.Type != resource.TypeLambdaFunction {
			t.Errorf("expected LambdaFunction type, got %s", fn.Type)
		}
	}
}

func TestCloudFormationParser_Parse_WithTypeFilter(t *testing.T) {
	tmpDir := t.TempDir()
	cfnPath := filepath.Join(tmpDir, "template.yaml")

	cfnContent := `AWSTemplateFormatVersion: '2010-09-09'
Resources:
  MyBucket:
    Type: AWS::S3::Bucket
    Properties:
      BucketName: my-bucket

  MyQueue:
    Type: AWS::SQS::Queue
    Properties:
      QueueName: my-queue
`
	if err := os.WriteFile(cfnPath, []byte(cfnContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	p := NewCloudFormationParser()
	ctx := context.Background()
	opts := parser.NewParseOptions().WithFilterTypes(resource.TypeS3Bucket)

	infra, err := p.Parse(ctx, cfnPath, opts)
	if err != nil {
		t.Fatalf("failed to parse CloudFormation template: %v", err)
	}

	if len(infra.Resources) != 1 {
		t.Errorf("expected 1 resource (filtered), got %d", len(infra.Resources))
	}

	if _, exists := infra.Resources["MyBucket"]; !exists {
		t.Error("expected MyBucket resource after filtering")
	}
}

func TestCloudFormationParser_Parse_Directory(t *testing.T) {
	tmpDir := t.TempDir()

	// Create multiple template files
	template1 := `AWSTemplateFormatVersion: '2010-09-09'
Resources:
  Bucket1:
    Type: AWS::S3::Bucket
`
	template2 := `AWSTemplateFormatVersion: '2010-09-09'
Resources:
  Bucket2:
    Type: AWS::S3::Bucket
`

	if err := os.WriteFile(filepath.Join(tmpDir, "template1.yaml"), []byte(template1), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "template2.yaml"), []byte(template2), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	p := NewCloudFormationParser()
	ctx := context.Background()
	opts := parser.NewParseOptions()

	infra, err := p.Parse(ctx, tmpDir, opts)
	if err != nil {
		t.Fatalf("failed to parse CloudFormation directory: %v", err)
	}

	if len(infra.Resources) != 2 {
		t.Errorf("expected 2 resources from directory, got %d", len(infra.Resources))
	}
}

func TestCloudFormationParser_Validate(t *testing.T) {
	tmpDir := t.TempDir()
	cfnPath := filepath.Join(tmpDir, "template.yaml")

	cfnContent := `AWSTemplateFormatVersion: '2010-09-09'
Resources:
  MyBucket:
    Type: AWS::S3::Bucket
`
	if err := os.WriteFile(cfnPath, []byte(cfnContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	p := NewCloudFormationParser()

	// Valid file
	if err := p.Validate(cfnPath); err != nil {
		t.Errorf("expected no error for valid file, got %v", err)
	}

	// Valid directory
	if err := p.Validate(tmpDir); err != nil {
		t.Errorf("expected no error for valid directory, got %v", err)
	}

	// Invalid path
	if err := p.Validate("/nonexistent/path"); err == nil {
		t.Error("expected error for invalid path")
	}
}

func TestMapCFNTypeToResourceType(t *testing.T) {
	tests := []struct {
		cfnType      string
		expectedType resource.Type
	}{
		{"AWS::EC2::Instance", resource.TypeEC2Instance},
		{"AWS::S3::Bucket", resource.TypeS3Bucket},
		{"AWS::RDS::DBInstance", resource.TypeRDSInstance},
		{"AWS::Lambda::Function", resource.TypeLambdaFunction},
		{"AWS::SQS::Queue", resource.TypeSQSQueue},
		{"AWS::SNS::Topic", resource.TypeSNSTopic},
		{"AWS::ElastiCache::CacheCluster", resource.TypeElastiCache},
		{"AWS::DynamoDB::Table", resource.TypeDynamoDBTable},
		{"AWS::Unknown::Resource", ""},
	}

	for _, tc := range tests {
		t.Run(tc.cfnType, func(t *testing.T) {
			result := mapCFNTypeToResourceType(tc.cfnType)
			if result != tc.expectedType {
				t.Errorf("expected %s, got %s", tc.expectedType, result)
			}
		})
	}
}

func TestToSnakeCase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"BucketName", "bucket_name"},
		{"InstanceType", "instance_type"},
		{"VpcId", "vpc_id"},
		{"DBInstanceIdentifier", "d_b_instance_identifier"},
		{"AllocatedStorage", "allocated_storage"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := toSnakeCase(tc.input)
			if result != tc.expected {
				t.Errorf("expected %s, got %s", tc.expected, result)
			}
		})
	}
}
