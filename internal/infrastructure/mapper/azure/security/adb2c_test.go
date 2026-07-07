package security

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestNewADB2CMapper(t *testing.T) {
	m := NewADB2CMapper()
	if m == nil {
		t.Fatal("NewADB2CMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeAzureADB2C {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeAzureADB2C)
	}
}

func TestADB2CMapper_ResourceType(t *testing.T) {
	m := NewADB2CMapper()
	got := m.ResourceType()
	want := resource.TypeAzureADB2C

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestADB2CConformanceManagedAToZ(t *testing.T) {
	result, err := NewADB2CMapper().Map(context.Background(), managedADB2CFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Azure AD B2C migration", result.ManualSteps)
	}
	if result.DockerService.Image != "quay.io/keycloak/keycloak:23.0" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Keycloak target: %#v", result.DockerService)
	}
	if !hasADB2CService(result, "postgres:16-alpine") {
		t.Fatalf("missing generated Postgres service: %#v", result.AdditionalServices)
	}
	for _, file := range []string{"config/keycloak/realm.json", "config/keycloak/postgres-service.yml", "config/adb2c/app-change.env", "config/adb2c/generated-client.patch", "config/adb2c/user-import-plan.json"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/adb2c/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_ADB2C_DIRECTORY=checkout.b2clogin.com", "KEYCLOAK_REALM=checkout-b2clogin-com"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"setup_keycloak.sh", "migrate_user_flows.sh", "export_adb2c_users.sh", "validate_adb2c_keycloak.sh", "backup_adb2c_config.sh", "cutover_adb2c_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-adb2c-users":    domainrunbook.StepTypeCommand,
		"setup-keycloak-realm":  domainrunbook.StepTypeCommand,
		"migrate-adb2c-flows":   domainrunbook.StepTypeCommand,
		"validate-adb2c-realm":  domainrunbook.StepTypeCommand,
		"backup-adb2c-config":   domainrunbook.StepTypeCommand,
		"cutover-adb2c-clients": domainrunbook.StepTypeAPICall,
		"rollback-adb2c-source": domainrunbook.StepTypeRollback,
	} {
		if !hasADB2CRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func managedADB2CFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "/subscriptions/demo/resourceGroups/rg/providers/Microsoft.AzureActiveDirectory/b2cDirectories/checkout",
		Type: resource.TypeAzureADB2C,
		Name: "checkout",
		Config: map[string]interface{}{
			"domain_name": "checkout.b2clogin.com",
			"tenant_id":   "tenant-123",
		},
	}
}

func hasADB2CRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}

func hasADB2CService(result *mapper.MappingResult, image string) bool {
	for _, svc := range result.AdditionalServices {
		if svc.Image == image {
			return true
		}
	}
	return false
}

func TestADB2CMapper_Dependencies(t *testing.T) {
	m := NewADB2CMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestADB2CMapper_Validate(t *testing.T) {
	m := NewADB2CMapper()

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
				Type: resource.TypeEC2Instance,
				Name: "test",
			},
			wantErr: true,
		},
		{
			name: "valid resource",
			res: &resource.AWSResource{
				ID:   "test-id",
				Type: resource.TypeAzureADB2C,
				Name: "test-b2c",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeAzureADB2C,
				Name: "test-b2c",
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

func TestADB2CMapper_Map(t *testing.T) {
	m := NewADB2CMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Azure AD B2C directory",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.AzureActiveDirectory/b2cDirectories/myb2c",
				Type: resource.TypeAzureADB2C,
				Name: "myb2c",
				Config: map[string]interface{}{
					"domain_name": "myb2c.onmicrosoft.com",
					"tenant_id":   "tenant-123",
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
				if result.DockerService.HealthCheck == nil {
					t.Error("HealthCheck is nil")
				}
			},
		},
		{
			name: "Azure AD B2C with tenant",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.AzureActiveDirectory/b2cDirectories/myb2c",
				Type: resource.TypeAzureADB2C,
				Name: "myb2c",
				Config: map[string]interface{}{
					"domain_name": "myapp.b2clogin.com",
					"tenant_id":   "my-tenant-id-123",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if !result.HasWarnings() {
					t.Error("Expected warnings for B2C configuration")
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
				Type: resource.TypeEC2Instance,
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
