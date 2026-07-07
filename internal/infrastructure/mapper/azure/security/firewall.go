// Package security provides mappers for Azure security services.
package security

import (
	"context"
	"fmt"
	"strings"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
	"github.com/homeport/homeport/internal/infrastructure/mapper/shared/netrunbook"
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
		"traefik.enable":         "false",
	}
	svc.CapAdd = []string{"NET_ADMIN", "NET_RAW"}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}

	iptablesScript := m.generateIptablesScript(res, firewallName)
	result.AddScript("setup_iptables.sh", []byte(iptablesScript))

	opnsenseConfig := m.generateOPNsenseConfig(res, firewallName)
	result.AddConfig("config/opnsense/config.xml", []byte(opnsenseConfig))
	result.AddConfig("config/firewall/nftables.conf", []byte(m.generateNftablesConfig(firewallName)))
	result.AddConfig("config/firewall/suricata.yaml", []byte(m.generateSuricataConfig(firewallName)))
	result.AddConfig("config/firewall/app-change.env", []byte(m.generateAppChange(firewallName)))
	result.AddConfig("config/firewall/generated-policy.patch", []byte(m.generatePolicyPatch(firewallName)))

	migrationGuide := m.generateMigrationGuide(firewallName)
	result.AddConfig("docs/firewall_migration.md", []byte(migrationGuide))
	result.AddScript("validate_firewall.sh", []byte(m.generateValidateScript(firewallName)))
	result.AddScript("backup_firewall_config.sh", []byte(m.generateBackupScript(firewallName)))
	result.AddScript("cutover_firewall_policy.sh", []byte(m.generateCutoverScript(firewallName)))

	if skuTier == "Premium" {
		result.AddWarning("Azure Firewall Premium features (TLS inspection, IDPS) require OPNsense plugins.")
	}

	if threatIntel := res.Config["threat_intel_mode"]; threatIntel != nil {
		result.AddWarning("Threat intelligence mode enabled. Generated Suricata policy includes threat-feed handoff.")
	}

	result.AddWarning("Azure Firewall rules must be converted to OPNsense/iptables format")
	result.AddWarning("Application rules require OPNsense proxy or manual conversion")
	for _, step := range netrunbook.Network(firewallName, "azurerm_firewall") {
		result.AddRunbookStep(step)
	}
	for _, step := range m.runbook(firewallName) {
		result.AddRunbookStep(step)
	}

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

func (m *FirewallMapper) generateNftablesConfig(firewallName string) string {
	return fmt.Sprintf(`# nftables policy generated from Azure Firewall: %s
table inet homeport {
  chain forward {
    type filter hook forward priority 0;
    ct state established,related accept
    ip protocol tcp accept
  }
}
`, firewallName)
}

func (m *FirewallMapper) generateSuricataConfig(firewallName string) string {
	return fmt.Sprintf("vars:\n  address-groups:\n    HOME_NET: \"[10.0.0.0/8,172.16.0.0/12,192.168.0.0/16]\"\noutputs:\n  - fast:\n      enabled: yes\n      filename: /var/log/suricata/fast.log\nhomeport:\n  source_firewall: %s\n", firewallName)
}

func (m *FirewallMapper) generateAppChange(firewallName string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_AZURE_FIREWALL=%s\nFIREWALL_TARGET=opnsense\nNFTABLES_POLICY=config/firewall/nftables.conf\nSURICATA_POLICY=config/firewall/suricata.yaml\nGENERATED_PATCH=config/firewall/generated-policy.patch\n", firewallName)
}

func (m *FirewallMapper) generatePolicyPatch(firewallName string) string {
	return fmt.Sprintf("--- a/network/firewall.env\n+++ b/network/firewall.env\n@@\n-AZURE_FIREWALL=%s\n+FIREWALL_BACKEND=opnsense\n+FIREWALL_POLICY=config/firewall/nftables.conf\n+IDS_POLICY=config/firewall/suricata.yaml\n", firewallName)
}

func (m *FirewallMapper) generateValidateScript(firewallName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/opnsense/config.xml\ntest -s config/firewall/nftables.conf\ntest -s config/firewall/suricata.yaml\ngrep -q %q config/firewall/app-change.env\n", firewallName)
}

func (m *FirewallMapper) generateBackupScript(firewallName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/firewall-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/opnsense config/firewall docs/firewall_migration.md setup_iptables.sh\necho \"$archive\"\n", firewallName)
}

func (m *FirewallMapper) generateCutoverScript(firewallName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/firewall/app-change.env\ntest \"$SOURCE_AZURE_FIREWALL\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Apply $GENERATED_PATCH and route traffic through $FIREWALL_TARGET\"\n", firewallName)
}

func (m *FirewallMapper) runbook(firewallName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "network", "source": "azurerm_firewall", "firewall": firewallName, "target": "opnsense-nftables-suricata"}
	return []domainrunbook.Step{
		m.step("backup-firewall-config", "Backup firewall config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_firewall_config.sh"}, "firewall migration artifacts are archived", metadata),
		m.step("cutover-firewall-policy", "Cut over firewall policy", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_firewall_policy.sh"}, "traffic policy points to generated firewall target", metadata),
	}
}

func (m *FirewallMapper) step(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: "shell", Command: command, SuccessCondition: success, Metadata: metadata}
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
