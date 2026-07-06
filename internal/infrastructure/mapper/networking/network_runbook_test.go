package networking

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestNetworkingRunbookFixtureCoversRoutingDNSCDNAndNetwork(t *testing.T) {
	fixture := []struct {
		name   string
		mapper interface {
			Map(context.Context, *resource.AWSResource) (*mapper.MappingResult, error)
		}
		resource *resource.AWSResource
		kind     string
	}{
		{
			name:   "alb routing",
			mapper: NewALBMapper(),
			resource: &resource.AWSResource{
				ID:     "alb-1",
				Type:   resource.TypeALB,
				Name:   "edge",
				Config: map[string]interface{}{"name": "edge"},
			},
			kind: "routing",
		},
		{
			name:   "route53 dns",
			mapper: NewRoute53Mapper(),
			resource: &resource.AWSResource{
				ID:     "zone-1",
				Type:   resource.TypeRoute53Zone,
				Name:   "example.com",
				Config: map[string]interface{}{"name": "example.com"},
			},
			kind: "dns",
		},
		{
			name:   "cloudfront edge",
			mapper: NewCloudFrontMapper(),
			resource: &resource.AWSResource{
				ID:     "dist-1",
				Type:   resource.TypeCloudFront,
				Name:   "cdn",
				Config: map[string]interface{}{},
			},
			kind: "edge",
		},
		{
			name:   "vpc network",
			mapper: NewVPCMapper(),
			resource: &resource.AWSResource{
				ID:     "vpc-1",
				Type:   resource.TypeVPC,
				Name:   "main",
				Config: map[string]interface{}{"cidr_block": "10.0.0.0/16"},
			},
			kind: "network",
		},
	}

	for _, tt := range fixture {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.mapper.Map(context.Background(), tt.resource)
			if err != nil {
				t.Fatalf("Map() error = %v", err)
			}
			if !hasNetworkingRunbookKind(result, tt.kind) {
				t.Fatalf("missing %s runbook steps: %#v", tt.kind, result.RunbookSteps)
			}
		})
	}
}

func hasNetworkingRunbookKind(result *mapper.MappingResult, kind string) bool {
	for _, step := range result.RunbookSteps {
		if step.Metadata["kind"] == kind {
			return true
		}
	}
	return false
}
