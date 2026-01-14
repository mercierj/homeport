package secrets

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeport/homeport/internal/domain/secrets"
)

func TestParseEnvFile(t *testing.T) {
	content := `
# Comment line
DATABASE_PASSWORD=secret123
API_KEY="quoted value"
SINGLE_QUOTED='single quoted'
EMPTY_VALUE=
SPACED_KEY = spaced value

# Another comment
MULTILINE="line1\nline2"
`

	env := parseEnvFile(content)

	tests := []struct {
		key      string
		expected string
	}{
		{"DATABASE_PASSWORD", "secret123"},
		{"API_KEY", "quoted value"},
		{"SINGLE_QUOTED", "single quoted"},
		{"EMPTY_VALUE", ""},
		{"SPACED_KEY", "spaced value"},
		{"MULTILINE", "line1\nline2"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got, ok := env[tt.key]
			if !ok && tt.expected != "" {
				t.Errorf("Key %q not found", tt.key)
				return
			}
			if got != tt.expected {
				t.Errorf("env[%q] = %q, want %q", tt.key, got, tt.expected)
			}
		})
	}
}

func TestResolver_ResolveFromEnv(t *testing.T) {
	// Set up test environment variable
	_ = os.Setenv("HOMEPORT_SECRET_TEST_VAR", "env_value")
	defer func() { _ = os.Unsetenv("HOMEPORT_SECRET_TEST_VAR") }()

	opts := DefaultResolverOptions()
	resolver := NewResolver(opts)

	ref := secrets.NewSecretReference("TEST_VAR", secrets.SourceEnv).
		WithKey("TEST_VAR")

	// Test resolution from env
	value, ok := resolver.resolveFromEnv(ref)
	if !ok {
		t.Error("resolveFromEnv should find the env variable")
	}
	if value != "env_value" {
		t.Errorf("value = %q, want %q", value, "env_value")
	}
}

func TestResolver_ResolveFromFile(t *testing.T) {
	// Create a temporary secrets file
	tmpDir := t.TempDir()
	secretsFile := filepath.Join(tmpDir, ".env.secrets")

	content := `DATABASE_PASSWORD=file_secret
API_KEY=api_secret_value
`
	if err := os.WriteFile(secretsFile, []byte(content), 0600); err != nil {
		t.Fatalf("Failed to write secrets file: %v", err)
	}

	opts := &ResolverOptions{
		SecretsFilePath: secretsFile,
		EnvPrefix:       "HOMEPORT_SECRET_",
	}
	resolver := NewResolver(opts)

	// Test resolution from file
	ref := secrets.NewSecretReference("DATABASE_PASSWORD", secrets.SourceManual)
	value, ok := resolver.resolveFromFile(ref)
	if !ok {
		t.Error("resolveFromFile should find the secret")
	}
	if value != "file_secret" {
		t.Errorf("value = %q, want %q", value, "file_secret")
	}

	// Test secret not in file
	ref2 := secrets.NewSecretReference("NONEXISTENT", secrets.SourceManual)
	_, ok = resolver.resolveFromFile(ref2)
	if ok {
		t.Error("resolveFromFile should not find non-existent secret")
	}
}

func TestResolver_ResolveAll(t *testing.T) {
	// Set up environment
	_ = os.Setenv("HOMEPORT_SECRET_DB_PASSWORD", "env_db_pass")
	_ = os.Setenv("HOMEPORT_SECRET_API_KEY", "env_api_key")
	defer func() {
		_ = os.Unsetenv("HOMEPORT_SECRET_DB_PASSWORD")
		_ = os.Unsetenv("HOMEPORT_SECRET_API_KEY")
	}()

	// Create manifest
	manifest := secrets.NewSecretsManifest()
	_ = manifest.AddSecret(secrets.NewSecretReference("DB_PASSWORD", secrets.SourceEnv).WithKey("DB_PASSWORD"))
	_ = manifest.AddSecret(secrets.NewSecretReference("API_KEY", secrets.SourceEnv).WithKey("API_KEY"))
	_ = manifest.AddSecret(secrets.NewSecretReference("OPTIONAL_SECRET", secrets.SourceManual).Optional())

	opts := DefaultResolverOptions()
	opts.AllowInteractive = false // Disable interactive for test
	resolver := NewResolver(opts)

	ctx := context.Background()
	resolved, err := resolver.ResolveAll(ctx, manifest)
	// May have error for optional secrets, that's OK - we continue regardless
	_ = err

	// Check resolved secrets
	if !resolved.Has("DB_PASSWORD") {
		t.Error("Should have resolved DB_PASSWORD")
	}
	if !resolved.Has("API_KEY") {
		t.Error("Should have resolved API_KEY")
	}

	// Verify values
	value, _ := resolved.GetValue("DB_PASSWORD")
	if value != "env_db_pass" {
		t.Errorf("DB_PASSWORD = %q, want %q", value, "env_db_pass")
	}
}

func TestResolver_CheckResolvability(t *testing.T) {
	// Set up environment
	_ = os.Setenv("HOMEPORT_SECRET_AVAILABLE", "value")
	defer func() { _ = os.Unsetenv("HOMEPORT_SECRET_AVAILABLE") }()

	manifest := secrets.NewSecretsManifest()
	_ = manifest.AddSecret(secrets.NewSecretReference("AVAILABLE", secrets.SourceEnv).WithKey("AVAILABLE"))
	_ = manifest.AddSecret(secrets.NewSecretReference("UNAVAILABLE", secrets.SourceAWSSecretsManager).WithKey("some/key"))

	opts := DefaultResolverOptions()
	opts.AllowInteractive = false
	resolver := NewResolver(opts)

	ctx := context.Background()
	report := resolver.CheckResolvability(ctx, manifest)

	// AVAILABLE should be resolvable
	if len(report.Resolvable) != 1 || report.Resolvable[0] != "AVAILABLE" {
		t.Errorf("Resolvable = %v, want [AVAILABLE]", report.Resolvable)
	}

	// UNAVAILABLE should be in maybe/unresolvable (depending on AWS CLI availability)
	if len(report.Unresolvable)+len(report.MaybeResolvable) == 0 {
		t.Error("UNAVAILABLE should be in maybe or unresolvable")
	}
}

func TestResolutionError(t *testing.T) {
	err := &ResolutionError{
		MissingSecrets: []string{"SECRET1", "SECRET2"},
	}

	msg := err.Error()
	if msg == "" {
		t.Error("Error message should not be empty")
	}

	if !contains(msg, "2") {
		t.Error("Error should mention count")
	}
	if !contains(msg, "SECRET1") {
		t.Error("Error should mention secret names")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsImpl(s, substr))
}

func containsImpl(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestCreateEnvFile(t *testing.T) {
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, ".env")

	resolved := secrets.NewResolvedSecrets()
	ref := secrets.NewSecretReference("TEST_SECRET", secrets.SourceManual)
	resolved.Add("TEST_SECRET", "test_value", "test", ref)

	err := CreateEnvFile(resolved, outputPath)
	if err != nil {
		t.Fatalf("CreateEnvFile() error = %v", err)
	}

	// Verify file contents
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}

	if !contains(string(content), "TEST_SECRET=test_value") {
		t.Error("File should contain the secret")
	}

	// Verify permissions (should be 0600)
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("Failed to stat output file: %v", err)
	}

	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("File permissions = %o, want %o", perm, 0600)
	}
}

func TestCreateEnvTemplate(t *testing.T) {
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, ".env.template")

	manifest := secrets.NewSecretsManifest()
	_ = manifest.AddSecret(secrets.NewSecretReference("DATABASE_PASSWORD", secrets.SourceAWSSecretsManager).
		WithKey("prod/db").
		WithDescription("Database password"))

	err := CreateEnvTemplate(manifest, outputPath)
	if err != nil {
		t.Fatalf("CreateEnvTemplate() error = %v", err)
	}

	// Verify file contents
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}

	if !contains(string(content), "DATABASE_PASSWORD=") {
		t.Error("Template should contain the variable")
	}
	if !contains(string(content), "REQUIRED") {
		t.Error("Template should mark required secrets")
	}
}
