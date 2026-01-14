# Contributing to Homeport

Thank you for your interest in contributing to Homeport! This document provides guidelines and information for contributors.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [Development Setup](#development-setup)
- [How to Contribute](#how-to-contribute)
- [Adding a New Mapper](#adding-a-new-mapper)
- [Pull Request Process](#pull-request-process)
- [Code Style](#code-style)
- [Testing](#testing)
- [Documentation](#documentation)

## Code of Conduct

We are committed to providing a welcoming and inclusive experience for everyone. Please be respectful and constructive in all interactions.

## Getting Started

### Prerequisites

- Go 1.21 or later
- Docker (for integration tests)
- Make
- Git

### Development Setup

1. **Fork and clone the repository:**
   ```bash
   git clone https://github.com/YOUR_USERNAME/homeport.git
   cd homeport
   ```

2. **Install dependencies:**
   ```bash
   make deps
   ```

3. **Build the project:**
   ```bash
   make build
   ```

4. **Run tests:**
   ```bash
   make test
   ```

5. **Verify the build:**
   ```bash
   ./bin/homeport --version
   ```

## How to Contribute

### Report Bugs

- Use the [bug report template](https://github.com/mercierj/homeport/issues/new?template=bug_report.md)
- Include your Go version and OS
- Provide a minimal reproduction case
- Include relevant Terraform/state file snippets (sanitized)

### Suggest Features

- Use the [feature request template](https://github.com/mercierj/homeport/issues/new?template=feature_request.md)
- Explain the use case and expected behavior
- Consider if it fits the project scope

### Submit Code

1. Check existing issues and PRs to avoid duplication
2. For significant changes, open an issue first to discuss
3. Create a feature branch from `main`
4. Make your changes with tests
5. Submit a pull request

## Adding a New Mapper

Mappers convert AWS resources to self-hosted equivalents. Here's how to add one:

### 1. Create the Mapper File

Create a new file in `internal/infrastructure/mapper/<category>/`:

```go
// internal/infrastructure/mapper/storage/efs.go
package storage

import (
    "context"
    "github.com/mercierj/homeport/internal/domain/mapper"
    "github.com/mercierj/homeport/internal/domain/resource"
)

// EFSMapper maps AWS EFS to NFS Docker container
type EFSMapper struct {
    mapper.BaseMapper
}

// NewEFSMapper creates a new EFS mapper
func NewEFSMapper() *EFSMapper {
    return &EFSMapper{
        BaseMapper: mapper.BaseMapper{
            Type: resource.TypeEFSFileSystem,
        },
    }
}

// Map converts an EFS resource to self-hosted equivalent
func (m *EFSMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
    // Implementation here
    result := &mapper.MappingResult{
        Services: []mapper.DockerService{
            {
                Name:  res.Name + "-nfs",
                Image: "itsthenetwork/nfs-server-alpine:latest",
                // ... configuration
            },
        },
    }
    return result, nil
}

// Validate checks if the resource can be mapped
func (m *EFSMapper) Validate(res *resource.AWSResource) error {
    // Validation logic
    return nil
}

// Dependencies returns required resource types
func (m *EFSMapper) Dependencies() []resource.Type {
    return nil
}
```

### 2. Register the Mapper

Add the mapper to the registry in `internal/infrastructure/mapper/registry.go`:

```go
func NewRegistry() *Registry {
    r := &Registry{
        mappers: make(map[resource.Type]mapper.Mapper),
    }

    // Existing mappers...

    // Register new mapper
    r.Register(storage.NewEFSMapper())

    return r
}
```

### 3. Add the Resource Type

If needed, add the new resource type in `internal/domain/resource/types.go`:

```go
const (
    // Storage
    TypeS3Bucket      Type = "aws_s3_bucket"
    TypeEFSFileSystem Type = "aws_efs_file_system"  // Add this
)
```

### 4. Add Tests

Create tests in the same directory:

```go
// internal/infrastructure/mapper/storage/efs_test.go
package storage

import (
    "context"
    "testing"
    "github.com/mercierj/homeport/internal/domain/resource"
)

func TestEFSMapper_Map(t *testing.T) {
    mapper := NewEFSMapper()

    res := &resource.AWSResource{
        ID:   "fs-12345",
        Name: "shared-storage",
        Type: resource.TypeEFSFileSystem,
        Config: map[string]interface{}{
            "performance_mode": "generalPurpose",
        },
    }

    result, err := mapper.Map(context.Background(), res)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }

    if len(result.Services) == 0 {
        t.Error("expected at least one service")
    }
}
```

### 5. Add Documentation

Update the service documentation in `docs/services/` or the README.

## Pull Request Process

### Before Submitting

1. **Run all checks:**
   ```bash
   make test
   go vet ./...
   gofmt -s -w .
   ```

2. **Update documentation** if needed

3. **Add tests** for new functionality

4. **Update CHANGELOG.md** with your changes

### PR Guidelines

- Use a clear, descriptive title
- Reference related issues (`Fixes #123`)
- Describe what changed and why
- Include screenshots for UI changes
- Keep PRs focused and atomic

### Review Process

1. Automated checks must pass
2. At least one maintainer review required
3. All comments must be resolved
4. Squash merge to main

## Code Style

### Go Code

- Follow [Effective Go](https://golang.org/doc/effective_go)
- Use `gofmt` for formatting
- Use `golint` suggestions
- Keep functions focused and small
- Add comments for exported symbols

### Naming Conventions

```go
// Types: PascalCase
type ResourceMapper struct {}

// Functions: PascalCase for exported, camelCase for private
func ParseState() {}
func parseInstance() {}

// Variables: camelCase
var resourceCount int

// Constants: PascalCase or ALL_CAPS for env vars
const DefaultTimeout = 30
const ENV_CONFIG_PATH = "CLOUDEXIT_CONFIG"
```

### Error Handling

```go
// Wrap errors with context
if err != nil {
    return fmt.Errorf("failed to parse resource %s: %w", name, err)
}

// Use custom error types for specific cases
type UnsupportedResourceError struct {
    Type string
}

func (e *UnsupportedResourceError) Error() string {
    return fmt.Sprintf("unsupported resource type: %s", e.Type)
}
```

## Testing

### Test Categories

1. **Unit Tests**: Test individual functions
   ```bash
   go test ./internal/...
   ```

2. **Integration Tests**: Test component interactions
   ```bash
   go test ./test/integration/...
   ```

3. **E2E Tests**: Test full CLI workflows
   ```bash
   go test ./test/e2e/...
   ```

### Writing Tests

```go
func TestMapper_Map(t *testing.T) {
    // Table-driven tests preferred
    tests := []struct {
        name     string
        input    *resource.AWSResource
        expected int
        wantErr  bool
    }{
        {
            name: "basic resource",
            input: &resource.AWSResource{
                ID:   "test-1",
                Type: resource.TypeEC2Instance,
            },
            expected: 1,
            wantErr:  false,
        },
        // More test cases...
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            mapper := NewEC2Mapper()
            result, err := mapper.Map(context.Background(), tt.input)

            if (err != nil) != tt.wantErr {
                t.Errorf("unexpected error: %v", err)
            }

            if len(result.Services) != tt.expected {
                t.Errorf("got %d services, want %d", len(result.Services), tt.expected)
            }
        })
    }
}
```

### Test Fixtures

Place test data in `test/fixtures/`:

```
test/fixtures/
├── simple-webapp/
│   ├── main.tf
│   └── terraform.tfstate
├── serverless-api/
└── full-stack/
```

## Documentation

### Code Comments

- Add package documentation in a `doc.go` file
- Document all exported types and functions
- Use examples where helpful

```go
// Package mapper provides AWS to self-hosted resource mapping.
//
// Example usage:
//
//     registry := mapper.NewRegistry()
//     mapper, ok := registry.Get(resource.TypeS3Bucket)
//     if ok {
//         result, err := mapper.Map(ctx, awsResource)
//     }
package mapper
```

### User Documentation

- Update README.md for user-facing changes
- Add examples for new features
- Keep language clear and concise

## Questions?

- Open a [Discussion](https://github.com/mercierj/homeport/discussions)
- Join our [Discord](https://discord.gg/homeport)
- Email: maintainers@homeport.dev

---

Thank you for contributing to Homeport!
