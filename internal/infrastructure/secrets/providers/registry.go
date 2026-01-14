package providers

import (
	"github.com/homeport/homeport/internal/domain/secrets"
	infraSecrets "github.com/homeport/homeport/internal/infrastructure/secrets"
)

// Registry manages all available secret providers.
type Registry struct {
	providers map[secrets.SecretSource]infraSecrets.Provider
}

// NewRegistry creates a new provider registry.
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[secrets.SecretSource]infraSecrets.Provider),
	}
}

// Register adds a provider to the registry.
func (r *Registry) Register(p infraSecrets.Provider) {
	r.providers[p.Name()] = p
}

// Get returns a provider by source type.
func (r *Registry) Get(source secrets.SecretSource) (infraSecrets.Provider, bool) {
	p, ok := r.providers[source]
	return p, ok
}

// List returns all registered provider source types.
func (r *Registry) List() []secrets.SecretSource {
	sources := make([]secrets.SecretSource, 0, len(r.providers))
	for source := range r.providers {
		sources = append(sources, source)
	}
	return sources
}

// RegisterAll registers all providers with a resolver.
func (r *Registry) RegisterAll(resolver *infraSecrets.Resolver) {
	for _, p := range r.providers {
		resolver.RegisterProvider(p)
	}
}

// DefaultRegistry creates a registry with all default providers.
func DefaultRegistry() *Registry {
	r := NewRegistry()

	// Register all providers
	r.Register(NewEnvProvider())
	r.Register(NewFileProvider())
	r.Register(NewManualProvider())
	r.Register(NewAWSSecretsManagerProvider())
	r.Register(NewGCPSecretManagerProvider())
	r.Register(NewAzureKeyVaultProvider())
	r.Register(NewHashiCorpVaultProvider())

	return r
}

// ConfiguredRegistry creates a registry with configured providers.
type RegistryConfig struct {
	// AWS configuration
	AWSProfile string
	AWSRegion  string

	// GCP configuration
	GCPProject string

	// Azure configuration
	AzureVault        string
	AzureSubscription string

	// HashiCorp Vault configuration
	VaultAddress   string
	VaultToken     string
	VaultNamespace string
	VaultMount     string

	// File provider configuration
	FileBasePath string

	// Env provider configuration
	EnvPrefix string
}

// ConfiguredRegistry creates a registry with providers configured from the given config.
func ConfiguredRegistry(cfg *RegistryConfig) *Registry {
	r := NewRegistry()

	// Environment provider
	envProvider := NewEnvProvider()
	if cfg.EnvPrefix != "" {
		envProvider.WithPrefix(cfg.EnvPrefix)
	}
	r.Register(envProvider)

	// File provider
	fileProvider := NewFileProvider()
	if cfg.FileBasePath != "" {
		fileProvider.WithBasePath(cfg.FileBasePath)
	}
	r.Register(fileProvider)

	// Manual provider (always available)
	r.Register(NewManualProvider())

	// AWS Secrets Manager
	awsProvider := NewAWSSecretsManagerProvider()
	if cfg.AWSProfile != "" {
		awsProvider.WithProfile(cfg.AWSProfile)
	}
	if cfg.AWSRegion != "" {
		awsProvider.WithRegion(cfg.AWSRegion)
	}
	r.Register(awsProvider)

	// GCP Secret Manager
	gcpProvider := NewGCPSecretManagerProvider()
	if cfg.GCPProject != "" {
		gcpProvider.WithProject(cfg.GCPProject)
	}
	r.Register(gcpProvider)

	// Azure Key Vault
	azureProvider := NewAzureKeyVaultProvider()
	if cfg.AzureVault != "" {
		azureProvider.WithVaultName(cfg.AzureVault)
	}
	if cfg.AzureSubscription != "" {
		azureProvider.WithSubscription(cfg.AzureSubscription)
	}
	r.Register(azureProvider)

	// HashiCorp Vault
	vaultProvider := NewHashiCorpVaultProvider()
	if cfg.VaultAddress != "" {
		vaultProvider.WithAddress(cfg.VaultAddress)
	}
	if cfg.VaultToken != "" {
		vaultProvider.WithToken(cfg.VaultToken)
	}
	if cfg.VaultNamespace != "" {
		vaultProvider.WithNamespace(cfg.VaultNamespace)
	}
	if cfg.VaultMount != "" {
		vaultProvider.WithMount(cfg.VaultMount)
	}
	r.Register(vaultProvider)

	return r
}

// ProviderInfo contains information about a registered provider.
type ProviderInfo struct {
	Source      secrets.SecretSource
	Available   bool
	ConfigError error
}

// CheckAvailability checks which providers are properly configured.
func (r *Registry) CheckAvailability() []ProviderInfo {
	var infos []ProviderInfo

	for source, provider := range r.providers {
		info := ProviderInfo{
			Source: source,
		}

		err := provider.ValidateConfig()
		if err == nil {
			info.Available = true
		} else {
			info.ConfigError = err
		}

		infos = append(infos, info)
	}

	return infos
}

// GetAvailable returns only providers that are properly configured.
func (r *Registry) GetAvailable() []infraSecrets.Provider {
	var available []infraSecrets.Provider

	for _, provider := range r.providers {
		if err := provider.ValidateConfig(); err == nil {
			available = append(available, provider)
		}
	}

	return available
}
