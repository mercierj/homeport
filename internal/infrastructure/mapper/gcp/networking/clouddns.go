// Package networking provides mappers for GCP networking services.
package networking

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

// CloudDNSMapper converts GCP Cloud DNS to CoreDNS or PowerDNS.
type CloudDNSMapper struct {
	*mapper.BaseMapper
}

// NewCloudDNSMapper creates a new Cloud DNS to CoreDNS/PowerDNS mapper.
func NewCloudDNSMapper() *CloudDNSMapper {
	return &CloudDNSMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeCloudDNS, nil),
	}
}

// Map converts a GCP Cloud DNS managed zone to a CoreDNS service.
func (m *CloudDNSMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	zoneName := res.GetConfigString("name")
	if zoneName == "" {
		zoneName = res.Name
	}

	dnsName := res.GetConfigString("dns_name")
	if dnsName == "" {
		dnsName = "example.com."
	}

	result := mapper.NewMappingResult(m.sanitizeName(zoneName))
	svc := result.DockerService

	// Use CoreDNS as the DNS server
	svc.Image = "coredns/coredns:1.11.1"

	// Configure ports
	svc.Ports = []string{
		"53:53/udp", // DNS UDP
		"53:53/tcp", // DNS TCP
	}

	// Environment variables
	svc.Environment = map[string]string{
		"DNS_ZONE": dnsName,
	}

	// Labels
	svc.Labels = map[string]string{
		"cloudexit.source":       "google_dns_managed_zone",
		"cloudexit.service_name": zoneName,
		"cloudexit.dns_name":     dnsName,
	}

	svc.Networks = []string{"cloudexit"}
	svc.Restart = "unless-stopped"

	// Volume for CoreDNS configuration
	svc.Volumes = []string{
		"./config/coredns:/etc/coredns:ro",
	}

	// Generate Corefile configuration
	corefile := m.generateCorefile(dnsName, res)
	result.AddConfig("coredns/Corefile", []byte(corefile))

	// Extract and generate DNS records
	description := res.GetConfigString("description")
	visibility := res.GetConfigString("visibility")

	// Generate zone file
	zoneFile := m.generateZoneFile(dnsName, zoneName, description)
	result.AddConfig(fmt.Sprintf("coredns/%s.zone", m.sanitizeZoneName(dnsName)), []byte(zoneFile))

	// Handle DNSSEC
	if dnssecConfig := res.Config["dnssec_config"]; dnssecConfig != nil {
		if dnssecMap, ok := dnssecConfig.(map[string]interface{}); ok {
			if state, ok := dnssecMap["state"].(string); ok && state == "on" {
				result.AddWarning("DNSSEC is enabled on the source zone. Configure DNSSEC for CoreDNS manually.")
				result.AddManualStep("Generate DNSSEC keys for your zone")
				result.AddManualStep("Configure DNSSEC plugin in Corefile")
			}
		}
	}

	// Handle visibility
	if visibility == "private" {
		result.AddWarning("Private DNS zone detected. Configure firewall rules to restrict access.")
		result.AddManualStep("Configure Docker network policies or firewall rules for private DNS access")
	}

	// Health check
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD-SHELL", "dig @localhost -p 53 " + strings.TrimSuffix(dnsName, ".") + " +short || exit 1"},
		Interval: 30 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  3,
	}

	// Generate migration script for DNS records
	migrationScript := m.generateMigrationScript(dnsName, zoneName)
	result.AddScript("migrate-dns-records.sh", []byte(migrationScript))

	result.AddManualStep(fmt.Sprintf("Import DNS records from GCP to zone file: config/coredns/%s.zone", m.sanitizeZoneName(dnsName)))
	result.AddManualStep("Update DNS records in the zone file with actual values")
	result.AddManualStep("Point your domain nameservers to this CoreDNS instance")
	result.AddManualStep("Test DNS resolution: dig @localhost " + strings.TrimSuffix(dnsName, "."))
	result.AddWarning("Consider using PowerDNS for a more feature-rich DNS server with web UI")

	return result, nil
}

// generateCorefile generates the CoreDNS Corefile configuration.
func (m *CloudDNSMapper) generateCorefile(dnsName string, res *resource.AWSResource) string {
	zoneName := m.sanitizeZoneName(dnsName)
	visibility := res.GetConfigString("visibility")

	privateNote := ""
	if visibility == "private" {
		privateNote = `
    # Private zone - restrict access as needed
    # Consider using the acl plugin to restrict access`
	}

	return fmt.Sprintf(`# CoreDNS configuration for %s
%s {
    # Primary zone file
    file /etc/coredns/%s.zone

    # Enable logging
    log

    # Handle errors
    errors

    # Cache responses
    cache 30

    # Enable query logging
    # querylog

    # Load balancing
    loadbalance round_robin
    %s
}

# Forward all other queries to upstream DNS
. {
    forward . 8.8.8.8 8.8.4.4
    log
    errors
    cache 30
}
`, dnsName, dnsName, zoneName, privateNote)
}

// generateZoneFile generates a DNS zone file.
func (m *CloudDNSMapper) generateZoneFile(dnsName, zoneName, description string) string {
	// Remove trailing dot for display
	domain := strings.TrimSuffix(dnsName, ".")

	descComment := ""
	if description != "" {
		descComment = fmt.Sprintf("; %s\n", description)
	}

	return fmt.Sprintf(`$ORIGIN %s.
$TTL 3600
%s
; SOA Record
@   IN  SOA ns1.%s. admin.%s. (
            2024010101  ; Serial (YYYYMMDDNN)
            7200        ; Refresh (2 hours)
            3600        ; Retry (1 hour)
            1209600     ; Expire (2 weeks)
            3600        ; Minimum TTL (1 hour)
        )

; Name Server Records
@   IN  NS  ns1.%s.
@   IN  NS  ns2.%s.

; A Records
ns1 IN  A   10.0.0.1
ns2 IN  A   10.0.0.2

; Example records - update with your actual records
@   IN  A   10.0.0.10
www IN  A   10.0.0.10

; CNAME Records
; Example:
; blog    IN  CNAME   www

; MX Records
; Example:
; @       IN  MX  10  mail.%s.

; TXT Records
; Example:
; @       IN  TXT "v=spf1 mx -all"

; Add your DNS records here
`, domain, descComment, domain, domain, domain, domain, domain)
}

// generateMigrationScript generates a script to help migrate DNS records.
func (m *CloudDNSMapper) generateMigrationScript(dnsName, zoneName string) string {
	domain := strings.TrimSuffix(dnsName, ".")
	zoneFileName := m.sanitizeZoneName(dnsName)

	return fmt.Sprintf(`#!/bin/bash
# DNS Record Migration Script for %s
# This script helps export DNS records from GCP Cloud DNS

set -e

ZONE_NAME="%s"
DNS_NAME="%s"
OUTPUT_FILE="config/coredns/%s.zone"

echo "Migrating DNS records for zone: $ZONE_NAME ($DNS_NAME)"

# Check if gcloud is installed
if ! command -v gcloud &> /dev/null; then
    echo "Error: gcloud CLI is not installed"
    echo "Install it from: https://cloud.google.com/sdk/docs/install"
    exit 1
fi

# Export DNS records from GCP
echo "Exporting DNS records from GCP..."
gcloud dns record-sets list --zone="$ZONE_NAME" --format="table(name,type,ttl,rrdatas)" > dns-records.txt

echo ""
echo "DNS records exported to dns-records.txt"
echo "Review the records and add them to: $OUTPUT_FILE"
echo ""
echo "Example conversions:"
echo "  A Record:     example.com.  IN A     192.0.2.1"
echo "  CNAME:        www           IN CNAME example.com."
echo "  MX:           @             IN MX    10 mail.example.com."
echo "  TXT:          @             IN TXT   \"v=spf1 mx -all\""
echo ""
echo "After updating the zone file, reload CoreDNS:"
echo "  docker restart <coredns-container-name>"
`, domain, zoneName, dnsName, zoneFileName)
}

// sanitizeZoneName sanitizes a DNS name for use as a filename.
func (m *CloudDNSMapper) sanitizeZoneName(dnsName string) string {
	name := strings.TrimSuffix(dnsName, ".")
	name = strings.ReplaceAll(name, ".", "-")
	return name
}

// sanitizeName sanitizes the name for Docker.
func (m *CloudDNSMapper) sanitizeName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "_", "-")
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.ReplaceAll(name, ".", "-")

	validName := ""
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' {
			validName += string(ch)
		}
	}

	validName = strings.TrimLeft(validName, "-0123456789")
	if validName == "" {
		validName = "dns"
	}

	return validName
}
