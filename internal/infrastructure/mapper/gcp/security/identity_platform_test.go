package security

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestNewIdentityPlatformMapper(t *testing.T) {
	m := NewIdentityPlatformMapper()
	if m == nil {
		t.Fatal("NewIdentityPlatformMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeIdentityPlatform {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeIdentityPlatform)
	}
}

func TestIdentityPlatformMapper_ResourceType(t *testing.T) {
	m := NewIdentityPlatformMapper()
	got := m.ResourceType()
	want := resource.TypeIdentityPlatform

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestIdentityPlatformMapper_Dependencies(t *testing.T) {
	m := NewIdentityPlatformMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestIdentityPlatformMapper_Validate(t *testing.T) {
	m := NewIdentityPlatformMapper()

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
				Type: resource.TypeIdentityPlatform,
				Name: "test-project",
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

func TestIdentityPlatformMapper_Map(t *testing.T) {
	m := NewIdentityPlatformMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Identity Platform config",
			res: &resource.AWSResource{
				ID:   "my-project",
				Type: resource.TypeIdentityPlatform,
				Name: "my-project",
				Config: map[string]interface{}{
					"project": "my-project",
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
				if result.DockerService.Image != "quay.io/keycloak/keycloak:23.0" {
					t.Errorf("Expected Keycloak image, got %s", result.DockerService.Image)
				}
			},
		},
		{
			name: "Identity Platform with email sign-in",
			res: &resource.AWSResource{
				ID:   "my-project",
				Type: resource.TypeIdentityPlatform,
				Name: "my-project",
				Config: map[string]interface{}{
					"project": "my-project",
					"sign_in": map[string]interface{}{
						"email": map[string]interface{}{
							"enabled": true,
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about email sign-in
				hasEmailWarning := false
				for _, w := range result.Warnings {
					if containsIdentity(w, "Email sign-in") {
						hasEmailWarning = true
						break
					}
				}
				if !hasEmailWarning {
					t.Error("Expected warning about email sign-in")
				}
			},
		},
		{
			name: "Identity Platform with phone sign-in",
			res: &resource.AWSResource{
				ID:   "my-project",
				Type: resource.TypeIdentityPlatform,
				Name: "my-project",
				Config: map[string]interface{}{
					"project": "my-project",
					"sign_in": map[string]interface{}{
						"phone_number": map[string]interface{}{
							"enabled": true,
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about phone sign-in
				hasPhoneWarning := false
				for _, w := range result.Warnings {
					if containsIdentity(w, "Phone sign-in") {
						hasPhoneWarning = true
						break
					}
				}
				if !hasPhoneWarning {
					t.Error("Expected warning about phone sign-in")
				}
			},
		},
		{
			name: "Identity Platform with MFA enabled",
			res: &resource.AWSResource{
				ID:   "my-project",
				Type: resource.TypeIdentityPlatform,
				Name: "my-project",
				Config: map[string]interface{}{
					"project": "my-project",
					"mfa": map[string]interface{}{
						"state": "ENABLED",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about MFA
				hasMFAWarning := false
				for _, w := range result.Warnings {
					if containsIdentity(w, "MFA") {
						hasMFAWarning = true
						break
					}
				}
				if !hasMFAWarning {
					t.Error("Expected warning about MFA")
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

func TestIdentityPlatformMapper_generateRealmConfig(t *testing.T) {
	m := NewIdentityPlatformMapper()

	res := &resource.AWSResource{
		ID:   "my-project",
		Type: resource.TypeIdentityPlatform,
		Config: map[string]interface{}{
			"project": "my-project",
		},
	}

	config := m.generateRealmConfig(res, "my-project")

	// Check that config is valid JSON-like content
	if config == "" {
		t.Error("generateRealmConfig returned empty string")
	}
	if !containsIdentity(config, "my-project") {
		t.Error("Realm config should contain project ID")
	}
}

// containsIdentity is a helper to check if a string contains a substring
func containsIdentity(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
