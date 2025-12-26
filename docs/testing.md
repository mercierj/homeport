# Testing Guide

This guide covers how to run and write tests for the Agnostech project.

## Test Structure

```
test/
├── fixtures/          # Sample Terraform files and state files for testing
│   ├── sample.tfstate
│   ├── simple-webapp/
│   ├── serverless-api/
│   └── full-stack/
├── integration/       # Integration tests (require build tag)
│   ├── aws/
│   ├── azure/
│   └── gcp/
└── e2e/              # End-to-end migration tests
    ├── aws_migration_test.go
    ├── azure_migration_test.go
    ├── gcp_migration_test.go
    └── multi_cloud_migration_test.go
```

## Running Tests

### Unit Tests

Run all unit tests:

```bash
go test ./...
```

Run with verbose output:

```bash
go test -v ./...
```

Run with race detection:

```bash
go test -race ./...
```

Run tests for a specific package:

```bash
go test -v ./internal/infrastructure/mapper/...
```

### Integration Tests

Integration tests are tagged with `//go:build integration` and require the integration build tag:

```bash
go test -v -tags=integration ./test/integration/...
```

Run integration tests for a specific cloud provider:

```bash
# AWS integration tests
go test -v -tags=integration ./test/integration/aws/...

# Azure integration tests
go test -v -tags=integration ./test/integration/azure/...

# GCP integration tests
go test -v -tags=integration ./test/integration/gcp/...
```

### End-to-End Tests

E2E tests simulate full migration workflows:

```bash
go test -v ./test/e2e/...
```

## Test Coverage

Generate coverage report:

```bash
go test -coverprofile=coverage.out -covermode=atomic ./...
```

View coverage in browser:

```bash
go tool cover -html=coverage.out
```

Get coverage percentage:

```bash
go tool cover -func=coverage.out | grep total
```

The CI pipeline enforces a minimum coverage threshold of **50%**.

## Writing Tests

### Unit Tests

Place unit tests in the same package as the code being tested. Use the `_test.go` suffix:

```go
// internal/infrastructure/mapper/compute/ec2.go
package compute

func (m *EC2Mapper) Map(res *resource.Resource) (*Alternative, error) {
    // ...
}

// internal/infrastructure/mapper/compute/ec2_test.go
package compute

func TestEC2Mapper_Map(t *testing.T) {
    // ...
}
```

### Integration Tests

Integration tests test interactions between components and external systems. Place them in `test/integration/`:

```go
//go:build integration

package aws_test

import (
    "testing"
)

func TestAPIParser_Integration(t *testing.T) {
    // Test integration with real or mocked AWS services
}
```

### Table-Driven Tests

Prefer table-driven tests for testing multiple scenarios:

```go
func TestMapper_Map(t *testing.T) {
    tests := []struct {
        name     string
        input    *resource.Resource
        expected *Alternative
        wantErr  bool
    }{
        {
            name: "basic instance",
            input: &resource.Resource{
                Type: resource.TypeEC2Instance,
                Config: map[string]interface{}{
                    "instance_type": "t3.medium",
                },
            },
            expected: &Alternative{
                Type:     AlternativeTypeCompute,
                Provider: ProviderHetzner,
            },
            wantErr: false,
        },
        // Add more test cases...
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            mapper := NewEC2Mapper()
            result, err := mapper.Map(tt.input)

            if (err != nil) != tt.wantErr {
                t.Errorf("Map() error = %v, wantErr %v", err, tt.wantErr)
                return
            }

            if !reflect.DeepEqual(result, tt.expected) {
                t.Errorf("Map() = %v, want %v", result, tt.expected)
            }
        })
    }
}
```

## Test Fixtures

Test fixtures are located in `test/fixtures/`. Use them for realistic test data:

```go
func TestParseState(t *testing.T) {
    statePath := "../../test/fixtures/sample.tfstate"

    infra, err := parser.ParseState(statePath)
    if err != nil {
        t.Fatalf("failed to parse state: %v", err)
    }

    // Assert on parsed infrastructure...
}
```

## Mocking

For tests that require external dependencies, use interfaces and mock implementations:

```go
// Define interface
type CloudClient interface {
    ListInstances(ctx context.Context) ([]Instance, error)
}

// Mock implementation
type mockCloudClient struct {
    instances []Instance
    err       error
}

func (m *mockCloudClient) ListInstances(ctx context.Context) ([]Instance, error) {
    return m.instances, m.err
}

// Use in tests
func TestService_ProcessInstances(t *testing.T) {
    client := &mockCloudClient{
        instances: []Instance{{ID: "i-123"}},
    }

    service := NewService(client)
    err := service.ProcessInstances(context.Background())

    // Assert...
}
```

## Cloud Provider Credentials

Integration tests that require real cloud access use environment variables:

### AWS

```bash
export AWS_ACCESS_KEY_ID="your-key"
export AWS_SECRET_ACCESS_KEY="your-secret"
export AWS_DEFAULT_REGION="us-east-1"
```

### Azure

```bash
export AZURE_SUBSCRIPTION_ID="your-subscription"
export AZURE_TENANT_ID="your-tenant"
export AZURE_CLIENT_ID="your-client-id"
export AZURE_CLIENT_SECRET="your-secret"
```

### GCP

```bash
export GOOGLE_APPLICATION_CREDENTIALS="/path/to/service-account.json"
export GOOGLE_PROJECT="your-project"
```

Tests that require credentials should skip gracefully when credentials are unavailable:

```go
func TestWithCredentials(t *testing.T) {
    if os.Getenv("AWS_ACCESS_KEY_ID") == "" {
        t.Skip("Skipping test - AWS credentials not available")
    }
    // ... rest of test
}
```

## CI/CD

The GitHub Actions CI pipeline runs:

1. **Lint**: golangci-lint with 5-minute timeout
2. **Test**: Unit tests across Go 1.22, 1.23, 1.24 on Linux, macOS, Windows
3. **Coverage**: Reports to Codecov (Go 1.24, Ubuntu only)
4. **Integration**: Integration tests (requires test tag)
5. **Security**: gosec and Trivy vulnerability scanning
6. **Build**: Cross-platform binary compilation

See `.github/workflows/ci.yml` for the complete configuration.
