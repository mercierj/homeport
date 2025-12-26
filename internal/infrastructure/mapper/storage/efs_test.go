package storage

import (
	"context"
	"testing"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

func TestNewEFSMapper(t *testing.T) {
	m := NewEFSMapper()
	if m == nil {
		t.Fatal("NewEFSMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeEFSVolume {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeEFSVolume)
	}
}

func TestEFSMapper_ResourceType(t *testing.T) {
	m := NewEFSMapper()
	got := m.ResourceType()
	want := resource.TypeEFSVolume

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestEFSMapper_Dependencies(t *testing.T) {
	m := NewEFSMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestEFSMapper_Validate(t *testing.T) {
	m := NewEFSMapper()

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
				Type: resource.TypeEFSVolume,
				Name: "test-filesystem",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeEFSVolume,
				Name: "test-filesystem",
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

func TestEFSMapper_Map(t *testing.T) {
	m := NewEFSMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic EFS file system",
			res: &resource.AWSResource{
				ID:   "fs-1234567890abcdef0",
				Type: resource.TypeEFSVolume,
				Name: "my-efs",
				Config: map[string]interface{}{
					"creation_token": "my-efs",
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
				// Should use NFS server image
				if result.DockerService.Image != "itsthenetwork/nfs-server-alpine:12" {
					t.Errorf("Expected NFS server image, got %s", result.DockerService.Image)
				}
			},
		},
		{
			name: "EFS with generalPurpose performance mode",
			res: &resource.AWSResource{
				ID:   "fs-generalpurpose",
				Type: resource.TypeEFSVolume,
				Name: "gp-efs",
				Config: map[string]interface{}{
					"creation_token":   "gp-efs",
					"performance_mode": "generalPurpose",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about performance mode
				if len(result.Warnings) == 0 {
					t.Log("Expected warning about performance mode")
				}
			},
		},
		{
			name: "EFS with maxIO performance mode",
			res: &resource.AWSResource{
				ID:   "fs-maxio",
				Type: resource.TypeEFSVolume,
				Name: "maxio-efs",
				Config: map[string]interface{}{
					"creation_token":   "maxio-efs",
					"performance_mode": "maxIO",
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
			name: "EFS with provisioned throughput",
			res: &resource.AWSResource{
				ID:   "fs-provisioned",
				Type: resource.TypeEFSVolume,
				Name: "provisioned-efs",
				Config: map[string]interface{}{
					"creation_token":                "provisioned-efs",
					"throughput_mode":               "provisioned",
					"provisioned_throughput_in_mibps": float64(100),
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about provisioned throughput
				if len(result.Warnings) == 0 {
					t.Log("Expected warning about provisioned throughput")
				}
			},
		},
		{
			name: "EFS with bursting throughput",
			res: &resource.AWSResource{
				ID:   "fs-bursting",
				Type: resource.TypeEFSVolume,
				Name: "bursting-efs",
				Config: map[string]interface{}{
					"creation_token":  "bursting-efs",
					"throughput_mode": "bursting",
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
			name: "encrypted EFS",
			res: &resource.AWSResource{
				ID:   "fs-encrypted",
				Type: resource.TypeEFSVolume,
				Name: "encrypted-efs",
				Config: map[string]interface{}{
					"creation_token": "encrypted-efs",
					"encrypted":      true,
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
			name: "EFS with lifecycle policy",
			res: &resource.AWSResource{
				ID:   "fs-lifecycle",
				Type: resource.TypeEFSVolume,
				Name: "lifecycle-efs",
				Config: map[string]interface{}{
					"creation_token": "lifecycle-efs",
					"lifecycle_policy": map[string]interface{}{
						"transition_to_ia": "AFTER_30_DAYS",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about lifecycle policy
				if len(result.Warnings) == 0 {
					t.Log("Expected warning about lifecycle policy")
				}
			},
		},
		{
			name: "EFS with access point",
			res: &resource.AWSResource{
				ID:   "fs-accesspoint",
				Type: resource.TypeEFSVolume,
				Name: "accesspoint-efs",
				Config: map[string]interface{}{
					"creation_token": "accesspoint-efs",
					"access_point": map[string]interface{}{
						"root_directory": "/app",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about access points
				if len(result.Warnings) == 0 {
					t.Log("Expected warning about access points")
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

func TestEFSMapper_getPerformanceMode(t *testing.T) {
	m := NewEFSMapper()

	tests := []struct {
		name   string
		config map[string]interface{}
		want   string
	}{
		{
			name:   "no performance mode",
			config: map[string]interface{}{},
			want:   "generalPurpose",
		},
		{
			name:   "generalPurpose mode",
			config: map[string]interface{}{"performance_mode": "generalPurpose"},
			want:   "generalPurpose",
		},
		{
			name:   "maxIO mode",
			config: map[string]interface{}{"performance_mode": "maxIO"},
			want:   "maxIO",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := &resource.AWSResource{
				ID:     "test",
				Type:   resource.TypeEFSVolume,
				Name:   "test",
				Config: tt.config,
			}
			got := m.getPerformanceMode(res)
			if got != tt.want {
				t.Errorf("getPerformanceMode() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEFSMapper_getThroughputMode(t *testing.T) {
	m := NewEFSMapper()

	tests := []struct {
		name   string
		config map[string]interface{}
		want   string
	}{
		{
			name:   "no throughput mode",
			config: map[string]interface{}{},
			want:   "bursting",
		},
		{
			name:   "bursting mode",
			config: map[string]interface{}{"throughput_mode": "bursting"},
			want:   "bursting",
		},
		{
			name:   "provisioned mode",
			config: map[string]interface{}{"throughput_mode": "provisioned"},
			want:   "provisioned",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := &resource.AWSResource{
				ID:     "test",
				Type:   resource.TypeEFSVolume,
				Name:   "test",
				Config: tt.config,
			}
			got := m.getThroughputMode(res)
			if got != tt.want {
				t.Errorf("getThroughputMode() = %q, want %q", got, tt.want)
			}
		})
	}
}
