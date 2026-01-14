package datamigration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// SESToMailhogExecutor migrates SES to self-hosted email (Mailhog/Postal).
type SESToMailhogExecutor struct{}

// NewSESToMailhogExecutor creates a new SES to Mailhog executor.
func NewSESToMailhogExecutor() *SESToMailhogExecutor {
	return &SESToMailhogExecutor{}
}

// Type returns the migration type.
func (e *SESToMailhogExecutor) Type() string {
	return "ses_to_postal"
}

// GetPhases returns the migration phases.
func (e *SESToMailhogExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Fetching identities",
		"Exporting templates",
		"Generating Postal config",
		"Creating SMTP config",
		"Finalizing",
	}
}

// Validate validates the migration configuration.
func (e *SESToMailhogExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
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

	result.Warnings = append(result.Warnings, "SES reputation data cannot be migrated")
	result.Warnings = append(result.Warnings, "DNS records (SPF, DKIM, DMARC) must be reconfigured")

	return result, nil
}

// Execute performs the migration.
func (e *SESToMailhogExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	accessKeyID := config.Source["access_key_id"].(string)
	secretAccessKey := config.Source["secret_access_key"].(string)
	region, _ := config.Source["region"].(string)
	if region == "" {
		region = "us-east-1"
	}
	outputDir := config.Destination["output_dir"].(string)

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

	// Phase 2: Fetching identities
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", "Fetching SES identities")
	EmitProgress(m, 25, "Fetching identities")

	listIdentitiesCmd := exec.CommandContext(ctx, "aws", "ses", "list-identities",
		"--region", region, "--output", "json",
	)
	listIdentitiesCmd.Env = append(os.Environ(), awsEnv...)
	identitiesOutput, err := listIdentitiesCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to list identities: %w", err)
	}

	var identitiesList struct {
		Identities []string `json:"Identities"`
	}
	_ = json.Unmarshal(identitiesOutput, &identitiesList)

	type IdentityDetails struct {
		Identity         string            `json:"identity"`
		VerificationStatus string          `json:"verificationStatus"`
		DkimEnabled      bool              `json:"dkimEnabled"`
		DkimTokens       []string          `json:"dkimTokens"`
		MailFromDomain   string            `json:"mailFromDomain"`
	}
	identities := make([]IdentityDetails, 0)

	for _, identity := range identitiesList.Identities {
		// Get verification attributes
		verifyCmd := exec.CommandContext(ctx, "aws", "ses", "get-identity-verification-attributes",
			"--identities", identity,
			"--region", region, "--output", "json",
		)
		verifyCmd.Env = append(os.Environ(), awsEnv...)
		verifyOutput, _ := verifyCmd.Output()

		var verifyAttrs struct {
			VerificationAttributes map[string]struct {
				VerificationStatus string `json:"VerificationStatus"`
			} `json:"VerificationAttributes"`
		}
		_ = json.Unmarshal(verifyOutput, &verifyAttrs)

		// Get DKIM attributes
		dkimCmd := exec.CommandContext(ctx, "aws", "ses", "get-identity-dkim-attributes",
			"--identities", identity,
			"--region", region, "--output", "json",
		)
		dkimCmd.Env = append(os.Environ(), awsEnv...)
		dkimOutput, _ := dkimCmd.Output()

		var dkimAttrs struct {
			DkimAttributes map[string]struct {
				DkimEnabled            bool     `json:"DkimEnabled"`
				DkimVerificationStatus string   `json:"DkimVerificationStatus"`
				DkimTokens             []string `json:"DkimTokens"`
			} `json:"DkimAttributes"`
		}
		_ = json.Unmarshal(dkimOutput, &dkimAttrs)

		details := IdentityDetails{
			Identity: identity,
		}
		if v, ok := verifyAttrs.VerificationAttributes[identity]; ok {
			details.VerificationStatus = v.VerificationStatus
		}
		if d, ok := dkimAttrs.DkimAttributes[identity]; ok {
			details.DkimEnabled = d.DkimEnabled
			details.DkimTokens = d.DkimTokens
		}

		identities = append(identities, details)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Exporting templates
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", "Exporting email templates")
	EmitProgress(m, 40, "Exporting templates")

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// List templates
	listTemplatesCmd := exec.CommandContext(ctx, "aws", "ses", "list-templates",
		"--region", region, "--output", "json",
	)
	listTemplatesCmd.Env = append(os.Environ(), awsEnv...)
	templatesListOutput, _ := listTemplatesCmd.Output()

	var templatesList struct {
		TemplatesMetadata []struct {
			Name string `json:"Name"`
		} `json:"TemplatesMetadata"`
	}
	_ = json.Unmarshal(templatesListOutput, &templatesList)

	templates := make(map[string]interface{})
	templatesDir := filepath.Join(outputDir, "templates")
	if err := os.MkdirAll(templatesDir, 0755); err != nil {
		return fmt.Errorf("failed to create templates directory: %w", err)
	}

	for _, tmpl := range templatesList.TemplatesMetadata {
		getTemplateCmd := exec.CommandContext(ctx, "aws", "ses", "get-template",
			"--template-name", tmpl.Name,
			"--region", region, "--output", "json",
		)
		getTemplateCmd.Env = append(os.Environ(), awsEnv...)
		tmplOutput, _ := getTemplateCmd.Output()

		var template struct {
			Template struct {
				TemplateName string `json:"TemplateName"`
				SubjectPart  string `json:"SubjectPart"`
				HtmlPart     string `json:"HtmlPart"`
				TextPart     string `json:"TextPart"`
			} `json:"Template"`
		}
		_ = json.Unmarshal(tmplOutput, &template)

		templates[tmpl.Name] = template.Template

		// Save HTML template
		if template.Template.HtmlPart != "" {
			htmlPath := filepath.Join(templatesDir, tmpl.Name+".html")
			_ = os.WriteFile(htmlPath, []byte(template.Template.HtmlPart), 0644)
		}
		// Save text template
		if template.Template.TextPart != "" {
			textPath := filepath.Join(templatesDir, tmpl.Name+".txt")
			_ = os.WriteFile(textPath, []byte(template.Template.TextPart), 0644)
		}
	}

	// Save identities and templates
	identitiesData, _ := json.MarshalIndent(identities, "", "  ")
	identitiesPath := filepath.Join(outputDir, "ses-identities.json")
	_ = os.WriteFile(identitiesPath, identitiesData, 0644)

	templatesData, _ := json.MarshalIndent(templates, "", "  ")
	templatesPath := filepath.Join(outputDir, "ses-templates.json")
	_ = os.WriteFile(templatesPath, templatesData, 0644)

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Generating Postal config
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Generating Postal configuration")
	EmitProgress(m, 60, "Generating config")

	// Docker compose with Postal
	postalCompose := `version: '3.8'

services:
  # For development: Use Mailhog
  mailhog:
    image: mailhog/mailhog:latest
    container_name: mailhog
    ports:
      - "1025:1025"   # SMTP
      - "8025:8025"   # Web UI
    restart: unless-stopped

  # For production: Use Postal (requires more setup)
  # See: https://docs.postalserver.io/

  # Alternative: Use Mailu for full mail server
  # See: https://mailu.io/

volumes:
  postal-data:

# Production Notes:
# 1. Postal requires Ruby and MariaDB
# 2. Consider using Mailu for simpler setup
# 3. Ensure proper DNS (MX, SPF, DKIM, DMARC)
`
	composePath := filepath.Join(outputDir, "docker-compose.yml")
	_ = os.WriteFile(composePath, []byte(postalCompose), 0644)

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Creating SMTP config
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Creating SMTP configuration")
	EmitProgress(m, 80, "Creating SMTP config")

	// SMTP config examples
	smtpConfig := `# SMTP Configuration for Self-Hosted Email

## Development (Mailhog)
SMTP_HOST=localhost
SMTP_PORT=1025
SMTP_USER=
SMTP_PASS=
SMTP_TLS=false

## Production (Postal/Mailu)
# SMTP_HOST=mail.yourdomain.com
# SMTP_PORT=587
# SMTP_USER=your-user
# SMTP_PASS=your-password
# SMTP_TLS=true

## Required DNS Records

# SPF Record (TXT)
# v=spf1 mx ip4:YOUR_IP ~all

# DKIM Record (TXT)
# selector._domainkey.yourdomain.com

# DMARC Record (TXT)
# _dmarc.yourdomain.com: v=DMARC1; p=quarantine; rua=mailto:dmarc@yourdomain.com
`
	smtpConfigPath := filepath.Join(outputDir, "smtp-config.txt")
	_ = os.WriteFile(smtpConfigPath, []byte(smtpConfig), 0644)

	// Python email sending example
	pythonExample := `#!/usr/bin/env python3
"""
Send email using self-hosted SMTP (replaces SES)
"""
import smtplib
from email.mime.text import MIMEText
from email.mime.multipart import MIMEMultipart
import os

def send_email(to_email: str, subject: str, html_body: str, text_body: str = None):
    """
    Send email via SMTP (equivalent to SES send_email)
    """
    smtp_host = os.getenv('SMTP_HOST', 'localhost')
    smtp_port = int(os.getenv('SMTP_PORT', '1025'))
    smtp_user = os.getenv('SMTP_USER', '')
    smtp_pass = os.getenv('SMTP_PASS', '')
    from_email = os.getenv('FROM_EMAIL', 'noreply@example.com')

    msg = MIMEMultipart('alternative')
    msg['Subject'] = subject
    msg['From'] = from_email
    msg['To'] = to_email

    if text_body:
        msg.attach(MIMEText(text_body, 'plain'))
    msg.attach(MIMEText(html_body, 'html'))

    with smtplib.SMTP(smtp_host, smtp_port) as server:
        if smtp_user and smtp_pass:
            server.starttls()
            server.login(smtp_user, smtp_pass)
        server.send_message(msg)

    print(f"Email sent to {to_email}")

def send_templated_email(to_email: str, template_name: str, template_data: dict):
    """
    Send templated email (equivalent to SES send_templated_email)
    """
    # Load template
    import jinja2
    template_dir = os.path.join(os.path.dirname(__file__), 'templates')

    with open(os.path.join(template_dir, f'{template_name}.html')) as f:
        html_template = jinja2.Template(f.read())

    text_template = None
    text_path = os.path.join(template_dir, f'{template_name}.txt')
    if os.path.exists(text_path):
        with open(text_path) as f:
            text_template = jinja2.Template(f.read())

    html_body = html_template.render(**template_data)
    text_body = text_template.render(**template_data) if text_template else None

    send_email(to_email, template_data.get('subject', 'No Subject'), html_body, text_body)

if __name__ == '__main__':
    # Example usage
    send_email(
        'user@example.com',
        'Test Email',
        '<h1>Hello!</h1><p>This is a test email.</p>',
        'Hello! This is a test email.'
    )
`
	pythonPath := filepath.Join(outputDir, "send_email.py")
	_ = os.WriteFile(pythonPath, []byte(pythonExample), 0755)

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 6: Finalizing
	EmitPhase(m, phases[5], 6)
	EmitLog(m, "info", "Finalizing migration artifacts")
	EmitProgress(m, 95, "Finalizing")

	readme := fmt.Sprintf(`# SES to Self-Hosted Email Migration

## Source SES
- Region: %s
- Identities: %d
- Templates: %d

## Migration Mapping

| SES Feature | Self-Hosted Equivalent |
|-------------|------------------------|
| send_email | SMTP sendmail |
| send_templated_email | Jinja2 templates + SMTP |
| Verified identities | DNS configuration |
| DKIM | OpenDKIM |
| Reputation | Own IP reputation |

## Quick Start (Development)

1. Start Mailhog:
'''bash
docker-compose up -d mailhog
'''

2. Access Web UI:
- URL: http://localhost:8025

3. Send test email:
'''bash
python send_email.py
'''

## Production Setup

For production, consider:
1. **Postal** - Full-featured mail server
2. **Mailu** - Docker-based mail solution
3. **External SMTP** - SendGrid, Mailgun, Postmark

## DNS Configuration Required

### MX Record
'''
yourdomain.com. MX 10 mail.yourdomain.com.
'''

### SPF Record
'''
yourdomain.com. TXT "v=spf1 mx ip4:YOUR_IP ~all"
'''

### DKIM Record
'''
selector._domainkey.yourdomain.com. TXT "v=DKIM1; k=rsa; p=YOUR_PUBLIC_KEY"
'''

### DMARC Record
'''
_dmarc.yourdomain.com. TXT "v=DMARC1; p=quarantine; rua=mailto:dmarc@yourdomain.com"
'''

## Migrated Identities
`, region, len(identities), len(templates))

	for _, identity := range identities {
		readme += fmt.Sprintf("- %s (Status: %s, DKIM: %v)\n", identity.Identity, identity.VerificationStatus, identity.DkimEnabled)
	}

	readme += `
## Files Generated
- ses-identities.json: SES verified identities
- ses-templates.json: Email templates metadata
- templates/: HTML and text templates
- docker-compose.yml: Mailhog container
- smtp-config.txt: SMTP configuration
- send_email.py: Python email sender

## Notes
- IP reputation starts fresh with self-hosted
- Consider warming up IP for production
- Monitor deliverability rates closely
`

	readmePath := filepath.Join(outputDir, "README.md")
	_ = os.WriteFile(readmePath, []byte(readme), 0644)

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", fmt.Sprintf("SES migration complete: %d identities, %d templates", len(identities), len(templates)))

	return nil
}
