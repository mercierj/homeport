// Package secrets provides secret resolution for HPRT bundle imports.
// Secrets are resolved at import/deploy time from various sources.
// Bundle files NEVER contain actual secret values - only references.
package secrets

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/secrets"
)

// Provider defines the interface for secret resolution providers.
// Each provider knows how to retrieve secrets from a specific source.
type Provider interface {
	// Name returns the provider's unique identifier.
	Name() secrets.SecretSource

	// CanResolve checks if this provider can resolve the given secret reference.
	CanResolve(ref *secrets.SecretReference) bool

	// Resolve retrieves the secret value for the given reference.
	// Returns the secret value and the source it was resolved from.
	Resolve(ctx context.Context, ref *secrets.SecretReference) (string, error)

	// ValidateConfig checks if the provider is properly configured.
	ValidateConfig() error
}

// Resolver orchestrates secret resolution from multiple providers.
type Resolver struct {
	providers map[secrets.SecretSource]Provider

	// Options for resolution behavior.
	options *ResolverOptions

	// Interactive prompt function for manual secrets.
	promptFunc PromptFunc
}

// ResolverOptions configures the resolver behavior.
type ResolverOptions struct {
	// SecretsFilePath is the path to a .env file with secret values.
	SecretsFilePath string

	// PullFrom specifies a cloud provider to pull secrets from.
	PullFrom string

	// EnvPrefix is the prefix for environment variables (default: "HOMEPORT_SECRET_").
	EnvPrefix string

	// AllowInteractive enables interactive prompting for missing secrets.
	AllowInteractive bool

	// FailOnMissing causes resolution to fail if any required secret is missing.
	FailOnMissing bool

	// Timeout for provider operations.
	Timeout time.Duration
}

// DefaultResolverOptions returns sensible defaults.
func DefaultResolverOptions() *ResolverOptions {
	return &ResolverOptions{
		EnvPrefix:        "HOMEPORT_SECRET_",
		AllowInteractive: true,
		FailOnMissing:    true,
		Timeout:          30 * time.Second,
	}
}

// PromptFunc is a function type for prompting users for secret values.
type PromptFunc func(ref *secrets.SecretReference) (string, error)

// NewResolver creates a new secret resolver.
func NewResolver(opts *ResolverOptions) *Resolver {
	if opts == nil {
		opts = DefaultResolverOptions()
	}

	return &Resolver{
		providers: make(map[secrets.SecretSource]Provider),
		options:   opts,
	}
}

// RegisterProvider adds a provider to the resolver.
func (r *Resolver) RegisterProvider(p Provider) {
	r.providers[p.Name()] = p
}

// SetPromptFunc sets the interactive prompt function.
func (r *Resolver) SetPromptFunc(fn PromptFunc) {
	r.promptFunc = fn
}

// ResolveAll resolves all secrets from a manifest.
// Returns resolved secrets and any errors encountered.
func (r *Resolver) ResolveAll(ctx context.Context, manifest *secrets.SecretsManifest) (*secrets.ResolvedSecrets, error) {
	resolved := secrets.NewResolvedSecrets()

	// Track unresolved required secrets
	var unresolvedRequired []string

	for _, ref := range manifest.Secrets {
		value, source, err := r.resolveOne(ctx, ref)
		if err != nil {
			if ref.Required {
				unresolvedRequired = append(unresolvedRequired, ref.Name)
			}
			continue
		}

		resolved.Add(ref.Name, value, source, ref)
	}

	// Check for missing required secrets
	if r.options.FailOnMissing && len(unresolvedRequired) > 0 {
		return resolved, &ResolutionError{
			MissingSecrets: unresolvedRequired,
		}
	}

	return resolved, nil
}

// Resolve resolves a single secret reference.
func (r *Resolver) Resolve(ctx context.Context, ref *secrets.SecretReference) (string, error) {
	value, _, err := r.resolveOne(ctx, ref)
	return value, err
}

// resolveOne attempts to resolve a single secret using the resolution chain.
// Resolution order:
// 1. Secrets file (--secrets-file)
// 2. Pull from cloud provider (--pull-secrets-from)
// 3. Environment variables (HOMEPORT_SECRET_*)
// 4. Registered provider for the source type
// 5. Interactive prompt (if enabled)
func (r *Resolver) resolveOne(ctx context.Context, ref *secrets.SecretReference) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.options.Timeout)
	defer cancel()

	// 1. Try secrets file
	if r.options.SecretsFilePath != "" {
		if value, ok := r.resolveFromFile(ref); ok {
			return value, "file:" + r.options.SecretsFilePath, nil
		}
	}

	// 2. Try environment variables
	if value, ok := r.resolveFromEnv(ref); ok {
		return value, "env:" + r.options.EnvPrefix + ref.Name, nil
	}

	// 3. Try cloud provider pull (if specified)
	if r.options.PullFrom != "" && ref.Source.IsCloudProvider() {
		if value, err := r.resolveFromCloud(ctx, ref); err == nil {
			return value, "cloud:" + r.options.PullFrom, nil
		}
	}

	// 4. Try registered provider for this source type
	if provider, ok := r.providers[ref.Source]; ok {
		if provider.CanResolve(ref) {
			if value, err := provider.Resolve(ctx, ref); err == nil {
				return value, "provider:" + string(ref.Source), nil
			}
		}
	}

	// 5. Try interactive prompt
	if r.options.AllowInteractive && r.promptFunc != nil {
		if value, err := r.promptFunc(ref); err == nil && value != "" {
			return value, "interactive", nil
		}
	}

	return "", "", fmt.Errorf("%w: %s", secrets.ErrSecretNotResolved, ref.Name)
}

// resolveFromFile attempts to resolve a secret from the secrets file.
func (r *Resolver) resolveFromFile(ref *secrets.SecretReference) (string, bool) {
	content, err := os.ReadFile(r.options.SecretsFilePath)
	if err != nil {
		return "", false
	}

	env := parseEnvFile(string(content))
	if value, ok := env[ref.Name]; ok && value != "" {
		return value, true
	}

	return "", false
}

// resolveFromEnv attempts to resolve a secret from environment variables.
func (r *Resolver) resolveFromEnv(ref *secrets.SecretReference) (string, bool) {
	// Try with prefix first
	envName := r.options.EnvPrefix + ref.Name
	if value := os.Getenv(envName); value != "" {
		return value, true
	}

	// Try direct name if source is env
	if ref.Source == secrets.SourceEnv && ref.Key != "" {
		if value := os.Getenv(ref.Key); value != "" {
			return value, true
		}
	}

	return "", false
}

// resolveFromCloud attempts to resolve a secret from the specified cloud provider.
func (r *Resolver) resolveFromCloud(ctx context.Context, ref *secrets.SecretReference) (string, error) {
	// Map PullFrom to SecretSource
	var source secrets.SecretSource
	switch strings.ToLower(r.options.PullFrom) {
	case "aws":
		source = secrets.SourceAWSSecretsManager
	case "gcp":
		source = secrets.SourceGCPSecretManager
	case "azure":
		source = secrets.SourceAzureKeyVault
	default:
		return "", fmt.Errorf("unknown cloud provider: %s", r.options.PullFrom)
	}

	provider, ok := r.providers[source]
	if !ok {
		return "", fmt.Errorf("no provider registered for %s", source)
	}

	return provider.Resolve(ctx, ref)
}

// CheckResolvability checks which secrets can be resolved without actually resolving them.
func (r *Resolver) CheckResolvability(ctx context.Context, manifest *secrets.SecretsManifest) *ResolvabilityReport {
	report := &ResolvabilityReport{
		Secrets: make(map[string]ResolvabilityStatus),
	}

	for _, ref := range manifest.Secrets {
		status := r.checkOneResolvability(ref)
		report.Secrets[ref.Name] = status

		switch status.Status {
		case StatusResolvable:
			report.Resolvable = append(report.Resolvable, ref.Name)
		case StatusMaybeResolvable:
			report.MaybeResolvable = append(report.MaybeResolvable, ref.Name)
		case StatusNeedsInteractive:
			report.NeedsInteractive = append(report.NeedsInteractive, ref.Name)
		case StatusUnresolvable:
			report.Unresolvable = append(report.Unresolvable, ref.Name)
		}
	}

	return report
}

// checkOneResolvability checks if a secret can likely be resolved.
func (r *Resolver) checkOneResolvability(ref *secrets.SecretReference) ResolvabilityStatus {
	// Check secrets file
	if r.options.SecretsFilePath != "" {
		if _, ok := r.resolveFromFile(ref); ok {
			return ResolvabilityStatus{
				Status: StatusResolvable,
				Method: "secrets-file",
			}
		}
	}

	// Check environment
	if _, ok := r.resolveFromEnv(ref); ok {
		return ResolvabilityStatus{
			Status: StatusResolvable,
			Method: "environment",
		}
	}

	// Check if provider is available
	if provider, ok := r.providers[ref.Source]; ok {
		if err := provider.ValidateConfig(); err == nil {
			return ResolvabilityStatus{
				Status: StatusMaybeResolvable,
				Method: string(ref.Source),
			}
		}
	}

	// Check if interactive is available
	if r.options.AllowInteractive && r.promptFunc != nil {
		return ResolvabilityStatus{
			Status: StatusNeedsInteractive,
			Method: "interactive-prompt",
		}
	}

	return ResolvabilityStatus{
		Status: StatusUnresolvable,
		Method: "none",
	}
}

// ResolvabilityReport describes which secrets can be resolved.
type ResolvabilityReport struct {
	Secrets          map[string]ResolvabilityStatus
	Resolvable       []string
	MaybeResolvable  []string
	NeedsInteractive []string
	Unresolvable     []string
}

// CanResolveAll returns true if all required secrets can be resolved.
func (r *ResolvabilityReport) CanResolveAll(manifest *secrets.SecretsManifest) bool {
	for _, ref := range manifest.GetRequired() {
		status := r.Secrets[ref.Name]
		if status.Status == StatusUnresolvable {
			return false
		}
	}
	return true
}

// ResolvabilityStatus describes the resolvability of a single secret.
type ResolvabilityStatus struct {
	Status ResolvabilityStatusType
	Method string
}

// ResolvabilityStatusType categorizes resolvability.
type ResolvabilityStatusType string

const (
	// StatusResolvable means the secret is definitely resolvable.
	StatusResolvable ResolvabilityStatusType = "resolvable"

	// StatusMaybeResolvable means the secret might be resolvable (provider available).
	StatusMaybeResolvable ResolvabilityStatusType = "maybe"

	// StatusNeedsInteractive means the secret requires interactive input.
	StatusNeedsInteractive ResolvabilityStatusType = "interactive"

	// StatusUnresolvable means the secret cannot be resolved.
	StatusUnresolvable ResolvabilityStatusType = "unresolvable"
)

// ResolutionError represents an error during secret resolution.
type ResolutionError struct {
	MissingSecrets []string
}

func (e *ResolutionError) Error() string {
	return fmt.Sprintf("failed to resolve %d required secrets: %s",
		len(e.MissingSecrets), strings.Join(e.MissingSecrets, ", "))
}

// parseEnvFile parses a .env file content into a map.
func parseEnvFile(content string) map[string]string {
	env := make(map[string]string)
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Find the first =
		idx := strings.Index(line, "=")
		if idx == -1 {
			continue
		}

		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])

		// Remove surrounding quotes if present
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}

		// Unescape common escape sequences
		value = strings.ReplaceAll(value, "\\n", "\n")
		value = strings.ReplaceAll(value, "\\t", "\t")
		value = strings.ReplaceAll(value, "\\\"", "\"")
		value = strings.ReplaceAll(value, "\\'", "'")
		value = strings.ReplaceAll(value, "\\\\", "\\")

		env[key] = value
	}

	return env
}

// CreateEnvFile generates a .env file from resolved secrets.
func CreateEnvFile(resolved *secrets.ResolvedSecrets, outputPath string) error {
	content := resolved.ToEnvFile()
	return os.WriteFile(outputPath, []byte(content), 0600) // Restrictive permissions
}

// CreateEnvTemplate generates a .env.template file from a manifest.
func CreateEnvTemplate(manifest *secrets.SecretsManifest, outputPath string) error {
	content := manifest.GenerateEnvTemplate()
	return os.WriteFile(outputPath, []byte(content), 0644)
}
