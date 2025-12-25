# Example: Serverless Application

This example demonstrates migrating a serverless application (Lambda + API Gateway + DynamoDB) from AWS to a self-hosted Docker stack.

## Architecture

```
                       AWS Architecture
┌────────────────────────────────────────────────────────────┐
│                                                             │
│   ┌─────────────┐                                          │
│   │ API Gateway │                                          │
│   └──────┬──────┘                                          │
│          │                                                  │
│   ┌──────┴───────────────────────────────┐                 │
│   │              │              │         │                 │
│   ▼              ▼              ▼         ▼                 │
│ ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐       │
│ │  Lambda  │ │  Lambda  │ │  Lambda  │ │  Lambda  │       │
│ │ (Create) │ │  (Read)  │ │ (Update) │ │ (Delete) │       │
│ └────┬─────┘ └────┬─────┘ └────┬─────┘ └────┬─────┘       │
│      │            │            │            │               │
│      └────────────┼────────────┼────────────┘               │
│                   │            │                            │
│                   ▼            ▼                            │
│            ┌──────────┐  ┌──────────┐                      │
│            │ DynamoDB │  │    S3    │                      │
│            └──────────┘  └──────────┘                      │
│                                                             │
└────────────────────────────────────────────────────────────┘

                          ▼ CloudExit ▼

                    Self-Hosted Architecture
┌────────────────────────────────────────────────────────────┐
│                                                             │
│   ┌─────────────────────────────────────────────────┐      │
│   │                    Traefik                       │      │
│   │           (API Gateway + Routing)                │      │
│   └──────────────────────┬──────────────────────────┘      │
│                          │                                  │
│   ┌──────────────────────┴───────────────────────┐         │
│   │              │              │         │       │         │
│   ▼              ▼              ▼         ▼       ▼         │
│ ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐        │
│ │   API    │ │   API    │ │   API    │ │   API    │        │
│ │(Container│ │(Container│ │(Container│ │(Container│        │
│ └────┬─────┘ └────┬─────┘ └────┬─────┘ └────┬─────┘        │
│      │            │            │            │               │
│      └────────────┼────────────┼────────────┘               │
│                   │            │                            │
│                   ▼            ▼                            │
│            ┌──────────┐  ┌──────────┐                      │
│            │ ScyllaDB │  │  MinIO   │                      │
│            └──────────┘  └──────────┘                      │
│                                                             │
└────────────────────────────────────────────────────────────┘
```

## Terraform Configuration (Input)

### `main.tf`

```hcl
# API Gateway
resource "aws_api_gateway_rest_api" "main" {
  name        = "items-api"
  description = "CRUD API for items"
}

resource "aws_api_gateway_resource" "items" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_rest_api.main.root_resource_id
  path_part   = "items"
}

resource "aws_api_gateway_resource" "item" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.items.id
  path_part   = "{id}"
}

# Lambda Functions
resource "aws_lambda_function" "create_item" {
  filename         = "create.zip"
  function_name    = "create-item"
  role             = aws_iam_role.lambda.arn
  handler          = "index.handler"
  runtime          = "nodejs18.x"
  memory_size      = 256
  timeout          = 30

  environment {
    variables = {
      TABLE_NAME = aws_dynamodb_table.items.name
    }
  }
}

resource "aws_lambda_function" "get_item" {
  filename         = "get.zip"
  function_name    = "get-item"
  role             = aws_iam_role.lambda.arn
  handler          = "index.handler"
  runtime          = "nodejs18.x"
  memory_size      = 256
  timeout          = 10

  environment {
    variables = {
      TABLE_NAME = aws_dynamodb_table.items.name
    }
  }
}

resource "aws_lambda_function" "list_items" {
  filename         = "list.zip"
  function_name    = "list-items"
  role             = aws_iam_role.lambda.arn
  handler          = "index.handler"
  runtime          = "nodejs18.x"
  memory_size      = 512
  timeout          = 30

  environment {
    variables = {
      TABLE_NAME = aws_dynamodb_table.items.name
    }
  }
}

resource "aws_lambda_function" "update_item" {
  filename         = "update.zip"
  function_name    = "update-item"
  role             = aws_iam_role.lambda.arn
  handler          = "index.handler"
  runtime          = "nodejs18.x"
  memory_size      = 256
  timeout          = 30

  environment {
    variables = {
      TABLE_NAME = aws_dynamodb_table.items.name
    }
  }
}

resource "aws_lambda_function" "delete_item" {
  filename         = "delete.zip"
  function_name    = "delete-item"
  role             = aws_iam_role.lambda.arn
  handler          = "index.handler"
  runtime          = "nodejs18.x"
  memory_size      = 256
  timeout          = 10

  environment {
    variables = {
      TABLE_NAME = aws_dynamodb_table.items.name
    }
  }
}

# DynamoDB Table
resource "aws_dynamodb_table" "items" {
  name           = "items"
  billing_mode   = "PAY_PER_REQUEST"
  hash_key       = "id"

  attribute {
    name = "id"
    type = "S"
  }

  attribute {
    name = "userId"
    type = "S"
  }

  global_secondary_index {
    name            = "userId-index"
    hash_key        = "userId"
    projection_type = "ALL"
  }

  tags = {
    Environment = "production"
  }
}

# S3 Bucket for uploads
resource "aws_s3_bucket" "uploads" {
  bucket = "items-uploads"
}
```

## CloudExit Migration Strategy

For serverless applications, CloudExit offers two approaches:

### Option 1: Containerized Functions (Recommended)

Convert each Lambda to a Docker container that handles HTTP requests.

### Option 2: Consolidated API

Combine all Lambdas into a single API container.

## CloudExit Migration

```bash
cloudexit migrate ./terraform \
  --output ./serverless-stack \
  --domain api.example.com \
  --serverless-mode=containerized
```

## Generated Output

### `docker-compose.yml` (Containerized Mode)

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
      - frontend
      - backend

  # Consolidated API Service
  # (Lambdas combined into single container)
  api:
    image: node:18-alpine
    restart: unless-stopped
    working_dir: /app
    command: npm start
    environment:
      NODE_ENV: production
      PORT: 8080
      DATABASE_TYPE: scylladb
      DATABASE_HOST: scylladb
      DATABASE_PORT: 9042
      DATABASE_KEYSPACE: items
      S3_ENDPOINT: http://minio:9000
      S3_ACCESS_KEY: ${MINIO_ACCESS_KEY}
      S3_SECRET_KEY: ${MINIO_SECRET_KEY}
      S3_BUCKET: uploads
    volumes:
      - ./api:/app
    depends_on:
      - scylladb
      - minio
    networks:
      - frontend
      - backend
    labels:
      - "cloudexit.source=aws_lambda_function"
      - "traefik.enable=true"
      - "traefik.http.routers.api.rule=Host(`api.example.com`)"
      - "traefik.http.routers.api.entrypoints=websecure"
      - "traefik.http.routers.api.tls.certresolver=letsencrypt"
      - "traefik.http.services.api.loadbalancer.server.port=8080"
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
      interval: 30s
      timeout: 10s
      retries: 3

  # DynamoDB replacement
  scylladb:
    image: scylladb/scylla:latest
    restart: unless-stopped
    command: --alternator-port=8000 --alternator-write-isolation=always
    ports:
      - "9042:9042"   # CQL
      - "8000:8000"   # DynamoDB-compatible API
    volumes:
      - ./data/scylladb:/var/lib/scylla
    networks:
      - backend
    labels:
      - "cloudexit.source=aws_dynamodb_table"
    healthcheck:
      test: ["CMD-SHELL", "nodetool status"]
      interval: 30s
      timeout: 10s
      retries: 5

  # S3 replacement
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
      - backend
    labels:
      - "cloudexit.source=aws_s3_bucket"
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:9000/minio/health/live"]
      interval: 30s
      timeout: 5s
      retries: 3

networks:
  frontend:
    driver: bridge
  backend:
    driver: bridge
```

### API Wrapper Code

CloudExit generates an Express.js wrapper to replace Lambda handlers:

`api/server.js`:

```javascript
const express = require('express');
const { ScyllaClient } = require('./db');
const { S3Client } = require('./storage');

const app = express();
app.use(express.json());

const db = new ScyllaClient({
  contactPoints: [process.env.DATABASE_HOST],
  keyspace: process.env.DATABASE_KEYSPACE,
});

const s3 = new S3Client({
  endpoint: process.env.S3_ENDPOINT,
  accessKeyId: process.env.S3_ACCESS_KEY,
  secretAccessKey: process.env.S3_SECRET_KEY,
});

// Health check
app.get('/health', (req, res) => {
  res.json({ status: 'healthy' });
});

// CREATE - POST /items
app.post('/items', async (req, res) => {
  try {
    const item = {
      id: require('crypto').randomUUID(),
      ...req.body,
      createdAt: new Date().toISOString(),
    };
    await db.insert('items', item);
    res.status(201).json(item);
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

// READ - GET /items/:id
app.get('/items/:id', async (req, res) => {
  try {
    const item = await db.findOne('items', { id: req.params.id });
    if (!item) {
      return res.status(404).json({ error: 'Item not found' });
    }
    res.json(item);
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

// LIST - GET /items
app.get('/items', async (req, res) => {
  try {
    const items = await db.findAll('items', req.query);
    res.json(items);
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

// UPDATE - PUT /items/:id
app.put('/items/:id', async (req, res) => {
  try {
    const item = await db.update('items', req.params.id, req.body);
    res.json(item);
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

// DELETE - DELETE /items/:id
app.delete('/items/:id', async (req, res) => {
  try {
    await db.delete('items', req.params.id);
    res.status(204).send();
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

const PORT = process.env.PORT || 8080;
app.listen(PORT, () => {
  console.log(`API server running on port ${PORT}`);
});
```

### Traefik API Gateway Configuration

`traefik/dynamic/api.yml`:

```yaml
http:
  routers:
    api:
      rule: "Host(`api.example.com`)"
      service: api
      entryPoints:
        - websecure
      tls:
        certResolver: letsencrypt
      middlewares:
        - ratelimit
        - cors
        - auth-headers

  services:
    api:
      loadBalancer:
        servers:
          - url: "http://api:8080"
        healthCheck:
          path: /health
          interval: 10s

  middlewares:
    ratelimit:
      rateLimit:
        average: 100
        burst: 50
        period: 1s

    cors:
      headers:
        accessControlAllowMethods:
          - GET
          - POST
          - PUT
          - DELETE
          - OPTIONS
        accessControlAllowHeaders:
          - Content-Type
          - Authorization
        accessControlAllowOriginList:
          - "https://app.example.com"
        accessControlMaxAge: 86400

    auth-headers:
      headers:
        customRequestHeaders:
          X-Request-ID: "{{uuid}}"
```

## DynamoDB Migration

### Create ScyllaDB Schema

`scripts/init-scylladb.sh`:

```bash
#!/bin/bash
set -e

echo "Waiting for ScyllaDB to be ready..."
until cqlsh scylladb -e "SELECT now() FROM system.local" > /dev/null 2>&1; do
  sleep 2
done

echo "Creating keyspace and tables..."
cqlsh scylladb << EOF
CREATE KEYSPACE IF NOT EXISTS items
WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1};

USE items;

CREATE TABLE IF NOT EXISTS items (
  id text PRIMARY KEY,
  userId text,
  name text,
  description text,
  data text,
  createdAt timestamp,
  updatedAt timestamp
);

CREATE INDEX IF NOT EXISTS ON items (userId);
EOF

echo "ScyllaDB initialized successfully!"
```

### Migrate DynamoDB Data

`scripts/migrate-dynamodb.sh`:

```bash
#!/bin/bash
set -e

echo "DynamoDB to ScyllaDB Migration"
echo "=============================="

# Export from DynamoDB
echo "Step 1: Exporting data from DynamoDB..."
aws dynamodb scan \
  --table-name items \
  --output json > /tmp/dynamodb-export.json

# Transform and import
echo "Step 2: Importing to ScyllaDB..."
node << 'EOF'
const fs = require('fs');
const { Client } = require('cassandra-driver');

const client = new Client({
  contactPoints: ['scylladb'],
  localDataCenter: 'datacenter1',
  keyspace: 'items'
});

async function migrate() {
  await client.connect();

  const data = JSON.parse(fs.readFileSync('/tmp/dynamodb-export.json'));

  for (const item of data.Items) {
    const query = `
      INSERT INTO items (id, userId, name, description, data, createdAt)
      VALUES (?, ?, ?, ?, ?, ?)
    `;
    await client.execute(query, [
      item.id.S,
      item.userId?.S,
      item.name?.S,
      item.description?.S,
      JSON.stringify(item),
      new Date()
    ], { prepare: true });
  }

  console.log(`Migrated ${data.Items.length} items`);
  await client.shutdown();
}

migrate().catch(console.error);
EOF

echo "Migration complete!"
```

## Application Code Changes

### Before (Lambda + AWS SDK)

```javascript
const { DynamoDB } = require('@aws-sdk/client-dynamodb');
const { DynamoDBDocument } = require('@aws-sdk/lib-dynamodb');

const client = new DynamoDB({});
const dynamo = DynamoDBDocument.from(client);

exports.handler = async (event) => {
  const result = await dynamo.get({
    TableName: process.env.TABLE_NAME,
    Key: { id: event.pathParameters.id }
  });
  return {
    statusCode: 200,
    body: JSON.stringify(result.Item)
  };
};
```

### After (Express + ScyllaDB)

```javascript
const { Client } = require('cassandra-driver');

const client = new Client({
  contactPoints: [process.env.DATABASE_HOST],
  localDataCenter: 'datacenter1',
  keyspace: process.env.DATABASE_KEYSPACE
});

app.get('/items/:id', async (req, res) => {
  const result = await client.execute(
    'SELECT * FROM items WHERE id = ?',
    [req.params.id],
    { prepare: true }
  );

  if (result.rows.length === 0) {
    return res.status(404).json({ error: 'Not found' });
  }

  res.json(result.rows[0]);
});
```

## Deployment

### 1. Initialize ScyllaDB

```bash
docker compose up -d scylladb
sleep 30  # Wait for ScyllaDB to start
./scripts/init-scylladb.sh
```

### 2. Migrate Data

```bash
./scripts/migrate-dynamodb.sh
```

### 3. Start Services

```bash
docker compose up -d
```

### 4. Verify

```bash
# Test API
curl https://api.example.com/health

# Test CRUD
curl -X POST https://api.example.com/items \
  -H "Content-Type: application/json" \
  -d '{"name": "Test Item"}'
```

## Performance Comparison

| Metric | Lambda | Self-Hosted |
|--------|--------|-------------|
| Cold Start | 100-500ms | N/A |
| Warm Response | 10-50ms | 5-20ms |
| Max Concurrent | 1000 | Limited by resources |
| Cost (1M requests) | ~$20 | ~$5 (VPS cost) |

## Scaling

```bash
# Scale API containers
docker compose up -d --scale api=4

# ScyllaDB handles more traffic via:
# - Add more nodes to the cluster
# - Use SSD storage
# - Increase memory allocation
```
