// Package security provides mappers for Azure security services.
package security

import (
	"context"
	"fmt"
	"strings"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

// FirewallMapper converts Azure Firewall to iptables or OPNsense.
type FirewallMapper struct {
	*mapper.BaseMapper
}

// NewFirewallMapper creates a new Azure Firewall mapper.
func NewFirewallMapper() *FirewallMapper {
	return &FirewallMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeAzureFirewall, nil),
	}
}

// Map converts an Azure Firewall to iptables rules or OPNsense container.
func (m *FirewallMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	firewallName := res.GetConfigString("name")
	if firewallName == "" {
		firewallName = res.Name
	}

	skuTier := res.GetConfigString("sku_tier")

	result := mapper.NewMappingResult("opnsense")
	svc := result.DockerService

	// Use OPNsense for full firewall functionality
	svc.Image = "opnsense/opnsense:latest"
	svc.Environment = map[string]string{
		"TZ": "UTC",
	}
	svc.Ports = []string{
		"80:80",
		"443:443",
		"8443:8443",
	}
	svc.Volumes = []string{
		"./data/opnsense/config:/conf",
		"./data/opnsense/log:/var/log",
	}
	svc.Networks = []string{"homeport"}
	svc.Labels = map[string]string{
		"homeport.source":        "azurerm_firewall",
		"homeport.firewall_name": firewallName,
		"homeport.sku_tier":      skuTier,
		"traefik.enable":          "false",
	}
	svc.CapAdd = []string{"NET_ADMIN", "NET_RAW"}
	svc.Restart = "unless-stopped"

	iptablesScript := m.generateIptablesScript(res, firewallName)
	result.AddScript("setup_iptables.sh", []byte(iptablesScript))

	opnsenseConfig := m.generateOPNsenseConfig(res, firewallName)
	result.AddConfig("config/opnsense/config.xml", []byte(opnsenseConfig))

	migrationGuide := m.generateMigrationGuide(firewallName)
	result.AddConfig("docs/firewall_migration.md", []byte(migrationGuide))

	if skuTier == "Premium" {
		result.AddWarning("Azure Firewall Premium features (TLS inspection, IDPS) require OPNsense plugins.")
		result.AddManualStep("Install OPNsense IDS/IPS plugins for equivalent functionality")
	}

	if threatIntel := res.Config["threat_intel_mode"]; threatIntel != nil {
		result.AddWarning("Threat intelligence mode enabled. Configure OPNsense threat feeds.")
		result.AddManualStep("Set up threat intelligence feeds in OPNsense")
	}

	result.AddManualStep("Access OPNsense at https://localhost:8443")
	result.AddManualStep("Default credentials: root/opnsense")
	result.AddManualStep("Import firewall rules from migration guide")
	result.AddManualStep("For simple setups, use iptables script instead of OPNsense")

	result.AddWarning("Azure Firewall rules must be converted to OPNsense/iptables format")
	result.AddWarning("Application rules require OPNsense proxy or manual conversion")

	return result, nil
}

func (m *FirewallMapper) generateIptablesScript(res *resource.AWSResource, firewallName string) string {
	var rules strings.Builder

	rules.WriteString(fmt.Sprintf(`#!/bin/bash
# iptables rules migrated from Azure Firewall: %s
set -e

echo "Setting up iptables rules..."

# Flush existing rules
iptables -F
iptables -X
iptables -t nat -F
iptables -t nat -X

# Default policies
iptables -P INPUT DROP
iptables -P FORWARD DROP
iptables -P OUTPUT ACCEPT

# Allow loopback
iptables -A INPUT -i lo -j ACCEPT
iptables -A OUTPUT -o lo -j ACCEPT

# Allow established connections
iptables -A INPUT -m state --state ESTABLISHED,RELATED -j ACCEPT
iptables -A FORWARD -m state --state ESTABLISHED,RELATED -j ACCEPT

`, firewallName))

	// Extract network rules if available
	if networkRules := res.Config["network_rule_collection"]; networkRules != nil {
		rules.WriteString("# Network Rules (from Azure Firewall)\n")
		rules.WriteString("# TODO: Convert Azure network rules to iptables format\n")
		rules.WriteString("# Example:\n")
		rules.WriteString("# iptables -A FORWARD -p tcp --dport 443 -j ACCEPT\n\n")
	}

	// Extract NAT rules if available
	if natRules := res.Config["nat_rule_collection"]; natRules != nil {
		rules.WriteString("# NAT Rules (from Azure Firewall)\n")
		rules.WriteString("# TODO: Convert Azure NAT rules to iptables format\n")
		rules.WriteString("# Example:\n")
		rules.WriteString("# iptables -t nat -A PREROUTING -p tcp --dport 80 -j DNAT --to-destination 10.0.0.10:80\n\n")
	}

	rules.WriteString(`# Enable IP forwarding
echo 1 > /proc/sys/net/ipv4/ip_forward

# Save rules
iptables-save > /etc/iptables/rules.v4

echo "iptables rules applied!"
`)

	return rules.String()
}

func (m *FirewallMapper) generateOPNsenseConfig(res *resource.AWSResource, firewallName string) string {
	return fmt.Sprintf(`<?xml version="1.0"?>
<opnsense>
  <system>
    <hostname>%s</hostname>
    <domain>local</domain>
    <timezone>UTC</timezone>
  </system>
  <interfaces>
    <wan>
      <enable>1</enable>
      <if>em0</if>
      <ipaddr>dhcp</ipaddr>
    </wan>
    <lan>
      <enable>1</enable>
      <if>em1</if>
      <ipaddr>10.0.0.1</ipaddr>
      <subnet>24</subnet>
    </lan>
  </interfaces>
  <filter>
    <!-- Rules will be populated during migration -->
    <rule>
      <type>pass</type>
      <interface>lan</interface>
      <protocol>any</protocol>
      <source>lan</source>
      <destination>any</destination>
      <descr>Allow LAN to any</descr>
    </rule>
  </filter>
</opnsense>
`, firewallName)
}

func (m *FirewallMapper) generateMigrationGuide(firewallName string) string {
	return fmt.Sprintf(`# Azure Firewall to Self-Hosted Migration Guide

## Source: %s

## Overview

Azure Firewall features map to self-hosted alternatives as follows:

| Azure Feature | Self-Hosted Alternative |
|--------------|------------------------|
| Network Rules | iptables / OPNsense firewall rules |
| Application Rules | OPNsense proxy / Squid |
| NAT Rules | iptables NAT / OPNsense NAT |
| Threat Intelligence | OPNsense threat feeds |
| TLS Inspection | OPNsense proxy with SSL bump |

## Migration Steps

### 1. Network Rules
Convert Azure network rules to iptables format:
`+"```"+`bash
# Azure: Allow TCP 443 from 10.0.0.0/24 to any
iptables -A FORWARD -s 10.0.0.0/24 -p tcp --dport 443 -j ACCEPT
`+"```"+`

### 2. NAT Rules
Convert Azure NAT rules to iptables DNAT:
`+"```"+`bash
# Azure: DNAT port 80 to internal server
iptables -t nat -A PREROUTING -p tcp --dport 80 -j DNAT --to-destination 10.0.0.10:80
`+"```"+`

### 3. Application Rules
For FQDN-based filtering, use OPNsense proxy or DNS-based blocking.

## OPNsense Setup

1. Access OPNsense at https://localhost:8443
2. Navigate to Firewall > Rules
3. Import rules based on Azure configuration

## Testing

After migration, test each rule category:
- [ ] Inbound connectivity
- [ ] Outbound connectivity
- [ ] NAT rules
- [ ] Application filtering (if applicable)
`, firewallName)
}
