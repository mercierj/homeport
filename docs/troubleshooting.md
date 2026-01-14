# Troubleshooting Guide

This guide covers common issues and their solutions when using Homeport.

## Table of Contents

- [Installation Issues](#installation-issues)
- [Authentication Issues](#authentication-issues)
- [API Server Issues](#api-server-issues)
- [Web Dashboard Issues](#web-dashboard-issues)
- [Cloud Provider Issues](#cloud-provider-issues)
- [Docker & Container Issues](#docker--container-issues)
- [TLS/HTTPS Issues](#tlshttps-issues)
- [Migration Issues](#migration-issues)
- [Performance Issues](#performance-issues)

---

## Installation Issues

### Binary not found after installation

**Symptom:** Running `homeport` returns "command not found"

**Solutions:**
1. Ensure the binary is in your PATH:
   ```bash
   # Check if homeport is in PATH
   which homeport

   # Add to PATH if using Homebrew
   export PATH="/opt/homebrew/bin:$PATH"

   # Or for manual installation
   export PATH="$HOME/.local/bin:$PATH"
   ```

2. Verify the binary has execute permissions:
   ```bash
   chmod +x /path/to/homeport
   ```

### Go build fails with module errors

**Symptom:** `go build` fails with missing module errors

**Solution:**
```bash
# Clean module cache
go clean -modcache

# Download dependencies
go mod download

# Tidy modules
go mod tidy

# Retry build
go build ./cmd/homeport
```

---

## Authentication Issues

### "ADMIN_PASSWORD environment variable not set" error

**Symptom:** Server fails to start with admin password error

**Solution:**
Set the `ADMIN_PASSWORD` environment variable before starting the server:
```bash
# Linux/macOS
export ADMIN_PASSWORD="your-secure-password"
homeport serve

# Or inline
ADMIN_PASSWORD="your-secure-password" homeport serve
```

**Requirements:**
- Password must be at least 8 characters
- Required only on first startup to create admin user

### "Invalid credentials" when logging in

**Symptom:** Cannot log in to web dashboard despite correct password

**Solutions:**
1. Check if the password meets minimum requirements (8+ characters)
2. Reset the user database:
   ```bash
   rm ~/.homeport/users.json
   # Restart server with new ADMIN_PASSWORD
   ```
3. Check for whitespace in password (avoid trailing spaces)

### Token expired or invalid

**Symptom:** API requests fail with 401 Unauthorized

**Solutions:**
1. Log in again to get a fresh token
2. Check token expiry setting:
   ```yaml
   # In .homeport.yaml
   auth:
     token_expiry: "24h"
   ```
3. Verify the token is being sent in the Authorization header:
   ```bash
   curl -H "Authorization: Bearer YOUR_TOKEN" http://localhost:8080/api/v1/...
   ```

---

## API Server Issues

### "Address already in use" error

**Symptom:** Server fails to start because port is in use

**Solutions:**
1. Find and kill the process using the port:
   ```bash
   # Find process
   lsof -i :8080

   # Kill process
   kill -9 <PID>
   ```

2. Use a different port:
   ```bash
   homeport serve --port 8081
   ```

### CORS errors in browser console

**Symptom:** API requests fail with CORS policy errors

**Solutions:**
1. Ensure the frontend origin is allowed. Default allowed origins:
   - `http://localhost:5173`
   - `http://localhost:5174`
   - `http://localhost:3000`

2. For custom domains, configure CORS in `.homeport.yaml`:
   ```yaml
   server:
     cors_origins:
       - "https://your-domain.com"
   ```

3. For development, disable CORS checking in browser (not recommended for production)

### Server crashes with "out of memory"

**Symptom:** Server crashes when processing large files

**Solutions:**
1. Increase available memory
2. Process smaller batches of resources
3. Use the `--quiet` flag to reduce logging overhead:
   ```bash
   homeport serve --quiet
   ```

---

## Web Dashboard Issues

### Dashboard shows blank page

**Symptom:** Web dashboard loads but shows nothing

**Solutions:**
1. Check browser console for JavaScript errors (F12)
2. Clear browser cache and reload
3. Verify the API is running:
   ```bash
   curl http://localhost:8080/health
   ```
4. Check if assets are being served correctly

### "Failed to fetch" errors

**Symptom:** Dashboard shows network errors

**Solutions:**
1. Verify API server is running on the expected port
2. Check VITE_API_URL configuration:
   ```bash
   # In .env
   VITE_API_URL=http://localhost:8080
   ```
3. Check for browser network blocking (ad blockers, firewalls)

### Frontend development server won't start

**Symptom:** `npm run dev` fails

**Solutions:**
```bash
cd web

# Clear node_modules and reinstall
rm -rf node_modules package-lock.json
npm install

# Clear Vite cache
rm -rf node_modules/.vite

# Retry
npm run dev
```

---

## Cloud Provider Issues

### AWS: "Unable to locate credentials"

**Symptom:** AWS API scanning fails with credential errors

**Solutions:**
1. Configure AWS credentials:
   ```bash
   # Option 1: AWS CLI configuration
   aws configure

   # Option 2: Environment variables
   export AWS_ACCESS_KEY_ID="your-key"
   export AWS_SECRET_ACCESS_KEY="your-secret"
   export AWS_REGION="us-east-1"

   # Option 3: AWS profile
   homeport analyze --source aws-api --profile my-profile
   ```

2. Verify credentials are valid:
   ```bash
   aws sts get-caller-identity
   ```

### AWS: Access denied errors

**Symptom:** Scanning returns "AccessDenied" for some resources

**Solution:**
Ensure IAM user/role has required permissions. Minimum required policies:
- `AmazonEC2ReadOnlyAccess`
- `AmazonRDSReadOnlyAccess`
- `AmazonS3ReadOnlyAccess`
- `AWSLambda_ReadOnlyAccess`

Or use a custom policy with `Describe*` and `List*` actions.

### Azure: Authentication failed

**Symptom:** Azure API scanning fails with auth errors

**Solutions:**
1. Configure service principal credentials:
   ```bash
   export AZURE_TENANT_ID="your-tenant-id"
   export AZURE_CLIENT_ID="your-client-id"
   export AZURE_CLIENT_SECRET="your-client-secret"
   export AZURE_SUBSCRIPTION_ID="your-subscription-id"
   ```

2. Use Azure CLI authentication:
   ```bash
   az login
   ```

### GCP: "Could not find default credentials"

**Symptom:** GCP API scanning fails with credential errors

**Solutions:**
1. Set up application default credentials:
   ```bash
   gcloud auth application-default login
   ```

2. Or use a service account key:
   ```bash
   export GOOGLE_APPLICATION_CREDENTIALS="/path/to/service-account.json"
   ```

3. Specify project explicitly:
   ```bash
   homeport analyze --source gcp-api --project my-project-id
   ```

---

## Docker & Container Issues

### Docker Compose services won't start

**Symptom:** `docker compose up` fails

**Solutions:**
1. Check Docker daemon is running:
   ```bash
   docker info
   ```

2. Check for port conflicts:
   ```bash
   docker compose ps
   netstat -tulpn | grep 8080
   ```

3. View service logs:
   ```bash
   docker compose logs api
   ```

4. Rebuild containers:
   ```bash
   docker compose build --no-cache
   docker compose up -d
   ```

### Container health check failing

**Symptom:** Container shows "unhealthy" status

**Solutions:**
1. Check container logs:
   ```bash
   docker logs homeport-api
   ```

2. Test health endpoint manually:
   ```bash
   docker exec homeport-api wget -q --spider http://localhost:8080/health
   ```

3. Increase health check timeouts in docker-compose.yml

### Volume permission errors

**Symptom:** Container can't write to mounted volumes

**Solutions:**
1. Check volume ownership:
   ```bash
   ls -la ~/.homeport/
   ```

2. Fix permissions:
   ```bash
   sudo chown -R 1000:1000 ~/.homeport/
   ```

3. On macOS/Windows, ensure Docker has file sharing enabled

---

## TLS/HTTPS Issues

### Certificate not trusted

**Symptom:** Browser shows "connection not secure" warning

**Solutions:**
1. For Let's Encrypt, ensure:
   - Domain points to server IP
   - Port 80 and 443 are accessible
   - Valid email is configured

2. For self-signed certificates, add to system trust:
   ```bash
   # macOS
   sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain cert.pem

   # Linux
   sudo cp cert.pem /usr/local/share/ca-certificates/
   sudo update-ca-certificates
   ```

### Let's Encrypt rate limits

**Symptom:** Certificate issuance fails with rate limit error

**Solutions:**
1. Use staging environment for testing:
   ```yaml
   tls:
     auto:
       staging: true
   ```

2. Wait for rate limit to reset (typically 1 week)
3. Check existing certificates at https://crt.sh

### TLS handshake errors

**Symptom:** Clients can't connect with TLS

**Solutions:**
1. Verify certificate and key match:
   ```bash
   openssl x509 -noout -modulus -in cert.pem | md5
   openssl rsa -noout -modulus -in key.pem | md5
   # Outputs should match
   ```

2. Check certificate chain is complete
3. Verify TLS version compatibility:
   ```yaml
   tls:
     min_version: "1.2"  # Try lowering if clients are old
   ```

---

## Migration Issues

### Generated Docker Compose is invalid

**Symptom:** `docker compose config` fails on generated output

**Solutions:**
1. Validate the generated compose file:
   ```bash
   docker compose -f output/docker-compose.yml config
   ```

2. Check for YAML syntax errors
3. Run Homeport validation:
   ```bash
   homeport validate output/
   ```

### Missing environment variables in output

**Symptom:** Generated services reference undefined variables

**Solution:**
Create `.env` file with required variables, or use inline values:
```bash
# Check what variables are needed
grep -r '\${' output/docker-compose.yml
```

### Service dependencies not starting correctly

**Symptom:** Services fail because dependencies aren't ready

**Solutions:**
1. Add proper health checks to dependencies
2. Use `depends_on` with `condition: service_healthy`
3. Add startup delays or retry logic

---

## Performance Issues

### Slow analysis of large infrastructure

**Symptom:** Analysis takes too long for large environments

**Solutions:**
1. Filter by region to reduce scope:
   ```bash
   homeport analyze --source aws-api --region us-east-1
   ```

2. Use parallel processing (enabled by default)
3. Increase available memory
4. Use quiet mode to reduce logging overhead

### High memory usage

**Symptom:** Homeport uses excessive memory

**Solutions:**
1. Process resources in batches by region
2. Reduce verbose logging
3. Increase swap space if on limited hardware

### Slow web dashboard

**Symptom:** Dashboard is sluggish

**Solutions:**
1. Check browser dev tools for slow API calls
2. Reduce the number of resources displayed per page
3. Enable browser caching for static assets

---

## Getting Help

If your issue isn't covered here:

1. **Check logs** - Run with `--verbose` for detailed output
2. **Search issues** - Check [GitHub Issues](https://github.com/homeport/homeport/issues)
3. **Open new issue** - Include:
   - Homeport version (`homeport version`)
   - Operating system
   - Steps to reproduce
   - Relevant log output (use `--verbose`)
   - Configuration (redact sensitive values)

---

## Diagnostic Commands

Quick commands for debugging:

```bash
# Version info
homeport version

# Verbose server startup
homeport serve --verbose

# Test API health
curl http://localhost:8080/health

# Check Docker status
docker compose ps
docker compose logs --tail=50

# Validate configuration
homeport validate ./output/

# Test AWS credentials
aws sts get-caller-identity

# Test Azure credentials
az account show

# Test GCP credentials
gcloud auth list
```
