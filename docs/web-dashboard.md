# Web Dashboard Guide

The Homeport Web Dashboard provides a comprehensive interface for managing cloud migrations and self-hosted infrastructure.

## Quick Start

```bash
# Build with web UI
make build-with-web

# Start the dashboard
./bin/homeport serve

# Open in browser
open http://localhost:8080
```

## Server Options

| Flag | Description | Default |
|------|-------------|---------|
| `--port, -p` | Port to listen on | 8080 |
| `--host, -H` | Host to bind to | localhost |
| `--no-auth` | Disable authentication (dev only) | false |

```bash
# Bind to all interfaces on port 3000
homeport serve --host 0.0.0.0 --port 3000
```

## Dashboard Pages

### Migration Wizard

The 7-step migration wizard guides you through the complete cloud-to-self-hosted migration process:

1. **Analyze** - Upload Terraform state/config and analyze your cloud infrastructure
2. **Export** - Review detected resources and configure mapping options
3. **Upload** - Upload additional configuration files
4. **Secrets** - Configure secrets and environment variables
5. **Deploy** - Generate and deploy Docker Compose stack
6. **Sync** - Synchronize data from cloud services (S3, databases)
7. **Cutover** - Final production switchover

Access: **Dashboard → Migrate**

### Stacks Management

Manage Docker Compose stacks and their lifecycle:

- View all deployed stacks
- Start/stop/restart containers
- View container logs in real-time
- Access container shell via web terminal
- Monitor resource usage

Access: **Dashboard → Stacks**

### Database Management

Manage database services:

- PostgreSQL, MySQL, MongoDB connections
- Run queries via web interface
- View table schemas
- Monitor connections and performance

Access: **Dashboard → Database**

### Storage Browser

S3-compatible storage management (MinIO):

- Browse buckets and objects
- Upload/download files
- Create and configure buckets
- Manage access policies

Access: **Dashboard → Storage**

### Cache Management

Redis and Memcached inspection:

- View cache keys
- Get/set values
- Monitor memory usage
- Flush cache

Access: **Dashboard → Cache**

### Queue Management

RabbitMQ message queue operations:

- View queue depths
- Inspect messages
- Purge queues
- Monitor consumer status

Access: **Dashboard → Queues**

### Functions (Serverless)

Manage serverless functions:

- Deploy function containers
- View invocation logs
- Scale functions
- Configure triggers

Access: **Dashboard → Functions**

### Web Terminal

Interactive shell access to containers:

- Select container from dropdown
- Full terminal emulation (xterm.js)
- Command history
- Copy/paste support

Access: **Dashboard → Terminal**

### Identity Management

User and service account management:

- Create/delete users
- Manage service accounts
- Configure authentication providers
- Session management

Access: **Dashboard → Identity**

### Certificate Management

SSL/TLS certificate operations:

- View installed certificates
- Request Let's Encrypt certificates
- Upload custom certificates
- Monitor expiration dates

Access: **Dashboard → Certificates**

### Secrets Management

Secure credential storage:

- Store API keys and passwords
- Reference secrets in deployments
- Rotate credentials
- Audit access logs

Access: **Dashboard → Secrets**

### Policy Management

RBAC policy configuration:

- Define roles and permissions
- Assign policies to users
- Audit policy violations

Access: **Dashboard → Policies**

### DNS Management

DNS zone and record management:

- Create DNS zones
- Manage A, AAAA, CNAME, MX, TXT records
- Configure TTL values

Access: **Dashboard → DNS**

### Logs

Full-text log search and exploration:

- Search across all containers
- Filter by service, level, time range
- Real-time log streaming
- Export logs

Access: **Dashboard → Logs**

### Metrics

Real-time monitoring dashboard:

- CPU, memory, disk usage
- Container health status
- Network I/O
- Custom metric queries

Access: **Dashboard → Metrics**

### Backup Management

Backup scheduling and restoration:

- Schedule automated backups
- View backup history
- Restore from backup
- Configure retention policies

Access: **Dashboard → Backup**

## Development Mode

For frontend development with hot-reload:

```bash
# Terminal 1: Start backend API
make build && ./bin/homeport serve --port 8080

# Terminal 2: Start frontend dev server
cd web && npm install && npm run dev
```

The Vite dev server runs on port 5173 and proxies API requests to port 8080.

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `VITE_API_URL` | Backend API URL | `http://localhost:8080` |

## Architecture

### Backend

- **Framework**: Chi router (Go)
- **Authentication**: Session cookies + Bearer tokens
- **WebSocket**: Real-time logs and metrics streaming
- **Static Files**: Embedded in binary via `go:embed`

### Frontend

- **Framework**: React 19 + TypeScript
- **Routing**: React Router 7
- **State**: Zustand + TanStack Query
- **Styling**: Tailwind CSS
- **Icons**: Lucide React
- **Terminal**: xterm.js

### API Structure

```
/api/v1/
├── migrate/          # Migration wizard endpoints
├── deploy/           # Deployment operations
├── stacks/           # Stack management
│   └── {id}/
│       ├── containers/
│       ├── logs/
│       ├── metrics/
│       └── ...
├── functions/        # Serverless functions
├── dns/              # DNS management
├── backup/           # Backup operations
├── identity/         # User management
├── policies/         # RBAC policies
└── ...
```

## Production Deployment

### Reverse Proxy (nginx)

```nginx
server {
    listen 443 ssl;
    server_name homeport.example.com;

    ssl_certificate /etc/ssl/certs/homeport.crt;
    ssl_certificate_key /etc/ssl/private/homeport.key;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

### Docker Deployment

```yaml
version: '3.8'
services:
  homeport:
    image: mercierj/homeport:latest
    ports:
      - "8080:8080"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - ./data:/data
    environment:
      - HOMEPORT_HOST=0.0.0.0
      - HOMEPORT_PORT=8080
```

## Troubleshooting

### Dashboard won't load

1. Ensure the binary was built with `make build-with-web`
2. Check if port 8080 is available
3. Verify static files exist in `internal/api/static/`

### WebSocket connection failed

1. Check if reverse proxy supports WebSocket upgrades
2. Verify firewall rules allow WebSocket connections
3. Check browser console for connection errors

### API returns 401 Unauthorized

1. Clear browser cookies and re-login
2. Check if `--no-auth` flag is set for development
3. Verify token hasn't expired

## See Also

- [API Reference](api-reference.md) - REST API documentation
- [Architecture](architecture.md) - Technical architecture overview
- [Contributing](contributing.md) - How to contribute to the project
