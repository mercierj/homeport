package security

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestNewIAMMapper(t *testing.T) {
	m := NewIAMMapper()
	if m == nil {
		t.Fatal("NewIAMMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeIAMRole {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeIAMRole)
	}
}

func TestIAMMapper_ResourceType(t *testing.T) {
	m := NewIAMMapper()
	got := m.ResourceType()
	want := resource.TypeIAMRole

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestIAMMapper_Dependencies(t *testing.T) {
	m := NewIAMMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestIAMMapper_Validate(t *testing.T) {
	m := NewIAMMapper()

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
				Type: resource.TypeIAMRole,
				Name: "test-role",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeIAMRole,
				Name: "test-role",
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

func TestIAMMapper_Map(t *testing.T) {
	m := NewIAMMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic IAM role",
			res: &resource.AWSResource{
				ID:   "arn:aws:iam::123456789012:role/my-role",
				Type: resource.TypeIAMRole,
				Name: "my-role",
				Config: map[string]interface{}{
					"name": "my-role",
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
				// Should use Keycloak image
				if result.DockerService.Image != "quay.io/keycloak/keycloak:23.0" {
					t.Errorf("Expected Keycloak image, got %s", result.DockerService.Image)
				}
			},
		},
		{
			name: "IAM role with assume role policy",
			res: &resource.AWSResource{
				ID:   "arn:aws:iam::123456789012:role/lambda-role",
				Type: resource.TypeIAMRole,
				Name: "lambda-role",
				Config: map[string]interface{}{
					"name": "lambda-role",
					"assume_role_policy": `{
						"Version": "2012-10-17",
						"Statement": [{
							"Effect": "Allow",
							"Principal": {"Service": "lambda.amazonaws.com"},
							"Action": "sts:AssumeRole"
						}]
					}`,
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about assume role policy
				if len(result.Warnings) == 0 {
					t.Log("Expected warning about assume role policy")
				}
			},
		},
		{
			name: "IAM role with managed policies",
			res: &resource.AWSResource{
				ID:   "arn:aws:iam::123456789012:role/admin-role",
				Type: resource.TypeIAMRole,
				Name: "admin-role",
				Config: map[string]interface{}{
					"name": "admin-role",
					"managed_policy_arns": []interface{}{
						"arn:aws:iam::aws:policy/AdministratorAccess",
						"arn:aws:iam::aws:policy/ReadOnlyAccess",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about attached policies
				if len(result.Warnings) == 0 {
					t.Log("Expected warning about attached policies")
				}
			},
		},
		{
			name: "IAM role with inline policy",
			res: &resource.AWSResource{
				ID:   "arn:aws:iam::123456789012:role/s3-role",
				Type: resource.TypeIAMRole,
				Name: "s3-role",
				Config: map[string]interface{}{
					"name": "s3-role",
					"inline_policy": []interface{}{
						map[string]interface{}{
							"name": "s3-access",
							"policy": `{
								"Version": "2012-10-17",
								"Statement": [{
									"Effect": "Allow",
									"Action": ["s3:GetObject", "s3:PutObject"],
									"Resource": "arn:aws:s3:::my-bucket/*"
								}]
							}`,
						},
					},
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

func TestIAMMapper_extractAttachedPolicies(t *testing.T) {
	m := NewIAMMapper()

	tests := []struct {
		name   string
		config map[string]interface{}
		want   int
	}{
		{
			name:   "no policies",
			config: map[string]interface{}{},
			want:   0,
		},
		{
			name: "with managed policies",
			config: map[string]interface{}{
				"managed_policy_arns": []interface{}{
					"arn:aws:iam::aws:policy/AdministratorAccess",
					"arn:aws:iam::aws:policy/ReadOnlyAccess",
				},
			},
			want: 2,
		},
		{
			name: "empty policies",
			config: map[string]interface{}{
				"managed_policy_arns": []interface{}{},
			},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := &resource.AWSResource{
				ID:     "test",
				Type:   resource.TypeIAMRole,
				Name:   "test",
				Config: tt.config,
			}
			got := m.extractAttachedPolicies(res)
			if len(got) != tt.want {
				t.Errorf("extractAttachedPolicies() returned %d policies, want %d", len(got), tt.want)
			}
		})
	}
}

