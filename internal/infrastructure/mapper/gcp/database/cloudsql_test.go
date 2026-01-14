package database

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestNewCloudSQLMapper(t *testing.T) {
	m := NewCloudSQLMapper()
	if m == nil {
		t.Fatal("NewCloudSQLMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeCloudSQL {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeCloudSQL)
	}
}

func TestCloudSQLMapper_ResourceType(t *testing.T) {
	m := NewCloudSQLMapper()
	got := m.ResourceType()
	want := resource.TypeCloudSQL

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestCloudSQLMapper_Dependencies(t *testing.T) {
	m := NewCloudSQLMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestCloudSQLMapper_Validate(t *testing.T) {
	m := NewCloudSQLMapper()

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
				Type: resource.TypeCloudSQL,
				Name: "test-db",
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

func TestCloudSQLMapper_Map(t *testing.T) {
	m := NewCloudSQLMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "PostgreSQL Cloud SQL instance",
			res: &resource.AWSResource{
				ID:   "my-project:us-central1:my-postgres",
				Type: resource.TypeCloudSQL,
				Name: "my-postgres",
				Config: map[string]interface{}{
					"name":             "my-postgres",
					"database_version": "POSTGRES_15",
					"region":           "us-central1",
					"settings": map[string]interface{}{
						"tier": "db-f1-micro",
					},
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
			name: "MySQL Cloud SQL instance",
			res: &resource.AWSResource{
				ID:   "my-project:us-central1:my-mysql",
				Type: resource.TypeCloudSQL,
				Name: "my-mysql",
				Config: map[string]interface{}{
					"name":             "my-mysql",
					"database_version": "MYSQL_8_0",
					"region":           "us-central1",
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
