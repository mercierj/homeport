package storage

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestNewBlobMapper(t *testing.T) {
	m := NewBlobMapper()
	if m == nil {
		t.Fatal("NewBlobMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeBlobStorage {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeBlobStorage)
	}
}

func TestBlobMapper_ResourceType(t *testing.T) {
	m := NewBlobMapper()
	got := m.ResourceType()
	want := resource.TypeBlobStorage

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestBlobMapper_Dependencies(t *testing.T) {
	m := NewBlobMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestBlobMapper_Validate(t *testing.T) {
	m := NewBlobMapper()

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
				Type: resource.TypeBlobStorage,
				Name: "test-container",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeBlobStorage,
				Name: "test-container",
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

func TestBlobMapper_Map(t *testing.T) {
	m := NewBlobMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Blob container",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/sa/blobServices/default/containers/mycontainer",
				Type: resource.TypeBlobStorage,
				Name: "mycontainer",
				Config: map[string]interface{}{
					"name": "my-blob-container",
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
			},
		},
		{
			name: "Blob container with public access",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/sa/blobServices/default/containers/publiccontainer",
				Type: resource.TypeBlobStorage,
				Name: "publiccontainer",
				Config: map[string]interface{}{
					"name":                  "public-blob-container",
					"container_access_type": "blob",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if !result.HasWarnings() {
					t.Error("Expected warnings for public access")
				}
			},
		},
		{
			name: "Blob container with metadata",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/sa/blobServices/default/containers/metacontainer",
				Type: resource.TypeBlobStorage,
				Name: "metacontainer",
				Config: map[string]interface{}{
					"name": "meta-blob-container",
					"metadata": map[string]interface{}{
						"environment": "production",
						"team":        "backend",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if !result.HasWarnings() {
					t.Error("Expected warnings for metadata")
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
