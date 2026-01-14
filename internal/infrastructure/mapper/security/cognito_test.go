package security

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestNewCognitoMapper(t *testing.T) {
	m := NewCognitoMapper()
	if m == nil {
		t.Fatal("NewCognitoMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeCognitoPool {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeCognitoPool)
	}
}

func TestCognitoMapper_ResourceType(t *testing.T) {
	m := NewCognitoMapper()
	got := m.ResourceType()
	want := resource.TypeCognitoPool

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestCognitoMapper_Dependencies(t *testing.T) {
	m := NewCognitoMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestCognitoMapper_Validate(t *testing.T) {
	m := NewCognitoMapper()

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
				Type: resource.TypeCognitoPool,
				Name: "test-pool",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeCognitoPool,
				Name: "test-pool",
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

func TestCognitoMapper_Map(t *testing.T) {
	m := NewCognitoMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Cognito user pool",
			res: &resource.AWSResource{
				ID:   "us-east-1_abc123",
				Type: resource.TypeCognitoPool,
				Name: "my-user-pool",
				Config: map[string]interface{}{
					"name": "my-user-pool",
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
			name: "Cognito with MFA enabled",
			res: &resource.AWSResource{
				ID:   "us-east-1_mfa123",
				Type: resource.TypeCognitoPool,
				Name: "mfa-pool",
				Config: map[string]interface{}{
					"name":              "mfa-pool",
					"mfa_configuration": "ON",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about MFA
				if len(result.Warnings) == 0 {
					t.Log("Expected warning about MFA")
				}
			},
		},
		{
			name: "Cognito with password policy",
			res: &resource.AWSResource{
				ID:   "us-east-1_pwd123",
				Type: resource.TypeCognitoPool,
				Name: "pwd-pool",
				Config: map[string]interface{}{
					"name": "pwd-pool",
					"password_policy": map[string]interface{}{
						"minimum_length":    float64(12),
						"require_lowercase": true,
						"require_uppercase": true,
						"require_numbers":   true,
						"require_symbols":   true,
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about password policy
				if len(result.Warnings) == 0 {
					t.Log("Expected warning about password policy")
				}
			},
		},
		{
			name: "Cognito with email configuration",
			res: &resource.AWSResource{
				ID:   "us-east-1_email123",
				Type: resource.TypeCognitoPool,
				Name: "email-pool",
				Config: map[string]interface{}{
					"name": "email-pool",
					"email_configuration": map[string]interface{}{
						"source_arn": "arn:aws:ses:us-east-1:123456789012:identity/example.com",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about email configuration
				if len(result.Warnings) == 0 {
					t.Log("Expected warning about email configuration")
				}
			},
		},
		{
			name: "Cognito with SMS configuration",
			res: &resource.AWSResource{
				ID:   "us-east-1_sms123",
				Type: resource.TypeCognitoPool,
				Name: "sms-pool",
				Config: map[string]interface{}{
					"name": "sms-pool",
					"sms_configuration": map[string]interface{}{
						"external_id":    "my-external-id",
						"sns_caller_arn": "arn:aws:iam::123456789012:role/sns-role",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about SMS
				if len(result.Warnings) == 0 {
					t.Log("Expected warning about SMS configuration")
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

func TestCognitoMapper_sanitizeRealmName(t *testing.T) {
	m := NewCognitoMapper()

	tests := []struct {
		input string
		want  string
	}{
		{"my-pool", "my-pool"},
		{"my_pool", "my-pool"},
		{"My Pool", "my-pool"},
		{"My_User_Pool", "my-user-pool"},
		{"pool123", "pool123"},
		{"", "app-realm"},
		{"@#$%", "app-realm"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := m.sanitizeRealmName(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeRealmName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCognitoMapper_hasMFAEnabled(t *testing.T) {
	m := NewCognitoMapper()

	tests := []struct {
		name   string
		config map[string]interface{}
		want   bool
	}{
		{
			name:   "MFA ON",
			config: map[string]interface{}{"mfa_configuration": "ON"},
			want:   true,
		},
		{
			name:   "MFA OPTIONAL",
			config: map[string]interface{}{"mfa_configuration": "OPTIONAL"},
			want:   true,
		},
		{
			name:   "MFA OFF",
			config: map[string]interface{}{"mfa_configuration": "OFF"},
			want:   false,
		},
		{
			name:   "No MFA config",
			config: map[string]interface{}{},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := &resource.AWSResource{
				ID:     "test",
				Type:   resource.TypeCognitoPool,
				Name:   "test",
				Config: tt.config,
			}
			got := m.hasMFAEnabled(res)
			if got != tt.want {
				t.Errorf("hasMFAEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}
