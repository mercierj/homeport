package aws

import (
	"os"
	"testing"
)

func TestNewCredentialConfig(t *testing.T) {
	cfg := NewCredentialConfig()

	if cfg.Source != CredentialSourceDefault {
		t.Errorf("expected default source, got %s", cfg.Source)
	}
	if cfg.Region != "us-east-1" {
		t.Errorf("expected us-east-1 region, got %s", cfg.Region)
	}
}

func TestCredentialConfig_WithProfile(t *testing.T) {
	cfg := NewCredentialConfig().WithProfile("myprofile")

	if cfg.Source != CredentialSourceProfile {
		t.Errorf("expected profile source, got %s", cfg.Source)
	}
	if cfg.Profile != "myprofile" {
		t.Errorf("expected myprofile, got %s", cfg.Profile)
	}
}

func TestCredentialConfig_WithRegion(t *testing.T) {
	cfg := NewCredentialConfig().WithRegion("eu-west-1")

	if cfg.Region != "eu-west-1" {
		t.Errorf("expected eu-west-1, got %s", cfg.Region)
	}
}

func TestCredentialConfig_WithStaticCredentials(t *testing.T) {
	cfg := NewCredentialConfig().WithStaticCredentials("AKID", "SECRET", "SESSION")

	if cfg.Source != CredentialSourceStatic {
		t.Errorf("expected static source, got %s", cfg.Source)
	}
	if cfg.AccessKeyID != "AKID" {
		t.Errorf("expected AKID, got %s", cfg.AccessKeyID)
	}
	if cfg.SecretAccessKey != "SECRET" {
		t.Errorf("expected SECRET, got %s", cfg.SecretAccessKey)
	}
	if cfg.SessionToken != "SESSION" {
		t.Errorf("expected SESSION, got %s", cfg.SessionToken)
	}
}

func TestCredentialConfig_WithRole(t *testing.T) {
	cfg := NewCredentialConfig().WithRole("arn:aws:iam::123456789012:role/MyRole", "ext-id")

	if cfg.Source != CredentialSourceRole {
		t.Errorf("expected role source, got %s", cfg.Source)
	}
	if cfg.RoleARN != "arn:aws:iam::123456789012:role/MyRole" {
		t.Errorf("expected role ARN, got %s", cfg.RoleARN)
	}
	if cfg.ExternalID != "ext-id" {
		t.Errorf("expected ext-id, got %s", cfg.ExternalID)
	}
}

func TestDetectCredentialSource(t *testing.T) {
	// Clear environment variables for test
	originalAccessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	originalSecretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	originalProfile := os.Getenv("AWS_PROFILE")
	originalRoleARN := os.Getenv("AWS_ROLE_ARN")

	defer func() {
		_ = os.Setenv("AWS_ACCESS_KEY_ID", originalAccessKey)
		_ = os.Setenv("AWS_SECRET_ACCESS_KEY", originalSecretKey)
		_ = os.Setenv("AWS_PROFILE", originalProfile)
		_ = os.Setenv("AWS_ROLE_ARN", originalRoleARN)
	}()

	// Test env source
	_ = os.Setenv("AWS_ACCESS_KEY_ID", "test")
	_ = os.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	_ = os.Unsetenv("AWS_PROFILE")
	_ = os.Unsetenv("AWS_ROLE_ARN")

	if source := DetectCredentialSource(); source != CredentialSourceEnv {
		t.Errorf("expected env source, got %s", source)
	}

	// Test profile source
	_ = os.Unsetenv("AWS_ACCESS_KEY_ID")
	_ = os.Unsetenv("AWS_SECRET_ACCESS_KEY")
	_ = os.Setenv("AWS_PROFILE", "myprofile")

	if source := DetectCredentialSource(); source != CredentialSourceProfile {
		t.Errorf("expected profile source, got %s", source)
	}

	// Test role source
	_ = os.Unsetenv("AWS_PROFILE")
	_ = os.Setenv("AWS_ROLE_ARN", "arn:aws:iam::123456789012:role/MyRole")

	if source := DetectCredentialSource(); source != CredentialSourceRole {
		t.Errorf("expected role source, got %s", source)
	}

	// Test default source
	_ = os.Unsetenv("AWS_ROLE_ARN")

	if source := DetectCredentialSource(); source != CredentialSourceDefault {
		t.Errorf("expected default source, got %s", source)
	}
}

func TestFromParseOptions(t *testing.T) {
	// Test profile from options
	opts := map[string]string{"profile": "myprofile"}
	regions := []string{"eu-central-1"}

	cfg := FromParseOptions(opts, regions)

	if cfg.Source != CredentialSourceProfile {
		t.Errorf("expected profile source, got %s", cfg.Source)
	}
	if cfg.Profile != "myprofile" {
		t.Errorf("expected myprofile, got %s", cfg.Profile)
	}
	if cfg.Region != "eu-central-1" {
		t.Errorf("expected eu-central-1, got %s", cfg.Region)
	}

	// Test static credentials from options
	opts = map[string]string{
		"access_key_id":     "AKID",
		"secret_access_key": "SECRET",
		"session_token":     "SESSION",
	}
	regions = []string{"us-west-2"}

	cfg = FromParseOptions(opts, regions)

	if cfg.Source != CredentialSourceStatic {
		t.Errorf("expected static source, got %s", cfg.Source)
	}
	if cfg.AccessKeyID != "AKID" {
		t.Errorf("expected AKID, got %s", cfg.AccessKeyID)
	}

	// Test role from options
	opts = map[string]string{
		"role_arn":    "arn:aws:iam::123456789012:role/MyRole",
		"external_id": "ext-123",
	}
	regions = []string{}

	cfg = FromParseOptions(opts, regions)

	if cfg.Source != CredentialSourceRole {
		t.Errorf("expected role source, got %s", cfg.Source)
	}
	if cfg.RoleARN != "arn:aws:iam::123456789012:role/MyRole" {
		t.Errorf("expected role ARN, got %s", cfg.RoleARN)
	}
}
