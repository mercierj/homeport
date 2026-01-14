package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/homeport/homeport/internal/domain/secrets"
)

// HashiCorpVaultProvider resolves secrets from HashiCorp Vault.
// Requires the Vault CLI to be installed and configured.
type HashiCorpVaultProvider struct {
	// Address is the Vault server address (e.g., https://vault.example.com:8200).
	Address string

	// Token is the Vault token for authentication.
	Token string

	// Namespace is the Vault namespace (for Vault Enterprise).
	Namespace string

	// Mount is the secrets engine mount path (default: "secret").
	Mount string
}

// NewHashiCorpVaultProvider creates a new HashiCorp Vault provider.
func NewHashiCorpVaultProvider() *HashiCorpVaultProvider {
	return &HashiCorpVaultProvider{
		Mount: "secret",
	}
}

// WithAddress sets the Vault address.
func (p *HashiCorpVaultProvider) WithAddress(addr string) *HashiCorpVaultProvider {
	p.Address = addr
	return p
}

// WithToken sets the Vault token.
func (p *HashiCorpVaultProvider) WithToken(token string) *HashiCorpVaultProvider {
	p.Token = token
	return p
}

// WithNamespace sets the Vault namespace.
func (p *HashiCorpVaultProvider) WithNamespace(ns string) *HashiCorpVaultProvider {
	p.Namespace = ns
	return p
}

// WithMount sets the secrets engine mount path.
func (p *HashiCorpVaultProvider) WithMount(mount string) *HashiCorpVaultProvider {
	p.Mount = mount
	return p
}

// Name returns the provider identifier.
func (p *HashiCorpVaultProvider) Name() secrets.SecretSource {
	return secrets.SourceHashiCorpVault
}

// CanResolve checks if this provider can handle the secret reference.
func (p *HashiCorpVaultProvider) CanResolve(ref *secrets.SecretReference) bool {
	return ref.Source == secrets.SourceHashiCorpVault && ref.Key != ""
}

// Resolve retrieves a secret from HashiCorp Vault.
func (p *HashiCorpVaultProvider) Resolve(ctx context.Context, ref *secrets.SecretReference) (string, error) {
	if !p.CanResolve(ref) {
		return "", fmt.Errorf("cannot resolve secret %s: invalid source or missing key", ref.Name)
	}

	// Parse the key - expected format: "path/to/secret" or "path/to/secret#field"
	secretPath := ref.Key
	fieldName := ""

	if idx := strings.Index(secretPath, "#"); idx != -1 {
		fieldName = secretPath[idx+1:]
		secretPath = secretPath[:idx]
	}

	// Build vault command
	args := []string{"kv", "get", "-format=json"}

	// Handle KV v2 path
	fullPath := secretPath
	if !strings.HasPrefix(secretPath, p.Mount) {
		fullPath = p.Mount + "/data/" + secretPath
	}
	args = append(args, fullPath)

	cmd := exec.CommandContext(ctx, "vault", args...)

	// Set environment variables
	env := os.Environ()
	if p.Address != "" {
		env = append(env, "VAULT_ADDR="+p.Address)
	}
	if p.Token != "" {
		env = append(env, "VAULT_TOKEN="+p.Token)
	}
	if p.Namespace != "" {
		env = append(env, "VAULT_NAMESPACE="+p.Namespace)
	}
	cmd.Env = env

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("vault CLI error: %s", string(exitErr.Stderr))
		}
		return "", fmt.Errorf("failed to run vault CLI: %w", err)
	}

	// Parse JSON response
	var response struct {
		Data struct {
			Data map[string]interface{} `json:"data"`
		} `json:"data"`
	}

	if err := json.Unmarshal(output, &response); err != nil {
		return "", fmt.Errorf("failed to parse vault response: %w", err)
	}

	data := response.Data.Data

	// If field name specified, return that field
	if fieldName != "" {
		if value, ok := data[fieldName]; ok {
			if strValue, ok := value.(string); ok {
				return strValue, nil
			}
			// Try to JSON encode non-string values
			if jsonBytes, err := json.Marshal(value); err == nil {
				return string(jsonBytes), nil
			}
		}
		return "", fmt.Errorf("field %s not found in secret", fieldName)
	}

	// If only one field, return it
	if len(data) == 1 {
		for _, v := range data {
			if strValue, ok := v.(string); ok {
				return strValue, nil
			}
		}
	}

	// Return full JSON
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("failed to serialize secret data: %w", err)
	}
	return string(jsonBytes), nil
}

// ValidateConfig checks if Vault CLI is available and configured.
func (p *HashiCorpVaultProvider) ValidateConfig() error {
	// Check if vault CLI is available
	if _, err := exec.LookPath("vault"); err != nil {
		return fmt.Errorf("Vault CLI not found in PATH: %w", err)
	}

	// Check authentication
	cmd := exec.Command("vault", "token", "lookup", "-format=json")

	env := os.Environ()
	if p.Address != "" {
		env = append(env, "VAULT_ADDR="+p.Address)
	}
	if p.Token != "" {
		env = append(env, "VAULT_TOKEN="+p.Token)
	}
	if p.Namespace != "" {
		env = append(env, "VAULT_NAMESPACE="+p.Namespace)
	}
	cmd.Env = env

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("vault not authenticated or not reachable: %w", err)
	}

	return nil
}

// ListSecrets lists available secrets at a given path.
func (p *HashiCorpVaultProvider) ListSecrets(ctx context.Context, path string) ([]string, error) {
	if path == "" {
		path = p.Mount + "/metadata"
	}

	args := []string{"kv", "list", "-format=json", path}

	cmd := exec.CommandContext(ctx, "vault", args...)

	env := os.Environ()
	if p.Address != "" {
		env = append(env, "VAULT_ADDR="+p.Address)
	}
	if p.Token != "" {
		env = append(env, "VAULT_TOKEN="+p.Token)
	}
	if p.Namespace != "" {
		env = append(env, "VAULT_NAMESPACE="+p.Namespace)
	}
	cmd.Env = env

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list secrets: %w", err)
	}

	var keys []string
	if err := json.Unmarshal(output, &keys); err != nil {
		return nil, fmt.Errorf("failed to parse secret list: %w", err)
	}

	return keys, nil
}

// WriteSecret writes a secret to Vault.
func (p *HashiCorpVaultProvider) WriteSecret(ctx context.Context, path string, data map[string]string) error {
	fullPath := path
	if !strings.HasPrefix(path, p.Mount) {
		fullPath = p.Mount + "/" + path
	}

	args := []string{"kv", "put", fullPath}
	for k, v := range data {
		args = append(args, fmt.Sprintf("%s=%s", k, v))
	}

	cmd := exec.CommandContext(ctx, "vault", args...)

	env := os.Environ()
	if p.Address != "" {
		env = append(env, "VAULT_ADDR="+p.Address)
	}
	if p.Token != "" {
		env = append(env, "VAULT_TOKEN="+p.Token)
	}
	if p.Namespace != "" {
		env = append(env, "VAULT_NAMESPACE="+p.Namespace)
	}
	cmd.Env = env

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("failed to write secret: %s", string(exitErr.Stderr))
		}
		return fmt.Errorf("failed to write secret: %w", err)
	}

	return nil
}
