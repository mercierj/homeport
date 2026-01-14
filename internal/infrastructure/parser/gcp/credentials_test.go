package gcp

import (
	"os"
	"testing"
)

func TestNewCredentialConfig(t *testing.T) {
	cfg := NewCredentialConfig()
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.Source != CredentialSourceDefault {
		t.Errorf("expected default source, got %s", cfg.Source)
	}
}

func TestCredentialConfig_WithProject(t *testing.T) {
	cfg := NewCredentialConfig().WithProject("my-project")
	if cfg.Project != "my-project" {
		t.Errorf("expected 'my-project', got %s", cfg.Project)
	}
}

func TestCredentialConfig_WithCredentialsFile(t *testing.T) {
	cfg := NewCredentialConfig().WithCredentialsFile("/path/to/creds.json")
	if cfg.CredentialsFile != "/path/to/creds.json" {
		t.Errorf("expected '/path/to/creds.json', got %s", cfg.CredentialsFile)
	}
	if cfg.Source != CredentialSourceServiceAccount {
		t.Errorf("expected service_account source, got %s", cfg.Source)
	}
}

func TestDetectCredentialSource_Environment(t *testing.T) {
	// Save and restore original value
	original := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	defer func() { _ = os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", original) }()

	_ = os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "/path/to/creds.json")
	source := DetectCredentialSource()
	if source != CredentialSourceEnvironment {
		t.Errorf("expected environment source, got %s", source)
	}
}

func TestDetectCredentialSource_Default(t *testing.T) {
	// Save and restore original value
	original := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	defer func() { _ = os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", original) }()

	_ = os.Unsetenv("GOOGLE_APPLICATION_CREDENTIALS")
	source := DetectCredentialSource()
	// Should be either default or user_account depending on gcloud config
	if source != CredentialSourceDefault && source != CredentialSourceUserAccount {
		t.Errorf("expected default or user_account source, got %s", source)
	}
}

func TestFromParseOptions(t *testing.T) {
	creds := map[string]string{
		"project":          "test-project",
		"credentials_file": "/path/to/creds.json",
	}

	cfg := FromParseOptions(creds, []string{"us-central1"})

	if cfg.Project != "test-project" {
		t.Errorf("expected 'test-project', got %s", cfg.Project)
	}
	if cfg.CredentialsFile != "/path/to/creds.json" {
		t.Errorf("expected credentials file, got %s", cfg.CredentialsFile)
	}
	if cfg.Source != CredentialSourceServiceAccount {
		t.Errorf("expected service_account source, got %s", cfg.Source)
	}
}

func TestFromParseOptions_Empty(t *testing.T) {
	cfg := FromParseOptions(nil, nil)
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.Source != CredentialSourceDefault {
		t.Errorf("expected default source, got %s", cfg.Source)
	}
}
