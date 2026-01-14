package providers

import (
	"context"
	"fmt"
	"os"

	"github.com/homeport/homeport/internal/domain/secrets"
)

// EnvProvider resolves secrets from environment variables.
type EnvProvider struct {
	// Prefix is an optional prefix to add to environment variable names.
	Prefix string
}

// NewEnvProvider creates a new environment variable provider.
func NewEnvProvider() *EnvProvider {
	return &EnvProvider{}
}

// WithPrefix sets the environment variable prefix.
func (p *EnvProvider) WithPrefix(prefix string) *EnvProvider {
	p.Prefix = prefix
	return p
}

// Name returns the provider identifier.
func (p *EnvProvider) Name() secrets.SecretSource {
	return secrets.SourceEnv
}

// CanResolve checks if this provider can handle the secret reference.
func (p *EnvProvider) CanResolve(ref *secrets.SecretReference) bool {
	// Env provider can resolve any secret with source "env" or with a key set
	if ref.Source != secrets.SourceEnv {
		return false
	}
	return ref.Key != "" || ref.Name != ""
}

// Resolve retrieves a secret from environment variables.
func (p *EnvProvider) Resolve(ctx context.Context, ref *secrets.SecretReference) (string, error) {
	if !p.CanResolve(ref) {
		return "", fmt.Errorf("cannot resolve secret %s: invalid source", ref.Name)
	}

	// Try the key first (explicit env var name)
	if ref.Key != "" {
		if value := os.Getenv(ref.Key); value != "" {
			return value, nil
		}
		// Try with prefix
		if p.Prefix != "" {
			if value := os.Getenv(p.Prefix + ref.Key); value != "" {
				return value, nil
			}
		}
	}

	// Fall back to the secret name
	if value := os.Getenv(ref.Name); value != "" {
		return value, nil
	}

	// Try with prefix
	if p.Prefix != "" {
		if value := os.Getenv(p.Prefix + ref.Name); value != "" {
			return value, nil
		}
	}

	return "", fmt.Errorf("environment variable not set for secret %s", ref.Name)
}

// ValidateConfig always returns nil as env provider requires no configuration.
func (p *EnvProvider) ValidateConfig() error {
	return nil
}

// GetEnvName returns the environment variable name that would be used for a secret.
func (p *EnvProvider) GetEnvName(ref *secrets.SecretReference) string {
	if ref.Key != "" {
		if p.Prefix != "" {
			return p.Prefix + ref.Key
		}
		return ref.Key
	}
	if p.Prefix != "" {
		return p.Prefix + ref.Name
	}
	return ref.Name
}

// ListAvailable returns environment variables that match known patterns.
func (p *EnvProvider) ListAvailable(refs []*secrets.SecretReference) map[string]bool {
	result := make(map[string]bool)

	for _, ref := range refs {
		if p.CanResolve(ref) {
			_, err := p.Resolve(context.Background(), ref)
			result[ref.Name] = err == nil
		}
	}

	return result
}
