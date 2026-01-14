package database

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestNewRDSClusterMapper(t *testing.T) {
	m := NewRDSClusterMapper()
	if m == nil {
		t.Fatal("NewRDSClusterMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeRDSCluster {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeRDSCluster)
	}
}

func TestRDSClusterMapper_ResourceType(t *testing.T) {
	m := NewRDSClusterMapper()
	got := m.ResourceType()
	want := resource.TypeRDSCluster

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestRDSClusterMapper_Dependencies(t *testing.T) {
	m := NewRDSClusterMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestRDSClusterMapper_Validate(t *testing.T) {
	m := NewRDSClusterMapper()

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
				Type: resource.TypeRDSCluster,
				Name: "test-cluster",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeRDSCluster,
				Name: "test-cluster",
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

func TestRDSClusterMapper_Map(t *testing.T) {
	m := NewRDSClusterMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "Aurora PostgreSQL cluster",
			res: &resource.AWSResource{
				ID:   "aurora-postgres-cluster",
				Type: resource.TypeRDSCluster,
				Name: "my-aurora-postgres",
				Config: map[string]interface{}{
					"cluster_identifier": "my-aurora-postgres",
					"engine":             "aurora-postgresql",
					"engine_version":     "15.4",
					"database_name":      "mydb",
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
			name: "Aurora MySQL cluster",
			res: &resource.AWSResource{
				ID:   "aurora-mysql-cluster",
				Type: resource.TypeRDSCluster,
				Name: "my-aurora-mysql",
				Config: map[string]interface{}{
					"cluster_identifier": "my-aurora-mysql",
					"engine":             "aurora-mysql",
					"engine_version":     "8.0.mysql_aurora.3.04.0",
					"database_name":      "mydb",
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
			name: "Aurora cluster with encryption",
			res: &resource.AWSResource{
				ID:   "aurora-encrypted",
				Type: resource.TypeRDSCluster,
				Name: "encrypted-cluster",
				Config: map[string]interface{}{
					"cluster_identifier": "encrypted-cluster",
					"engine":             "aurora-postgresql",
					"database_name":      "mydb",
					"storage_encrypted":  true,
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about encryption
				if len(result.Warnings) == 0 {
					t.Log("Expected warning about storage encryption")
				}
			},
		},
		{
			name: "Aurora cluster with deletion protection",
			res: &resource.AWSResource{
				ID:   "aurora-protected",
				Type: resource.TypeRDSCluster,
				Name: "protected-cluster",
				Config: map[string]interface{}{
					"cluster_identifier":  "protected-cluster",
					"engine":              "aurora-postgresql",
					"database_name":       "mydb",
					"deletion_protection": true,
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about deletion protection
				if len(result.Warnings) == 0 {
					t.Log("Expected warning about deletion protection")
				}
			},
		},
		{
			name: "Aurora cluster with HTTP endpoint",
			res: &resource.AWSResource{
				ID:   "aurora-data-api",
				Type: resource.TypeRDSCluster,
				Name: "data-api-cluster",
				Config: map[string]interface{}{
					"cluster_identifier":   "data-api-cluster",
					"engine":               "aurora-postgresql",
					"database_name":        "mydb",
					"enable_http_endpoint": true,
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about Data API
				if len(result.Warnings) == 0 {
					t.Log("Expected warning about Aurora Data API")
				}
			},
		},
		{
			name: "unsupported engine",
			res: &resource.AWSResource{
				ID:   "unsupported-cluster",
				Type: resource.TypeRDSCluster,
				Name: "unsupported-cluster",
				Config: map[string]interface{}{
					"cluster_identifier": "unsupported-cluster",
					"engine":             "unsupported-engine",
					"database_name":      "mydb",
				},
			},
			wantErr: true,
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
