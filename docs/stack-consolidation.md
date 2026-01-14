# Stack Consolidation

Stack consolidation is a feature that reduces container sprawl by grouping similar cloud resources into unified, self-hosted stacks. Instead of creating a separate Docker container for each cloud resource, consolidation merges related resources into fewer, more manageable services.

## Why Stack Consolidation?

When migrating cloud infrastructure directly, you might end up with:
- 5 separate PostgreSQL containers (one per RDS instance)
- 3 separate RabbitMQ containers (one per SQS queue)
- 4 separate Redis containers (one per ElastiCache cluster)

This creates operational overhead with:
- More containers to monitor and maintain
- Higher memory footprint
- Complex networking between many services
- Difficult backup and disaster recovery

**With stack consolidation**, the same infrastructure becomes:
- 1 PostgreSQL container (serving all 5 databases)
- 1 RabbitMQ container (with 3 virtual hosts/queues)
- 1 Redis container (with multiple databases)

## Stack Types

Homeport consolidates resources into 8 logical stack types:

### 1. Database Stack

Consolidates all SQL and NoSQL database resources into a single database service.

| Cloud Resources | Self-Hosted Service |
|-----------------|---------------------|
| AWS RDS (PostgreSQL, MySQL) | PostgreSQL or MySQL |
| AWS Aurora | PostgreSQL or MySQL |
| Azure SQL, Azure PostgreSQL, Azure MySQL | PostgreSQL or MySQL |
| GCP Cloud SQL | PostgreSQL or MySQL |
| AWS DynamoDB | ScyllaDB |
| Azure Cosmos DB | MongoDB |
| GCP Firestore, Spanner, Bigtable | ScyllaDB |

**Example consolidation:**
```
3 RDS PostgreSQL instances -> 1 PostgreSQL container (3 databases)
2 RDS MySQL instances      -> 1 MySQL container (2 databases)
```

### 2. Cache Stack

Consolidates all caching resources into a single Redis instance.

| Cloud Resources | Self-Hosted Service |
|-----------------|---------------------|
| AWS ElastiCache (Redis, Memcached) | Redis |
| Azure Cache for Redis | Redis |
| GCP Memorystore | Redis |

**Example consolidation:**
```
4 ElastiCache clusters -> 1 Redis container (multiple databases 0-15)
```

### 3. Messaging Stack

Consolidates queues, pub/sub, and event streaming into a unified message broker.

| Cloud Resources | Self-Hosted Service |
|-----------------|---------------------|
| AWS SQS | RabbitMQ (queues) |
| AWS SNS | RabbitMQ (exchanges) |
| AWS Kinesis | RabbitMQ Streams |
| Azure Service Bus | RabbitMQ |
| Azure Event Hub | RabbitMQ |
| GCP Pub/Sub | RabbitMQ or NATS |

**Example consolidation:**
```
5 SQS queues + 3 SNS topics -> 1 RabbitMQ container (5 queues, 3 exchanges)
```

### 4. Storage Stack

Consolidates object and file storage into MinIO.

| Cloud Resources | Self-Hosted Service |
|-----------------|---------------------|
| AWS S3 | MinIO |
| Azure Blob Storage | MinIO |
| GCP Cloud Storage | MinIO |
| AWS EFS | Local volumes or NFS |
| Azure Files | Local volumes or NFS |
| GCP Filestore | Local volumes or NFS |

**Example consolidation:**
```
8 S3 buckets -> 1 MinIO container (8 buckets)
```

### 5. Authentication Stack

Consolidates identity and authentication services into Keycloak.

| Cloud Resources | Self-Hosted Service |
|-----------------|---------------------|
| AWS Cognito | Keycloak |
| Azure AD B2C | Keycloak |
| GCP Identity Platform | Keycloak |

**Example consolidation:**
```
2 Cognito user pools -> 1 Keycloak container (2 realms)
```

### 6. Secrets Stack

Consolidates secret management into HashiCorp Vault.

| Cloud Resources | Self-Hosted Service |
|-----------------|---------------------|
| AWS Secrets Manager | Vault |
| Azure Key Vault | Vault |
| GCP Secret Manager | Vault |

**Example consolidation:**
```
15 Secrets Manager secrets -> 1 Vault container (15 secrets)
```

### 7. Observability Stack

Consolidates monitoring, logging, and tracing into a unified observability stack.

| Cloud Resources | Self-Hosted Service |
|-----------------|---------------------|
| AWS CloudWatch | Prometheus + Grafana |
| Azure Monitor | Prometheus + Grafana |
| GCP Cloud Monitoring | Prometheus + Grafana |
| AWS X-Ray | Jaeger |
| Azure Application Insights | Jaeger |
| GCP Cloud Trace | Jaeger |

**Example consolidation:**
```
CloudWatch + X-Ray -> Prometheus + Grafana + Loki + Jaeger
```

### 8. Compute Stack

Consolidates serverless functions into OpenFaaS.

| Cloud Resources | Self-Hosted Service |
|-----------------|---------------------|
| AWS Lambda | OpenFaaS functions |
| Azure Functions | OpenFaaS functions |
| GCP Cloud Functions | OpenFaaS functions |
| GCP Cloud Run | OpenFaaS functions |

**Example consolidation:**
```
12 Lambda functions -> 1 OpenFaaS gateway (12 functions)
```

### Passthrough Resources

Some resources are not consolidated and remain as individual services:

- EC2 instances
- Azure VMs
- GCP Compute Engine instances
- ECS/EKS/AKS/GKE clusters
- Custom application containers

These resources require specific configurations and cannot be meaningfully merged.

## Using Stack Consolidation

### CLI Usage

Use the `--consolidate` flag with the migrate command:

```bash
# Standard migration (one container per resource)
homeport migrate ./infrastructure

# Consolidated migration (grouped stacks)
homeport migrate ./infrastructure --consolidate

# With additional options
homeport migrate ./infrastructure \
  --consolidate \
  --output ./my-stack \
  --domain example.com \
  --include-monitoring
```

### CLI Output

When using `--consolidate`, you'll see a consolidation summary:

```
────────────────────────────────────────────────────────
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

### Web UI Usage

1. Navigate to the **Migrate** page
2. Upload or discover your infrastructure
3. Select resources for migration
4. Toggle the **Consolidate** button in the top bar
5. View the **Consolidation Preview** panel showing:
   - Total resources vs. resulting services
   - Reduction ratio
   - Per-stack breakdown
6. Click **Generate Stack**

## Before and After Examples

### Example 1: E-commerce Application

**Before (24 resources):**
- 3 RDS PostgreSQL instances
- 2 ElastiCache Redis clusters
- 5 SQS queues
- 2 SNS topics
- 4 S3 buckets
- 1 Cognito user pool
- 3 Lambda functions
- 4 EC2 instances

**Without consolidation: 24 containers**

**With consolidation: 9 containers**
- 1 PostgreSQL (3 databases)
- 1 Redis (2 logical databases)
- 1 RabbitMQ (5 queues, 2 exchanges)
- 1 MinIO (4 buckets)
- 1 Keycloak (1 realm)
- 1 OpenFaaS (3 functions)
- 3 application containers

**Reduction: 2.7x fewer containers**

### Example 2: Microservices Platform

**Before (45 resources):**
- 8 RDS instances
- 5 ElastiCache clusters
- 12 SQS queues
- 6 SNS topics
- 8 S3 buckets
- 2 Cognito pools
- 4 Secrets Manager secrets

**Without consolidation: 45 containers**

**With consolidation: 7 containers**
- 1 PostgreSQL
- 1 Redis
- 1 RabbitMQ
- 1 MinIO
- 1 Keycloak
- 1 Vault
- 1 Observability stack

**Reduction: 6.4x fewer containers**

## Manual Steps After Consolidation

After running a consolidated migration, some manual configuration may be required:

### Database Stack
1. Create individual databases within the PostgreSQL/MySQL container
2. Set up database users with appropriate permissions
3. Import data from each source database

### Messaging Stack
1. Create virtual hosts in RabbitMQ for isolation
2. Configure queue bindings to match original routing
3. Set up dead letter exchanges

### Authentication Stack
1. Create realms in Keycloak for each user pool
2. Configure identity providers
3. Import users and set up federation

### Storage Stack
1. Create buckets in MinIO
2. Configure bucket policies
3. Set up lifecycle rules

See `MIGRATION_NOTES.md` in the generated output for specific manual steps for your migration.

## Configuration Options

Stack consolidation behavior can be customized:

```yaml
# .homeport.yaml
consolidation:
  # Choose database engine (postgres, mysql, mariadb)
  database_engine: postgres

  # Choose messaging broker (rabbitmq, nats, kafka)
  messaging_broker: rabbitmq

  # Include support services (pgBouncer, Grafana, etc.)
  include_support_services: true

  # Prefix for generated stack names
  name_prefix: "prod"

  # Disable specific stack types
  disabled_stacks:
    - observability
```

## Best Practices

1. **Review the preview**: Always check the consolidation preview before generating
2. **Test in staging**: Run consolidated stacks in a staging environment first
3. **Plan data migration**: Consolidation changes data organization
4. **Update connection strings**: Applications need updated connection details
5. **Configure resource limits**: Set appropriate CPU/memory for consolidated services
6. **Set up backups**: Consolidated services need comprehensive backup strategies

## Limitations

- Consolidation may not preserve all cloud-specific features
- Some advanced configurations require manual setup
- Cross-region resources are consolidated per-region
- Highly customized resources may not consolidate well
- Performance characteristics may differ from original services

## Related Documentation

- [CLI Reference](./cli-reference.md) - Full command documentation
- [Migration Guides](./migration-guides.md) - Step-by-step migration guides
- [Troubleshooting](./troubleshooting.md) - Common issues and solutions
