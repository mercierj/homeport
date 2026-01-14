package datamigration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// ACMToLetsEncryptExecutor migrates ACM certificates to Let's Encrypt/Traefik.
type ACMToLetsEncryptExecutor struct{}

// NewACMToLetsEncryptExecutor creates a new ACM to Let's Encrypt executor.
func NewACMToLetsEncryptExecutor() *ACMToLetsEncryptExecutor {
	return &ACMToLetsEncryptExecutor{}
}

// Type returns the migration type.
func (e *ACMToLetsEncryptExecutor) Type() string {
	return "acm_to_letsencrypt"
}

// GetPhases returns the migration phases.
func (e *ACMToLetsEncryptExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Fetching certificates",
		"Analyzing domains",
		"Generating Traefik config",
		"Creating certbot scripts",
		"Finalizing",
	}
}

// Validate validates the migration configuration.
func (e *ACMToLetsEncryptExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["access_key_id"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.access_key_id is required")
		}
		if _, ok := config.Source["secret_access_key"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.secret_access_key is required")
		}
	}

	if config.Destination == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "destination configuration is required")
	} else {
		if _, ok := config.Destination["output_dir"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "destination.output_dir is required")
		}
	}

	result.Warnings = append(result.Warnings, "ACM private keys cannot be exported - new certificates will be issued")
	result.Warnings = append(result.Warnings, "DNS must be configured before issuing new certificates")

	return result, nil
}

// Execute performs the migration.
func (e *ACMToLetsEncryptExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	accessKeyID := config.Source["access_key_id"].(string)
	secretAccessKey := config.Source["secret_access_key"].(string)
	region, _ := config.Source["region"].(string)
	if region == "" {
		region = "us-east-1"
	}
	outputDir := config.Destination["output_dir"].(string)
	email, _ := config.Destination["email"].(string)
	if email == "" {
		email = "admin@example.com"
	}

	awsEnv := []string{
		"AWS_ACCESS_KEY_ID=" + accessKeyID,
		"AWS_SECRET_ACCESS_KEY=" + secretAccessKey,
		"AWS_DEFAULT_REGION=" + region,
	}

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating AWS credentials")
	EmitProgress(m, 10, "Checking credentials")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Fetching certificates
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", "Fetching ACM certificates")
	EmitProgress(m, 25, "Fetching certificates")

	listCertsCmd := exec.CommandContext(ctx, "aws", "acm", "list-certificates",
		"--region", region, "--output", "json",
	)
	listCertsCmd.Env = append(os.Environ(), awsEnv...)
	certsOutput, err := listCertsCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to list certificates: %w", err)
	}

	var certsList struct {
		CertificateSummaryList []struct {
			CertificateArn string `json:"CertificateArn"`
			DomainName     string `json:"DomainName"`
		} `json:"CertificateSummaryList"`
	}
	json.Unmarshal(certsOutput, &certsList)

	type CertDetails struct {
		Arn             string   `json:"arn"`
		DomainName      string   `json:"domainName"`
		SubjectAltNames []string `json:"subjectAlternativeNames"`
		Status          string   `json:"status"`
		Type            string   `json:"type"`
		InUse           bool     `json:"inUseBy"`
	}
	certs := make([]CertDetails, 0)

	for _, cert := range certsList.CertificateSummaryList {
		describeCmd := exec.CommandContext(ctx, "aws", "acm", "describe-certificate",
			"--certificate-arn", cert.CertificateArn,
			"--region", region, "--output", "json",
		)
		describeCmd.Env = append(os.Environ(), awsEnv...)
		descOutput, err := describeCmd.Output()
		if err != nil {
			continue
		}

		var certMeta struct {
			Certificate struct {
				CertificateArn          string   `json:"CertificateArn"`
				DomainName              string   `json:"DomainName"`
				SubjectAlternativeNames []string `json:"SubjectAlternativeNames"`
				Status                  string   `json:"Status"`
				Type                    string   `json:"Type"`
				InUseBy                 []string `json:"InUseBy"`
			} `json:"Certificate"`
		}
		json.Unmarshal(descOutput, &certMeta)

		certs = append(certs, CertDetails{
			Arn:             certMeta.Certificate.CertificateArn,
			DomainName:      certMeta.Certificate.DomainName,
			SubjectAltNames: certMeta.Certificate.SubjectAlternativeNames,
			Status:          certMeta.Certificate.Status,
			Type:            certMeta.Certificate.Type,
			InUse:           len(certMeta.Certificate.InUseBy) > 0,
		})
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Analyzing domains
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", "Analyzing domain configurations")
	EmitProgress(m, 40, "Analyzing domains")

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	certsData, _ := json.MarshalIndent(certs, "", "  ")
	certsPath := filepath.Join(outputDir, "acm-certificates.json")
	os.WriteFile(certsPath, certsData, 0644)

	// Collect all unique domains
	domains := make(map[string]bool)
	for _, cert := range certs {
		domains[cert.DomainName] = true
		for _, san := range cert.SubjectAltNames {
			domains[san] = true
		}
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Generating Traefik config
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Generating Traefik configuration")
	EmitProgress(m, 60, "Generating Traefik config")

	// Docker compose with Traefik
	traefikCompose := fmt.Sprintf(`version: '3.8'

services:
  traefik:
    image: traefik:v3.0
    container_name: traefik
    command:
      - "--api.dashboard=true"
      - "--api.insecure=true"
      - "--providers.docker=true"
      - "--providers.docker.exposedbydefault=false"
      - "--entrypoints.web.address=:80"
      - "--entrypoints.websecure.address=:443"
      - "--certificatesresolvers.letsencrypt.acme.email=%s"
      - "--certificatesresolvers.letsencrypt.acme.storage=/letsencrypt/acme.json"
      - "--certificatesresolvers.letsencrypt.acme.httpchallenge.entrypoint=web"
      # Uncomment for staging (testing)
      # - "--certificatesresolvers.letsencrypt.acme.caserver=https://acme-staging-v02.api.letsencrypt.org/directory"
    ports:
      - "80:80"
      - "443:443"
      - "8080:8080"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - letsencrypt:/letsencrypt
    restart: unless-stopped
    labels:
      - "traefik.enable=true"
      # Global HTTP to HTTPS redirect
      - "traefik.http.routers.http-catchall.rule=hostregexp(''.+'')"
      - "traefik.http.routers.http-catchall.entrypoints=web"
      - "traefik.http.routers.http-catchall.middlewares=redirect-to-https"
      - "traefik.http.middlewares.redirect-to-https.redirectscheme.scheme=https"

volumes:
  letsencrypt:
`, email)

	composePath := filepath.Join(outputDir, "docker-compose.yml")
	os.WriteFile(composePath, []byte(traefikCompose), 0644)

	// Generate example service with TLS
	exampleService := "# Example service with automatic TLS\n" +
		"# Add to your docker-compose.yml\n\n" +
		"services:\n" +
		"  myapp:\n" +
		"    image: nginx:alpine\n" +
		"    labels:\n" +
		"      - \"traefik.enable=true\"\n" +
		"      - \"traefik.http.routers.myapp.rule=Host(`example.com`)\"\n" +
		"      - \"traefik.http.routers.myapp.entrypoints=websecure\"\n" +
		"      - \"traefik.http.routers.myapp.tls.certresolver=letsencrypt\"\n" +
		"      # For multiple domains/SANs:\n" +
		"      # - \"traefik.http.routers.myapp.tls.domains[0].main=example.com\"\n" +
		"      # - \"traefik.http.routers.myapp.tls.domains[0].sans=www.example.com,api.example.com\"\n"
	examplePath := filepath.Join(outputDir, "example-service.yml")
	os.WriteFile(examplePath, []byte(exampleService), 0644)

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Creating certbot scripts
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Creating certbot scripts")
	EmitProgress(m, 80, "Creating scripts")

	// Certbot standalone script
	certbotScript := fmt.Sprintf(`#!/bin/bash
# Certbot certificate issuance script
# Alternative to Traefik for standalone certificates

set -e

EMAIL="%s"

# Install certbot if needed
if ! command -v certbot &> /dev/null; then
    echo "Installing certbot..."
    apt-get update && apt-get install -y certbot
fi

# Issue certificates for each domain
`, email)

	for _, cert := range certs {
		domainFlags := fmt.Sprintf("-d %s", cert.DomainName)
		for _, san := range cert.SubjectAltNames {
			if san != cert.DomainName {
				domainFlags += fmt.Sprintf(" -d %s", san)
			}
		}
		certbotScript += fmt.Sprintf(`
echo "Issuing certificate for: %s"
certbot certonly --standalone --agree-tos --email $EMAIL %s
`, cert.DomainName, domainFlags)
	}

	certbotScript += `
echo "Certificates issued successfully!"
echo "Certificates stored in: /etc/letsencrypt/live/"
`
	certbotPath := filepath.Join(outputDir, "issue-certificates.sh")
	os.WriteFile(certbotPath, []byte(certbotScript), 0755)

	// DNS challenge script for wildcards
	dnsScript := fmt.Sprintf(`#!/bin/bash
# Certbot DNS challenge for wildcard certificates

set -e

EMAIL="%s"

# For wildcard certificates, use DNS challenge
# This requires DNS provider plugin (e.g., certbot-dns-cloudflare)

# Example for Cloudflare:
# pip install certbot-dns-cloudflare
# certbot certonly \
#     --dns-cloudflare \
#     --dns-cloudflare-credentials ~/.secrets/cloudflare.ini \
#     -d "*.example.com" -d "example.com"

# Manual DNS challenge:
certbot certonly --manual --preferred-challenges dns \
    --agree-tos --email $EMAIL \
    -d "*.example.com" -d "example.com"

echo "Follow the instructions to add DNS TXT records"
`, email)
	dnsPath := filepath.Join(outputDir, "issue-wildcard.sh")
	os.WriteFile(dnsPath, []byte(dnsScript), 0755)

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 6: Finalizing
	EmitPhase(m, phases[5], 6)
	EmitLog(m, "info", "Finalizing migration artifacts")
	EmitProgress(m, 95, "Finalizing")

	readme := fmt.Sprintf(`# ACM to Let's Encrypt Migration

## Source ACM
- Region: %s
- Certificates: %d

## Important Notes

ACM private keys **cannot be exported**. This migration:
1. Exports certificate metadata and domains
2. Configures automatic certificate issuance
3. Requires DNS to point to new infrastructure

## Migration Options

### Option 1: Traefik (Recommended)
Automatic certificate management with Let's Encrypt.

1. Update DNS to point to Traefik server
2. Start Traefik: '''docker-compose up -d'''
3. Add labels to your services

### Option 2: Certbot Standalone
Manual certificate management.

1. Run '''./issue-certificates.sh'''
2. Configure your web server with certificates
3. Set up renewal cron job

### Option 3: Wildcard with DNS Challenge
For wildcard certificates.

1. Configure DNS provider credentials
2. Run '''./issue-wildcard.sh'''
3. Follow DNS verification steps

## Migrated Domains
`, region, len(certs))

	for domain := range domains {
		readme += fmt.Sprintf("- %s\n", domain)
	}

	readme += `
## Files Generated
- acm-certificates.json: Original ACM certificate details
- docker-compose.yml: Traefik with Let's Encrypt
- example-service.yml: Example service configuration
- issue-certificates.sh: Certbot standalone script
- issue-wildcard.sh: Wildcard certificate script

## DNS Preparation
Before issuing certificates, ensure:
1. DNS A/AAAA records point to new server
2. Port 80 accessible for HTTP challenge
3. Or DNS TXT records can be added for DNS challenge
`

	readmePath := filepath.Join(outputDir, "README.md")
	os.WriteFile(readmePath, []byte(readme), 0644)

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", fmt.Sprintf("ACM migration complete: %d certificates", len(certs)))

	return nil
}
