package storage

import (
	"context"
	"testing"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

func TestNewManagedDiskMapper(t *testing.T) {
	m := NewManagedDiskMapper()
	if m == nil {
		t.Fatal("NewManagedDiskMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeManagedDisk {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeManagedDisk)
	}
}

func TestManagedDiskMapper_ResourceType(t *testing.T) {
	m := NewManagedDiskMapper()
	got := m.ResourceType()
	want := resource.TypeManagedDisk

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestManagedDiskMapper_Dependencies(t *testing.T) {
	m := NewManagedDiskMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestManagedDiskMapper_Validate(t *testing.T) {
	m := NewManagedDiskMapper()

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
				Type: resource.TypeEC2Instance,
				Name: "test",
			},
			wantErr: true,
		},
		{
			name: "valid resource",
			res: &resource.AWSResource{
				ID:   "test-id",
				Type: resource.TypeManagedDisk,
				Name: "test-disk",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeManagedDisk,
				Name: "test-disk",
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

func TestManagedDiskMapper_Map(t *testing.T) {
	m := NewManagedDiskMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Managed Disk",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Compute/disks/my-disk",
				Type: resource.TypeManagedDisk,
				Name: "my-disk",
				Config: map[string]interface{}{
					"name":                 "my-managed-disk",
					"disk_size_gb":         float64(128),
					"storage_account_type": "Standard_LRS",
					"create_option":        "Empty",
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
				if len(result.Volumes) == 0 {
					t.Error("Expected at least one volume")
				}
			},
		},
		{
			name: "Premium SSD Disk",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Compute/disks/premium-disk",
				Type: resource.TypeManagedDisk,
				Name: "premium-disk",
				Config: map[string]interface{}{
					"name":                 "premium-managed-disk",
					"disk_size_gb":         float64(256),
					"storage_account_type": "Premium_LRS",
					"create_option":        "Empty",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if !result.HasWarnings() {
					t.Error("Expected warnings for Premium SSD")
				}
			},
		},
		{
			name: "Disk with encryption",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Compute/disks/encrypted-disk",
				Type: resource.TypeManagedDisk,
				Name: "encrypted-disk",
				Config: map[string]interface{}{
					"name":                 "encrypted-managed-disk",
					"disk_size_gb":         float64(128),
					"storage_account_type": "Standard_LRS",
					"encryption_settings": map[string]interface{}{
						"enabled": true,
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if !result.HasWarnings() {
					t.Error("Expected warnings for encryption settings")
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
				Type: resource.TypeEC2Instance,
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
