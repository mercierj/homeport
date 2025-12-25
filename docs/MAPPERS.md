# CloudExit Mappers Documentation

This document describes all AWS to self-hosted service mappers implemented in the CloudExit project.

## Overview

The mapper system converts AWS resources to equivalent self-hosted Docker-based services. Each mapper:

- Implements the `Mapper` interface from `internal/domain/mapper`
- Returns a `MappingResult` with Docker services, configs, and scripts
- Includes warnings for unsupported features
- Documents manual steps required for complete migration

## Available Mappers

### Storage

#### S3 to MinIO (`storage/s3.go`)

Maps AWS S3 buckets to MinIO object storage.

**Features:**
- Bucket creation with MinIO client (mc) setup
- Versioning configuration
- Lifecycle rules mapping
- CORS configuration
- Public access settings
- Server-side encryption notes
- Replication warnings

**Output:**
- MinIO Docker service with console
- Setup script for bucket creation
- CORS configuration files
- Lifecycle policy scripts

**Traefik Integration:**
- Management console at `minio.localhost`
- Port 9000 for S3-compatible API
- Port 9001 for web console

### Database

#### RDS to PostgreSQL/MySQL (`database/rds.go`)

Maps AWS RDS instances to PostgreSQL or MySQL containers.

**Features:**
- Engine detection (postgres, mysql, mariadb)
- Instance class to resource limits mapping
- Automated backup services (postgres-backup-local)
- Parameter groups as config files
- Migration scripts for data import
- Multi-AZ warnings
- Encryption notes

**Output:**
- Database Docker service (PostgreSQL or MySQL)
- Backup service for PostgreSQL
- Configuration files optimized for workload
- Migration scripts for data import
- Backup scripts for MySQL

**Resource Mapping:**
- db.t3.micro: 0.5 CPU, 1G RAM
- db.t3.small: 1.0 CPU, 2G RAM
- db.t3.medium: 2.0 CPU, 4G RAM
- db.t3.large: 2.0 CPU, 8G RAM
- And more...

#### ElastiCache to Redis/Memcached (`database/elasticache.go`)

Maps AWS ElastiCache clusters to Redis or Memcached.

**Features:**
- Engine detection (redis, memcached)
- Persistence configuration (AOF/RDB for Redis)
- Cluster mode support
- Authentication setup
- Encryption warnings
- Node type to resource limits

**Output:**
- Redis or Memcached Docker service
- Configuration files
- Cluster setup scripts for Redis Cluster mode
- Migration scripts and guides

**Redis Features:**
- Optional persistence (AOF + RDB)
- Password protection
- TLS encryption notes
- Cluster mode scripts

### Compute

#### EC2 to Docker Container (`compute/ec2.go`)

Maps AWS EC2 instances to Docker containers.

**Features:**
- AMI to base image detection
- User data script extraction and execution
- Instance type to resource limits
- Security group rules to exposed ports
- EBS volumes as Docker volumes
- IAM role warnings
- SSH key pair notes

**Output:**
- Docker service definition
- Custom Dockerfile if user data exists
- Setup scripts
- Volume mount configurations

**Base Image Detection:**
- Ubuntu AMIs → ubuntu:22.04
- Amazon Linux → amazonlinux:2023
- Debian → debian:12
- Alpine → alpine:3.18
- RHEL → redhat/ubi9

#### Lambda to OpenFaaS/Docker (`compute/lambda.go`)

Maps AWS Lambda functions to containerized functions.

**Features:**
- Runtime detection and Dockerfile generation
- Multiple runtime support (Node.js, Python, Go, Java, .NET, Ruby)
- Environment variables mapping
- VPC configuration as Docker networks
- Timeout and memory settings
- Event source mapping notes
- Layer dependency warnings

**Output:**
- Function Docker service
- Runtime-specific Dockerfile
- Handler code templates
- Deployment scripts
- OpenFaaS labels for scaling

**Supported Runtimes:**
- nodejs16, nodejs18, nodejs20
- python3.9, python3.10, python3.11, python3.12
- go1.x, go1.20, go1.21
- java11, java17, java21
- dotnetcore6, dotnetcore8
- ruby3.1, ruby3.2

### Networking

#### ALB to Traefik (`networking/alb.go`)

Maps AWS Application Load Balancers to Traefik reverse proxy.

**Features:**
- Listener rules to Traefik routers
- Target groups to services
- Health check configuration
- SSL/TLS certificate handling
- HTTP/2 support
- WAF integration notes
- Access logs configuration

**Output:**
- Traefik Docker service
- Static configuration (traefik.yml)
- Dynamic configuration (routes and services)
- Certificate configuration templates
- Dashboard access

**Traefik Features:**
- Dashboard at `traefik.localhost:8080`
- Automatic HTTPS redirect
- Health checks
- Load balancing
- Middleware support

### Security

#### Cognito to Keycloak (`security/cognito.go`)

Maps AWS Cognito User Pools to Keycloak identity provider.

**Features:**
- User pool to realm conversion
- Password policy mapping
- MFA configuration
- Email configuration
- User pool clients to Keycloak clients
- User attribute schema mapping
- Auto-verified attributes

**Output:**
- Keycloak Docker service
- PostgreSQL database for Keycloak
- Realm configuration JSON
- Client configurations
- Setup and user migration scripts

**Keycloak Setup:**
- Admin console at `localhost:8080`
- Default credentials: admin/admin
- PostgreSQL backend
- Realm import/export support
- SMTP configuration for emails

### Messaging

#### SQS to RabbitMQ (`messaging/sqs.go`)

Maps AWS SQS queues to RabbitMQ.

**Features:**
- Queue settings migration
- FIFO queue warnings
- Dead letter queue configuration
- Visibility timeout as message TTL
- Message retention mapping
- Delay queue notes
- Management UI access

**Output:**
- RabbitMQ Docker service with management plugin
- Queue definitions JSON
- RabbitMQ configuration file
- Setup scripts
- DLQ configuration notes

**RabbitMQ Features:**
- Management UI at `localhost:15672`
- AMQP protocol on port 5672
- Queue durability
- Message TTL
- Dead letter exchanges
- Delayed message plugin support

## Mapper Registry

The `Registry` class (`internal/infrastructure/mapper/registry.go`) manages all mappers:

```go
// Create registry with all default mappers
registry := mapper.NewRegistry()

// Map a single resource
result, err := registry.Map(ctx, awsResource)

// Map multiple resources
results, err := registry.MapBatch(ctx, resources)

// Check if mapper exists
if registry.HasMapper(resource.TypeS3Bucket) {
    // ...
}
```

## Usage Example

```go
import (
    "context"

    "github.com/cloudexit/cloudexit/internal/domain/resource"
    "github.com/cloudexit/cloudexit/internal/infrastructure/mapper"
)

func main() {
    ctx := context.Background()
    registry := mapper.NewRegistry()

    // Create AWS resource
    s3Bucket := &resource.AWSResource{
        ID:   "my-bucket",
        Type: resource.TypeS3Bucket,
        Name: "my-bucket",
        Attributes: map[string]interface{}{
            "bucket": "my-bucket",
            "versioning": map[string]interface{}{
                "enabled": true,
            },
        },
        Region: "us-east-1",
    }

    // Map to self-hosted
    result, err := registry.Map(ctx, s3Bucket)
    if err != nil {
        panic(err)
    }

    // Access mapping results
    for _, service := range result.Services {
        fmt.Printf("Service: %s\n", service.Name)
        fmt.Printf("Image: %s\n", service.Image)
    }

    for _, warning := range result.Warnings {
        fmt.Printf("Warning: %s\n", warning)
    }

    for _, step := range result.ManualSteps {
        fmt.Printf("Manual step: %s\n", step)
    }
}
```

## Common Patterns

### Resource Limits Mapping

All mappers include intelligent mapping of AWS instance/node types to Docker resource limits:

- **Micro**: 0.5 CPU, 512M-1G RAM
- **Small**: 1.0 CPU, 1-2G RAM
- **Medium**: 2.0 CPU, 3-4G RAM
- **Large**: 2.0 CPU, 6-8G RAM
- **XLarge**: 4.0 CPU, 12-16G RAM
- **2XLarge**: 8.0 CPU, 25-32G RAM
- **4XLarge**: 16.0 CPU, 50-64G RAM

### Health Checks

All services include appropriate health checks:

- **Databases**: Connection tests (pg_isready, mysqladmin ping)
- **Cache**: Redis PING, Memcached connection
- **Web Services**: HTTP endpoint checks
- **Message Queues**: Management API checks

### Traefik Integration

Services exposed via web include Traefik labels:

```yaml
labels:
  - "traefik.enable=true"
  - "traefik.http.routers.{service}.rule=Host(`{service}.localhost`)"
  - "traefik.http.services.{service}.loadbalancer.server.port={port}"
```

### Configuration Files

All mappers generate appropriate configuration files:

- Database configs (postgresql.conf, my.cnf, redis.conf)
- Server configs (traefik.yml, rabbitmq.conf)
- Application configs (realm.json for Keycloak)

### Scripts

Generated scripts include:

- **Setup scripts**: Initial service configuration
- **Migration scripts**: Data import from AWS
- **Backup scripts**: Automated backup routines
- **Deployment scripts**: Build and deploy functions

## Extending Mappers

To add a new mapper:

1. Create a new package under `internal/infrastructure/mapper/`
2. Implement the `Mapper` interface
3. Register in `registry.go`

Example:

```go
// internal/infrastructure/mapper/database/dynamodb.go
package database

import (
    "context"
    "github.com/cloudexit/cloudexit/internal/domain/mapper"
    "github.com/cloudexit/cloudexit/internal/domain/resource"
)

type DynamoDBMapper struct {
    *mapper.BaseMapper
}

func NewDynamoDBMapper() *DynamoDBMapper {
    return &DynamoDBMapper{
        BaseMapper: mapper.NewBaseMapper(resource.TypeDynamoDBTable, nil),
    }
}

func (m *DynamoDBMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
    // Implementation
}
```

Then register in `registry.go`:

```go
func (r *Registry) RegisterDefaults() {
    // ... existing mappers
    r.Register(database.NewDynamoDBMapper())
}
```

## Testing Mappers

Each mapper should include:

1. Unit tests for mapping logic
2. Integration tests with Docker
3. Example AWS resource fixtures
4. Validation of generated configurations

## Future Enhancements

Planned mapper additions:

- DynamoDB to MongoDB/ScyllaDB
- CloudFront to Caddy/Nginx
- Route53 to CoreDNS/Pi-hole
- API Gateway to Kong/Tyk
- ECS to Docker Compose/Kubernetes
- Secrets Manager to Vault
- SNS to RabbitMQ/NATS
- EventBridge to custom event bus

## License

Part of the CloudExit project.
