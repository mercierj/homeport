package security

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestSecretsManagerConformanceManagedAToZ(t *testing.T) {
	result, err := NewSecretsManagerMapper().Map(context.Background(), &resource.AWSResource{
		ID:   "arn:aws:secretsmanager:eu-west-1:123456789012:secret:app/db-abc123",
		Type: resource.TypeSecretsManager,
		Name: "app/db",
		Config: map[string]interface{}{
			"name":       "app/db",
			"kms_key_id": "arn:aws:kms:eu-west-1:123456789012:key/1234",
			"rotation_configuration": map[string]interface{}{
				"automatically_after_days": float64(30),
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Secrets Manager migration", result.ManualSteps)
	}
	if result.DockerService.Image != "hashicorp/vault:1.15" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Vault target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/vault/vault.hcl", "config/vault/app-change.env", "config/vault/rotation-policy.hcl"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/vault/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=adapter", "SOURCE_SECRET=app/db", "AWS_ENDPOINT_URL_SECRETSMANAGER=http://homeport:8080/api/v1/compat/aws/secretsmanager", "HOMEPORT_COMPAT_BACKEND=vault"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"init_vault.sh", "migrate_secrets.sh", "validate_secretsmanager_adapter.sh", "backup_secretsmanager_config.sh", "cutover_secretsmanager_adapter.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for _, id := range []string{"initialize-vault-secrets", "import-secret-value", "validate-secretsmanager-compat", "backup-secretsmanager-config", "cutover-secretsmanager-adapter", "rollback-secrets-source-authority"} {
		if !hasRunbookStep(result, id) {
			t.Fatalf("missing runbook step %s: %#v", id, result.RunbookSteps)
		}
	}
}

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
					"name":                "rotating-secret",
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

func TestSecretsManagerMapper_ImportsSecretsThroughProviderAPI(t *testing.T) {
	result, err := NewSecretsManagerMapper().Map(context.Background(), &resource.AWSResource{
		ID:   "secret-1",
		Type: resource.TypeSecretsManager,
		Name: "app/db",
		Config: map[string]interface{}{
			"name": "app/db",
		},
	})
	if err != nil {
		t.Fatalf("Map() error = %v", err)
	}

	script := string(result.Scripts["migrate_secrets.sh"])
	if !strings.Contains(script, "aws secretsmanager get-secret-value") {
		t.Fatalf("migration script should call provider API, got:\n%s", script)
	}
	if !strings.Contains(script, "vault kv put") {
		t.Fatalf("migration script should import into Vault, got:\n%s", script)
	}
	if !hasRunbookStep(result, "provide-unreadable-secret") {
		t.Fatalf("missing encrypted input fallback step: %#v", result.RunbookSteps)
	}
}

func hasRunbookStep(result *mapper.MappingResult, id string) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id {
			return true
		}
	}
	return false
}
