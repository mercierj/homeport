# Homeport API Reference

This document provides a reference for the Homeport REST API. For the complete OpenAPI specification, see [`api/openapi.yaml`](../api/openapi.yaml).

## Base URL

- **Local Development**: `http://localhost:8080`
- **Production**: `https://api.homeport.local`

## Authentication

Most API endpoints require authentication. Homeport supports two authentication methods:

### Session Cookie

After a successful login, a `session` cookie is set automatically. This cookie is used for browser-based authentication.

### Bearer Token

For programmatic access, use the token from the login response in the `Authorization` header:

```bash
curl -H "Authorization: Bearer <token>" http://localhost:8080/api/v1/...
```

## Common Response Formats

### Success Response

Successful responses return JSON data directly without a wrapper. Each endpoint documents its specific response format below.

```json
{
  "field1": "value1",
  "field2": "value2"
}
```

### Error Response

Error responses include an error message and optional details:

```json
{
  "error": "Description of what went wrong",
  "details": "Additional context (optional)"
}
```

### Pagination Response

List endpoints return paginated results:

```json
{
  "items": [...],
  "count": 25,
  "total": 100,
  "offset": 0,
  "limit": 25
}
```

### HTTP Status Codes

| Code | Description |
|------|-------------|
| 200  | Success |
| 201  | Created - Resource successfully created |
| 204  | No Content - Successful deletion |
| 400  | Bad Request - Invalid parameters or malformed request |
| 401  | Unauthorized - Missing or invalid authentication |
| 403  | Forbidden - Valid authentication but insufficient permissions |
| 404  | Not Found - Resource doesn't exist |
| 409  | Conflict - Resource already exists or version conflict |
| 422  | Unprocessable Entity - Validation error |
| 500  | Internal Server Error |
| 502  | Bad Gateway - External service connection failed |
| 503  | Service Unavailable - Dependency unavailable |

---

## Health Endpoints

### GET /health

Basic health check for load balancer probes.

**Response:**
```json
{
  "status": "healthy"
}
```

### GET /health/detailed

Detailed health status including dependencies and system info. Returns 503 if unhealthy.

**Response:**
```json
{
  "status": "healthy",
  "version": "1.0.0",
  "uptime": "2h30m15s",
  "started_at": "2024-01-10T08:00:00Z",
  "dependencies": {
    "docker": {
      "status": "healthy",
      "latency": "5ms",
      "error": ""
    }
  },
  "system": {
    "go_version": "go1.21.0",
    "num_goroutine": 12,
    "num_cpu": 8
  }
}
```

**Health Status Values:**
- `healthy` - All dependencies operational
- `degraded` - Some optional dependencies unavailable
- `unhealthy` - Critical dependencies failed

### GET /health/ready

Kubernetes readiness probe. Returns 200 when the service is ready to accept traffic.

**Response:**
```json
{
  "status": "ready"
}
```

### GET /health/live

Kubernetes liveness probe. Returns 200 when the service is alive.

**Response:**
```json
{
  "status": "alive"
}
```

---

## Authentication Endpoints

### POST /api/v1/auth/login

Authenticate and obtain a session token.

**Request:**
```json
{
  "username": "admin",
  "password": "your-password"
}
```

**Response:**
```json
{
  "token": "abc123...",
  "username": "admin",
  "expires_at": "2024-01-15T12:00:00Z"
}
```

### POST /api/v1/auth/logout

Invalidate current session.

**Response:**
```json
{
  "status": "logged out"
}
```

### GET /api/v1/auth/me

Get current user information.

**Response:**
```json
{
  "username": "admin",
  "expires_at": "2024-01-15T12:00:00Z"
}
```

### POST /api/v1/auth/change-password

Change password for current user.

**Request:**
```json
{
  "old_password": "current-password",
  "new_password": "new-password"
}
```

---

## Migration Endpoints

### POST /api/v1/migrate/analyze

Analyze infrastructure-as-code files and discover resources.

**Request:**
```json
{
  "type": "terraform",
  "content": "resource \"aws_s3_bucket\" \"example\" {\n  bucket = \"my-bucket\"\n}"
}
```

Supported types:
- `terraform` - Terraform HCL files
- `cloudformation` - AWS CloudFormation YAML/JSON
- `arm` - Azure Resource Manager templates

**Response:**
```json
{
  "resources": [
    {
      "id": "aws_s3_bucket.example",
      "name": "example",
      "type": "aws_s3_bucket",
      "category": "storage",
      "dependencies": [],
      "tags": {}
    }
  ],
  "warnings": [],
  "provider": "aws"
}
```

### POST /api/v1/migrate/generate

Generate Docker Compose stack from analyzed resources.

**Request:**
```json
{
  "resources": [
    {
      "id": "aws_s3_bucket.example",
      "name": "example",
      "type": "aws_s3_bucket",
      "category": "storage"
    }
  ],
  "options": {
    "ha": false,
    "include_migration": true,
    "include_monitoring": false,
    "domain": "myapp.local"
  }
}
```

**Response:**
```json
{
  "compose": "version: '3.8'\n\nservices:\n  minio:\n    image: minio/minio:latest\n    ...",
  "scripts": {
    "start.sh": "#!/bin/bash\ndocker compose up -d",
    "stop.sh": "#!/bin/bash\ndocker compose down"
  },
  "docs": "# Generated Homeport Stack\n\n..."
}
```

### POST /api/v1/migrate/download

Download generated stack as a ZIP file.

**Request:** Same as `/generate`

**Response:** ZIP file containing:
- `docker-compose.yml`
- `scripts/start.sh`
- `scripts/stop.sh`
- `README.md`

---

## Credentials Endpoints

Credentials are stored in-memory per session and used to connect to storage and database services.

### POST /api/v1/credentials/storage

Store MinIO/S3 credentials.

**Request:**
```json
{
  "endpoint": "localhost:9000",
  "access_key": "minioadmin",
  "secret_key": "minioadmin"
}
```

### DELETE /api/v1/credentials/storage

Remove stored storage credentials.

### POST /api/v1/credentials/database

Store PostgreSQL credentials.

**Request:**
```json
{
  "host": "localhost",
  "port": 5432,
  "database": "mydb",
  "user": "postgres",
  "password": "secret",
  "ssl_mode": "disable"
}
```

### DELETE /api/v1/credentials/database

Remove stored database credentials.

---

## Container Endpoints

Manage Docker containers within a stack.

### GET /api/v1/stacks/{stackId}/containers

List all containers in a stack.

**Parameters:**
- `stackId` - Stack identifier (use "default" for default stack)

**Response:**
```json
{
  "containers": [
    {
      "id": "abc123",
      "name": "myapp-postgres",
      "image": "postgres:15",
      "status": "Up 2 hours",
      "state": "running",
      "ports": [
        {
          "host_port": "5432",
          "container_port": "5432",
          "protocol": "tcp"
        }
      ],
      "created": "2024-01-10T10:00:00Z",
      "labels": {
        "com.docker.compose.project": "myapp"
      }
    }
  ],
  "count": 1
}
```

### GET /api/v1/stacks/{stackId}/containers/{name}/logs

Get container logs.

**Parameters:**
- `name` - Container name
- `tail` - Number of lines (1-10000, default 100)

**Response:**
```json
{
  "logs": "2024-01-10 10:00:00 INFO Starting application..."
}
```

### POST /api/v1/stacks/{stackId}/containers/{name}/restart

Restart a container.

### POST /api/v1/stacks/{stackId}/containers/{name}/stop

Stop a container.

### POST /api/v1/stacks/{stackId}/containers/{name}/start

Start a container.

---

## Storage Endpoints

S3-compatible object storage operations (MinIO).

### GET /api/v1/stacks/{stackId}/storage/buckets

List all buckets.

**Response:**
```json
{
  "buckets": [
    {
      "name": "my-bucket",
      "created": "2024-01-10T10:00:00Z",
      "region": "us-east-1"
    }
  ],
  "count": 1
}
```

### POST /api/v1/stacks/{stackId}/storage/buckets

Create a new bucket.

**Request:**
```json
{
  "name": "my-new-bucket"
}
```

Bucket naming rules:
- 3-63 characters
- Lowercase letters, numbers, hyphens, and periods only
- Must start and end with letter or number

### DELETE /api/v1/stacks/{stackId}/storage/buckets/{bucket}

Delete an empty bucket.

### GET /api/v1/stacks/{stackId}/storage/buckets/{bucket}/objects

List objects in a bucket.

**Parameters:**
- `prefix` - Filter by prefix (optional)

**Response:**
```json
{
  "objects": [
    {
      "key": "documents/report.pdf",
      "size": 1048576,
      "last_modified": "2024-01-10T10:00:00Z",
      "content_type": "application/pdf",
      "is_dir": false
    }
  ],
  "count": 1,
  "bucket": "my-bucket",
  "prefix": "documents/"
}
```

### POST /api/v1/stacks/{stackId}/storage/buckets/{bucket}/upload

Upload a file.

**Request:** `multipart/form-data`
- `file` - File to upload (required)
- `key` - Object key (optional, defaults to filename)

### GET /api/v1/stacks/{stackId}/storage/buckets/{bucket}/download/{key}

Download a file.

### DELETE /api/v1/stacks/{stackId}/storage/buckets/{bucket}/objects/{key}

Delete an object.

### GET /api/v1/stacks/{stackId}/storage/buckets/{bucket}/presign/{key}

Get a presigned URL for direct access (valid for 15 minutes).

**Response:**
```json
{
  "url": "http://localhost:9000/my-bucket/file.pdf?X-Amz-..."
}
```

---

## Database Endpoints

PostgreSQL database operations.

### GET /api/v1/stacks/{stackId}/database/databases

List all databases.

**Response:**
```json
{
  "databases": ["postgres", "myapp", "template0", "template1"],
  "count": 4
}
```

### GET /api/v1/stacks/{stackId}/database/tables

List tables in a schema.

**Parameters:**
- `schema` - Schema name (default "public")

**Response:**
```json
{
  "tables": [
    {
      "schema": "public",
      "name": "users",
      "type": "BASE TABLE",
      "owner": "postgres",
      "row_count": 1234,
      "size": "64 kB"
    }
  ],
  "count": 1,
  "schema": "public"
}
```

### GET /api/v1/stacks/{stackId}/database/tables/{table}/schema

Get table schema (columns).

**Response:**
```json
{
  "columns": [
    {
      "name": "id",
      "type": "integer",
      "nullable": false,
      "default": "nextval('users_id_seq'::regclass)",
      "primary_key": true
    },
    {
      "name": "email",
      "type": "character varying(255)",
      "nullable": false,
      "default": null,
      "primary_key": false
    }
  ],
  "table": "users",
  "schema": "public"
}
```

### GET /api/v1/stacks/{stackId}/database/tables/{table}/data

Get table data.

**Parameters:**
- `schema` - Schema name (default "public")
- `limit` - Max rows (1-1000, default 100)

**Response:**
```json
{
  "columns": ["id", "email", "created_at"],
  "rows": [
    [1, "user@example.com", "2024-01-10T10:00:00Z"]
  ],
  "row_count": 1,
  "duration": "5ms"
}
```

### POST /api/v1/stacks/{stackId}/database/query

Execute a SQL query.

**Request:**
```json
{
  "query": "SELECT * FROM users WHERE active = true LIMIT 10",
  "read_only": true
}
```

**Response:**
```json
{
  "columns": ["id", "email", "active"],
  "rows": [
    [1, "user@example.com", true]
  ],
  "row_count": 1,
  "duration": "3ms"
}
```

Note: Queries are executed in read-only mode by default for safety.

---

## OpenAPI Specification

The complete OpenAPI 3.0 specification is available at:
- **File**: [`api/openapi.yaml`](../api/openapi.yaml)
- **Swagger UI**: `http://localhost:8080/swagger/` (when running the server)

You can use tools like [Swagger Editor](https://editor.swagger.io) to visualize the spec or generate client libraries.
