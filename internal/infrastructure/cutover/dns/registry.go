package dns

import (
	"fmt"
	"sync"

	"github.com/homeport/homeport/internal/domain/cutover"
)

// Registry manages DNS provider registrations.
type Registry struct {
	providers map[string]cutover.DNSProvider
	mu        sync.RWMutex
}

var (
	defaultRegistry *Registry
	once            sync.Once
)

// DefaultRegistry returns the default DNS provider registry.
func DefaultRegistry() *Registry {
	once.Do(func() {
		defaultRegistry = NewRegistry()
		// Register the manual provider by default
		defaultRegistry.Register("manual", NewManualProvider())
	})
	return defaultRegistry
}

// NewRegistry creates a new DNS provider registry.
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]cutover.DNSProvider),
	}
}

// Register adds a DNS provider to the registry.
func (r *Registry) Register(name string, provider cutover.DNSProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[name] = provider
}

// Get retrieves a DNS provider by name.
func (r *Registry) Get(name string) (cutover.DNSProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	provider, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("DNS provider not found: %s", name)
	}
	return provider, nil
}

// Has checks if a provider is registered.
func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.providers[name]
	return ok
}

// List returns all registered provider names.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}

// CreateProvider creates and returns a DNS provider based on configuration.
func CreateProvider(providerType string, config *cutover.DNSProviderConfig) (cutover.DNSProvider, error) {
	switch cutover.DNSProviderType(providerType) {
	case cutover.DNSProviderManual:
		return NewManualProvider(), nil

	case cutover.DNSProviderCloudflare:
		if config == nil || config.APIToken == "" {
			return nil, fmt.Errorf("cloudflare provider requires API token")
		}
		if config.ZoneID == "" {
			return nil, fmt.Errorf("cloudflare provider requires zone ID")
		}
		return NewCloudflareProvider(&CloudflareConfig{
			APIToken: config.APIToken,
			ZoneID:   config.ZoneID,
		}), nil

	case cutover.DNSProviderRoute53:
		if config == nil || config.ZoneID == "" {
			return nil, fmt.Errorf("route53 provider requires hosted zone ID")
		}
		return NewRoute53Provider(&Route53Config{
			HostedZoneID:    config.ZoneID,
			Region:          config.Region,
			AccessKeyID:     config.APIKey,
			SecretAccessKey: config.APISecret,
			SessionToken:    config.APIToken,
		}), nil

	default:
		return nil, fmt.Errorf("unsupported DNS provider: %s", providerType)
	}
}

// SupportedProviders returns the list of supported DNS providers.
func SupportedProviders() []string {
	return []string{
		string(cutover.DNSProviderManual),
		string(cutover.DNSProviderCloudflare),
		string(cutover.DNSProviderRoute53),
	}
}
