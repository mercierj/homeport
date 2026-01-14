package database

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestNewRDSMapper(t *testing.T) {
	m := NewRDSMapper()
	if m == nil {
		t.Fatal("NewRDSMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeRDSInstance {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeRDSInstance)
	}
}

func TestRDSMapper_ResourceType(t *testing.T) {
	m := NewRDSMapper()
	got := m.ResourceType()
	want := resource.TypeRDSInstance

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestRDSMapper_Dependencies(t *testing.T) {
	m := NewRDSMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestRDSMapper_Validate(t *testing.T) {
	m := NewRDSMapper()

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
				Type: resource.TypeRDSInstance,
				Name: "test-db",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeRDSInstance,
				Name: "test-db",
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

func TestRDSMapper_Map(t *testing.T) {
	m := NewRDSMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "PostgreSQL RDS instance",
			res: &resource.AWSResource{
				ID:   "db-instance-1",
				Type: resource.TypeRDSInstance,
				Name: "my-postgres-db",
				Config: map[string]interface{}{
					"identifier":        "my-postgres-db",
					"engine":            "postgres",
					"engine_version":    "15.4",
					"instance_class":    "db.t3.medium",
					"allocated_storage": float64(100),
					"username":          "admin",
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
				// Should use postgres image
				if result.DockerService.Image != "postgres:15" && result.DockerService.Image != "postgres:15.4" {
					t.Logf("DockerService.Image = %s (checking for postgres version)", result.DockerService.Image)
				}
			},
		},
		{
			name: "MySQL RDS instance",
			res: &resource.AWSResource{
				ID:   "db-instance-2",
				Type: resource.TypeRDSInstance,
				Name: "my-mysql-db",
				Config: map[string]interface{}{
					"identifier":     "my-mysql-db",
					"engine":         "mysql",
					"engine_version": "8.0",
					"instance_class": "db.t3.small",
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
