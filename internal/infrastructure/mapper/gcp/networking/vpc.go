// Package networking provides mappers for GCP networking services.
package networking

import (
	"context"
	"fmt"
	"strings"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

// VPCMapper converts GCP VPC Network to Docker networks.
type VPCMapper struct {
	*mapper.BaseMapper
}

// NewVPCMapper creates a new GCP VPC Network to Docker networks mapper.
func NewVPCMapper() *VPCMapper {
	return &VPCMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeGCPVPCNetwork, nil),
	}
}

// Map converts a GCP VPC Network to Docker network configurations.
func (m *VPCMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	networkName := res.GetConfigString("name")
	if networkName == "" {
		networkName = res.Name
	}

	// Create result - no specific service, just network configuration
	result := mapper.NewMappingResult(fmt.Sprintf("vpc-%s", m.sanitizeName(networkName)))
	svc := result.DockerService

	// VPC Network doesn't map to a Docker service, but we create a placeholder
	// The actual network is created via Docker Compose networks section
	svc.Image = "alpine:latest"
	svc.Command = []string{"sh", "-c", "echo 'GCP VPC Network placeholder - networks defined in docker-compose.yml' && sleep infinity"}
	svc.Networks = []string{m.sanitizeName(networkName)}
	svc.Labels = map[string]string{
		"homeport.source":       "google_compute_network",
		"homeport.network_name": networkName,
	}
	svc.Restart = "no"

	// Extract routing mode
	routingMode := m.extractRoutingMode(res)
	svc.Labels["homeport.routing_mode"] = routingMode

	// Generate Docker network configuration
	networkConfig := m.generateDockerNetworkConfig(res, networkName, routingMode)
	result.AddConfig("docker-compose-networks.yml", []byte(networkConfig))

	// Generate network documentation
	networkDoc := m.generateNetworkDocumentation(res, networkName, routingMode)
	result.AddConfig("config/vpc/network-mapping.md", []byte(networkDoc))

	// Handle auto_create_subnetworks
	autoCreateSubnetworks := res.GetConfigBool("auto_create_subnetworks")
	if autoCreateSubnetworks {
		result.AddWarning("Auto-create subnetworks is enabled. GCP automatically creates subnets in each region.")
		result.AddManualStep("Configure Docker network subnets to match your regional requirements")
	}

	// Handle subnetworks
	if m.hasSubnetworks(res) {
		subnetConfig := m.generateSubnetworkConfig(res)
		result.AddConfig("config/vpc/subnetworks.yml", []byte(subnetConfig))
		result.AddManualStep("Review and configure subnetwork mappings in subnetworks.yml")
	}

	// Handle peering connections
	if m.hasPeeringConnections(res) {
		result.AddWarning("VPC peering connections detected. Docker networks can be connected using network attachments.")
		result.AddManualStep("Configure Docker network connections for cross-network communication")

		peeringConfig := m.generatePeeringConfig()
		result.AddConfig("config/vpc/peering-guide.md", []byte(peeringConfig))
	}

	// Handle shared VPC
	if m.isSharedVPC(res) {
		result.AddWarning("Shared VPC detected. In Docker, this maps to a shared network accessible by multiple projects/services.")
		result.AddManualStep("Configure shared Docker network for multi-tenant access")
	}

	// Handle firewall rules
	if m.hasFirewallRules(res) {
		result.AddWarning("GCP Firewall rules detected. Implement using iptables, nftables, or Docker network policies.")
		result.AddManualStep("Review Firewall rules and configure host firewall accordingly")

		firewallConfig := m.generateFirewallMapping()
		result.AddConfig("config/vpc/firewall-rules.md", []byte(firewallConfig))
	}

	// Handle Cloud Router
	if m.hasCloudRouter(res) {
		result.AddWarning("Cloud Router detected. For dynamic routing, configure BGP on your self-hosted router or use static routes.")
		result.AddManualStep("Configure routing for Docker networks (static or dynamic)")

		routerConfig := m.generateRouterConfig()
		result.AddConfig("config/vpc/router-setup.md", []byte(routerConfig))
	}

	// Handle Cloud NAT
	if m.hasCloudNAT(res) {
		result.AddWarning("Cloud NAT detected. Docker provides outbound NAT by default. Configure iptables for custom NAT behavior.")
		result.AddManualStep("Review NAT configuration and implement equivalent routing if needed")
	}

	// Handle VPN Gateway
	if m.hasVPNGateway(res) {
		result.AddWarning("VPN Gateway detected. Set up VPN server (WireGuard, OpenVPN) for secure remote access.")
		result.AddManualStep("Configure VPN server for remote access to Docker networks")

		vpnConfig := m.generateVPNConfig()
		result.AddConfig("config/vpc/vpn-setup.md", []byte(vpnConfig))
	}

	// Handle Cloud Interconnect
	if m.hasCloudInterconnect(res) {
		result.AddWarning("Cloud Interconnect detected. For on-premises connectivity, use VPN or dedicated connection.")
		result.AddManualStep("Configure site-to-site VPN or dedicated network link for on-premises connectivity")
	}

	// Handle Private Google Access
	if m.hasPrivateGoogleAccess(res) {
		result.AddWarning("Private Google Access enabled. In self-hosted environment, configure direct access to Google APIs if needed.")
		result.AddManualStep("Configure DNS and routing for any required Google API access")
	}

	// Handle VPC Service Controls
	if m.hasServiceControls(res) {
		result.AddWarning("VPC Service Controls detected. Implement equivalent access policies using network policies or service mesh.")
		result.AddManualStep("Configure access policies for service isolation")
	}

	// Handle Internal DNS
	if m.hasInternalDNS(res) {
		result.AddWarning("Internal DNS configuration detected. Set up DNS resolver (CoreDNS, dnsmasq) for container name resolution.")
		result.AddManualStep("Configure internal DNS for Docker services")

		dnsConfig := m.generateDNSConfig()
		result.AddConfig("config/vpc/dns-config.yml", []byte(dnsConfig))
	}

	// Handle MTU setting
	if mtu := res.GetConfigInt("mtu"); mtu > 0 {
		svc.Labels["homeport.mtu"] = fmt.Sprintf("%d", mtu)
		result.AddWarning(fmt.Sprintf("Custom MTU of %d detected. Configure Docker network MTU accordingly.", mtu))
	}

	result.AddManualStep("Create Docker networks defined in docker-compose-networks.yml")
	result.AddManualStep("Configure host firewall rules for network isolation")
	result.AddManualStep("Test network connectivity between services")
	result.AddManualStep("Set up network monitoring and observability")

	// Add comprehensive warning about VPC limitations
	result.AddWarning("IMPORTANT: GCP VPC provides advanced networking features that cannot be fully replicated with Docker networks alone. Consider using Kubernetes with network policies, or running a full virtual network solution if complex networking is required.")

	return result, nil
}

// extractRoutingMode extracts the VPC routing mode.
func (m *VPCMapper) extractRoutingMode(res *resource.AWSResource) string {
	if routingMode := res.GetConfigString("routing_mode"); routingMode != "" {
		return routingMode
	}
	return "REGIONAL" // Default routing mode
}

// generateDockerNetworkConfig creates Docker network configuration.
func (m *VPCMapper) generateDockerNetworkConfig(res *resource.AWSResource, networkName, routingMode string) string {
	sanitizedName := m.sanitizeName(networkName)

	// Use a default CIDR for Docker network
	cidrBlock := "172.20.0.0/16"

	config := `# Docker Compose Network Configuration
# Generated from GCP VPC Network: %s
# Routing Mode: %s
# Add this to your docker-compose.yml

networks:
  %s:
    driver: bridge
    ipam:
      config:
        - subnet: %s
    driver_opts:
      com.docker.network.bridge.name: br-%s
      com.docker.network.bridge.enable_ip_masquerade: "true"
      com.docker.network.bridge.enable_icc: "true"
      com.docker.network.driver.mtu: "1460"
    labels:
      homeport.source: "google_compute_network"
      homeport.network_name: "%s"
      homeport.routing_mode: "%s"

  # Private network for internal services (no external access)
  %s-private:
    driver: bridge
    internal: true
    ipam:
      config:
        - subnet: 172.21.0.0/16
    labels:
      homeport.source: "google_compute_network_private"
      homeport.network_name: "%s"
`

	return fmt.Sprintf(config, networkName, routingMode, sanitizedName, cidrBlock, sanitizedName, networkName, routingMode, sanitizedName, networkName)
}

// generateNetworkDocumentation creates network mapping documentation.
func (m *VPCMapper) generateNetworkDocumentation(res *resource.AWSResource, networkName, routingMode string) string {
	sanitizedName := m.sanitizeName(networkName)

	doc := fmt.Sprintf(`# GCP VPC Network to Docker Network Mapping

## Overview
GCP VPC Network: %s
Routing Mode: %s

## Docker Network Architecture

### Network Types
1. Bridge Networks - Default Docker networks with NAT
2. Overlay Networks - For multi-host networking (requires Docker Swarm)
3. Macvlan Networks - For containers to appear on physical network
4. Host Networks - Container uses host network stack

## GCP VPC Component Mapping

| GCP Component | Docker Equivalent | Notes |
|---------------|------------------|-------|
| VPC Network | Docker Network | Isolated network namespace |
| Subnetwork | IPAM Config | Subnet within Docker network |
| Firewall Rule | iptables/nftables | Host-level firewall rules |
| Route | Docker Routes | Managed automatically by Docker |
| Cloud Router | Host Router | BGP or static routing |
| Cloud NAT | iptables masquerade | Docker handles outbound NAT |
| VPC Peering | Network Connect | Connect Docker networks |
| Cloud VPN | WireGuard/OpenVPN | Self-hosted VPN server |
| Cloud Interconnect | Site-to-site VPN | Direct or VPN connection |
| Private Google Access | DNS/Routing | Direct API access |

## Network Isolation

### Public Services
Services that need internet access should be connected to network: %s

### Private Services
Services without internet access should use network: %s-private

## Routing Mode Implications

- REGIONAL: Routes within the same region (default Docker behavior)
- GLOBAL: Routes across all regions (requires overlay networks or multiple hosts)

## Migration Notes

1. GCP VPC Networks are flat networks with regional subnetworks
2. Subnets map to Docker network IPAM configurations
3. Firewall rules need manual implementation via iptables
4. NAT functionality is built into Docker bridge driver
5. Cloud Router/VPN require separate infrastructure setup

For detailed examples, see Docker Compose documentation.
`, networkName, routingMode, sanitizedName, sanitizedName)

	return doc
}

// generateSubnetworkConfig creates subnetwork configuration.
func (m *VPCMapper) generateSubnetworkConfig(res *resource.AWSResource) string {
	config := `# GCP Subnetwork Configuration

# GCP subnetworks map to Docker network IPAM configurations
# Each subnetwork becomes a subnet configuration in Docker network

# Example subnetwork mapping:
subnetworks:
  - name: default-us-central1
    cidr: 10.128.0.0/20
    region: us-central1
    docker_network: vpc-main
    type: public
    private_ip_google_access: true

  - name: default-us-east1
    cidr: 10.142.0.0/20
    region: us-east1
    docker_network: vpc-main
    type: public

  - name: private-subnet
    cidr: 10.10.0.0/20
    region: us-central1
    docker_network: vpc-main-private
    type: private

# Implementation Notes:
# 1. Public subnetworks -> Docker bridge network with internet access
# 2. Private subnetworks -> Docker bridge network with 'internal: true'
# 3. Regions -> Different Docker hosts (for HA) or same host
# 4. Secondary IP ranges -> Additional IPAM configurations
# 5. Flow logs -> Network traffic monitoring tools
`

	return config
}

// generatePeeringConfig creates VPC peering configuration guide.
func (m *VPCMapper) generatePeeringConfig() string {
	config := `# GCP VPC Peering to Docker Network Connection

## Overview
VPC peering allows VPC networks to communicate. In Docker, this is achieved by:
1. Connecting containers to multiple networks
2. Using custom Docker networks with routing
3. Using overlay networks for multi-host scenarios

## Single Host Setup

Create multiple networks and connect services to both networks.
Services connected to multiple networks can route traffic between them.

## Multi-Host Setup (Docker Swarm)

Use overlay networks with attachable flag for cross-host communication.

## Peering Behavior

GCP VPC peering exports routes between networks. In Docker:
- All containers on the same network can communicate by default
- Cross-network communication requires connecting to multiple networks
- Use network aliases for service discovery across networks

## Example Configuration

services:
  app:
    networks:
      - network-a
      - network-b  # Connected to both networks for peering

networks:
  network-a:
    driver: bridge
  network-b:
    driver: bridge
`

	return config
}

// generateFirewallMapping creates firewall rules mapping guide.
func (m *VPCMapper) generateFirewallMapping() string {
	config := `# GCP Firewall Rules to Docker Network Policy Mapping

## Overview
GCP Firewall rules control traffic to/from VMs. Docker provides basic network
isolation but requires additional tools for fine-grained control.

## Implementation Options

### 1. Host-Level Firewall (iptables)
Control traffic using iptables rules on the Docker host.

### 2. Docker Network Policies
Use labels and scripts to enforce policies.

### 3. Service Mesh
Use Istio, Linkerd, or Consul for advanced policies:
- mTLS between services
- Fine-grained access control
- Traffic routing and shaping

### 4. Kubernetes Network Policies
If using Kubernetes, use NetworkPolicy resources.

## Common Firewall Patterns

### Allow HTTP/HTTPS (Ingress)
iptables -A DOCKER-USER -p tcp --dport 80 -j ACCEPT
iptables -A DOCKER-USER -p tcp --dport 443 -j ACCEPT

### Allow Internal Traffic Only
# Use Docker internal networks with 'internal: true'

### Allow from Specific Source
iptables -A DOCKER-USER -s 10.0.0.0/8 -j ACCEPT

## Priority and Direction

| GCP Direction | iptables Chain |
|---------------|----------------|
| INGRESS | DOCKER-USER (INPUT) |
| EGRESS | DOCKER-USER (OUTPUT) |

## Best Practices

1. Default deny all, then allow specific traffic
2. Use internal networks for databases
3. Implement least privilege access
4. Monitor network traffic
5. Consider service mesh for complex scenarios
`

	return config
}

// generateRouterConfig creates Cloud Router configuration guide.
func (m *VPCMapper) generateRouterConfig() string {
	config := `# GCP Cloud Router Replacement

## Overview
Cloud Router provides dynamic routing via BGP. For self-hosted environments,
you can use static routing or set up BGP on your host.

## Static Routing

For simple topologies, use static routes:
ip route add 10.0.0.0/8 via 192.168.1.1

## Dynamic Routing with BGP

For dynamic routing, use a BGP daemon:

### FRRouting (Recommended)
Docker-compatible routing stack supporting BGP, OSPF, and more.

### BIRD
Lightweight routing daemon with BGP support.

## Docker Network Routing

Docker manages container routes automatically. For cross-network routing:
1. Enable IP forwarding: sysctl -w net.ipv4.ip_forward=1
2. Configure iptables for cross-network routing
3. Or use overlay networks for multi-host scenarios

## Route Advertisement

In GCP, Cloud Router advertises routes to peered networks.
In Docker, configure routes on each host or use a centralized router.
`

	return config
}

// generateVPNConfig creates VPN setup guide.
func (m *VPCMapper) generateVPNConfig() string {
	config := `# GCP VPN Gateway Replacement

## Overview
Replace GCP VPN Gateway with self-hosted VPN server.

## Recommended: WireGuard

WireGuard is a modern, fast VPN protocol.

Use linuxserver/wireguard Docker image:
- Modern and performant
- Easy to configure
- Built-in peer management

## Alternative: OpenVPN

OpenVPN is a mature VPN solution.

Use kylemanna/openvpn Docker image:
- Well-established
- Wide client support
- More complex configuration

## Site-to-Site VPN

For connecting to on-premises networks:

1. Configure WireGuard or OpenVPN in site-to-site mode
2. Set up routing between Docker networks and VPN tunnel
3. Configure firewall rules for VPN traffic
4. Test connectivity and failover

## Setup Steps

1. Initialize VPN configuration
2. Generate peer/client configs
3. Configure routing to Docker networks
4. Set up firewall rules
5. Test connectivity

## Cloud VPN Gateway Equivalent

GCP Cloud VPN supports HA VPN with multiple tunnels.
For equivalent HA:
- Run multiple VPN servers
- Use keepalived for failover
- Configure routing with health checks
`

	return config
}

// generateDNSConfig creates internal DNS configuration.
func (m *VPCMapper) generateDNSConfig() string {
	config := `# Internal DNS Configuration

# Use CoreDNS for container name resolution
# Add this service to your docker-compose.yml

services:
  coredns:
    image: coredns/coredns:1.11
    volumes:
      - ./Corefile:/etc/coredns/Corefile
    networks:
      - internal
    ports:
      - "53:53/udp"
      - "53:53/tcp"

# Corefile example:
# .:53 {
#     forward . 8.8.8.8 8.8.4.4
#     log
#     errors
# }
#
# internal.local:53 {
#     hosts {
#         172.20.0.10 service-a.internal.local
#         172.20.0.11 service-b.internal.local
#         fallthrough
#     }
# }

# Configure Docker daemon to use internal DNS:
# /etc/docker/daemon.json
# {
#   "dns": ["172.20.0.2"],
#   "dns-search": ["internal.local"]
# }
`

	return config
}

// sanitizeName sanitizes the name for Docker.
func (m *VPCMapper) sanitizeName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "_", "-")
	name = strings.ReplaceAll(name, " ", "-")

	validName := ""
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' {
			validName += string(ch)
		}
	}

	validName = strings.TrimLeft(validName, "-0123456789")
	if validName == "" {
		validName = "vpc-network"
	}

	return validName
}

// hasSubnetworks checks if the VPC has subnetwork configurations.
func (m *VPCMapper) hasSubnetworks(res *resource.AWSResource) bool {
	if subnets := res.Config["subnetwork"]; subnets != nil {
		return true
	}
	if subnets := res.Config["subnetworks"]; subnets != nil {
		return true
	}
	return false
}

// hasPeeringConnections checks if the VPC has peering connections.
func (m *VPCMapper) hasPeeringConnections(res *resource.AWSResource) bool {
	if peering := res.Config["peering"]; peering != nil {
		return true
	}
	if peering := res.Config["network_peering"]; peering != nil {
		return true
	}
	return false
}

// isSharedVPC checks if the VPC is a shared VPC.
func (m *VPCMapper) isSharedVPC(res *resource.AWSResource) bool {
	if shared := res.GetConfigBool("shared_vpc_host"); shared {
		return true
	}
	if shared := res.Config["shared_vpc"]; shared != nil {
		return true
	}
	return false
}

// hasFirewallRules checks if the VPC has associated firewall rules.
func (m *VPCMapper) hasFirewallRules(res *resource.AWSResource) bool {
	if fw := res.Config["firewall"]; fw != nil {
		return true
	}
	if fw := res.Config["firewall_rules"]; fw != nil {
		return true
	}
	return false
}

// hasCloudRouter checks if the VPC has a Cloud Router.
func (m *VPCMapper) hasCloudRouter(res *resource.AWSResource) bool {
	if router := res.Config["router"]; router != nil {
		return true
	}
	if router := res.Config["cloud_router"]; router != nil {
		return true
	}
	return false
}

// hasCloudNAT checks if the VPC has Cloud NAT configured.
func (m *VPCMapper) hasCloudNAT(res *resource.AWSResource) bool {
	if nat := res.Config["nat"]; nat != nil {
		return true
	}
	if nat := res.Config["cloud_nat"]; nat != nil {
		return true
	}
	return false
}

// hasVPNGateway checks if the VPC has a VPN gateway.
func (m *VPCMapper) hasVPNGateway(res *resource.AWSResource) bool {
	if vpn := res.Config["vpn_gateway"]; vpn != nil {
		return true
	}
	if vpn := res.Config["ha_vpn_gateway"]; vpn != nil {
		return true
	}
	return false
}

// hasCloudInterconnect checks if the VPC has Cloud Interconnect.
func (m *VPCMapper) hasCloudInterconnect(res *resource.AWSResource) bool {
	if interconnect := res.Config["interconnect"]; interconnect != nil {
		return true
	}
	if interconnect := res.Config["interconnect_attachment"]; interconnect != nil {
		return true
	}
	return false
}

// hasPrivateGoogleAccess checks if Private Google Access is enabled.
func (m *VPCMapper) hasPrivateGoogleAccess(res *resource.AWSResource) bool {
	if pga := res.GetConfigBool("private_ip_google_access"); pga {
		return true
	}
	return false
}

// hasServiceControls checks if VPC Service Controls are configured.
func (m *VPCMapper) hasServiceControls(res *resource.AWSResource) bool {
	if sc := res.Config["service_controls"]; sc != nil {
		return true
	}
	if sc := res.Config["access_policy"]; sc != nil {
		return true
	}
	return false
}

// hasInternalDNS checks if internal DNS is configured.
func (m *VPCMapper) hasInternalDNS(res *resource.AWSResource) bool {
	if dns := res.Config["dns_policy"]; dns != nil {
		return true
	}
	if dns := res.Config["private_dns"]; dns != nil {
		return true
	}
	return false
}
