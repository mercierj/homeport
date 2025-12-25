# CloudExit

**Escape AWS. Own your infrastructure.**

CloudExit transforms your AWS infrastructure into a self-hosted Docker stack. Zero US dependencies. Full sovereignty. One command.

[![License: AGPL v3](https://img.shields.io/badge/License-AGPL%20v3-blue.svg)](https://www.gnu.org/licenses/agpl-3.0)
[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go)](https://go.dev/)

---

## Features

- **70+ AWS services supported** - Full mapping to self-hosted equivalents
- **One command** - Complete Docker Compose stack generation
- **Zero US cloud** - 100% self-hosted, GDPR compliant
- **Migration scripts** - Automated S3, RDS, DynamoDB data transfer
- **Monitoring included** - Prometheus, Grafana, Loki stack
- **Extensible** - Plugin system for custom mappers

## Quick Start

### Installation

**Homebrew (macOS/Linux):**
```bash
brew install cloudexit/tap/cloudexit
```

**Go Install:**
```bash
go install github.com/cloudexit/cloudexit/cmd/cloudexit@latest
```

**From Source:**
```bash
git clone https://github.com/cloudexit/cloudexit.git
cd cloudexit
make build
./bin/cloudexit --help
```

### Usage

```bash
# Analyze your AWS infrastructure
cloudexit analyze ./terraform

# Generate self-hosted stack
cloudexit migrate ./terraform --output ./my-stack --domain myapp.com

# Validate generated configuration
cloudexit validate ./my-stack

# Deploy
cd my-stack
cp .env.example .env
docker compose up -d
```

## Supported Services

| AWS Service | Self-Hosted Equivalent | Status |
|-------------|------------------------|--------|
| EC2 | Docker Containers | ✅ |
| Lambda | OpenFaaS / Docker | ✅ |
| S3 | MinIO | ✅ |
| RDS (PostgreSQL) | PostgreSQL Docker | ✅ |
| RDS (MySQL) | MySQL Docker | ✅ |
| DynamoDB | ScyllaDB | ✅ |
| ElastiCache (Redis) | Redis Docker | ✅ |
| ElastiCache (Memcached) | Memcached Docker | ✅ |
| ALB/NLB | Traefik | ✅ |
| API Gateway | Traefik + Middleware | ✅ |
| Cognito | Keycloak | ✅ |
| SQS | RabbitMQ | ✅ |
| SNS | RabbitMQ Exchanges | ✅ |
| CloudWatch | Prometheus + Grafana + Loki | ✅ |
| Secrets Manager | HashiCorp Vault / .env | ✅ |
| Route53 | PowerDNS / DNS Export | ✅ |
| ACM | Let's Encrypt (via Traefik) | ✅ |

## Example Output

Running `cloudexit migrate` generates:

```
my-stack/
├── docker-compose.yml        # Complete Docker stack
├── docker-compose.override.yml
├── .env.example              # Environment template
├── traefik/
│   ├── traefik.yml           # Traefik configuration
│   └── dynamic/              # Dynamic routing
├── configs/
│   ├── postgres/             # DB init scripts
│   ├── minio/                # S3 policies
│   └── keycloak/             # Auth realm
├── scripts/
│   ├── migrate-s3.sh         # S3 → MinIO migration
│   ├── migrate-rds.sh        # RDS → PostgreSQL migration
│   └── backup.sh             # Backup automation
├── monitoring/
│   ├── prometheus.yml
│   ├── alertmanager.yml
│   └── grafana/dashboards/
└── README.md                 # Deployment guide
```

## Configuration

Create `.cloudexit.yaml` in your project or home directory:

```yaml
output:
  directory: "./output"
  format: "compose"  # compose, swarm, k8s (future)

domain: "myapp.com"

resources:
  database:
    rds:
      engine_mapping:
        postgres: "postgres:15-alpine"
        mysql: "mysql:8.0"

  storage:
    s3:
      image: "quay.io/minio/minio:latest"
      console_port: 9001

ssl:
  enabled: true
  provider: "letsencrypt"
  email: "admin@myapp.com"
```

## CLI Reference

```
cloudexit - AWS to Self-Hosted Migration Tool

Usage:
  cloudexit [command]

Available Commands:
  analyze     Analyze AWS infrastructure from Terraform
  migrate     Generate self-hosted stack from AWS infrastructure
  validate    Validate generated stack configuration
  version     Show version information
  help        Help about any command

Flags:
      --config string   Config file (default: ~/.cloudexit.yaml)
  -h, --help            Help for cloudexit
  -v, --verbose         Enable verbose output
  -q, --quiet           Suppress non-essential output
```

### Analyze Command

```bash
cloudexit analyze ./terraform [flags]

Flags:
  -o, --output string   Output file for analysis (default: stdout)
  -f, --format string   Output format: table, json, yaml (default: table)
```

### Migrate Command

```bash
cloudexit migrate ./terraform [flags]

Flags:
  -o, --output string    Output directory (default: ./output)
  -d, --domain string    Base domain for services (default: localhost)
      --include-migration    Generate migration scripts (default: true)
      --include-monitoring   Include monitoring stack (default: true)
```

### Validate Command

```bash
cloudexit validate ./output [flags]

Flags:
      --strict   Enable strict validation mode
```

## Development

### Prerequisites

- Go 1.21+
- Docker (for testing)
- Make

### Building

```bash
# Clone repository
git clone https://github.com/cloudexit/cloudexit.git
cd cloudexit

# Install dependencies
make deps

# Build
make build

# Run tests
make test

# Build for all platforms
make build-all
```

### Project Structure

```
cloudexit/
├── cmd/cloudexit/          # CLI entry point
├── internal/
│   ├── app/                # Application services
│   ├── cli/                # CLI commands
│   ├── domain/             # Core domain models
│   └── infrastructure/     # Implementations
│       ├── parser/         # Terraform/HCL parsing
│       ├── mapper/         # AWS → Docker mapping
│       └── generator/      # Output generation
├── pkg/                    # Public packages
├── templates/              # Go templates
└── test/                   # Tests and fixtures
```

## Contributing

We welcome contributions! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

Quick start:
1. Fork the repository
2. Create a feature branch
3. Make your changes with tests
4. Submit a pull request

## Roadmap

- [x] Core Terraform parser
- [x] Essential AWS service mappers
- [x] Docker Compose generation
- [x] Migration scripts
- [ ] Kubernetes output format
- [ ] GCP/Azure support
- [ ] Web UI dashboard
- [ ] Plugin marketplace

## License

CloudExit is licensed under the [GNU Affero General Public License v3.0](LICENSE).

This means:
- ✅ Free to use, modify, and distribute
- ✅ Commercial use allowed
- ✅ Modifications must remain open source
- ✅ Network use requires source disclosure

## Support

- **Documentation**: [docs.cloudexit.dev](https://docs.cloudexit.dev)
- **Issues**: [GitHub Issues](https://github.com/cloudexit/cloudexit/issues)
- **Discord**: [Join our community](https://discord.gg/cloudexit)

---

Made with care for digital sovereignty.
