package azure

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

func TestCredentialConfig_WithSubscriptionID(t *testing.T) {
	cfg := NewCredentialConfig().WithSubscriptionID("sub-123")
	if cfg.SubscriptionID != "sub-123" {
		t.Errorf("expected 'sub-123', got %s", cfg.SubscriptionID)
	}
}

func TestCredentialConfig_WithTenantID(t *testing.T) {
	cfg := NewCredentialConfig().WithTenantID("tenant-123")
	if cfg.TenantID != "tenant-123" {
		t.Errorf("expected 'tenant-123', got %s", cfg.TenantID)
	}
}

func TestCredentialConfig_WithServicePrincipal(t *testing.T) {
	cfg := NewCredentialConfig().WithServicePrincipal("tenant-123", "client-456", "secret-789")
	if cfg.TenantID != "tenant-123" {
		t.Errorf("expected 'tenant-123', got %s", cfg.TenantID)
	}
	if cfg.ClientID != "client-456" {
		t.Errorf("expected 'client-456', got %s", cfg.ClientID)
	}
	if cfg.ClientSecret != "secret-789" {
		t.Errorf("expected 'secret-789', got %s", cfg.ClientSecret)
	}
	if cfg.Source != CredentialSourceServicePrincipal {
		t.Errorf("expected service_principal source, got %s", cfg.Source)
	}
}

func TestDetectCredentialSource_ServicePrincipal(t *testing.T) {
	// Save and restore original values
	origClientID := os.Getenv("AZURE_CLIENT_ID")
	origClientSecret := os.Getenv("AZURE_CLIENT_SECRET")
	origTenantID := os.Getenv("AZURE_TENANT_ID")
	defer func() {
		_ = os.Setenv("AZURE_CLIENT_ID", origClientID)
		_ = os.Setenv("AZURE_CLIENT_SECRET", origClientSecret)
		_ = os.Setenv("AZURE_TENANT_ID", origTenantID)
	}()

	_ = os.Setenv("AZURE_CLIENT_ID", "test-client")
	_ = os.Setenv("AZURE_CLIENT_SECRET", "test-secret")
	_ = os.Setenv("AZURE_TENANT_ID", "test-tenant")

	source := DetectCredentialSource()
	if source != CredentialSourceServicePrincipal {
		t.Errorf("expected service_principal source, got %s", source)
	}
}

func TestDetectCredentialSource_ManagedIdentity(t *testing.T) {
	// Save and restore original values
	origClientID := os.Getenv("AZURE_CLIENT_ID")
	origClientSecret := os.Getenv("AZURE_CLIENT_SECRET")
	origTenantID := os.Getenv("AZURE_TENANT_ID")
	defer func() {
		_ = os.Setenv("AZURE_CLIENT_ID", origClientID)
		_ = os.Setenv("AZURE_CLIENT_SECRET", origClientSecret)
		_ = os.Setenv("AZURE_TENANT_ID", origTenantID)
	}()

	_ = os.Setenv("AZURE_CLIENT_ID", "test-client")
	_ = os.Unsetenv("AZURE_CLIENT_SECRET")
	_ = os.Unsetenv("AZURE_TENANT_ID")

	source := DetectCredentialSource()
	if source != CredentialSourceManagedIdentity {
		t.Errorf("expected managed_identity source, got %s", source)
	}
}

func TestDetectCredentialSource_Default(t *testing.T) {
	// Save and restore original values
	origClientID := os.Getenv("AZURE_CLIENT_ID")
	origClientSecret := os.Getenv("AZURE_CLIENT_SECRET")
	origTenantID := os.Getenv("AZURE_TENANT_ID")
	defer func() {
		_ = os.Setenv("AZURE_CLIENT_ID", origClientID)
		_ = os.Setenv("AZURE_CLIENT_SECRET", origClientSecret)
		_ = os.Setenv("AZURE_TENANT_ID", origTenantID)
	}()

	_ = os.Unsetenv("AZURE_CLIENT_ID")
	_ = os.Unsetenv("AZURE_CLIENT_SECRET")
	_ = os.Unsetenv("AZURE_TENANT_ID")

	source := DetectCredentialSource()
	// Should be either default or cli depending on Azure CLI config
	if source != CredentialSourceDefault && source != CredentialSourceCLI {
		t.Errorf("expected default or cli source, got %s", source)
	}
}

func TestFromParseOptions(t *testing.T) {
	creds := map[string]string{
		"subscription_id": "sub-123",
		"tenant_id":       "tenant-456",
		"client_id":       "client-789",
		"client_secret":   "secret-abc",
	}

	cfg := FromParseOptions(creds, []string{"eastus"})

	if cfg.SubscriptionID != "sub-123" {
		t.Errorf("expected 'sub-123', got %s", cfg.SubscriptionID)
	}
	if cfg.TenantID != "tenant-456" {
		t.Errorf("expected 'tenant-456', got %s", cfg.TenantID)
	}
	if cfg.ClientID != "client-789" {
		t.Errorf("expected 'client-789', got %s", cfg.ClientID)
	}
	if cfg.ClientSecret != "secret-abc" {
		t.Errorf("expected 'secret-abc', got %s", cfg.ClientSecret)
	}
	if cfg.Source != CredentialSourceServicePrincipal {
		t.Errorf("expected service_principal source, got %s", cfg.Source)
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

func TestGetSubscriptionID_FromConfig(t *testing.T) {
	cfg := NewCredentialConfig().WithSubscriptionID("sub-123")
	subID, err := cfg.GetSubscriptionID()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if subID != "sub-123" {
		t.Errorf("expected 'sub-123', got %s", subID)
	}
}

func TestGetSubscriptionID_FromEnv(t *testing.T) {
	// Save and restore original value
	original := os.Getenv("AZURE_SUBSCRIPTION_ID")
	defer func() { _ = os.Setenv("AZURE_SUBSCRIPTION_ID", original) }()

	_ = os.Setenv("AZURE_SUBSCRIPTION_ID", "env-sub-123")
	cfg := NewCredentialConfig()
	subID, err := cfg.GetSubscriptionID()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if subID != "env-sub-123" {
		t.Errorf("expected 'env-sub-123', got %s", subID)
	}
}
