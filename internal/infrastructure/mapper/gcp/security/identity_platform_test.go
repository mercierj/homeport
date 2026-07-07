package security

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestIdentityPlatformConformanceManagedAToZ(t *testing.T) {
	result, err := NewIdentityPlatformMapper().Map(context.Background(), managedIdentityPlatformFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Identity Platform migration", result.ManualSteps)
	}
	if result.DockerService.Image != "quay.io/keycloak/keycloak:23.0" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Keycloak target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/keycloak/realm.json", "config/identity-platform/app-change.env", "config/identity-platform/migration.env"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/identity-platform/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_IDENTITY_PLATFORM_PROJECT=demo", "TARGET_AUTH_PROVIDER=keycloak"} {
		if !containsIdentity(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"setup_keycloak.sh", "migrate_users.sh", "export_identity_platform_users.sh", "import_identity_platform_keycloak.sh", "validate_identity_platform_keycloak.sh", "backup_identity_platform_config.sh", "cutover_identity_platform_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-identity-platform-users":      domainrunbook.StepTypeCommand,
		"provision-keycloak-identity":         domainrunbook.StepTypeCommand,
		"migrate-identity-platform-users":     domainrunbook.StepTypeCommand,
		"validate-identity-platform-keycloak": domainrunbook.StepTypeCommand,
		"backup-identity-platform-config":     domainrunbook.StepTypeCommand,
		"cutover-identity-platform-clients":   domainrunbook.StepTypeAPICall,
		"rollback-identity-platform-source":   domainrunbook.StepTypeRollback,
	} {
		if !hasIdentityPlatformRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

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

func managedIdentityPlatformFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "projects/demo/identity-platform",
		Type: resource.TypeIdentityPlatform,
		Name: "demo",
		Config: map[string]interface{}{
			"project": "demo",
			"sign_in": map[string]interface{}{
				"email": map[string]interface{}{"enabled": true},
			},
			"mfa": map[string]interface{}{"state": "ENABLED"},
		},
	}
}

func hasIdentityPlatformRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
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
