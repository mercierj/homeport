// Package networking provides mappers for AWS networking services.
package networking

import (
	"context"
	"fmt"
	"strings"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

// VPCMapper converts AWS VPC to Docker networks.
type VPCMapper struct {
	*mapper.BaseMapper
}

// NewVPCMapper creates a new VPC to Docker networks mapper.
func NewVPCMapper() *VPCMapper {
	return &VPCMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeVPC, nil),
	}
}

// Map converts an AWS VPC to Docker network configurations.
func (m *VPCMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	vpcID := res.ID
	vpcName := res.Name
	if vpcName == "" {
		vpcName = vpcID
	}

	cidrBlock := res.GetConfigString("cidr_block")
	if cidrBlock == "" {
		cidrBlock = "172.18.0.0/16" // Default Docker network CIDR
	}

	// Create result - no specific service, just network configuration
	result := mapper.NewMappingResult(fmt.Sprintf("vpc-%s", m.sanitizeName(vpcName)))
	svc := result.DockerService

	// VPC doesn't map to a Docker service, but we create a placeholder
	// The actual network is created via Docker Compose networks section
	svc.Image = "alpine:latest"
	svc.Command = []string{"sh", "-c", "echo 'VPC placeholder - networks defined in docker-compose.yml' && sleep infinity"}
	svc.Networks = []string{m.sanitizeName(vpcName)}
	svc.Labels = map[string]string{
		"cloudexit.source":     "aws_vpc",
		"cloudexit.vpc_id":     vpcID,
		"cloudexit.vpc_name":   vpcName,
		"cloudexit.cidr_block": cidrBlock,
	}
	svc.Restart = "no"

	// Generate Docker network configuration
	networkConfig := m.generateDockerNetworkConfig(res, vpcName, cidrBlock)
	result.AddConfig("docker-compose-networks.yml", []byte(networkConfig))

	// Generate network documentation
	networkDoc := m.generateNetworkDocumentation(res, vpcName, cidrBlock)
	result.AddConfig("config/vpc/network-mapping.md", []byte(networkDoc))

	// Generate subnet configuration
	if m.hasSubnets(res) {
		subnetConfig := m.generateSubnetConfig(res)
		result.AddConfig("config/vpc/subnets.yml", []byte(subnetConfig))
	}

	// Handle VPC peering
	if m.hasVPCPeering(res) {
		result.AddWarning("VPC peering connections detected. Docker networks can be connected using network attachments.")
		result.AddManualStep("Configure Docker network connections for cross-network communication")

		peeringConfig := m.generatePeeringConfig()
		result.AddConfig("config/vpc/peering-guide.md", []byte(peeringConfig))
	}

	// Handle NAT Gateways
	if m.hasNATGateway(res) {
		result.AddWarning("NAT Gateway detected. Docker containers can access external networks directly. Configure iptables/firewall rules if specific NAT behavior is required.")
		result.AddManualStep("Review NAT Gateway configuration and implement equivalent routing if needed")
	}

	// Handle Internet Gateway
	if m.hasInternetGateway(res) {
		result.AddWarning("Internet Gateway detected. Docker containers have internet access by default.")
		result.AddManualStep("Configure Docker network driver to control internet access if needed")
	}

	// Handle Security Groups
	if m.hasSecurityGroups(res) {
		result.AddWarning("Security Groups detected. These need to be implemented using Docker network policies, iptables, or service-level firewalls.")
		result.AddManualStep("Review Security Group rules and implement equivalent network policies")

		securityGroupConfig := m.generateSecurityGroupMapping()
		result.AddConfig("config/vpc/security-groups.md", []byte(securityGroupConfig))
	}

	// Handle Network ACLs
	if m.hasNetworkACLs(res) {
		result.AddWarning("Network ACLs detected. Implement using iptables, nftables, or Docker network policies.")
		result.AddManualStep("Review Network ACL rules and configure host firewall accordingly")

		aclConfig := m.generateACLMapping()
		result.AddConfig("config/vpc/network-acls.md", []byte(aclConfig))
	}

	// Handle VPN Gateway
	if m.hasVPNGateway(res) {
		result.AddWarning("VPN Gateway detected. Set up VPN server (WireGuard, OpenVPN) for secure remote access.")
		result.AddManualStep("Configure VPN server for remote access to Docker networks")

		vpnConfig := m.generateVPNConfig()
		result.AddConfig("config/vpc/vpn-setup.md", []byte(vpnConfig))
	}

	// Handle VPC Endpoints
	if m.hasVPCEndpoints(res) {
		result.AddWarning("VPC Endpoints detected. Service endpoints need to be configured in service discovery or DNS.")
		result.AddManualStep("Configure service discovery for private service endpoints")
	}

	// Handle DNS settings
	if enableDNSHostnames := res.GetConfigBool("enable_dns_hostnames"); enableDNSHostnames {
		result.AddWarning("VPC DNS hostnames enabled. Configure Docker DNS for service discovery.")
		result.AddManualStep("Set up DNS resolver (CoreDNS, dnsmasq) for container name resolution")
	}

	// Handle DHCP options
	if m.hasDHCPOptions(res) {
		result.AddWarning("Custom DHCP options detected. Configure Docker daemon with custom DNS servers if needed.")
		result.AddManualStep("Update Docker daemon.json with custom DNS configuration")

		dhcpConfig := m.generateDHCPConfig()
		result.AddConfig("config/vpc/dhcp-options.json", []byte(dhcpConfig))
	}

	// Handle IPv6
	if m.hasIPv6(res) {
		result.AddWarning("IPv6 CIDR block detected. Enable IPv6 in Docker daemon and network configuration.")
		result.AddManualStep("Enable IPv6 in Docker daemon.json and create dual-stack networks")

		ipv6Config := m.generateIPv6Config()
		result.AddConfig("config/vpc/ipv6-config.md", []byte(ipv6Config))
	}

	// Handle flow logs
	if m.hasFlowLogs(res) {
		result.AddWarning("VPC Flow Logs detected. Implement network traffic monitoring using tools like tcpdump, Wireshark, or network monitoring solutions.")
		result.AddManualStep("Set up network traffic logging and monitoring")
	}

	result.AddManualStep("Create Docker networks defined in docker-compose-networks.yml")
	result.AddManualStep("Configure host firewall rules for network isolation")
	result.AddManualStep("Test network connectivity between services")
	result.AddManualStep("Set up network monitoring and observability")

	// Add comprehensive warning about VPC limitations
	result.AddWarning("IMPORTANT: AWS VPC provides advanced networking features that cannot be fully replicated with Docker networks alone. Consider using Kubernetes with network policies, or running a full virtual network solution if complex networking is required.")

	return result, nil
}

// generateDockerNetworkConfig creates Docker network configuration.
func (m *VPCMapper) generateDockerNetworkConfig(res *resource.AWSResource, vpcName, cidrBlock string) string {
	networkName := m.sanitizeName(vpcName)

	config := `# Docker Compose Network Configuration
# Generated from AWS VPC
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
      com.docker.network.driver.mtu: "1500"
    labels:
      cloudexit.source: "aws_vpc"
      cloudexit.vpc_name: "%s"
      cloudexit.cidr_block: "%s"

# Additional isolated network for private subnets
  %s-private:
    driver: bridge
    internal: true
    ipam:
      config:
        - subnet: 172.19.0.0/16
    labels:
      cloudexit.source: "aws_vpc_private"
      cloudexit.vpc_name: "%s"
`

	return fmt.Sprintf(config, networkName, cidrBlock, networkName, vpcName, cidrBlock, networkName, vpcName)
}

// generateNetworkDocumentation creates network mapping documentation.
func (m *VPCMapper) generateNetworkDocumentation(res *resource.AWSResource, vpcName, cidrBlock string) string {
	networkName := m.sanitizeName(vpcName)

	doc := fmt.Sprintf(`# VPC to Docker Network Mapping

## Overview
AWS VPC: %s
CIDR Block: %s

## Docker Network Architecture

### Network Types
1. Bridge Networks - Default Docker networks with NAT
2. Overlay Networks - For multi-host networking (requires Docker Swarm)
3. Macvlan Networks - For containers to appear on physical network
4. Host Networks - Container uses host network stack

## VPC Component Mapping

| AWS Component | Docker Equivalent | Notes |
|---------------|------------------|-------|
| VPC | Docker Network | Isolated network namespace |
| Subnet | IPAM Config | Subnet within Docker network |
| Internet Gateway | Default NAT | Docker provides outbound internet by default |
| NAT Gateway | iptables/masquerade | Docker handles outbound NAT automatically |
| Security Group | Network Policies | Implement with iptables or service mesh |
| Network ACL | Host Firewall | Use iptables/nftables on host |
| Route Table | Docker Routes | Managed automatically by Docker |
| VPC Peering | Network Connect | Connect Docker networks |
| VPN Gateway | VPN Server | WireGuard/OpenVPN for remote access |
| VPC Endpoint | Service Discovery | DNS or service mesh |

## Network Isolation

### Public Services
Services that need internet access should be connected to network: %s

### Private Services
Services without internet access should use network: %s-private

## Inter-Network Communication

To allow communication between networks, connect services to multiple networks.

## Migration Notes

1. VPC CIDR blocks map to Docker network subnets
2. Subnets become IPAM configurations
3. Routing is handled automatically by Docker
4. Security rules need manual implementation
5. NAT/IGW functionality is built into Docker bridge driver

For detailed examples, see Docker Compose documentation.
`, vpcName, cidrBlock, networkName, networkName)

	return doc
}

// generateSubnetConfig creates subnet configuration.
func (m *VPCMapper) generateSubnetConfig(res *resource.AWSResource) string {
	config := `# Subnet Configuration

# AWS VPC subnets map to Docker network IPAM configurations
# Each subnet in AWS becomes a subnet configuration in Docker network

# Example subnet mapping:
subnets:
  - name: public-subnet-1
    cidr: 172.18.1.0/24
    availability_zone: us-east-1a
    docker_network: vpc-main
    type: public

  - name: public-subnet-2
    cidr: 172.18.2.0/24
    availability_zone: us-east-1b
    docker_network: vpc-main
    type: public

  - name: private-subnet-1
    cidr: 172.18.10.0/24
    availability_zone: us-east-1a
    docker_network: vpc-main-private
    type: private

  - name: private-subnet-2
    cidr: 172.18.11.0/24
    availability_zone: us-east-1b
    docker_network: vpc-main-private
    type: private

# Implementation Notes:
# 1. Public subnets -> Docker bridge network with internet access
# 2. Private subnets -> Docker bridge network with 'internal: true'
# 3. Availability zones -> Different Docker hosts (for HA) or same host
# 4. Subnet tags -> Docker network/container labels
`

	return config
}

// generatePeeringConfig creates VPC peering configuration guide.
func (m *VPCMapper) generatePeeringConfig() string {
	config := `# VPC Peering to Docker Network Connection

## Overview
VPC peering allows VPCs to communicate. In Docker, this is achieved by:
1. Connecting containers to multiple networks
2. Using custom Docker networks with routing
3. Using overlay networks for multi-host scenarios

## Single Host Setup

Create multiple networks and connect services to both networks.
Services connected to multiple networks can route traffic between them.

## Multi-Host Setup (Docker Swarm)

Use overlay networks with attachable flag for cross-host communication.

## Routing Between Networks

Enable IP forwarding on host:
  sysctl -w net.ipv4.ip_forward=1

Configure iptables for cross-network routing between bridge interfaces.
`

	return config
}

// generateSecurityGroupMapping creates security group mapping guide.
func (m *VPCMapper) generateSecurityGroupMapping() string {
	config := `# Security Group to Docker Network Policy Mapping

## Overview
AWS Security Groups are stateful firewalls. Docker provides basic network
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

## Common Security Group Patterns

### Web Tier (Public)
- Ingress: 80, 443 from anywhere
- Egress: All traffic

### Application Tier (Private)
- Ingress: 8080 from web tier only
- Egress: Database tier only

### Database Tier (Private)
- Ingress: 5432 from app tier only
- Egress: None (use internal network)

## Best Practices

1. Use internal networks for databases
2. Implement least privilege access
3. Monitor network traffic
4. Use service discovery for dynamic IPs
5. Consider service mesh for complex scenarios
`

	return config
}

// generateACLMapping creates network ACL mapping guide.
func (m *VPCMapper) generateACLMapping() string {
	config := `# Network ACL to Firewall Rules Mapping

## Overview
Network ACLs are stateless firewalls at the subnet level.
In Docker, implement using host-level firewall rules.

## Implementation

Use iptables or nftables on the Docker host to create ACL-like rules.

Create chains for network ACL:
- Default deny policy
- Allow specific ports inbound
- Allow ephemeral ports outbound
- Apply to Docker bridge interfaces

## ACL vs Security Group

| Feature | Network ACL | Security Group |
|---------|-------------|----------------|
| Level | Subnet | Instance |
| State | Stateless | Stateful |
| Rules | Allow/Deny | Allow only |
| Order | Priority | All evaluated |
| Docker | Host firewall | Container isolation |
`

	return config
}

// generateVPNConfig creates VPN setup guide.
func (m *VPCMapper) generateVPNConfig() string {
	config := `# VPN Gateway Replacement

## Overview
Replace AWS VPN Gateway with self-hosted VPN server.

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

## Setup Steps

1. Initialize VPN configuration
2. Generate client configs
3. Configure routing to Docker networks
4. Set up firewall rules
5. Test connectivity

## Client Access

Clients connect via VPN and can access Docker services through the VPN server,
which routes traffic to Docker networks.
`

	return config
}

// generateDHCPConfig creates DHCP options configuration.
func (m *VPCMapper) generateDHCPConfig() string {
	config := `{
  "dns": ["8.8.8.8", "8.8.4.4"],
  "dns-search": ["example.com"],
  "mtu": 1500,
  "log-driver": "json-file",
  "log-opts": {
    "max-size": "10m",
    "max-file": "3"
  }
}
`

	return config
}

// generateIPv6Config creates IPv6 configuration guide.
func (m *VPCMapper) generateIPv6Config() string {
	config := `# IPv6 Configuration for Docker

## Enable IPv6 in Docker Daemon

Edit /etc/docker/daemon.json:
{
  "ipv6": true,
  "fixed-cidr-v6": "2001:db8:1::/64"
}

Restart Docker after configuration changes.

## Docker Compose Configuration

Configure networks with enable_ipv6: true and specify IPv6 subnet.

## Testing

Test IPv6 connectivity using Docker containers with ping6 and ip commands.
`

	return config
}

// sanitizeName sanitizes names for Docker network names.
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

	return validName
}

// hasSubnets checks if the VPC has subnet configurations.
func (m *VPCMapper) hasSubnets(res *resource.AWSResource) bool {
	if subnets := res.Config["subnet"]; subnets != nil {
		return true
	}
	return false
}

// hasVPCPeering checks if the VPC has peering connections.
func (m *VPCMapper) hasVPCPeering(res *resource.AWSResource) bool {
	if peering := res.Config["vpc_peering_connection"]; peering != nil {
		return true
	}
	return false
}

// hasNATGateway checks if the VPC has NAT gateways.
func (m *VPCMapper) hasNATGateway(res *resource.AWSResource) bool {
	if nat := res.Config["nat_gateway"]; nat != nil {
		return true
	}
	return false
}

// hasInternetGateway checks if the VPC has an internet gateway.
func (m *VPCMapper) hasInternetGateway(res *resource.AWSResource) bool {
	if igw := res.Config["internet_gateway"]; igw != nil {
		return true
	}
	return false
}

// hasSecurityGroups checks if the VPC references security groups.
func (m *VPCMapper) hasSecurityGroups(res *resource.AWSResource) bool {
	if sg := res.Config["security_group"]; sg != nil {
		return true
	}
	return false
}

// hasNetworkACLs checks if the VPC has network ACLs.
func (m *VPCMapper) hasNetworkACLs(res *resource.AWSResource) bool {
	if acl := res.Config["network_acl"]; acl != nil {
		return true
	}
	return false
}

// hasVPNGateway checks if the VPC has a VPN gateway.
func (m *VPCMapper) hasVPNGateway(res *resource.AWSResource) bool {
	if vpn := res.Config["vpn_gateway"]; vpn != nil {
		return true
	}
	return false
}

// hasVPCEndpoints checks if the VPC has VPC endpoints.
func (m *VPCMapper) hasVPCEndpoints(res *resource.AWSResource) bool {
	if endpoints := res.Config["vpc_endpoint"]; endpoints != nil {
		return true
	}
	return false
}

// hasDHCPOptions checks if the VPC has custom DHCP options.
func (m *VPCMapper) hasDHCPOptions(res *resource.AWSResource) bool {
	if dhcp := res.Config["dhcp_options"]; dhcp != nil {
		return true
	}
	return false
}

// hasIPv6 checks if the VPC has IPv6 configured.
func (m *VPCMapper) hasIPv6(res *resource.AWSResource) bool {
	if ipv6CIDR := res.GetConfigString("ipv6_cidr_block"); ipv6CIDR != "" {
		return true
	}
	if ipv6 := res.Config["ipv6_cidr_block_network_border_group"]; ipv6 != nil {
		return true
	}
	return false
}

// hasFlowLogs checks if the VPC has flow logs enabled.
func (m *VPCMapper) hasFlowLogs(res *resource.AWSResource) bool {
	if flowLogs := res.Config["flow_log"]; flowLogs != nil {
		return true
	}
	return false
}
