# Migration Guides

This document provides comprehensive guides for migrating from cloud services to self-hosted alternatives using Homeport.

## Table of Contents

- [Pre-Migration Checklist](#pre-migration-checklist)
- [Database Migration](#database-migration)
  - [AWS RDS to PostgreSQL](#aws-rds-to-postgresql)
  - [AWS RDS to MySQL](#aws-rds-to-mysql)
  - [DynamoDB to ScyllaDB](#dynamodb-to-scylladb)
  - [ElastiCache to Redis](#elasticache-to-redis)
- [Storage Migration](#storage-migration)
  - [AWS S3 to MinIO](#aws-s3-to-minio)
  - [EFS to NFS](#efs-to-nfs)
- [Authentication Migration](#authentication-migration)
  - [AWS Cognito to Keycloak](#aws-cognito-to-keycloak)
- [Message Queue Migration](#message-queue-migration)
  - [AWS SQS to RabbitMQ](#aws-sqs-to-rabbitmq)
- [Serverless Migration](#serverless-migration)
  - [AWS Lambda to OpenFaaS](#aws-lambda-to-openfaas)
- [Zero-Downtime Migration Strategy](#zero-downtime-migration-strategy)
- [Rollback Procedures](#rollback-procedures)

---

## Pre-Migration Checklist

Before starting any migration, complete this checklist:

### 1. Inventory Assessment

```bash
# Generate a complete infrastructure analysis
homeport analyze ./terraform --format table --output inventory.json
```

Review the output to understand:
- Total resources to migrate
- Dependencies between resources
- Any unsupported resources that need manual handling

### 2. Data Volume Assessment

Estimate data volumes for planning:

| Resource Type | Check Command | Considerations |
|--------------|---------------|----------------|
| RDS Database | `SELECT pg_size_pretty(pg_database_size('dbname'))` | Larger DBs need more migration time |
| S3 Buckets | `aws s3 ls s3://bucket --recursive --summarize` | Consider network bandwidth |
| DynamoDB | Check table metrics in console | Read/write capacity planning |

### 3. Application Compatibility

- [ ] Application supports PostgreSQL/MySQL connection strings
- [ ] S3 SDK calls are S3-compatible (MinIO works with standard S3 SDKs)
- [ ] Authentication flows support OIDC/SAML (for Keycloak migration)
- [ ] Queue consumers support AMQP (for RabbitMQ migration)

### 4. Network Planning

- [ ] Domain name(s) configured
- [ ] DNS TTL lowered (for quick cutover)
- [ ] Firewall rules documented
- [ ] SSL certificates planned (Let's Encrypt or custom)

### 5. Backup Strategy

- [ ] Full backup of all databases
- [ ] S3 bucket versioning enabled
- [ ] Backup of Terraform state
- [ ] Documented rollback procedure

---

## Database Migration

### AWS RDS to PostgreSQL

#### Overview

| Source | Target | Complexity | Downtime |
|--------|--------|------------|----------|
| AWS RDS PostgreSQL | PostgreSQL (Docker) | Low | Minimal |
| AWS RDS MySQL | MySQL (Docker) | Low | Minimal |

#### Step 1: Generate Migration Stack

```bash
homeport migrate ./terraform \
  --output ./stack \
  --domain example.com \
  --include-migration
```

This generates migration scripts in `./stack/scripts/migrate-rds.sh`.

#### Step 2: Prepare Source Database

Enable logical replication on RDS (if not already enabled):

```sql
-- Check current setting
SHOW wal_level;

-- Should be 'logical' for streaming replication
-- If not, modify the RDS parameter group and reboot
```

#### Step 3: Initial Data Dump

```bash
# Create dump from RDS
pg_dump -h your-rds-endpoint.rds.amazonaws.com \
  -U admin \
  -d myapp \
  -F c \
  -f myapp_backup.dump

# Or use the generated script
./scripts/migrate-rds.sh dump
```

#### Step 4: Start Target Database

```bash
cd stack
docker compose up -d postgres

# Wait for PostgreSQL to be ready
docker compose exec postgres pg_isready -U webapp
```

#### Step 5: Restore Data

```bash
# Restore to self-hosted PostgreSQL
docker compose exec -T postgres pg_restore \
  -U webapp \
  -d webapp \
  --no-owner \
  --no-privileges \
  < myapp_backup.dump

# Or use the generated script
./scripts/migrate-rds.sh restore
```

#### Step 6: Verify Migration

```bash
# Compare row counts
echo "Source rows:"
psql -h your-rds-endpoint.rds.amazonaws.com -U admin -d myapp \
  -c "SELECT COUNT(*) FROM users;"

echo "Target rows:"
docker compose exec postgres psql -U webapp -d webapp \
  -c "SELECT COUNT(*) FROM users;"

# Verify application connectivity
docker compose exec postgres psql -U webapp -d webapp \
  -c "SELECT version();"
```

#### Step 7: Update Application

Update your application's environment variables:

```bash
# Old (AWS RDS)
DATABASE_URL=postgres://admin:password@your-rds.rds.amazonaws.com:5432/myapp

# New (Self-hosted)
DATABASE_URL=postgres://webapp:${POSTGRES_PASSWORD}@postgres:5432/webapp
```

#### Continuous Replication (Optional)

For zero-downtime migration, set up logical replication:

```bash
# On RDS (source) - create publication
psql -h your-rds-endpoint -U admin -d myapp \
  -c "CREATE PUBLICATION my_publication FOR ALL TABLES;"

# On self-hosted (target) - create subscription
docker compose exec postgres psql -U webapp -d webapp -c "
CREATE SUBSCRIPTION my_subscription
  CONNECTION 'host=your-rds-endpoint dbname=myapp user=admin password=xxx'
  PUBLICATION my_publication;"
```

---

### AWS RDS to MySQL

#### Step 1: Dump Source Database

```bash
mysqldump -h your-rds-endpoint.rds.amazonaws.com \
  -u admin -p \
  --single-transaction \
  --routines \
  --triggers \
  myapp > myapp_backup.sql
```

#### Step 2: Start Target MySQL

```bash
docker compose up -d mysql
docker compose exec mysql mysqladmin ping -h localhost
```

#### Step 3: Restore Data

```bash
docker compose exec -T mysql mysql -u webapp -p webapp < myapp_backup.sql
```

#### Step 4: Verify

```bash
docker compose exec mysql mysql -u webapp -p -e "SHOW DATABASES; USE webapp; SHOW TABLES;"
```

---

### DynamoDB to ScyllaDB

#### Overview

ScyllaDB provides DynamoDB-compatible API through Alternator.

| Feature | DynamoDB | ScyllaDB Alternator |
|---------|----------|-------------------|
| API Compatibility | Native | High |
| Data Types | Full | Most |
| Transactions | Yes | Limited |
| Streams | Yes | CDC |

#### Step 1: Export DynamoDB Data

```bash
# Using AWS CLI
aws dynamodb scan \
  --table-name MyTable \
  --output json > mytable_export.json

# Or use AWS Data Pipeline for large tables
```

#### Step 2: Start ScyllaDB with Alternator

The generated docker-compose.yml includes:

```yaml
scylladb:
  image: scylladb/scylla:5.2
  command: --alternator-port=8000 --alternator-write-isolation=always
  ports:
    - "9042:9042"    # CQL
    - "8000:8000"    # DynamoDB-compatible API
  volumes:
    - ./data/scylla:/var/lib/scylla
```

```bash
docker compose up -d scylladb
```

#### Step 3: Create Tables

```python
# Python script to recreate tables
import boto3

# Point to ScyllaDB Alternator
dynamodb = boto3.resource(
    'dynamodb',
    endpoint_url='http://localhost:8000',
    region_name='local'
)

# Create table with same schema
table = dynamodb.create_table(
    TableName='MyTable',
    KeySchema=[
        {'AttributeName': 'pk', 'KeyType': 'HASH'},
        {'AttributeName': 'sk', 'KeyType': 'RANGE'}
    ],
    AttributeDefinitions=[
        {'AttributeName': 'pk', 'AttributeType': 'S'},
        {'AttributeName': 'sk', 'AttributeType': 'S'}
    ],
    BillingMode='PAY_PER_REQUEST'
)
```

#### Step 4: Import Data

```python
# Import exported data
import json

with open('mytable_export.json') as f:
    data = json.load(f)

table = dynamodb.Table('MyTable')
with table.batch_writer() as batch:
    for item in data['Items']:
        batch.put_item(Item=item)
```

#### Step 5: Update Application

```python
# Old configuration
dynamodb = boto3.resource('dynamodb', region_name='us-east-1')

# New configuration
dynamodb = boto3.resource(
    'dynamodb',
    endpoint_url=os.environ.get('DYNAMODB_ENDPOINT', 'http://scylladb:8000'),
    region_name='local'
)
```

---

### ElastiCache to Redis

#### Step 1: Export Data from ElastiCache

```bash
# Create RDB snapshot in ElastiCache console
# or use BGSAVE if you have access

# Download the snapshot
aws s3 cp s3://your-backup-bucket/redis-snapshot.rdb ./
```

#### Step 2: Start Self-Hosted Redis

```bash
docker compose up -d redis
```

#### Step 3: Restore Data

```bash
# Stop Redis, copy RDB, restart
docker compose stop redis
cp redis-snapshot.rdb ./data/redis/dump.rdb
docker compose start redis
```

#### Alternative: Live Sync with RIOT

```bash
# Use Redis RIOT for live migration
docker run redislabs/riot-redis \
  replicate \
  redis://elasticache-endpoint:6379 \
  redis://localhost:6379
```

#### Step 4: Verify

```bash
docker compose exec redis redis-cli INFO keyspace
docker compose exec redis redis-cli DBSIZE
```

---

## Storage Migration

### AWS S3 to MinIO

#### Overview

MinIO is 100% S3-compatible, making migration straightforward.

| Feature | AWS S3 | MinIO |
|---------|--------|-------|
| S3 API | Native | Full compatibility |
| Bucket policies | Yes | Yes |
| Versioning | Yes | Yes |
| Lifecycle rules | Yes | Yes |
| Multipart upload | Yes | Yes |

#### Step 1: Start MinIO

```bash
docker compose up -d minio
```

Access MinIO Console at `http://localhost:9001`.

#### Step 2: Create Buckets

```bash
# Install MinIO client
brew install minio/stable/mc  # macOS
# or
wget https://dl.min.io/client/mc/release/linux-amd64/mc  # Linux

# Configure aliases
mc alias set s3 https://s3.amazonaws.com $AWS_ACCESS_KEY_ID $AWS_SECRET_ACCESS_KEY
mc alias set minio http://localhost:9000 minioadmin minioadmin

# Create buckets
mc mb minio/my-bucket
```

#### Step 3: Migrate Data

```bash
# Using MinIO Client (recommended for large migrations)
mc mirror s3/source-bucket minio/my-bucket

# Or using the generated script
./scripts/migrate-s3.sh

# For very large buckets, use rclone
rclone sync s3:source-bucket minio:my-bucket --progress
```

#### Step 4: Verify Migration

```bash
# Compare object counts
echo "S3 objects: $(mc ls s3/source-bucket --recursive | wc -l)"
echo "MinIO objects: $(mc ls minio/my-bucket --recursive | wc -l)"

# Verify specific files
mc stat s3/source-bucket/important-file.txt
mc stat minio/my-bucket/important-file.txt
```

#### Step 5: Update Application

```python
# Old configuration (AWS S3)
s3_client = boto3.client('s3')

# New configuration (MinIO)
s3_client = boto3.client(
    's3',
    endpoint_url=os.environ.get('S3_ENDPOINT', 'http://minio:9000'),
    aws_access_key_id=os.environ.get('S3_ACCESS_KEY'),
    aws_secret_access_key=os.environ.get('S3_SECRET_KEY')
)
```

Update environment variables:
```bash
S3_ENDPOINT=http://minio:9000
S3_ACCESS_KEY=minioadmin
S3_SECRET_KEY=your-secret-key
S3_BUCKET=my-bucket
```

---

### EFS to NFS

#### Step 1: Mount EFS on Migration Host

```bash
sudo mount -t nfs4 \
  -o nfsvers=4.1,rsize=1048576,wsize=1048576,hard,timeo=600,retrans=2 \
  fs-12345678.efs.us-east-1.amazonaws.com:/ \
  /mnt/efs
```

#### Step 2: Copy Data

```bash
# Simple copy
rsync -avz --progress /mnt/efs/ /path/to/local/nfs/

# Or use the generated script
./scripts/migrate-efs.sh
```

#### Step 3: Configure NFS in Docker

```yaml
volumes:
  nfs_data:
    driver: local
    driver_opts:
      type: nfs
      o: addr=nfs-server,rw
      device: ":/path/to/share"
```

---

## Authentication Migration

### AWS Cognito to Keycloak

#### Overview

| Feature | Cognito | Keycloak |
|---------|---------|----------|
| OIDC | Yes | Yes |
| SAML | Yes | Yes |
| User Federation | Limited | Extensive |
| Custom Flows | Lambda | SPI/Extensions |
| MFA | Yes | Yes |

#### Step 1: Export Cognito Users

```bash
# Using AWS CLI (basic export)
aws cognito-idp list-users \
  --user-pool-id us-east-1_XXXXX \
  --output json > cognito_users.json

# Note: Passwords cannot be exported from Cognito
# Users will need to reset passwords or use migration triggers
```

#### Step 2: Start Keycloak

```bash
docker compose up -d keycloak
```

Access Keycloak at `https://auth.yourdomain.com` (or `http://localhost:8080`).

#### Step 3: Configure Realm

Create a new realm matching your Cognito User Pool:

1. Login to Keycloak Admin Console
2. Create new realm (e.g., `myapp`)
3. Configure realm settings:
   - Enable user registration (if needed)
   - Configure email settings
   - Set up password policies

#### Step 4: Import Users

```python
# Python script to import users
import json
import requests

KEYCLOAK_URL = "http://localhost:8080"
REALM = "myapp"
ADMIN_TOKEN = "your-admin-token"

with open('cognito_users.json') as f:
    cognito_data = json.load(f)

for user in cognito_data['Users']:
    keycloak_user = {
        "username": user['Username'],
        "email": next((attr['Value'] for attr in user['Attributes']
                      if attr['Name'] == 'email'), None),
        "enabled": user['Enabled'],
        "emailVerified": user['UserStatus'] == 'CONFIRMED',
        "requiredActions": ["UPDATE_PASSWORD"]  # Force password reset
    }

    response = requests.post(
        f"{KEYCLOAK_URL}/admin/realms/{REALM}/users",
        json=keycloak_user,
        headers={"Authorization": f"Bearer {ADMIN_TOKEN}"}
    )
```

#### Step 5: Configure Client Application

```javascript
// Old Cognito configuration
const cognitoConfig = {
  region: 'us-east-1',
  userPoolId: 'us-east-1_XXXXX',
  userPoolWebClientId: 'xxxxx'
};

// New Keycloak configuration
const keycloakConfig = {
  url: process.env.KEYCLOAK_URL || 'https://auth.yourdomain.com',
  realm: 'myapp',
  clientId: 'my-app-client'
};
```

#### Step 6: Set Up User Migration

For seamless password migration, configure Keycloak's User Federation:

1. Go to User Federation in Keycloak
2. Add "cognito-user-storage" provider (custom SPI)
3. Configure Cognito connection details
4. Users authenticate against Cognito on first login, password is then stored in Keycloak

---

## Message Queue Migration

### AWS SQS to RabbitMQ

#### Overview

| Feature | SQS | RabbitMQ |
|---------|-----|----------|
| Protocol | HTTP/HTTPS | AMQP |
| Dead Letter Queue | Yes | Yes |
| Message TTL | Yes | Yes |
| FIFO | Optional | Built-in |
| Exchanges | N/A | Yes (more flexible) |

#### Step 1: Start RabbitMQ

```bash
docker compose up -d rabbitmq
```

Access Management UI at `http://localhost:15672`.

#### Step 2: Create Queues

```bash
# Using RabbitMQ Management API
curl -u guest:guest -X PUT \
  http://localhost:15672/api/queues/%2f/my-queue \
  -H "content-type: application/json" \
  -d '{"durable": true}'
```

Or via the generated configuration:

```yaml
# configs/rabbitmq/definitions.json
{
  "queues": [
    {
      "name": "my-queue",
      "durable": true,
      "arguments": {
        "x-dead-letter-exchange": "dlx",
        "x-message-ttl": 86400000
      }
    }
  ]
}
```

#### Step 3: Migrate Messages (if needed)

```python
# Drain SQS and send to RabbitMQ
import boto3
import pika

sqs = boto3.client('sqs')
queue_url = 'https://sqs.us-east-1.amazonaws.com/123456789/my-queue'

connection = pika.BlockingConnection(
    pika.ConnectionParameters('localhost')
)
channel = connection.channel()
channel.queue_declare(queue='my-queue', durable=True)

while True:
    response = sqs.receive_message(
        QueueUrl=queue_url,
        MaxNumberOfMessages=10,
        WaitTimeSeconds=5
    )

    if 'Messages' not in response:
        break

    for message in response['Messages']:
        channel.basic_publish(
            exchange='',
            routing_key='my-queue',
            body=message['Body'],
            properties=pika.BasicProperties(delivery_mode=2)
        )
        sqs.delete_message(
            QueueUrl=queue_url,
            ReceiptHandle=message['ReceiptHandle']
        )

connection.close()
```

#### Step 4: Update Application

```python
# Old SQS configuration
import boto3
sqs = boto3.client('sqs')

# New RabbitMQ configuration
import pika
connection = pika.BlockingConnection(
    pika.ConnectionParameters(
        host=os.environ.get('RABBITMQ_HOST', 'rabbitmq'),
        credentials=pika.PlainCredentials(
            os.environ.get('RABBITMQ_USER', 'guest'),
            os.environ.get('RABBITMQ_PASS', 'guest')
        )
    )
)
```

---

## Serverless Migration

### AWS Lambda to OpenFaaS

#### Step 1: Start OpenFaaS

```bash
docker compose up -d openfaas
```

#### Step 2: Install faas-cli

```bash
curl -sL https://cli.openfaas.com | sh
faas-cli login --password $(cat ./openfaas-password.txt)
```

#### Step 3: Convert Lambda Functions

Create function template:

```yaml
# stack.yml
version: 1.0
provider:
  name: openfaas
  gateway: http://localhost:8080
functions:
  my-function:
    lang: python3
    handler: ./my-function
    image: my-function:latest
```

Adapt handler code:

```python
# Lambda handler (old)
def lambda_handler(event, context):
    return {'statusCode': 200, 'body': 'Hello'}

# OpenFaaS handler (new)
def handle(req):
    return 'Hello'
```

#### Step 4: Deploy Functions

```bash
faas-cli build -f stack.yml
faas-cli push -f stack.yml
faas-cli deploy -f stack.yml
```

---

## Zero-Downtime Migration Strategy

### Blue-Green Migration

```
Phase 1: Parallel Setup
┌─────────────────────────────────────────────────────────┐
│                                                          │
│   AWS (Blue - Active)         Self-Hosted (Green)       │
│   ┌──────────────────┐       ┌──────────────────┐       │
│   │      RDS         │──────▶│   PostgreSQL     │       │
│   │      S3          │──────▶│     MinIO        │       │
│   │    Cognito       │──────▶│    Keycloak      │       │
│   └──────────────────┘       └──────────────────┘       │
│          ↓                                               │
│   Application (pointing to AWS)                          │
│                                                          │
└─────────────────────────────────────────────────────────┘

Phase 2: Data Sync
- Real-time replication active
- Application still on Blue

Phase 3: Cutover
┌─────────────────────────────────────────────────────────┐
│                                                          │
│   AWS (Blue - Standby)       Self-Hosted (Green - Active)│
│   ┌──────────────────┐       ┌──────────────────┐       │
│   │      RDS         │       │   PostgreSQL     │◀──┐   │
│   │      S3          │       │     MinIO        │◀──┤   │
│   │    Cognito       │       │    Keycloak      │◀──┤   │
│   └──────────────────┘       └──────────────────┘   │   │
│                                      ↓              │   │
│                         Application (pointing to ───┘   │
│                              Self-Hosted)               │
│                                                          │
└─────────────────────────────────────────────────────────┘
```

### Migration Steps

1. **Prepare** (Day -7 to -1)
   - Set up self-hosted infrastructure
   - Configure replication from cloud to self-hosted
   - Test with read-only queries

2. **Sync** (Day 0, T-4h to T-0)
   - Ensure replication is current
   - Prepare DNS changes (low TTL already set)

3. **Cutover** (Day 0, T-0)
   - Stop writes to cloud services
   - Wait for final replication
   - Update DNS/configuration
   - Enable writes to self-hosted
   - Monitor closely

4. **Validate** (Day 0, T+1h)
   - Run automated tests
   - Check application logs
   - Verify data integrity

5. **Cleanup** (Day +7)
   - Disable replication
   - Archive cloud backups
   - Terminate cloud resources

---

## Rollback Procedures

### Quick Rollback (within 1 hour)

```bash
# 1. Stop application
docker compose stop app

# 2. Revert DNS/configuration to cloud endpoints
# 3. Restart with original configuration
docker compose up -d
```

### Data Rollback (if self-hosted data is corrupted)

```bash
# 1. Stop self-hosted services
docker compose stop

# 2. Restore from backup
./scripts/restore-backup.sh latest

# 3. Or revert to cloud entirely
# Update configuration and restart
```

### Rollback Checklist

- [ ] Application configuration reverted
- [ ] DNS pointing to original endpoints
- [ ] Data integrity verified
- [ ] Monitoring alerts cleared
- [ ] Team notified
- [ ] Incident documented

---

## Migration Scripts Reference

Homeport generates several migration scripts in the `scripts/` directory:

| Script | Purpose |
|--------|---------|
| `migrate-s3.sh` | Sync S3 bucket to MinIO |
| `migrate-rds.sh` | Dump and restore RDS databases |
| `migrate-dynamodb.sh` | Export DynamoDB to ScyllaDB |
| `migrate-cognito.sh` | Export Cognito users |
| `backup.sh` | Create backups of all services |
| `restore.sh` | Restore from backups |

Run any script with `--help` for usage information:

```bash
./scripts/migrate-s3.sh --help
```
