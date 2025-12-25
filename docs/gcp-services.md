# GCP Service Mapping Reference

This document provides a comprehensive reference for all Google Cloud Platform services supported by CloudExit and their self-hosted equivalents.

## Overview

CloudExit analyzes your GCP infrastructure (via Terraform state or HCL files) and generates Docker Compose configurations using open-source, self-hosted alternatives. GCP support was added to enable organizations to migrate from any major cloud provider.

## Service Mapping Summary

| Category | GCP Service | Self-Hosted | Mapper File | Parity |
|----------|-------------|-------------|-------------|--------|
| **Compute** | Compute Engine (GCE) | Docker | `gcp/compute/gce.go` | Full |
| | Cloud Run | Docker | `gcp/compute/cloudrun.go` | Full |
| | Cloud Functions | Docker | `gcp/compute/cloudfunction.go` | Full |
| | GKE | Kubernetes | `gcp/compute/gke.go` | Partial |
| **Storage** | Cloud Storage (GCS) | MinIO | `gcp/storage/gcs.go` | Full |
| | Persistent Disk | Docker Volumes | `gcp/storage/persistent_disk.go` | Full |
| | Filestore | NFS | `gcp/storage/filestore.go` | Full |
| **Database** | Cloud SQL | PostgreSQL/MySQL | `gcp/database/cloudsql.go` | Full |
| | Firestore | MongoDB | `gcp/database/firestore.go` | Full |
| | Bigtable | HBase | `gcp/database/bigtable.go` | Partial |
| | Memorystore | Redis | `gcp/database/memorystore.go` | Full |
| | Spanner | CockroachDB | `gcp/database/spanner.go` | Partial |
| **Networking** | Cloud Load Balancing | Traefik | `gcp/networking/cloudlb.go` | Full |
| | Cloud DNS | PowerDNS | `gcp/networking/clouddns.go` | Full |
| | Cloud CDN | Nginx/Traefik | `gcp/networking/cloudcdn.go` | Full |
| **Messaging** | Pub/Sub | RabbitMQ | `gcp/messaging/pubsub.go` | Full |
| | Cloud Tasks | Task Queue | `gcp/messaging/cloudtasks.go` | Full |
| **Security** | Identity Platform | Keycloak | `gcp/security/identity_platform.go` | Full |

---

## Compute Services

### Compute Engine (GCE)

**Self-Hosted Alternative:** Docker Containers

GCE instances are converted to Docker containers with equivalent resource configurations.

**Terraform Input:**
```hcl
resource "google_compute_instance" "web" {
  name         = "web-server"
  machine_type = "e2-medium"
  zone         = "us-central1-a"

  boot_disk {
    initialize_params {
      image = "debian-cloud/debian-11"
    }
  }

  network_interface {
    network = "default"
    access_config {}
  }

  metadata_startup_script = "apt-get update && apt-get install -y nginx"
}
```

**Docker Output:**
```yaml
services:
  web-server:
    image: debian:11
    restart: unless-stopped
    labels:
      cloudexit.source: google_compute_instance
      cloudexit.zone: us-central1-a
    networks:
      - cloudexit
```

### Cloud Run

**Self-Hosted Alternative:** Docker Container with Traefik routing

Cloud Run services are converted to Docker containers with HTTP routing via Traefik.

**Terraform Input:**
```hcl
resource "google_cloud_run_service" "api" {
  name     = "api-service"
  location = "us-central1"

  template {
    spec {
      containers {
        image = "gcr.io/my-project/api:latest"
        ports {
          container_port = 8080
        }
        resources {
          limits = {
            cpu    = "1000m"
            memory = "512Mi"
          }
        }
      }
    }
  }
}
```

**Docker Output:**
```yaml
services:
  api-service:
    image: gcr.io/my-project/api:latest
    restart: unless-stopped
    deploy:
      resources:
        limits:
          cpus: "1.0"
          memory: 512M
    labels:
      cloudexit.source: google_cloud_run_service
      traefik.enable: "true"
      traefik.http.routers.api-service.rule: "Host(`api.localhost`)"
      traefik.http.services.api-service.loadbalancer.server.port: "8080"
    networks:
      - cloudexit
```

### Cloud Functions

**Self-Hosted Alternative:** Docker Container with HTTP endpoint or Cron

**Docker Output:**
```yaml
services:
  my-function:
    image: node:18-alpine
    restart: unless-stopped
    environment:
      FUNCTION_NAME: my-function
      FUNCTION_TRIGGER: http
    labels:
      cloudexit.source: google_cloudfunctions_function
      traefik.enable: "true"
      traefik.http.routers.my-function.rule: "PathPrefix(`/function`)"
```

### GKE (Google Kubernetes Engine)

**Self-Hosted Alternative:** Kubernetes / K3s

GKE clusters generate Kubernetes manifest files for deployment to any K8s cluster.

---

## Storage Services

### Cloud Storage (GCS)

**Self-Hosted Alternative:** MinIO

MinIO provides an S3-compatible API that works with GCS client libraries after minimal code changes.

**Terraform Input:**
```hcl
resource "google_storage_bucket" "assets" {
  name     = "my-assets-bucket"
  location = "US"

  versioning {
    enabled = true
  }

  lifecycle_rule {
    condition {
      age = 30
    }
    action {
      type = "Delete"
    }
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
      cloudexit.source: google_storage_bucket
      cloudexit.bucket: my-assets-bucket
```

**Migration Script Generated:**
```bash
#!/bin/bash
# GCS to MinIO Migration
mc alias set local http://localhost:9000 minioadmin minioadmin
gsutil -m rsync -r gs://my-assets-bucket s3://my-assets-bucket
```

### Persistent Disk

**Self-Hosted Alternative:** Docker Volumes

Persistent disks become Docker named volumes with appropriate drivers.

### Filestore

**Self-Hosted Alternative:** NFS Server

Filestore instances are converted to NFS configurations.

---

## Database Services

### Cloud SQL

**Self-Hosted Alternatives:**
- PostgreSQL: `postgres:16-alpine`
- MySQL: `mysql:8.0`

**Terraform Input:**
```hcl
resource "google_sql_database_instance" "main" {
  name             = "main-instance"
  database_version = "POSTGRES_15"
  region           = "us-central1"

  settings {
    tier = "db-f1-micro"

    backup_configuration {
      enabled = true
    }
  }
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
      POSTGRES_DB: main
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: changeme
    volumes:
      - ./data/postgres:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U postgres"]
      interval: 10s
      timeout: 5s
      retries: 5
    labels:
      cloudexit.source: google_sql_database_instance
```

### Firestore

**Self-Hosted Alternative:** MongoDB

MongoDB provides similar document-oriented storage capabilities.

**Docker Output:**
```yaml
services:
  mongodb:
    image: mongo:7
    restart: unless-stopped
    ports:
      - "27017:27017"
    environment:
      MONGO_INITDB_ROOT_USERNAME: admin
      MONGO_INITDB_ROOT_PASSWORD: changeme
    volumes:
      - ./data/mongodb:/data/db
    healthcheck:
      test: ["CMD", "mongosh", "--eval", "db.adminCommand('ping')"]
      interval: 10s
      timeout: 5s
      retries: 5
```

### Bigtable

**Self-Hosted Alternative:** Apache HBase

HBase provides similar wide-column store capabilities for big data workloads.

### Memorystore

**Self-Hosted Alternative:** Redis

**Docker Output:**
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

### Spanner

**Self-Hosted Alternative:** CockroachDB

CockroachDB provides similar distributed SQL capabilities with strong consistency.

**Docker Output:**
```yaml
services:
  cockroachdb:
    image: cockroachdb/cockroach:latest
    command: start-single-node --insecure
    ports:
      - "26257:26257"
      - "8080:8080"
    volumes:
      - ./data/cockroach:/cockroach/cockroach-data
```

---

## Networking Services

### Cloud Load Balancing

**Self-Hosted Alternative:** Traefik

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
```

### Cloud DNS

**Self-Hosted Alternative:** PowerDNS / CoreDNS

DNS zones and records are exported for import into self-hosted DNS servers.

### Cloud CDN

**Self-Hosted Alternative:** Nginx with caching / Traefik

CDN configurations are converted to caching proxy configurations.

---

## Messaging Services

### Pub/Sub

**Self-Hosted Alternative:** RabbitMQ

Pub/Sub topics and subscriptions are converted to RabbitMQ exchanges and queues.

**Terraform Input:**
```hcl
resource "google_pubsub_topic" "orders" {
  name = "orders-topic"
}

resource "google_pubsub_subscription" "orders" {
  name  = "orders-subscription"
  topic = google_pubsub_topic.orders.name

  ack_deadline_seconds = 20
}
```

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
```

### Cloud Tasks

**Self-Hosted Alternative:** Task Queue (Redis-based)

Cloud Tasks queues are converted to Redis-based task queue configurations.

---

## Security Services

### Identity Platform

**Self-Hosted Alternative:** Keycloak

Identity Platform configurations are converted to Keycloak realm settings.

**Docker Output:**
```yaml
services:
  keycloak:
    image: quay.io/keycloak/keycloak:latest
    command: start-dev
    ports:
      - "8080:8080"
    environment:
      KEYCLOAK_ADMIN: admin
      KEYCLOAK_ADMIN_PASSWORD: changeme
    depends_on:
      - postgres
```

---

## GCP-Specific Considerations

### Application Default Credentials

When migrating from GCP, update your applications to:
1. Use environment variables for configuration
2. Switch from GCS client to S3-compatible clients (MinIO SDK)
3. Update connection strings for database services

### SDK Migration

| GCP SDK | Self-Hosted Alternative |
|---------|------------------------|
| `cloud.google.com/go/storage` | MinIO Go SDK or AWS SDK |
| `cloud.google.com/go/firestore` | MongoDB Go Driver |
| `cloud.google.com/go/pubsub` | RabbitMQ AMQP client |
| `cloud.google.com/go/redis` | go-redis |

### IAM and Service Accounts

GCP IAM roles are converted to application-level permissions. Service account credentials should be replaced with local authentication mechanisms.

---

## Limitations and Manual Steps

### Services Requiring Manual Configuration

1. **BigQuery** - Consider alternatives like ClickHouse or Apache Druid
2. **Cloud Composer** - Use Apache Airflow directly
3. **Dataflow** - Consider Apache Beam with local runners
4. **Cloud KMS** - Use HashiCorp Vault for key management

### Feature Gaps

| GCP Feature | Status | Notes |
|-------------|--------|-------|
| Global Load Balancing | Partial | Single-region Traefik setup |
| Cloud Armor | Manual | Configure WAF separately |
| VPC Service Controls | N/A | Network-level security |
| Cloud Logging | Replaced | Use Loki for log aggregation |

---

## Adding New GCP Mappers

To add support for a new GCP service:

1. Create mapper in `internal/infrastructure/mapper/gcp/<category>/<service>.go`
2. Implement the `Mapper` interface
3. Register in `internal/infrastructure/mapper/gcp/registry.go`
4. Add resource type to `internal/domain/resource/types.go`
5. Write tests and update documentation

See [Contributing Guide](contributing.md) for detailed instructions.
