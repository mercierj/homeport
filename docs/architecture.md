# AgnosTech Architecture

This document describes the technical architecture of AgnosTech, including its design principles, component structure, and data flow.

## Overview

AgnosTech follows a **clean hexagonal architecture** (ports and adapters) with clear separation between domain logic, application services, and infrastructure implementations.

```
┌─────────────────────────────────────────────────────────────────────┐
│                              CLI Layer                               │
│                         (cmd/agnostech, internal/cli)                │
└─────────────────────────────────┬───────────────────────────────────┘
                                  │
┌─────────────────────────────────▼───────────────────────────────────┐
│                         Application Layer                            │
│                           (internal/app)                             │
└─────────────────────────────────┬───────────────────────────────────┘
                                  │
┌─────────────────────────────────▼───────────────────────────────────┐
│                          Domain Layer                                │
│                         (internal/domain)                            │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌────────────┐  │
│  │   Mapper    │  │   Parser    │  │  Generator  │  │  Resource  │  │
│  │  Interface  │  │  Interface  │  │  Interface  │  │   Types    │  │
│  └─────────────┘  └─────────────┘  └─────────────┘  └────────────┘  │
└─────────────────────────────────┬───────────────────────────────────┘
                                  │
┌─────────────────────────────────▼───────────────────────────────────┐
│                       Infrastructure Layer                           │
│                      (internal/infrastructure)                       │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │                         Parsers                              │    │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐     │    │
│  │  │ Terraform│  │   HCL    │  │   AWS    │  │GCP/Azure │     │    │
│  │  │  State   │  │  Parser  │  │  Parser  │  │ Parsers  │     │    │
│  │  └──────────┘  └──────────┘  └──────────┘  └──────────┘     │    │
│  └─────────────────────────────────────────────────────────────┘    │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │                         Mappers                              │    │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐     │    │
│  │  │   AWS    │  │   GCP    │  │  Azure   │  │ Registry │     │    │
│  │  │ Mappers  │  │ Mappers  │  │ Mappers  │  │          │     │    │
│  │  └──────────┘  └──────────┘  └──────────┘  └──────────┘     │    │
│  └─────────────────────────────────────────────────────────────┘    │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │                        Generators                            │    │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐     │    │
│  │  │ Compose  │  │ Traefik  │  │ Scripts  │  │   Docs   │     │    │
│  │  └──────────┘  └──────────┘  └──────────┘  └──────────┘     │    │
│  └─────────────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────────┘
```

## Directory Structure

```
agnostech/
├── cmd/agnostech/              # CLI entry point
│   └── main.go                 # Application bootstrap
│
├── internal/                   # Private application code
│   ├── app/                    # Application services (orchestration)
│   │   └── migrator.go         # Migration orchestration service
│   │
│   ├── cli/                    # CLI command implementations
│   │   ├── root.go             # Root command configuration
│   │   ├── analyze.go          # Analyze command
│   │   ├── migrate.go          # Migrate command
│   │   ├── validate.go         # Validate command
│   │   ├── version.go          # Version command
│   │   └── ui/                 # Terminal UI components
│   │
│   ├── domain/                 # Core domain models
│   │   ├── mapper/             # Mapper interface & types
│   │   │   ├── mapper.go       # Mapper interface definition
│   │   │   ├── result.go       # MappingResult type
│   │   │   └── errors.go       # Domain errors
│   │   ├── parser/             # Parser interface
│   │   ├── generator/          # Generator interface
│   │   ├── resource/           # Resource type definitions
│   │   │   ├── types.go        # AWS/GCP/Azure resource types
│   │   │   └── resource.go     # Resource model
│   │   └── target/             # Deployment target models
│   │
│   └── infrastructure/         # Implementation layer
│       ├── parser/             # Terraform/HCL parsers
│       │   ├── terraform.go    # Main parser orchestration
│       │   ├── tfstate.go      # State file parser
│       │   ├── hcl.go          # HCL file parser
│       │   ├── aws/            # AWS-specific parser
│       │   ├── gcp/            # GCP-specific parser
│       │   └── azure/          # Azure-specific parser
│       │
│       ├── mapper/             # Cloud -> Docker mappers
│       │   ├── registry.go     # Central mapper registry
│       │   ├── compute/        # EC2, Lambda, ECS, EKS
│       │   ├── storage/        # S3, EBS, EFS
│       │   ├── database/       # RDS, DynamoDB, ElastiCache
│       │   ├── networking/     # ALB, API Gateway, etc.
│       │   ├── security/       # Cognito, IAM, ACM
│       │   ├── messaging/      # SQS, SNS, EventBridge
│       │   ├── gcp/            # GCP mappers
│       │   │   ├── registry.go
│       │   │   ├── compute/
│       │   │   ├── storage/
│       │   │   ├── database/
│       │   │   ├── networking/
│       │   │   ├── messaging/
│       │   │   └── security/
│       │   └── azure/          # Azure mappers
│       │       ├── registry.go
│       │       ├── compute/
│       │       ├── storage/
│       │       ├── database/
│       │       ├── networking/
│       │       ├── messaging/
│       │       └── security/
│       │
│       └── generator/          # Output generators
│           ├── compose/        # Docker Compose YAML
│           ├── traefik/        # Traefik configuration
│           ├── scripts/        # Migration scripts
│           └── docs/           # Documentation
│
├── pkg/                        # Public packages
│   └── version/                # Version information
│
├── templates/                  # Go templates (embedded)
│   ├── compose/                # Docker Compose templates
│   ├── traefik/                # Traefik config templates
│   ├── scripts/                # Script templates
│   └── docs/                   # Doc templates
│
└── test/                       # Tests
    ├── fixtures/               # Sample Terraform files
    ├── integration/            # Integration tests
    └── e2e/                    # End-to-end tests
```

## Core Components

### 1. Domain Layer

The domain layer defines the core interfaces and models that all implementations must follow.

#### Mapper Interface

```go
type Mapper interface {
    // ResourceType returns the cloud resource type this mapper handles
    ResourceType() resource.Type

    // Map converts a cloud resource to Docker equivalent
    Map(ctx context.Context, res *resource.AWSResource) (*MappingResult, error)

    // Validate checks if the resource can be mapped
    Validate(res *resource.AWSResource) error

    // Dependencies returns resource types this mapper depends on
    Dependencies() []resource.Type
}
```

#### MappingResult

```go
type MappingResult struct {
    DockerService *DockerService      // Container definition
    Configs       map[string][]byte   // Configuration files
    Scripts       map[string][]byte   // Migration/setup scripts
    Warnings      []string            // Migration warnings
    ManualSteps   []string            // Required manual actions
}
```

#### Resource Types

```go
// AWS resource types
const (
    TypeAWSInstance    Type = "aws_instance"
    TypeAWSS3Bucket    Type = "aws_s3_bucket"
    TypeAWSDBInstance  Type = "aws_db_instance"
    // ... 50+ AWS types
)

// GCP resource types
const (
    TypeGCEInstance    Type = "google_compute_instance"
    TypeGCSBucket      Type = "google_storage_bucket"
    TypeCloudSQL       Type = "google_sql_database_instance"
    // ... GCP types
)

// Azure resource types
const (
    TypeAzureVM        Type = "azurerm_linux_virtual_machine"
    TypeAzureBlob      Type = "azurerm_storage_blob"
    TypeAzurePostgres  Type = "azurerm_postgresql_flexible_server"
    // ... Azure types
)
```

### 2. Parser System

The parser system handles reading and interpreting Terraform configurations.

```
┌────────────────────┐
│  Terraform Input   │
│  *.tfstate / *.tf  │
└─────────┬──────────┘
          │
          ▼
┌────────────────────┐     ┌────────────────────┐
│   State Parser     │────▶│   HCL Parser       │
│   (tfstate.go)     │     │   (hcl.go)         │
└─────────┬──────────┘     └─────────┬──────────┘
          │                          │
          ▼                          ▼
┌─────────────────────────────────────────────────┐
│              Infrastructure Model                │
│  ┌─────────────────────────────────────────┐    │
│  │ Resources []Resource                     │    │
│  │ Variables map[string]Variable            │    │
│  │ Outputs   map[string]Output              │    │
│  │ DependencyGraph                          │    │
│  └─────────────────────────────────────────┘    │
└─────────────────────────────────────────────────┘
```

#### Supported Input Formats

1. **Terraform State (v3/v4)** - JSON format with deployed resource state
2. **HCL Files** - Terraform configuration files (`.tf`)
3. **Combined** - State + HCL for complete context

### 3. Mapper Registry

The registry is a central dispatcher that routes resources to appropriate mappers.

```go
type Registry struct {
    mappers map[resource.Type]Mapper
}

func (r *Registry) Register(m Mapper) {
    r.mappers[m.ResourceType()] = m
}

func (r *Registry) Map(ctx context.Context, res *resource.AWSResource) (*MappingResult, error) {
    mapper, exists := r.mappers[res.Type]
    if !exists {
        return nil, ErrUnsupportedResource
    }
    return mapper.Map(ctx, res)
}
```

#### Multi-Cloud Registry Structure

```
Registry (main)
├── AWS Mappers (registered directly)
│   ├── S3Mapper
│   ├── RDSMapper
│   └── ...
│
├── GCP Registry (RegisterAll)
│   ├── GCSMapper
│   ├── CloudSQLMapper
│   └── ...
│
└── Azure Registry (RegisterAll)
    ├── BlobMapper
    ├── PostgresMapper
    └── ...
```

### 4. Generator System

Generators transform mapping results into deployable artifacts.

```
┌─────────────────────┐
│   MappingResults    │
│   []MappingResult   │
└─────────┬───────────┘
          │
          ▼
┌─────────────────────────────────────────────────────────────┐
│                      Generators                              │
│                                                              │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐       │
│  │   Compose    │  │   Traefik    │  │   Scripts    │       │
│  │  Generator   │  │  Generator   │  │  Generator   │       │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘       │
│         │                 │                 │                │
└─────────┼─────────────────┼─────────────────┼────────────────┘
          │                 │                 │
          ▼                 ▼                 ▼
┌─────────────────────────────────────────────────────────────┐
│                    Output Directory                          │
│                                                              │
│  docker-compose.yml    traefik/         scripts/            │
│  .env.example          ├── traefik.yml  ├── migrate-s3.sh   │
│                        └── dynamic/     └── backup.sh       │
└─────────────────────────────────────────────────────────────┘
```

## Data Flow

### Complete Migration Flow

```
1. INPUT PHASE
   ┌─────────────────────────────────────────────────────────┐
   │  User runs: agnostech migrate ./terraform -o ./output   │
   └─────────────────────────────────┬───────────────────────┘
                                     │
2. PARSING PHASE                     ▼
   ┌─────────────────────────────────────────────────────────┐
   │  Parser reads Terraform files                           │
   │  - Detects .tfstate and .tf files                       │
   │  - Parses resource definitions                          │
   │  - Builds dependency graph                              │
   └─────────────────────────────────┬───────────────────────┘
                                     │
3. ANALYSIS PHASE                    ▼
   ┌─────────────────────────────────────────────────────────┐
   │  Analyzer categorizes resources                         │
   │  - Groups by provider (AWS/GCP/Azure)                   │
   │  - Identifies supported/unsupported                     │
   │  - Calculates dependencies                              │
   └─────────────────────────────────┬───────────────────────┘
                                     │
4. MAPPING PHASE                     ▼
   ┌─────────────────────────────────────────────────────────┐
   │  Registry routes to appropriate mappers                 │
   │  - Each mapper converts resource to Docker service      │
   │  - Generates configs, scripts, warnings                 │
   │  - Respects dependency ordering                         │
   └─────────────────────────────────┬───────────────────────┘
                                     │
5. GENERATION PHASE                  ▼
   ┌─────────────────────────────────────────────────────────┐
   │  Generators produce output files                        │
   │  - Docker Compose with all services                     │
   │  - Traefik reverse proxy config                         │
   │  - Migration scripts                                    │
   │  - Documentation                                        │
   └─────────────────────────────────┬───────────────────────┘
                                     │
6. OUTPUT PHASE                      ▼
   ┌─────────────────────────────────────────────────────────┐
   │  Files written to output directory                      │
   │  - Validation performed                                 │
   │  - Summary displayed to user                            │
   └─────────────────────────────────────────────────────────┘
```

## Extension Points

### Adding a New Cloud Provider

1. Define resource types in `internal/domain/resource/types.go`
2. Create parser in `internal/infrastructure/parser/<provider>/`
3. Create mappers in `internal/infrastructure/mapper/<provider>/`
4. Create registry in `internal/infrastructure/mapper/<provider>/registry.go`
5. Register in main registry

### Adding a New Output Format

1. Create generator in `internal/infrastructure/generator/<format>/`
2. Implement generator interface
3. Add CLI flag for format selection
4. Create templates in `templates/<format>/`

### Adding a New Service Mapper

1. Create mapper file in appropriate category directory
2. Implement `Mapper` interface
3. Register in appropriate registry
4. Add tests
5. Update documentation

## Design Decisions

### Why Hexagonal Architecture?

- **Testability**: Domain logic is isolated and easily testable
- **Flexibility**: Swap implementations without changing core logic
- **Maintainability**: Clear boundaries between components
- **Extensibility**: Easy to add new providers, mappers, generators

### Why Single Registry Pattern?

- **Simplicity**: One place to find all mappers
- **Performance**: O(1) lookup by resource type
- **Extensibility**: Easy to add multi-cloud support
- **Consistency**: Same interface for all providers

### Why Go Templates?

- **Flexibility**: Templates can be customized
- **Readability**: Generated output is human-readable
- **Separation**: Template logic separate from Go code
- **Embedding**: Templates compiled into binary

### Why Docker Compose as Primary Output?

- **Simplicity**: Single command to deploy
- **Portability**: Works on any Docker host
- **Familiarity**: Widely understood format
- **Ecosystem**: Rich tooling support

## Performance Considerations

### Parallel Processing

- Resources are parsed in parallel where possible
- Mapping respects dependency ordering but parallelizes independent resources
- File I/O is batched for efficiency

### Memory Management

- Large state files are streamed, not loaded entirely
- Resources are processed incrementally
- Unused mappers are not initialized

### Caching

- Parsed resources are cached during a run
- Template compilation is cached
- Dependency graph is computed once

## Security Considerations

### Secrets Handling

- Secrets from cloud providers are extracted to `.env.example`
- Actual values replaced with placeholders
- User must provide real values before deployment

### Input Validation

- All user input is validated
- Path traversal attacks are prevented
- Resource attributes are sanitized

### Output Safety

- Generated scripts use safe shell practices
- Passwords are generated with secure defaults
- Docker images use specific tags, not `latest` where possible
