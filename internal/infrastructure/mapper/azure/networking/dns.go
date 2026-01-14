// Package networking provides mappers for Azure networking services.
package networking

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
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

	// Generate setup script
	setupScript := m.generateSetupScript(zoneName)
	result.AddScript("setup_dns.sh", []byte(setupScript))

	result.AddWarning(fmt.Sprintf("DNS zone '%s' has %d record sets. Configure them in the zone file.", zoneName, numberOfRecordSets))
	result.AddWarning("CoreDNS configuration provided. PowerDNS alternative also available in config/powerdns/")
	result.AddManualStep("Update DNS records in config/coredns/*.zone files")
	result.AddManualStep("Update your domain registrar to point to the new DNS servers")
	result.AddManualStep("Test DNS resolution: dig @localhost example.com")
	result.AddManualStep("Configure DNSSEC if required")

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
