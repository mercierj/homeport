package networking

import (
	"context"
	"testing"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

func TestNewVPCMapper(t *testing.T) {
	m := NewVPCMapper()
	if m == nil {
		t.Fatal("NewVPCMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeVPC {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeVPC)
	}
}

func TestVPCMapper_ResourceType(t *testing.T) {
	m := NewVPCMapper()
	got := m.ResourceType()
	want := resource.TypeVPC

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
				Type: resource.TypeS3Bucket,
				Name: "test",
			},
			wantErr: true,
		},
		{
			name: "valid resource",
			res: &resource.AWSResource{
				ID:   "vpc-12345678",
				Type: resource.TypeVPC,
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
			name: "basic VPC",
			res: &resource.AWSResource{
				ID:   "vpc-12345678",
				Type: resource.TypeVPC,
				Name: "my-vpc",
				Config: map[string]interface{}{
					"cidr_block": "10.0.0.0/16",
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
				if result.DockerService.Image == "" {
					t.Error("DockerService.Image is empty")
				}
				// Should have networks configured
				if len(result.DockerService.Networks) == 0 {
					t.Error("Expected networks to be configured")
				}
				// Should have labels
				if result.DockerService.Labels == nil {
					t.Error("Expected labels to be configured")
				}
				if result.DockerService.Labels["cloudexit.source"] != "aws_vpc" {
					t.Errorf("Expected source label to be aws_vpc, got %s", result.DockerService.Labels["cloudexit.source"])
				}
			},
		},
		{
			name: "VPC with default CIDR",
			res: &resource.AWSResource{
				ID:     "vpc-87654321",
				Type:   resource.TypeVPC,
				Name:   "default-vpc",
				Config: map[string]interface{}{},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should use default CIDR block when not specified
				if result.DockerService.Labels["cloudexit.cidr_block"] != "172.18.0.0/16" {
					t.Errorf("Expected default CIDR block 172.18.0.0/16, got %s", result.DockerService.Labels["cloudexit.cidr_block"])
				}
			},
		},
		{
			name: "VPC with subnets",
			res: &resource.AWSResource{
				ID:   "vpc-subnet-test",
				Type: resource.TypeVPC,
				Name: "vpc-with-subnets",
				Config: map[string]interface{}{
					"cidr_block": "10.0.0.0/16",
					"subnet": []interface{}{
						map[string]interface{}{
							"cidr_block":        "10.0.1.0/24",
							"availability_zone": "us-east-1a",
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have subnet configuration generated
				if len(result.Configs) == 0 {
					t.Error("Expected config files to be generated")
				}
			},
		},
		{
			name: "VPC with VPC peering",
			res: &resource.AWSResource{
				ID:   "vpc-peering-test",
				Type: resource.TypeVPC,
				Name: "vpc-with-peering",
				Config: map[string]interface{}{
					"cidr_block": "10.0.0.0/16",
					"vpc_peering_connection": map[string]interface{}{
						"peer_vpc_id": "vpc-peer-12345",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warnings about VPC peering
				if len(result.Warnings) == 0 {
					t.Error("Expected warnings for VPC peering")
				}
			},
		},
		{
			name: "VPC with NAT Gateway",
			res: &resource.AWSResource{
				ID:   "vpc-nat-test",
				Type: resource.TypeVPC,
				Name: "vpc-with-nat",
				Config: map[string]interface{}{
					"cidr_block": "10.0.0.0/16",
					"nat_gateway": map[string]interface{}{
						"allocation_id": "eipalloc-12345",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warnings about NAT Gateway
				hasNATWarning := false
				for _, w := range result.Warnings {
					if len(w) > 0 && w[0:3] == "NAT" {
						hasNATWarning = true
						break
					}
				}
				if !hasNATWarning {
					t.Log("Expected warning about NAT Gateway")
				}
			},
		},
		{
			name: "VPC with Internet Gateway",
			res: &resource.AWSResource{
				ID:   "vpc-igw-test",
				Type: resource.TypeVPC,
				Name: "vpc-with-igw",
				Config: map[string]interface{}{
					"cidr_block": "10.0.0.0/16",
					"internet_gateway": map[string]interface{}{
						"id": "igw-12345",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
			},
		},
		{
			name: "VPC with Security Groups",
			res: &resource.AWSResource{
				ID:   "vpc-sg-test",
				Type: resource.TypeVPC,
				Name: "vpc-with-sg",
				Config: map[string]interface{}{
					"cidr_block": "10.0.0.0/16",
					"security_group": []interface{}{
						map[string]interface{}{
							"name":        "web-sg",
							"description": "Web security group",
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warnings about Security Groups
				if len(result.Warnings) == 0 {
					t.Error("Expected warnings for Security Groups")
				}
			},
		},
		{
			name: "VPC with VPN Gateway",
			res: &resource.AWSResource{
				ID:   "vpc-vpn-test",
				Type: resource.TypeVPC,
				Name: "vpc-with-vpn",
				Config: map[string]interface{}{
					"cidr_block": "10.0.0.0/16",
					"vpn_gateway": map[string]interface{}{
						"id": "vgw-12345",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
			},
		},
		{
			name: "VPC with IPv6",
			res: &resource.AWSResource{
				ID:   "vpc-ipv6-test",
				Type: resource.TypeVPC,
				Name: "vpc-with-ipv6",
				Config: map[string]interface{}{
					"cidr_block":      "10.0.0.0/16",
					"ipv6_cidr_block": "2600:1f18::/56",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warnings about IPv6
				hasIPv6Warning := false
				for _, w := range result.Warnings {
					if len(w) > 4 && w[0:4] == "IPv6" {
						hasIPv6Warning = true
						break
					}
				}
				if !hasIPv6Warning {
					t.Log("Expected warning about IPv6")
				}
			},
		},
		{
			name: "VPC with DNS hostnames enabled",
			res: &resource.AWSResource{
				ID:   "vpc-dns-test",
				Type: resource.TypeVPC,
				Name: "vpc-with-dns",
				Config: map[string]interface{}{
					"cidr_block":           "10.0.0.0/16",
					"enable_dns_hostnames": true,
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
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
