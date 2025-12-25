// Package networking provides mappers for Azure networking services.
package networking

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudexit/cloudexit/internal/domain/mapper"
	"github.com/cloudexit/cloudexit/internal/domain/resource"
)

// VNetMapper converts Azure Virtual Network to Docker networks.
type VNetMapper struct {
	*mapper.BaseMapper
}

// NewVNetMapper creates a new Azure Virtual Network to Docker network mapper.
func NewVNetMapper() *VNetMapper {
	return &VNetMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeAzureVNet, nil),
	}
}

// Map converts an Azure Virtual Network to Docker network configurations.
func (m *VNetMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	vnetName := res.GetConfigString("name")
	if vnetName == "" {
		vnetName = res.Name
	}

	result := mapper.NewMappingResult(m.sanitizeName(vnetName))
	svc := result.DockerService

	// Virtual networks don't create services, but we create a network management container
	svc.Image = "alpine:latest"
	svc.Command = []string{
		"sh",
		"-c",
		"echo 'Docker network for Azure VNet: " + vnetName + "' && tail -f /dev/null",
	}
	svc.Networks = []string{m.sanitizeName(vnetName)}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"cloudexit.source":    "azurerm_virtual_network",
		"cloudexit.vnet_name": vnetName,
		"cloudexit.type":      "network-placeholder",
	}

	// Get address space
	var addressSpaces []string
	if addrSpace := res.Config["address_space"]; addrSpace != nil {
		if addrSlice, ok := addrSpace.([]interface{}); ok {
			for _, addr := range addrSlice {
				if addrStr, ok := addr.(string); ok {
					addressSpaces = append(addressSpaces, addrStr)
				}
			}
		}
	}

	// Get DNS servers
	var dnsServers []string
	if dns := res.Config["dns_servers"]; dns != nil {
		if dnsSlice, ok := dns.([]interface{}); ok {
			for _, server := range dnsSlice {
				if serverStr, ok := server.(string); ok {
					dnsServers = append(dnsServers, serverStr)
				}
			}
		}
	}

	// Handle subnets
	var subnets []interface{}
	if subnetConfig := res.Config["subnet"]; subnetConfig != nil {
		if subnetSlice, ok := subnetConfig.([]interface{}); ok {
			subnets = subnetSlice
		}
	}

	// Generate Docker network configurations for each subnet
	networkConfigs := m.generateNetworkConfigs(vnetName, addressSpaces, subnets)
	for networkName, config := range networkConfigs {
		result.AddConfig(fmt.Sprintf("config/networks/%s.yml", networkName), []byte(config))
	}

	// Generate docker-compose network section
	composeNetworks := m.generateComposeNetworks(vnetName, addressSpaces, subnets)
	result.AddConfig("config/networks/docker-compose-networks.yml", []byte(composeNetworks))

	// Generate network setup script
	setupScript := m.generateSetupScript(vnetName, addressSpaces, subnets)
	result.AddScript("setup_networks.sh", []byte(setupScript))

	// Generate network diagram
	diagram := m.generateNetworkDiagram(vnetName, addressSpaces, subnets)
	result.AddConfig("config/networks/network-diagram.txt", []byte(diagram))

	// Add warnings and manual steps
	if len(addressSpaces) > 0 {
		result.AddWarning(fmt.Sprintf("VNet address space: %s. Docker networks created with similar CIDR ranges.", strings.Join(addressSpaces, ", ")))
	}

	if len(dnsServers) > 0 {
		result.AddWarning(fmt.Sprintf("Custom DNS servers configured: %s. Set these in Docker daemon config.", strings.Join(dnsServers, ", ")))
		result.AddManualStep("Configure DNS servers in /etc/docker/daemon.json")
	}

	result.AddWarning("Azure VNet peering is not supported in Docker. Use overlay networks for multi-host networking.")
	result.AddWarning("Network Security Groups (NSGs) should be replaced with Docker firewall rules or iptables.")
	result.AddWarning("Service endpoints are not applicable. Use Docker service discovery instead.")

	result.AddManualStep("Create Docker networks using the generated configurations")
	result.AddManualStep("Update services to use the appropriate networks")
	result.AddManualStep("Configure firewall rules if NSGs were used")
	result.AddManualStep("Review network isolation requirements")

	return result, nil
}

// generateNetworkConfigs generates Docker network configurations for subnets.
func (m *VNetMapper) generateNetworkConfigs(vnetName string, addressSpaces []string, subnets []interface{}) map[string]string {
	configs := make(map[string]string)

	if len(subnets) > 0 {
		for i, subnet := range subnets {
			if subnetMap, ok := subnet.(map[string]interface{}); ok {
				name, _ := subnetMap["name"].(string)
				addressPrefix, _ := subnetMap["address_prefix"].(string)

				if name == "" {
					name = fmt.Sprintf("subnet-%d", i)
				}

				networkName := m.sanitizeName(fmt.Sprintf("%s-%s", vnetName, name))
				config := m.generateNetworkConfig(networkName, addressPrefix)
				configs[networkName] = config
			}
		}
	} else if len(addressSpaces) > 0 {
		// Create a default network for the VNet
		networkName := m.sanitizeName(vnetName)
		config := m.generateNetworkConfig(networkName, addressSpaces[0])
		configs[networkName] = config
	}

	return configs
}

// generateNetworkConfig generates a single Docker network configuration.
func (m *VNetMapper) generateNetworkConfig(networkName, subnet string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Docker network configuration for: %s\n\n", networkName))
	sb.WriteString("# To create this network, run:\n")
	sb.WriteString(fmt.Sprintf("# docker network create \\\n"))
	sb.WriteString(fmt.Sprintf("#   --driver=bridge \\\n"))

	if subnet != "" {
		sb.WriteString(fmt.Sprintf("#   --subnet=%s \\\n", subnet))
	}

	sb.WriteString(fmt.Sprintf("#   %s\n\n", networkName))

	sb.WriteString("Network details:\n")
	sb.WriteString(fmt.Sprintf("  Name: %s\n", networkName))
	sb.WriteString("  Driver: bridge\n")
	if subnet != "" {
		sb.WriteString(fmt.Sprintf("  Subnet: %s\n", subnet))
	}
	sb.WriteString("  Attachable: true\n")
	sb.WriteString("  Internal: false\n")

	return sb.String()
}

// generateComposeNetworks generates docker-compose network definitions.
func (m *VNetMapper) generateComposeNetworks(vnetName string, addressSpaces []string, subnets []interface{}) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Docker Compose network definitions for Azure VNet: %s\n\n", vnetName))
	sb.WriteString("# Add this to your docker-compose.yml file:\n\n")
	sb.WriteString("networks:\n")

	if len(subnets) > 0 {
		for i, subnet := range subnets {
			if subnetMap, ok := subnet.(map[string]interface{}); ok {
				name, _ := subnetMap["name"].(string)
				addressPrefix, _ := subnetMap["address_prefix"].(string)

				if name == "" {
					name = fmt.Sprintf("subnet-%d", i)
				}

				networkName := m.sanitizeName(fmt.Sprintf("%s-%s", vnetName, name))
				sb.WriteString(fmt.Sprintf("  %s:\n", networkName))
				sb.WriteString("    driver: bridge\n")

				if addressPrefix != "" {
					sb.WriteString("    ipam:\n")
					sb.WriteString("      config:\n")
					sb.WriteString(fmt.Sprintf("        - subnet: %s\n", addressPrefix))
				}

				sb.WriteString("    labels:\n")
				sb.WriteString(fmt.Sprintf("      cloudexit.vnet: %s\n", vnetName))
				sb.WriteString(fmt.Sprintf("      cloudexit.subnet: %s\n", name))
				sb.WriteString("\n")
			}
		}
	} else if len(addressSpaces) > 0 {
		networkName := m.sanitizeName(vnetName)
		sb.WriteString(fmt.Sprintf("  %s:\n", networkName))
		sb.WriteString("    driver: bridge\n")
		sb.WriteString("    ipam:\n")
		sb.WriteString("      config:\n")
		sb.WriteString(fmt.Sprintf("        - subnet: %s\n", addressSpaces[0]))
		sb.WriteString("    labels:\n")
		sb.WriteString(fmt.Sprintf("      cloudexit.vnet: %s\n", vnetName))
		sb.WriteString("\n")
	}

	return sb.String()
}

// generateSetupScript generates a network setup script.
func (m *VNetMapper) generateSetupScript(vnetName string, addressSpaces []string, subnets []interface{}) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("#!/bin/bash\n"))
	sb.WriteString(fmt.Sprintf("# Network setup script for Azure VNet: %s\n\n", vnetName))
	sb.WriteString("set -e\n\n")

	sb.WriteString("echo 'Creating Docker networks for Azure VNet...'\n\n")

	if len(subnets) > 0 {
		for i, subnet := range subnets {
			if subnetMap, ok := subnet.(map[string]interface{}); ok {
				name, _ := subnetMap["name"].(string)
				addressPrefix, _ := subnetMap["address_prefix"].(string)

				if name == "" {
					name = fmt.Sprintf("subnet-%d", i)
				}

				networkName := m.sanitizeName(fmt.Sprintf("%s-%s", vnetName, name))

				sb.WriteString(fmt.Sprintf("# Create network: %s\n", networkName))
				sb.WriteString(fmt.Sprintf("if ! docker network ls | grep -q %s; then\n", networkName))
				sb.WriteString(fmt.Sprintf("  docker network create \\\n"))
				sb.WriteString("    --driver=bridge \\\n")

				if addressPrefix != "" {
					sb.WriteString(fmt.Sprintf("    --subnet=%s \\\n", addressPrefix))
				}

				sb.WriteString(fmt.Sprintf("    --label cloudexit.vnet=%s \\\n", vnetName))
				sb.WriteString(fmt.Sprintf("    --label cloudexit.subnet=%s \\\n", name))
				sb.WriteString(fmt.Sprintf("    %s\n", networkName))
				sb.WriteString(fmt.Sprintf("  echo 'Created network: %s'\n", networkName))
				sb.WriteString("else\n")
				sb.WriteString(fmt.Sprintf("  echo 'Network already exists: %s'\n", networkName))
				sb.WriteString("fi\n\n")
			}
		}
	} else if len(addressSpaces) > 0 {
		networkName := m.sanitizeName(vnetName)
		sb.WriteString(fmt.Sprintf("# Create network: %s\n", networkName))
		sb.WriteString(fmt.Sprintf("if ! docker network ls | grep -q %s; then\n", networkName))
		sb.WriteString(fmt.Sprintf("  docker network create \\\n"))
		sb.WriteString("    --driver=bridge \\\n")
		sb.WriteString(fmt.Sprintf("    --subnet=%s \\\n", addressSpaces[0]))
		sb.WriteString(fmt.Sprintf("    --label cloudexit.vnet=%s \\\n", vnetName))
		sb.WriteString(fmt.Sprintf("    %s\n", networkName))
		sb.WriteString(fmt.Sprintf("  echo 'Created network: %s'\n", networkName))
		sb.WriteString("else\n")
		sb.WriteString(fmt.Sprintf("  echo 'Network already exists: %s'\n", networkName))
		sb.WriteString("fi\n\n")
	}

	sb.WriteString("echo ''\n")
	sb.WriteString("echo 'Docker networks created successfully!'\n")
	sb.WriteString("echo 'List networks:'\n")
	sb.WriteString("docker network ls --filter label=cloudexit.vnet\n")

	return sb.String()
}

// generateNetworkDiagram generates a text-based network diagram.
func (m *VNetMapper) generateNetworkDiagram(vnetName string, addressSpaces []string, subnets []interface{}) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Network Diagram for Azure VNet: %s\n", vnetName))
	sb.WriteString(strings.Repeat("=", 60) + "\n\n")

	if len(addressSpaces) > 0 {
		sb.WriteString(fmt.Sprintf("VNet Address Space: %s\n", strings.Join(addressSpaces, ", ")))
		sb.WriteString(strings.Repeat("-", 60) + "\n\n")
	}

	if len(subnets) > 0 {
		sb.WriteString("Subnets:\n\n")
		for i, subnet := range subnets {
			if subnetMap, ok := subnet.(map[string]interface{}); ok {
				name, _ := subnetMap["name"].(string)
				addressPrefix, _ := subnetMap["address_prefix"].(string)

				if name == "" {
					name = fmt.Sprintf("subnet-%d", i)
				}

				networkName := m.sanitizeName(fmt.Sprintf("%s-%s", vnetName, name))

				sb.WriteString(fmt.Sprintf("  [%s]\n", name))
				sb.WriteString(fmt.Sprintf("    Docker Network: %s\n", networkName))
				if addressPrefix != "" {
					sb.WriteString(fmt.Sprintf("    CIDR: %s\n", addressPrefix))
				}
				sb.WriteString("\n")
			}
		}
	}

	sb.WriteString(strings.Repeat("=", 60) + "\n")
	sb.WriteString("Docker Network Mapping:\n")
	sb.WriteString(strings.Repeat("-", 60) + "\n\n")
	sb.WriteString("Azure VNet → Docker Bridge Networks\n")
	sb.WriteString("Azure Subnets → Separate Docker Networks\n")
	sb.WriteString("Azure VNet Peering → Not supported (use overlay networks)\n")
	sb.WriteString("Azure NSGs → Docker firewall rules / iptables\n")
	sb.WriteString("Azure Service Endpoints → Docker service discovery\n")

	return sb.String()
}

// sanitizeName creates a valid Docker network name.
func (m *VNetMapper) sanitizeName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "_", "-")
	validName := ""
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '.' {
			validName += string(ch)
		}
	}
	validName = strings.TrimLeft(validName, "-0123456789.")
	if validName == "" {
		validName = "vnet"
	}
	return validName
}
