// Package networking provides mappers for AWS networking services.
package networking

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

// Route53Mapper converts AWS Route53 zones to CoreDNS or PowerDNS.
type Route53Mapper struct {
	*mapper.BaseMapper
}

// NewRoute53Mapper creates a new Route53 to CoreDNS/PowerDNS mapper.
func NewRoute53Mapper() *Route53Mapper {
	return &Route53Mapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeRoute53Zone, nil),
	}
}

// Map converts a Route53 hosted zone to a CoreDNS service.
func (m *Route53Mapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	zoneName := res.Config["name"]
	if zoneName == nil {
		zoneName = res.Name
	}
	zoneNameStr := fmt.Sprintf("%v", zoneName)

	// Use CoreDNS as the primary DNS server (cloud-native, simpler than PowerDNS)
	result := mapper.NewMappingResult("coredns")
	svc := result.DockerService

	// Configure CoreDNS service
	svc.Image = "coredns/coredns:1.11.1"
	svc.Ports = []string{
		"53:53/tcp",
		"53:53/udp",
	}
	svc.Volumes = []string{
		"./config/coredns:/etc/coredns",
	}
	svc.Command = []string{
		"-conf", "/etc/coredns/Corefile",
	}
	svc.Networks = []string{"homeport"}
	svc.Labels = map[string]string{
		"homeport.source":    "aws_route53",
		"homeport.zone_name": zoneNameStr,
	}
	svc.Restart = "unless-stopped"

	// Add health check
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD-SHELL", "dig @127.0.0.1 -p 53 health.check.dns +short || exit 1"},
		Interval: 30 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  3,
	}

	// Generate Corefile configuration
	corefile := m.generateCorefile(res, zoneNameStr)
	result.AddConfig("config/coredns/Corefile", []byte(corefile))

	// Generate zone file
	zoneFile := m.generateZoneFile(res, zoneNameStr)
	result.AddConfig(fmt.Sprintf("config/coredns/db.%s", m.sanitizeZoneName(zoneNameStr)), []byte(zoneFile))

	// Generate PowerDNS alternative configuration
	powerDNSConfig := m.generatePowerDNSConfig(res, zoneNameStr)
	result.AddConfig("config/powerdns/pdns.conf", []byte(powerDNSConfig))
	result.AddConfig("config/powerdns/README.md", []byte("# Alternative PowerDNS configuration\nIf you prefer PowerDNS over CoreDNS, use this configuration.\n"))

	// Generate migration script
	migrationScript := m.generateMigrationScript(res)
	result.AddScript("scripts/route53-export.sh", []byte(migrationScript))

	// Handle hosted zone type
	if isPrivate := res.GetConfigBool("private_zone"); isPrivate {
		result.AddWarning("Private hosted zone detected. CoreDNS will serve this zone only within Docker networks.")
		result.AddManualStep("Ensure Docker networks are properly configured for private DNS resolution")
	} else {
		result.AddWarning("Public hosted zone detected. Update your domain's nameservers to point to your CoreDNS instance.")
		result.AddManualStep("Update domain nameservers at your registrar to point to your DNS server")
	}

	// Handle DNSSEC
	if dnssec := res.GetConfigBool("dnssec_config.signing_enabled"); dnssec {
		result.AddWarning("DNSSEC is enabled on Route53. CoreDNS supports DNSSEC via the dnssec plugin.")
		result.AddManualStep("Configure DNSSEC keys and signing in CoreDNS")

		dnssecConfig := m.generateDNSSECConfig(res)
		result.AddConfig("config/coredns/dnssec-config.txt", []byte(dnssecConfig))
	}

	// Handle query logging
	if res.GetConfigBool("enable_logging") {
		result.AddWarning("Route53 query logging detected. CoreDNS supports logging via the log plugin.")
	}

	// Handle health checks
	if m.hasHealthChecks(res) {
		result.AddWarning("Route53 health checks detected. These need to be migrated to external monitoring solutions.")
		result.AddManualStep("Set up external health checks using monitoring tools (Prometheus, Uptime Kuma, etc.)")
	}

	// Handle traffic policies
	if m.hasTrafficPolicies(res) {
		result.AddWarning("Route53 traffic policies detected. Implement equivalent routing logic using CoreDNS plugins or external DNS management.")
		result.AddManualStep("Review traffic policies and implement routing rules")
	}

	// Handle record types
	if m.hasComplexRecordTypes(res) {
		result.AddWarning("Complex DNS record types detected (geolocation, latency, weighted). CoreDNS requires plugins for advanced routing.")
		result.AddManualStep("Install and configure CoreDNS plugins for advanced routing (geoip, multi-answer)")
	}

	result.AddManualStep("Export Route53 zone data using: aws route53 list-resource-record-sets --hosted-zone-id <zone-id>")
	result.AddManualStep("Update zone file with exported DNS records")
	result.AddManualStep("Test DNS resolution: dig @localhost <domain>")
	result.AddManualStep("Monitor DNS query logs and performance")
	result.AddManualStep("Set up secondary DNS servers for redundancy")

	return result, nil
}

// generateCorefile creates the CoreDNS Corefile configuration.
func (m *Route53Mapper) generateCorefile(res *resource.AWSResource, zoneName string) string {
	config := `# Corefile - CoreDNS Configuration
# Generated from AWS Route53 Hosted Zone

# Health check endpoint
health.check.dns:53 {
    whoami
}

# Primary zone configuration
%s:53 {
    # Load zone data from file
    file /etc/coredns/db.%s

    # Enable query logging
    log

    # Enable error logging
    errors

    # Cache responses
    cache 30

    # Prometheus metrics
    prometheus :9153

    # Load balancing for multiple backends
    loadbalance round_robin

    # Reload zone file on changes
    reload
}

# Forward all other queries to upstream DNS
.:53 {
    # Forward to public DNS servers
    forward . 8.8.8.8 8.8.4.4 1.1.1.1 {
        max_concurrent 1000
    }

    # Cache upstream responses
    cache 60

    # Enable error logging
    errors

    # Enable query logging
    log

    # Prometheus metrics
    prometheus :9153
}
`

	return fmt.Sprintf(config, zoneName, m.sanitizeZoneName(zoneName))
}

// generateZoneFile creates a DNS zone file for CoreDNS.
func (m *Route53Mapper) generateZoneFile(res *resource.AWSResource, zoneName string) string {
	zoneFile := `$ORIGIN %s.
$TTL 3600

; SOA Record
@ IN SOA ns1.%s. admin.%s. (
    2024010101  ; Serial (update this when making changes)
    7200        ; Refresh
    3600        ; Retry
    1209600     ; Expire
    3600        ; Minimum TTL
)

; NS Records
@ IN NS ns1.%s.
@ IN NS ns2.%s.

; A Records for nameservers
ns1 IN A 127.0.0.1
ns2 IN A 127.0.0.1

; TODO: Add your DNS records here
; Export from Route53 and convert to zone file format
; Example records:

; A Record
; www IN A 192.0.2.1

; AAAA Record
; www IN AAAA 2001:db8::1

; CNAME Record
; blog IN CNAME www.%s.

; MX Records
; @ IN MX 10 mail.%s.
; @ IN MX 20 mail2.%s.

; TXT Records
; @ IN TXT "v=spf1 include:_spf.%s ~all"
; _dmarc IN TXT "v=DMARC1; p=quarantine; rua=mailto:dmarc@%s"

; SRV Records
; _service._tcp IN SRV 10 60 5060 server.%s.

; CAA Records
; @ IN CAA 0 issue "letsencrypt.org"
`

	return fmt.Sprintf(zoneFile, zoneName, zoneName, zoneName, zoneName, zoneName,
		zoneName, zoneName, zoneName, zoneName, zoneName, zoneName)
}

// generatePowerDNSConfig creates an alternative PowerDNS configuration.
func (m *Route53Mapper) generatePowerDNSConfig(res *resource.AWSResource, zoneName string) string {
	config := `# PowerDNS Configuration
# Alternative to CoreDNS

# Load modules
launch=bind

# Bind backend configuration
bind-config=/etc/powerdns/named.conf

# API Configuration
api=yes
api-key=changeme
webserver=yes
webserver-address=0.0.0.0
webserver-port=8081
webserver-allow-from=0.0.0.0/0

# Logging
loglevel=4
log-dns-queries=yes
log-dns-details=yes

# Performance
cache-ttl=60
query-cache-ttl=60
negquery-cache-ttl=60

# Security
allow-axfr-ips=
allow-notify-from=
disable-axfr=yes

# Zone configuration file
# Create named.conf:
# zone "%s" {
#     type master;
#     file "/etc/powerdns/zones/db.%s";
# };
`

	return fmt.Sprintf(config, zoneName, zoneName)
}

// generateMigrationScript creates a script to export Route53 records.
func (m *Route53Mapper) generateMigrationScript(res *resource.AWSResource) string {
	script := `#!/bin/bash
# Route53 to CoreDNS Migration Script

set -e

ZONE_ID="$1"
OUTPUT_FILE="${2:-zone-export.json}"

if [ -z "$ZONE_ID" ]; then
    echo "Usage: $0 <hosted-zone-id> [output-file]"
    echo "Example: $0 Z1234567890ABC zone-export.json"
    exit 1
fi

echo "Exporting Route53 hosted zone: $ZONE_ID"

# Export all record sets
aws route53 list-resource-record-sets \
    --hosted-zone-id "$ZONE_ID" \
    --output json > "$OUTPUT_FILE"

echo "Zone data exported to: $OUTPUT_FILE"

# Convert to zone file format
echo "Converting to zone file format..."

cat "$OUTPUT_FILE" | jq -r '.ResourceRecordSets[] |
    select(.Type != "NS" and .Type != "SOA") |
    "\(.Name) \(.TTL // 300) IN \(.Type) \(
        if .ResourceRecords then
            .ResourceRecords[].Value
        else
            .AliasTarget.DNSName
        end
    )"' > zone-records.txt

echo "Zone records saved to: zone-records.txt"
echo ""
echo "Next steps:"
echo "1. Review zone-records.txt"
echo "2. Add records to config/coredns/db.<zone>"
echo "3. Reload CoreDNS configuration"
echo "4. Test DNS resolution"
`

	return script
}

// generateDNSSECConfig creates DNSSEC configuration documentation.
func (m *Route53Mapper) generateDNSSECConfig(res *resource.AWSResource) string {
	config := `# DNSSEC Configuration for CoreDNS

CoreDNS supports DNSSEC via the dnssec plugin.

## Installation
The dnssec plugin is built into CoreDNS by default.

## Configuration
Add to your Corefile:

example.com:53 {
    file /etc/coredns/db.example.com

    # Enable DNSSEC signing
    dnssec {
        key file /etc/coredns/Kexample.com.+008+12345
        key file /etc/coredns/Kexample.com.+008+67890
    }

    cache 30
    log
    errors
}

## Generate DNSSEC Keys
Use dnssec-keygen to generate keys:

# Generate Zone Signing Key (ZSK)
dnssec-keygen -a ECDSAP256SHA256 -n ZONE example.com

# Generate Key Signing Key (KSK)
dnssec-keygen -a ECDSAP256SHA256 -n ZONE -f KSK example.com

## DS Records
1. Generate DS records from KSK
2. Add DS records to parent zone (at your registrar)

dnssec-dsfromkey Kexample.com.+008+67890.key

## Validation
Test DNSSEC:
dig +dnssec example.com

## TODO:
- [ ] Generate DNSSEC keys
- [ ] Configure keys in Corefile
- [ ] Add DS records to registrar
- [ ] Test DNSSEC validation
`

	return config
}

// sanitizeZoneName sanitizes the zone name for file naming.
func (m *Route53Mapper) sanitizeZoneName(zoneName string) string {
	// Remove trailing dot
	zoneName = strings.TrimSuffix(zoneName, ".")
	return zoneName
}

// hasHealthChecks checks if the Route53 zone has health checks configured.
func (m *Route53Mapper) hasHealthChecks(res *resource.AWSResource) bool {
	if healthCheck := res.Config["health_check"]; healthCheck != nil {
		return true
	}
	if healthCheckID := res.GetConfigString("health_check_id"); healthCheckID != "" {
		return true
	}
	return false
}

// hasTrafficPolicies checks if the Route53 zone uses traffic policies.
func (m *Route53Mapper) hasTrafficPolicies(res *resource.AWSResource) bool {
	if policy := res.Config["traffic_policy"]; policy != nil {
		return true
	}
	if policyID := res.GetConfigString("traffic_policy_instance_id"); policyID != "" {
		return true
	}
	return false
}

// hasComplexRecordTypes checks if the zone has complex routing policies.
func (m *Route53Mapper) hasComplexRecordTypes(res *resource.AWSResource) bool {
	// Check for geolocation, latency-based, weighted, or failover routing
	if geolocation := res.Config["geolocation_routing_policy"]; geolocation != nil {
		return true
	}
	if latency := res.Config["latency_routing_policy"]; latency != nil {
		return true
	}
	if weighted := res.Config["weighted_routing_policy"]; weighted != nil {
		return true
	}
	if failover := res.Config["failover_routing_policy"]; failover != nil {
		return true
	}
	if multivalue := res.Config["multivalue_answer_routing"]; multivalue != nil {
		return true
	}
	return false
}
