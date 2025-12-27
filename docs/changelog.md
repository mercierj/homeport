# Changelog

All notable changes to the Homeport API are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Production deployment guide documentation
- Security hardening documentation
- API changelog
- Test fixtures for full-stack and serverless-api scenarios

### Changed
- Updated API reference response format documentation

---

## [1.0.0] - 2024-01-15

### Added

#### API Endpoints
- **Authentication**: Session-based authentication with secure cookie handling
  - `POST /api/v1/auth/login` - User login with rate limiting
  - `POST /api/v1/auth/logout` - Session invalidation
  - `GET /api/v1/auth/me` - Current user information
  - `POST /api/v1/auth/change-password` - Password change

- **Migration**: Cloud infrastructure analysis and Docker Compose generation
  - `POST /api/v1/migrate/analyze` - Parse IaC files (Terraform, CloudFormation, ARM)
  - `POST /api/v1/migrate/generate` - Generate Docker Compose from resources
  - `POST /api/v1/migrate/download` - Download generated stack as ZIP
  - `POST /api/v1/migrate/discover` - Discover resources via cloud APIs

- **Discovery State Management**:
  - `GET /api/v1/migrate/discoveries` - List saved discoveries
  - `GET /api/v1/migrate/discoveries/{id}` - Get specific discovery
  - `POST /api/v1/migrate/discoveries` - Save discovery result
  - `PATCH /api/v1/migrate/discoveries/{id}` - Rename discovery
  - `DELETE /api/v1/migrate/discoveries/{id}` - Delete discovery

- **Credentials**: In-memory credential storage per session
  - `POST /api/v1/credentials/storage` - Store MinIO/S3 credentials
  - `DELETE /api/v1/credentials/storage` - Clear storage credentials
  - `POST /api/v1/credentials/database` - Store PostgreSQL credentials
  - `DELETE /api/v1/credentials/database` - Clear database credentials

- **Container Management**: Docker container operations
  - `GET /api/v1/stacks/{stackId}/containers` - List containers
  - `GET /api/v1/stacks/{stackId}/containers/{name}/logs` - Get container logs
  - `POST /api/v1/stacks/{stackId}/containers/{name}/restart` - Restart container
  - `POST /api/v1/stacks/{stackId}/containers/{name}/stop` - Stop container
  - `POST /api/v1/stacks/{stackId}/containers/{name}/start` - Start container

- **Storage**: S3-compatible object storage operations
  - `GET /api/v1/stacks/{stackId}/storage/buckets` - List buckets
  - `POST /api/v1/stacks/{stackId}/storage/buckets` - Create bucket
  - `DELETE /api/v1/stacks/{stackId}/storage/buckets/{bucket}` - Delete bucket
  - `GET /api/v1/stacks/{stackId}/storage/buckets/{bucket}/objects` - List objects
  - `POST /api/v1/stacks/{stackId}/storage/buckets/{bucket}/upload` - Upload file
  - `GET /api/v1/stacks/{stackId}/storage/buckets/{bucket}/download/{key}` - Download file
  - `DELETE /api/v1/stacks/{stackId}/storage/buckets/{bucket}/objects/{key}` - Delete object
  - `GET /api/v1/stacks/{stackId}/storage/buckets/{bucket}/presign/{key}` - Get presigned URL

- **Database**: PostgreSQL database operations
  - `GET /api/v1/stacks/{stackId}/database/databases` - List databases
  - `GET /api/v1/stacks/{stackId}/database/tables` - List tables
  - `GET /api/v1/stacks/{stackId}/database/tables/{table}/schema` - Get table schema
  - `GET /api/v1/stacks/{stackId}/database/tables/{table}/data` - Get table data
  - `POST /api/v1/stacks/{stackId}/database/query` - Execute SQL query

- **Health**: Application health monitoring
  - `GET /health` - Basic health check
  - `GET /health/detailed` - Detailed health with dependencies
  - `GET /health/ready` - Kubernetes readiness probe
  - `GET /health/live` - Kubernetes liveness probe

#### Cloud Provider Support
- **AWS**: EC2, RDS, S3, ECS, EKS, Lambda, ALB, CloudFront, SQS, SNS, ElastiCache, Cognito, Secrets Manager, API Gateway, DynamoDB, EventBridge
- **Azure**: Virtual Machines, App Service, Functions, AKS, Container Instances, SQL, PostgreSQL, MySQL, Cosmos DB, Cache for Redis, Storage, Service Bus, Event Hubs, Event Grid, Logic Apps, Key Vault, AD B2C, CDN, Front Door, Application Gateway, DNS, VNet
- **GCP**: Compute Engine, Cloud Run, Cloud Functions, App Engine, GKE, Cloud SQL, Spanner, Firestore, Bigtable, Memorystore, Cloud Storage, Pub/Sub, Cloud Tasks, Cloud Scheduler, Cloud CDN, Cloud DNS, Cloud Load Balancing, Secret Manager, Identity Platform, IAM, Cloud Armor

#### Parser Support
- Terraform HCL files (`.tf`)
- Terraform state files (`.tfstate`)
- AWS CloudFormation templates (YAML/JSON)
- Azure ARM templates (JSON)
- Azure Bicep files (`.bicep`)
- GCP Deployment Manager templates (YAML)

### Security
- Session-based authentication with HttpOnly, Secure, SameSite cookies
- SQL injection prevention with parameterized queries
- Read-only query mode by default
- Rate limiting on authentication endpoints (5 requests/minute)
- Account lockout after 5 failed attempts
- Password hashing with Argon2id
- CORS configuration
- Security headers (HSTS, CSP, X-Frame-Options, etc.)
- Input validation on all endpoints

---

## [0.9.0] - 2024-01-10

### Added
- Multi-region AWS resource discovery
- SQL security hardening with blocked operations
- Session and cookie security improvements
- Comprehensive unit tests for all cloud mappers

### Changed
- Improved resource dependency detection
- Enhanced error messages for failed mappings

### Fixed
- Cycle detection for non-existent dependencies
- Azure messaging mappers registration

---

## [0.8.0] - 2024-01-05

### Added
- Database explorer with query editor UI
- Docker container management dashboard
- Real-time container logs streaming

### Changed
- Improved container status display
- Enhanced log viewer with search

---

## [0.7.0] - 2024-01-01

### Added
- Dedicated parsers for AWS, GCP, and Azure
- Support for Azure Bicep files
- GCP Deployment Manager template parsing

### Changed
- Refactored parser architecture for better extensibility
- Improved resource type detection

---

## [0.6.0] - 2023-12-20

### Added
- Go backend foundation
- RESTful API implementation
- OpenAPI specification

### Changed
- Migrated from prototype to production architecture

---

## API Versioning

The API uses URL path versioning (`/api/v1/`). Breaking changes will increment the major version number.

### Deprecation Policy

- Deprecated endpoints are marked in documentation
- Deprecated endpoints remain functional for 6 months
- Removal is announced at least 3 months in advance

### Breaking Changes Guidelines

Changes that require a new API version:
- Removing endpoints
- Removing required request fields
- Changing response structure
- Changing authentication mechanism
- Changing error response format

Changes that do NOT require a new version:
- Adding new endpoints
- Adding optional request fields
- Adding new response fields
- Adding new error codes
- Performance improvements

---

## Migration Guides

### Migrating from v0.x to v1.0

1. **Authentication**: Session cookies are now HttpOnly and Secure
   - Ensure your frontend handles authentication via cookies
   - Remove any manual token handling

2. **Response Format**: Success responses no longer use wrapper
   - Old: `{"status": "ok", "data": {...}}`
   - New: `{...}` (data directly)

3. **Error Format**: Consistent error structure
   - All errors now return `{"error": "message", "details": "..."}`

4. **Rate Limiting**: Authentication endpoints are now rate-limited
   - Login: 5 requests/minute
   - Implement exponential backoff for retries

---

## Support

For API-related questions or issues:
- Documentation: [API Reference](api-reference.md)
- Issues: [GitHub Issues](https://github.com/homeport/homeport/issues)
- Security: security@homeport.local
