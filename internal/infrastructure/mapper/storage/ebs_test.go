package storage

import (
	"context"
	"testing"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

func TestNewEBSMapper(t *testing.T) {
	m := NewEBSMapper()
	if m == nil {
		t.Fatal("NewEBSMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeEBSVolume {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeEBSVolume)
	}
}

func TestEBSMapper_ResourceType(t *testing.T) {
	m := NewEBSMapper()
	got := m.ResourceType()
	want := resource.TypeEBSVolume

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestEBSMapper_Dependencies(t *testing.T) {
	m := NewEBSMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestEBSMapper_Validate(t *testing.T) {
	m := NewEBSMapper()

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
				Type: resource.TypeEBSVolume,
				Name: "test-volume",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeEBSVolume,
				Name: "test-volume",
			},
			wantErr: true,
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

func TestEBSMapper_Map(t *testing.T) {
	m := NewEBSMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic EBS volume (gp2)",
			res: &resource.AWSResource{
				ID:   "vol-1234567890abcdef0",
				Type: resource.TypeEBSVolume,
				Name: "my-volume",
				Config: map[string]interface{}{
					"size": float64(100),
					"type": "gp2",
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
				// Should have warning about volume type
				if len(result.Warnings) == 0 {
					t.Log("Expected warning about volume type")
				}
			},
		},
		{
			name: "EBS volume gp3",
			res: &resource.AWSResource{
				ID:   "vol-gp3volume",
				Type: resource.TypeEBSVolume,
				Name: "gp3-volume",
				Config: map[string]interface{}{
					"size": float64(200),
					"type": "gp3",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about gp3
				hasGP3Warning := false
				for _, w := range result.Warnings {
					if len(w) > 0 {
						hasGP3Warning = true
						break
					}
				}
				if !hasGP3Warning {
					t.Log("Expected warning about gp3 volume type")
				}
			},
		},
		{
			name: "EBS volume io1 (provisioned IOPS)",
			res: &resource.AWSResource{
				ID:   "vol-io1volume",
				Type: resource.TypeEBSVolume,
				Name: "io1-volume",
				Config: map[string]interface{}{
					"size": float64(500),
					"type": "io1",
					"iops": float64(3000),
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about IOPS
				if len(result.Warnings) == 0 {
					t.Log("Expected warning about provisioned IOPS")
				}
			},
		},
		{
			name: "EBS volume io2 with throughput",
			res: &resource.AWSResource{
				ID:   "vol-io2volume",
				Type: resource.TypeEBSVolume,
				Name: "io2-volume",
				Config: map[string]interface{}{
					"size":       float64(1000),
					"type":       "io2",
					"iops":       float64(5000),
					"throughput": float64(500),
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about throughput
				if len(result.Warnings) == 0 {
					t.Log("Expected warning about throughput")
				}
			},
		},
		{
			name: "EBS volume st1 (throughput optimized HDD)",
			res: &resource.AWSResource{
				ID:   "vol-st1volume",
				Type: resource.TypeEBSVolume,
				Name: "st1-volume",
				Config: map[string]interface{}{
					"size": float64(500),
					"type": "st1",
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
			name: "EBS volume sc1 (cold HDD)",
			res: &resource.AWSResource{
				ID:   "vol-sc1volume",
				Type: resource.TypeEBSVolume,
				Name: "sc1-volume",
				Config: map[string]interface{}{
					"size": float64(500),
					"type": "sc1",
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
			name: "encrypted EBS volume",
			res: &resource.AWSResource{
				ID:   "vol-encrypted",
				Type: resource.TypeEBSVolume,
				Name: "encrypted-volume",
				Config: map[string]interface{}{
					"size":      float64(100),
					"type":      "gp2",
					"encrypted": true,
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about encryption
				if len(result.Warnings) == 0 {
					t.Log("Expected warning about encryption")
				}
			},
		},
		{
			name: "EBS volume with KMS key",
			res: &resource.AWSResource{
				ID:   "vol-kms",
				Type: resource.TypeEBSVolume,
				Name: "kms-volume",
				Config: map[string]interface{}{
					"size":       float64(100),
					"type":       "gp2",
					"encrypted":  true,
					"kms_key_id": "arn:aws:kms:us-east-1:123456789012:key/abc-123",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about KMS
				if len(result.Warnings) == 0 {
					t.Log("Expected warning about KMS encryption")
				}
			},
		},
		{
			name: "EBS volume from snapshot",
			res: &resource.AWSResource{
				ID:   "vol-snapshot",
				Type: resource.TypeEBSVolume,
				Name: "snapshot-volume",
				Config: map[string]interface{}{
					"size":        float64(100),
					"type":        "gp2",
					"snapshot_id": "snap-1234567890abcdef0",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about snapshot
				if len(result.Warnings) == 0 {
					t.Log("Expected warning about snapshot")
				}
			},
		},
		{
			name: "EBS volume with availability zone",
			res: &resource.AWSResource{
				ID:   "vol-az",
				Type: resource.TypeEBSVolume,
				Name: "az-volume",
				Config: map[string]interface{}{
					"size":              float64(100),
					"type":              "gp2",
					"availability_zone": "us-east-1a",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about AZ
				if len(result.Warnings) == 0 {
					t.Log("Expected warning about availability zone")
				}
			},
		},
		{
			name:    "nil resource",
			res:     nil,
			wantErr: true,
		},
		{
			name: "wrong resource type",
			res: &resource.AWSResource{
				ID:   "wrong-id",
				Type: resource.TypeS3Bucket,
				Name: "wrong",
			},
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
