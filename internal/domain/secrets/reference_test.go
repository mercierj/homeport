package secrets

import (
	"strings"
	"testing"
)

func TestSecretSource_IsValid(t *testing.T) {
	tests := []struct {
		source SecretSource
		valid  bool
	}{
		{SourceManual, true},
		{SourceEnv, true},
		{SourceFile, true},
		{SourceAWSSecretsManager, true},
		{SourceGCPSecretManager, true},
		{SourceAzureKeyVault, true},
		{SourceHashiCorpVault, true},
		{SecretSource("unknown"), false},
		{SecretSource(""), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.source), func(t *testing.T) {
			if got := tt.source.IsValid(); got != tt.valid {
				t.Errorf("SecretSource(%q).IsValid() = %v, want %v", tt.source, got, tt.valid)
			}
		})
	}
}

func TestSecretSource_IsCloudProvider(t *testing.T) {
	tests := []struct {
		source   SecretSource
		isCloud  bool
	}{
		{SourceAWSSecretsManager, true},
		{SourceGCPSecretManager, true},
		{SourceAzureKeyVault, true},
		{SourceHashiCorpVault, false},
		{SourceManual, false},
		{SourceEnv, false},
		{SourceFile, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.source), func(t *testing.T) {
			if got := tt.source.IsCloudProvider(); got != tt.isCloud {
				t.Errorf("SecretSource(%q).IsCloudProvider() = %v, want %v", tt.source, got, tt.isCloud)
			}
		})
	}
}

func TestNewSecretReference(t *testing.T) {
	ref := NewSecretReference("DATABASE_PASSWORD", SourceAWSSecretsManager)

	if ref.Name != "DATABASE_PASSWORD" {
		t.Errorf("Name = %q, want %q", ref.Name, "DATABASE_PASSWORD")
	}
	if ref.Source != SourceAWSSecretsManager {
		t.Errorf("Source = %q, want %q", ref.Source, SourceAWSSecretsManager)
	}
	if !ref.Required {
		t.Error("Required should be true by default")
	}
	if ref.Type != TypeGeneric {
		t.Errorf("Type = %q, want %q", ref.Type, TypeGeneric)
	}
}

func TestSecretReference_Validate(t *testing.T) {
	tests := []struct {
		name    string
		ref     *SecretReference
		wantErr bool
	}{
		{
			name: "valid aws secret",
			ref: &SecretReference{
				Name:     "DATABASE_PASSWORD",
				Source:   SourceAWSSecretsManager,
				Key:      "prod/myapp/db-password",
				Required: true,
			},
			wantErr: false,
		},
		{
			name: "valid manual secret",
			ref: &SecretReference{
				Name:     "API_KEY",
				Source:   SourceManual,
				Required: true,
			},
			wantErr: false,
		},
		{
			name: "empty name",
			ref: &SecretReference{
				Name:   "",
				Source: SourceManual,
			},
			wantErr: true,
		},
		{
			name: "invalid name format",
			ref: &SecretReference{
				Name:   "database-password", // lowercase with hyphen
				Source: SourceManual,
			},
			wantErr: true,
		},
		{
			name: "invalid source",
			ref: &SecretReference{
				Name:   "DATABASE_PASSWORD",
				Source: SecretSource("invalid"),
			},
			wantErr: true,
		},
		{
			name: "missing key for non-manual source",
			ref: &SecretReference{
				Name:   "DATABASE_PASSWORD",
				Source: SourceAWSSecretsManager,
				Key:    "", // key required for AWS
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.ref.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSecretReference_ChainedMethods(t *testing.T) {
	ref := NewSecretReference("DATABASE_PASSWORD", SourceAWSSecretsManager).
		WithKey("prod/myapp/db").
		WithDescription("Main database password").
		WithType(TypePassword).
		AddUsedBy("postgres").
		AddUsedBy("api-server")

	if ref.Key != "prod/myapp/db" {
		t.Errorf("Key = %q, want %q", ref.Key, "prod/myapp/db")
	}
	if ref.Description != "Main database password" {
		t.Errorf("Description = %q, want %q", ref.Description, "Main database password")
	}
	if ref.Type != TypePassword {
		t.Errorf("Type = %q, want %q", ref.Type, TypePassword)
	}
	if len(ref.UsedBy) != 2 {
		t.Errorf("UsedBy length = %d, want %d", len(ref.UsedBy), 2)
	}

	// Test Optional
	ref.Optional()
	if ref.Required {
		t.Error("Required should be false after Optional()")
	}
}

func TestSecretsManifest_AddSecret(t *testing.T) {
	m := NewSecretsManifest()

	// Add first secret
	ref1 := NewSecretReference("DATABASE_PASSWORD", SourceAWSSecretsManager).WithKey("prod/db")
	err := m.AddSecret(ref1)
	if err != nil {
		t.Fatalf("AddSecret() error = %v", err)
	}

	if len(m.Secrets) != 1 {
		t.Errorf("Secrets length = %d, want %d", len(m.Secrets), 1)
	}
	if m.RequiredCount != 1 {
		t.Errorf("RequiredCount = %d, want %d", m.RequiredCount, 1)
	}

	// Add duplicate should fail
	ref2 := NewSecretReference("DATABASE_PASSWORD", SourceManual)
	err = m.AddSecret(ref2)
	if err == nil {
		t.Error("AddSecret() should fail for duplicate name")
	}

	// Add optional secret
	ref3 := NewSecretReference("OPTIONAL_KEY", SourceEnv).WithKey("OPTIONAL_KEY").Optional()
	err = m.AddSecret(ref3)
	if err != nil {
		t.Fatalf("AddSecret() error = %v", err)
	}

	if m.RequiredCount != 1 { // Should not increase for optional
		t.Errorf("RequiredCount = %d, want %d", m.RequiredCount, 1)
	}
}

func TestSecretsManifest_GetBySource(t *testing.T) {
	m := NewSecretsManifest()

	_ = m.AddSecret(NewSecretReference("AWS_SECRET_1", SourceAWSSecretsManager).WithKey("key1"))
	_ = m.AddSecret(NewSecretReference("AWS_SECRET_2", SourceAWSSecretsManager).WithKey("key2"))
	_ = m.AddSecret(NewSecretReference("GCP_SECRET_1", SourceGCPSecretManager).WithKey("key3"))
	_ = m.AddSecret(NewSecretReference("MANUAL_SECRET", SourceManual))

	awsSecrets := m.GetBySource(SourceAWSSecretsManager)
	if len(awsSecrets) != 2 {
		t.Errorf("AWS secrets = %d, want %d", len(awsSecrets), 2)
	}

	gcpSecrets := m.GetBySource(SourceGCPSecretManager)
	if len(gcpSecrets) != 1 {
		t.Errorf("GCP secrets = %d, want %d", len(gcpSecrets), 1)
	}

	cloudSecrets := m.GetCloudSecrets()
	if len(cloudSecrets) != 3 {
		t.Errorf("Cloud secrets = %d, want %d", len(cloudSecrets), 3)
	}
}

func TestSecretsManifest_GenerateEnvTemplate(t *testing.T) {
	m := NewSecretsManifest()

	_ = m.AddSecret(NewSecretReference("DATABASE_PASSWORD", SourceAWSSecretsManager).
		WithKey("prod/db").
		WithDescription("PostgreSQL database password"))

	_ = m.AddSecret(NewSecretReference("API_KEY", SourceManual).
		WithDescription("Third-party API key").
		Optional())

	template := m.GenerateEnvTemplate()

	// Check for required markers
	if !strings.Contains(template, "# REQUIRED") {
		t.Error("Template should contain REQUIRED marker")
	}
	if !strings.Contains(template, "# OPTIONAL") {
		t.Error("Template should contain OPTIONAL marker")
	}

	// Check for variable names
	if !strings.Contains(template, "DATABASE_PASSWORD=") {
		t.Error("Template should contain DATABASE_PASSWORD")
	}
	if !strings.Contains(template, "API_KEY=") {
		t.Error("Template should contain API_KEY")
	}

	// Check for descriptions
	if !strings.Contains(template, "PostgreSQL database password") {
		t.Error("Template should contain description")
	}
}

func TestResolvedSecrets(t *testing.T) {
	resolved := NewResolvedSecrets()

	ref := NewSecretReference("DATABASE_PASSWORD", SourceManual)
	resolved.Add("DATABASE_PASSWORD", "secret123", "interactive", ref)

	// Test Get
	s, ok := resolved.Get("DATABASE_PASSWORD")
	if !ok {
		t.Error("Get() should return true for existing secret")
	}
	if s.Value != "secret123" {
		t.Errorf("Value = %q, want %q", s.Value, "secret123")
	}

	// Test GetValue
	value, ok := resolved.GetValue("DATABASE_PASSWORD")
	if !ok || value != "secret123" {
		t.Error("GetValue() failed")
	}

	// Test Has
	if !resolved.Has("DATABASE_PASSWORD") {
		t.Error("Has() should return true")
	}
	if resolved.Has("NONEXISTENT") {
		t.Error("Has() should return false for non-existent")
	}

	// Test Count
	if resolved.Count() != 1 {
		t.Errorf("Count() = %d, want %d", resolved.Count(), 1)
	}

	// Test ToEnvMap
	envMap := resolved.ToEnvMap()
	if envMap["DATABASE_PASSWORD"] != "secret123" {
		t.Error("ToEnvMap() failed")
	}

	// Test Clear
	resolved.Clear()
	if resolved.Count() != 0 {
		t.Error("Clear() should empty the secrets")
	}
}

func TestEscapeEnvValue(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"with spaces", "\"with spaces\""},
		{"with\nnewline", "\"with\nnewline\""},
		{"with\"quotes", "\"with\\\"quotes\""},
		{"with$dollar", "\"with\\$dollar\""},
		{"with\\backslash", "\"with\\\\backslash\""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := escapeEnvValue(tt.input)
			if got != tt.expected {
				t.Errorf("escapeEnvValue(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
