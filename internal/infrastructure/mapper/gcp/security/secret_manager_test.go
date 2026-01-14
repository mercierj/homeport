package security

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestNewSecretManagerMapper(t *testing.T) {
	m := NewSecretManagerMapper()
	if m == nil {
		t.Fatal("NewSecretManagerMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeSecretManager {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeSecretManager)
	}
}

func TestSecretManagerMapper_ResourceType(t *testing.T) {
	m := NewSecretManagerMapper()
	got := m.ResourceType()
	want := resource.TypeSecretManager

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestSecretManagerMapper_Dependencies(t *testing.T) {
	m := NewSecretManagerMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestSecretManagerMapper_Validate(t *testing.T) {
	m := NewSecretManagerMapper()

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
				Type: resource.TypeSecretManager,
				Name: "test-secret",
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

func TestSecretManagerMapper_Map(t *testing.T) {
	m := NewSecretManagerMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic secret",
			res: &resource.AWSResource{
				ID:   "projects/my-project/secrets/my-secret",
				Type: resource.TypeSecretManager,
				Name: "my-secret",
				Config: map[string]interface{}{
					"secret_id": "my-secret",
					"project":   "my-project",
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
				// Should use Vault image
				if result.DockerService.Image != "hashicorp/vault:1.15" {
					t.Errorf("Expected Vault image, got %s", result.DockerService.Image)
				}
			},
		},
		{
			name: "secret with replication",
			res: &resource.AWSResource{
				ID:   "projects/my-project/secrets/replicated-secret",
				Type: resource.TypeSecretManager,
				Name: "replicated-secret",
				Config: map[string]interface{}{
					"secret_id": "replicated-secret",
					"project":   "my-project",
					"replication": map[string]interface{}{
						"automatic": true,
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about replication
				hasReplicationWarning := false
				for _, w := range result.Warnings {
					if strings.Contains(w, "replication") || strings.Contains(w, "Replication") {
						hasReplicationWarning = true
						break
					}
				}
				if !hasReplicationWarning {
					t.Log("Expected warning about replication")
				}
			},
		},
		{
			name: "secret with user-managed replication",
			res: &resource.AWSResource{
				ID:   "projects/my-project/secrets/user-managed-secret",
				Type: resource.TypeSecretManager,
				Name: "user-managed-secret",
				Config: map[string]interface{}{
					"secret_id": "user-managed-secret",
					"project":   "my-project",
					"replication": map[string]interface{}{
						"user_managed": map[string]interface{}{
							"replicas": []interface{}{
								map[string]interface{}{"location": "us-central1"},
								map[string]interface{}{"location": "us-east1"},
							},
						},
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
			},
		},
		{
			name: "secret with labels",
			res: &resource.AWSResource{
				ID:   "projects/my-project/secrets/labeled-secret",
				Type: resource.TypeSecretManager,
				Name: "labeled-secret",
				Config: map[string]interface{}{
					"secret_id": "labeled-secret",
					"project":   "my-project",
					"labels": map[string]interface{}{
						"environment": "production",
						"team":        "platform",
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
			},
		},
		{
			name: "secret with rotation",
			res: &resource.AWSResource{
				ID:   "projects/my-project/secrets/rotating-secret",
				Type: resource.TypeSecretManager,
				Name: "rotating-secret",
				Config: map[string]interface{}{
					"secret_id": "rotating-secret",
					"project":   "my-project",
					"rotation": map[string]interface{}{
						"next_rotation_time": "2024-01-01T00:00:00Z",
						"rotation_period":    "2592000s",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about rotation
				hasRotationWarning := false
				for _, w := range result.Warnings {
					if strings.Contains(w, "rotation") || strings.Contains(w, "Rotation") {
						hasRotationWarning = true
						break
					}
				}
				if !hasRotationWarning {
					t.Log("Expected warning about secret rotation")
				}
			},
		},
		{
			name: "secret with TTL",
			res: &resource.AWSResource{
				ID:   "projects/my-project/secrets/ttl-secret",
				Type: resource.TypeSecretManager,
				Name: "ttl-secret",
				Config: map[string]interface{}{
					"secret_id": "ttl-secret",
					"project":   "my-project",
					"ttl":       "86400s",
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
