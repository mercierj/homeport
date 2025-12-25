# Terraform Parser

This package provides functionality to parse Terraform state files and HCL configuration files, converting them into the CloudExit infrastructure model.

## Features

- Parse Terraform state files (JSON format)
- Support for both v3 and v4 state file formats
- Parse Terraform HCL (.tf) files
- Extract resource configurations and attributes
- Build dependency graphs between resources
- Extract variables, locals, and outputs
- Map Terraform resource types to CloudExit resource types

## Supported AWS Resources

The parser supports mapping for the following AWS resource types:

### Compute
- `aws_instance` - EC2 instances
- `aws_lambda_function` - Lambda functions
- `aws_ecs_service` - ECS services
- `aws_ecs_task_definition` - ECS task definitions

### Storage
- `aws_s3_bucket` - S3 buckets
- `aws_ebs_volume` - EBS volumes

### Database
- `aws_db_instance` - RDS instances
- `aws_rds_cluster` - RDS clusters
- `aws_dynamodb_table` - DynamoDB tables
- `aws_elasticache_cluster` - ElastiCache clusters

### Networking
- `aws_lb` / `aws_alb` - Application/Network Load Balancers
- `aws_api_gateway_rest_api` - API Gateway
- `aws_route53_zone` - Route53 DNS zones
- `aws_cloudfront_distribution` - CloudFront distributions

### Security
- `aws_cognito_user_pool` - Cognito user pools
- `aws_secretsmanager_secret` - Secrets Manager secrets
- `aws_iam_role` - IAM roles

### Messaging
- `aws_sqs_queue` - SQS queues
- `aws_sns_topic` - SNS topics
- `aws_cloudwatch_event_rule` - EventBridge rules

## Usage

### Parse State File Only

```go
import "github.com/cloudexit/cloudexit/internal/infrastructure/parser"

// Parse a Terraform state file
infra, err := parser.ParseState("/path/to/terraform.tfstate")
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Found %d resources\n", len(infra.Resources))
```

### Parse HCL Files Only

```go
// Parse Terraform .tf files
infra, err := parser.ParseHCL("/path/to/terraform")
if err != nil {
    log.Fatal(err)
}
```

### Parse Both State and HCL

```go
// Build complete infrastructure model
infra, err := parser.BuildInfrastructure(
    "/path/to/terraform.tfstate",
    "/path/to/terraform",
)
if err != nil {
    log.Fatal(err)
}
```

### Auto-detect Terraform Project

```go
// Automatically find terraform.tfstate and .tf files
infra, err := parser.ParseTerraformProject("/path/to/project")
if err != nil {
    log.Fatal(err)
}
```

### Parse with Options

```go
opts := parser.ParseOptions{
    StatePath:           "/path/to/terraform.tfstate",
    TerraformDir:        "/path/to/terraform",
    ExtractDependencies: true,
    ValidateResources:   true,
}

infra, err := parser.ParseWithOptions(opts)
if err != nil {
    log.Fatal(err)
}
```

## Working with Resources

### Get Resources by Type

```go
// Get all EC2 instances
instances := infra.GetResourcesByType("aws_instance")
for _, inst := range instances {
    fmt.Printf("Instance: %s\n", inst.Name)
}
```

### Get Specific Resource

```go
res, err := infra.GetResource("aws_instance.web")
if err != nil {
    log.Fatal(err)
}

// Access attributes
instanceType := res.GetConfigString("instance_type")
ami := res.GetConfigString("ami")
```

### Work with Dependencies

```go
// Get resource dependencies
deps, err := infra.GetDependencies("aws_instance.web")
if err != nil {
    log.Fatal(err)
}

for _, dep := range deps {
    fmt.Printf("Depends on: %s\n", dep.ID)
}
```

### Access Resource Attributes

```go
// String attributes
engine := rds.GetConfigString("engine")

// Integer attributes
storage := rds.GetConfigInt("allocated_storage")

// Boolean attributes
multiAZ := rds.GetConfigBool("multi_az")

// Map attributes
tags := instance.Tags
if name, ok := tags["Name"]; ok {
    fmt.Printf("Name: %s\n", name)
}
```

## State File Format

The parser expects Terraform state files in JSON format (v3 or v4):

```json
{
  "version": 4,
  "terraform_version": "1.6.0",
  "resources": [
    {
      "mode": "managed",
      "type": "aws_instance",
      "name": "web",
      "provider": "provider[\"registry.terraform.io/hashicorp/aws\"]",
      "instances": [
        {
          "attributes": {
            "id": "i-1234567890abcdef0",
            "instance_type": "t3.medium",
            "ami": "ami-0c55b159cbfafe1f0",
            ...
          },
          "dependencies": ["aws_security_group.web"]
        }
      ]
    }
  ]
}
```

## HCL File Support

The parser can extract:

- **Resources**: Resource configurations and attributes
- **Variables**: Variable definitions with types, defaults, and descriptions
- **Locals**: Local value definitions
- **Outputs**: Output definitions

Example HCL:

```hcl
variable "instance_type" {
  description = "EC2 instance type"
  type        = string
  default     = "t3.medium"
}

locals {
  common_tags = {
    Environment = "production"
    ManagedBy   = "terraform"
  }
}

resource "aws_instance" "web" {
  ami           = "ami-0c55b159cbfafe1f0"
  instance_type = var.instance_type

  tags = local.common_tags
}

output "instance_ip" {
  description = "Public IP of the instance"
  value       = aws_instance.web.public_ip
}
```

## Dependency Detection

The parser detects dependencies in multiple ways:

1. **Explicit dependencies**: From the `dependencies` field in state files
2. **Implicit dependencies**: By analyzing resource attributes that reference other resources:
   - `vpc_id`
   - `subnet_id` / `subnet_ids`
   - `security_group_ids`
   - `db_subnet_group_name`
   - `load_balancer_arn`
   - And more...

## Testing

Run the tests:

```bash
cd internal/infrastructure/parser
go test -v
```

Test with the included fixture:

```bash
go test -v -run TestParseTerraformProject
```

## Architecture

The parser is organized into three main files:

- **terraform.go**: Main entry points and high-level parsing functions
- **tfstate.go**: Terraform state file parsing logic
- **hcl.go**: HCL file parsing logic

### Data Flow

```
terraform.tfstate ──┐
                    ├──> BuildInfrastructure() ──> Infrastructure
*.tf files ─────────┘
```

1. State file is parsed to extract deployed resources
2. HCL files are parsed to extract configuration context
3. Both are merged into a unified Infrastructure model
4. Dependencies are resolved and validated

## Limitations

- Only supports AWS resources currently (extensible to other providers)
- HCL parsing doesn't evaluate complex expressions (uses string representation)
- Module parsing is limited (doesn't recursively parse nested modules)
- Terraform workspaces are not explicitly supported

## Future Enhancements

- [ ] Support for Google Cloud Platform resources
- [ ] Support for Azure resources
- [ ] Recursive module parsing
- [ ] Terraform workspace support
- [ ] Remote state backend support
- [ ] Advanced expression evaluation in HCL
