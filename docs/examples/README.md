# CloudExit Examples

This directory contains example migration projects demonstrating CloudExit's capabilities across different architectures and cloud providers.

## Examples

| Example | Description | Cloud Provider | Complexity |
|---------|-------------|----------------|------------|
| [Simple Web App](simple-web-app.md) | Basic web application with database | AWS | Beginner |
| [Microservices](microservices.md) | Multi-service architecture with queues | AWS | Intermediate |
| [Serverless](serverless.md) | Lambda-based application | AWS | Intermediate |
| [Multi-Cloud](multi-cloud.md) | GCP and Azure migration examples | GCP, Azure | Advanced |

## Quick Start

Each example includes:

1. **Architecture Diagram** - Visual representation of the infrastructure
2. **Terraform Configuration** - Input files you would provide
3. **CloudExit Command** - The migration command to run
4. **Generated Output** - What CloudExit produces
5. **Deployment Steps** - How to deploy the generated stack
6. **Verification** - How to verify the migration worked

## Running the Examples

### Prerequisites

- CloudExit installed (`brew install cloudexit/tap/cloudexit`)
- Docker and Docker Compose
- Terraform files from your cloud provider

### General Workflow

```bash
# 1. Analyze your infrastructure
cloudexit analyze ./terraform --format table

# 2. Generate self-hosted stack
cloudexit migrate ./terraform --output ./my-stack --domain example.com

# 3. Review generated files
ls -la ./my-stack

# 4. Configure environment
cd my-stack
cp .env.example .env
# Edit .env with your settings

# 5. Deploy
docker compose up -d

# 6. Verify
docker compose ps
curl http://localhost
```

## Customizing Examples

All examples can be customized by:

1. **Modifying `.cloudexit.yaml`** - Change Docker images, ports, settings
2. **Editing `docker-compose.yml`** - Add services, volumes, networks
3. **Updating environment** - Configure via `.env` file
4. **Adding Traefik routes** - Modify `traefik/dynamic/` for routing

## Getting Help

- Review the [Architecture Guide](../architecture.md)
- Check service-specific docs: [AWS](../aws-services.md), [GCP](../gcp-services.md), [Azure](../azure-services.md)
- Open an issue on GitHub
