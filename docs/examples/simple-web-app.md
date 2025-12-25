# Example: Simple Web Application

This example demonstrates migrating a simple web application from AWS to a self-hosted Docker stack.

## Architecture

```
                    AWS Architecture
┌─────────────────────────────────────────────────────┐
│                                                      │
│   ┌─────────────┐    ┌─────────────┐                │
│   │   Route53   │───▶│     ALB     │                │
│   └─────────────┘    └──────┬──────┘                │
│                             │                        │
│                      ┌──────▼──────┐                │
│                      │     EC2     │                │
│                      │  (Web App)  │                │
│                      └──────┬──────┘                │
│                             │                        │
│              ┌──────────────┼──────────────┐        │
│              ▼              ▼              ▼        │
│       ┌──────────┐   ┌──────────┐   ┌──────────┐   │
│       │   RDS    │   │    S3    │   │  ElastiC │   │
│       │ Postgres │   │ (Assets) │   │  (Redis) │   │
│       └──────────┘   └──────────┘   └──────────┘   │
│                                                      │
└─────────────────────────────────────────────────────┘

                        ▼ CloudExit ▼

                  Self-Hosted Architecture
┌─────────────────────────────────────────────────────┐
│                                                      │
│   ┌─────────────────────────────────────────────┐   │
│   │                  Traefik                     │   │
│   │            (Reverse Proxy + SSL)             │   │
│   └──────────────────────┬──────────────────────┘   │
│                          │                          │
│                   ┌──────▼──────┐                   │
│                   │   Web App   │                   │
│                   │  (Docker)   │                   │
│                   └──────┬──────┘                   │
│                          │                          │
│           ┌──────────────┼──────────────┐          │
│           ▼              ▼              ▼          │
│    ┌──────────┐   ┌──────────┐   ┌──────────┐     │
│    │PostgreSQL│   │  MinIO   │   │  Redis   │     │
│    │ (Docker) │   │ (Docker) │   │ (Docker) │     │
│    └──────────┘   └──────────┘   └──────────┘     │
│                                                      │
└─────────────────────────────────────────────────────┘
```

## Terraform Configuration (Input)

### `main.tf`

```hcl
terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region = "us-east-1"
}

# VPC
resource "aws_vpc" "main" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_hostnames = true

  tags = {
    Name = "webapp-vpc"
  }
}

# EC2 Instance
resource "aws_instance" "web" {
  ami           = "ami-0c55b159cbfafe1f0"
  instance_type = "t3.medium"
  subnet_id     = aws_subnet.public.id

  tags = {
    Name = "webapp-server"
  }

  user_data = <<-EOF
    #!/bin/bash
    docker run -d -p 80:8080 myapp/webapp:latest
  EOF
}

# RDS PostgreSQL
resource "aws_db_instance" "main" {
  identifier           = "webapp-db"
  engine               = "postgres"
  engine_version       = "15.4"
  instance_class       = "db.t3.micro"
  allocated_storage    = 20
  db_name              = "webapp"
  username             = "webapp"
  password             = "changeme123"
  skip_final_snapshot  = true

  tags = {
    Name = "webapp-database"
  }
}

# S3 Bucket for assets
resource "aws_s3_bucket" "assets" {
  bucket = "webapp-assets-bucket"

  tags = {
    Name = "webapp-assets"
  }
}

resource "aws_s3_bucket_versioning" "assets" {
  bucket = aws_s3_bucket.assets.id
  versioning_configuration {
    status = "Enabled"
  }
}

# ElastiCache Redis
resource "aws_elasticache_cluster" "cache" {
  cluster_id           = "webapp-cache"
  engine               = "redis"
  node_type            = "cache.t3.micro"
  num_cache_nodes      = 1
  parameter_group_name = "default.redis7"
  port                 = 6379

  tags = {
    Name = "webapp-cache"
  }
}

# Application Load Balancer
resource "aws_lb" "main" {
  name               = "webapp-alb"
  internal           = false
  load_balancer_type = "application"
  subnets            = [aws_subnet.public.id, aws_subnet.public2.id]

  tags = {
    Name = "webapp-alb"
  }
}
```

## CloudExit Migration

### Step 1: Analyze Infrastructure

```bash
cloudexit analyze ./terraform --format table
```

**Output:**
```
CloudExit - Infrastructure Analysis
===================================

Provider: AWS
Resources Found: 6

┌──────────────────────────┬────────────────┬─────────────────┬────────┐
│ Resource                 │ Type           │ Self-Hosted     │ Status │
├──────────────────────────┼────────────────┼─────────────────┼────────┤
│ webapp-server            │ aws_instance   │ Docker          │ ✓      │
│ webapp-db                │ aws_db_instance│ PostgreSQL      │ ✓      │
│ webapp-assets-bucket     │ aws_s3_bucket  │ MinIO           │ ✓      │
│ webapp-cache             │ aws_elasticache│ Redis           │ ✓      │
│ webapp-alb               │ aws_lb         │ Traefik         │ ✓      │
│ webapp-vpc               │ aws_vpc        │ Docker Network  │ ✓      │
└──────────────────────────┴────────────────┴─────────────────┴────────┘

Fully Supported: 6/6 (100%)
```

### Step 2: Generate Docker Stack

```bash
cloudexit migrate ./terraform \
  --output ./webapp-stack \
  --domain webapp.example.com \
  --include-monitoring
```

## Generated Output

### Directory Structure

```
webapp-stack/
├── docker-compose.yml
├── docker-compose.override.yml
├── .env.example
├── traefik/
│   ├── traefik.yml
│   └── dynamic/
│       └── webapp.yml
├── configs/
│   └── postgres/
│       └── init.sql
├── scripts/
│   ├── migrate-s3.sh
│   ├── migrate-rds.sh
│   └── backup.sh
├── monitoring/
│   ├── prometheus.yml
│   └── grafana/
│       └── dashboards/
└── README.md
```

### `docker-compose.yml`

```yaml
version: "3.8"

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
      - traefik-certs:/letsencrypt
    networks:
      - cloudexit
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.dashboard.rule=Host(`traefik.webapp.example.com`)"
      - "traefik.http.routers.dashboard.service=api@internal"

  webapp:
    image: myapp/webapp:latest
    restart: unless-stopped
    environment:
      DATABASE_URL: postgres://webapp:changeme@postgres:5432/webapp
      REDIS_URL: redis://redis:6379
      S3_ENDPOINT: http://minio:9000
      S3_ACCESS_KEY: minioadmin
      S3_SECRET_KEY: minioadmin
      S3_BUCKET: webapp-assets
    depends_on:
      - postgres
      - redis
      - minio
    networks:
      - cloudexit
    labels:
      - "cloudexit.source=aws_instance"
      - "traefik.enable=true"
      - "traefik.http.routers.webapp.rule=Host(`webapp.example.com`)"
      - "traefik.http.routers.webapp.entrypoints=websecure"
      - "traefik.http.routers.webapp.tls.certresolver=letsencrypt"

  postgres:
    image: postgres:15-alpine
    restart: unless-stopped
    environment:
      POSTGRES_DB: webapp
      POSTGRES_USER: webapp
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:-changeme}
    volumes:
      - ./data/postgres:/var/lib/postgresql/data
      - ./configs/postgres:/docker-entrypoint-initdb.d
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U webapp"]
      interval: 10s
      timeout: 5s
      retries: 5
    networks:
      - cloudexit
    labels:
      - "cloudexit.source=aws_db_instance"

  minio:
    image: minio/minio:latest
    command: server /data --console-address ":9001"
    restart: unless-stopped
    ports:
      - "9001:9001"
    environment:
      MINIO_ROOT_USER: ${MINIO_ROOT_USER:-minioadmin}
      MINIO_ROOT_PASSWORD: ${MINIO_ROOT_PASSWORD:-minioadmin}
    volumes:
      - ./data/minio:/data
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:9000/minio/health/live"]
      interval: 30s
      timeout: 5s
      retries: 3
    networks:
      - cloudexit
    labels:
      - "cloudexit.source=aws_s3_bucket"
      - "traefik.enable=true"
      - "traefik.http.routers.minio.rule=Host(`s3.webapp.example.com`)"
      - "traefik.http.services.minio.loadbalancer.server.port=9000"

  redis:
    image: redis:7-alpine
    restart: unless-stopped
    command: redis-server --appendonly yes
    volumes:
      - ./data/redis:/data
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 10s
      timeout: 5s
      retries: 5
    networks:
      - cloudexit
    labels:
      - "cloudexit.source=aws_elasticache_cluster"

networks:
  cloudexit:
    driver: bridge

volumes:
  traefik-certs:
```

### `.env.example`

```bash
# Database
POSTGRES_PASSWORD=changeme

# MinIO (S3-compatible storage)
MINIO_ROOT_USER=minioadmin
MINIO_ROOT_PASSWORD=changeme

# Domain
DOMAIN=webapp.example.com

# Let's Encrypt
ACME_EMAIL=admin@example.com
```

## Deployment Steps

### 1. Configure Environment

```bash
cd webapp-stack
cp .env.example .env

# Edit .env with your actual passwords
vim .env
```

### 2. Start the Stack

```bash
docker compose up -d
```

### 3. Create MinIO Bucket

```bash
# Install MinIO client
brew install minio/stable/mc

# Configure alias
mc alias set local http://localhost:9000 minioadmin minioadmin

# Create bucket
mc mb local/webapp-assets
```

### 4. Migrate Data

```bash
# Migrate S3 data
./scripts/migrate-s3.sh

# Migrate database
./scripts/migrate-rds.sh
```

## Verification

### Check Services

```bash
docker compose ps
```

Expected output:
```
NAME                STATUS              PORTS
traefik             running             0.0.0.0:80->80/tcp, 0.0.0.0:443->443/tcp
webapp              running
postgres            running (healthy)   5432/tcp
minio               running (healthy)   9000/tcp, 0.0.0.0:9001->9001/tcp
redis               running (healthy)   6379/tcp
```

### Test Endpoints

```bash
# Web application
curl -I https://webapp.example.com

# MinIO Console
open http://localhost:9001

# Traefik Dashboard (if enabled)
open http://traefik.webapp.example.com
```

### Verify Database

```bash
docker compose exec postgres psql -U webapp -c "SELECT version();"
```

## Cost Comparison

| Service | AWS Monthly | Self-Hosted Monthly |
|---------|-------------|---------------------|
| EC2 t3.medium | ~$30 | - |
| RDS db.t3.micro | ~$15 | - |
| S3 (10GB) | ~$0.23 | - |
| ElastiCache | ~$12 | - |
| ALB | ~$20 | - |
| **Total AWS** | **~$77** | - |
| VPS (4GB RAM) | - | ~$20 |
| **Total Self-Hosted** | - | **~$20** |

*Note: VPS costs vary by provider. Prices are approximate.*

## Next Steps

1. Set up automated backups with the generated `backup.sh` script
2. Configure monitoring with Prometheus and Grafana
3. Set up CI/CD for your application deployments
4. Review security settings and firewall rules
