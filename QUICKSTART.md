# AgnosTech CLI - Quick Start Guide

This guide will help you get started with the AgnosTech CLI in 5 minutes.

## Prerequisites

- Go 1.21 or later
- Docker (for deploying generated stacks)
- Git (optional)

## Installation

### Option 1: Using Make (Recommended)

```bash
# Navigate to the project directory
cd /Users/jo/Prog/exit_gafam

# Build the CLI
make build

# The binary will be available at: ./bin/agnostech
```

### Option 2: Using the Setup Script

```bash
# Make the script executable
chmod +x setup-cli.sh

# Run the setup
./setup-cli.sh
```

### Option 3: Manual Build

```bash
# Download dependencies
go mod download
go mod tidy

# Build the binary
go build -o bin/agnostech ./cmd/agnostech

# Test it
./bin/agnostech --help
```

## Quick Test

Test the CLI with the sample Terraform state:

```bash
# Run all tests
chmod +x test-cli.sh
./test-cli.sh
```

## Your First Migration

### 1. Analyze AWS Infrastructure

```bash
# Analyze the sample Terraform state
./bin/agnostech analyze ./test/fixtures/sample.tfstate --format table

# Output:
# - Shows EC2 instances, RDS databases, S3 buckets
# - Displays how each will be migrated to Docker
```

### 2. Generate Self-Hosted Stack

```bash
# Generate a Docker stack
./bin/agnostech migrate ./test/fixtures/sample.tfstate \
  --output ./my-stack \
  --domain myapp.local \
  --include-monitoring

# This creates:
# - docker-compose.yml
# - Traefik configuration
# - Environment files
# - Documentation
```

### 3. Validate the Stack

```bash
# Validate the generated configuration
./bin/agnostech validate ./my-stack --verbose

# Check all configurations are valid
```

### 4. Deploy (Optional)

```bash
# Navigate to the generated stack
cd my-stack

# Review and update environment variables
cp .env.example .env
# Edit .env with your configuration

# Create Docker network
docker network create web

# Start the stack
docker-compose up -d

# Check status
docker-compose ps
```

## Common Commands

### Help

```bash
# General help
./bin/agnostech --help

# Command-specific help
./bin/agnostech analyze --help
./bin/agnostech migrate --help
./bin/agnostech validate --help
```

### Version

```bash
./bin/agnostech version
```

### Using Make Commands

```bash
make help              # Show all available commands
make build             # Build the binary
make install           # Install to GOPATH/bin
make test              # Run Go tests
make clean             # Clean build artifacts
make dev               # Run in development mode
make example-analyze   # Run example analyze
make example-migrate   # Run example migrate
make example-validate  # Run example validate
```

## Configuration

Create a configuration file at `~/.agnostech.yaml`:

```yaml
output: ./stacks
domain: myapp.com
include-monitoring: true
format: table
```

Or copy the example:

```bash
cp .agnostech.example.yaml ~/.agnostech.yaml
```

## Next Steps

1. Read the full documentation: [CLI_README.md](./CLI_README.md)
2. Try analyzing your own Terraform state files
3. Customize the configuration in `.agnostech.yaml`
4. Explore the generated Docker configurations
5. Deploy to your server!

## Troubleshooting

### "Command not found"

Make sure you're using the correct path:
```bash
./bin/agnostech --help
```

Or add to PATH:
```bash
export PATH=$PATH:$(pwd)/bin
agnostech --help
```

### "Permission denied"

Make the binary executable:
```bash
chmod +x bin/agnostech
```

### "Module not found"

Install dependencies:
```bash
make deps
# or
go mod download && go mod tidy
```

### Build errors

Check Go version:
```bash
go version  # Should be 1.21 or later
```

Update dependencies:
```bash
go mod tidy
make clean
make build
```

## Support

- GitHub Issues: https://github.com/agnostech/agnostech/issues
- Documentation: [CLI_README.md](./CLI_README.md)

## What's Next?

The CLI provides the foundation for analyzing and migrating AWS infrastructure. You can extend it by:

1. Adding parsers for more AWS resource types
2. Customizing Docker service templates
3. Adding support for other cloud providers
4. Integrating with CI/CD pipelines
5. Adding interactive migration wizards

Happy migrating! 🚀
