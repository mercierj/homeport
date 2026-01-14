# Homeport CLI Reference

Complete reference documentation for the Homeport command-line interface.

## Overview

Homeport is a CLI tool that transforms cloud infrastructure from AWS, GCP, and Azure into self-hosted Docker stacks. This reference provides detailed documentation for all commands, flags, and configuration options.

```
homeport [command] [flags]
```

## Commands

| Command | Description |
|---------|-------------|
| `analyze` | Analyze cloud infrastructure from various sources |
| `migrate` | Generate self-hosted Docker stack from cloud infrastructure |
| `validate` | Validate generated stack configuration |
| `serve` | Start the Homeport web dashboard |
| `version` | Print version information |

---

## Global Flags

These flags are available for all commands:

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--config` | | `$HOME/.homeport.yaml` | Path to configuration file |
| `--verbose` | `-v` | `false` | Enable verbose output |
| `--quiet` | `-q` | `false` | Quiet mode (errors only) |

### Configuration File Locations

Homeport searches for configuration in the following order:

1. Path specified with `--config` flag
2. `.homeport.yaml` in current directory
3. `$HOME/.homeport.yaml`

---

## analyze

Analyze cloud infrastructure from Terraform, CloudFormation, ARM templates, or live API.

### Synopsis

```
homeport analyze <path> [flags]
```

### Description

The analyze command parses your cloud infrastructure configuration and generates a detailed analysis including:

- All discovered resources with types and names
- Resource dependencies
- Migration targets (which self-hosted service replaces each cloud service)
- Statistics by category (compute, database, storage, etc.)

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--output` | `-o` | `analysis.json` | Output file path (use `-` for stdout) |
| `--format` | `-f` | `json` | Output format: `json`, `yaml`, or `table` |
| `--source` | `-s` | auto-detect | Source type (see supported sources below) |
| `--profile` | `-p` | | AWS profile name (for aws-api source) |
| `--project` | | | GCP project ID (for gcp-api source) |
| `--region` | `-r` | | Region(s) to scan (can be repeated) |
| `--credentials` | | | Path to credentials file |

### Supported Sources

| Source | Description | Requirements |
|--------|-------------|--------------|
| `terraform` | Terraform state files and HCL | `*.tfstate`, `*.tf` files |
| `cloudformation` | AWS CloudFormation templates | `*.yaml`, `*.json`, `*.template` |
| `arm` | Azure Resource Manager templates | `*.json` ARM templates |
| `aws-api` | Live AWS infrastructure scanning | AWS credentials/profile |
| `gcp-api` | Live GCP infrastructure scanning | GCP credentials/project |
| `azure-api` | Live Azure infrastructure scanning | Azure credentials |

### Examples

```bash
# Auto-detect and analyze Terraform directory
homeport analyze ./infrastructure

# Analyze specific Terraform state file
homeport analyze terraform.tfstate

# Analyze with YAML output
homeport analyze ./infrastructure --format yaml

# Analyze CloudFormation templates
homeport analyze --source cloudformation ./templates

# Analyze ARM templates
homeport analyze --source arm ./arm-templates

# Analyze live AWS infrastructure
homeport analyze --source aws-api --profile production --region us-east-1

# Analyze multiple AWS regions
homeport analyze --source aws-api --region us-east-1 --region eu-west-1

# Analyze live GCP infrastructure
homeport analyze --source gcp-api --project my-project --region us-central1

# Analyze live Azure infrastructure
homeport analyze --source azure-api --region eastus

# Output as table to terminal
homeport analyze ./infrastructure --format table

# Output to stdout
homeport analyze ./infrastructure --output -
```

### Output Format

The analysis output includes:

```json
{
  "input_path": "/path/to/infrastructure",
  "resource_type": "terraform",
  "resources": [
    {
      "type": "aws_db_instance",
      "name": "main-db",
      "id": "aws_db_instance.main-db",
      "region": "us-east-1",
      "migrate_as": "postgres_container",
      "dependencies": ["aws_vpc.main"]
    }
  ],
  "statistics": {
    "total_resources": 15,
    "by_type": { "aws_db_instance": 2, "aws_s3_bucket": 3 },
    "by_region": { "us-east-1": 10, "eu-west-1": 5 },
    "migration": {
      "compute": 4,
      "database": 3,
      "storage": 5,
      "networking": 2,
      "security": 1
    }
  },
  "dependencies": [
    { "from": "aws_instance.web", "to": "aws_db_instance.main", "type": "depends_on" }
  ]
}
```

---

## migrate

Generate a complete self-hosted Docker stack from cloud infrastructure.

### Synopsis

```
homeport migrate <path> [flags]
```

### Description

The migrate command takes your cloud infrastructure configuration and generates a complete self-hosted stack including:

- Docker Compose configuration with all services
- Traefik reverse proxy setup with SSL
- Service-specific configurations
- Environment variable templates
- Data migration scripts
- Documentation

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--output` | `-o` | `./output` | Output directory path |
| `--domain` | `-d` | | Domain name for services |
| `--include-migration` | | `false` | Include data migration tools and scripts |
| `--include-monitoring` | | `false` | Include monitoring stack (Prometheus, Grafana) |
| `--consolidate` | | `false` | Consolidate similar resources into unified stacks |

### Examples

```bash
# Basic migration from Terraform state
homeport migrate terraform.tfstate

# Migrate with custom output directory
homeport migrate ./infrastructure --output ./my-stack

# Migrate with domain configuration
homeport migrate ./infrastructure --domain example.com

# Full migration with monitoring and migration tools
homeport migrate ./infrastructure \
  --output ./production-stack \
  --domain example.com \
  --include-migration \
  --include-monitoring

# Migrate with verbose output
homeport migrate ./infrastructure --domain example.com --verbose

# Migrate with stack consolidation (reduces container count)
homeport migrate ./infrastructure --consolidate

# Consolidated migration with all options
homeport migrate ./infrastructure \
  --consolidate \
  --output ./consolidated-stack \
  --domain example.com \
  --include-monitoring
```

### Output Structure

The migrate command generates the following directory structure:

```
output/
├── docker-compose.yml          # Main Docker Compose configuration
├── docker-compose.override.yml # Local development overrides
├── .env.example                # Environment variables template
├── README.md                   # Generated documentation
├── traefik/
│   ├── traefik.yml            # Main Traefik configuration
│   └── dynamic/               # Dynamic routing configuration
│       └── services.yml
├── configs/
│   ├── postgres/
│   │   └── init.sql           # Database initialization
│   ├── minio/
│   └── keycloak/
├── scripts/
│   ├── migrate-s3.sh          # S3 to MinIO migration
│   ├── migrate-rds.sh         # RDS to PostgreSQL migration
│   └── backup.sh              # Automated backup script
└── monitoring/                 # (if --include-monitoring)
    ├── prometheus.yml
    └── grafana/
        └── dashboards/
```

### Service Mappings

The migrate command automatically maps cloud services to self-hosted alternatives:

| Cloud Service | Self-Hosted Alternative |
|---------------|------------------------|
| AWS RDS / Azure SQL / Cloud SQL | PostgreSQL / MySQL |
| AWS S3 / Azure Blob / GCS | MinIO |
| AWS ElastiCache / Azure Cache | Redis |
| AWS DynamoDB / Azure CosmosDB | ScyllaDB |
| AWS Cognito / Azure AD B2C | Keycloak |
| AWS ALB / Azure LB / GCP LB | Traefik |
| AWS SQS / Azure Service Bus | RabbitMQ |
| AWS Lambda / Azure Functions | OpenFaaS |
| AWS EKS / AKS / GKE | K3s |

### Stack Consolidation

Use `--consolidate` to reduce container sprawl by grouping similar resources into unified stacks:

| Stack Type | Consolidated Resources | Self-Hosted Service |
|------------|----------------------|---------------------|
| Database | RDS, Cloud SQL, Azure SQL | Single PostgreSQL/MySQL |
| Cache | ElastiCache, Memorystore, Azure Cache | Single Redis |
| Messaging | SQS, SNS, Pub/Sub, Service Bus | Single RabbitMQ |
| Storage | S3, GCS, Blob Storage | Single MinIO |
| Auth | Cognito, Identity Platform, AD B2C | Single Keycloak |
| Secrets | Secrets Manager, Key Vault | Single Vault |
| Observability | CloudWatch, Cloud Monitoring | Prometheus + Grafana |
| Compute | Lambda, Cloud Functions | Single OpenFaaS |

**Example output with consolidation:**
```
Stack Consolidation Summary
────────────────────────────────────────────────────────
  Source resources:     24
  Consolidated stacks:  5
  Total services:       8
  Consolidation ratio:  3.0x reduction

  Stack breakdown:
    Database: 5 resources -> 1 service(s)
    Cache: 3 resources -> 1 service(s)
    Messaging: 8 resources -> 1 service(s)
    Storage: 4 resources -> 1 service(s)
    Passthrough: 4 resources (individual services)
```

For detailed documentation, see [Stack Consolidation](./stack-consolidation.md).

---

## validate

Validate generated stack configuration before deployment.

### Synopsis

```
homeport validate <path> [flags]
```

### Description

The validate command performs comprehensive checks on generated stacks:

- Docker Compose syntax validation
- Required files and directories verification
- Environment variable configuration
- Network configuration
- Volume mounts and permissions
- Service dependency validation
- Documentation completeness

### Examples

```bash
# Validate generated stack
homeport validate ./output

# Validate with verbose details
homeport validate ./output --verbose

# Validate quietly (only show errors)
homeport validate ./output --quiet
```

### Validation Checks

| Check | Description | Status |
|-------|-------------|--------|
| Docker Compose | Validates syntax and structure | Required |
| Traefik Config | Checks reverse proxy configuration | Optional |
| Environment Files | Validates .env.example exists | Recommended |
| Network Config | Checks network definitions | Required |
| Volume Config | Validates volume mounts | Required |
| Service Dependencies | Checks depends_on ordering | Required |
| Documentation | Verifies README.md exists | Recommended |

### Output Example

```
┌─────────────────────────────┬──────────┬─────────────────────────────────┐
│ Check                       │ Status   │ Message                         │
├─────────────────────────────┼──────────┼─────────────────────────────────┤
│ Docker Compose Configuration│ ✓ success│ Docker Compose file is valid    │
│ Traefik Configuration       │ ✓ success│ Traefik configuration is valid  │
│ Environment Files           │ ✓ success│ Environment configuration valid │
│ Network Configuration       │ ✓ success│ Network configuration is valid  │
│ Volume Configuration        │ ✓ success│ Volume configuration is valid   │
│ Service Dependencies        │ ✓ success│ Service dependencies are valid  │
│ Documentation               │ ✓ success│ Documentation is available      │
└─────────────────────────────┴──────────┴─────────────────────────────────┘

Validation completed successfully
```

---

## serve

Start the Homeport web dashboard for interactive infrastructure management.

### Synopsis

```
homeport serve [flags]
```

### Description

The serve command starts a web-based dashboard providing:

- Migration wizard for Terraform/CloudFormation/ARM
- Visual infrastructure mapping
- Stack deployment management
- Storage, database, and secrets management
- Real-time monitoring integration

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--port` | `-p` | `8080` | Port to serve on |
| `--host` | `-H` | `localhost` | Host to bind to |
| `--no-auth` | | `false` | Disable authentication (development mode) |

### Examples

```bash
# Start on default port (localhost:8080)
homeport serve

# Start on custom port
homeport serve --port 3000

# Listen on all interfaces (for remote access)
homeport serve --host 0.0.0.0

# Development mode without authentication
homeport serve --no-auth

# Combined options
homeport serve --host 0.0.0.0 --port 3000 --verbose
```

### Security Considerations

- By default, the server binds to `localhost` only
- Use `--host 0.0.0.0` carefully, only in trusted networks
- The `--no-auth` flag should only be used for local development
- For production, always use authentication and HTTPS

---

## version

Print version information.

### Synopsis

```
homeport version
```

### Description

Displays the current version, git commit hash, and build date.

### Output Example

```
Homeport v1.0.0
Commit: abc123def
Built: 2024-01-15T10:30:00Z
```

---

## Configuration File

Homeport can be configured via a YAML configuration file.

### Location

Configuration files are loaded from:
1. Path specified with `--config` flag
2. `.homeport.yaml` in current working directory
3. `$HOME/.homeport.yaml`

### Full Configuration Reference

```yaml
# Default output directory for generated stacks
output: ./output

# Default domain name for services
domain: example.com

# Output format for analysis (json, yaml, table)
format: json

# Include migration tools and scripts by default
include-migration: false

# Include monitoring stack (Prometheus, Grafana) by default
include-monitoring: false

# Verbose output
verbose: false

# Quiet mode (errors only)
quiet: false

# Custom configuration options
config:
  # Traefik version to use
  traefik_version: "v2.10"

  # Docker Compose version
  compose_version: "3.8"

  # Default timezone for containers
  timezone: "UTC"

  # Enable SSL by default
  ssl_enabled: true

  # SSL provider (letsencrypt, custom)
  ssl_provider: "letsencrypt"

  # ACME email for Let's Encrypt
  acme_email: "admin@example.com"

# Resource mappings (customize how cloud resources are migrated)
mappings:
  # EC2 instance to Docker service
  ec2:
    image_prefix: "app"
    restart_policy: "unless-stopped"

  # RDS to Docker database
  rds:
    postgres:
      image: "postgres:16-alpine"
    mysql:
      image: "mysql:8.0"

  # S3 to MinIO
  s3:
    image: "minio/minio:latest"
    enable_console: true

# Network configuration
network:
  name: "web"
  driver: "bridge"
  ipam:
    subnet: "172.20.0.0/16"

# Logging configuration
logging:
  driver: "json-file"
  options:
    max-size: "10m"
    max-file: "3"
```

### Environment Variables

All configuration options can also be set via environment variables with the `AGNOSTECH_` prefix:

| Environment Variable | Config Key |
|---------------------|------------|
| `AGNOSTECH_OUTPUT` | `output` |
| `AGNOSTECH_DOMAIN` | `domain` |
| `AGNOSTECH_FORMAT` | `format` |
| `AGNOSTECH_VERBOSE` | `verbose` |
| `AGNOSTECH_QUIET` | `quiet` |

---

## Exit Codes

| Code | Description |
|------|-------------|
| 0 | Success |
| 1 | General error |
| 2 | Invalid arguments |
| 3 | Configuration error |
| 4 | Input file not found |
| 5 | Parse error |
| 6 | Validation error |

---

## Common Workflows

### Complete Migration Workflow

```bash
# 1. Analyze infrastructure
homeport analyze ./terraform --format table

# 2. Generate self-hosted stack
homeport migrate ./terraform \
  --output ./my-stack \
  --domain myapp.com \
  --include-monitoring

# 3. Validate generated configuration
homeport validate ./my-stack --verbose

# 4. Deploy
cd my-stack
cp .env.example .env
# Edit .env with your values
docker network create web
docker compose up -d
```

### Multi-Region AWS Analysis

```bash
homeport analyze \
  --source aws-api \
  --profile production \
  --region us-east-1 \
  --region us-west-2 \
  --region eu-west-1 \
  --format table
```

### CI/CD Integration

```bash
#!/bin/bash
set -e

# Analyze and migrate
homeport migrate ./terraform \
  --output ./stack \
  --domain $DOMAIN \
  --quiet

# Validate
homeport validate ./stack --quiet

# Deploy (if validation passes)
cd stack && docker compose up -d
```

---

## Troubleshooting

### Common Issues

**CLI doesn't recognize commands**
```bash
# Ensure dependencies are installed
make deps
make build
```

**Import errors**
```bash
go mod tidy
```

**Permission issues**
```bash
chmod +x setup-cli.sh
./setup-cli.sh
```

**AWS credentials not found**
```bash
# Set up AWS profile
aws configure --profile myprofile

# Use with homeport
homeport analyze --source aws-api --profile myprofile
```

### Debug Mode

Enable verbose output for detailed debugging:
```bash
homeport analyze ./terraform --verbose
```

### Getting Help

- Documentation: https://homeport.github.io
- GitHub Issues: https://github.com/homeport/homeport/issues
