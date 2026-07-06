# Homeport

**Reclaim your infrastructure.**

Homeport transforms your AWS, GCP, or Azure infrastructure into a self-hosted Docker stack. Zero US dependencies. Full sovereignty. One command.

[![License: AGPL v3](https://img.shields.io/badge/License-AGPL%20v3-blue.svg)](https://www.gnu.org/licenses/agpl-3.0)
[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go)](https://go.dev/)
[![Release](https://img.shields.io/github/v/release/mercierj/homeport?include_prereleases)](https://github.com/mercierj/homeport/releases)

---

## Features

- **Web Dashboard** - Full-featured UI for migration management and infrastructure control
- **Multi-Cloud Support** - AWS, GCP, and Azure infrastructure migration
- **70+ Services Mapped** - Comprehensive mapping to self-hosted equivalents
- **One Command** - Complete Docker Compose stack generation
- **Zero Cloud Dependencies** - 100% self-hosted, GDPR compliant
- **Migration Scripts** - Automated data transfer (S3, RDS, DynamoDB, etc.)
- **Monitoring Included** - Prometheus, Grafana, Loki stack ready
- **Extensible** - Plugin system for custom mappers

## Web Dashboard

Homeport includes a full-featured web dashboard for managing your cloud migration and self-hosted infrastructure.

### Start the Dashboard

```bash
# Build with web UI included
make build-with-web

# Start the dashboard
./bin/homeport serve

# Open http://localhost:8080 in your browser
```

### Dashboard Features

| Category | Features |
|----------|----------|
| **Migration** | 7-step wizard (Analyze → Export → Upload → Secrets → Deploy → Sync → Cutover) |
| **Stacks** | Docker Compose stack management, container lifecycle control |
| **Compute** | Container management, serverless functions, web terminal |
| **Data** | Database admin, S3/MinIO browser, Redis/cache inspection, queue management |
| **Security** | Identity management, SSL certificates, secrets vault, RBAC policies |
| **Networking** | DNS zone management, load balancer configuration |
| **Monitoring** | Real-time metrics, log search, alerting |
| **Backup** | Scheduled backups, point-in-time recovery |

### Development Mode

For frontend development with hot-reload:

```bash
# Terminal 1: Start backend API
make build && ./bin/homeport serve --port 8080

# Terminal 2: Start frontend dev server (with API proxy)
cd web && npm install && npm run dev
# Open http://localhost:5173
```

### A-to-Z wizard readiness check

Use Node 22.12.0+ or 20.19.0+ for the web build:

```bash
cd web
nvm use
cd ..
make acceptance
```

The wizard is considered ready only when Go tests, the production web build, and the Playwright A-to-Z smoke test pass.

### Centralized A-to-Z migration UX

The supported migration journey is `/migrate`.

`/migrate` is responsible for:
- analyzing a source or uploading a `.hprt` bundle,
- resolving required secrets,
- choosing local Docker, remote SSH, or EU cloud provider deployment,
- exporting Docker or Terraform artifacts when manual deployment is preferred,
- running sync and cutover checks,
- showing the final migration completion state.

`/deploy` is a legacy URL and redirects to `/migrate`; it must not present a second deployment wizard.

## Quick Start

```bash
# 1. Install
brew install mercierj/tap/homeport

# 2. Analyze your infrastructure
homeport analyze ./terraform

# 3. Generate self-hosted stack
homeport migrate ./terraform --output ./my-stack --domain myapp.com

# 4. Deploy
cd my-stack && docker compose up -d
```

## Installation

### Homebrew (macOS/Linux)

```bash
brew install mercierj/tap/homeport
```

### Go Install

```bash
go install github.com/mercierj/homeport/cmd/homeport@latest
```

### From Source

```bash
git clone https://github.com/mercierj/homeport.git
cd homeport
make build
./bin/homeport --help
```

### Docker

```bash
docker pull mercierj/homeport:latest
docker run -v $(pwd):/workspace mercierj/homeport migrate /workspace/terraform -o /workspace/output
```

## Usage

### Analyze Infrastructure

```bash
# Analyze from Terraform state
homeport analyze ./terraform --format table

# JSON output for CI/CD
homeport analyze ./terraform --format json -o analysis.json
```

### Generate Docker Stack

```bash
# Basic migration
homeport migrate ./terraform --output ./my-stack --domain myapp.com

# With monitoring stack
homeport migrate ./terraform -o ./my-stack -d myapp.com --include-monitoring

# Skip migration scripts
homeport migrate ./terraform -o ./my-stack --include-migration=false
```

### Validate Configuration

```bash
# Validate generated stack
homeport validate ./my-stack

# Strict mode (fail on warnings)
homeport validate ./my-stack --strict
```

## Supported Services

The authoritative coverage ledger is in [docs/coverage/services.md](docs/coverage/services.md).

### AWS Services

| Service | Self-Hosted | Status |
|---------|-------------|--------|
| EC2 | Docker Containers | Mapped |
| Lambda | OpenFaaS / Docker | Mapped |
| ECS/EKS | Docker / K8s | Mapped |
| S3 | MinIO | Mapped |
| RDS (PostgreSQL/MySQL) | PostgreSQL/MySQL | Mapped |
| DynamoDB | ScyllaDB | Mapped |
| ElastiCache | Redis/Memcached | Mapped |
| ALB/NLB | Traefik | Mapped |
| API Gateway | Traefik + Middleware | Mapped |
| Cognito | Keycloak | Mapped |
| SQS/SNS | RabbitMQ | Mapped |
| CloudWatch | Prometheus + Grafana | Mapped |
| Secrets Manager | Vault / .env | Mapped |
| Route53 | PowerDNS | Mapped |

[Full AWS Service Reference](docs/aws-services.md)

### GCP Services

| Service | Self-Hosted | Status |
|---------|-------------|--------|
| Compute Engine | Docker | Mapped |
| Cloud Run | Docker | Mapped |
| Cloud Functions | Docker | Mapped |
| GKE | Kubernetes | Mapped |
| Cloud Storage | MinIO | Mapped |
| Cloud SQL | PostgreSQL/MySQL | Mapped |
| Firestore | MongoDB | Mapped |
| Memorystore | Redis | Mapped |
| Pub/Sub | RabbitMQ | Mapped |
| Cloud DNS | PowerDNS | Mapped |

[Full GCP Service Reference](docs/gcp-services.md)

### Azure Services

| Service | Self-Hosted | Status |
|---------|-------------|--------|
| Virtual Machines | Docker | Mapped |
| Azure Functions | Docker | Mapped |
| AKS | Kubernetes | Mapped |
| Blob Storage | MinIO | Mapped |
| Azure SQL | MSSQL/PostgreSQL | Mapped |
| CosmosDB | MongoDB/ScyllaDB | Mapped |
| Azure Cache | Redis | Mapped |
| Service Bus | RabbitMQ | Mapped |
| Azure DNS | PowerDNS | Mapped |
| Key Vault | Vault / .env | Mapped |

[Full Azure Service Reference](docs/azure-services.md)

## Output Structure

Running `homeport migrate` generates:

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

Create `.homeport.yaml` in your project or home directory:

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
homeport - Cloud to Self-Hosted Migration Tool

Usage:
  homeport [command]

Available Commands:
  analyze     Analyze cloud infrastructure from Terraform
  migrate     Generate self-hosted stack from cloud infrastructure
  validate    Validate generated stack configuration
  serve       Start the web dashboard and API server
  version     Show version information
  help        Help about any command

Flags:
      --config string   Config file (default: ~/.homeport.yaml)
  -h, --help            Help for homeport
  -v, --verbose         Enable verbose output
  -q, --quiet           Suppress non-essential output
```

### Serve Command

```bash
# Start dashboard on default port (8080)
homeport serve

# Custom host and port
homeport serve --host 0.0.0.0 --port 3000

# Development mode (disable auth)
homeport serve --no-auth
```

| Flag | Description | Default |
|------|-------------|---------|
| `--port, -p` | Port to listen on | 8080 |
| `--host, -H` | Host to bind to | localhost |
| `--no-auth` | Disable authentication (dev only) | false |

## Documentation

- [Architecture](docs/architecture.md) - Technical architecture overview
- [Web Dashboard Guide](docs/web-dashboard.md) - Dashboard features and usage
- [API Reference](docs/api-reference.md) - REST API documentation
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

# Build CLI only
make build

# Build with web dashboard (recommended)
make build-with-web

# Run tests
make test

# Build for all platforms
make build-all
```

### Web Development

```bash
# Install web dependencies
make web-install

# Build web UI only
make web-build

# Run web dev server (hot reload)
make web-dev

# Start full stack (API + Web)
make serve
```

### Project Structure

```
homeport/
├── cmd/homeport/          # CLI entry point
├── internal/
│   ├── api/                # REST API server & handlers
│   ├── app/                # Application services
│   ├── cli/                # CLI commands
│   ├── domain/             # Core domain models
│   └── infrastructure/     # Implementations
│       ├── parser/         # Terraform/HCL parsing
│       ├── mapper/         # Cloud -> Docker mapping
│       │   ├── gcp/        # GCP mappers
│       │   └── azure/      # Azure mappers
│       └── generator/      # Output generation
├── web/                    # React frontend (dashboard)
│   ├── src/
│   │   ├── pages/          # Dashboard pages
│   │   ├── components/     # UI components
│   │   └── lib/            # Utilities & API client
│   └── package.json
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
- [x] Web UI dashboard
- [ ] Kubernetes output format
- [ ] Plugin marketplace

## License

Homeport is licensed under the [GNU Affero General Public License v3.0](LICENSE).

This means:
- Free to use, modify, and distribute
- Commercial use allowed
- Modifications must remain open source
- Network use requires source disclosure

## Support

- **Documentation**: [docs/](docs/)
- **Issues**: [GitHub Issues](https://github.com/mercierj/homeport/issues)
- **Discussions**: [GitHub Discussions](https://github.com/mercierj/homeport/discussions)

---

Made with care for digital sovereignty.
