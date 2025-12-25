# Contributing to CloudExit

Thank you for your interest in contributing to CloudExit! This guide will help you get started with development and understand our contribution process.

## Table of Contents

- [Getting Started](#getting-started)
- [Development Setup](#development-setup)
- [Project Structure](#project-structure)
- [Making Changes](#making-changes)
- [Adding a New Mapper](#adding-a-new-mapper)
- [Adding a New Generator](#adding-a-new-generator)
- [Code Style](#code-style)
- [Testing](#testing)
- [Pull Request Process](#pull-request-process)
- [Issue Guidelines](#issue-guidelines)

## Getting Started

### Prerequisites

- **Go 1.23+** - [Download](https://go.dev/dl/)
- **Git** - For version control
- **Make** - Build automation
- **Docker** - For testing generated output (optional)

### Fork and Clone

```bash
# Fork the repository on GitHub, then:
git clone https://github.com/YOUR_USERNAME/cloudexit.git
cd cloudexit
git remote add upstream https://github.com/cloudexit/cloudexit.git
```

## Development Setup

### Install Dependencies

```bash
make deps
```

This runs `go mod download` and `go mod tidy` to fetch all dependencies.

### Build

```bash
make build
```

The binary will be created at `bin/cloudexit`.

### Run Tests

```bash
make test
```

### Run the CLI

```bash
./bin/cloudexit --help
./bin/cloudexit analyze ./test/fixtures/sample.tfstate
```

### Development Mode

```bash
make dev
# or
go run ./cmd/cloudexit --verbose --help
```

## Project Structure

```
cloudexit/
├── cmd/cloudexit/          # CLI entry point
├── internal/
│   ├── app/                # Application services
│   ├── cli/                # CLI commands
│   │   ├── root.go         # Root command
│   │   ├── analyze.go      # Analyze command
│   │   ├── migrate.go      # Migrate command
│   │   └── validate.go     # Validate command
│   ├── domain/             # Core interfaces and types
│   │   ├── mapper/         # Mapper interface
│   │   ├── parser/         # Parser interface
│   │   ├── generator/      # Generator interface
│   │   └── resource/       # Resource types
│   └── infrastructure/     # Implementations
│       ├── parser/         # Terraform parsers
│       ├── mapper/         # Cloud -> Docker mappers
│       │   ├── gcp/        # GCP mappers
│       │   └── azure/      # Azure mappers
│       └── generator/      # Output generators
├── pkg/                    # Public packages
├── templates/              # Go templates
├── test/                   # Tests
│   ├── fixtures/           # Sample files
│   └── integration/        # Integration tests
├── go.mod                  # Go module file
└── Makefile                # Build automation
```

## Making Changes

### Create a Feature Branch

```bash
git checkout -b feature/my-feature
# or
git checkout -b fix/my-bugfix
```

### Branch Naming Convention

- `feature/` - New features
- `fix/` - Bug fixes
- `docs/` - Documentation changes
- `refactor/` - Code refactoring
- `test/` - Test additions/changes

### Commit Messages

Follow conventional commit format:

```
type(scope): description

[optional body]

[optional footer]
```

Examples:
```
feat(mapper): add AWS Kinesis mapper
fix(parser): handle empty tfstate files
docs(readme): update installation instructions
refactor(registry): simplify mapper registration
```

## Adding a New Mapper

This is the most common type of contribution. Here's a step-by-step guide:

### 1. Add Resource Type

Edit `internal/domain/resource/types.go`:

```go
// Add to the appropriate provider section
const (
    // AWS types
    TypeAWSKinesis Type = "aws_kinesis_stream"

    // GCP types
    TypeGCPDataproc Type = "google_dataproc_cluster"

    // Azure types
    TypeAzureEventHub Type = "azurerm_eventhub"
)
```

### 2. Create Mapper File

For AWS, create `internal/infrastructure/mapper/messaging/kinesis.go`:

```go
package messaging

import (
    "context"
    "fmt"
    "time"

    "github.com/cloudexit/cloudexit/internal/domain/mapper"
    "github.com/cloudexit/cloudexit/internal/domain/resource"
)

// KinesisMapper converts AWS Kinesis streams to self-hosted alternatives.
type KinesisMapper struct {
    *mapper.BaseMapper
}

// NewKinesisMapper creates a new Kinesis mapper.
func NewKinesisMapper() *KinesisMapper {
    return &KinesisMapper{
        BaseMapper: mapper.NewBaseMapper(resource.TypeAWSKinesis, nil),
    }
}

// Map converts a Kinesis stream to a Kafka/Redis service.
func (m *KinesisMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
    if err := m.Validate(res); err != nil {
        return nil, err
    }

    streamName := res.GetConfigString("name")
    if streamName == "" {
        streamName = res.Name
    }

    result := mapper.NewMappingResult("kafka")
    svc := result.DockerService

    svc.Image = "confluentinc/cp-kafka:latest"
    svc.Ports = []string{"9092:9092"}
    svc.Environment = map[string]string{
        "KAFKA_BROKER_ID": "1",
        // ... more configuration
    }
    svc.Labels = map[string]string{
        "cloudexit.source": "aws_kinesis_stream",
        "cloudexit.stream": streamName,
    }

    // Add warnings for feature gaps
    shardCount := res.GetConfigInt("shard_count")
    if shardCount > 1 {
        result.AddWarning(fmt.Sprintf(
            "Kinesis stream has %d shards. Kafka partitions may need manual tuning.",
            shardCount,
        ))
    }

    return result, nil
}
```

### 3. Register the Mapper

For AWS mappers, edit `internal/infrastructure/mapper/registry.go`:

```go
func NewRegistry() *Registry {
    r := &Registry{
        mappers: make(map[resource.Type]Mapper),
    }

    // ... existing registrations

    // Messaging
    r.Register(messaging.NewSQSMapper())
    r.Register(messaging.NewSNSMapper())
    r.Register(messaging.NewKinesisMapper())  // Add this line

    return r
}
```

For GCP/Azure mappers, edit the respective registry file.

### 4. Write Tests

Create `internal/infrastructure/mapper/messaging/kinesis_test.go`:

```go
package messaging_test

import (
    "context"
    "testing"

    "github.com/cloudexit/cloudexit/internal/domain/resource"
    "github.com/cloudexit/cloudexit/internal/infrastructure/mapper/messaging"
)

func TestKinesisMapper_Map(t *testing.T) {
    mapper := messaging.NewKinesisMapper()

    tests := []struct {
        name     string
        resource *resource.AWSResource
        wantErr  bool
    }{
        {
            name: "basic stream",
            resource: &resource.AWSResource{
                ID:   "kinesis-123",
                Type: resource.TypeAWSKinesis,
                Name: "my-stream",
                Config: map[string]interface{}{
                    "name":        "my-stream",
                    "shard_count": 1,
                },
            },
            wantErr: false,
        },
        // Add more test cases
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result, err := mapper.Map(context.Background(), tt.resource)

            if (err != nil) != tt.wantErr {
                t.Errorf("Map() error = %v, wantErr %v", err, tt.wantErr)
                return
            }

            if !tt.wantErr {
                if result.DockerService.Image == "" {
                    t.Error("Expected Docker image to be set")
                }
            }
        })
    }
}
```

### 5. Update Documentation

Update the relevant service reference in `docs/`:

- `docs/aws-services.md` for AWS mappers
- `docs/gcp-services.md` for GCP mappers
- `docs/azure-services.md` for Azure mappers

## Adding a New Generator

### 1. Create Generator Package

Create `internal/infrastructure/generator/kubernetes/`:

```go
package kubernetes

import (
    "github.com/cloudexit/cloudexit/internal/domain/mapper"
)

type Generator struct {
    // configuration
}

func New() *Generator {
    return &Generator{}
}

func (g *Generator) Generate(results []*mapper.MappingResult) (map[string][]byte, error) {
    files := make(map[string][]byte)

    // Generate Kubernetes manifests
    // ...

    return files, nil
}
```

### 2. Add Templates

Create templates in `templates/kubernetes/`:

```yaml
# templates/kubernetes/deployment.yaml.tmpl
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ .Name }}
spec:
  replicas: {{ .Replicas }}
  # ...
```

### 3. Integrate with CLI

Add format option to migrate command.

## Code Style

### Go Formatting

All code must be formatted with `gofmt`:

```bash
gofmt -w .
```

### Linting

Run the linter before committing:

```bash
go vet ./...
golangci-lint run  # if installed
```

### Naming Conventions

- **Packages**: lowercase, single word when possible
- **Files**: lowercase with underscores (`kinesis_mapper.go`)
- **Types**: PascalCase (`KinesisMapper`)
- **Functions**: PascalCase for exported, camelCase for internal
- **Variables**: camelCase
- **Constants**: PascalCase for exported

### Documentation

- All exported types and functions must have documentation comments
- Comments should be complete sentences
- Start comments with the name of the thing being documented

```go
// KinesisMapper converts AWS Kinesis streams to Kafka clusters.
// It supports basic stream configuration and generates appropriate
// Kafka topic configurations.
type KinesisMapper struct {
    // ...
}
```

### Error Handling

- Wrap errors with context using `fmt.Errorf`
- Use custom error types for domain errors
- Never ignore errors silently

```go
if err != nil {
    return nil, fmt.Errorf("failed to parse resource %s: %w", res.Name, err)
}
```

## Testing

### Running Tests

```bash
# All tests
make test

# Specific package
go test ./internal/infrastructure/mapper/...

# With coverage
go test -cover ./...

# Verbose
go test -v ./...
```

### Test Types

1. **Unit Tests** - Test individual functions/methods
2. **Integration Tests** - Test component interactions
3. **E2E Tests** - Test full CLI workflows

### Test File Naming

- Unit tests: `*_test.go` in the same package
- Integration tests: `test/integration/`
- E2E tests: `test/e2e/`

### Table-Driven Tests

Prefer table-driven tests for multiple cases:

```go
func TestMapper_Map(t *testing.T) {
    tests := []struct {
        name     string
        input    *resource.AWSResource
        want     *mapper.MappingResult
        wantErr  bool
    }{
        // test cases
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // test logic
        })
    }
}
```

## Pull Request Process

### Before Submitting

1. **Tests pass**: `make test`
2. **Code is formatted**: `gofmt -w .`
3. **No lint errors**: `go vet ./...`
4. **Documentation updated**: If adding features
5. **Commits are clean**: Squash WIP commits

### PR Template

```markdown
## Description
Brief description of changes

## Type of Change
- [ ] Bug fix
- [ ] New feature
- [ ] Breaking change
- [ ] Documentation update

## Testing
How was this tested?

## Checklist
- [ ] Tests added/updated
- [ ] Documentation updated
- [ ] Code follows style guidelines
```

### Review Process

1. Maintainer reviews code
2. CI checks must pass
3. At least one approval required
4. Squash and merge

## Issue Guidelines

### Bug Reports

Include:
- CloudExit version
- Go version
- OS/platform
- Steps to reproduce
- Expected vs actual behavior
- Relevant logs/errors

### Feature Requests

Include:
- Use case description
- Proposed solution
- Alternative solutions considered
- Willingness to contribute

### Good First Issues

Look for issues labeled `good first issue` if you're new to the project.

## Community

### Code of Conduct

Be respectful and inclusive. We follow the [Contributor Covenant](https://www.contributor-covenant.org/).

### Getting Help

- Open an issue for bugs/features
- Use discussions for questions
- Check existing issues before creating new ones

---

Thank you for contributing to CloudExit!
