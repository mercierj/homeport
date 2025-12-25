# AWS Service Mapping Reference

This document provides a comprehensive reference for all AWS services supported by CloudExit and their self-hosted equivalents.

## Overview

CloudExit analyzes your AWS infrastructure (via Terraform state or HCL files) and generates Docker Compose configurations using open-source, self-hosted alternatives. Each mapper handles the translation of AWS-specific configurations to their Docker equivalents.

## Service Mapping Summary

| Category | AWS Service | Self-Hosted | Mapper File | Parity |
|----------|-------------|-------------|-------------|--------|
| **Compute** | EC2 | Docker | `compute/ec2.go` | Full |
| | Lambda | Docker/Cron | `compute/lambda.go` | Full |
| | ECS | Docker Compose | `compute/ecs.go` | Full |
| | EKS | Kubernetes | `compute/eks.go` | Partial |
| **Storage** | S3 | MinIO | `storage/s3.go` | Full |
| | EBS | Docker Volumes | `storage/ebs.go` | Full |
| | EFS | NFS | `storage/efs.go` | Full |
| **Database** | RDS PostgreSQL | PostgreSQL | `database/rds.go` | Full |
| | RDS MySQL | MySQL | `database/rds.go` | Full |
| | RDS Cluster | PostgreSQL/MySQL | `database/rds_cluster.go` | Full |
| | DynamoDB | ScyllaDB | `database/dynamodb.go` | Full |
| | ElastiCache Redis | Redis | `database/elasticache.go` | Full |
| | ElastiCache Memcached | Memcached | `database/elasticache.go` | Full |
| **Networking** | ALB/NLB | Traefik | `networking/alb.go` | Full |
| | API Gateway | Traefik | `networking/apigateway.go` | Full |
| | CloudFront | Nginx/Traefik | `networking/cloudfront.go` | Full |
| | Route53 | PowerDNS | `networking/route53.go` | Partial |
| | VPC | Docker Networks | `networking/vpc.go` | Full |
| **Security** | Cognito | Keycloak | `security/cognito.go` | Full |
| | IAM | Config/Policies | `security/iam.go` | Partial |
| | ACM | Let's Encrypt | `security/acm.go` | Full |
| **Messaging** | SQS | RabbitMQ | `messaging/sqs.go` | Full |
| | SNS | RabbitMQ | `messaging/sns.go` | Full |
| | EventBridge | Custom Handler | `messaging/eventbridge.go` | Partial |
| | Kinesis | Kafka/Redis | `messaging/kinesis.go` | Partial |

---

## Compute Services

### EC2 (Elastic Compute Cloud)

**Self-Hosted Alternative:** Docker Containers

CloudExit converts EC2 instances to Docker containers, preserving:
- Instance configuration (environment variables)
- Security group rules (as Docker network policies)
- User data scripts (as container initialization)

**Terraform Input:**
```hcl
resource "aws_instance" "web" {
  ami           = "ami-0c55b159cbfafe1f0"
  instance_type = "t3.medium"

  tags = {
    Name = "web-server"
  }

  user_data = <<-EOF
    #!/bin/bash
    apt-get update
    apt-get install -y nginx
  EOF
}
```

**Docker Output:**
```yaml
services:
  web-server:
    image: ubuntu:22.04
    restart: unless-stopped
    labels:
      cloudexit.source: aws_instance
      cloudexit.instance: web-server
    networks:
      - cloudexit
```

### Lambda

**Self-Hosted Alternative:** Docker Container with Cron (for scheduled) or HTTP endpoint

**Terraform Input:**
```hcl
resource "aws_lambda_function" "processor" {
  filename      = "lambda.zip"
  function_name = "data-processor"
  role          = aws_iam_role.lambda.arn
  handler       = "index.handler"
  runtime       = "nodejs18.x"
  memory_size   = 256
  timeout       = 30
}
```

**Docker Output:**
```yaml
services:
  data-processor:
    image: node:18-alpine
    restart: unless-stopped
    environment:
      AWS_LAMBDA_FUNCTION_NAME: data-processor
      AWS_LAMBDA_FUNCTION_MEMORY_SIZE: "256"
    deploy:
      resources:
        limits:
          memory: 256M
    labels:
      cloudexit.source: aws_lambda_function
```

### ECS/EKS

**Self-Hosted Alternative:** Docker Compose / Kubernetes

ECS task definitions are converted to Docker Compose services. EKS clusters generate Kubernetes manifests.

---

## Storage Services

### S3 (Simple Storage Service)

**Self-Hosted Alternative:** MinIO

MinIO is an S3-compatible object storage server. CloudExit preserves:
- Bucket configurations
- CORS rules
- Lifecycle policies
- Versioning settings

**Terraform Input:**
```hcl
resource "aws_s3_bucket" "assets" {
  bucket = "my-assets-bucket"
}

resource "aws_s3_bucket_versioning" "assets" {
  bucket = aws_s3_bucket.assets.id
  versioning_configuration {
    status = "Enabled"
  }
}
```

**Docker Output:**
```yaml
services:
  minio:
    image: minio/minio:latest
    command: server /data --console-address ":9001"
    ports:
      - "9000:9000"
      - "9001:9001"
    environment:
      MINIO_ROOT_USER: minioadmin
      MINIO_ROOT_PASSWORD: minioadmin
    volumes:
      - ./data/minio:/data
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:9000/minio/health/live"]
      interval: 30s
      timeout: 5s
      retries: 3
    labels:
      cloudexit.source: aws_s3_bucket
      cloudexit.bucket: my-assets-bucket
```

**Migration Script Generated:**
```bash
#!/bin/bash
# S3 to MinIO Migration
mc alias set local http://localhost:9000 minioadmin minioadmin
mc alias set aws https://s3.amazonaws.com $AWS_ACCESS_KEY $AWS_SECRET_KEY
mc mirror aws/my-assets-bucket local/my-assets-bucket
```

### EBS/EFS

**Self-Hosted Alternative:** Docker Volumes / NFS

EBS volumes become Docker named volumes. EFS mounts are converted to NFS configurations.

---

## Database Services

### RDS (Relational Database Service)

**Self-Hosted Alternatives:**
- PostgreSQL: `postgres:16-alpine`
- MySQL: `mysql:8.0`
- MariaDB: `mariadb:10`

**Terraform Input:**
```hcl
resource "aws_db_instance" "main" {
  identifier           = "production-db"
  engine               = "postgres"
  engine_version       = "15.4"
  instance_class       = "db.t3.medium"
  allocated_storage    = 100
  db_name              = "myapp"
  username             = "admin"
  password             = "changeme"
  skip_final_snapshot  = true
}
```

**Docker Output:**
```yaml
services:
  postgres:
    image: postgres:15-alpine
    restart: unless-stopped
    ports:
      - "5432:5432"
    environment:
      POSTGRES_DB: myapp
      POSTGRES_USER: admin
      POSTGRES_PASSWORD: changeme
      PGDATA: /var/lib/postgresql/data/pgdata
    volumes:
      - ./data/postgres:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U admin"]
      interval: 10s
      timeout: 5s
      retries: 5
    labels:
      cloudexit.source: aws_db_instance
      cloudexit.engine: postgres
```

### DynamoDB

**Self-Hosted Alternative:** ScyllaDB

ScyllaDB provides a DynamoDB-compatible API with high performance.

**Docker Output:**
```yaml
services:
  scylladb:
    image: scylladb/scylla:latest
    restart: unless-stopped
    ports:
      - "9042:9042"
      - "8000:8000"  # DynamoDB-compatible API
    command: --alternator-port=8000 --alternator-write-isolation=always
    volumes:
      - ./data/scylladb:/var/lib/scylla
```

### ElastiCache

**Self-Hosted Alternatives:**
- Redis: `redis:7-alpine`
- Memcached: `memcached:latest`

**Docker Output (Redis):**
```yaml
services:
  redis:
    image: redis:7-alpine
    restart: unless-stopped
    ports:
      - "6379:6379"
    command: redis-server --appendonly yes
    volumes:
      - ./data/redis:/data
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 10s
      timeout: 5s
      retries: 5
```

---

## Networking Services

### ALB/NLB (Application/Network Load Balancer)

**Self-Hosted Alternative:** Traefik

Traefik provides load balancing, SSL termination, and routing.

**Docker Output:**
```yaml
services:
  traefik:
    image: traefik:v3.0
    restart: unless-stopped
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - ./traefik:/etc/traefik
    labels:
      traefik.enable: "true"
      traefik.http.routers.dashboard.rule: "Host(`traefik.localhost`)"
      traefik.http.routers.dashboard.service: "api@internal"
```

### API Gateway

**Self-Hosted Alternative:** Traefik with Middleware

API Gateway routes are converted to Traefik router configurations with appropriate middleware (rate limiting, authentication, etc.).

### Route53

**Self-Hosted Alternative:** PowerDNS / CoreDNS

DNS records are exported and can be imported into PowerDNS or your DNS provider.

---

## Security Services

### Cognito

**Self-Hosted Alternative:** Keycloak

Keycloak provides full identity and access management functionality.

**Docker Output:**
```yaml
services:
  keycloak:
    image: quay.io/keycloak/keycloak:latest
    restart: unless-stopped
    command: start-dev
    ports:
      - "8080:8080"
    environment:
      KEYCLOAK_ADMIN: admin
      KEYCLOAK_ADMIN_PASSWORD: changeme
      KC_DB: postgres
      KC_DB_URL: jdbc:postgresql://postgres:5432/keycloak
      KC_DB_USERNAME: keycloak
      KC_DB_PASSWORD: changeme
    depends_on:
      - postgres
```

### ACM (Certificate Manager)

**Self-Hosted Alternative:** Let's Encrypt via Traefik

Traefik automatically handles Let's Encrypt certificate provisioning and renewal.

---

## Messaging Services

### SQS (Simple Queue Service)

**Self-Hosted Alternative:** RabbitMQ

**Docker Output:**
```yaml
services:
  rabbitmq:
    image: rabbitmq:3-management-alpine
    restart: unless-stopped
    ports:
      - "5672:5672"
      - "15672:15672"
    environment:
      RABBITMQ_DEFAULT_USER: admin
      RABBITMQ_DEFAULT_PASS: changeme
    volumes:
      - ./data/rabbitmq:/var/lib/rabbitmq
    healthcheck:
      test: ["CMD", "rabbitmq-diagnostics", "check_running"]
      interval: 30s
      timeout: 10s
      retries: 5
```

### SNS (Simple Notification Service)

**Self-Hosted Alternative:** RabbitMQ Exchanges

SNS topics are converted to RabbitMQ exchanges with appropriate bindings.

### EventBridge

**Self-Hosted Alternative:** Custom event router

EventBridge rules are converted to a custom event routing configuration.

---

## Limitations and Manual Steps

### Services Requiring Manual Configuration

1. **IAM Roles/Policies** - Mapped to application-level permissions
2. **VPC Peering** - Requires network configuration
3. **CloudWatch Alarms** - Converted to Prometheus alerting rules (review required)
4. **Secrets Manager** - Secrets exported to `.env` files (secure storage recommended)

### Feature Gaps

| AWS Feature | Status | Notes |
|-------------|--------|-------|
| Multi-AZ deployments | Manual | Configure replication manually |
| Auto Scaling | Partial | Use Docker Swarm or K8s for scaling |
| Cross-region replication | Manual | Configure at application level |
| AWS-specific APIs | N/A | Use S3-compatible or standard APIs |

---

## Adding New AWS Mappers

To add support for a new AWS service:

1. Create mapper file in `internal/infrastructure/mapper/<category>/<service>.go`
2. Implement the `Mapper` interface:
   ```go
   type Mapper interface {
       ResourceType() resource.Type
       Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error)
       Validate(res *resource.AWSResource) error
       Dependencies() []resource.Type
   }
   ```
3. Register in `internal/infrastructure/mapper/registry.go`
4. Add resource type to `internal/domain/resource/types.go`
5. Write tests and update documentation

See [Contributing Guide](contributing.md) for detailed instructions.
