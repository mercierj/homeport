// Package messaging provides mappers for AWS messaging services.
package messaging

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

// SESMapper converts AWS SES identities to Postal mail server.
type SESMapper struct {
	*mapper.BaseMapper
}

// NewSESMapper creates a new SES to Postal mapper.
func NewSESMapper() *SESMapper {
	return &SESMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeSESIdentity, nil),
	}
}

// Map converts an SES identity to a Postal mail server service.
func (m *SESMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	identityName := res.Name
	identityType := res.GetConfigString("identity_type")

	result := mapper.NewMappingResult("postal")
	svc := result.DockerService

	// Postal mail server
	svc.Image = "ghcr.io/postalserver/postal:3"
	svc.Environment = map[string]string{
		"POSTAL_HOSTNAME":           "mail.localhost",
		"POSTAL_WEB_HOSTNAME":       "postal.localhost",
		"POSTAL_SECRET_KEY":         "${POSTAL_SECRET_KEY:-changeme}",
		"POSTAL_SMTP_RELAY_MODE":    "false",
		"MYSQL_HOST":                "postal-db",
		"MYSQL_DATABASE":            "postal",
		"MYSQL_USER":                "postal",
		"MYSQL_PASSWORD":            "${MYSQL_PASSWORD:-postal}",
		"RABBITMQ_HOST":             "postal-rabbitmq",
		"RABBITMQ_DEFAULT_USER":     "postal",
		"RABBITMQ_DEFAULT_PASS":     "${RABBITMQ_PASSWORD:-postal}",
	}
	svc.Ports = []string{
		"25:25",     // SMTP
		"587:587",   // Submission
		"465:465",   // SMTPS
		"5000:5000", // Web UI
	}
	svc.Volumes = []string{
		"./data/postal/storage:/opt/postal/storage",
		"./data/postal/assets:/opt/postal/public/assets",
		"./config/postal/postal.yml:/opt/postal/config/postal.yml:ro",
		"./config/postal/signing.key:/opt/postal/config/signing.key:ro",
	}
	svc.Networks = []string{"homeport"}
	svc.DependsOn = []string{"postal-db", "postal-rabbitmq"}
	svc.Labels = map[string]string{
		"homeport.source":        "aws_ses_identity",
		"homeport.identity_name": identityName,
		"homeport.identity_type": identityType,
		"traefik.enable":          "true",
		"traefik.http.routers.postal.rule":                      "Host(`postal.localhost`)",
		"traefik.http.services.postal.loadbalancer.server.port": "5000",
	}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "curl", "-f", "http://localhost:5000/healthz"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  5,
	}
	svc.Restart = "unless-stopped"

	// Add supporting services
	m.addDatabaseService(result)
	m.addRabbitMQService(result)

	// Generate configuration files
	postalConfig := m.generatePostalConfig(identityName, identityType)
	result.AddConfig("config/postal/postal.yml", []byte(postalConfig))

	// DKIM signing key placeholder
	dkimKey := m.generateDKIMKeyPlaceholder(identityName)
	result.AddConfig("config/postal/signing.key", []byte(dkimKey))

	// Setup script
	setupScript := m.generateSetupScript(identityName)
	result.AddScript("scripts/postal-setup.sh", []byte(setupScript))

	// Export script for SES data
	exportScript := m.generateExportScript(res)
	result.AddScript("scripts/ses-export.sh", []byte(exportScript))

	// DNS records helper
	dnsRecords := m.generateDNSRecords(identityName, identityType)
	result.AddConfig("config/postal/dns-records.txt", []byte(dnsRecords))

	// Add warnings and manual steps
	m.addMigrationWarnings(result, res, identityName, identityType)

	return result, nil
}

func (m *SESMapper) addDatabaseService(result *mapper.MappingResult) {
	dbService := &mapper.DockerService{
		Name:  "postal-db",
		Image: "mariadb:10.11",
		Environment: map[string]string{
			"MYSQL_ROOT_PASSWORD": "${MYSQL_ROOT_PASSWORD:-postal}",
			"MYSQL_DATABASE":      "postal",
			"MYSQL_USER":          "postal",
			"MYSQL_PASSWORD":      "${MYSQL_PASSWORD:-postal}",
		},
		Volumes: []string{
			"./data/postal/mysql:/var/lib/mysql",
		},
		Networks: []string{"homeport"},
		HealthCheck: &mapper.HealthCheck{
			Test:     []string{"CMD", "healthcheck.sh", "--connect", "--innodb_initialized"},
			Interval: 30 * time.Second,
			Timeout:  10 * time.Second,
			Retries:  5,
		},
		Restart: "unless-stopped",
	}
	result.AddService(dbService)
}

func (m *SESMapper) addRabbitMQService(result *mapper.MappingResult) {
	mqService := &mapper.DockerService{
		Name:  "postal-rabbitmq",
		Image: "rabbitmq:3.12-management-alpine",
		Environment: map[string]string{
			"RABBITMQ_DEFAULT_USER": "postal",
			"RABBITMQ_DEFAULT_PASS": "${RABBITMQ_PASSWORD:-postal}",
		},
		Volumes: []string{
			"./data/postal/rabbitmq:/var/lib/rabbitmq",
		},
		Networks: []string{"homeport"},
		HealthCheck: &mapper.HealthCheck{
			Test:     []string{"CMD", "rabbitmq-diagnostics", "-q", "ping"},
			Interval: 30 * time.Second,
			Timeout:  10 * time.Second,
			Retries:  5,
		},
		Restart: "unless-stopped",
	}
	result.AddService(mqService)
}

func (m *SESMapper) generatePostalConfig(identityName, identityType string) string {
	domain := identityName
	if identityType != "DOMAIN" {
		// Extract domain from email address
		parts := strings.Split(identityName, "@")
		if len(parts) == 2 {
			domain = parts[1]
		}
	}

	return fmt.Sprintf(`# Postal Configuration
# Generated from SES identity: %s
# Identity type: %s

general:
  # The domain the web interface runs on
  web_hostname: postal.localhost
  # The SMTP hostname for sending mail
  smtp_hostname: mail.localhost
  # Use IP pools for sending
  use_ip_pools: false

web:
  # Host for the web interface
  host: postal.localhost
  # Protocol (http or https)
  protocol: http
  # Port
  port: 5000

smtp_server:
  # Port to listen for incoming SMTP connections
  port: 25
  # Enable TLS
  tls_enabled: true
  # TLS certificate path
  tls_certificate_path: /opt/postal/config/certs/smtp.crt
  tls_private_key_path: /opt/postal/config/certs/smtp.key
  # Submission port
  proxy_protocol: false
  log_ip_address_exclusion_matcher:

smtp_relays:
  # Configure external SMTP relays if needed
  []

dns:
  # DNS settings for verification
  mx_records:
    - mx1.localhost
  spf_include: spf.localhost
  return_path_domain: rp.localhost
  route_domain: routes.localhost
  track_domain: track.localhost
  helo_hostname: mail.localhost

# Domain-specific configuration
# Migrated from: %s
default_domain: %s

logging:
  rails_log_enabled: true
  max_delivery_attempts: 18
  max_hold_expiry_days: 7
  suppression_list_automatic_removal_days: 90

# Rate limiting (adjust based on your SES quotas)
rate_limiting:
  per_server_per_minute: 1000
  per_organization_per_minute: 500
  per_domain_per_minute: 100
`, identityName, identityType, identityName, domain)
}

func (m *SESMapper) generateDKIMKeyPlaceholder(identityName string) string {
	return fmt.Sprintf(`# DKIM Signing Key Placeholder
# Generated for: %s
#
# To generate a real DKIM key pair, run:
#   openssl genrsa -out signing.key 2048
#   openssl rsa -in signing.key -pubout -out signing.pub
#
# Then add the public key to your DNS records as a TXT record:
#   postal._domainkey.yourdomain.com IN TXT "v=DKIM1; k=rsa; p=<public_key_base64>"
#
# Replace this file with your generated private key.

-----BEGIN PLACEHOLDER-----
Generate a real key using the instructions above.
This placeholder will not work for DKIM signing.
-----END PLACEHOLDER-----
`, identityName)
}

func (m *SESMapper) generateSetupScript(identityName string) string {
	return fmt.Sprintf(`#!/bin/bash
# Postal Setup Script
# Migrated from SES identity: %s

set -e

echo "============================================"
echo "Postal Mail Server Setup"
echo "Migrated from SES identity: %s"
echo "============================================"

# Wait for database to be ready
echo "Waiting for MariaDB..."
until docker-compose exec postal-db healthcheck.sh --connect 2>/dev/null; do
  sleep 2
done
echo "MariaDB is ready."

# Wait for RabbitMQ to be ready
echo "Waiting for RabbitMQ..."
until docker-compose exec postal-rabbitmq rabbitmq-diagnostics -q ping 2>/dev/null; do
  sleep 2
done
echo "RabbitMQ is ready."

# Initialize Postal database
echo "Initializing Postal..."
docker-compose exec postal postal initialize

# Create admin user
echo "Creating admin user..."
docker-compose exec postal postal make-user

echo ""
echo "============================================"
echo "Postal Setup Complete!"
echo "============================================"
echo ""
echo "Web Interface: http://postal.localhost:5000"
echo "SMTP Server:   mail.localhost:25"
echo "Submission:    mail.localhost:587"
echo ""
echo "Next steps:"
echo "1. Log into the web interface and create an organization"
echo "2. Add your domain: %s"
echo "3. Configure DNS records (see dns-records.txt)"
echo "4. Generate DKIM keys and update signing.key"
echo "5. Update your application SMTP settings"
echo ""
`, identityName, identityName, identityName)
}

func (m *SESMapper) generateExportScript(res *resource.AWSResource) string {
	identityName := res.Name
	region := res.Region

	return fmt.Sprintf(`#!/bin/bash
# SES Export Script
# Export SES configuration and templates for migration

set -e

AWS_REGION="%s"
IDENTITY="%s"
OUTPUT_DIR="./ses-export"

echo "Exporting SES configuration for: $IDENTITY"
mkdir -p "$OUTPUT_DIR"

# Export identity verification attributes
echo "Exporting identity attributes..."
aws ses get-identity-verification-attributes \
  --identities "$IDENTITY" \
  --region "$AWS_REGION" \
  --output json > "$OUTPUT_DIR/verification.json"

# Export DKIM attributes
echo "Exporting DKIM attributes..."
aws ses get-identity-dkim-attributes \
  --identities "$IDENTITY" \
  --region "$AWS_REGION" \
  --output json > "$OUTPUT_DIR/dkim.json"

# Export mail from domain attributes
echo "Exporting mail-from attributes..."
aws ses get-identity-mail-from-domain-attributes \
  --identities "$IDENTITY" \
  --region "$AWS_REGION" \
  --output json > "$OUTPUT_DIR/mail-from.json"

# Export sending authorization policies
echo "Exporting identity policies..."
aws ses list-identity-policies \
  --identity "$IDENTITY" \
  --region "$AWS_REGION" \
  --output json > "$OUTPUT_DIR/policies.json"

# Export email templates
echo "Exporting email templates..."
aws ses list-templates \
  --region "$AWS_REGION" \
  --output json > "$OUTPUT_DIR/templates-list.json"

# Export each template
for template in $(jq -r '.TemplatesMetadata[].Name' "$OUTPUT_DIR/templates-list.json" 2>/dev/null); do
  echo "  Exporting template: $template"
  aws ses get-template \
    --template-name "$template" \
    --region "$AWS_REGION" \
    --output json > "$OUTPUT_DIR/template-$template.json"
done

# Export configuration sets
echo "Exporting configuration sets..."
aws sesv2 list-configuration-sets \
  --region "$AWS_REGION" \
  --output json > "$OUTPUT_DIR/config-sets.json" 2>/dev/null || true

echo ""
echo "Export complete! Files saved to: $OUTPUT_DIR"
echo ""
echo "Review these files to migrate your email templates and configuration."
`, region, identityName)
}

func (m *SESMapper) generateDNSRecords(identityName, identityType string) string {
	domain := identityName
	if identityType != "DOMAIN" {
		parts := strings.Split(identityName, "@")
		if len(parts) == 2 {
			domain = parts[1]
		}
	}

	return fmt.Sprintf(`# DNS Records for Postal Mail Server
# Domain: %s
# Original SES identity: %s

# ============================================
# Required DNS Records
# ============================================

# MX Record - Route incoming mail to Postal
%s.    MX    10 mail.%s.

# A Record - Point mail server to your IP
mail.%s.    A    <YOUR_SERVER_IP>

# SPF Record - Authorize your server to send mail
%s.    TXT    "v=spf1 a mx ip4:<YOUR_SERVER_IP> -all"

# DKIM Record - Email authentication (replace with your public key)
postal._domainkey.%s.    TXT    "v=DKIM1; k=rsa; p=<YOUR_DKIM_PUBLIC_KEY>"

# DMARC Record - Email authentication policy
_dmarc.%s.    TXT    "v=DMARC1; p=quarantine; rua=mailto:dmarc@%s"

# ============================================
# Postal-specific Records
# ============================================

# Return path for bounce handling
rp.%s.    CNAME    mail.%s.

# Tracking domain for open/click tracking
track.%s.    CNAME    mail.%s.

# Routes domain for routing
routes.%s.    MX    10 mail.%s.

# ============================================
# Notes
# ============================================
# 1. Replace <YOUR_SERVER_IP> with your actual server IP
# 2. Generate DKIM keys and replace <YOUR_DKIM_PUBLIC_KEY>
# 3. Adjust DMARC policy (p=none for testing, p=quarantine or p=reject for production)
# 4. Verify all records with: dig TXT %s
`, domain, identityName,
		domain, domain, // MX
		domain,            // A
		domain,            // SPF
		domain,            // DKIM
		domain, domain, // DMARC
		domain, domain, // Return path
		domain, domain, // Tracking
		domain, domain, // Routes
		domain) // dig
}

func (m *SESMapper) addMigrationWarnings(result *mapper.MappingResult, res *resource.AWSResource, identityName, identityType string) {
	// Identity type warning
	if identityType == "EMAIL_ADDRESS" {
		result.AddWarning(fmt.Sprintf("SES email identity '%s' detected. Configure the domain in Postal instead.", identityName))
	}

	// DKIM warning
	if res.GetConfigBool("dkim_signing_enabled") {
		result.AddWarning("DKIM signing was enabled in SES. Generate new DKIM keys for Postal.")
		result.AddManualStep("Generate DKIM keys: openssl genrsa -out config/postal/signing.key 2048")
		result.AddManualStep("Extract public key and add to DNS as TXT record")
	}

	// Verification status
	if !res.GetConfigBool("verified_for_sending_status") {
		result.AddWarning("SES identity was not verified for sending. Verify your domain in Postal.")
	}

	// Mail from domain
	if mailFrom := res.GetConfigString("mail_from_domain"); mailFrom != "" {
		result.AddWarning(fmt.Sprintf("Custom MAIL FROM domain '%s' was configured. Update DNS for Postal.", mailFrom))
	}

	// Configuration set
	if configSet := res.GetConfigString("configuration_set"); configSet != "" {
		result.AddWarning(fmt.Sprintf("SES configuration set '%s' was in use. Configure webhooks in Postal for event tracking.", configSet))
	}

	// Standard migration steps
	result.AddManualStep("Run scripts/ses-export.sh to export SES templates and configuration")
	result.AddManualStep("Run scripts/postal-setup.sh to initialize Postal")
	result.AddManualStep("Update DNS records according to config/postal/dns-records.txt")
	result.AddManualStep("Update application SMTP settings to use Postal (mail.localhost:587)")
	result.AddManualStep("Test email sending before decommissioning SES")

	// Volumes
	result.AddVolume(mapper.Volume{
		Name:   "postal-storage",
		Driver: "local",
	})
	result.AddVolume(mapper.Volume{
		Name:   "postal-mysql",
		Driver: "local",
	})
	result.AddVolume(mapper.Volume{
		Name:   "postal-rabbitmq",
		Driver: "local",
	})
}
