package networking

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestNewVPCMapper(t *testing.T) {
	m := NewVPCMapper()
	if m == nil {
		t.Fatal("NewVPCMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeGCPVPCNetwork {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeGCPVPCNetwork)
	}
}

func TestVPCMapper_ResourceType(t *testing.T) {
	m := NewVPCMapper()
	got := m.ResourceType()
	want := resource.TypeGCPVPCNetwork

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestVPCMapper_Dependencies(t *testing.T) {
	m := NewVPCMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestVPCMapper_Validate(t *testing.T) {
	m := NewVPCMapper()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
	}{
		{
			name:    "nil resource",
			res:     nil,
			wantErr: true,
		},
		{
			name: "wrong resource type",
			res: &resource.AWSResource{
				ID:   "test-id",
				Type: resource.TypeGCSBucket,
				Name: "test",
			},
			wantErr: true,
		},
		{
			name: "valid resource",
			res: &resource.AWSResource{
				ID:   "test-id",
				Type: resource.TypeGCPVPCNetwork,
				Name: "test-vpc",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := m.Validate(tt.res)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestVPCMapper_Map(t *testing.T) {
	m := NewVPCMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic VPC network",
			res: &resource.AWSResource{
				ID:   "my-project/my-vpc",
				Type: resource.TypeGCPVPCNetwork,
				Name: "my-vpc",
				Config: map[string]interface{}{
					"name": "my-vpc",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if result.DockerService == nil {
					t.Fatal("DockerService is nil")
				}
				if result.DockerService.Image != "alpine:latest" {
					t.Errorf("Expected alpine image, got %s", result.DockerService.Image)
				}
			},
		},
		{
			name: "VPC with auto_create_subnetworks",
			res: &resource.AWSResource{
				ID:   "my-project/auto-vpc",
				Type: resource.TypeGCPVPCNetwork,
				Name: "auto-vpc",
				Config: map[string]interface{}{
					"name":                    "auto-vpc",
					"auto_create_subnetworks": true,
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about auto-create subnetworks
				hasWarning := false
				for _, w := range result.Warnings {
					if len(w) > 0 {
						hasWarning = true
						break
					}
				}
				if !hasWarning {
					t.Error("Expected warnings about auto-create subnetworks")
				}
			},
		},
		{
			name: "VPC with global routing mode",
			res: &resource.AWSResource{
				ID:   "my-project/global-vpc",
				Type: resource.TypeGCPVPCNetwork,
				Name: "global-vpc",
				Config: map[string]interface{}{
					"name":         "global-vpc",
					"routing_mode": "GLOBAL",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if result.DockerService.Labels == nil {
					t.Error("Labels should not be nil")
					return
				}
				if result.DockerService.Labels["homeport.routing_mode"] != "GLOBAL" {
					t.Errorf("Expected routing mode GLOBAL, got %s", result.DockerService.Labels["homeport.routing_mode"])
				}
			},
		},
		{
			name: "VPC with subnetworks",
			res: &resource.AWSResource{
				ID:   "my-project/subnet-vpc",
				Type: resource.TypeGCPVPCNetwork,
				Name: "subnet-vpc",
				Config: map[string]interface{}{
					"name": "subnet-vpc",
					"subnetwork": []interface{}{
						map[string]interface{}{
							"name":        "subnet-1",
							"ip_cidr_range": "10.0.0.0/24",
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have subnetwork config
				hasSubnetConfig := false
				for name := range result.Configs {
					if name == "config/vpc/subnetworks.yml" {
						hasSubnetConfig = true
						break
					}
				}
				if !hasSubnetConfig {
					t.Error("Expected subnetworks.yml config")
				}
			},
		},
		{
			name: "VPC with peering",
			res: &resource.AWSResource{
				ID:   "my-project/peered-vpc",
				Type: resource.TypeGCPVPCNetwork,
				Name: "peered-vpc",
				Config: map[string]interface{}{
					"name": "peered-vpc",
					"peering": []interface{}{
						map[string]interface{}{
							"name":    "peering-1",
							"network": "projects/other-project/global/networks/other-vpc",
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have peering config
				hasPeeringConfig := false
				for name := range result.Configs {
					if name == "config/vpc/peering-guide.md" {
						hasPeeringConfig = true
						break
					}
				}
				if !hasPeeringConfig {
					t.Error("Expected peering-guide.md config")
				}
			},
		},
		{
			name: "VPC with firewall rules",
			res: &resource.AWSResource{
				ID:   "my-project/fw-vpc",
				Type: resource.TypeGCPVPCNetwork,
				Name: "fw-vpc",
				Config: map[string]interface{}{
					"name": "fw-vpc",
					"firewall": []interface{}{
						map[string]interface{}{
							"name":    "allow-http",
							"allowed": "tcp:80",
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have firewall config
				hasFirewallConfig := false
				for name := range result.Configs {
					if name == "config/vpc/firewall-rules.md" {
						hasFirewallConfig = true
						break
					}
				}
				if !hasFirewallConfig {
					t.Error("Expected firewall-rules.md config")
				}
			},
		},
		{
			name: "VPC with VPN gateway",
			res: &resource.AWSResource{
				ID:   "my-project/vpn-vpc",
				Type: resource.TypeGCPVPCNetwork,
				Name: "vpn-vpc",
				Config: map[string]interface{}{
					"name":        "vpn-vpc",
					"vpn_gateway": "my-vpn-gateway",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have VPN config
				hasVPNConfig := false
				for name := range result.Configs {
					if name == "config/vpc/vpn-setup.md" {
						hasVPNConfig = true
						break
					}
				}
				if !hasVPNConfig {
					t.Error("Expected vpn-setup.md config")
				}
			},
		},
		{
			name: "VPC with custom MTU",
			res: &resource.AWSResource{
				ID:   "my-project/mtu-vpc",
				Type: resource.TypeGCPVPCNetwork,
				Name: "mtu-vpc",
				Config: map[string]interface{}{
					"name": "mtu-vpc",
					"mtu":  1500,
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if result.DockerService.Labels == nil {
					t.Error("Labels should not be nil")
					return
				}
				if result.DockerService.Labels["homeport.mtu"] != "1500" {
					t.Errorf("Expected MTU label 1500, got %s", result.DockerService.Labels["homeport.mtu"])
				}
			},
		},
		{
			name:    "nil resource",
			res:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := m.Map(ctx, tt.res)
			if (err != nil) != tt.wantErr {
				t.Errorf("Map() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}

func TestVPCMapper_extractRoutingMode(t *testing.T) {
	m := NewVPCMapper()

	tests := []struct {
		name   string
		res    *resource.AWSResource
		expect string
	}{
		{
			name: "with GLOBAL routing_mode",
			res: &resource.AWSResource{
				Config: map[string]interface{}{
					"routing_mode": "GLOBAL",
				},
			},
			expect: "GLOBAL",
		},
		{
			name: "with REGIONAL routing_mode",
			res: &resource.AWSResource{
				Config: map[string]interface{}{
					"routing_mode": "REGIONAL",
				},
			},
			expect: "REGIONAL",
		},
		{
			name: "without routing_mode",
			res: &resource.AWSResource{
				Config: map[string]interface{}{},
			},
			expect: "REGIONAL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.extractRoutingMode(tt.res)
			if got != tt.expect {
				t.Errorf("extractRoutingMode() = %v, want %v", got, tt.expect)
			}
		})
	}
}

func TestVPCMapper_sanitizeName(t *testing.T) {
	m := NewVPCMapper()

	tests := []struct {
		input string
		want  string
	}{
		{"my-vpc", "my-vpc"},
		{"MY_VPC", "my-vpc"},
		{"my vpc", "my-vpc"},
		{"123vpc", "vpc"},
		{"", "vpc-network"},
		{"my-project-vpc", "my-project-vpc"},
		{"vpc_with_underscores", "vpc-with-underscores"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := m.sanitizeName(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestVPCMapper_hasSubnetworks(t *testing.T) {
	m := NewVPCMapper()

	tests := []struct {
		name   string
		res    *resource.AWSResource
		expect bool
	}{
		{
			name: "with subnetwork",
			res: &resource.AWSResource{
				Config: map[string]interface{}{
					"subnetwork": []interface{}{},
				},
			},
			expect: true,
		},
		{
			name: "with subnetworks",
			res: &resource.AWSResource{
				Config: map[string]interface{}{
					"subnetworks": []interface{}{},
				},
			},
			expect: true,
		},
		{
			name: "without subnetworks",
			res: &resource.AWSResource{
				Config: map[string]interface{}{},
			},
			expect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.hasSubnetworks(tt.res)
			if got != tt.expect {
				t.Errorf("hasSubnetworks() = %v, want %v", got, tt.expect)
			}
		})
	}
}

func TestVPCMapper_hasPeeringConnections(t *testing.T) {
	m := NewVPCMapper()

	tests := []struct {
		name   string
		res    *resource.AWSResource
		expect bool
	}{
		{
			name: "with peering",
			res: &resource.AWSResource{
				Config: map[string]interface{}{
					"peering": []interface{}{},
				},
			},
			expect: true,
		},
		{
			name: "with network_peering",
			res: &resource.AWSResource{
				Config: map[string]interface{}{
					"network_peering": []interface{}{},
				},
			},
			expect: true,
		},
		{
			name: "without peering",
			res: &resource.AWSResource{
				Config: map[string]interface{}{},
			},
			expect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.hasPeeringConnections(tt.res)
			if got != tt.expect {
				t.Errorf("hasPeeringConnections() = %v, want %v", got, tt.expect)
			}
		})
	}
}

func TestVPCMapper_hasFirewallRules(t *testing.T) {
	m := NewVPCMapper()

	tests := []struct {
		name   string
		res    *resource.AWSResource
		expect bool
	}{
		{
			name: "with firewall",
			res: &resource.AWSResource{
				Config: map[string]interface{}{
					"firewall": []interface{}{},
				},
			},
			expect: true,
		},
		{
			name: "with firewall_rules",
			res: &resource.AWSResource{
				Config: map[string]interface{}{
					"firewall_rules": []interface{}{},
				},
			},
			expect: true,
		},
		{
			name: "without firewall",
			res: &resource.AWSResource{
				Config: map[string]interface{}{},
			},
			expect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.hasFirewallRules(tt.res)
			if got != tt.expect {
				t.Errorf("hasFirewallRules() = %v, want %v", got, tt.expect)
			}
		})
	}
}

func TestVPCMapper_hasVPNGateway(t *testing.T) {
	m := NewVPCMapper()

	tests := []struct {
		name   string
		res    *resource.AWSResource
		expect bool
	}{
		{
			name: "with vpn_gateway",
			res: &resource.AWSResource{
				Config: map[string]interface{}{
					"vpn_gateway": "my-gateway",
				},
			},
			expect: true,
		},
		{
			name: "with ha_vpn_gateway",
			res: &resource.AWSResource{
				Config: map[string]interface{}{
					"ha_vpn_gateway": "my-ha-gateway",
				},
			},
			expect: true,
		},
		{
			name: "without vpn",
			res: &resource.AWSResource{
				Config: map[string]interface{}{},
			},
			expect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.hasVPNGateway(tt.res)
			if got != tt.expect {
				t.Errorf("hasVPNGateway() = %v, want %v", got, tt.expect)
			}
		})
	}
}
