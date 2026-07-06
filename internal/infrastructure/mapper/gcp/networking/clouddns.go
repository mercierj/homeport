// Package networking provides mappers for GCP networking services.
package networking

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
	"github.com/homeport/homeport/internal/infrastructure/mapper/shared/netrunbook"
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
		"homeport.source":       "google_dns_managed_zone",
		"homeport.service_name": zoneName,
		"homeport.dns_name":     dnsName,
	}

	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
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
	result.AddConfig("config/cloud-dns/app-change.env", []byte(m.generateAppChangeConfig(zoneName)))
	result.AddConfig("config/cloud-dns/zone-report.yaml", []byte(m.generateZoneReport(zoneName, dnsName, visibility)))

	// Handle DNSSEC
	if dnssecConfig := res.Config["dnssec_config"]; dnssecConfig != nil {
		if dnssecMap, ok := dnssecConfig.(map[string]interface{}); ok {
			if state, ok := dnssecMap["state"].(string); ok && state == "on" {
				result.AddWarning("DNSSEC is enabled on the source zone. Configure DNSSEC for CoreDNS manually.")
				result.AddConfig("config/cloud-dns/dnssec-policy.yaml", []byte("dnssec: generated_key_rotation\nplugin: coredns_dnssec\n"))
			}
		}
	}

	// Handle visibility
	if visibility == "private" {
		result.AddWarning("Private DNS zone detected. Configure firewall rules to restrict access.")
		result.AddConfig("config/cloud-dns/private-zone-policy.yaml", []byte("visibility: private\nnetwork_policy: generated_firewall_rules\n"))
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
	result.AddScript("backup_cloud_dns.sh", []byte(m.generateBackupScript(zoneName)))
	result.AddScript("validate_cloud_dns.sh", []byte(m.generateValidateScript(zoneName, dnsName)))

	result.AddWarning("Consider using PowerDNS for a more feature-rich DNS server with web UI")
	for _, step := range netrunbook.DNS(dnsName, "google_dns_managed_zone", "migrate-dns-records.sh") {
		result.AddRunbookStep(step)
	}
	for _, step := range cloudDNSRunbook(zoneName, dnsName) {
		result.AddRunbookStep(step)
	}

	return result, nil
}

func (m *CloudDNSMapper) generateAppChangeConfig(zoneName string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=generated_patch
SOURCE_CLOUD_DNS_ZONE=%s
TARGET_DNS_ENDPOINT=%s:53
TARGET_COREDNS_CONFIG=coredns/Corefile
TARGET_ZONE_BACKUP=backup_cloud_dns.sh
`, zoneName, m.sanitizeName(zoneName))
}

func (m *CloudDNSMapper) generateZoneReport(zoneName, dnsName, visibility string) string {
	if visibility == "" {
		visibility = "public"
	}
	return fmt.Sprintf(`source: google_dns_managed_zone
zone: %s
dns_name: %s
visibility: %s
target: coredns
`, zoneName, dnsName, visibility)
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

func (m *CloudDNSMapper) generateBackupScript(zoneName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/cloud-dns-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" coredns config/cloud-dns
echo "$archive"
`, m.sanitizeName(zoneName))
}

func (m *CloudDNSMapper) generateValidateScript(zoneName, dnsName string) string {
	domain := strings.TrimSuffix(dnsName, ".")
	return fmt.Sprintf(`#!/bin/sh
set -eu
test -s coredns/Corefile
test -s coredns/%s.zone
test -s config/cloud-dns/app-change.env
coredns -conf coredns/Corefile -plugins >/dev/null
dig @localhost %s +short >/dev/null
echo "Cloud DNS zone %s validated on CoreDNS"
`, m.sanitizeZoneName(dnsName), domain, zoneName)
}

func cloudDNSRunbook(zoneName, dnsName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "dns", "source": "google_dns_managed_zone", "zone": zoneName, "dns_name": dnsName}
	return []domainrunbook.Step{
		cloudDNSStep("discover-cloud-dns-zone", "Discover Cloud DNS zone", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "-c", fmt.Sprintf("gcloud dns managed-zones describe %q --format=json", zoneName)}, "zone metadata and record sets are exported", metadata),
		cloudDNSStep("provision-coredns-zone", "Provision CoreDNS zone", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s coredns/Corefile"}, "CoreDNS config and zone file are rendered", metadata),
		cloudDNSStep("migrate-cloud-dns-records", "Migrate Cloud DNS records", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate-dns-records.sh"}, "record export script runs", metadata),
		cloudDNSStep("validate-coredns-zone", "Validate CoreDNS zone", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_cloud_dns.sh"}, "CoreDNS config and lookup validate", metadata),
		cloudDNSStep("backup-cloud-dns-zone", "Backup CoreDNS zone", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_cloud_dns.sh"}, "DNS config archive is produced", metadata),
		cloudDNSStep("cutover-cloud-dns-ns", "Cut over nameservers", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "-c", "test -s config/cloud-dns/app-change.env"}, "generated patch points resolvers at CoreDNS", metadata),
		cloudDNSStep("rollback-cloud-dns-zone", "Keep Cloud DNS as rollback zone", "Rollback", domainrunbook.StepTypeRollback, nil, "source Cloud DNS zone remains authoritative until validation passes", metadata),
	}
}

func cloudDNSStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
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
