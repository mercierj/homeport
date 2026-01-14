package storage

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestNewFilesMapper(t *testing.T) {
	m := NewFilesMapper()
	if m == nil {
		t.Fatal("NewFilesMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeAzureFiles {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeAzureFiles)
	}
}

func TestFilesMapper_ResourceType(t *testing.T) {
	m := NewFilesMapper()
	got := m.ResourceType()
	want := resource.TypeAzureFiles

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestFilesMapper_Dependencies(t *testing.T) {
	m := NewFilesMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestFilesMapper_Validate(t *testing.T) {
	m := NewFilesMapper()

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
				Type: resource.TypeAzureFiles,
				Name: "test-share",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeAzureFiles,
				Name: "test-share",
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

func TestFilesMapper_Map(t *testing.T) {
	m := NewFilesMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Azure Files share",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/sa/fileServices/default/shares/myshare",
				Type: resource.TypeAzureFiles,
				Name: "myshare",
				Config: map[string]interface{}{
					"name":  "my-file-share",
					"quota": float64(100),
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
				if result.DockerService.HealthCheck == nil {
					t.Error("HealthCheck is nil")
				}
			},
		},
		{
			name: "Azure Files with NFS protocol",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/sa/fileServices/default/shares/nfsshare",
				Type: resource.TypeAzureFiles,
				Name: "nfsshare",
				Config: map[string]interface{}{
					"name":             "nfs-file-share",
					"quota":            float64(500),
					"enabled_protocol": "NFS",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if !result.HasWarnings() {
					t.Error("Expected warnings for NFS protocol")
				}
			},
		},
		{
			name: "Azure Files with access tier",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/sa/fileServices/default/shares/tieredshare",
				Type: resource.TypeAzureFiles,
				Name: "tieredshare",
				Config: map[string]interface{}{
					"name":        "tiered-file-share",
					"quota":       float64(200),
					"access_tier": "Cool",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if !result.HasWarnings() {
					t.Error("Expected warnings for access tier")
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
