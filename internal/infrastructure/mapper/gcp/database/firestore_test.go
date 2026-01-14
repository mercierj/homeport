package database

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestNewFirestoreMapper(t *testing.T) {
	m := NewFirestoreMapper()
	if m == nil {
		t.Fatal("NewFirestoreMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeFirestore {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeFirestore)
	}
}

func TestFirestoreMapper_ResourceType(t *testing.T) {
	m := NewFirestoreMapper()
	got := m.ResourceType()
	want := resource.TypeFirestore

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestFirestoreMapper_Dependencies(t *testing.T) {
	m := NewFirestoreMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestFirestoreMapper_Validate(t *testing.T) {
	m := NewFirestoreMapper()

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
				Type: resource.TypeFirestore,
				Name: "test-firestore",
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

func TestFirestoreMapper_Map(t *testing.T) {
	m := NewFirestoreMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Firestore database",
			res: &resource.AWSResource{
				ID:   "my-project/default",
				Type: resource.TypeFirestore,
				Name: "default",
				Config: map[string]interface{}{
					"name": "default",
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
				if result.DockerService.Image != "mongo:7" {
					t.Errorf("Expected MongoDB image, got %s", result.DockerService.Image)
				}
			},
		},
		{
			name: "Firestore with custom name",
			res: &resource.AWSResource{
				ID:   "my-project/app-database",
				Type: resource.TypeFirestore,
				Name: "app-database",
				Config: map[string]interface{}{
					"name": "app-database",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Check MongoDB environment is set
				if result.DockerService.Environment["MONGO_INITDB_ROOT_USERNAME"] != "admin" {
					t.Error("Expected MONGO_INITDB_ROOT_USERNAME to be admin")
				}
			},
		},
		{
			name: "Firestore with warnings",
			res: &resource.AWSResource{
				ID:   "my-project/users-db",
				Type: resource.TypeFirestore,
				Name: "users-db",
				Config: map[string]interface{}{
					"name": "users-db",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warnings about Firestore to MongoDB migration
				if len(result.Warnings) == 0 {
					t.Error("Expected warnings about Firestore to MongoDB migration")
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
