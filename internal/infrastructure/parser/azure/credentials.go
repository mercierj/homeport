package azure

import (
	"context"
	"fmt"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

// CredentialSource indicates how credentials were obtained.
type CredentialSource string

const (
	// CredentialSourceDefault uses DefaultAzureCredential chain.
	CredentialSourceDefault CredentialSource = "default"

	// CredentialSourceEnvironment uses environment variables.
	CredentialSourceEnvironment CredentialSource = "environment"

	// CredentialSourceCLI uses Azure CLI credentials.
	CredentialSourceCLI CredentialSource = "cli"

	// CredentialSourceServicePrincipal uses a service principal.
	CredentialSourceServicePrincipal CredentialSource = "service_principal"

	// CredentialSourceManagedIdentity uses managed identity.
	CredentialSourceManagedIdentity CredentialSource = "managed_identity"
)

// CredentialConfig holds configuration for Azure authentication.
type CredentialConfig struct {
	// SubscriptionID is the Azure subscription ID.
	SubscriptionID string

	// TenantID is the Azure AD tenant ID.
	TenantID string

	// ClientID is the service principal client ID.
	ClientID string

	// ClientSecret is the service principal client secret.
	ClientSecret string

	// Source indicates how credentials should be obtained.
	Source CredentialSource
}

// NewCredentialConfig creates a new credential configuration with defaults.
func NewCredentialConfig() *CredentialConfig {
	return &CredentialConfig{
		Source: CredentialSourceDefault,
	}
}

// WithSubscriptionID sets the subscription ID.
func (c *CredentialConfig) WithSubscriptionID(subscriptionID string) *CredentialConfig {
	c.SubscriptionID = subscriptionID
	return c
}

// WithTenantID sets the tenant ID.
func (c *CredentialConfig) WithTenantID(tenantID string) *CredentialConfig {
	c.TenantID = tenantID
	return c
}

// WithServicePrincipal sets service principal credentials.
func (c *CredentialConfig) WithServicePrincipal(tenantID, clientID, clientSecret string) *CredentialConfig {
	c.TenantID = tenantID
	c.ClientID = clientID
	c.ClientSecret = clientSecret
	c.Source = CredentialSourceServicePrincipal
	return c
}

// DetectCredentialSource detects how Azure credentials are configured.
func DetectCredentialSource() CredentialSource {
	// Check for service principal env vars
	if os.Getenv("AZURE_CLIENT_ID") != "" &&
		os.Getenv("AZURE_CLIENT_SECRET") != "" &&
		os.Getenv("AZURE_TENANT_ID") != "" {
		return CredentialSourceServicePrincipal
	}

	// Check for managed identity
	if os.Getenv("AZURE_CLIENT_ID") != "" && os.Getenv("AZURE_CLIENT_SECRET") == "" {
		return CredentialSourceManagedIdentity
	}

	// Check for Azure CLI
	if _, err := os.Stat(getAzureCLIPath()); err == nil {
		return CredentialSourceCLI
	}

	return CredentialSourceDefault
}

// getAzureCLIPath returns the path to Azure CLI token cache.
func getAzureCLIPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home + "/.azure/accessTokens.json"
}

// GetCredential returns an Azure credential based on the configuration.
func (c *CredentialConfig) GetCredential(ctx context.Context) (*azidentity.DefaultAzureCredential, error) {
	switch c.Source {
	case CredentialSourceServicePrincipal:
		// For service principal, we would use ClientSecretCredential
		// but return DefaultAzureCredential which also picks it up from env
		return azidentity.NewDefaultAzureCredential(nil)

	case CredentialSourceCLI:
		// Azure CLI credentials are picked up by DefaultAzureCredential
		return azidentity.NewDefaultAzureCredential(nil)

	case CredentialSourceManagedIdentity:
		// Managed identity is picked up by DefaultAzureCredential
		return azidentity.NewDefaultAzureCredential(nil)

	default:
		// Use default credential chain
		return azidentity.NewDefaultAzureCredential(nil)
	}
}

// GetSubscriptionID returns the subscription ID, attempting to auto-detect if not set.
func (c *CredentialConfig) GetSubscriptionID() (string, error) {
	if c.SubscriptionID != "" {
		return c.SubscriptionID, nil
	}

	// Try from environment
	if subID := os.Getenv("AZURE_SUBSCRIPTION_ID"); subID != "" {
		return subID, nil
	}

	return "", fmt.Errorf("could not determine Azure subscription ID")
}

// FromParseOptions creates a credential config from parser options.
func FromParseOptions(creds map[string]string, regions []string) *CredentialConfig {
	cfg := NewCredentialConfig()

	if subID, ok := creds["subscription_id"]; ok {
		cfg.SubscriptionID = subID
	}

	if tenantID, ok := creds["tenant_id"]; ok {
		cfg.TenantID = tenantID
	}

	if clientID, ok := creds["client_id"]; ok {
		cfg.ClientID = clientID
	}

	if clientSecret, ok := creds["client_secret"]; ok {
		cfg.ClientSecret = clientSecret
		cfg.Source = CredentialSourceServicePrincipal
	}

	return cfg
}

// CallerIdentity contains information about the authenticated identity.
type CallerIdentity struct {
	// SubscriptionID is the Azure subscription ID.
	SubscriptionID string

	// TenantID is the Azure AD tenant ID.
	TenantID string

	// ObjectID is the object ID of the authenticated principal.
	ObjectID string

	// PrincipalType is the type of principal (User, ServicePrincipal, etc.).
	PrincipalType string
}
