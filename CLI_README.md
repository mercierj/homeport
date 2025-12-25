# CloudExit CLI

CloudExit is a command-line tool that helps you migrate from AWS infrastructure to a self-hosted Docker-based stack.

## Features

- Analyze AWS infrastructure from Terraform state files or configurations
- Generate complete Docker Compose stacks with Traefik reverse proxy
- Validate generated configurations
- Beautiful terminal UI with progress indicators and styled output
- Support for multiple output formats (JSON, YAML, Table)

## Installation

### From Source

```bash
# Clone the repository
git clone https://github.com/cloudexit/cloudexit.git
cd cloudexit

# Build the CLI
make build

# Or install to GOPATH/bin
make install
```

### Using the Makefile

The project includes a Makefile for common tasks:

```bash
make help          # Show all available commands
make deps          # Download dependencies
make build         # Build the binary
make install       # Install to GOPATH/bin
make test          # Run tests
make clean         # Clean build artifacts
make run           # Build and run with --help
```

## Quick Start

### 1. Analyze Infrastructure

Analyze your AWS infrastructure to see what resources will be migrated:

```bash
# Analyze Terraform state file
cloudexit analyze terraform.tfstate

# Analyze with table output
cloudexit analyze ./infrastructure --format table

# Save analysis to custom file
cloudexit analyze ./infrastructure --output my-analysis.json
```

### 2. Generate Self-Hosted Stack

Generate a complete Docker stack from your AWS infrastructure:

```bash
# Basic migration
cloudexit migrate terraform.tfstate

# With domain configuration
cloudexit migrate ./infrastructure --domain example.com

# With migration tools and monitoring
cloudexit migrate ./infrastructure \
  --output ./my-stack \
  --domain example.com \
  --include-migration \
  --include-monitoring
```

### 3. Validate Generated Stack

Validate the generated configuration before deployment:

```bash
# Validate the generated stack
cloudexit validate ./output

# Validate with verbose output
cloudexit validate ./output --verbose
```

## Commands

### analyze

Analyze AWS infrastructure from Terraform state/files.

```bash
cloudexit analyze <path> [flags]
```

**Flags:**
- `-o, --output` - Output file path (default: "analysis.json")
- `-f, --format` - Output format: json, yaml, table (default: "json")

**Examples:**
```bash
cloudexit analyze terraform.tfstate
cloudexit analyze ./infrastructure --format yaml
cloudexit analyze ./infrastructure --output report.json --format table
```

### migrate

Generate self-hosted stack from AWS infrastructure.

```bash
cloudexit migrate <path> [flags]
```

**Flags:**
- `-o, --output` - Output directory path (default: "./output")
- `-d, --domain` - Domain name for services
- `--include-migration` - Include migration tools and scripts
- `--include-monitoring` - Include monitoring stack (Prometheus, Grafana)

**Examples:**
```bash
cloudexit migrate terraform.tfstate
cloudexit migrate ./infrastructure --domain myapp.com
cloudexit migrate ./infrastructure --output ./prod-stack --include-monitoring
```

### validate

Validate generated stack configuration.

```bash
cloudexit validate <path>
```

**Examples:**
```bash
cloudexit validate ./output
cloudexit validate ./output --verbose
```

### version

Print version information.

```bash
cloudexit version
```

## Global Flags

These flags are available for all commands:

- `--config` - Config file (default: $HOME/.cloudexit.yaml)
- `-v, --verbose` - Verbose output
- `-q, --quiet` - Quiet output (errors only)

## Configuration File

You can create a configuration file at `~/.cloudexit.yaml`:

```yaml
# Default output directory
output: ./stacks

# Default domain
domain: example.com

# Always include monitoring
include-monitoring: true

# Verbose mode
verbose: false
```

## Output Structure

The migrate command generates the following structure:

```
output/
├── docker-compose.yml      # Main compose file
├── .env.example           # Environment variables template
├── README.md              # Generated documentation
├── traefik/
│   └── traefik.yml       # Traefik configuration
└── certs/                 # SSL certificates directory
```

## Migration Workflow

1. **Analyze** your AWS infrastructure:
   ```bash
   cloudexit analyze terraform.tfstate --format table
   ```

2. **Review** the analysis output to understand what will be migrated

3. **Generate** the self-hosted stack:
   ```bash
   cloudexit migrate terraform.tfstate --domain myapp.com --output ./my-stack
   ```

4. **Validate** the generated configuration:
   ```bash
   cloudexit validate ./my-stack --verbose
   ```

5. **Configure** environment variables:
   ```bash
   cd my-stack
   cp .env.example .env
   # Edit .env with your configuration
   ```

6. **Deploy** the stack:
   ```bash
   docker network create web
   docker-compose up -d
   ```

## Supported AWS Resources

CloudExit currently supports migrating:

- **Compute**: EC2 instances, ECS containers
- **Database**: RDS (PostgreSQL, MySQL), DynamoDB
- **Storage**: S3 buckets, EBS volumes
- **Networking**: VPC, Load Balancers, Security Groups
- **Security**: IAM roles, KMS keys

## Development

### Building with Version Information

```bash
# Build with version information
make VERSION=1.0.0 build

# Build for all platforms
make build-all
```

The version information is injected at build time using ldflags.

### Running in Development Mode

```bash
# Run directly with go run
make dev

# Or with custom flags
go run ./cmd/cloudexit --verbose analyze ./test/fixtures
```

## Troubleshooting

### CLI doesn't recognize commands

Make sure you've run `make deps` and `make build` successfully.

### Import errors

Run `go mod tidy` to ensure all dependencies are correctly installed.

### Permission issues

Make sure the setup script is executable:
```bash
chmod +x setup-cli.sh
./setup-cli.sh
```

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

MIT License - see LICENSE file for details.

## Support

For issues and questions:
- GitHub Issues: https://github.com/cloudexit/cloudexit/issues
- Documentation: https://cloudexit.github.io
