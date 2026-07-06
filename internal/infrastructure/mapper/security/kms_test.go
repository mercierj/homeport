package security

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

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

func hasWarningContaining(result *mapper.MappingResult, text string) bool {
	for _, warning := range result.Warnings {
		if strings.Contains(warning, text) {
			return true
		}
	}
	return false
}
