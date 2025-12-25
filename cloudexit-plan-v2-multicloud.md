# AgnosTech — Plan Multi-Cloud & High Availability

**Extension du plan initial : GCP, Azure, Multi-AZ, Providers EU**

Version 2.0 | Décembre 2025

---

## 1. Vision Étendue

### 1.1 Nouveau Scope

```
┌─────────────────────────────────────────────────────────────────┐
│                      AgnosTech v2 Vision                         │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  SOURCES (Input)                                                │
│  ├── AWS          ✅ Plan v1                                    │
│  ├── Google Cloud 🆕 Ce plan                                    │
│  ├── Microsoft Azure 🆕 Ce plan                                 │
│  └── Multi-cloud (mix des 3)                                    │
│                                                                 │
│  TARGETS (Output)                                               │
│  ├── Self-Hosted                                                │
│  │   ├── Single Server (Docker Compose)                         │
│  │   ├── Multi-Server (Docker Swarm)                            │
│  │   └── Kubernetes (K3s/K8s)                                   │
│  │                                                              │
│  ├── EU Cloud Providers 🆕                                      │
│  │   ├── Scaleway (FR)                                          │
│  │   ├── OVHcloud (FR)                                          │
│  │   ├── Hetzner (DE)                                           │
│  │   ├── Exoscale (CH)                                          │
│  │   └── Infomaniak (CH)                                        │
│  │                                                              │
│  └── Hybrid                                                     │
│      └── Mix self-hosted + EU cloud                             │
│                                                                 │
│  HIGH AVAILABILITY 🆕                                           │
│  ├── Multi-AZ equivalent                                        │
│  ├── Database replication                                       │
│  ├── Load balancing                                             │
│  └── Auto-failover                                              │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

### 1.2 Commandes CLI Étendues

```bash
# Analyze depuis n'importe quelle source
agnostech analyze ./terraform                    # Auto-detect
agnostech analyze ./terraform --source=aws
agnostech analyze ./terraform --source=gcp
agnostech analyze ./terraform --source=azure
agnostech analyze --api --source=aws             # Via API directe

# Migrate vers différentes cibles
agnostech migrate ./terraform --target=self-hosted
agnostech migrate ./terraform --target=scaleway
agnostech migrate ./terraform --target=ovh
agnostech migrate ./terraform --target=hetzner

# Options HA
agnostech migrate ./terraform --target=self-hosted --ha=multi-server
agnostech migrate ./terraform --target=scaleway --ha=multi-az

# Mode interactif (wizard)
agnostech migrate ./terraform --interactive
```

---

## 2. Inventaire Google Cloud Platform (GCP)

### 2.1 Mapping GCP → Self-Hosted

| GCP Service | Description | Self-Hosted | EU Cloud |
|-------------|-------------|-------------|----------|
| **Compute** |
| Compute Engine | VMs | Docker | Instance |
| Cloud Functions | Serverless | OpenFaaS | Scaleway Functions |
| Cloud Run | Containers | Docker + Traefik | Scaleway Containers |
| GKE | Kubernetes | K3s | Kapsule (Scaleway) |
| App Engine | PaaS | Dokku | - |
| **Storage** |
| Cloud Storage | Object storage | MinIO | Object Storage |
| Persistent Disk | Block storage | Docker volumes | Block Storage |
| Filestore | NFS | NFS server | - |
| **Database** |
| Cloud SQL | Managed SQL | PostgreSQL/MySQL | Managed DB |
| Cloud Spanner | Global SQL | CockroachDB | - |
| Firestore | Document DB | MongoDB | - |
| Bigtable | Wide-column | ScyllaDB | - |
| Memorystore | Redis/Memcached | Redis | - |
| **Networking** |
| Cloud Load Balancing | LB | Traefik/HAProxy | Load Balancer |
| Cloud CDN | CDN | Nginx cache | - |
| Cloud DNS | DNS | PowerDNS | DNS |
| Cloud Armor | WAF | ModSecurity | - |
| VPC | Network | Docker networks | VPC |
| **Security** |
| Identity Platform | Auth | Keycloak | - |
| Secret Manager | Secrets | Vault | - |
| Cloud KMS | Key mgmt | Vault Transit | - |
| IAM | Access mgmt | Keycloak + policies | IAM |
| **Messaging** |
| Pub/Sub | Messaging | RabbitMQ/NATS | - |
| Cloud Tasks | Task queue | Celery/Bull | - |
| Cloud Scheduler | Cron | Ofelia/cron | - |
| Eventarc | Events | EventBridge equiv | - |
| Workflows | Orchestration | Temporal | - |
| **Monitoring** |
| Cloud Monitoring | Metrics | Prometheus | - |
| Cloud Logging | Logs | Loki | - |
| Cloud Trace | Tracing | Jaeger | - |
| Error Reporting | Errors | Sentry | - |
| **AI/ML** |
| Vertex AI | ML Platform | MLflow | - |
| Vision AI | Image | YOLO/OpenCV | - |
| Speech-to-Text | STT | Whisper | - |
| Translation | Translation | LibreTranslate | - |

### 2.2 Terraform GCP Resources

```hcl
# Exemple infrastructure GCP typique

resource "google_compute_instance" "web" {
  name         = "web-server"
  machine_type = "e2-medium"
  zone         = "europe-west1-b"
  
  boot_disk {
    initialize_params {
      image = "debian-cloud/debian-11"
    }
  }
  
  network_interface {
    network = google_compute_network.vpc.name
    access_config {}
  }
}

resource "google_sql_database_instance" "main" {
  name             = "main-db"
  database_version = "POSTGRES_15"
  region           = "europe-west1"
  
  settings {
    tier = "db-f1-micro"
    availability_type = "REGIONAL"  # Multi-AZ
    
    backup_configuration {
      enabled = true
      point_in_time_recovery_enabled = true
    }
  }
}

resource "google_storage_bucket" "assets" {
  name     = "myapp-assets"
  location = "EU"
  
  versioning {
    enabled = true
  }
  
  lifecycle_rule {
    condition {
      age = 90
    }
    action {
      type = "SetStorageClass"
      storage_class = "COLDLINE"
    }
  }
}

resource "google_cloud_run_service" "api" {
  name     = "api-service"
  location = "europe-west1"
  
  template {
    spec {
      containers {
        image = "gcr.io/myproject/api:latest"
        
        env {
          name  = "DB_HOST"
          value = google_sql_database_instance.main.connection_name
        }
      }
    }
  }
}

resource "google_pubsub_topic" "events" {
  name = "app-events"
}

resource "google_pubsub_subscription" "worker" {
  name  = "worker-sub"
  topic = google_pubsub_topic.events.name
  
  ack_deadline_seconds = 60
  
  dead_letter_policy {
    dead_letter_topic     = google_pubsub_topic.dlq.id
    max_delivery_attempts = 5
  }
}
```

---

## 3. Inventaire Microsoft Azure

### 3.1 Mapping Azure → Self-Hosted

| Azure Service | Description | Self-Hosted | EU Cloud |
|---------------|-------------|-------------|----------|
| **Compute** |
| Virtual Machines | VMs | Docker | Instance |
| Azure Functions | Serverless | OpenFaaS | Scaleway Functions |
| Container Instances | Containers | Docker | Containers |
| AKS | Kubernetes | K3s | Kapsule |
| App Service | PaaS | Dokku | - |
| **Storage** |
| Blob Storage | Object storage | MinIO | Object Storage |
| Managed Disks | Block storage | Docker volumes | Block Storage |
| Azure Files | File shares | NFS/Samba | - |
| **Database** |
| Azure SQL | SQL Server | PostgreSQL* | Managed DB |
| Cosmos DB | Multi-model | MongoDB/CockroachDB | - |
| Azure Database for PostgreSQL | PG | PostgreSQL | Managed DB |
| Azure Database for MySQL | MySQL | MySQL/MariaDB | Managed DB |
| Azure Cache for Redis | Redis | Redis | - |
| **Networking** |
| Azure Load Balancer | LB | Traefik/HAProxy | Load Balancer |
| Application Gateway | L7 LB | Traefik | - |
| Azure CDN | CDN | Nginx cache | - |
| Azure DNS | DNS | PowerDNS | DNS |
| Front Door | Global LB | Traefik + Geo | - |
| Azure Firewall | Firewall | iptables/nftables | - |
| **Security** |
| Azure AD B2C | Auth | Keycloak | - |
| Key Vault | Secrets/Keys | Vault | - |
| **Messaging** |
| Service Bus | Messaging | RabbitMQ | - |
| Event Hubs | Streaming | Kafka/Redpanda | - |
| Event Grid | Events | RabbitMQ | - |
| Queue Storage | Queues | Redis/RabbitMQ | - |
| Logic Apps | Workflows | n8n/Temporal | - |
| **Monitoring** |
| Azure Monitor | Metrics | Prometheus | - |
| Log Analytics | Logs | Loki/ELK | - |
| Application Insights | APM | Jaeger + Prometheus | - |

*Note: SQL Server → PostgreSQL nécessite migration schema

### 3.2 Terraform Azure Resources

```hcl
# Exemple infrastructure Azure typique

resource "azurerm_linux_virtual_machine" "web" {
  name                = "web-server"
  resource_group_name = azurerm_resource_group.main.name
  location            = "West Europe"
  size                = "Standard_B2s"
  
  availability_set_id = azurerm_availability_set.web.id  # Multi-AZ
  
  admin_username = "adminuser"
  
  network_interface_ids = [
    azurerm_network_interface.web.id,
  ]
  
  os_disk {
    caching              = "ReadWrite"
    storage_account_type = "Premium_LRS"
  }
  
  source_image_reference {
    publisher = "Canonical"
    offer     = "0001-com-ubuntu-server-jammy"
    sku       = "22_04-lts"
    version   = "latest"
  }
}

resource "azurerm_postgresql_flexible_server" "main" {
  name                   = "myapp-db"
  resource_group_name    = azurerm_resource_group.main.name
  location               = "West Europe"
  version                = "15"
  administrator_login    = "psqladmin"
  administrator_password = var.db_password
  zone                   = "1"
  
  high_availability {
    mode                      = "ZoneRedundant"  # Multi-AZ
    standby_availability_zone = "2"
  }
  
  storage_mb = 32768
  sku_name   = "GP_Standard_D2s_v3"
}

resource "azurerm_storage_account" "main" {
  name                     = "myappstorage"
  resource_group_name      = azurerm_resource_group.main.name
  location                 = "West Europe"
  account_tier             = "Standard"
  account_replication_type = "GRS"  # Geo-redundant
}

resource "azurerm_storage_container" "assets" {
  name                  = "assets"
  storage_account_name  = azurerm_storage_account.main.name
  container_access_type = "blob"
}

resource "azurerm_servicebus_namespace" "main" {
  name                = "myapp-servicebus"
  location            = "West Europe"
  resource_group_name = azurerm_resource_group.main.name
  sku                 = "Standard"
}

resource "azurerm_servicebus_queue" "tasks" {
  name         = "tasks"
  namespace_id = azurerm_servicebus_namespace.main.id
  
  dead_lettering_on_message_expiration = true
  max_delivery_count                   = 5
}
```

---

## 4. High Availability & Multi-AZ

### 4.1 Le Problème

Les cloud providers offrent du Multi-AZ "gratuit" (inclus dans le prix) :
- AWS: `multi_az = true` sur RDS
- GCP: `availability_type = "REGIONAL"` sur Cloud SQL  
- Azure: `high_availability { mode = "ZoneRedundant" }`

En self-hosted, il faut le construire soi-même.

### 4.2 Niveaux de HA

```
┌─────────────────────────────────────────────────────────────────┐
│                    HA Levels in AgnosTech                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  LEVEL 0: Single Server (--ha=none)                             │
│  ├── 1 serveur, tous services                                   │
│  ├── Backups automatiques                                       │
│  ├── Monitoring + alertes                                       │
│  └── RTO: heures, RPO: minutes-heures                          │
│                                                                 │
│  LEVEL 1: Single Server + Replicas (--ha=basic)                 │
│  ├── 1 serveur principal                                        │
│  ├── DB avec réplication async vers backup                      │
│  ├── S3/MinIO répliqué vers backup                              │
│  └── RTO: ~1h, RPO: minutes                                     │
│                                                                 │
│  LEVEL 2: Multi-Server Active-Passive (--ha=multi-server)       │
│  ├── 2+ serveurs (1 active, N passive)                          │
│  ├── Floating IP ou DNS failover                                │
│  ├── DB replication sync                                        │
│  ├── Shared storage ou sync                                     │
│  └── RTO: minutes, RPO: seconds                                 │
│                                                                 │
│  LEVEL 3: Multi-Server Active-Active (--ha=cluster)             │
│  ├── 3+ serveurs tous actifs                                    │
│  ├── Load balancer distribué                                    │
│  ├── DB cluster (Patroni/Galera)                                │
│  ├── Distributed storage (MinIO cluster)                        │
│  └── RTO: seconds, RPO: ~0                                      │
│                                                                 │
│  LEVEL 4: Multi-DC / Geo (--ha=geo)                             │
│  ├── Serveurs dans 2+ datacenters                               │
│  ├── GeoDNS ou Anycast                                          │
│  ├── Async replication cross-DC                                 │
│  └── RTO: seconds, RPO: seconds-minutes                         │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

### 4.3 Architecture Multi-Server Self-Hosted

```yaml
# docker-compose.ha.yml - Level 2/3 HA

version: "3.8"

services:
  # ─────────────────────────────────────────────────────
  # LOAD BALANCER (sur chaque node)
  # ─────────────────────────────────────────────────────
  traefik:
    image: traefik:v3.0
    command:
      - "--providers.docker.swarmMode=true"
      - "--providers.docker.exposedbydefault=false"
      - "--entrypoints.web.address=:80"
      - "--entrypoints.websecure.address=:443"
    ports:
      - target: 80
        published: 80
        mode: host
      - target: 443
        published: 443
        mode: host
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    networks:
      - traefik-public
    deploy:
      mode: global  # Sur chaque node
      placement:
        constraints:
          - node.role == manager

  # ─────────────────────────────────────────────────────
  # DATABASE CLUSTER (Patroni + PostgreSQL)
  # ─────────────────────────────────────────────────────
  postgres:
    image: postgres:15-alpine
    environment:
      POSTGRES_DB: myapp
      POSTGRES_USER: admin
      POSTGRES_PASSWORD_FILE: /run/secrets/db_password
    volumes:
      - postgres_data:/var/lib/postgresql/data
    secrets:
      - db_password
    networks:
      - internal
    deploy:
      replicas: 1
      placement:
        constraints:
          - node.labels.db == primary

  postgres-replica:
    image: postgres:15-alpine
    environment:
      POSTGRES_DB: myapp
      POSTGRES_USER: admin
      POSTGRES_PASSWORD_FILE: /run/secrets/db_password
      POSTGRES_PRIMARY_HOST: postgres
    command: |
      bash -c "
        until pg_isready -h postgres -U admin; do sleep 1; done
        pg_basebackup -h postgres -D /var/lib/postgresql/data -U replicator -Fp -Xs -P -R
        postgres
      "
    volumes:
      - postgres_replica_data:/var/lib/postgresql/data
    secrets:
      - db_password
    networks:
      - internal
    deploy:
      replicas: 1
      placement:
        constraints:
          - node.labels.db == replica

  # ─────────────────────────────────────────────────────
  # PGPOOL-II (Connection pooling + failover)
  # ─────────────────────────────────────────────────────
  pgpool:
    image: bitnami/pgpool:4
    environment:
      PGPOOL_BACKEND_NODES: "0:postgres:5432,1:postgres-replica:5432"
      PGPOOL_SR_CHECK_USER: replicator
      PGPOOL_SR_CHECK_PASSWORD_FILE: /run/secrets/db_password
      PGPOOL_ENABLE_LOAD_BALANCING: "yes"
      PGPOOL_POSTGRES_USERNAME: admin
      PGPOOL_POSTGRES_PASSWORD_FILE: /run/secrets/db_password
      PGPOOL_ADMIN_USERNAME: pgpool_admin
      PGPOOL_ADMIN_PASSWORD_FILE: /run/secrets/pgpool_password
    secrets:
      - db_password
      - pgpool_password
    networks:
      - internal
    deploy:
      replicas: 2

  # ─────────────────────────────────────────────────────
  # MINIO CLUSTER (Distributed mode)
  # ─────────────────────────────────────────────────────
  minio:
    image: quay.io/minio/minio:latest
    command: server --console-address ":9001" http://minio{1...4}/data{1...2}
    environment:
      MINIO_ROOT_USER: ${MINIO_ROOT_USER}
      MINIO_ROOT_PASSWORD: ${MINIO_ROOT_PASSWORD}
    volumes:
      - minio_data1:/data1
      - minio_data2:/data2
    networks:
      - internal
    deploy:
      replicas: 4
      endpoint_mode: dnsrr

  # ─────────────────────────────────────────────────────
  # REDIS SENTINEL (HA Redis)
  # ─────────────────────────────────────────────────────
  redis-master:
    image: redis:7-alpine
    command: redis-server --appendonly yes
    volumes:
      - redis_data:/data
    networks:
      - internal
    deploy:
      replicas: 1
      placement:
        constraints:
          - node.labels.redis == master

  redis-replica:
    image: redis:7-alpine
    command: redis-server --replicaof redis-master 6379 --appendonly yes
    volumes:
      - redis_replica_data:/data
    networks:
      - internal
    deploy:
      replicas: 2

  redis-sentinel:
    image: redis:7-alpine
    command: redis-sentinel /etc/redis/sentinel.conf
    configs:
      - source: sentinel_conf
        target: /etc/redis/sentinel.conf
    networks:
      - internal
    deploy:
      replicas: 3

  # ─────────────────────────────────────────────────────
  # APPLICATION (Stateless, scalable)
  # ─────────────────────────────────────────────────────
  api:
    image: myapp/api:latest
    environment:
      DATABASE_URL: postgres://admin:${DB_PASSWORD}@pgpool:5432/myapp
      REDIS_URL: redis://redis-sentinel:26379
      S3_ENDPOINT: http://minio:9000
    networks:
      - internal
      - traefik-public
    deploy:
      replicas: 3
      labels:
        - "traefik.enable=true"
        - "traefik.http.routers.api.rule=Host(`api.${DOMAIN}`)"
      update_config:
        parallelism: 1
        delay: 10s
      rollback_config:
        parallelism: 1

networks:
  traefik-public:
    driver: overlay
  internal:
    driver: overlay
    internal: true

volumes:
  postgres_data:
  postgres_replica_data:
  minio_data1:
  minio_data2:
  redis_data:
  redis_replica_data:

secrets:
  db_password:
    external: true
  pgpool_password:
    external: true

configs:
  sentinel_conf:
    file: ./configs/redis/sentinel.conf
```

### 4.4 Setup Multi-Server avec Ansible

```yaml
# ansible/playbook-ha.yml

- name: Setup AgnosTech HA Cluster
  hosts: all
  become: yes
  
  vars:
    swarm_manager: "{{ groups['managers'][0] }}"
    
  tasks:
    - name: Install Docker
      apt:
        name: 
          - docker.io
          - docker-compose-plugin
        state: present
        update_cache: yes

    - name: Initialize Swarm on first manager
      docker_swarm:
        state: present
        advertise_addr: "{{ ansible_default_ipv4.address }}"
      when: inventory_hostname == swarm_manager
      register: swarm_init

    - name: Get join token for managers
      command: docker swarm join-token -q manager
      delegate_to: "{{ swarm_manager }}"
      register: manager_token
      when: "'managers' in group_names and inventory_hostname != swarm_manager"

    - name: Get join token for workers
      command: docker swarm join-token -q worker
      delegate_to: "{{ swarm_manager }}"
      register: worker_token
      when: "'workers' in group_names"

    - name: Join Swarm as manager
      docker_swarm:
        state: join
        join_token: "{{ manager_token.stdout }}"
        remote_addrs: ["{{ swarm_manager }}:2377"]
      when: "'managers' in group_names and inventory_hostname != swarm_manager"

    - name: Join Swarm as worker
      docker_swarm:
        state: join
        join_token: "{{ worker_token.stdout }}"
        remote_addrs: ["{{ swarm_manager }}:2377"]
      when: "'workers' in group_names"

    - name: Label nodes for placement
      command: "docker node update --label-add {{ item.label }}={{ item.value }} {{ item.node }}"
      delegate_to: "{{ swarm_manager }}"
      loop:
        - { node: "node1", label: "db", value: "primary" }
        - { node: "node2", label: "db", value: "replica" }
        - { node: "node1", label: "redis", value: "master" }
      when: inventory_hostname == swarm_manager

    - name: Deploy stack
      docker_stack:
        name: agnostech
        compose:
          - /opt/agnostech/docker-compose.ha.yml
        state: present
      delegate_to: "{{ swarm_manager }}"
      when: inventory_hostname == swarm_manager
```

```ini
# ansible/inventory.ini

[managers]
node1 ansible_host=10.0.1.1
node2 ansible_host=10.0.1.2
node3 ansible_host=10.0.1.3

[workers]
node4 ansible_host=10.0.1.4
node5 ansible_host=10.0.1.5

[all:vars]
ansible_user=ubuntu
ansible_ssh_private_key_file=~/.ssh/agnostech
```

---

## 5. Providers EU Cloud

### 5.1 Mapping Unifié

```
┌─────────────────────────────────────────────────────────────────┐
│              Unified Resource Mapping                            │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ABSTRACT      │ AWS         │ GCP          │ AZURE            │
│  ─────────────────────────────────────────────────────────────  │
│  Instance      │ EC2         │ Compute Eng. │ VM               │
│  Function      │ Lambda      │ Cloud Func.  │ Functions        │
│  Container     │ ECS/Fargate │ Cloud Run    │ Container Inst.  │
│  Kubernetes    │ EKS         │ GKE          │ AKS              │
│  ObjectStore   │ S3          │ GCS          │ Blob Storage     │
│  SQL DB        │ RDS         │ Cloud SQL    │ Azure SQL        │
│  NoSQL DB      │ DynamoDB    │ Firestore    │ Cosmos DB        │
│  Cache         │ ElastiCache │ Memorystore  │ Azure Cache      │
│  Queue         │ SQS         │ Pub/Sub      │ Service Bus      │
│  LoadBalancer  │ ALB/NLB     │ Cloud LB     │ Azure LB         │
│  CDN           │ CloudFront  │ Cloud CDN    │ Azure CDN        │
│  DNS           │ Route53     │ Cloud DNS    │ Azure DNS        │
│  Auth          │ Cognito     │ Identity Plf │ Azure AD B2C     │
│  Secrets       │ Secrets Mgr │ Secret Mgr   │ Key Vault        │
│  Monitoring    │ CloudWatch  │ Cloud Mon.   │ Azure Monitor    │
│                                                                 │
│  ─────────────────────────────────────────────────────────────  │
│                                                                 │
│  ABSTRACT      │ SELF-HOSTED │ SCALEWAY     │ OVH    │ HETZNER │
│  ─────────────────────────────────────────────────────────────  │
│  Instance      │ Docker      │ Instance     │ Instance│ Server │
│  Function      │ OpenFaaS    │ Functions    │ -       │ -      │
│  Container     │ Docker      │ Containers   │ -       │ -      │
│  Kubernetes    │ K3s         │ Kapsule      │ MKS     │ -      │
│  ObjectStore   │ MinIO       │ Obj.Storage  │ S3      │ S3*    │
│  SQL DB        │ PostgreSQL  │ Managed DB   │ CloudDB │ -      │
│  NoSQL DB      │ MongoDB     │ -            │ -       │ -      │
│  Cache         │ Redis       │ -            │ -       │ -      │
│  Queue         │ RabbitMQ    │ -            │ -       │ -      │
│  LoadBalancer  │ Traefik     │ Load Bal.    │ LB      │ LB     │
│  CDN           │ Nginx       │ -            │ CDN     │ -      │
│  DNS           │ PowerDNS    │ DNS          │ DNS     │ DNS    │
│  Auth          │ Keycloak    │ -            │ -       │ -      │
│  Secrets       │ Vault       │ Secret Mgr   │ -       │ -      │
│  Monitoring    │ Prometheus  │ Cockpit      │ Metrics │ -      │
│                                                                 │
│  * Hetzner S3 = Compatible mais basique                         │
│  - = Non disponible, utiliser self-hosted sur instance          │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

### 5.2 Scaleway Terraform Generation

```hcl
# Output généré pour target=scaleway

terraform {
  required_providers {
    scaleway = {
      source  = "scaleway/scaleway"
      version = "~> 2.0"
    }
  }
}

provider "scaleway" {
  zone   = "fr-par-1"
  region = "fr-par"
}

# ─────────────────────────────────────────────────────
# COMPUTE
# ─────────────────────────────────────────────────────

resource "scaleway_instance_server" "web" {
  name  = "web-server"
  type  = "DEV1-M"  # Mapping depuis AWS t3.medium
  image = "ubuntu_jammy"
  
  root_volume {
    size_in_gb = 40
  }
  
  tags = ["env:production", "app:myapp"]
}

# Multi-AZ via placement groups
resource "scaleway_instance_placement_group" "ha" {
  name        = "ha-group"
  policy_type = "max_availability"
  policy_mode = "enforced"
}

resource "scaleway_instance_server" "web_ha" {
  count = 3
  name  = "web-server-${count.index}"
  type  = "DEV1-M"
  image = "ubuntu_jammy"
  
  placement_group_id = scaleway_instance_placement_group.ha.id
}

# ─────────────────────────────────────────────────────
# DATABASE (Managed)
# ─────────────────────────────────────────────────────

resource "scaleway_rdb_instance" "main" {
  name           = "myapp-db"
  node_type      = "DB-DEV-S"
  engine         = "PostgreSQL-15"
  is_ha_cluster  = true  # Multi-AZ equivalent
  
  disable_backup = false
  backup_schedule_frequency = 24
  backup_schedule_retention = 7
  
  volume_type    = "bssd"
  volume_size_in_gb = 50
}

resource "scaleway_rdb_database" "myapp" {
  instance_id = scaleway_rdb_instance.main.id
  name        = "myapp"
}

resource "scaleway_rdb_user" "admin" {
  instance_id = scaleway_rdb_instance.main.id
  name        = "admin"
  password    = var.db_password
  is_admin    = true
}

# ─────────────────────────────────────────────────────
# OBJECT STORAGE
# ─────────────────────────────────────────────────────

resource "scaleway_object_bucket" "assets" {
  name = "myapp-assets"
  
  versioning {
    enabled = true
  }
  
  lifecycle_rule {
    id      = "archive-old"
    enabled = true
    
    transition {
      days          = 90
      storage_class = "GLACIER"
    }
    
    expiration {
      days = 365
    }
  }
}

resource "scaleway_object_bucket_acl" "assets" {
  bucket = scaleway_object_bucket.assets.name
  acl    = "private"
}

# ─────────────────────────────────────────────────────
# SERVERLESS FUNCTIONS
# ─────────────────────────────────────────────────────

resource "scaleway_function_namespace" "main" {
  name = "myapp-functions"
}

resource "scaleway_function" "api_handler" {
  namespace_id = scaleway_function_namespace.main.id
  name         = "api-handler"
  runtime      = "node20"
  handler      = "handler.handle"
  privacy      = "public"
  
  min_scale    = 1
  max_scale    = 10
  memory_limit = 256
  timeout      = 30
  
  environment_variables = {
    DB_HOST = scaleway_rdb_instance.main.endpoint_ip
    BUCKET  = scaleway_object_bucket.assets.name
  }
  
  secret_environment_variables = {
    DB_PASSWORD = var.db_password
  }
}

# ─────────────────────────────────────────────────────
# CONTAINERS (Serverless)
# ─────────────────────────────────────────────────────

resource "scaleway_container_namespace" "main" {
  name = "myapp"
}

resource "scaleway_container" "api" {
  namespace_id = scaleway_container_namespace.main.id
  name         = "api"
  
  registry_image = "rg.fr-par.scw.cloud/myapp/api:latest"
  
  min_scale    = 1
  max_scale    = 10
  memory_limit = 512
  cpu_limit    = 500
  
  port = 8080
  
  environment_variables = {
    DB_HOST = scaleway_rdb_instance.main.endpoint_ip
  }
  
  secret_environment_variables = {
    DB_PASSWORD = var.db_password
  }
}

# ─────────────────────────────────────────────────────
# LOAD BALANCER
# ─────────────────────────────────────────────────────

resource "scaleway_lb" "main" {
  name = "myapp-lb"
  type = "LB-S"
}

resource "scaleway_lb_backend" "web" {
  lb_id            = scaleway_lb.main.id
  name             = "web-backend"
  forward_protocol = "http"
  forward_port     = 80
  
  health_check_http {
    uri = "/health"
  }
  
  server_ips = scaleway_instance_server.web_ha[*].private_ip
}

resource "scaleway_lb_frontend" "web" {
  lb_id        = scaleway_lb.main.id
  backend_id   = scaleway_lb_backend.web.id
  name         = "web-frontend"
  inbound_port = 443
  
  certificate_ids = [scaleway_lb_certificate.main.id]
}

resource "scaleway_lb_certificate" "main" {
  lb_id = scaleway_lb.main.id
  name  = "main-cert"
  
  letsencrypt {
    common_name = "myapp.com"
  }
}

# ─────────────────────────────────────────────────────
# DNS
# ─────────────────────────────────────────────────────

resource "scaleway_domain_record" "web" {
  dns_zone = "myapp.com"
  name     = ""
  type     = "A"
  data     = scaleway_lb.main.ip_address
  ttl      = 300
}

resource "scaleway_domain_record" "api" {
  dns_zone = "myapp.com"
  name     = "api"
  type     = "A"
  data     = scaleway_lb.main.ip_address
  ttl      = 300
}

# ─────────────────────────────────────────────────────
# KUBERNETES (Alternative)
# ─────────────────────────────────────────────────────

resource "scaleway_k8s_cluster" "main" {
  name    = "myapp-cluster"
  version = "1.28"
  cni     = "cilium"
  
  auto_upgrade {
    enable                        = true
    maintenance_window_start_hour = 3
    maintenance_window_day        = "sunday"
  }
}

resource "scaleway_k8s_pool" "main" {
  cluster_id = scaleway_k8s_cluster.main.id
  name       = "main-pool"
  node_type  = "DEV1-M"
  size       = 3
  
  autoscaling    = true
  min_size       = 2
  max_size       = 10
  autohealing    = true
  
  container_runtime = "containerd"
}
```

### 5.3 OVHcloud Terraform Generation

```hcl
# Output généré pour target=ovh

terraform {
  required_providers {
    ovh = {
      source  = "ovh/ovh"
      version = "~> 0.40"
    }
    openstack = {
      source  = "terraform-provider-openstack/openstack"
      version = "~> 1.53"
    }
  }
}

provider "ovh" {
  endpoint = "ovh-eu"
}

provider "openstack" {
  auth_url    = "https://auth.cloud.ovh.net/v3"
  domain_name = "Default"
  tenant_name = var.ovh_tenant
  user_name   = var.ovh_user
  password    = var.ovh_password
  region      = "GRA11"
}

# ─────────────────────────────────────────────────────
# COMPUTE (Public Cloud)
# ─────────────────────────────────────────────────────

resource "openstack_compute_instance_v2" "web" {
  count           = 3
  name            = "web-server-${count.index}"
  image_name      = "Ubuntu 22.04"
  flavor_name     = "b2-15"  # 4 vCPU, 15GB RAM
  key_pair        = openstack_compute_keypair_v2.main.name
  security_groups = [openstack_networking_secgroup_v2.web.name]
  
  # Multi-AZ via server groups
  scheduler_hints {
    group = openstack_compute_servergroup_v2.ha.id
  }
  
  network {
    name = openstack_networking_network_v2.private.name
  }
  
  metadata = {
    env = "production"
    app = "myapp"
  }
}

resource "openstack_compute_servergroup_v2" "ha" {
  name     = "ha-group"
  policies = ["anti-affinity"]  # Spread across hosts
}

# ─────────────────────────────────────────────────────
# DATABASE (Managed)
# ─────────────────────────────────────────────────────

resource "ovh_cloud_project_database" "main" {
  service_name = var.ovh_project_id
  engine       = "postgresql"
  version      = "15"
  plan         = "business"  # HA inclus
  flavor       = "db1-7"
  
  nodes {
    region = "GRA"
  }
  nodes {
    region = "GRA"  # Second node for HA
  }
}

resource "ovh_cloud_project_database_database" "myapp" {
  service_name  = var.ovh_project_id
  engine        = "postgresql"
  cluster_id    = ovh_cloud_project_database.main.id
  name          = "myapp"
}

resource "ovh_cloud_project_database_user" "admin" {
  service_name = var.ovh_project_id
  engine       = "postgresql"
  cluster_id   = ovh_cloud_project_database.main.id
  name         = "admin"
}

# ─────────────────────────────────────────────────────
# OBJECT STORAGE (S3 Compatible)
# ─────────────────────────────────────────────────────

resource "openstack_objectstorage_container_v1" "assets" {
  name = "myapp-assets"
  
  metadata = {
    "X-Container-Meta-Access-Control-Allow-Origin" = "*"
  }
  
  versioning_legacy {
    type     = "versions"
    location = "myapp-assets-versions"
  }
}

# ─────────────────────────────────────────────────────
# LOAD BALANCER
# ─────────────────────────────────────────────────────

resource "openstack_lb_loadbalancer_v2" "main" {
  name          = "myapp-lb"
  vip_subnet_id = openstack_networking_subnet_v2.public.id
}

resource "openstack_lb_listener_v2" "https" {
  name            = "https-listener"
  protocol        = "HTTPS"
  protocol_port   = 443
  loadbalancer_id = openstack_lb_loadbalancer_v2.main.id
  default_tls_container_ref = var.certificate_ref
}

resource "openstack_lb_pool_v2" "web" {
  name        = "web-pool"
  protocol    = "HTTP"
  lb_method   = "ROUND_ROBIN"
  listener_id = openstack_lb_listener_v2.https.id
}

resource "openstack_lb_member_v2" "web" {
  count         = 3
  pool_id       = openstack_lb_pool_v2.web.id
  address       = openstack_compute_instance_v2.web[count.index].access_ip_v4
  protocol_port = 80
}

resource "openstack_lb_monitor_v2" "web" {
  pool_id     = openstack_lb_pool_v2.web.id
  type        = "HTTP"
  url_path    = "/health"
  delay       = 5
  timeout     = 3
  max_retries = 3
}

# ─────────────────────────────────────────────────────
# KUBERNETES (Managed)
# ─────────────────────────────────────────────────────

resource "ovh_cloud_project_kube" "main" {
  service_name = var.ovh_project_id
  name         = "myapp-cluster"
  region       = "GRA7"
  version      = "1.28"
}

resource "ovh_cloud_project_kube_nodepool" "main" {
  service_name  = var.ovh_project_id
  kube_id       = ovh_cloud_project_kube.main.id
  name          = "main-pool"
  flavor_name   = "b2-7"
  desired_nodes = 3
  min_nodes     = 2
  max_nodes     = 10
  autoscale     = true
}

# ─────────────────────────────────────────────────────
# DNS
# ─────────────────────────────────────────────────────

resource "ovh_domain_zone_record" "web" {
  zone      = "myapp.com"
  subdomain = ""
  fieldtype = "A"
  ttl       = 300
  target    = openstack_lb_loadbalancer_v2.main.vip_address
}
```

### 5.4 Hetzner Terraform Generation

```hcl
# Output généré pour target=hetzner

terraform {
  required_providers {
    hcloud = {
      source  = "hetznercloud/hcloud"
      version = "~> 1.44"
    }
  }
}

provider "hcloud" {
  token = var.hetzner_token
}

# ─────────────────────────────────────────────────────
# NETWORK
# ─────────────────────────────────────────────────────

resource "hcloud_network" "main" {
  name     = "myapp-network"
  ip_range = "10.0.0.0/16"
}

resource "hcloud_network_subnet" "main" {
  network_id   = hcloud_network.main.id
  type         = "cloud"
  network_zone = "eu-central"
  ip_range     = "10.0.1.0/24"
}

# ─────────────────────────────────────────────────────
# COMPUTE
# ─────────────────────────────────────────────────────

resource "hcloud_placement_group" "ha" {
  name = "ha-group"
  type = "spread"  # Anti-affinity
}

resource "hcloud_server" "web" {
  count              = 3
  name               = "web-server-${count.index}"
  server_type        = "cx21"  # 2 vCPU, 4GB RAM
  image              = "ubuntu-22.04"
  location           = "fsn1"  # Falkenstein
  placement_group_id = hcloud_placement_group.ha.id
  
  network {
    network_id = hcloud_network.main.id
    ip         = "10.0.1.${count.index + 10}"
  }
  
  ssh_keys = [hcloud_ssh_key.main.id]
  
  labels = {
    env = "production"
    app = "myapp"
  }
  
  user_data = file("${path.module}/cloud-init.yml")
}

# Multi-location for geo-redundancy
resource "hcloud_server" "web_backup" {
  count              = 2
  name               = "web-backup-${count.index}"
  server_type        = "cx21"
  image              = "ubuntu-22.04"
  location           = "nbg1"  # Nuremberg (different DC)
  
  network {
    network_id = hcloud_network.main.id
    ip         = "10.0.1.${count.index + 20}"
  }
  
  ssh_keys = [hcloud_ssh_key.main.id]
}

# ─────────────────────────────────────────────────────
# VOLUMES (for databases)
# ─────────────────────────────────────────────────────

resource "hcloud_volume" "db_data" {
  name      = "db-data"
  size      = 100
  server_id = hcloud_server.web[0].id
  automount = true
  format    = "ext4"
}

# ─────────────────────────────────────────────────────
# LOAD BALANCER
# ─────────────────────────────────────────────────────

resource "hcloud_load_balancer" "main" {
  name               = "myapp-lb"
  load_balancer_type = "lb11"
  location           = "fsn1"
  
  algorithm {
    type = "round_robin"
  }
}

resource "hcloud_load_balancer_network" "main" {
  load_balancer_id = hcloud_load_balancer.main.id
  network_id       = hcloud_network.main.id
  ip               = "10.0.1.1"
}

resource "hcloud_load_balancer_target" "web" {
  count            = 3
  type             = "server"
  load_balancer_id = hcloud_load_balancer.main.id
  server_id        = hcloud_server.web[count.index].id
  use_private_ip   = true
}

resource "hcloud_load_balancer_service" "https" {
  load_balancer_id = hcloud_load_balancer.main.id
  protocol         = "https"
  listen_port      = 443
  destination_port = 80
  
  http {
    certificates = [hcloud_managed_certificate.main.id]
  }
  
  health_check {
    protocol = "http"
    port     = 80
    interval = 10
    timeout  = 5
    retries  = 3
    
    http {
      path         = "/health"
      status_codes = ["200"]
    }
  }
}

resource "hcloud_managed_certificate" "main" {
  name         = "myapp-cert"
  domain_names = ["myapp.com", "*.myapp.com"]
}

# ─────────────────────────────────────────────────────
# FIREWALL
# ─────────────────────────────────────────────────────

resource "hcloud_firewall" "web" {
  name = "web-firewall"
  
  rule {
    direction = "in"
    protocol  = "tcp"
    port      = "22"
    source_ips = [var.admin_ip]
  }
  
  rule {
    direction = "in"
    protocol  = "tcp"
    port      = "80"
    source_ips = ["0.0.0.0/0", "::/0"]
  }
  
  rule {
    direction = "in"
    protocol  = "tcp"
    port      = "443"
    source_ips = ["0.0.0.0/0", "::/0"]
  }
  
  apply_to {
    server = hcloud_server.web[*].id
  }
}

# ─────────────────────────────────────────────────────
# DNS (via Hetzner DNS)
# ─────────────────────────────────────────────────────

resource "hetznerdns_zone" "main" {
  name = "myapp.com"
  ttl  = 300
}

resource "hetznerdns_record" "web" {
  zone_id = hetznerdns_zone.main.id
  name    = "@"
  type    = "A"
  value   = hcloud_load_balancer.main.ipv4
  ttl     = 300
}

# ─────────────────────────────────────────────────────
# FLOATING IPS (for failover)
# ─────────────────────────────────────────────────────

resource "hcloud_floating_ip" "main" {
  type          = "ipv4"
  home_location = "fsn1"
}

resource "hcloud_floating_ip_assignment" "main" {
  floating_ip_id = hcloud_floating_ip.main.id
  server_id      = hcloud_server.web[0].id
}

# ─────────────────────────────────────────────────────
# OUTPUT: Docker Compose for self-hosted services
# ─────────────────────────────────────────────────────
# Note: Hetzner doesn't have managed DB/Redis/etc.
# We deploy them via Docker on the servers

output "docker_compose" {
  value = templatefile("${path.module}/templates/docker-compose.yml.tpl", {
    db_volume_path = "/mnt/${hcloud_volume.db_data.id}"
    servers        = hcloud_server.web[*].ipv4_address
  })
}
```

---

## 6. Architecture Finale Multi-Target

### 6.1 Domain Model Étendu

```go
// internal/domain/target/target.go

package target

// Target represents a deployment target
type Target string

const (
    // Self-hosted targets
    TargetDockerCompose Target = "docker-compose"
    TargetDockerSwarm   Target = "docker-swarm"
    TargetK3s           Target = "k3s"
    TargetKubernetes    Target = "kubernetes"
    
    // EU Cloud targets
    TargetScaleway      Target = "scaleway"
    TargetOVH           Target = "ovh"
    TargetHetzner       Target = "hetzner"
    TargetExoscale      Target = "exoscale"
    TargetInfmaniak     Target = "infomaniak"
    
    // Hybrid
    TargetHybrid        Target = "hybrid"
)

// HALevel represents the high availability level
type HALevel string

const (
    HANone        HALevel = "none"         // Single server
    HABasic       HALevel = "basic"        // Backups + monitoring
    HAMultiServer HALevel = "multi-server" // Active-passive
    HACluster     HALevel = "cluster"      // Active-active
    HAGeo         HALevel = "geo"          // Multi-datacenter
)

// TargetConfig holds target-specific configuration
type TargetConfig struct {
    Target      Target
    HALevel     HALevel
    Region      string
    Zones       []string  // For multi-AZ
    Servers     int       // Number of servers
    
    // Provider-specific
    Scaleway    *ScalewayConfig
    OVH         *OVHConfig
    Hetzner     *HetznerConfig
}

type ScalewayConfig struct {
    ProjectID   string
    Zone        string
    Region      string
    AccessKey   string
    SecretKey   string
}

type OVHConfig struct {
    Endpoint    string
    TenantName  string
    Username    string
    Password    string
    Region      string
}

type HetznerConfig struct {
    Token       string
    Location    string
    NetworkZone string
}
```

### 6.2 Generator Interface Étendue

```go
// internal/domain/generator/generator.go

package generator

import (
    "context"
    "github.com/agnostech/agnostech/internal/domain/resource"
    "github.com/agnostech/agnostech/internal/domain/target"
)

// Generator generates deployment artifacts for a target
type Generator interface {
    // Target returns the target this generator handles
    Target() target.Target
    
    // Generate creates all artifacts for the infrastructure
    Generate(ctx context.Context, infra *resource.Infrastructure, config *target.TargetConfig) (*GenerationResult, error)
    
    // Validate checks if the infrastructure can be deployed to this target
    Validate(infra *resource.Infrastructure, config *target.TargetConfig) error
    
    // SupportedHALevels returns supported HA levels
    SupportedHALevels() []target.HALevel
    
    // RequiresCredentials returns true if target needs cloud credentials
    RequiresCredentials() bool
}

// GenerationResult contains all generated artifacts
type GenerationResult struct {
    // Primary deployment files
    Files map[string][]byte
    
    // Terraform files (for cloud targets)
    TerraformFiles map[string][]byte
    
    // Docker Compose files (for self-hosted)
    ComposeFiles map[string][]byte
    
    // Kubernetes manifests (for k8s targets)
    K8sManifests map[string][]byte
    
    // Ansible playbooks (for multi-server)
    AnsibleFiles map[string][]byte
    
    // Scripts
    Scripts map[string][]byte
    
    // Documentation
    Docs map[string][]byte
    
    // Warnings and manual steps
    Warnings    []string
    ManualSteps []string
    
    // Estimated costs (if applicable)
    EstimatedMonthlyCost *CostEstimate
}

type CostEstimate struct {
    Currency string
    Compute  float64
    Storage  float64
    Database float64
    Network  float64
    Other    float64
    Total    float64
    Details  map[string]float64
}
```

### 6.3 CLI Flow

```go
// internal/cli/migrate.go

func migrateCmd() *cobra.Command {
    var (
        targetFlag  string
        haFlag      string
        outputFlag  string
        interactive bool
        dryRun      bool
    )
    
    cmd := &cobra.Command{
        Use:   "migrate [path]",
        Short: "Migrate infrastructure to target platform",
        RunE: func(cmd *cobra.Command, args []string) error {
            ctx := cmd.Context()
            
            // Parse source
            infra, err := parseInfrastructure(args[0])
            if err != nil {
                return err
            }
            
            // Interactive mode
            if interactive {
                return runInteractiveMigration(ctx, infra)
            }
            
            // Build config
            config := &target.TargetConfig{
                Target:  target.Target(targetFlag),
                HALevel: target.HALevel(haFlag),
            }
            
            // Get appropriate generator
            gen, err := generator.Registry.Get(config.Target)
            if err != nil {
                return err
            }
            
            // Validate
            if err := gen.Validate(infra, config); err != nil {
                return err
            }
            
            // Generate
            result, err := gen.Generate(ctx, infra, config)
            if err != nil {
                return err
            }
            
            // Dry run - just show what would be generated
            if dryRun {
                return showDryRun(result)
            }
            
            // Write output
            return writeOutput(outputFlag, result)
        },
    }
    
    cmd.Flags().StringVarP(&targetFlag, "target", "t", "docker-compose", 
        "Target platform (docker-compose, docker-swarm, k3s, scaleway, ovh, hetzner)")
    cmd.Flags().StringVar(&haFlag, "ha", "none",
        "HA level (none, basic, multi-server, cluster, geo)")
    cmd.Flags().StringVarP(&outputFlag, "output", "o", "./output",
        "Output directory")
    cmd.Flags().BoolVarP(&interactive, "interactive", "i", false,
        "Interactive mode with wizard")
    cmd.Flags().BoolVar(&dryRun, "dry-run", false,
        "Show what would be generated without writing files")
    
    return cmd
}

func runInteractiveMigration(ctx context.Context, infra *resource.Infrastructure) error {
    // Use Bubble Tea for interactive TUI
    
    // Step 1: Show detected resources
    fmt.Printf("Detected %d resources from %s\n", len(infra.Resources), infra.Source)
    
    // Step 2: Choose target
    targetChoice := promptSelect("Choose target platform:", []string{
        "Self-Hosted (Docker Compose) - Single server, simplest",
        "Self-Hosted (Docker Swarm) - Multi-server, HA",
        "Self-Hosted (K3s) - Kubernetes, most flexible",
        "Scaleway (FR) - EU cloud, managed services",
        "OVHcloud (FR) - EU cloud, good pricing",
        "Hetzner (DE) - EU cloud, best value",
    })
    
    // Step 3: Choose HA level
    haChoice := promptSelect("Choose availability level:", []string{
        "None - Single server, for dev/testing",
        "Basic - Automated backups, monitoring",
        "Multi-Server - Active-passive failover",
        "Cluster - Active-active, full HA",
        "Geo - Multi-datacenter (advanced)",
    })
    
    // Step 4: Review mappings
    showMappingPreview(infra, targetChoice, haChoice)
    
    // Step 5: Confirm and generate
    if promptConfirm("Generate deployment artifacts?") {
        // Generate...
    }
    
    return nil
}
```

---

## 7. Coût Comparatif

### 7.1 Estimation Automatique

```go
// internal/infrastructure/cost/estimator.go

package cost

type Estimator interface {
    Estimate(infra *resource.Infrastructure, target target.Target, ha target.HALevel) (*Estimate, error)
}

type Estimate struct {
    Monthly map[string]ComponentCost
    Total   float64
    
    Comparison map[string]float64 // vs AWS/GCP/Azure
}

type ComponentCost struct {
    Service     string
    Quantity    int
    UnitPrice   float64
    Total       float64
    Notes       string
}
```

### 7.2 Grille Tarifaire Exemple (mensuel)

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                     Cost Comparison (Typical Web App)                        │
│                     2 servers, DB, Storage, LB                               │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  Component          │ AWS      │ GCP      │ Azure    │ Self*    │ Scaleway │
│  ───────────────────┼──────────┼──────────┼──────────┼──────────┼──────────│
│  Compute (2x)       │ $140     │ $120     │ $130     │ $20**    │ $30      │
│  Database (HA)      │ $180     │ $150     │ $160     │ $0       │ $45      │
│  Storage (100GB)    │ $25      │ $20      │ $22      │ $5       │ $5       │
│  Load Balancer      │ $25      │ $20      │ $25      │ $0       │ $10      │
│  Bandwidth (500GB)  │ $45      │ $60      │ $45      │ $0       │ $0       │
│  ───────────────────┼──────────┼──────────┼──────────┼──────────┼──────────│
│  TOTAL              │ $415     │ $370     │ $382     │ $25      │ $90      │
│  ───────────────────┼──────────┼──────────┼──────────┼──────────┼──────────│
│  vs AWS             │ -        │ -11%     │ -8%      │ -94%     │ -78%     │
│                                                                             │
│  * Self-hosted on Hetzner CX21 (€4.5/mo each)                              │
│  ** Server cost only, DB/LB included via Docker                            │
│                                                                             │
│  ─────────────────────────────────────────────────────────────────────────  │
│                                                                             │
│  HIDDEN COSTS (Self-Hosted):                                               │
│  • Ops time: ~2-4h/month maintenance                                       │
│  • Initial setup: ~8-16h                                                   │
│  • Learning curve: variable                                                │
│                                                                             │
│  HIDDEN BENEFITS (Self-Hosted):                                            │
│  • No vendor lock-in                                                       │
│  • Full data sovereignty                                                   │
│  • No egress fees                                                          │
│  • Predictable costs                                                       │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 8. Roadmap Mise à Jour

### Phase 1: AWS → Self-Hosted (Semaines 1-8)
*Déjà planifié dans v1*

### Phase 2: Multi-Target (Semaines 9-12)
- [ ] Generator interface abstraction
- [ ] Scaleway generator
- [ ] OVH generator  
- [ ] Hetzner generator
- [ ] Cost estimator

### Phase 3: GCP Support (Semaines 13-16)
- [ ] GCP Terraform parser
- [ ] GCP API scanner
- [ ] GCP → Self-hosted mappers (40+ services)
- [ ] GCP → EU cloud mappers

### Phase 4: Azure Support (Semaines 17-20)
- [ ] Azure Terraform parser
- [ ] Azure API scanner
- [ ] Azure → Self-hosted mappers (40+ services)
- [ ] Azure → EU cloud mappers

### Phase 5: High Availability (Semaines 21-24)
- [ ] Docker Swarm generator
- [ ] K3s/Kubernetes generator
- [ ] Ansible playbooks
- [ ] HA documentation
- [ ] Multi-server testing

### Phase 6: Polish & Enterprise (Semaines 25-28)
- [ ] Interactive TUI wizard
- [ ] Cost comparison tool
- [ ] Migration assistant
- [ ] Enterprise documentation
- [ ] Security hardening guide

---

## 9. Résumé Décisions

| Décision | Choix | Raison |
|----------|-------|--------|
| **Langage** | Go | Distribution binaire, écosystème cloud |
| **Parser** | hashicorp/hcl + AWS/GCP/Azure SDKs | Officiel, complet |
| **Self-Hosted Base** | Docker Compose | Simplicité, adoption |
| **Self-Hosted HA** | Docker Swarm | Plus simple que K8s |
| **Self-Hosted Advanced** | K3s | K8s léger, flexible |
| **EU Cloud Priority** | Scaleway > Hetzner > OVH | Completude des services |
| **HA Default** | basic | Bon compromis |
| **License** | AGPLv3 | Protection, standard |

---

*Document v2.0 | Décembre 2025*
