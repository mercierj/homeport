package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/homeport/homeport/internal/domain/secrets"
)

// AzureKeyVaultProvider resolves secrets from Azure Key Vault.
// Requires Azure CLI (az) to be installed and configured.
type AzureKeyVaultProvider struct {
	// VaultName is the Azure Key Vault name.
	VaultName string

	// Subscription is the Azure subscription ID (optional).
	Subscription string
}

// NewAzureKeyVaultProvider creates a new Azure Key Vault provider.
func NewAzureKeyVaultProvider() *AzureKeyVaultProvider {
	return &AzureKeyVaultProvider{}
}

// WithVaultName sets the Key Vault name.
func (p *AzureKeyVaultProvider) WithVaultName(name string) *AzureKeyVaultProvider {
	p.VaultName = name
	return p
}

// WithSubscription sets the Azure subscription.
func (p *AzureKeyVaultProvider) WithSubscription(sub string) *AzureKeyVaultProvider {
	p.Subscription = sub
	return p
}

// Name returns the provider identifier.
func (p *AzureKeyVaultProvider) Name() secrets.SecretSource {
	return secrets.SourceAzureKeyVault
}

// CanResolve checks if this provider can handle the secret reference.
func (p *AzureKeyVaultProvider) CanResolve(ref *secrets.SecretReference) bool {
	return ref.Source == secrets.SourceAzureKeyVault && ref.Key != ""
}

// Resolve retrieves a secret from Azure Key Vault.
func (p *AzureKeyVaultProvider) Resolve(ctx context.Context, ref *secrets.SecretReference) (string, error) {
	if !p.CanResolve(ref) {
		return "", fmt.Errorf("cannot resolve secret %s: invalid source or missing key", ref.Name)
	}

	// Parse the key - can be in formats:
	// - "secret-name" (requires VaultName to be set)
	// - "vault-name/secret-name"
	// - Full URL: "https://vault-name.vault.azure.net/secrets/secret-name"
	vaultName := p.VaultName
	secretName := ref.Key

	// Parse vault name from key if in format "vault/secret"
	if strings.Contains(secretName, "/") && !strings.HasPrefix(secretName, "https://") {
		parts := strings.SplitN(secretName, "/", 2)
		vaultName = parts[0]
		secretName = parts[1]
	}

	// Parse from full URL
	if strings.HasPrefix(secretName, "https://") {
		// https://vault-name.vault.azure.net/secrets/secret-name
		secretName = strings.TrimPrefix(secretName, "https://")
		parts := strings.Split(secretName, "/")
		if len(parts) >= 3 {
			vaultName = strings.Split(parts[0], ".")[0]
			secretName = parts[2]
		}
	}

	if vaultName == "" {
		return "", fmt.Errorf("vault name not specified for secret %s", ref.Name)
	}

	// Build az CLI command
	args := []string{"keyvault", "secret", "show",
		"--vault-name", vaultName,
		"--name", secretName,
		"--query", "value",
		"--output", "tsv",
	}

	if ref.Version != "" {
		args = append(args, "--version", ref.Version)
	}
	if p.Subscription != "" {
		args = append(args, "--subscription", p.Subscription)
	}

	cmd := exec.CommandContext(ctx, "az", args...)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("az CLI error: %s", string(exitErr.Stderr))
		}
		return "", fmt.Errorf("failed to run az CLI: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// ValidateConfig checks if Azure CLI is available and configured.
func (p *AzureKeyVaultProvider) ValidateConfig() error {
	// Check if az CLI is available
	if _, err := exec.LookPath("az"); err != nil {
		return fmt.Errorf("Azure CLI (az) not found in PATH: %w", err)
	}

	// Check authentication
	args := []string{"account", "show"}
	if p.Subscription != "" {
		args = append(args, "--subscription", p.Subscription)
	}

	cmd := exec.Command("az", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Azure CLI not authenticated: %w", err)
	}

	return nil
}

// ListSecrets lists available secrets in Azure Key Vault.
func (p *AzureKeyVaultProvider) ListSecrets(ctx context.Context) ([]string, error) {
	if p.VaultName == "" {
		return nil, fmt.Errorf("vault name not specified")
	}

	args := []string{"keyvault", "secret", "list",
		"--vault-name", p.VaultName,
		"--query", "[].name",
		"--output", "json",
	}

	if p.Subscription != "" {
		args = append(args, "--subscription", p.Subscription)
	}

	cmd := exec.CommandContext(ctx, "az", args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list secrets: %w", err)
	}

	var names []string
	if err := json.Unmarshal(output, &names); err != nil {
		return nil, fmt.Errorf("failed to parse secret list: %w", err)
	}

	return names, nil
}

// ListVaults lists available Key Vaults in the subscription.
func (p *AzureKeyVaultProvider) ListVaults(ctx context.Context) ([]string, error) {
	args := []string{"keyvault", "list",
		"--query", "[].name",
		"--output", "json",
	}

	if p.Subscription != "" {
		args = append(args, "--subscription", p.Subscription)
	}

	cmd := exec.CommandContext(ctx, "az", args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list vaults: %w", err)
	}

	var names []string
	if err := json.Unmarshal(output, &names); err != nil {
		return nil, fmt.Errorf("failed to parse vault list: %w", err)
	}

	return names, nil
}

// CreateSecret creates or updates a secret in Azure Key Vault.
func (p *AzureKeyVaultProvider) CreateSecret(ctx context.Context, name, value string) error {
	if p.VaultName == "" {
		return fmt.Errorf("vault name not specified")
	}

	args := []string{"keyvault", "secret", "set",
		"--vault-name", p.VaultName,
		"--name", name,
		"--value", value,
	}

	if p.Subscription != "" {
		args = append(args, "--subscription", p.Subscription)
	}

	cmd := exec.CommandContext(ctx, "az", args...)
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("failed to create secret: %s", string(exitErr.Stderr))
		}
		return fmt.Errorf("failed to create secret: %w", err)
	}

	return nil
}
