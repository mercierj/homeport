package detector

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/resource"
	"github.com/homeport/homeport/internal/domain/secrets"
)

func TestAWSDetector_RDSWithSecretsManager(t *testing.T) {
	detector := NewAWSDetector()

	res := resource.NewAWSResource("db-instance-1", "prod-db", resource.TypeRDSInstance)
	res.Config["engine"] = "postgres"
	res.Config["master_user_secret_arn"] = "arn:aws:secretsmanager:us-east-1:123456789012:secret:rds/prod-db-abc123"

	detected, err := detector.Detect(context.Background(), res)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(detected) != 1 {
		t.Fatalf("expected 1 secret, got %d", len(detected))
	}

	secret := detected[0]
	if secret.Source != secrets.SourceAWSSecretsManager {
		t.Errorf("expected source %s, got %s", secrets.SourceAWSSecretsManager, secret.Source)
	}
	if secret.Key != "arn:aws:secretsmanager:us-east-1:123456789012:secret:rds/prod-db-abc123" {
		t.Errorf("unexpected key: %s", secret.Key)
	}
	if !secret.Required {
		t.Error("expected secret to be required")
	}
}

func TestAWSDetector_RDSManualPassword(t *testing.T) {
	detector := NewAWSDetector()

	res := resource.NewAWSResource("db-instance-1", "dev-db", resource.TypeRDSInstance)
	res.Config["engine"] = "mysql"

	detected, err := detector.Detect(context.Background(), res)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(detected) != 1 {
		t.Fatalf("expected 1 secret, got %d", len(detected))
	}

	secret := detected[0]
	if secret.Source != secrets.SourceManual {
		t.Errorf("expected source %s, got %s", secrets.SourceManual, secret.Source)
	}
	if secret.Key != "" {
		t.Errorf("expected empty key for manual secret, got %s", secret.Key)
	}
}

func TestAWSDetector_LambdaEnvVars(t *testing.T) {
	detector := NewAWSDetector()

	res := resource.NewAWSResource("lambda-1", "api-handler", resource.TypeLambdaFunction)
	res.Config["environment"] = map[string]interface{}{
		"variables": map[string]interface{}{
			"DATABASE_URL":     "postgres://...",
			"API_KEY":          "secret-key",
			"LOG_LEVEL":        "info",
			"SESSION_SECRET":   "session-secret",
			"NORMAL_VAR":       "not-sensitive",
		},
	}

	detected, err := detector.Detect(context.Background(), res)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should detect API_KEY and SESSION_SECRET as sensitive
	if len(detected) < 2 {
		t.Fatalf("expected at least 2 secrets, got %d", len(detected))
	}

	// Check that detected secrets have correct source
	for _, secret := range detected {
		if secret.Source != secrets.SourceManual {
			t.Errorf("expected manual source for Lambda env vars, got %s", secret.Source)
		}
	}
}

func TestAWSDetector_ECSWithSecretsManager(t *testing.T) {
	detector := NewAWSDetector()

	res := resource.NewAWSResource("task-def-1", "web-app", resource.TypeECSTaskDef)
	res.Config["container_definitions"] = []interface{}{
		map[string]interface{}{
			"name": "web",
			"secrets": []interface{}{
				map[string]interface{}{
					"name":      "DB_PASSWORD",
					"valueFrom": "arn:aws:secretsmanager:us-east-1:123:secret:db-password-xyz",
				},
			},
			"environment": []interface{}{
				map[string]interface{}{
					"name":  "PORT",
					"value": "8080",
				},
				map[string]interface{}{
					"name":  "API_SECRET",
					"value": "xxx",
				},
			},
		},
	}

	detected, err := detector.Detect(context.Background(), res)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(detected) < 2 {
		t.Fatalf("expected at least 2 secrets (DB_PASSWORD from Secrets Manager, API_SECRET from env), got %d", len(detected))
	}

	// Find the Secrets Manager secret
	var smSecret *secrets.DetectedSecret
	for _, s := range detected {
		if s.Source == secrets.SourceAWSSecretsManager {
			smSecret = s
			break
		}
	}

	if smSecret == nil {
		t.Error("expected to find a Secrets Manager secret")
	} else {
		if smSecret.Key != "arn:aws:secretsmanager:us-east-1:123:secret:db-password-xyz" {
			t.Errorf("unexpected secret key: %s", smSecret.Key)
		}
	}
}

func TestAWSDetector_ElastiCacheWithAuth(t *testing.T) {
	detector := NewAWSDetector()

	res := resource.NewAWSResource("cache-1", "redis-cluster", resource.TypeElastiCache)
	res.Config["engine"] = "redis"
	res.Config["auth_token_enabled"] = true
	res.Config["transit_encryption_enabled"] = true

	detected, err := detector.Detect(context.Background(), res)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(detected) != 1 {
		t.Fatalf("expected 1 secret, got %d", len(detected))
	}

	secret := detected[0]
	if secret.Source != secrets.SourceManual {
		t.Errorf("expected source %s, got %s", secrets.SourceManual, secret.Source)
	}
	if secret.Type != secrets.TypePassword {
		t.Errorf("expected type %s, got %s", secrets.TypePassword, secret.Type)
	}
}

func TestAWSDetector_SecretsManagerResource(t *testing.T) {
	detector := NewAWSDetector()

	res := resource.NewAWSResource("secret-1", "prod/db-credentials", resource.TypeSecretsManager)
	res.ARN = "arn:aws:secretsmanager:us-east-1:123:secret:prod/db-credentials-xyz"
	res.Config["name"] = "prod/db-credentials"
	res.Config["description"] = "Production database credentials"

	detected, err := detector.Detect(context.Background(), res)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(detected) != 1 {
		t.Fatalf("expected 1 secret, got %d", len(detected))
	}

	secret := detected[0]
	if secret.Source != secrets.SourceAWSSecretsManager {
		t.Errorf("expected source %s, got %s", secrets.SourceAWSSecretsManager, secret.Source)
	}
	if secret.Key != res.ARN {
		t.Errorf("expected key to be ARN, got %s", secret.Key)
	}
}

func TestDefaultRegistry_DetectAll(t *testing.T) {
	registry := NewDefaultRegistry()

	resources := []*resource.AWSResource{
		func() *resource.AWSResource {
			res := resource.NewAWSResource("rds-1", "prod-db", resource.TypeRDSInstance)
			res.Config["engine"] = "postgres"
			res.Config["master_user_secret_arn"] = "arn:aws:secretsmanager:us-east-1:123:secret:rds-secret"
			return res
		}(),
		func() *resource.AWSResource {
			res := resource.NewAWSResource("lambda-1", "api-handler", resource.TypeLambdaFunction)
			res.Config["environment"] = map[string]interface{}{
				"variables": map[string]interface{}{
					"API_KEY": "xxx",
				},
			}
			return res
		}(),
	}

	manifest, err := registry.DetectAll(context.Background(), resources)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(manifest.Secrets) < 2 {
		t.Errorf("expected at least 2 secrets, got %d", len(manifest.Secrets))
	}

	// Verify manifest has correct metadata
	if manifest.Version != secrets.SecretsManifestVersion {
		t.Errorf("expected version %s, got %s", secrets.SecretsManifestVersion, manifest.Version)
	}
}

func TestDefaultRegistry_Deduplication(t *testing.T) {
	registry := NewDefaultRegistry()

	// Two resources using the same secret
	secretARN := "arn:aws:secretsmanager:us-east-1:123:secret:shared-secret"
	resources := []*resource.AWSResource{
		func() *resource.AWSResource {
			res := resource.NewAWSResource("rds-1", "db-1", resource.TypeRDSInstance)
			res.Config["engine"] = "postgres"
			res.Config["master_user_secret_arn"] = secretARN
			return res
		}(),
		func() *resource.AWSResource {
			res := resource.NewAWSResource("rds-2", "db-2", resource.TypeRDSInstance)
			res.Config["engine"] = "postgres"
			res.Config["master_user_secret_arn"] = secretARN
			return res
		}(),
	}

	manifest, err := registry.DetectAll(context.Background(), resources)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should only have 1 secret due to deduplication
	if len(manifest.Secrets) != 1 {
		t.Errorf("expected 1 secret (deduplicated), got %d", len(manifest.Secrets))
	}

	// The UsedBy should contain both resources
	if len(manifest.Secrets[0].UsedBy) != 2 {
		t.Errorf("expected UsedBy to have 2 entries, got %d", len(manifest.Secrets[0].UsedBy))
	}
}
