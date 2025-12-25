# AgnosTech

**Reclaim your infrastructure.**

AgnosTech transforms your AWS, GCP, or Azure infrastructure into a self-hosted Docker stack. Zero US dependencies. Full sovereignty. One command.

[![License: AGPL v3](https://img.shields.io/badge/License-AGPL%20v3-blue.svg)](https://www.gnu.org/licenses/agpl-3.0)
[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go)](https://go.dev/)
[![Release](https://img.shields.io/github/v/release/agnostech/agnostech?include_prereleases)](https://github.com/agnostech/agnostech/releases)

---

## Features

- **Multi-Cloud Support** - AWS, GCP, and Azure infrastructure migration
- **70+ Services Mapped** - Comprehensive mapping to self-hosted equivalents
- **One Command** - Complete Docker Compose stack generation
- **Zero Cloud Dependencies** - 100% self-hosted, GDPR compliant
- **Migration Scripts** - Automated data transfer (S3, RDS, DynamoDB, etc.)
- **Monitoring Included** - Prometheus, Grafana, Loki stack ready
- **Extensible** - Plugin system for custom mappers

## Quick Start

```bash
# 1. Install
brew install agnostech/tap/agnostech

# 2. Analyze your infrastructure
agnostech analyze ./terraform

# 3. Generate self-hosted stack
agnostech migrate ./terraform --output ./my-stack --domain myapp.com

# 4. Deploy
cd my-stack && docker compose up -d
```

## Installation

### Homebrew (macOS/Linux)

```bash
brew install agnostech/tap/agnostech
```

### Go Install

```bash
go install github.com/agnostech/agnostech/cmd/agnostech@latest
```

### From Source

```bash
git clone https://github.com/agnostech/agnostech.git
cd agnostech
make build
./bin/agnostech --help
```

### Docker

```bash
docker pull agnostech/agnostech:latest
docker run -v $(pwd):/workspace agnostech/agnostech migrate /workspace/terraform -o /workspace/output
```

## Usage

### Analyze Infrastructure

```bash
# Analyze from Terraform state
agnostech analyze ./terraform --format table

# JSON output for CI/CD
agnostech analyze ./terraform --format json -o analysis.json
```

### Generate Docker Stack

```bash
# Basic migration
agnostech migrate ./terraform --output ./my-stack --domain myapp.com

# With monitoring stack
agnostech migrate ./terraform -o ./my-stack -d myapp.com --include-monitoring

# Skip migration scripts
agnostech migrate ./terraform -o ./my-stack --include-migration=false
```

### Validate Configuration

```bash
# Validate generated stack
agnostech validate ./my-stack

# Strict mode (fail on warnings)
agnostech validate ./my-stack --strict
```

## Supported Services

### AWS Services

| Service | Self-Hosted | Status |
|---------|-------------|--------|
| EC2 | Docker Containers | Full |
| Lambda | OpenFaaS / Docker | Full |
| ECS/EKS | Docker / K8s | Full |
| S3 | MinIO | Full |
| RDS (PostgreSQL/MySQL) | PostgreSQL/MySQL | Full |
| DynamoDB | ScyllaDB | Full |
| ElastiCache | Redis/Memcached | Full |
| ALB/NLB | Traefik | Full |
| API Gateway | Traefik + Middleware | Full |
| Cognito | Keycloak | Full |
| SQS/SNS | RabbitMQ | Full |
| CloudWatch | Prometheus + Grafana | Full |
| Secrets Manager | Vault / .env | Full |
| Route53 | PowerDNS | Full |

[Full AWS Service Reference](docs/aws-services.md)

### GCP Services

| Service | Self-Hosted | Status |
|---------|-------------|--------|
| Compute Engine | Docker | Full |
| Cloud Run | Docker | Full |
| Cloud Functions | Docker | Full |
| GKE | Kubernetes | Full |
| Cloud Storage | MinIO | Full |
| Cloud SQL | PostgreSQL/MySQL | Full |
| Firestore | MongoDB | Full |
| Memorystore | Redis | Full |
| Pub/Sub | RabbitMQ | Full |
| Cloud DNS | PowerDNS | Full |

[Full GCP Service Reference](docs/gcp-services.md)

### Azure Services

| Service | Self-Hosted | Status |
|---------|-------------|--------|
| Virtual Machines | Docker | Full |
| Azure Functions | Docker | Full |
| AKS | Kubernetes | Full |
| Blob Storage | MinIO | Full |
| Azure SQL | MSSQL/PostgreSQL | Full |
| CosmosDB | MongoDB/ScyllaDB | Full |
| Azure Cache | Redis | Full |
| Service Bus | RabbitMQ | Full |
| Azure DNS | PowerDNS | Full |
| Key Vault | Vault / .env | Full |

[Full Azure Service Reference](docs/azure-services.md)

## Output Structure

Running `agnostech migrate` generates:

```
my-stack/
├── docker-compose.yml        # Complete Docker stack
├── docker-compose.override.yml
├── .env.example              # Environment template
├── traefik/
│   ├── traefik.yml           # Traefik configuration
│   └── dynamic/              # Dynamic routing rules
├── configs/
│   ├── postgres/             # DB init scripts
│   ├── minio/                # S3 policies
│   └── keycloak/             # Auth realm
├── scripts/
│   ├── migrate-s3.sh         # S3 -> MinIO migration
│   ├── migrate-rds.sh        # RDS -> PostgreSQL migration
│   └── backup.sh             # Backup automation
├── monitoring/
│   ├── prometheus.yml
│   ├── alertmanager.yml
│   └── grafana/dashboards/
└── README.md                 # Deployment guide
```

## Configuration

Create `.agnostech.yaml` in your project or home directory:

```yaml
output:
  directory: "./output"
  format: "compose"  # compose, swarm, k8s (planned)

domain: "myapp.com"

resources:
  database:
    rds:
      engine_mapping:
        postgres: "postgres:16-alpine"
        mysql: "mysql:8.0"

  storage:
    s3:
      image: "quay.io/minio/minio:latest"
      console_port: 9001

ssl:
  enabled: true
  provider: "letsencrypt"
  email: "admin@myapp.com"

monitoring:
  enabled: true
  prometheus:
    retention: "15d"
  grafana:
    admin_password: "${GRAFANA_PASSWORD}"
```

## CLI Reference

```
agnostech - Cloud to Self-Hosted Migration Tool

Usage:
  agnostech [command]

Available Commands:
  analyze     Analyze cloud infrastructure from Terraform
  migrate     Generate self-hosted stack from cloud infrastructure
  validate    Validate generated stack configuration
  version     Show version information
  help        Help about any command

Flags:
      --config string   Config file (default: ~/.agnostech.yaml)
  -h, --help            Help for agnostech
  -v, --verbose         Enable verbose output
  -q, --quiet           Suppress non-essential output
```

## Documentation

- [Architecture](docs/architecture.md) - Technical architecture overview
- [AWS Services](docs/aws-services.md) - AWS service mapping reference
- [GCP Services](docs/gcp-services.md) - GCP service mapping reference
- [Azure Services](docs/azure-services.md) - Azure service mapping reference
- [Contributing](docs/contributing.md) - Contribution guide
- [Examples](docs/examples/) - Example migration projects

## Development

### Prerequisites

- Go 1.23+
- Docker (for testing)
- Make

### Building

```bash
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
agnostech/
├── cmd/agnostech/          # CLI entry point
├── internal/
│   ├── app/                # Application services
│   ├── cli/                # CLI commands
│   ├── domain/             # Core domain models
│   └── infrastructure/     # Implementations
│       ├── parser/         # Terraform/HCL parsing
│       ├── mapper/         # Cloud -> Docker mapping
│       │   ├── gcp/        # GCP mappers
│       │   └── azure/      # Azure mappers
│       └── generator/      # Output generation
├── pkg/                    # Public packages
├── templates/              # Go templates
└── test/                   # Tests and fixtures
```

## Contributing

We welcome contributions! See [docs/contributing.md](docs/contributing.md) for guidelines.

Quick start:
1. Fork the repository
2. Create a feature branch
3. Make your changes with tests
4. Submit a pull request

## Roadmap

- [x] Core Terraform parser
- [x] AWS service mappers (17 services)
- [x] GCP service mappers (15 services)
- [x] Azure service mappers (22 services)
- [x] Docker Compose generation
- [x] Migration scripts
- [x] Monitoring stack
- [ ] Kubernetes output format
- [ ] Web UI dashboard
- [ ] Plugin marketplace

## License

AgnosTech is licensed under the [GNU Affero General Public License v3.0](LICENSE).

This means:
- Free to use, modify, and distribute
- Commercial use allowed
- Modifications must remain open source
- Network use requires source disclosure

## Support

- **Documentation**: [docs/](docs/)
- **Issues**: [GitHub Issues](https://github.com/agnostech/agnostech/issues)
- **Discussions**: [GitHub Discussions](https://github.com/agnostech/agnostech/discussions)

---

Made with care for digital sovereignty.
