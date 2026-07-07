// Package networking provides mappers for Azure networking services.
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

// DNSMapper converts Azure DNS to CoreDNS or PowerDNS.
type DNSMapper struct {
	*mapper.BaseMapper
}

// NewDNSMapper creates a new Azure DNS to CoreDNS/PowerDNS mapper.
func NewDNSMapper() *DNSMapper {
	return &DNSMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeAzureDNS, nil),
	}
}

// Map converts an Azure DNS zone to a CoreDNS service.
func (m *DNSMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	zoneName := res.GetConfigString("name")
	if zoneName == "" {
		zoneName = res.Name
	}

	result := mapper.NewMappingResult("coredns")
	svc := result.DockerService

	// Use CoreDNS as the primary DNS server
	svc.Image = "coredns/coredns:1.11.1"
	svc.Command = []string{
		"-conf",
		"/etc/coredns/Corefile",
	}

	svc.Ports = []string{
		"53:53/udp",
		"53:53/tcp",
		"9153:9153", // Metrics
	}

	svc.Volumes = []string{
		"./config/coredns:/etc/coredns:ro",
	}

	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Labels = map[string]string{
		"homeport.source":    "azurerm_dns_zone",
		"homeport.zone_name": zoneName,
	}

	// Health check for CoreDNS
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD-SHELL", "dig @127.0.0.1 -p 53 health.check.local || exit 1"},
		Interval: 30 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  3,
	}

	// Get DNS zone properties
	numberOfRecordSets := 0
	if recordSets := res.Config["number_of_record_sets"]; recordSets != nil {
		if count, ok := recordSets.(float64); ok {
			numberOfRecordSets = int(count)
		}
	}

	// Generate CoreDNS Corefile
	corefileContent := m.generateCorefile(zoneName)
	result.AddConfig("config/coredns/Corefile", []byte(corefileContent))

	// Generate zone file
	zoneFileContent := m.generateZoneFile(zoneName, res)
	result.AddConfig(fmt.Sprintf("config/coredns/%s.zone", zoneName), []byte(zoneFileContent))

	// Generate PowerDNS alternative configuration
	powerDNSConfig := m.generatePowerDNSConfig(zoneName)
	result.AddConfig("config/powerdns/pdns.conf", []byte(powerDNSConfig))

	// Generate PowerDNS zone SQL
	powerDNSSQL := m.generatePowerDNSZoneSQL(zoneName, res)
	result.AddScript("powerdns_zones.sql", []byte(powerDNSSQL))
	result.AddConfig("config/dns/app-change.env", []byte(m.generateAppChange(zoneName)))
	result.AddConfig("config/dns/generated-zone.patch", []byte(m.generateZonePatch(zoneName)))

	// Generate setup script
	setupScript := m.generateSetupScript(zoneName)
	result.AddScript("setup_dns.sh", []byte(setupScript))
	result.AddScript("export_dns_zone.sh", []byte(m.generateExportScript(zoneName)))
	result.AddScript("validate_dns.sh", []byte(m.generateValidateScript(zoneName)))
	result.AddScript("backup_dns_zone.sh", []byte(m.generateBackupScript(zoneName)))
	result.AddScript("cutover_dns.sh", []byte(m.generateCutoverScript(zoneName)))

	result.AddWarning(fmt.Sprintf("DNS zone '%s' has %d record sets. Configure them in the zone file.", zoneName, numberOfRecordSets))
	result.AddWarning("CoreDNS configuration provided. PowerDNS alternative also available in config/powerdns/")
	for _, step := range netrunbook.DNS(zoneName, "azurerm_dns_zone", "export_dns_zone.sh") {
		result.AddRunbookStep(step)
	}
	for _, step := range m.runbook(zoneName) {
		result.AddRunbookStep(step)
	}

	return result, nil
}

// generateCorefile generates a CoreDNS Corefile configuration.
func (m *DNSMapper) generateCorefile(zoneName string) string {
	return fmt.Sprintf(`# CoreDNS configuration for Azure DNS zone: %s

# Main zone
%s {
    file /etc/coredns/%s.zone
    log
    errors
}

# Reverse DNS
in-addr.arpa {
    file /etc/coredns/reverse.zone
    log
    errors
}

# Health check endpoint
health.check.local {
    whoami
}

# Forward all other queries to upstream DNS
. {
    forward . 8.8.8.8 8.8.4.4
    log
    errors
    cache 30
}

# Prometheus metrics
. {
    prometheus :9153
}
`, zoneName, zoneName, zoneName)
}

// generateZoneFile generates a DNS zone file.
func (m *DNSMapper) generateZoneFile(zoneName string, res *resource.AWSResource) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("; Zone file for %s\n", zoneName))
	sb.WriteString(fmt.Sprintf("$ORIGIN %s.\n", zoneName))
	sb.WriteString("$TTL 3600\n\n")

	// SOA record
	sb.WriteString(fmt.Sprintf("@   IN  SOA ns1.%s. admin.%s. (\n", zoneName, zoneName))
	sb.WriteString("            2024010101  ; Serial\n")
	sb.WriteString("            3600        ; Refresh\n")
	sb.WriteString("            1800        ; Retry\n")
	sb.WriteString("            604800      ; Expire\n")
	sb.WriteString("            86400 )     ; Minimum TTL\n\n")

	// NS records
	sb.WriteString("; Name servers\n")
	sb.WriteString(fmt.Sprintf("@   IN  NS  ns1.%s.\n", zoneName))
	sb.WriteString(fmt.Sprintf("@   IN  NS  ns2.%s.\n\n", zoneName))

	// Sample records (to be updated)
	sb.WriteString("; A records\n")
	sb.WriteString("@           IN  A   192.0.2.1\n")
	sb.WriteString("www         IN  A   192.0.2.1\n")
	sb.WriteString("ns1         IN  A   192.0.2.2\n")
	sb.WriteString("ns2         IN  A   192.0.2.3\n\n")

	// CNAME records
	sb.WriteString("; CNAME records\n")
	sb.WriteString("; Add your CNAME records here\n")
	sb.WriteString("; Example:\n")
	sb.WriteString("; blog       IN  CNAME   www\n\n")

	// MX records
	sb.WriteString("; MX records\n")
	sb.WriteString("; Add your MX records here\n")
	sb.WriteString("; Example:\n")
	sb.WriteString("; @          IN  MX  10  mail\n")
	sb.WriteString("; mail       IN  A   192.0.2.4\n\n")

	// TXT records
	sb.WriteString("; TXT records\n")
	sb.WriteString("; Add your TXT records here\n")
	sb.WriteString("; Example:\n")
	sb.WriteString("; @          IN  TXT \"v=spf1 mx -all\"\n")

	return sb.String()
}

// generatePowerDNSConfig generates PowerDNS configuration.
func (m *DNSMapper) generatePowerDNSConfig(zoneName string) string {
	return fmt.Sprintf(`# PowerDNS configuration for Azure DNS zone: %s

# Backend
launch=gsqlite3
gsqlite3-database=/var/lib/powerdns/pdns.sqlite3

# API
api=yes
api-key=changeme
webserver=yes
webserver-address=0.0.0.0
webserver-port=8081
webserver-allow-from=0.0.0.0/0

# Logging
log-dns-queries=yes
log-dns-details=yes
loglevel=4

# Performance
cache-ttl=20
negquery-cache-ttl=60
query-cache-ttl=20

# Security
allow-axfr-ips=
disable-axfr=yes
`, zoneName)
}

// generatePowerDNSZoneSQL generates SQL for PowerDNS zone setup.
func (m *DNSMapper) generatePowerDNSZoneSQL(zoneName string, res *resource.AWSResource) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("-- PowerDNS zone setup for %s\n\n", zoneName))
	sb.WriteString("-- Create domain\n")
	sb.WriteString(fmt.Sprintf("INSERT INTO domains (name, type) VALUES ('%s', 'NATIVE');\n\n", zoneName))

	domainID := "${DOMAIN_ID}"
	sb.WriteString("-- Get domain ID (run after inserting domain)\n")
	sb.WriteString(fmt.Sprintf("-- DOMAIN_ID=$(sqlite3 /var/lib/powerdns/pdns.sqlite3 \"SELECT id FROM domains WHERE name='%s';\")\n\n", zoneName))

	sb.WriteString("-- SOA record\n")
	sb.WriteString("INSERT INTO records (domain_id, name, type, content, ttl, prio) VALUES\n")
	sb.WriteString(fmt.Sprintf("(%s, '%s', 'SOA', 'ns1.%s. admin.%s. 2024010101 3600 1800 604800 86400', 3600, 0);\n\n", domainID, zoneName, zoneName, zoneName))

	sb.WriteString("-- NS records\n")
	sb.WriteString("INSERT INTO records (domain_id, name, type, content, ttl, prio) VALUES\n")
	sb.WriteString(fmt.Sprintf("(%s, '%s', 'NS', 'ns1.%s.', 3600, 0),\n", domainID, zoneName, zoneName))
	sb.WriteString(fmt.Sprintf("(%s, '%s', 'NS', 'ns2.%s.', 3600, 0);\n\n", domainID, zoneName, zoneName))

	sb.WriteString("-- A records (examples - update with your records)\n")
	sb.WriteString("INSERT INTO records (domain_id, name, type, content, ttl, prio) VALUES\n")
	sb.WriteString(fmt.Sprintf("(%s, '%s', 'A', '192.0.2.1', 3600, 0),\n", domainID, zoneName))
	sb.WriteString(fmt.Sprintf("(%s, 'www.%s', 'A', '192.0.2.1', 3600, 0),\n", domainID, zoneName))
	sb.WriteString(fmt.Sprintf("(%s, 'ns1.%s', 'A', '192.0.2.2', 3600, 0),\n", domainID, zoneName))
	sb.WriteString(fmt.Sprintf("(%s, 'ns2.%s', 'A', '192.0.2.3', 3600, 0);\n", domainID, zoneName))

	return sb.String()
}

// generateSetupScript generates a setup script.
func (m *DNSMapper) generateSetupScript(zoneName string) string {
	return fmt.Sprintf(`#!/bin/bash
# Setup script for Azure DNS zone: %s

set -e

echo "Creating CoreDNS configuration directory..."
mkdir -p ./config/coredns
mkdir -p ./config/powerdns

echo "Starting CoreDNS server..."
docker-compose up -d coredns

echo "Waiting for CoreDNS to be ready..."
sleep 5

echo ""
echo "Testing DNS resolution..."
dig @localhost %s

echo ""
echo "CoreDNS is running!"
echo "DNS server listening on port 53 (UDP/TCP)"
echo "Metrics available at: http://localhost:9153/metrics"
echo ""
echo "Next steps:"
echo "1. Update DNS records in config/coredns/%s.zone"
echo "2. Reload CoreDNS: docker-compose restart coredns"
echo "3. Test DNS: dig @localhost www.%s"
echo "4. Update your domain registrar's nameservers"
echo ""
echo "Alternative: To use PowerDNS instead, see config/powerdns/"
`, zoneName, zoneName, zoneName, zoneName)
}

func (m *DNSMapper) generateAppChange(zoneName string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_DNS_ZONE=%s\nCOREDNS_ENDPOINT=coredns:53\nZONE_FILE=config/coredns/%s.zone\nGENERATED_PATCH=config/dns/generated-zone.patch\n", zoneName, zoneName)
}

func (m *DNSMapper) generateZonePatch(zoneName string) string {
	return fmt.Sprintf("--- a/dns/%s.zone\n+++ b/dns/%s.zone\n@@\n-AZURE_DNS_ZONE=%s\n+NAMESERVER_1=ns1.%s\n+NAMESERVER_2=ns2.%s\n+DNS_TARGET=coredns:53\n", zoneName, zoneName, zoneName, zoneName, zoneName)
}

func (m *DNSMapper) generateExportScript(zoneName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\nOUTPUT_DIR=\"${OUTPUT_DIR:-./dns-export}\"\nmkdir -p \"$OUTPUT_DIR\"\naz network dns record-set list --zone-name %q --resource-group \"${AZURE_RESOURCE_GROUP}\" > \"$OUTPUT_DIR/%s-recordsets.json\"\n", zoneName, zoneName)
}

func (m *DNSMapper) generateValidateScript(zoneName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/coredns/Corefile\ntest -s config/coredns/%s.zone\ntest -s config/dns/app-change.env\ngrep -q %q config/dns/app-change.env\n", zoneName, zoneName)
}

func (m *DNSMapper) generateBackupScript(zoneName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/dns-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/coredns config/powerdns config/dns dns-export 2>/dev/null || tar -czf \"$archive\" config/coredns config/powerdns config/dns\necho \"$archive\"\n", strings.ReplaceAll(zoneName, ".", "-"))
}

func (m *DNSMapper) generateCutoverScript(zoneName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/dns/app-change.env\ntest \"$SOURCE_DNS_ZONE\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Publish generated NS/A/CNAME/TXT records from $GENERATED_PATCH for $SOURCE_DNS_ZONE\"\n", zoneName)
}

func (m *DNSMapper) runbook(zoneName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "dns", "source": "azurerm_dns_zone", "zone": zoneName, "target": "coredns"}
	return []domainrunbook.Step{
		m.step("backup-dns-zone", "Backup DNS zone config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_dns_zone.sh"}, "DNS migration artifacts are archived", metadata),
	}
}

func (m *DNSMapper) step(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: "shell", Command: command, SuccessCondition: success, Metadata: metadata}
}
