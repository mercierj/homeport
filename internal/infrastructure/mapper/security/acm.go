// Package security provides mappers for AWS security services.
package security

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

// ACMMapper converts AWS ACM certificates to Traefik with Let's Encrypt.
type ACMMapper struct {
	*mapper.BaseMapper
}

// NewACMMapper creates a new ACM to Traefik mapper.
func NewACMMapper() *ACMMapper {
	return &ACMMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeACMCertificate, nil),
	}
}

// Map converts an AWS ACM certificate to Traefik configuration.
func (m *ACMMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	domainName := res.GetConfigString("domain_name")
	if domainName == "" {
		domainName = res.Name
	}
	email := res.GetConfigString("acme_email")
	if email == "" {
		email = "admin@" + domainName
	}
	dnsProvider := res.GetConfigString("dns_challenge_provider")

	result := mapper.NewMappingResult("traefik")
	svc := result.DockerService

	svc.Image = "traefik:v3.0"
	svc.Command = []string{
		"--api.dashboard=true",
		"--providers.docker=true",
		"--providers.docker.exposedbydefault=false",
		"--entrypoints.web.address=:80",
		"--entrypoints.websecure.address=:443",
		"--certificatesresolvers.letsencrypt.acme.email=" + email,
		"--certificatesresolvers.letsencrypt.acme.storage=/letsencrypt/acme.json",
	}
	if dnsProvider != "" {
		svc.Command = append(svc.Command, "--certificatesresolvers.letsencrypt.acme.dnschallenge.provider="+dnsProvider)
	} else {
		svc.Command = append(svc.Command, "--certificatesresolvers.letsencrypt.acme.httpchallenge.entrypoint=web")
	}
	svc.Ports = []string{"80:80", "443:443", "8080:8080"}
	svc.Volumes = []string{
		"/var/run/docker.sock:/var/run/docker.sock:ro",
		"./data/letsencrypt:/letsencrypt",
		"./config/traefik:/etc/traefik",
	}
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Labels = map[string]string{
		"homeport.source":                               "aws_acm_certificate",
		"homeport.domain":                               domainName,
		"traefik.enable":                                "true",
		"traefik.http.routers.dashboard.rule":           "Host(`traefik.localhost`)",
		"traefik.http.routers.dashboard.service":        "api@internal",
		"traefik.http.routers.dashboard.middlewares":    "auth",
		"traefik.http.middlewares.auth.basicauth.users": "admin:$$apr1$$xyz",
	}
	svc.Restart = "unless-stopped"
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "traefik", "healthcheck"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  3,
	}

	traefikConfig := m.generateTraefikConfig(domainName, email, dnsProvider)
	result.AddConfig("config/traefik/traefik.yml", []byte(traefikConfig))

	sans := m.extractSANs(res)
	dynamicConfig := m.generateDynamicConfig(domainName, sans)
	result.AddConfig("config/traefik/dynamic.yml", []byte(dynamicConfig))

	setupScript := m.generateSetupScript(domainName)
	result.AddScript("setup_certificates.sh", []byte(setupScript))
	result.AddScript("backup_acm_config.sh", []byte(m.generateBackupScript(domainName)))

	if len(sans) > 0 && dnsProvider == "" {
		result.AddWarning(fmt.Sprintf("Additional domains: %s", strings.Join(sans, ", ")))
		result.AddManualStep("Configure additional domains in Traefik dynamic config")
	}

	validationMethod := res.GetConfigString("validation_method")
	if validationMethod == "DNS" && dnsProvider == "" {
		result.AddWarning("DNS validation used in AWS. Configure DNS provider in Traefik.")
		result.AddManualStep("Set up DNS challenge provider for Let's Encrypt")
	}

	result.AddWarning("Traefik dashboard is available at http://traefik.localhost:8080")
	if res.GetConfigString("acme_email") == "" {
		result.AddManualStep("Update admin email for Let's Encrypt notifications")
	}
	result.AddWarning("Let's Encrypt has rate limits - test with staging first")
	for _, step := range acmRunbook(domainName) {
		result.AddRunbookStep(step)
	}

	return result, nil
}

func (m *ACMMapper) generateTraefikConfig(domain, email, dnsProvider string) string {
	challenge := `      httpChallenge:
        entryPoint: web`
	if dnsProvider != "" {
		challenge = fmt.Sprintf(`      dnsChallenge:
        provider: %s`, dnsProvider)
	}
	return fmt.Sprintf(`api:
  dashboard: true
  insecure: true

entryPoints:
  web:
    address: ":80"
    http:
      redirections:
        entryPoint:
          to: websecure
          scheme: https
  websecure:
    address: ":443"

providers:
  docker:
    endpoint: "unix:///var/run/docker.sock"
    exposedByDefault: false
  file:
    filename: /etc/traefik/dynamic.yml
    watch: true

certificatesResolvers:
  letsencrypt:
    acme:
      email: %s
      storage: /letsencrypt/acme.json
%s

log:
  level: INFO
`, email, challenge)
}

func (m *ACMMapper) generateDynamicConfig(domain string, sans []string) string {
	config := fmt.Sprintf("tls:\n"+
		"  options:\n"+
		"    default:\n"+
		"      minVersion: VersionTLS12\n"+
		"      sniStrict: true\n"+
		"      cipherSuites:\n"+
		"        - TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384\n"+
		"        - TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305\n"+
		"        - TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256\n"+
		"\n"+
		"http:\n"+
		"  routers:\n"+
		"    secure-router:\n"+
		"      rule: \"Host(`%s`)\"\n"+
		"      entryPoints:\n"+
		"        - websecure\n"+
		"      tls:\n"+
		"        certResolver: letsencrypt\n", domain)
	if len(sans) > 0 {
		var b strings.Builder
		b.WriteString(config)
		b.WriteString("        domains:\n")
		b.WriteString(fmt.Sprintf("          - main: \"%s\"\n", domain))
		b.WriteString("            sans:\n")
		for _, san := range sans {
			b.WriteString(fmt.Sprintf("              - \"%s\"\n", san))
		}
		config = b.String()
	}
	var b strings.Builder
	b.WriteString(config)
	b.WriteString("      service: app-service\n")
	b.WriteString("\n")
	b.WriteString("  services:\n")
	b.WriteString("    app-service:\n")
	b.WriteString("      loadBalancer:\n")
	b.WriteString("        servers:\n")
	b.WriteString("          - url: \"http://app:8080\"\n")
	return b.String()
}

func (m *ACMMapper) generateSetupScript(domain string) string {
	return fmt.Sprintf(`#!/bin/bash
set -e

DOMAIN="%s"

echo "Certificate Setup for $DOMAIN"
echo "=============================="

mkdir -p data/letsencrypt config/traefik
touch data/letsencrypt/acme.json
chmod 600 data/letsencrypt/acme.json

echo "Directories created"
echo ""
echo "Next steps:"
echo "1. Update DNS A record for $DOMAIN to your server IP"
echo "2. Start Traefik: docker-compose up -d traefik"
echo "3. Certificates will be auto-generated on first request"
`, domain)
}

func (m *ACMMapper) generateBackupScript(domain string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-acm-traefik-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/traefik data/letsencrypt
echo "$archive"
`, strings.NewReplacer("*", "wildcard-", ".", "-").Replace(domain))
}

func (m *ACMMapper) extractSANs(res *resource.AWSResource) []string {
	var sans []string
	if sanConfig := res.Config["subject_alternative_names"]; sanConfig != nil {
		if sanSlice, ok := sanConfig.([]interface{}); ok {
			for _, san := range sanSlice {
				if sanStr, ok := san.(string); ok {
					sans = append(sans, sanStr)
				}
			}
		}
	}
	return sans
}

func acmRunbook(domain string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "certificate", "name": domain, "source": "aws_acm_certificate"}
	return []domainrunbook.Step{
		{
			ID:               "provision-acme-dns-challenge",
			Name:             "Provision ACME DNS challenge",
			Group:            "Deploy",
			Type:             domainrunbook.StepTypeCommand,
			Status:           domainrunbook.StepStatusPending,
			Executor:         "shell",
			Command:          []string{"sh", "setup_certificates.sh"},
			SuccessCondition: "Traefik ACME storage and DNS challenge provider are configured",
			Metadata:         metadata,
		},
		{
			ID:               "validate-certificate-renewal",
			Name:             "Validate certificate renewal",
			Group:            "Validate",
			Type:             domainrunbook.StepTypeCommand,
			Status:           domainrunbook.StepStatusPending,
			Executor:         "shell",
			Command:          []string{"sh", "-c", "traefik healthcheck && test -f data/letsencrypt/acme.json"},
			SuccessCondition: "Traefik is healthy and ACME storage is present",
			Metadata:         metadata,
		},
		{
			ID:               "backup-certificate-config",
			Name:             "Backup certificate config",
			Group:            "Backup",
			Type:             domainrunbook.StepTypeCommand,
			Status:           domainrunbook.StepStatusPending,
			Executor:         "shell",
			Command:          []string{"sh", "backup_acm_config.sh"},
			SuccessCondition: "Traefik TLS config and ACME state are archived before cutover",
			Metadata:         metadata,
		},
		{
			ID:               "cutover-tls-termination-to-traefik",
			Name:             "Cut over TLS termination to Traefik",
			Group:            "Cutover",
			Type:             domainrunbook.StepTypeDNSCheck,
			Status:           domainrunbook.StepStatusPending,
			Executor:         "dns",
			SuccessCondition: "TLS hostnames terminate on HomePort Traefik with renewed certificates",
			Metadata:         metadata,
		},
		{
			ID:               "rollback-certificate-source-authority",
			Name:             "Keep ACM certificate as rollback authority",
			Group:            "Rollback",
			Type:             domainrunbook.StepTypeRollback,
			Status:           domainrunbook.StepStatusPending,
			Executor:         "noop",
			SuccessCondition: "AWS ACM remains authoritative until TLS cutover validation passes",
			Metadata:         metadata,
		},
	}
}
