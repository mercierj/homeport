package security

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestNewIAMMapper(t *testing.T) {
	m := NewIAMMapper()
	if m == nil {
		t.Fatal("NewIAMMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeGCPIAM {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeGCPIAM)
	}
}

func TestIAMMapper_ResourceType(t *testing.T) {
	m := NewIAMMapper()
	got := m.ResourceType()
	want := resource.TypeGCPIAM

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
				Type: resource.TypeGCSBucket,
				Name: "test",
			},
			wantErr: true,
		},
		{
			name: "valid resource",
			res: &resource.AWSResource{
				ID:   "test-id",
				Type: resource.TypeGCPIAM,
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
			name: "basic IAM binding with owner role",
			res: &resource.AWSResource{
				ID:   "my-project/roles/owner/user:admin@example.com",
				Type: resource.TypeGCPIAM,
				Name: "my-project-owner",
				Config: map[string]interface{}{
					"project": "my-project",
					"role":    "roles/owner",
					"member":  "user:admin@example.com",
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
				// Check labels
				if result.DockerService.Labels["homeport.role"] != "roles/owner" {
					t.Errorf("Expected role label, got %s", result.DockerService.Labels["homeport.role"])
				}
			},
		},
		{
			name: "IAM binding with viewer role",
			res: &resource.AWSResource{
				ID:   "my-project/roles/viewer/user:viewer@example.com",
				Type: resource.TypeGCPIAM,
				Name: "my-project-viewer",
				Config: map[string]interface{}{
					"project": "my-project",
					"role":    "roles/viewer",
					"member":  "user:viewer@example.com",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have config files generated
				if len(result.Configs) == 0 {
					t.Error("Expected config files to be generated")
				}
			},
		},
		{
			name: "IAM binding with storage admin role",
			res: &resource.AWSResource{
				ID:   "my-project/roles/storage.admin/serviceAccount:sa@project.iam.gserviceaccount.com",
				Type: resource.TypeGCPIAM,
				Name: "my-project-storage-admin",
				Config: map[string]interface{}{
					"project": "my-project",
					"role":    "roles/storage.admin",
					"member":  "serviceAccount:sa@project.iam.gserviceaccount.com",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have scripts generated
				if len(result.Scripts) == 0 {
					t.Error("Expected scripts to be generated")
				}
			},
		},
		{
			name: "IAM binding with condition",
			res: &resource.AWSResource{
				ID:   "my-project/roles/editor/user:conditional@example.com",
				Type: resource.TypeGCPIAM,
				Name: "my-project-conditional",
				Config: map[string]interface{}{
					"project": "my-project",
					"role":    "roles/editor",
					"member":  "user:conditional@example.com",
					"condition": map[string]interface{}{
						"title":       "expires_after_2024",
						"description": "Expires at end of 2024",
						"expression":  "request.time < timestamp(\"2025-01-01T00:00:00Z\")",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about condition
				hasConditionWarning := false
				for _, w := range result.Warnings {
					if strings.Contains(w, "condition") {
						hasConditionWarning = true
						break
					}
				}
				if !hasConditionWarning {
					t.Error("Expected warning about IAM condition")
				}
			},
		},
		{
			name: "IAM binding with cloudfunctions role",
			res: &resource.AWSResource{
				ID:   "my-project/roles/cloudfunctions.invoker/allUsers",
				Type: resource.TypeGCPIAM,
				Name: "my-project-functions-invoker",
				Config: map[string]interface{}{
					"project": "my-project",
					"role":    "roles/cloudfunctions.invoker",
					"member":  "allUsers",
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

func TestIAMMapper_mapGCPRoleToKeycloakRoles(t *testing.T) {
	m := NewIAMMapper()

	tests := []struct {
		gcpRole       string
		expectRoles   []string
		unexpectRoles []string
	}{
		{
			gcpRole:       "roles/owner",
			expectRoles:   []string{"admin", "manage-users"},
			unexpectRoles: []string{},
		},
		{
			gcpRole:       "roles/editor",
			expectRoles:   []string{"user", "manage-clients"},
			unexpectRoles: []string{"admin"},
		},
		{
			gcpRole:       "roles/viewer",
			expectRoles:   []string{"view-users", "view-clients"},
			unexpectRoles: []string{"admin", "manage-users"},
		},
		{
			gcpRole:       "roles/storage.admin",
			expectRoles:   []string{"storage-access"},
			unexpectRoles: []string{},
		},
		{
			gcpRole:       "roles/cloudsql.client",
			expectRoles:   []string{"database-access"},
			unexpectRoles: []string{},
		},
		{
			gcpRole:       "roles/pubsub.publisher",
			expectRoles:   []string{"messaging-access"},
			unexpectRoles: []string{},
		},
		{
			gcpRole:       "roles/secretmanager.secretAccessor",
			expectRoles:   []string{"secrets-access"},
			unexpectRoles: []string{},
		},
		{
			gcpRole:       "roles/cloudfunctions.invoker",
			expectRoles:   []string{"compute-access", "service"},
			unexpectRoles: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.gcpRole, func(t *testing.T) {
			roles := m.mapGCPRoleToKeycloakRoles(tt.gcpRole)

			// Check expected roles are present
			for _, expected := range tt.expectRoles {
				found := false
				for _, r := range roles {
					if r == expected {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected role %s not found in %v for GCP role %s", expected, roles, tt.gcpRole)
				}
			}

			// Check unexpected roles are absent
			for _, unexpected := range tt.unexpectRoles {
				for _, r := range roles {
					if r == unexpected {
						t.Errorf("Unexpected role %s found in %v for GCP role %s", unexpected, roles, tt.gcpRole)
					}
				}
			}
		})
	}
}

func TestIAMMapper_extractPermissionsFromGCPRole(t *testing.T) {
	m := NewIAMMapper()

	tests := []struct {
		gcpRole         string
		expectPerms     []string
		unexpectPerms   []string
	}{
		{
			gcpRole:       "roles/storage.admin",
			expectPerms:   []string{"storage:admin", "storage:read", "storage:write"},
			unexpectPerms: []string{},
		},
		{
			gcpRole:       "roles/storage.objectViewer",
			expectPerms:   []string{"storage:read"},
			unexpectPerms: []string{"storage:write", "storage:admin"},
		},
		{
			gcpRole:       "roles/cloudsql.admin",
			expectPerms:   []string{"database:admin", "database:read"},
			unexpectPerms: []string{},
		},
		{
			gcpRole:       "roles/pubsub.publisher",
			expectPerms:   []string{"messaging:publish"},
			unexpectPerms: []string{"messaging:subscribe"},
		},
		{
			gcpRole:       "roles/owner",
			expectPerms:   []string{"admin:all"},
			unexpectPerms: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.gcpRole, func(t *testing.T) {
			perms := m.extractPermissionsFromGCPRole(tt.gcpRole)

			// Check expected permissions are present
			for _, expected := range tt.expectPerms {
				found := false
				for _, p := range perms {
					if p == expected {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected permission %s not found in %v for GCP role %s", expected, perms, tt.gcpRole)
				}
			}

			// Check unexpected permissions are absent
			for _, unexpected := range tt.unexpectPerms {
				for _, p := range perms {
					if p == unexpected {
						t.Errorf("Unexpected permission %s found in %v for GCP role %s", unexpected, perms, tt.gcpRole)
					}
				}
			}
		})
	}
}

func TestIAMMapper_generateRealmConfig(t *testing.T) {
	m := NewIAMMapper()

	config := m.generateRealmConfig("my-project", "roles/owner")

	// Check that config is valid JSON-like content
	if config == "" {
		t.Error("generateRealmConfig returned empty string")
	}
	if !strings.Contains(config, "my-project") {
		t.Error("Realm config should contain project ID")
	}
	if !strings.Contains(config, "gcp-my-project") {
		t.Error("Realm config should contain gcp-prefixed realm name")
	}
	if !strings.Contains(config, "roles/owner") {
		t.Error("Realm config should contain original GCP role")
	}
}

func TestIAMMapper_generateSetupScript(t *testing.T) {
	m := NewIAMMapper()

	script := m.generateSetupScript("my-project")

	if script == "" {
		t.Error("generateSetupScript returned empty string")
	}
	if !strings.Contains(script, "gcp-my-project") {
		t.Error("Setup script should contain realm name")
	}
	if !strings.Contains(script, "curl") {
		t.Error("Setup script should contain curl commands")
	}
}

func TestIAMMapper_generateMigrationScript(t *testing.T) {
	m := NewIAMMapper()

	script := m.generateMigrationScript("my-project", "roles/editor", "user:test@example.com")

	if script == "" {
		t.Error("generateMigrationScript returned empty string")
	}
	if !strings.Contains(script, "my-project") {
		t.Error("Migration script should contain project ID")
	}
	if !strings.Contains(script, "roles/editor") {
		t.Error("Migration script should contain role")
	}
	if !strings.Contains(script, "user:test@example.com") {
		t.Error("Migration script should contain member")
	}
	if !strings.Contains(script, "gcloud") {
		t.Error("Migration script should contain gcloud commands")
	}
}
