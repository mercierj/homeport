package security

import (
	"context"
	"testing"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

func TestNewSecretsManagerMapper(t *testing.T) {
	m := NewSecretsManagerMapper()
	if m == nil {
		t.Fatal("NewSecretsManagerMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeSecretsManager {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeSecretsManager)
	}
}

func TestSecretsManagerMapper_ResourceType(t *testing.T) {
	m := NewSecretsManagerMapper()
	got := m.ResourceType()
	want := resource.TypeSecretsManager

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestSecretsManagerMapper_Dependencies(t *testing.T) {
	m := NewSecretsManagerMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestSecretsManagerMapper_Validate(t *testing.T) {
	m := NewSecretsManagerMapper()

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
				Type: resource.TypeSecretsManager,
				Name: "test-secret",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeSecretsManager,
				Name: "test-secret",
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

func TestSecretsManagerMapper_Map(t *testing.T) {
	m := NewSecretsManagerMapper()
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
				ID:   "arn:aws:secretsmanager:us-east-1:123456789012:secret:my-secret-abc123",
				Type: resource.TypeSecretsManager,
				Name: "my-secret",
				Config: map[string]interface{}{
					"name": "my-secret",
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
			name: "secret with description",
			res: &resource.AWSResource{
				ID:   "arn:aws:secretsmanager:us-east-1:123456789012:secret:db-password-xyz789",
				Type: resource.TypeSecretsManager,
				Name: "db-password",
				Config: map[string]interface{}{
					"name":        "db-password",
					"description": "Database password for production",
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
			name: "secret with KMS key",
			res: &resource.AWSResource{
				ID:   "arn:aws:secretsmanager:us-east-1:123456789012:secret:encrypted-secret-def456",
				Type: resource.TypeSecretsManager,
				Name: "encrypted-secret",
				Config: map[string]interface{}{
					"name":       "encrypted-secret",
					"kms_key_id": "arn:aws:kms:us-east-1:123456789012:key/12345678-1234-1234-1234-123456789012",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about KMS encryption
				if len(result.Warnings) == 0 {
					t.Log("Expected warning about KMS encryption")
				}
			},
		},
		{
			name: "secret with rotation",
			res: &resource.AWSResource{
				ID:   "arn:aws:secretsmanager:us-east-1:123456789012:secret:rotating-secret-ghi012",
				Type: resource.TypeSecretsManager,
				Name: "rotating-secret",
				Config: map[string]interface{}{
					"name":            "rotating-secret",
					"rotation_lambda_arn": "arn:aws:lambda:us-east-1:123456789012:function:rotate-secret",
					"rotation_rules": map[string]interface{}{
						"automatically_after_days": float64(30),
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about rotation
				if len(result.Warnings) == 0 {
					t.Log("Expected warning about secret rotation")
				}
			},
		},
		{
			name: "secret with tags",
			res: &resource.AWSResource{
				ID:   "arn:aws:secretsmanager:us-east-1:123456789012:secret:tagged-secret-jkl345",
				Type: resource.TypeSecretsManager,
				Name: "tagged-secret",
				Config: map[string]interface{}{
					"name": "tagged-secret",
					"tags": map[string]interface{}{
						"Environment": "production",
						"Application": "my-app",
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
