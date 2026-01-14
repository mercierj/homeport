# HPRT Bundle & Migration Wizard Implementation Plan

## Security Principle: NO SECRETS IN BUNDLE

```
┌─────────────────────────────────────────────────────────────────────────┐
│  ⚠️  CRITICAL SECURITY RULE                                             │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  The .hprt bundle NEVER contains secret values.                         │
│                                                                          │
│  ✗ NO passwords                                                          │
│  ✗ NO API keys                                                           │
│  ✗ NO certificates or private keys                                       │
│  ✗ NO connection strings with credentials                                │
│  ✗ NO tokens                                                             │
│                                                                          │
│  Bundle contains REFERENCES only:                                        │
│  → "This app needs DATABASE_PASSWORD from aws-secrets-manager:prod/db"  │
│                                                                          │
│  Secrets are resolved at IMPORT time via:                                │
│  → Interactive prompt                                                    │
│  → Environment variables                                                 │
│  → --secrets-file .env.production                                        │
│  → --pull-secrets-from aws (pulls from source cloud)                    │
│                                                                          │
│  This means the bundle can be safely:                                    │
│  → Stored in version control                                             │
│  → Shared via email/Slack                                                │
│  → Kept as documentation                                                 │
│  → Transferred over insecure channels                                    │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## Vision

Transform cloud migration from a complex multi-step process into a simple wizard:

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         MIGRATION WIZARD                                 │
├─────────────────────────────────────────────────────────────────────────┤
│  ① Analyze  →  ② Export  →  ③ Deploy  →  ④ Sync  →  ⑤ Cutover         │
│     [✓]          [✓]          [○]         [○]         [○]              │
│                                                                          │
│  Source: AWS (us-east-1)           Target: Docker (192.168.1.100)       │
│  Resources: 24                      Status: Deploying stack 3/8...       │
│  Bundle: migration-2024.hprt        Progress: ████████░░ 78%            │
└─────────────────────────────────────────────────────────────────────────┘
```

**Goal:** User runs wizard, answers a few questions, clicks "Migrate" - done.

---

## Bundle Format Specification

### File Extension: `.hprt`

Format: gzip-compressed tar archive (like .tar.gz but with semantic meaning)

### Bundle Structure

```
my-migration.hprt
├── manifest.json              # Bundle metadata & checksums
├── compose/
│   ├── docker-compose.yml     # Main compose file
│   └── docker-compose.override.yml  # Environment-specific overrides
├── configs/
│   ├── nginx/
│   │   └── nginx.conf
│   ├── redis/
│   │   └── redis.conf
│   ├── postgres/
│   │   └── postgresql.conf
│   └── traefik/
│       └── traefik.yml
├── scripts/
│   ├── pre-deploy.sh          # Run before docker-compose up
│   ├── post-deploy.sh         # Run after docker-compose up
│   ├── backup.sh              # Backup script for target
│   ├── restore.sh             # Restore script
│   └── healthcheck.sh         # Validate deployment
├── migrations/
│   ├── postgres/
│   │   └── schema.sql         # DDL only (no data)
│   ├── redis/
│   │   └── init.lua           # Redis initialization
│   └── rabbitmq/
│       └── definitions.json   # Queue/exchange definitions
├── data-sync/
│   ├── sync-manifest.json     # What to sync, order, dependencies
│   ├── postgres-sync.sh       # DB sync script (pg_dump | pg_restore)
│   ├── s3-to-minio.sh         # Storage sync script
│   └── redis-sync.sh          # Cache warming script
├── secrets/
│   ├── .env.template          # Environment variables (PLACEHOLDERS ONLY)
│   ├── secrets-manifest.json  # Secret references (names, sources) - NO VALUES
│   └── README.md              # Instructions for providing secrets
├── dns/
│   ├── records.json           # DNS records to create/update
│   └── cutover.json           # DNS cutover plan
├── validation/
│   ├── endpoints.json         # Endpoints to healthcheck
│   ├── expected-responses.json # Expected responses for validation
│   └── rollback-triggers.json # Conditions that trigger rollback
└── README.md                  # Human-readable migration guide
```

### Manifest Schema

```json
{
  "version": "1.0.0",
  "format": "hprt",
  "created": "2024-01-15T10:30:00Z",
  "homeport_version": "0.5.0",

  "source": {
    "provider": "aws",
    "region": "us-east-1",
    "account_id": "123456789012",
    "resource_count": 24,
    "analyzed_at": "2024-01-15T10:25:00Z"
  },

  "target": {
    "type": "docker-compose",
    "consolidation": true,
    "stack_count": 8
  },

  "stacks": [
    {
      "name": "database",
      "services": ["postgres", "pgbouncer"],
      "resources_consolidated": 3,
      "data_sync_required": true,
      "estimated_sync_size": "50GB"
    }
  ],

  "checksums": {
    "compose/docker-compose.yml": "sha256:abc123...",
    "configs/nginx/nginx.conf": "sha256:def456..."
  },

  "dependencies": {
    "docker": ">=20.10",
    "docker-compose": ">=2.0"
  },

  "data_sync": {
    "total_estimated_size": "120GB",
    "databases": ["postgres:main", "postgres:analytics"],
    "storage": ["s3:assets", "s3:uploads"],
    "estimated_duration": "2h30m"
  },

  "rollback": {
    "supported": true,
    "snapshot_required": true
  }
}
```

---

## Architecture

### New Packages

```
internal/
├── domain/
│   ├── bundle/                    # Bundle domain types
│   │   ├── bundle.go              # Bundle, Manifest types
│   │   ├── manifest.go            # Manifest schema & validation
│   │   ├── checksum.go            # File checksums
│   │   └── version.go             # Version compatibility
│   │
│   ├── sync/                      # Data sync domain
│   │   ├── sync.go                # SyncPlan, SyncTask types
│   │   ├── strategy.go            # SyncStrategy interface
│   │   └── progress.go            # Progress tracking
│   │
│   └── cutover/                   # Cutover domain
│       ├── cutover.go             # CutoverPlan types
│       ├── dns.go                 # DNS record types
│       └── validation.go          # Health check types
│
├── infrastructure/
│   ├── bundle/                    # Bundle implementation
│   │   ├── exporter.go            # Creates .hprt files
│   │   ├── importer.go            # Extracts .hprt files
│   │   ├── archiver.go            # tar.gz operations
│   │   └── encryptor.go           # SOPS integration for secrets
│   │
│   ├── sync/                      # Data sync implementations
│   │   ├── orchestrator.go        # Coordinates sync tasks
│   │   ├── postgres.go            # PostgreSQL sync (pg_dump/restore)
│   │   ├── mysql.go               # MySQL sync
│   │   ├── redis.go               # Redis sync (RDB/AOF)
│   │   ├── minio.go               # S3→MinIO sync (rclone/mc)
│   │   └── rabbitmq.go            # RabbitMQ definitions sync
│   │
│   ├── deploy/                    # Deployment implementations
│   │   ├── deployer.go            # Deployment orchestrator
│   │   ├── compose.go             # docker-compose deployment
│   │   ├── ssh.go                 # Remote SSH execution
│   │   └── healthcheck.go         # Health check runner
│   │
│   └── cutover/                   # Cutover implementations
│       ├── orchestrator.go        # Cutover orchestrator
│       ├── dns/
│       │   ├── cloudflare.go      # Cloudflare DNS
│       │   ├── route53.go         # AWS Route53
│       │   └── manual.go          # Manual instructions
│       └── validator.go           # Post-cutover validation
│
├── app/
│   ├── bundle/
│   │   └── service.go             # Bundle application service
│   ├── sync/
│   │   └── service.go             # Sync application service
│   ├── deploy/
│   │   └── service.go             # Deploy application service (extend existing)
│   └── cutover/
│       └── service.go             # Cutover application service
│
├── cli/
│   ├── export.go                  # homeport export command
│   ├── import.go                  # homeport import command (already exists, extend)
│   ├── sync.go                    # homeport sync command
│   └── cutover.go                 # homeport cutover command
│
└── api/handlers/
    ├── bundle.go                  # Bundle API endpoints
    ├── sync.go                    # Sync API endpoints (extend)
    └── cutover.go                 # Cutover API endpoints
```

### Web UI Components

```
web/src/
├── components/
│   └── MigrationWizard/
│       ├── WizardContainer.tsx    # Main wizard container
│       ├── WizardProgress.tsx     # Step progress indicator
│       ├── steps/
│       │   ├── AnalyzeStep.tsx    # Step 1: Source analysis
│       │   ├── ExportStep.tsx     # Step 2: Bundle creation
│       │   ├── DeployStep.tsx     # Step 3: Target deployment
│       │   ├── SyncStep.tsx       # Step 4: Data synchronization
│       │   └── CutoverStep.tsx    # Step 5: DNS cutover
│       ├── BundlePreview.tsx      # Preview bundle contents
│       ├── SyncProgress.tsx       # Real-time sync progress
│       └── ValidationResults.tsx  # Health check results
│
├── pages/
│   └── Wizard.tsx                 # Full-page wizard experience
│
├── lib/
│   ├── bundle-api.ts              # Bundle API client (upload, download, export)
│   ├── secrets-api.ts             # Secrets resolution API (already exists, extend)
│   ├── wizard-api.ts              # Wizard session state API
│   └── websocket.ts               # Real-time progress updates (sync, deploy)
│
└── stores/
    └── wizard.ts                  # Wizard state management
```

---

## Sprint Breakdown (12 Sprints)

### Sprint 1: Bundle Domain Types

Create `internal/domain/bundle/` with core types.

**Files:**
- `bundle.go` - Bundle, BundleFile, BundleMetadata structs
- `manifest.go` - Manifest struct, validation, JSON schema
- `checksum.go` - ComputeChecksum, VerifyChecksums functions
- `version.go` - VersionConstraint, CheckCompatibility

**Key Types:**
```go
type Bundle struct {
    Manifest    *Manifest
    Files       map[string]*BundleFile
    CreatedAt   time.Time
}

type Manifest struct {
    Version         string           `json:"version"`
    Format          string           `json:"format"`
    Created         time.Time        `json:"created"`
    HomeportVersion string           `json:"homeport_version"`
    Source          *SourceInfo      `json:"source"`
    Target          *TargetInfo      `json:"target"`
    Stacks          []*StackInfo     `json:"stacks"`
    Checksums       map[string]string `json:"checksums"`
    DataSync        *DataSyncInfo    `json:"data_sync"`
}
```

---

### Sprint 2: Bundle Exporter

Create `internal/infrastructure/bundle/` with export functionality.

**Files:**
- `exporter.go` - Exporter struct, Export() method
- `archiver.go` - CreateArchive, AddFile, Compress functions
- `collector.go` - Collect files from generator output

**Flow:**
```
MigrationResult + ConsolidatedStacks → Exporter → .hprt file
```

---

### Sprint 3: Bundle Importer

Add import functionality to extract and validate bundles.

**Files:**
- `importer.go` - Importer struct, Import() method
- `validator.go` - ValidateBundle, CheckDependencies
- `extractor.go` - ExtractArchive, VerifyChecksums

---

### Sprint 4: Export CLI Command

Create `homeport export` command.

**File:** `internal/cli/export.go`

**Usage:**
```bash
# Basic export
homeport export -o migration.hprt

# With source analysis
homeport export --source ./terraform --output migration.hprt

# With consolidation
homeport export --source ./infra.tf --consolidate --output migration.hprt

# Detect secrets and create references (NO values stored)
homeport export --source ./infra.tf --detect-secrets -o migration.hprt
```

**Note:** Secrets are NEVER stored in the bundle. The `--detect-secrets` flag scans for secret references (env vars, secret manager ARNs) and creates `secrets-manifest.json` with references only.

---

### Sprint 5: Import CLI Command (Extend)

Extend existing import to handle .hprt bundles.

**File:** `internal/cli/import.go` (extend)

**Usage:**
```bash
# Import bundle locally (prompts for secrets interactively)
homeport import migration.hprt

# Import to remote target via SSH
homeport import migration.hprt --target user@192.168.1.100

# Import and deploy immediately
homeport import migration.hprt --deploy

# Provide secrets via file
homeport import migration.hprt --secrets-file .env.production

# Pull secrets from source cloud (requires credentials)
homeport import migration.hprt --pull-secrets-from aws

# Dry run
homeport import migration.hprt --dry-run
```

**Secrets Resolution Order:**
1. `--secrets-file` - Load from provided file
2. `--pull-secrets-from` - Pull from cloud provider
3. Environment variables (`HOMEPORT_SECRET_*`)
4. Interactive prompt for any remaining required secrets

---

### Sprint 6: Sync Domain & Orchestrator

Create data sync framework.

**Files:**
- `internal/domain/sync/sync.go` - SyncPlan, SyncTask, SyncStatus
- `internal/domain/sync/strategy.go` - SyncStrategy interface
- `internal/domain/sync/progress.go` - Progress, ETA calculation
- `internal/infrastructure/sync/orchestrator.go` - Coordinates sync

**Key Types:**
```go
type SyncPlan struct {
    Tasks       []*SyncTask
    TotalSize   int64
    Parallelism int
    Order       []string  // Dependency order
}

type SyncTask struct {
    ID          string
    Type        SyncType  // database, storage, cache
    Source      *Endpoint
    Target      *Endpoint
    Strategy    SyncStrategy
    Status      SyncStatus
    Progress    *Progress
}

type SyncStrategy interface {
    Name() string
    EstimateSize(ctx context.Context, source *Endpoint) (int64, error)
    Sync(ctx context.Context, source, target *Endpoint, progress chan<- *Progress) error
    Verify(ctx context.Context, source, target *Endpoint) (*VerifyResult, error)
}
```

---

### Sprint 7: Database Sync Strategies

Implement database synchronization.

**Files:**
- `internal/infrastructure/sync/postgres.go` - pg_dump/pg_restore wrapper
- `internal/infrastructure/sync/mysql.go` - mysqldump wrapper
- `internal/infrastructure/sync/redis.go` - Redis sync (BGSAVE + restore)

**PostgreSQL Strategy:**
```go
type PostgresSync struct{}

func (p *PostgresSync) Sync(ctx, source, target, progress) error {
    // 1. Create target database
    // 2. pg_dump --format=custom from source
    // 3. Stream to pg_restore on target
    // 4. Verify row counts
}
```

---

### Sprint 8: Storage Sync Strategies

Implement storage synchronization.

**Files:**
- `internal/infrastructure/sync/minio.go` - S3/GCS/Blob → MinIO sync
- `internal/infrastructure/sync/rclone.go` - rclone wrapper for cross-cloud

**MinIO Strategy:**
```go
type MinIOSync struct{}

func (m *MinIOSync) Sync(ctx, source, target, progress) error {
    // 1. List source bucket
    // 2. Parallel copy with mc mirror or rclone
    // 3. Verify object counts and checksums
}
```

---

### Sprint 9: Sync CLI Command

Create `homeport sync` command.

**File:** `internal/cli/sync.go`

**Usage:**
```bash
# Sync all data defined in bundle
homeport sync --bundle migration.hprt

# Sync specific resource types
homeport sync --bundle migration.hprt --type database

# Sync with source/target override
homeport sync \
  --source "postgres://aws-rds:5432/mydb" \
  --target "postgres://localhost:5432/mydb"

# Continuous sync (CDC mode for databases)
homeport sync --bundle migration.hprt --continuous

# Verify only (no sync)
homeport sync --bundle migration.hprt --verify-only
```

---

### Sprint 10: Cutover Domain & Orchestrator

Create cutover/DNS management.

**Files:**
- `internal/domain/cutover/cutover.go` - CutoverPlan, CutoverStep
- `internal/domain/cutover/dns.go` - DNSRecord, DNSProvider
- `internal/domain/cutover/validation.go` - HealthCheck, ValidationRule
- `internal/infrastructure/cutover/orchestrator.go` - Coordinates cutover

**Key Types:**
```go
type CutoverPlan struct {
    PreChecks       []*HealthCheck
    DNSChanges      []*DNSChange
    PostChecks      []*HealthCheck
    RollbackTriggers []*RollbackTrigger
    Timeout         time.Duration
}

type DNSChange struct {
    Domain      string
    RecordType  string  // A, CNAME, etc.
    OldValue    string
    NewValue    string
    TTL         int
}
```

---

### Sprint 11: Cutover CLI Command

Create `homeport cutover` command.

**File:** `internal/cli/cutover.go`

**Usage:**
```bash
# Execute cutover plan from bundle
homeport cutover --bundle migration.hprt

# Dry run (show what would change)
homeport cutover --bundle migration.hprt --dry-run

# With DNS provider
homeport cutover --bundle migration.hprt --dns-provider cloudflare

# Manual mode (just print instructions)
homeport cutover --bundle migration.hprt --manual

# Rollback
homeport cutover --bundle migration.hprt --rollback
```

---

### Sprint 12: Wizard CLI Command

Create unified wizard experience.

**File:** `internal/cli/wizard.go`

**Usage:**
```bash
# Interactive wizard
homeport wizard

# Wizard with source
homeport wizard --source ./terraform

# Non-interactive (use defaults)
homeport wizard --source ./terraform --target 192.168.1.100 --yes
```

**Wizard Flow:**
```
┌─────────────────────────────────────────────────────────────────┐
│  HOMEPORT MIGRATION WIZARD                                       │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  Step 1/5: Analyze Source                                        │
│  ─────────────────────────────────────────────────────────────   │
│                                                                  │
│  ? Select source type:                                           │
│    > Terraform files (*.tf)                                      │
│      CloudFormation (*.yaml)                                     │
│      Terraform state (*.tfstate)                                 │
│      AWS API (live)                                              │
│                                                                  │
│  ? Source path: ./infrastructure                                 │
│                                                                  │
│  Analyzing... ████████████████████ 100%                          │
│                                                                  │
│  Found 24 resources:                                             │
│    • 3 EC2 instances                                             │
│    • 2 RDS databases                                             │
│    • 5 Lambda functions                                          │
│    • 4 S3 buckets                                                │
│    • ...                                                         │
│                                                                  │
│  [Continue] [Back] [Cancel]                                      │
└─────────────────────────────────────────────────────────────────┘
```

---

### Sprint 13: Web UI - Wizard Container

Create wizard UI framework.

**Files:**
- `web/src/pages/Wizard.tsx` - Full-page wizard
- `web/src/components/MigrationWizard/WizardContainer.tsx`
- `web/src/components/MigrationWizard/WizardProgress.tsx`
- `web/src/stores/wizard.ts` - Wizard state

---

### Sprint 14: Web UI - Wizard Steps

Implement each wizard step.

**Files:**
- `web/src/components/MigrationWizard/steps/AnalyzeStep.tsx`
- `web/src/components/MigrationWizard/steps/ExportStep.tsx`
- `web/src/components/MigrationWizard/steps/SecretsStep.tsx` - NEW
- `web/src/components/MigrationWizard/steps/DeployStep.tsx`
- `web/src/components/MigrationWizard/steps/SyncStep.tsx`
- `web/src/components/MigrationWizard/steps/CutoverStep.tsx`
- `web/src/components/BundleUploader.tsx` - Drag & drop .hprt upload

**Web UI Bundle Flow:**
```
┌─────────────────────────────────────────────────────────────────────────┐
│  TWO ENTRY POINTS                                                        │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  OPTION A: Start from source                                             │
│  ┌──────────┐    ┌──────────┐    ┌──────────┐                           │
│  │ Analyze  │ →  │ Export   │ →  │ Download │  (save .hprt locally)     │
│  │ Source   │    │ Bundle   │    │ .hprt    │                           │
│  └──────────┘    └──────────┘    └──────────┘                           │
│       ↓                               │                                  │
│       └───────────────────────────────┼──────────────────┐              │
│                                       ↓                  ↓              │
│  OPTION B: Start from bundle     ┌──────────┐    ┌──────────┐          │
│  ┌──────────┐                    │ Provide  │ →  │ Deploy   │          │
│  │ Upload   │ ──────────────────→│ Secrets  │    │ + Sync   │          │
│  │ .hprt    │                    │ (form)   │    │          │          │
│  └──────────┘                    └──────────┘    └──────────┘          │
│                                                        ↓                │
│                                                  ┌──────────┐          │
│                                                  │ Cutover  │          │
│                                                  └──────────┘          │
└─────────────────────────────────────────────────────────────────────────┘
```

**ExportStep.tsx features:**
- Preview bundle contents before download
- Show what's included (configs, scripts, migrations)
- Show what's NOT included (secrets - references only)
- Download button → saves .hprt to user's machine

**SecretsStep.tsx features:**
- Lists all required secrets from bundle manifest
- Form inputs for each secret (password fields, masked)
- Option to pull from source cloud (AWS/GCP/Azure)
- Secrets sent to server, stored in memory only, never persisted
- Validation before proceeding

---

### Sprint 15: Web UI - Real-time Progress

Add WebSocket support for live updates.

**Files:**
- `web/src/lib/websocket.ts` - WebSocket client
- `web/src/components/MigrationWizard/SyncProgress.tsx`
- `internal/api/handlers/websocket.go` - WebSocket handler
- `internal/api/handlers/wizard.go` - Wizard session API

---

### Sprint 16: API Endpoints

Add REST API for all wizard operations.

**Files:**
- `internal/api/handlers/bundle.go`
  - `POST /api/bundle/export` - Create bundle from analyzed resources
  - `POST /api/bundle/upload` - Upload .hprt file from user's machine
  - `POST /api/bundle/import` - Import uploaded bundle to target
  - `GET /api/bundle/{id}` - Get bundle info (manifest, checksums)
  - `GET /api/bundle/{id}/download` - Download .hprt file to user's machine
  - `GET /api/bundle/{id}/secrets` - List required secrets (references only)
  - `POST /api/bundle/{id}/secrets` - Provide secret values for deployment

- `internal/api/handlers/sync.go` (extend)
  - `POST /api/sync/start` - Start sync
  - `GET /api/sync/{id}/status` - Get sync status
  - `POST /api/sync/{id}/pause` - Pause sync
  - `POST /api/sync/{id}/resume` - Resume sync

- `internal/api/handlers/cutover.go`
  - `POST /api/cutover/plan` - Create cutover plan
  - `POST /api/cutover/execute` - Execute cutover
  - `POST /api/cutover/rollback` - Rollback cutover

---

### Sprint 17: Secrets Handling (NO secrets in bundle)

**CRITICAL: Bundle NEVER contains secret values.**

**Strategy:** Reference-only approach

**Files:**
- `internal/domain/secrets/reference.go` - SecretReference, SecretSource types
- `internal/infrastructure/secrets/resolver.go` - Resolve references at deploy time
- `internal/infrastructure/secrets/providers/` - Secret source providers

**secrets-manifest.json example:**
```json
{
  "secrets": [
    {
      "name": "DATABASE_PASSWORD",
      "source": "aws-secrets-manager",
      "key": "prod/myapp/db-password",
      "required": true
    },
    {
      "name": "API_KEY",
      "source": "manual",
      "description": "Third-party API key",
      "required": true
    }
  ]
}
```

**At import time, secrets are resolved via:**
1. **Manual entry** - Wizard prompts user for each secret
2. **Environment variables** - `HOMEPORT_SECRET_DATABASE_PASSWORD=xxx`
3. **Secret manager pull** - Pull from AWS/GCP/Azure (with credentials)
4. **Vault push** - Forward to target Vault instance
5. **File reference** - `--secrets-file .env.production`

**Secret Sources Supported:**
- `manual` - User provides at import time
- `env` - From environment variable
- `file` - From local file
- `aws-secrets-manager` - Pull from AWS (requires credentials)
- `gcp-secret-manager` - Pull from GCP
- `azure-key-vault` - Pull from Azure
- `hashicorp-vault` - Pull from existing Vault

**Flow:**
```
Export: Detect secrets → Store REFERENCES only → Generate .env.template
Import: Read references → Prompt/pull values → Inject into target
```

**NEVER stored in bundle:**
- Passwords
- API keys
- Certificates/private keys
- Connection strings with credentials
- Any sensitive values

---

### Sprint 18: Testing & Documentation

Comprehensive testing and docs.

**Files:**
- `internal/domain/bundle/bundle_test.go`
- `internal/infrastructure/bundle/exporter_test.go`
- `internal/infrastructure/sync/postgres_test.go`
- `test/e2e/wizard_test.go`
- `docs/migration-wizard.md`
- `docs/hprt-format.md`

---

## CLI Command Summary

After implementation:

```bash
# Full wizard experience
homeport wizard

# Individual commands
homeport export --source ./terraform -o migration.hprt
homeport import migration.hprt --target user@server
homeport sync --bundle migration.hprt
homeport cutover --bundle migration.hprt

# Shortcuts
homeport migrate --source ./terraform --target user@server  # All-in-one
```

---

## Success Metrics

| Metric | Target |
|--------|--------|
| Commands to migrate | ≤ 3 (wizard, or export→import→cutover) |
| User decisions required | ≤ 5 (source, target, DNS provider, confirm) |
| Bundle size | < 5MB for typical migration |
| Sync reliability | 99.9% (with checksums, retries) |
| Rollback time | < 5 minutes |

---

## Dependencies

**Required:**
- `docker` >= 20.10
- `docker-compose` >= 2.0

**Optional (for sync):**
- `pg_dump` / `pg_restore` - PostgreSQL sync
- `mysqldump` - MySQL sync
- `rclone` or `mc` - Storage sync
- `redis-cli` - Redis sync

**For Secrets (at import time only):**
- AWS CLI - For pulling from AWS Secrets Manager
- gcloud CLI - For pulling from GCP Secret Manager
- az CLI - For pulling from Azure Key Vault

---

## Risk Mitigation

| Risk | Mitigation |
|------|------------|
| Large data sync fails | Resumable sync, chunked transfers, checksums |
| DNS propagation delay | Low TTL pre-cutover, health checks |
| Target unreachable | SSH connection validation before sync |
| Secrets exposure | **Never store secrets in bundle** - references only, resolve at import |
| Rollback needed | Automatic snapshots, tested rollback scripts |

---

## Timeline

| Sprint | Focus | Dependency | Status |
|--------|-------|------------|--------|
| 1 | Bundle Domain Types | None | ✅ Done |
| 2 | Bundle Exporter | Sprint 1 | ✅ Done |
| 3 | Bundle Importer | Sprint 1 | ✅ Done |
| 4-5 | CLI export/import | Sprints 1-3 | ✅ Done |
| 6 | Sync Domain | Sprints 1-3 | ✅ Done |
| 7 | Database Sync | Sprint 6 | ✅ Done |
| 8 | Storage Sync | Sprint 6 | ✅ Done |
| 9 | Sync CLI | Sprints 6-8 | ✅ Done |
| 10 | Cutover Domain | Sprints 1-3 | ✅ Done |
| 11 | Cutover CLI | Sprint 10 | ✅ Done |
| 12 | Wizard CLI | All previous | ✅ Done |
| 13-14 | Web UI Wizard | All previous | ✅ Done |
| 15 | Web UI Real-time | Sprints 13-14 | ✅ Done |
| 16 | API endpoints | Sprints 1-11 | ✅ Done |
| 17 | Secrets Handling | Sprints 1-3 | ✅ Done |
| 18 | Testing & docs | All | ✅ Done |

**Progress: 18/18 sprints completed** - All implementation complete!

### Completed CLI Commands

| Command | File | Description |
|---------|------|-------------|
| `homeport export` | `internal/cli/export.go` | Export migration as .hprt bundle |
| `homeport import bundle` | `internal/cli/import.go` | Import and deploy .hprt bundle |
| `homeport sync` | `internal/cli/sync.go` | Synchronize data (database, storage, cache) |
| `homeport cutover` | `internal/cli/cutover.go` | DNS cutover and validation |
| `homeport wizard` | `internal/cli/wizard.go` | Interactive 5-step migration wizard |
