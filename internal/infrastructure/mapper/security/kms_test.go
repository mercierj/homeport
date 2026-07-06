package security

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestKMSConformanceManagedAToZ(t *testing.T) {
	result, err := NewKMSMapper().Map(context.Background(), managedKMSFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated KMS migration", result.ManualSteps)
	}
	if result.DockerService.Image != "hashicorp/vault:1.15" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Vault Transit target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/vault/vault.hcl", "config/kms/app-change.env", "config/kms/reencrypt-plan.json"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/kms/app-change.env"])
	for _, want := range []string{"SOURCE_KEY_ID=key-1", "TARGET_TRANSIT_KEY=key-1", "APP_CHANGE_MODE=adapter", "AWS_ENDPOINT_URL_KMS=http://homeport:8080/api/v1/compat/aws/kms"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"scripts/kms-export.sh", "scripts/vault/setup-transit.sh", "scripts/kms-reencrypt.sh", "scripts/kms-backup.sh", "scripts/vault/test-transit.sh", "scripts/kms-cutover.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-kms-metadata":              domainrunbook.StepTypeCommand,
		"setup-vault-transit":              domainrunbook.StepTypeCommand,
		"reencrypt-kms-ciphertexts":        domainrunbook.StepTypeCommand,
		"validate-vault-transit-roundtrip": domainrunbook.StepTypeCommand,
		"backup-kms-vault":                 domainrunbook.StepTypeCommand,
		"cutover-kms-adapter":              domainrunbook.StepTypeAPICall,
		"rollback-kms-source-authority":    domainrunbook.StepTypeRollback,
	} {
		if !hasKMSRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestKMSKeyMaterialExportStatusClassifiesManagedKeysImpossible(t *testing.T) {
	got := KMSKeyMaterialExportStatus(&resource.AWSResource{
		ID:   "key-1",
		Type: resource.TypeKMSKey,
		Config: map[string]interface{}{
			"origin": "AWS_KMS",
		},
	})

	if got.Status != "impossible" {
		t.Fatalf("Status = %q, want impossible", got.Status)
	}
	if !strings.Contains(got.Reason, "non-exportable") {
		t.Fatalf("Reason = %q, want non-exportable explanation", got.Reason)
	}
}

func TestSecurityRunbookFixtureCoversSecretsAndKMS(t *testing.T) {
	fixture := []struct {
		name   string
		mapper interface {
			Map(context.Context, *resource.AWSResource) (*mapper.MappingResult, error)
		}
		resource *resource.AWSResource
		kind     string
	}{
		{
			name:   "secretsmanager",
			mapper: NewSecretsManagerMapper(),
			resource: &resource.AWSResource{
				ID:     "secret-1",
				Type:   resource.TypeSecretsManager,
				Name:   "app/db",
				Config: map[string]interface{}{"name": "app/db"},
			},
			kind: "secrets",
		},
		{
			name:   "kms",
			mapper: NewKMSMapper(),
			resource: &resource.AWSResource{
				ID:   "key-1",
				Type: resource.TypeKMSKey,
				Name: "app-key",
				Config: map[string]interface{}{
					"key_id":    "key-1",
					"key_usage": "ENCRYPT_DECRYPT",
					"key_spec":  "SYMMETRIC_DEFAULT",
				},
			},
			kind: "kms",
		},
	}

	for _, tt := range fixture {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.mapper.Map(context.Background(), tt.resource)
			if err != nil {
				t.Fatalf("Map() error = %v", err)
			}
			if !hasSecurityRunbookKind(result, tt.kind) {
				t.Fatalf("missing %s runbook steps: %#v", tt.kind, result.RunbookSteps)
			}
			if tt.kind == "kms" && !hasWarningContaining(result, "Key material export status: impossible") {
				t.Fatalf("missing impossible export warning: %#v", result.Warnings)
			}
		})
	}
}

func hasSecurityRunbookKind(result *mapper.MappingResult, kind string) bool {
	for _, step := range result.RunbookSteps {
		if step.Metadata["kind"] == kind {
			return true
		}
	}
	return false
}

func managedKMSFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:     "key-1",
		Type:   resource.TypeKMSKey,
		Name:   "orders-key",
		Region: "eu-west-1",
		Config: map[string]interface{}{
			"key_id":       "key-1",
			"key_usage":    "ENCRYPT_DECRYPT",
			"key_spec":     "SYMMETRIC_DEFAULT",
			"origin":       "AWS_KMS",
			"enabled":      true,
			"multi_region": true,
			"aliases":      []string{"alias/orders"},
		},
	}
}

func hasKMSRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}

func hasWarningContaining(result *mapper.MappingResult, text string) bool {
	for _, warning := range result.Warnings {
		if strings.Contains(warning, text) {
			return true
		}
	}
	return false
}
