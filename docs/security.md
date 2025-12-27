# Security Hardening Guide

This document outlines security requirements and best practices for Homeport deployments.

## Table of Contents

- [Security Overview](#security-overview)
- [Authentication & Authorization](#authentication--authorization)
- [API Security](#api-security)
- [Database Security](#database-security)
- [Network Security](#network-security)
- [Container Security](#container-security)
- [Secrets Management](#secrets-management)
- [Logging & Auditing](#logging--auditing)
- [Compliance Considerations](#compliance-considerations)
- [Security Checklist](#security-checklist)

---

## Security Overview

Homeport follows security-by-default principles. This guide helps ensure your deployment meets security best practices.

### Security Model

```
                    ┌─────────────────────────────────────┐
                    │           Internet                  │
                    └─────────────────┬───────────────────┘
                                      │
                    ┌─────────────────▼───────────────────┐
                    │     Traefik (TLS Termination)       │
                    │     - Rate limiting                 │
                    │     - Security headers              │
                    │     - IP filtering                  │
                    └─────────────────┬───────────────────┘
                                      │
                    ┌─────────────────▼───────────────────┐
                    │        API Gateway                  │
                    │     - Authentication                │
                    │     - Input validation              │
                    │     - Request logging               │
                    └─────────────────┬───────────────────┘
                                      │
       ┌──────────────┬───────────────┴───────────────┬──────────────┐
       ▼              ▼                               ▼              ▼
  ┌─────────┐   ┌─────────┐                     ┌─────────┐   ┌─────────┐
  │PostgreSQL│   │  Redis  │                     │  MinIO  │   │ Docker  │
  │(Internal)│   │(Internal)│                     │(Internal)│   │ (Local) │
  └─────────┘   └─────────┘                     └─────────┘   └─────────┘
```

---

## Authentication & Authorization

### Session-Based Authentication

Homeport uses secure session-based authentication by default.

#### Configuration Requirements

```yaml
# Environment variables
SESSION_SECRET=<random-256-bit-key>    # Must be at least 32 bytes
SESSION_TIMEOUT=3600                    # 1 hour default
SESSION_SECURE=true                     # HTTPS only cookies
SESSION_SAME_SITE=strict               # CSRF protection
```

#### Security Headers

```yaml
# Automatically set on authenticated responses
Set-Cookie: session=<token>; HttpOnly; Secure; SameSite=Strict; Path=/
```

### Password Requirements

| Requirement | Minimum |
|-------------|---------|
| Length | 12 characters |
| Uppercase | 1 character |
| Lowercase | 1 character |
| Numbers | 1 digit |
| Special | 1 symbol |

### Password Hashing

Passwords are hashed using Argon2id with secure parameters:

```go
// Default parameters
Time:    3
Memory:  64 * 1024  // 64 MB
Threads: 4
KeyLen:  32
```

### Rate Limiting

Authentication endpoints are rate-limited to prevent brute force attacks:

| Endpoint | Limit | Window |
|----------|-------|--------|
| POST /api/v1/auth/login | 5 requests | 1 minute |
| POST /api/v1/auth/change-password | 3 requests | 1 minute |
| All authenticated endpoints | 100 requests | 1 minute |

### Account Lockout

After 5 consecutive failed login attempts, the account is locked for 15 minutes.

---

## API Security

### Input Validation

All API inputs are validated and sanitized:

- **JSON Schema Validation**: Request bodies validated against OpenAPI schema
- **Parameter Validation**: Query parameters and path variables sanitized
- **Size Limits**: Request body limited to 10MB by default

### SQL Injection Prevention

All database queries use parameterized statements:

```go
// Safe - uses parameterized query
db.Query("SELECT * FROM users WHERE id = $1", userID)

// Never used - vulnerable to injection
db.Query("SELECT * FROM users WHERE id = " + userID)
```

### SQL Query Restrictions

The query endpoint enforces:

- **Read-only by default**: Only SELECT statements allowed unless explicitly enabled
- **Timeout limits**: Queries timeout after 30 seconds
- **Row limits**: Maximum 1000 rows returned per query
- **Blocked operations**: DROP, TRUNCATE, ALTER disabled entirely

```go
// Blocked SQL patterns
var blockedPatterns = []string{
    "DROP",
    "TRUNCATE",
    "ALTER",
    "CREATE",
    "GRANT",
    "REVOKE",
    "--",       // SQL comments
    "/*",       // Block comments
    ";",        // Statement terminator (prevents injection)
}
```

### XSS Prevention

- All output is HTML-escaped by default
- Content-Security-Policy headers are set
- X-XSS-Protection header enabled

### CORS Configuration

```yaml
# Production CORS settings
CORS_ALLOWED_ORIGINS=https://yourdomain.com
CORS_ALLOWED_METHODS=GET,POST,PUT,DELETE,OPTIONS
CORS_ALLOWED_HEADERS=Content-Type,Authorization
CORS_MAX_AGE=3600
```

### Security Headers

The following headers are set on all responses:

```
Strict-Transport-Security: max-age=31536000; includeSubDomains; preload
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
X-XSS-Protection: 1; mode=block
Content-Security-Policy: default-src 'self'
Referrer-Policy: strict-origin-when-cross-origin
Permissions-Policy: geolocation=(), microphone=(), camera=()
```

---

## Database Security

### Connection Security

#### TLS/SSL

Enable SSL for all database connections:

```yaml
# PostgreSQL SSL configuration
POSTGRES_SSL_MODE=verify-full
POSTGRES_SSL_CERT=/certs/client.crt
POSTGRES_SSL_KEY=/certs/client.key
POSTGRES_SSL_ROOT_CERT=/certs/ca.crt
```

#### Connection String Security

```bash
# Never log connection strings
DATABASE_URL="postgresql://user:PASSWORD@host:5432/db?sslmode=verify-full"

# Use environment variables
POSTGRES_PASSWORD_FILE=/run/secrets/postgres_password
```

### User Privileges

Follow the principle of least privilege:

```sql
-- Application user (restricted)
CREATE ROLE app_user WITH LOGIN PASSWORD 'secure_password';
GRANT CONNECT ON DATABASE homeport TO app_user;
GRANT USAGE ON SCHEMA public TO app_user;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO app_user;

-- No admin privileges
REVOKE CREATE ON SCHEMA public FROM app_user;
REVOKE ALL ON DATABASE postgres FROM app_user;
```

### Data Encryption

#### At Rest

```yaml
# PostgreSQL encryption
postgres:
  volumes:
    - type: volume
      source: postgres_data
      target: /var/lib/postgresql/data
      volume:
        driver_opts:
          type: luks  # Encrypted volume
```

#### Transparent Data Encryption

For sensitive deployments, enable TDE:

```ini
# postgresql.conf
ssl = on
ssl_cert_file = '/certs/server.crt'
ssl_key_file = '/certs/server.key'
ssl_ca_file = '/certs/ca.crt'
```

### Backup Encryption

```bash
# Encrypt backups with GPG
pg_dump -U homeport homeport | gpg --encrypt --recipient backup@example.com > backup.sql.gpg
```

---

## Network Security

### Network Isolation

Use Docker networks to isolate services:

```yaml
networks:
  # Public-facing services
  frontend:
    driver: bridge

  # Internal services only
  backend:
    driver: bridge
    internal: true  # No external access

services:
  traefik:
    networks:
      - frontend

  api:
    networks:
      - frontend
      - backend

  postgres:
    networks:
      - backend  # Only internal access
```

### Firewall Rules

Recommended iptables rules:

```bash
# Allow established connections
iptables -A INPUT -m state --state ESTABLISHED,RELATED -j ACCEPT

# Allow SSH (management)
iptables -A INPUT -p tcp --dport 22 -j ACCEPT

# Allow HTTP/HTTPS
iptables -A INPUT -p tcp --dport 80 -j ACCEPT
iptables -A INPUT -p tcp --dport 443 -j ACCEPT

# Drop all other inbound
iptables -A INPUT -j DROP
```

### Port Exposure

| Port | Expose to Internet | Notes |
|------|-------------------|-------|
| 80/443 | Yes | Traefik only |
| 22 | VPN/Bastion only | SSH management |
| 5432 | Never | PostgreSQL |
| 6379 | Never | Redis |
| 9000/9001 | Never | MinIO |

### TLS Configuration

Use strong TLS settings:

```yaml
# traefik/dynamic/tls.yml
tls:
  options:
    default:
      minVersion: VersionTLS12
      cipherSuites:
        - TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384
        - TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256
        - TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384
        - TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256
      curvePreferences:
        - secp521r1
        - secp384r1
```

---

## Container Security

### Non-Root Users

Run containers as non-root:

```yaml
services:
  api:
    user: "1000:1000"
    security_opt:
      - no-new-privileges:true
```

### Read-Only Filesystem

```yaml
services:
  api:
    read_only: true
    tmpfs:
      - /tmp
      - /var/run
```

### Capability Restrictions

```yaml
services:
  api:
    cap_drop:
      - ALL
    cap_add:
      - NET_BIND_SERVICE  # Only if binding to port < 1024
```

### Resource Limits

```yaml
services:
  api:
    deploy:
      resources:
        limits:
          cpus: '1'
          memory: 512M
        reservations:
          cpus: '0.25'
          memory: 128M
```

### Image Security

- Use specific image tags (not `latest`)
- Scan images for vulnerabilities
- Use official or verified images
- Keep images updated

```bash
# Scan images with Trivy
trivy image postgres:16-alpine
```

---

## Secrets Management

### Environment File Security

```bash
# Restrict permissions
chmod 600 .env
chown root:root .env
```

### Docker Secrets (Swarm Mode)

```yaml
services:
  postgres:
    secrets:
      - postgres_password
    environment:
      POSTGRES_PASSWORD_FILE: /run/secrets/postgres_password

secrets:
  postgres_password:
    external: true
```

### Secrets Rotation

Implement regular rotation:

| Secret Type | Rotation Frequency |
|-------------|-------------------|
| Database passwords | 90 days |
| API keys | 90 days |
| TLS certificates | Automatic (Let's Encrypt) |
| Session secrets | 30 days |

### Never Store Secrets In:

- Git repositories
- Docker images
- Log files
- Environment variables in docker-compose.yml

---

## Logging & Auditing

### Security Events to Log

| Event | Log Level | Retention |
|-------|-----------|-----------|
| Login success | INFO | 90 days |
| Login failure | WARN | 180 days |
| Password change | INFO | 1 year |
| Permission denied | WARN | 180 days |
| API errors | ERROR | 90 days |
| Configuration changes | INFO | 1 year |

### Log Format

```json
{
  "timestamp": "2024-01-15T10:30:00Z",
  "level": "WARN",
  "event": "login_failure",
  "user": "admin",
  "ip": "192.168.1.100",
  "reason": "invalid_password",
  "request_id": "abc123"
}
```

### Audit Trail Requirements

For compliance, maintain:

- All authentication events
- Data access logs
- Configuration changes
- Administrative actions

### Log Protection

```yaml
# Prevent log tampering
logging:
  driver: "syslog"
  options:
    syslog-address: "tcp://logging-server:514"
    syslog-tls-cert: "/certs/client.crt"
    syslog-tls-key: "/certs/client.key"
```

---

## Compliance Considerations

### GDPR

- Implement data deletion capabilities
- Log all data access
- Encrypt personal data
- Document data processing

### SOC 2

- Enable audit logging
- Implement access controls
- Regular security assessments
- Incident response procedures

### HIPAA

- Enable encryption at rest
- Audit all PHI access
- Implement access controls
- Business Associate Agreements

---

## Security Checklist

### Pre-Deployment

- [ ] All default passwords changed
- [ ] SSL/TLS certificates configured
- [ ] Firewall rules applied
- [ ] Secrets stored securely
- [ ] Images scanned for vulnerabilities
- [ ] Network isolation configured

### Authentication

- [ ] Strong password policy enabled
- [ ] Rate limiting configured
- [ ] Session timeout configured
- [ ] Secure cookie flags set

### API Security

- [ ] Input validation enabled
- [ ] SQL injection protection verified
- [ ] XSS protection headers set
- [ ] CORS properly configured
- [ ] Rate limiting enabled

### Database Security

- [ ] SSL connections required
- [ ] Least privilege users created
- [ ] Encryption at rest enabled
- [ ] Backups encrypted

### Container Security

- [ ] Non-root users configured
- [ ] Read-only filesystems where possible
- [ ] Capabilities dropped
- [ ] Resource limits set

### Monitoring

- [ ] Security events logged
- [ ] Alerts configured
- [ ] Log retention configured
- [ ] Audit trail enabled

### Ongoing

- [ ] Regular security updates
- [ ] Vulnerability scanning
- [ ] Penetration testing (annual)
- [ ] Security training

---

## Reporting Security Issues

If you discover a security vulnerability, please report it responsibly:

1. **Do not** disclose publicly
2. Email security@homeport.local with details
3. Include steps to reproduce
4. Allow 90 days for remediation before disclosure

We appreciate responsible disclosure and will acknowledge your contribution.
