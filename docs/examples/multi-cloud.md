# Example: Multi-Cloud Migration

This example demonstrates migrating applications from GCP and Azure to a self-hosted Docker stack using AgnosTech.

## GCP Migration Example

### Architecture

```
                      GCP Architecture
┌────────────────────────────────────────────────────────────┐
│                                                             │
│   ┌─────────────┐                                          │
│   │ Cloud Load  │                                          │
│   │  Balancing  │                                          │
│   └──────┬──────┘                                          │
│          │                                                  │
│   ┌──────▼──────┐                                          │
│   │  Cloud Run  │                                          │
│   │   (API)     │                                          │
│   └──────┬──────┘                                          │
│          │                                                  │
│   ┌──────┴─────────────────────────┐                       │
│   │              │                 │                        │
│   ▼              ▼                 ▼                        │
│ ┌──────────┐ ┌──────────┐ ┌──────────┐                    │
│ │Cloud SQL │ │  Cloud   │ │Memorystore│                    │
│ │(Postgres)│ │ Storage  │ │ (Redis)  │                    │
│ └──────────┘ └──────────┘ └──────────┘                    │
│                                                             │
└────────────────────────────────────────────────────────────┘

                          ▼ AgnosTech ▼

                    Self-Hosted Architecture
┌────────────────────────────────────────────────────────────┐
│                                                             │
│   ┌─────────────┐                                          │
│   │   Traefik   │                                          │
│   └──────┬──────┘                                          │
│          │                                                  │
│   ┌──────▼──────┐                                          │
│   │     API     │                                          │
│   │  (Docker)   │                                          │
│   └──────┬──────┘                                          │
│          │                                                  │
│   ┌──────┴─────────────────────────┐                       │
│   │              │                 │                        │
│   ▼              ▼                 ▼                        │
│ ┌──────────┐ ┌──────────┐ ┌──────────┐                    │
│ │PostgreSQL│ │  MinIO   │ │  Redis   │                    │
│ └──────────┘ └──────────┘ └──────────┘                    │
│                                                             │
└────────────────────────────────────────────────────────────┘
```

### GCP Terraform Configuration

```hcl
# Cloud Run Service
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
            cpu    = "2000m"
            memory = "1024Mi"
          }
        }

        env {
          name  = "DATABASE_URL"
          value = "postgres://..."
        }
      }
    }
  }

  traffic {
    percent         = 100
    latest_revision = true
  }
}

# Cloud SQL
resource "google_sql_database_instance" "main" {
  name             = "main-db"
  database_version = "POSTGRES_15"
  region           = "us-central1"

  settings {
    tier = "db-f1-micro"

    backup_configuration {
      enabled = true
    }
  }
}

resource "google_sql_database" "app" {
  name     = "app"
  instance = google_sql_database_instance.main.name
}

# Cloud Storage
resource "google_storage_bucket" "assets" {
  name     = "my-app-assets"
  location = "US"

  versioning {
    enabled = true
  }
}

# Memorystore (Redis)
resource "google_redis_instance" "cache" {
  name           = "cache"
  tier           = "BASIC"
  memory_size_gb = 1
  region         = "us-central1"
}

# Cloud Load Balancing
resource "google_compute_url_map" "main" {
  name            = "api-url-map"
  default_service = google_compute_backend_service.api.id
}
```

### AgnosTech Migration

```bash
agnostech migrate ./terraform \
  --output ./gcp-stack \
  --domain api.example.com
```

### Generated `docker-compose.yml`

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
    networks:
      - agnostech
    labels:
      - "agnostech.source=google_compute_url_map"

  api:
    image: gcr.io/my-project/api:latest
    restart: unless-stopped
    deploy:
      resources:
        limits:
          cpus: "2.0"
          memory: 1024M
    environment:
      DATABASE_URL: postgres://app:${DB_PASSWORD}@postgres:5432/app
      REDIS_URL: redis://redis:6379
      GCS_ENDPOINT: http://minio:9000
      GCS_ACCESS_KEY: ${MINIO_ACCESS_KEY}
      GCS_SECRET_KEY: ${MINIO_SECRET_KEY}
    depends_on:
      - postgres
      - redis
      - minio
    networks:
      - agnostech
    labels:
      - "agnostech.source=google_cloud_run_service"
      - "traefik.enable=true"
      - "traefik.http.routers.api.rule=Host(`api.example.com`)"
      - "traefik.http.routers.api.tls.certresolver=letsencrypt"

  postgres:
    image: postgres:15-alpine
    restart: unless-stopped
    environment:
      POSTGRES_DB: app
      POSTGRES_USER: app
      POSTGRES_PASSWORD: ${DB_PASSWORD}
    volumes:
      - ./data/postgres:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U app"]
      interval: 10s
      timeout: 5s
      retries: 5
    networks:
      - agnostech
    labels:
      - "agnostech.source=google_sql_database_instance"

  minio:
    image: minio/minio:latest
    command: server /data --console-address ":9001"
    restart: unless-stopped
    ports:
      - "9001:9001"
    environment:
      MINIO_ROOT_USER: ${MINIO_ACCESS_KEY}
      MINIO_ROOT_PASSWORD: ${MINIO_SECRET_KEY}
    volumes:
      - ./data/minio:/data
    networks:
      - agnostech
    labels:
      - "agnostech.source=google_storage_bucket"

  redis:
    image: redis:7-alpine
    restart: unless-stopped
    command: redis-server --appendonly yes
    volumes:
      - ./data/redis:/data
    networks:
      - agnostech
    labels:
      - "agnostech.source=google_redis_instance"

networks:
  agnostech:
    driver: bridge
```

---

## Azure Migration Example

### Architecture

```
                     Azure Architecture
┌────────────────────────────────────────────────────────────┐
│                                                             │
│   ┌─────────────┐                                          │
│   │ Application │                                          │
│   │   Gateway   │                                          │
│   └──────┬──────┘                                          │
│          │                                                  │
│   ┌──────▼──────┐                                          │
│   │   Azure     │                                          │
│   │  Functions  │                                          │
│   └──────┬──────┘                                          │
│          │                                                  │
│   ┌──────┴─────────────────────────┐                       │
│   │              │                 │                        │
│   ▼              ▼                 ▼                        │
│ ┌──────────┐ ┌──────────┐ ┌──────────┐                    │
│ │  Azure   │ │   Blob   │ │  Azure   │                    │
│ │  SQL DB  │ │ Storage  │ │  Cache   │                    │
│ └──────────┘ └──────────┘ └──────────┘                    │
│                                                             │
└────────────────────────────────────────────────────────────┘

                          ▼ AgnosTech ▼

                    Self-Hosted Architecture
┌────────────────────────────────────────────────────────────┐
│                                                             │
│   ┌─────────────┐                                          │
│   │   Traefik   │                                          │
│   └──────┬──────┘                                          │
│          │                                                  │
│   ┌──────▼──────┐                                          │
│   │   API App   │                                          │
│   │  (Docker)   │                                          │
│   └──────┬──────┘                                          │
│          │                                                  │
│   ┌──────┴─────────────────────────┐                       │
│   │              │                 │                        │
│   ▼              ▼                 ▼                        │
│ ┌──────────┐ ┌──────────┐ ┌──────────┐                    │
│ │PostgreSQL│ │  MinIO   │ │  Redis   │                    │
│ └──────────┘ └──────────┘ └──────────┘                    │
│                                                             │
└────────────────────────────────────────────────────────────┘
```

### Azure Terraform Configuration

```hcl
# Resource Group
resource "azurerm_resource_group" "main" {
  name     = "app-resources"
  location = "East US"
}

# Azure Functions
resource "azurerm_function_app" "api" {
  name                       = "api-functions"
  location                   = azurerm_resource_group.main.location
  resource_group_name        = azurerm_resource_group.main.name
  app_service_plan_id        = azurerm_app_service_plan.main.id
  storage_account_name       = azurerm_storage_account.main.name
  storage_account_access_key = azurerm_storage_account.main.primary_access_key
  version                    = "~4"

  app_settings = {
    FUNCTIONS_WORKER_RUNTIME = "node"
    WEBSITE_NODE_DEFAULT_VERSION = "~18"
    DATABASE_URL = "..."
    REDIS_URL = "..."
  }
}

# Azure SQL Database
resource "azurerm_mssql_server" "main" {
  name                         = "app-sql-server"
  resource_group_name          = azurerm_resource_group.main.name
  location                     = azurerm_resource_group.main.location
  version                      = "12.0"
  administrator_login          = "sqladmin"
  administrator_login_password = var.sql_password
}

resource "azurerm_mssql_database" "main" {
  name      = "appdb"
  server_id = azurerm_mssql_server.main.id
  sku_name  = "S0"
}

# Blob Storage
resource "azurerm_storage_account" "main" {
  name                     = "appstorageaccount"
  resource_group_name      = azurerm_resource_group.main.name
  location                 = azurerm_resource_group.main.location
  account_tier             = "Standard"
  account_replication_type = "LRS"
}

resource "azurerm_storage_container" "assets" {
  name                  = "assets"
  storage_account_name  = azurerm_storage_account.main.name
  container_access_type = "private"
}

# Azure Cache for Redis
resource "azurerm_redis_cache" "main" {
  name                = "app-cache"
  location            = azurerm_resource_group.main.location
  resource_group_name = azurerm_resource_group.main.name
  capacity            = 0
  family              = "C"
  sku_name            = "Basic"
}

# Application Gateway
resource "azurerm_application_gateway" "main" {
  name                = "app-gateway"
  resource_group_name = azurerm_resource_group.main.name
  location            = azurerm_resource_group.main.location

  sku {
    name     = "Standard_v2"
    tier     = "Standard_v2"
    capacity = 2
  }

  # ... gateway configuration
}
```

### AgnosTech Migration

```bash
agnostech migrate ./terraform \
  --output ./azure-stack \
  --domain api.example.com
```

### Generated `docker-compose.yml`

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
    networks:
      - agnostech
    labels:
      - "agnostech.source=azurerm_application_gateway"

  api:
    image: node:18-alpine
    restart: unless-stopped
    working_dir: /app
    command: npm start
    environment:
      NODE_ENV: production
      # Using PostgreSQL instead of Azure SQL
      DATABASE_URL: postgres://app:${DB_PASSWORD}@postgres:5432/app
      REDIS_URL: redis://redis:6379
      AZURE_STORAGE_ENDPOINT: http://minio:9000
      AZURE_STORAGE_ACCESS_KEY: ${MINIO_ACCESS_KEY}
      AZURE_STORAGE_SECRET_KEY: ${MINIO_SECRET_KEY}
    volumes:
      - ./api:/app
    depends_on:
      - postgres
      - redis
      - minio
    networks:
      - agnostech
    labels:
      - "agnostech.source=azurerm_function_app"
      - "traefik.enable=true"
      - "traefik.http.routers.api.rule=Host(`api.example.com`)"
      - "traefik.http.routers.api.tls.certresolver=letsencrypt"

  # Note: Using PostgreSQL as Azure SQL alternative
  # For SQL Server compatibility, use mcr.microsoft.com/mssql/server
  postgres:
    image: postgres:15-alpine
    restart: unless-stopped
    environment:
      POSTGRES_DB: app
      POSTGRES_USER: app
      POSTGRES_PASSWORD: ${DB_PASSWORD}
    volumes:
      - ./data/postgres:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U app"]
      interval: 10s
      timeout: 5s
      retries: 5
    networks:
      - agnostech
    labels:
      - "agnostech.source=azurerm_mssql_database"

  # Alternative: Use MSSQL for full Azure SQL compatibility
  # mssql:
  #   image: mcr.microsoft.com/mssql/server:2022-latest
  #   environment:
  #     ACCEPT_EULA: "Y"
  #     MSSQL_SA_PASSWORD: ${DB_PASSWORD}

  minio:
    image: minio/minio:latest
    command: server /data --console-address ":9001"
    restart: unless-stopped
    ports:
      - "9001:9001"
    environment:
      MINIO_ROOT_USER: ${MINIO_ACCESS_KEY}
      MINIO_ROOT_PASSWORD: ${MINIO_SECRET_KEY}
    volumes:
      - ./data/minio:/data
    networks:
      - agnostech
    labels:
      - "agnostech.source=azurerm_storage_account"

  redis:
    image: redis:7-alpine
    restart: unless-stopped
    command: redis-server --appendonly yes --requirepass ${REDIS_PASSWORD}
    volumes:
      - ./data/redis:/data
    networks:
      - agnostech
    labels:
      - "agnostech.source=azurerm_redis_cache"

networks:
  agnostech:
    driver: bridge
```

---

## SDK Migration Guide

### GCP SDK Changes

| GCP SDK | Self-Hosted Alternative |
|---------|------------------------|
| `@google-cloud/storage` | MinIO SDK or AWS S3 SDK |
| `@google-cloud/sql` | `pg` (node-postgres) |
| `@google-cloud/redis` | `ioredis` |

**Before (GCP):**
```javascript
const { Storage } = require('@google-cloud/storage');
const storage = new Storage();

async function uploadFile(file) {
  await storage.bucket('my-bucket').upload(file);
}
```

**After (MinIO):**
```javascript
const { S3Client, PutObjectCommand } = require('@aws-sdk/client-s3');

const s3 = new S3Client({
  endpoint: process.env.S3_ENDPOINT,
  credentials: {
    accessKeyId: process.env.S3_ACCESS_KEY,
    secretAccessKey: process.env.S3_SECRET_KEY,
  },
  forcePathStyle: true,
});

async function uploadFile(file) {
  await s3.send(new PutObjectCommand({
    Bucket: 'my-bucket',
    Key: file.name,
    Body: file.buffer,
  }));
}
```

### Azure SDK Changes

| Azure SDK | Self-Hosted Alternative |
|-----------|------------------------|
| `@azure/storage-blob` | MinIO SDK or AWS S3 SDK |
| `mssql` | `pg` or keep `mssql` with MSSQL container |
| `@azure/cache-redis` | `ioredis` |

**Before (Azure):**
```javascript
const { BlobServiceClient } = require('@azure/storage-blob');

const blobService = BlobServiceClient.fromConnectionString(process.env.AZURE_STORAGE_CONNECTION_STRING);

async function uploadFile(file) {
  const containerClient = blobService.getContainerClient('assets');
  const blockBlobClient = containerClient.getBlockBlobClient(file.name);
  await blockBlobClient.upload(file.buffer, file.size);
}
```

**After (MinIO):**
```javascript
const { S3Client, PutObjectCommand } = require('@aws-sdk/client-s3');

const s3 = new S3Client({
  endpoint: process.env.S3_ENDPOINT,
  credentials: {
    accessKeyId: process.env.S3_ACCESS_KEY,
    secretAccessKey: process.env.S3_SECRET_KEY,
  },
  forcePathStyle: true,
});

async function uploadFile(file) {
  await s3.send(new PutObjectCommand({
    Bucket: 'assets',
    Key: file.name,
    Body: file.buffer,
  }));
}
```

---

## Deployment Checklist

### Pre-Migration

- [ ] Export current cloud resources inventory
- [ ] Document all service dependencies
- [ ] Create database backups
- [ ] Export object storage data
- [ ] Document environment variables
- [ ] Review network/firewall rules

### Migration

- [ ] Run AgnosTech analysis
- [ ] Generate self-hosted stack
- [ ] Configure environment variables
- [ ] Set up SSL certificates
- [ ] Initialize databases
- [ ] Migrate data (databases, files)
- [ ] Update application SDKs
- [ ] Deploy containers

### Post-Migration

- [ ] Verify all services are healthy
- [ ] Test application functionality
- [ ] Configure monitoring/alerting
- [ ] Set up backup automation
- [ ] Update DNS records
- [ ] Document new architecture
- [ ] Decommission cloud resources

---

## Cost Comparison

| Service | GCP/Azure Monthly | Self-Hosted Monthly |
|---------|-------------------|---------------------|
| Compute (2 vCPU) | ~$60 | - |
| Database | ~$25 | - |
| Storage (100GB) | ~$2 | - |
| Cache | ~$15 | - |
| Load Balancer | ~$20 | - |
| **Total Cloud** | **~$122** | - |
| VPS (4GB RAM) | - | ~$20 |
| **Total Self-Hosted** | - | **~$20** |

*Prices are approximate and vary by region and provider.*
