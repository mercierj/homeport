package networking

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestNewRoute53Mapper(t *testing.T) {
	m := NewRoute53Mapper()
	if m == nil {
		t.Fatal("NewRoute53Mapper() returned nil")
	}
	if m.ResourceType() != resource.TypeRoute53Zone {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeRoute53Zone)
	}
}

func TestRoute53Mapper_ResourceType(t *testing.T) {
	m := NewRoute53Mapper()
	got := m.ResourceType()
	want := resource.TypeRoute53Zone

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestRoute53Mapper_Dependencies(t *testing.T) {
	m := NewRoute53Mapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestRoute53Mapper_Validate(t *testing.T) {
	m := NewRoute53Mapper()

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
				ID:   "Z1234567890ABC",
				Type: resource.TypeRoute53Zone,
				Name: "example.com",
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

func TestRoute53Mapper_Map(t *testing.T) {
	m := NewRoute53Mapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Route53 hosted zone",
			res: &resource.AWSResource{
				ID:   "Z1234567890ABC",
				Type: resource.TypeRoute53Zone,
				Name: "example.com",
				Config: map[string]interface{}{
					"name": "example.com",
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
				// Should use CoreDNS image
				if result.DockerService.Image != "coredns/coredns:1.11.1" {
					t.Errorf("Expected image coredns/coredns:1.11.1, got %s", result.DockerService.Image)
				}
				// Should have DNS ports configured
				if len(result.DockerService.Ports) == 0 {
					t.Error("Expected ports to be configured")
				}
				// Check for DNS port 53
				hasDNSPort := false
				for _, port := range result.DockerService.Ports {
					if port == "53:53/tcp" || port == "53:53/udp" {
						hasDNSPort = true
						break
					}
				}
				if !hasDNSPort {
					t.Error("Expected DNS port 53 to be configured")
				}
				// Should have labels
				if result.DockerService.Labels == nil {
					t.Error("Expected labels to be configured")
				}
				if result.DockerService.Labels["homeport.source"] != "aws_route53" {
					t.Errorf("Expected source label to be aws_route53, got %s", result.DockerService.Labels["homeport.source"])
				}
			},
		},
		{
			name: "private hosted zone",
			res: &resource.AWSResource{
				ID:   "Z1234567890DEF",
				Type: resource.TypeRoute53Zone,
				Name: "internal.example.com",
				Config: map[string]interface{}{
					"name":         "internal.example.com",
					"private_zone": true,
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warnings about private zone
				hasPrivateWarning := false
				for _, w := range result.Warnings {
					if containsSubstring(w, "Private") || containsSubstring(w, "private") {
						hasPrivateWarning = true
						break
					}
				}
				if !hasPrivateWarning {
					t.Log("Expected warning about private hosted zone")
				}
			},
		},
		{
			name: "public hosted zone",
			res: &resource.AWSResource{
				ID:   "Z1234567890GHI",
				Type: resource.TypeRoute53Zone,
				Name: "public.example.com",
				Config: map[string]interface{}{
					"name":         "public.example.com",
					"private_zone": false,
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warnings about public zone
				hasPublicWarning := false
				for _, w := range result.Warnings {
					if containsSubstring(w, "Public") || containsSubstring(w, "public") {
						hasPublicWarning = true
						break
					}
				}
				if !hasPublicWarning {
					t.Log("Expected warning about public hosted zone")
				}
			},
		},
		{
			name: "Route53 with DNSSEC enabled",
			res: &resource.AWSResource{
				ID:   "Z1234567890JKL",
				Type: resource.TypeRoute53Zone,
				Name: "secure.example.com",
				Config: map[string]interface{}{
					"name": "secure.example.com",
					"dnssec_config": map[string]interface{}{
						"signing_enabled": true,
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warnings about DNSSEC
				hasDNSSECWarning := false
				for _, w := range result.Warnings {
					if containsSubstring(w, "DNSSEC") {
						hasDNSSECWarning = true
						break
					}
				}
				if !hasDNSSECWarning {
					t.Log("Expected warning about DNSSEC")
				}
			},
		},
		{
			name: "Route53 with health checks",
			res: &resource.AWSResource{
				ID:   "Z1234567890MNO",
				Type: resource.TypeRoute53Zone,
				Name: "monitored.example.com",
				Config: map[string]interface{}{
					"name": "monitored.example.com",
					"health_check": map[string]interface{}{
						"type":              "HTTP",
						"resource_path":     "/health",
						"failure_threshold": 3,
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warnings about health checks
				hasHealthCheckWarning := false
				for _, w := range result.Warnings {
					if containsSubstring(w, "health") {
						hasHealthCheckWarning = true
						break
					}
				}
				if !hasHealthCheckWarning {
					t.Log("Expected warning about health checks")
				}
			},
		},
		{
			name: "Route53 with traffic policies",
			res: &resource.AWSResource{
				ID:   "Z1234567890PQR",
				Type: resource.TypeRoute53Zone,
				Name: "traffic.example.com",
				Config: map[string]interface{}{
					"name": "traffic.example.com",
					"traffic_policy": map[string]interface{}{
						"id":      "policy-12345",
						"version": 1,
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warnings about traffic policies
				hasTrafficWarning := false
				for _, w := range result.Warnings {
					if containsSubstring(w, "traffic") {
						hasTrafficWarning = true
						break
					}
				}
				if !hasTrafficWarning {
					t.Log("Expected warning about traffic policies")
				}
			},
		},
		{
			name: "Route53 with geolocation routing",
			res: &resource.AWSResource{
				ID:   "Z1234567890STU",
				Type: resource.TypeRoute53Zone,
				Name: "geo.example.com",
				Config: map[string]interface{}{
					"name": "geo.example.com",
					"geolocation_routing_policy": map[string]interface{}{
						"continent":   "EU",
						"country":     "DE",
						"subdivision": "",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warnings about complex record types
				hasComplexWarning := false
				for _, w := range result.Warnings {
					if containsSubstring(w, "Complex") || containsSubstring(w, "geolocation") {
						hasComplexWarning = true
						break
					}
				}
				if !hasComplexWarning {
					t.Log("Expected warning about complex record types")
				}
			},
		},
		{
			name: "Route53 with latency routing",
			res: &resource.AWSResource{
				ID:   "Z1234567890VWX",
				Type: resource.TypeRoute53Zone,
				Name: "latency.example.com",
				Config: map[string]interface{}{
					"name": "latency.example.com",
					"latency_routing_policy": map[string]interface{}{
						"region": "us-east-1",
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
			name: "Route53 with weighted routing",
			res: &resource.AWSResource{
				ID:   "Z1234567890YZA",
				Type: resource.TypeRoute53Zone,
				Name: "weighted.example.com",
				Config: map[string]interface{}{
					"name": "weighted.example.com",
					"weighted_routing_policy": map[string]interface{}{
						"weight": 70,
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
			name: "Route53 with failover routing",
			res: &resource.AWSResource{
				ID:   "Z1234567890BCD",
				Type: resource.TypeRoute53Zone,
				Name: "failover.example.com",
				Config: map[string]interface{}{
					"name": "failover.example.com",
					"failover_routing_policy": map[string]interface{}{
						"type": "PRIMARY",
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
			name: "Route53 with logging enabled",
			res: &resource.AWSResource{
				ID:   "Z1234567890EFG",
				Type: resource.TypeRoute53Zone,
				Name: "logged.example.com",
				Config: map[string]interface{}{
					"name":           "logged.example.com",
					"enable_logging": true,
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warnings about logging
				hasLoggingWarning := false
				for _, w := range result.Warnings {
					if containsSubstring(w, "logging") || containsSubstring(w, "log") {
						hasLoggingWarning = true
						break
					}
				}
				if !hasLoggingWarning {
					t.Log("Expected warning about query logging")
				}
			},
		},
		{
			name: "Route53 zone without explicit name",
			res: &resource.AWSResource{
				ID:     "Z1234567890HIJ",
				Type:   resource.TypeRoute53Zone,
				Name:   "fallback.example.com",
				Config: map[string]interface{}{},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should use resource Name as zone name
				if result.DockerService.Labels["homeport.zone_name"] != "fallback.example.com" {
					t.Errorf("Expected zone_name label to be fallback.example.com, got %s", result.DockerService.Labels["homeport.zone_name"])
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
