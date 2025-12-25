package networking

import (
	"context"
	"testing"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

func TestNewALBMapper(t *testing.T) {
	m := NewALBMapper()
	if m == nil {
		t.Fatal("NewALBMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeALB {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeALB)
	}
}

func TestALBMapper_ResourceType(t *testing.T) {
	m := NewALBMapper()
	got := m.ResourceType()
	want := resource.TypeALB

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestALBMapper_Dependencies(t *testing.T) {
	m := NewALBMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestALBMapper_Validate(t *testing.T) {
	m := NewALBMapper()

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
				ID:   "test-id",
				Type: resource.TypeALB,
				Name: "test-alb",
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

func TestALBMapper_Map(t *testing.T) {
	m := NewALBMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic ALB",
			res: &resource.AWSResource{
				ID:   "arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/my-alb/50dc6c495c0c9188",
				Type: resource.TypeALB,
				Name: "my-alb",
				Config: map[string]interface{}{
					"name":               "my-alb",
					"load_balancer_type": "application",
					"internal":           false,
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
				// Should have ports configured
				if len(result.DockerService.Ports) == 0 {
					t.Log("Expected ports to be configured")
				}
			},
		},
		{
			name: "internal ALB",
			res: &resource.AWSResource{
				ID:   "arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/internal-alb/abc123",
				Type: resource.TypeALB,
				Name: "internal-alb",
				Config: map[string]interface{}{
					"name":     "internal-alb",
					"internal": true,
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
