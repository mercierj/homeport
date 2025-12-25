// Package security provides mappers for AWS security services.
package security

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cloudexit/cloudexit/internal/domain/mapper"
	"github.com/cloudexit/cloudexit/internal/domain/resource"
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

	result := mapper.NewMappingResult("traefik")
	svc := result.DockerService

	svc.Image = "traefik:v3.0"
	svc.Command = []string{
		"--api.dashboard=true",
		"--providers.docker=true",
		"--providers.docker.exposedbydefault=false",
		"--entrypoints.web.address=:80",
		"--entrypoints.websecure.address=:443",
		"--certificatesresolvers.letsencrypt.acme.email=admin@" + domainName,
		"--certificatesresolvers.letsencrypt.acme.storage=/letsencrypt/acme.json",
		"--certificatesresolvers.letsencrypt.acme.httpchallenge.entrypoint=web",
	}
	svc.Ports = []string{"80:80", "443:443", "8080:8080"}
	svc.Volumes = []string{
		"/var/run/docker.sock:/var/run/docker.sock:ro",
		"./data/letsencrypt:/letsencrypt",
		"./config/traefik:/etc/traefik",
	}
	svc.Networks = []string{"cloudexit"}
	svc.Labels = map[string]string{
		"cloudexit.source": "aws_acm_certificate",
		"cloudexit.domain": domainName,
		"traefik.enable":   "true",
		"traefik.http.routers.dashboard.rule":                      "Host(`traefik.localhost`)",
		"traefik.http.routers.dashboard.service":                   "api@internal",
		"traefik.http.routers.dashboard.middlewares":               "auth",
		"traefik.http.middlewares.auth.basicauth.users":            "admin:$$apr1$$xyz",
	}
	svc.Restart = "unless-stopped"
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "traefik", "healthcheck"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  3,
	}

	traefikConfig := m.generateTraefikConfig(domainName)
	result.AddConfig("config/traefik/traefik.yml", []byte(traefikConfig))

	dynamicConfig := m.generateDynamicConfig(domainName)
	result.AddConfig("config/traefik/dynamic.yml", []byte(dynamicConfig))

	setupScript := m.generateSetupScript(domainName)
	result.AddScript("setup_certificates.sh", []byte(setupScript))

	sans := m.extractSANs(res)
	if len(sans) > 0 {
		result.AddWarning(fmt.Sprintf("Additional domains: %s", strings.Join(sans, ", ")))
		result.AddManualStep("Configure additional domains in Traefik dynamic config")
	}

	validationMethod := res.GetConfigString("validation_method")
	if validationMethod == "DNS" {
		result.AddWarning("DNS validation used in AWS. Configure DNS provider in Traefik.")
		result.AddManualStep("Set up DNS challenge provider for Let's Encrypt")
	}

	result.AddManualStep("Update DNS to point to your server")
	result.AddManualStep("Access Traefik dashboard at http://traefik.localhost:8080")
	result.AddManualStep("Update admin email for Let's Encrypt notifications")
	result.AddWarning("Let's Encrypt has rate limits - test with staging first")

	return result, nil
}

func (m *ACMMapper) generateTraefikConfig(domain string) string {
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
      email: admin@%s
      storage: /letsencrypt/acme.json
      httpChallenge:
        entryPoint: web

log:
  level: INFO
`, domain)
}

func (m *ACMMapper) generateDynamicConfig(domain string) string {
	return fmt.Sprintf("tls:\n"+
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
		"        certResolver: letsencrypt\n"+
		"      service: app-service\n"+
		"\n"+
		"  services:\n"+
		"    app-service:\n"+
		"      loadBalancer:\n"+
		"        servers:\n"+
		"          - url: \"http://app:8080\"\n", domain)
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
