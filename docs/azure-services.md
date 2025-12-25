# Azure Service Mapping Reference

This document provides a comprehensive reference for all Microsoft Azure services supported by AgnosTech and their self-hosted equivalents.

## Overview

AgnosTech analyzes your Azure infrastructure (via Terraform state or HCL files) and generates Docker Compose configurations using open-source, self-hosted alternatives. Azure support enables organizations to achieve digital sovereignty by migrating away from Microsoft cloud services.

## Service Mapping Summary

| Category | Azure Service | Self-Hosted | Mapper File | Parity |
|----------|---------------|-------------|-------------|--------|
| **Compute** | Virtual Machines | Docker | `azure/compute/vm.go` | Full |
| | Azure Functions | Docker | `azure/compute/function.go` | Full |
| | AKS | Kubernetes | `azure/compute/aks.go` | Partial |
| **Storage** | Blob Storage | MinIO | `azure/storage/blob.go` | Full |
| | Storage Account | MinIO | `azure/storage/storage_account.go` | Full |
| | Managed Disk | Docker Volumes | `azure/storage/managed_disk.go` | Full |
| | Azure Files | NFS/SMB | `azure/storage/files.go` | Full |
| **Database** | Azure SQL | MSSQL/PostgreSQL | `azure/database/azuresql.go` | Full |
| | PostgreSQL Flexible | PostgreSQL | `azure/database/postgres.go` | Full |
| | MySQL Flexible | MySQL | `azure/database/mysql.go` | Full |
| | CosmosDB | MongoDB/ScyllaDB | `azure/database/cosmosdb.go` | Full |
| | Azure Cache for Redis | Redis | `azure/database/cache.go` | Full |
| **Networking** | Load Balancer | Traefik | `azure/networking/lb.go` | Full |
| | Application Gateway | Traefik | `azure/networking/appgateway.go` | Full |
| | Azure DNS | PowerDNS | `azure/networking/dns.go` | Full |
| | Azure CDN | Nginx/Traefik | `azure/networking/cdn.go` | Full |
| | Front Door | Traefik | `azure/networking/frontdoor.go` | Full |
| | Virtual Network | Docker Networks | `azure/networking/vnet.go` | Full |
| **Messaging** | Service Bus | RabbitMQ | `azure/messaging/servicebus.go` | Full |
| | Service Bus Queue | RabbitMQ | `azure/messaging/servicebus_queue.go` | Full |
| | Event Hub | Kafka | `azure/messaging/eventhub.go` | Full |
| | Event Grid | Event Router | `azure/messaging/eventgrid.go` | Partial |
| | Logic Apps | n8n/Node-RED | `azure/messaging/logicapp.go` | Partial |
| **Security** | Azure AD B2C | Keycloak | `azure/security/adb2c.go` | Full |
| | Key Vault | Vault | `azure/security/keyvault.go` | Full |
| | Azure Firewall | iptables/UFW | `azure/security/firewall.go` | Partial |

---

## Compute Services

### Virtual Machines

**Self-Hosted Alternative:** Docker Containers

Azure VMs are converted to Docker containers with equivalent resource configurations.

**Terraform Input:**
```hcl
resource "azurerm_linux_virtual_machine" "web" {
  name                = "web-server"
  resource_group_name = azurerm_resource_group.main.name
  location            = azurerm_resource_group.main.location
  size                = "Standard_B2s"
  admin_username      = "adminuser"

  network_interface_ids = [
    azurerm_network_interface.web.id,
  ]

  os_disk {
    caching              = "ReadWrite"
    storage_account_type = "Standard_LRS"
  }

  source_image_reference {
    publisher = "Canonical"
    offer     = "0001-com-ubuntu-server-jammy"
    sku       = "22_04-lts"
    version   = "latest"
  }
}
```

**Docker Output:**
```yaml
services:
  web-server:
    image: ubuntu:22.04
    restart: unless-stopped
    labels:
      agnostech.source: azurerm_linux_virtual_machine
      agnostech.size: Standard_B2s
    networks:
      - agnostech
```

### Azure Functions

**Self-Hosted Alternative:** Docker Container with HTTP endpoint

**Terraform Input:**
```hcl
resource "azurerm_function_app" "api" {
  name                       = "api-functions"
  location                   = azurerm_resource_group.main.location
  resource_group_name        = azurerm_resource_group.main.name
  app_service_plan_id        = azurerm_app_service_plan.main.id
  storage_account_name       = azurerm_storage_account.main.name
  storage_account_access_key = azurerm_storage_account.main.primary_access_key

  app_settings = {
    FUNCTIONS_WORKER_RUNTIME = "node"
    NODE_VERSION             = "18"
  }
}
```

**Docker Output:**
```yaml
services:
  api-functions:
    image: node:18-alpine
    restart: unless-stopped
    environment:
      FUNCTIONS_WORKER_RUNTIME: node
    labels:
      agnostech.source: azurerm_function_app
      traefik.enable: "true"
      traefik.http.routers.api-functions.rule: "Host(`api.localhost`)"
    networks:
      - agnostech
```

### AKS (Azure Kubernetes Service)

**Self-Hosted Alternative:** Kubernetes / K3s

AKS clusters generate Kubernetes manifest files for deployment to any K8s cluster.

---

## Storage Services

### Blob Storage

**Self-Hosted Alternative:** MinIO

MinIO provides an S3-compatible API that works with Azure Blob SDK after configuration changes.

**Terraform Input:**
```hcl
resource "azurerm_storage_account" "main" {
  name                     = "mystorageaccount"
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
      agnostech.source: azurerm_storage_account
```

### Azure Files

**Self-Hosted Alternative:** NFS/SMB Server

Azure Files shares are converted to NFS or SMB configurations.

---

## Database Services

### Azure SQL Database

**Self-Hosted Alternatives:**
- MSSQL: `mcr.microsoft.com/mssql/server:2022-latest`
- PostgreSQL: `postgres:16-alpine` (for migration)

**Terraform Input:**
```hcl
resource "azurerm_mssql_server" "main" {
  name                         = "sql-server"
  resource_group_name          = azurerm_resource_group.main.name
  location                     = azurerm_resource_group.main.location
  version                      = "12.0"
  administrator_login          = "sqladmin"
  administrator_login_password = "P@ssw0rd1234!"
}

resource "azurerm_mssql_database" "main" {
  name      = "mydb"
  server_id = azurerm_mssql_server.main.id
  sku_name  = "S0"
}
```

**Docker Output:**
```yaml
services:
  mssql:
    image: mcr.microsoft.com/mssql/server:2022-latest
    restart: unless-stopped
    ports:
      - "1433:1433"
    environment:
      ACCEPT_EULA: "Y"
      MSSQL_SA_PASSWORD: "P@ssw0rd1234!"
      MSSQL_PID: "Express"
    volumes:
      - ./data/mssql:/var/opt/mssql
    healthcheck:
      test: ["CMD", "/opt/mssql-tools/bin/sqlcmd", "-S", "localhost", "-U", "sa", "-P", "$$MSSQL_SA_PASSWORD", "-Q", "SELECT 1"]
      interval: 10s
      timeout: 5s
      retries: 5
    labels:
      agnostech.source: azurerm_mssql_server
```

### PostgreSQL Flexible Server

**Self-Hosted Alternative:** PostgreSQL

**Terraform Input:**
```hcl
resource "azurerm_postgresql_flexible_server" "main" {
  name                   = "postgres-server"
  resource_group_name    = azurerm_resource_group.main.name
  location               = azurerm_resource_group.main.location
  version                = "16"
  administrator_login    = "psqladmin"
  administrator_password = "P@ssw0rd1234!"
  sku_name               = "GP_Standard_D2s_v3"
  storage_mb             = 32768
}
```

**Docker Output:**
```yaml
services:
  postgres:
    image: postgres:16-alpine
    restart: unless-stopped
    ports:
      - "5432:5432"
    environment:
      POSTGRES_DB: azure_db
      POSTGRES_USER: psqladmin
      POSTGRES_PASSWORD: changeme
      PGDATA: /var/lib/postgresql/data/pgdata
    volumes:
      - ./data/postgres:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U psqladmin"]
      interval: 10s
      timeout: 5s
      retries: 5
    labels:
      agnostech.source: azurerm_postgresql_flexible_server
```

### CosmosDB

**Self-Hosted Alternatives:**
- MongoDB API: MongoDB
- Table API: ScyllaDB
- SQL API: FerretDB (PostgreSQL-backed MongoDB)

**Docker Output (MongoDB API):**
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
```

### Azure Cache for Redis

**Self-Hosted Alternative:** Redis

**Docker Output:**
```yaml
services:
  redis:
    image: redis:7-alpine
    restart: unless-stopped
    ports:
      - "6379:6379"
    command: redis-server --appendonly yes --requirepass changeme
    volumes:
      - ./data/redis:/data
    healthcheck:
      test: ["CMD", "redis-cli", "-a", "changeme", "ping"]
      interval: 10s
      timeout: 5s
      retries: 5
```

---

## Networking Services

### Load Balancer / Application Gateway

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
    labels:
      agnostech.source: azurerm_lb
```

### Azure Front Door

**Self-Hosted Alternative:** Traefik with global routing

Front Door configurations are converted to Traefik with appropriate routing rules.

### Virtual Network

**Self-Hosted Alternative:** Docker Networks

VNets and subnets are mapped to Docker network configurations.

---

## Messaging Services

### Service Bus

**Self-Hosted Alternative:** RabbitMQ

Service Bus queues and topics are converted to RabbitMQ queues and exchanges.

**Terraform Input:**
```hcl
resource "azurerm_servicebus_namespace" "main" {
  name                = "my-servicebus"
  location            = azurerm_resource_group.main.location
  resource_group_name = azurerm_resource_group.main.name
  sku                 = "Standard"
}

resource "azurerm_servicebus_queue" "orders" {
  name         = "orders"
  namespace_id = azurerm_servicebus_namespace.main.id
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

### Event Hub

**Self-Hosted Alternative:** Apache Kafka

Event Hub namespaces and hubs are converted to Kafka topics.

**Docker Output:**
```yaml
services:
  kafka:
    image: confluentinc/cp-kafka:latest
    restart: unless-stopped
    ports:
      - "9092:9092"
    environment:
      KAFKA_BROKER_ID: 1
      KAFKA_ZOOKEEPER_CONNECT: zookeeper:2181
      KAFKA_ADVERTISED_LISTENERS: PLAINTEXT://kafka:29092,PLAINTEXT_HOST://localhost:9092
      KAFKA_LISTENER_SECURITY_PROTOCOL_MAP: PLAINTEXT:PLAINTEXT,PLAINTEXT_HOST:PLAINTEXT
    depends_on:
      - zookeeper

  zookeeper:
    image: confluentinc/cp-zookeeper:latest
    ports:
      - "2181:2181"
    environment:
      ZOOKEEPER_CLIENT_PORT: 2181
```

### Logic Apps

**Self-Hosted Alternative:** n8n / Node-RED

Logic Apps workflows are documented for manual recreation in n8n or Node-RED.

---

## Security Services

### Azure AD B2C

**Self-Hosted Alternative:** Keycloak

Azure AD B2C configurations are converted to Keycloak realm settings.

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
      KC_DB: postgres
      KC_DB_URL: jdbc:postgresql://postgres:5432/keycloak
      KC_DB_USERNAME: keycloak
      KC_DB_PASSWORD: changeme
    depends_on:
      - postgres
    labels:
      agnostech.source: azurerm_aadb2c_directory
```

### Key Vault

**Self-Hosted Alternative:** HashiCorp Vault

**Docker Output:**
```yaml
services:
  vault:
    image: hashicorp/vault:latest
    restart: unless-stopped
    ports:
      - "8200:8200"
    environment:
      VAULT_DEV_ROOT_TOKEN_ID: root
      VAULT_DEV_LISTEN_ADDRESS: 0.0.0.0:8200
    cap_add:
      - IPC_LOCK
    volumes:
      - ./data/vault:/vault/data
```

### Azure Firewall

**Self-Hosted Alternative:** iptables / UFW

Firewall rules are exported as iptables rules for host-level configuration.

---

## Azure-Specific Considerations

### Managed Identity Migration

Replace Azure Managed Identity with:
1. Service-specific credentials in environment variables
2. HashiCorp Vault for secret management
3. Docker secrets for sensitive data

### SDK Migration

| Azure SDK | Self-Hosted Alternative |
|-----------|------------------------|
| `Azure.Storage.Blobs` | MinIO .NET SDK or AWS SDK |
| `Microsoft.Azure.Cosmos` | MongoDB Driver |
| `Azure.Messaging.ServiceBus` | RabbitMQ Client |
| `Azure.Security.KeyVault` | Vault Client |

### Connection String Updates

Update connection strings from:
```
DefaultEndpointsProtocol=https;AccountName=xxx;AccountKey=xxx
```

To MinIO format:
```
endpoint=localhost:9000;accessKey=minioadmin;secretKey=minioadmin
```

---

## Limitations and Manual Steps

### Services Requiring Manual Configuration

1. **Azure DevOps** - Use GitLab, Gitea, or GitHub Actions
2. **Power BI** - Consider Apache Superset or Metabase
3. **Azure Synapse** - Use Apache Spark or Trino
4. **Azure Cognitive Services** - Use open-source ML models

### Feature Gaps

| Azure Feature | Status | Notes |
|---------------|--------|-------|
| Geo-redundancy | Manual | Configure replication manually |
| Azure AD Integration | Partial | Use Keycloak federation |
| Azure Monitor | Replaced | Use Prometheus + Grafana |
| Azure Backup | Replaced | Use restic or Velero |

---

## Adding New Azure Mappers

To add support for a new Azure service:

1. Create mapper in `internal/infrastructure/mapper/azure/<category>/<service>.go`
2. Implement the `Mapper` interface
3. Register in `internal/infrastructure/mapper/azure/registry.go`
4. Add resource type to `internal/domain/resource/types.go`
5. Write tests and update documentation

See [Contributing Guide](contributing.md) for detailed instructions.
