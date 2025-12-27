# Production Deployment Guide

This guide provides comprehensive instructions for deploying Homeport-generated stacks to production environments.

## Table of Contents

- [Production Readiness Checklist](#production-readiness-checklist)
- [System Requirements](#system-requirements)
- [Deployment Methods](#deployment-methods)
- [SSL/TLS Configuration](#ssltls-configuration)
- [Environment Configuration](#environment-configuration)
- [Monitoring and Logging](#monitoring-and-logging)
- [Backup Strategy](#backup-strategy)
- [Scaling Guidelines](#scaling-guidelines)
- [Troubleshooting](#troubleshooting)

---

## Production Readiness Checklist

Before deploying to production, verify the following:

### Security

- [ ] All default passwords changed
- [ ] SSL/TLS certificates configured
- [ ] Firewall rules configured (only expose necessary ports)
- [ ] Secrets stored securely (not in docker-compose.yml)
- [ ] Docker socket not exposed to untrusted containers
- [ ] Non-root users configured for containers
- [ ] Network isolation between services implemented

### Reliability

- [ ] Health checks configured for all services
- [ ] Restart policies set (`unless-stopped` or `always`)
- [ ] Resource limits defined (memory, CPU)
- [ ] Backup strategy implemented and tested
- [ ] Log rotation configured
- [ ] Monitoring and alerting set up

### Performance

- [ ] Database connection pooling configured
- [ ] Appropriate instance sizes selected
- [ ] Caching layer configured (Redis)
- [ ] CDN configured for static assets
- [ ] Database indexes optimized

---

## System Requirements

### Minimum Requirements

| Component | Specification |
|-----------|--------------|
| CPU | 2 cores |
| RAM | 4 GB |
| Storage | 50 GB SSD |
| OS | Ubuntu 22.04 LTS, Debian 12, or RHEL 9 |
| Docker | 24.0+ |
| Docker Compose | 2.20+ |

### Recommended for Production

| Component | Small (< 1000 users) | Medium (1000-10000 users) | Large (> 10000 users) |
|-----------|---------------------|---------------------------|----------------------|
| CPU | 4 cores | 8 cores | 16+ cores |
| RAM | 8 GB | 16 GB | 32+ GB |
| Storage | 100 GB SSD | 500 GB SSD | 1+ TB NVMe |
| Network | 100 Mbps | 1 Gbps | 10 Gbps |

### Port Requirements

| Port | Service | Required |
|------|---------|----------|
| 80 | HTTP (redirect to HTTPS) | Yes |
| 443 | HTTPS | Yes |
| 22 | SSH (management) | Yes |
| 5432 | PostgreSQL (internal only) | No |
| 6379 | Redis (internal only) | No |
| 9000 | MinIO API (internal only) | No |

---

## Deployment Methods

### Method 1: Docker Compose (Recommended for Single Server)

#### Initial Setup

```bash
# Create application directory
sudo mkdir -p /opt/homeport
cd /opt/homeport

# Extract generated stack
unzip homeport-stack.zip

# Create environment file
cp .env.example .env
nano .env  # Edit with production values

# Set permissions
chmod 600 .env
chmod +x scripts/*.sh

# Create data directories
mkdir -p data/{postgres,redis,minio}

# Start services
docker compose up -d
```

#### Update Deployment

```bash
cd /opt/homeport

# Pull latest images
docker compose pull

# Apply updates with zero downtime
docker compose up -d --no-deps --build

# Verify health
docker compose ps
docker compose logs --tail=100
```

### Method 2: Docker Swarm (Multi-Node Cluster)

#### Initialize Swarm

```bash
# On manager node
docker swarm init --advertise-addr <MANAGER_IP>

# Join worker nodes (run on workers)
docker swarm join --token <TOKEN> <MANAGER_IP>:2377
```

#### Deploy Stack

```bash
# Create overlay network
docker network create --driver overlay homeport-net

# Deploy using stack file
docker stack deploy -c docker-compose.yml homeport

# Check services
docker stack services homeport
docker stack ps homeport
```

### Method 3: Kubernetes

#### Using kubectl

```bash
# Create namespace
kubectl create namespace homeport

# Apply secrets
kubectl apply -f k8s/secrets.yaml -n homeport

# Apply configurations
kubectl apply -f k8s/ -n homeport

# Verify deployment
kubectl get pods -n homeport
kubectl get services -n homeport
```

#### Using Helm

```bash
# Add Homeport Helm repository
helm repo add homeport https://charts.homeport.local

# Install with custom values
helm install myapp homeport/stack \
  --namespace homeport \
  --create-namespace \
  -f values-production.yaml
```

---

## SSL/TLS Configuration

### Option 1: Let's Encrypt (Automatic)

Traefik automatically obtains and renews certificates:

```yaml
# traefik/traefik.yml
certificatesResolvers:
  letsencrypt:
    acme:
      email: admin@yourdomain.com
      storage: /letsencrypt/acme.json
      httpChallenge:
        entryPoint: web
```

Ensure:
- Domain DNS points to your server
- Ports 80 and 443 are accessible from the internet
- Email address is valid for expiration notices

### Option 2: Custom Certificates

```yaml
# traefik/dynamic/certs.yml
tls:
  certificates:
    - certFile: /certs/yourdomain.com.crt
      keyFile: /certs/yourdomain.com.key
  stores:
    default:
      defaultCertificate:
        certFile: /certs/yourdomain.com.crt
        keyFile: /certs/yourdomain.com.key
```

Mount certificates in docker-compose.yml:

```yaml
services:
  traefik:
    volumes:
      - ./certs:/certs:ro
```

### Option 3: Cloudflare Proxy

Configure Cloudflare as a proxy:

1. Set DNS to proxy mode (orange cloud)
2. Configure SSL mode to "Full (strict)"
3. Generate Origin Certificate in Cloudflare dashboard
4. Install Origin Certificate on server

---

## Environment Configuration

### Required Environment Variables

Create `.env` file with production values:

```bash
# Application
APP_ENV=production
APP_DEBUG=false
APP_URL=https://yourdomain.com

# Database
POSTGRES_HOST=postgres
POSTGRES_PORT=5432
POSTGRES_DB=homeport
POSTGRES_USER=homeport
POSTGRES_PASSWORD=<strong-random-password>

# Redis
REDIS_HOST=redis
REDIS_PORT=6379
REDIS_PASSWORD=<strong-random-password>

# MinIO
MINIO_ROOT_USER=<access-key>
MINIO_ROOT_PASSWORD=<secret-key>

# Authentication
JWT_SECRET=<random-256-bit-key>
SESSION_SECRET=<random-256-bit-key>

# Traefik
TRAEFIK_ACME_EMAIL=admin@yourdomain.com
TRAEFIK_DASHBOARD_AUTH=admin:<htpasswd-hash>
```

### Generate Secure Passwords

```bash
# Generate random password
openssl rand -base64 32

# Generate htpasswd hash for Traefik
htpasswd -nb admin your-password
```

### Secrets Management Best Practices

1. **Never commit secrets to git**
2. **Use Docker secrets in Swarm mode**:
   ```yaml
   secrets:
     postgres_password:
       external: true
   ```
3. **Use HashiCorp Vault** for enterprise deployments
4. **Rotate secrets regularly** (every 90 days minimum)

---

## Monitoring and Logging

### Log Aggregation

Configure centralized logging:

```yaml
# docker-compose.yml
services:
  myapp:
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"
        labels: "service,environment"
```

For production, consider using:
- **Loki + Grafana** - Lightweight log aggregation
- **ELK Stack** - Elasticsearch, Logstash, Kibana
- **Datadog** - Commercial SaaS option

### Metrics and Alerting

The monitoring stack includes Prometheus and Grafana:

```bash
# Access Grafana
https://grafana.yourdomain.com

# Default credentials (change immediately!)
Username: admin
Password: <set in GRAFANA_PASSWORD>
```

#### Recommended Alerts

| Alert | Condition | Severity |
|-------|-----------|----------|
| HighCPU | CPU > 80% for 5m | Warning |
| HighMemory | Memory > 90% for 5m | Critical |
| DiskSpace | Disk > 85% | Warning |
| ServiceDown | Service unhealthy for 1m | Critical |
| HighLatency | p99 > 1s for 5m | Warning |
| ErrorRate | 5xx errors > 1% for 5m | Critical |

### Health Check Endpoints

| Endpoint | Purpose |
|----------|---------|
| `/health` | Basic liveness check |
| `/health/detailed` | Full health with dependencies |
| `/health/ready` | Kubernetes readiness probe |
| `/health/live` | Kubernetes liveness probe |

---

## Backup Strategy

### Automated Backups

Use the provided backup script:

```bash
# Run manually
./scripts/backup.sh

# Schedule with cron
crontab -e
# Add:
0 2 * * * /opt/homeport/scripts/backup.sh >> /var/log/homeport-backup.log 2>&1
```

### Backup Retention Policy

| Type | Frequency | Retention |
|------|-----------|-----------|
| Daily | Every day at 2 AM | 7 days |
| Weekly | Sunday at 3 AM | 4 weeks |
| Monthly | 1st of month | 12 months |

### Offsite Backup

Configure backup replication to cloud storage:

```bash
# Using rclone to sync to S3-compatible storage
rclone sync /backups remote:homeport-backups --config /etc/rclone.conf
```

### Recovery Testing

Test backups monthly:

```bash
# Create test environment
docker compose -f docker-compose.test.yml up -d

# Restore database
gunzip -c /backups/postgres/backup_latest.sql.gz | \
  docker compose exec -T postgres-test psql -U homeport

# Verify data integrity
./scripts/verify-backup.sh
```

---

## Scaling Guidelines

### Horizontal Scaling

#### Docker Compose (with load balancer)

```yaml
services:
  webapp:
    deploy:
      replicas: 3
      resources:
        limits:
          cpus: '1'
          memory: 1G
        reservations:
          cpus: '0.5'
          memory: 512M
```

#### Database Scaling

1. **Read replicas** for read-heavy workloads
2. **Connection pooling** with PgBouncer
3. **Vertical scaling** for write-heavy workloads

```yaml
services:
  pgbouncer:
    image: edoburu/pgbouncer
    environment:
      DATABASE_URL: postgres://user:pass@postgres/db
      POOL_MODE: transaction
      MAX_CLIENT_CONN: 1000
      DEFAULT_POOL_SIZE: 20
```

### Vertical Scaling

Adjust resource limits based on load:

```yaml
services:
  postgres:
    deploy:
      resources:
        limits:
          cpus: '4'
          memory: 8G
```

### CDN Integration

Offload static assets to a CDN:

```yaml
# traefik/dynamic/cdn.yml
http:
  middlewares:
    cdn-headers:
      headers:
        customResponseHeaders:
          Cache-Control: "public, max-age=31536000"
```

---

## Troubleshooting

### Common Issues

#### Service Won't Start

```bash
# Check logs
docker compose logs <service>

# Check resource usage
docker stats

# Verify configuration
docker compose config
```

#### Database Connection Issues

```bash
# Test connectivity
docker compose exec webapp nc -zv postgres 5432

# Check PostgreSQL logs
docker compose logs postgres

# Verify credentials
docker compose exec postgres psql -U homeport -c "SELECT 1"
```

#### SSL Certificate Issues

```bash
# Check certificate status
docker compose exec traefik cat /letsencrypt/acme.json | jq

# Force certificate renewal
docker compose restart traefik

# Check Traefik logs
docker compose logs traefik | grep -i certificate
```

#### High Memory Usage

```bash
# Identify memory-heavy containers
docker stats --no-stream

# Check for memory leaks
docker compose exec <service> top

# Restart affected service
docker compose restart <service>
```

### Debug Mode

Enable debug logging temporarily:

```yaml
# docker-compose.override.yml
services:
  traefik:
    command:
      - "--log.level=DEBUG"
  webapp:
    environment:
      - LOG_LEVEL=debug
```

### Getting Help

1. Check the [Troubleshooting Guide](troubleshooting.md)
2. Search [GitHub Issues](https://github.com/homeport/homeport/issues)
3. Join the community Discord
4. For enterprise support, contact support@homeport.local

---

## Quick Reference

### Essential Commands

```bash
# Start all services
docker compose up -d

# Stop all services
docker compose down

# View logs
docker compose logs -f

# Check status
docker compose ps

# Update services
docker compose pull && docker compose up -d

# Backup database
./scripts/backup.sh

# Access database shell
docker compose exec postgres psql -U homeport
```

### Important File Locations

| Path | Description |
|------|-------------|
| `/opt/homeport/` | Application root |
| `/opt/homeport/.env` | Environment configuration |
| `/opt/homeport/data/` | Persistent data volumes |
| `/opt/homeport/configs/` | Service configurations |
| `/opt/homeport/certs/` | SSL certificates |
| `/var/log/homeport/` | Application logs |
| `/backups/` | Backup storage |
