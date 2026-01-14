// Package secrets defines domain types for secret handling during migrations.
// Bundles NEVER contain secret values - only references to secrets.
// Secrets are resolved at import/deploy time via various providers.
package secrets

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// SecretSource represents the type of source for a secret.
type SecretSource string

const (
	// SourceManual requires user to provide the value interactively.
	SourceManual SecretSource = "manual"

	// SourceEnv reads from an environment variable.
	SourceEnv SecretSource = "env"

	// SourceFile reads from a local file.
	SourceFile SecretSource = "file"

	// SourceAWSSecretsManager pulls from AWS Secrets Manager.
	SourceAWSSecretsManager SecretSource = "aws-secrets-manager"

	// SourceGCPSecretManager pulls from GCP Secret Manager.
	SourceGCPSecretManager SecretSource = "gcp-secret-manager"

	// SourceAzureKeyVault pulls from Azure Key Vault.
	SourceAzureKeyVault SecretSource = "azure-key-vault"

	// SourceHashiCorpVault pulls from HashiCorp Vault.
	SourceHashiCorpVault SecretSource = "hashicorp-vault"
)

// String returns the string representation of the secret source.
func (s SecretSource) String() string {
	return string(s)
}

// IsValid checks if the secret source is a recognized type.
func (s SecretSource) IsValid() bool {
	switch s {
	case SourceManual, SourceEnv, SourceFile,
		SourceAWSSecretsManager, SourceGCPSecretManager,
		SourceAzureKeyVault, SourceHashiCorpVault:
		return true
	default:
		return false
	}
}

// IsCloudProvider returns true if the source is a cloud secret manager.
func (s SecretSource) IsCloudProvider() bool {
	switch s {
	case SourceAWSSecretsManager, SourceGCPSecretManager, SourceAzureKeyVault:
		return true
	default:
		return false
	}
}

// RequiresCredentials returns true if the source needs credentials to access.
func (s SecretSource) RequiresCredentials() bool {
	switch s {
	case SourceAWSSecretsManager, SourceGCPSecretManager,
		SourceAzureKeyVault, SourceHashiCorpVault:
		return true
	default:
		return false
	}
}

// SecretReference describes a secret that must be provided at import time.
// CRITICAL: This contains references only, NEVER actual secret values.
type SecretReference struct {
	// Name is the environment variable name or identifier for this secret.
	// Example: "DATABASE_PASSWORD", "API_KEY", "TLS_CERT"
	Name string `json:"name"`

	// Source indicates where this secret should be retrieved from.
	Source SecretSource `json:"source"`

	// Key is the key/path to the secret in the source.
	// For aws-secrets-manager: "prod/myapp/db-password"
	// For env: "MY_DB_PASSWORD" (environment variable name)
	// For file: "/path/to/secret.txt"
	// For vault: "secret/data/myapp/db"
	Key string `json:"key,omitempty"`

	// Description provides human-readable context about the secret.
	Description string `json:"description,omitempty"`

	// Required indicates if this secret must be provided for deployment.
	Required bool `json:"required"`

	// UsedBy lists the services/containers that use this secret.
	UsedBy []string `json:"used_by,omitempty"`

	// Type indicates the secret type for validation purposes.
	Type SecretType `json:"type,omitempty"`

	// Version specifies a specific version of the secret (for versioned stores).
	Version string `json:"version,omitempty"`

	// Encoding specifies how the secret value is encoded (base64, plain).
	Encoding SecretEncoding `json:"encoding,omitempty"`
}

// SecretType categorizes secrets for validation and handling.
type SecretType string

const (
	// TypePassword is a password or passphrase.
	TypePassword SecretType = "password"

	// TypeAPIKey is an API key or token.
	TypeAPIKey SecretType = "api_key"

	// TypeCertificate is a TLS/SSL certificate.
	TypeCertificate SecretType = "certificate"

	// TypePrivateKey is a private key (SSH, TLS, etc.).
	TypePrivateKey SecretType = "private_key"

	// TypeConnectionString is a database or service connection string.
	TypeConnectionString SecretType = "connection_string"

	// TypeGeneric is any other type of secret.
	TypeGeneric SecretType = "generic"
)

// SecretEncoding specifies how secret values are encoded.
type SecretEncoding string

const (
	// EncodingPlain indicates the secret is plain text.
	EncodingPlain SecretEncoding = "plain"

	// EncodingBase64 indicates the secret is base64 encoded.
	EncodingBase64 SecretEncoding = "base64"
)

// NewSecretReference creates a new secret reference with defaults.
func NewSecretReference(name string, source SecretSource) *SecretReference {
	return &SecretReference{
		Name:     name,
		Source:   source,
		Required: true,
		Type:     TypeGeneric,
		Encoding: EncodingPlain,
		UsedBy:   make([]string, 0),
	}
}

// WithKey sets the key/path for the secret.
func (r *SecretReference) WithKey(key string) *SecretReference {
	r.Key = key
	return r
}

// WithDescription sets the description.
func (r *SecretReference) WithDescription(desc string) *SecretReference {
	r.Description = desc
	return r
}

// WithType sets the secret type.
func (r *SecretReference) WithType(t SecretType) *SecretReference {
	r.Type = t
	return r
}

// Optional marks the secret as not required.
func (r *SecretReference) Optional() *SecretReference {
	r.Required = false
	return r
}

// AddUsedBy adds a service that uses this secret.
func (r *SecretReference) AddUsedBy(service string) *SecretReference {
	r.UsedBy = append(r.UsedBy, service)
	return r
}

// Validate checks if the secret reference is valid.
func (r *SecretReference) Validate() error {
	if r.Name == "" {
		return ErrEmptySecretName
	}

	if !isValidSecretName(r.Name) {
		return fmt.Errorf("%w: %s", ErrInvalidSecretName, r.Name)
	}

	if !r.Source.IsValid() {
		return fmt.Errorf("%w: %s", ErrInvalidSecretSource, r.Source)
	}

	// Key is required for non-manual sources
	if r.Source != SourceManual && r.Key == "" {
		return fmt.Errorf("%w: key required for source %s", ErrMissingSecretKey, r.Source)
	}

	return nil
}

// isValidSecretName checks if a secret name is valid (alphanumeric + underscore).
var secretNameRegex = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

func isValidSecretName(name string) bool {
	return secretNameRegex.MatchString(name)
}

// SecretsManifest contains all secret references for a bundle.
// This is stored in secrets/secrets-manifest.json within the bundle.
type SecretsManifest struct {
	// Version is the manifest format version.
	Version string `json:"version"`

	// Secrets is the list of secret references.
	Secrets []*SecretReference `json:"secrets"`

	// EnvTemplate is the path to the .env.template file.
	EnvTemplate string `json:"env_template,omitempty"`

	// RequiredCount is the number of required secrets.
	RequiredCount int `json:"required_count"`

	// Sources lists all unique sources referenced.
	Sources []SecretSource `json:"sources,omitempty"`
}

// SecretsManifestVersion is the current secrets manifest version.
const SecretsManifestVersion = "1.0.0"

// NewSecretsManifest creates a new secrets manifest.
func NewSecretsManifest() *SecretsManifest {
	return &SecretsManifest{
		Version: SecretsManifestVersion,
		Secrets: make([]*SecretReference, 0),
		Sources: make([]SecretSource, 0),
	}
}

// AddSecret adds a secret reference to the manifest.
func (m *SecretsManifest) AddSecret(ref *SecretReference) error {
	if err := ref.Validate(); err != nil {
		return fmt.Errorf("invalid secret reference: %w", err)
	}

	// Check for duplicate names
	for _, existing := range m.Secrets {
		if existing.Name == ref.Name {
			return fmt.Errorf("%w: %s", ErrDuplicateSecretName, ref.Name)
		}
	}

	m.Secrets = append(m.Secrets, ref)

	if ref.Required {
		m.RequiredCount++
	}

	// Track unique sources
	if !m.hasSource(ref.Source) {
		m.Sources = append(m.Sources, ref.Source)
	}

	return nil
}

// hasSource checks if a source is already tracked.
func (m *SecretsManifest) hasSource(source SecretSource) bool {
	for _, s := range m.Sources {
		if s == source {
			return true
		}
	}
	return false
}

// GetSecret returns a secret reference by name.
func (m *SecretsManifest) GetSecret(name string) (*SecretReference, bool) {
	for _, s := range m.Secrets {
		if s.Name == name {
			return s, true
		}
	}
	return nil, false
}

// GetRequired returns all required secret references.
func (m *SecretsManifest) GetRequired() []*SecretReference {
	var required []*SecretReference
	for _, s := range m.Secrets {
		if s.Required {
			required = append(required, s)
		}
	}
	return required
}

// GetBySource returns all secrets from a specific source.
func (m *SecretsManifest) GetBySource(source SecretSource) []*SecretReference {
	var result []*SecretReference
	for _, s := range m.Secrets {
		if s.Source == source {
			result = append(result, s)
		}
	}
	return result
}

// GetCloudSecrets returns secrets that need to be pulled from cloud providers.
func (m *SecretsManifest) GetCloudSecrets() []*SecretReference {
	var result []*SecretReference
	for _, s := range m.Secrets {
		if s.Source.IsCloudProvider() {
			result = append(result, s)
		}
	}
	return result
}

// Validate checks if all secret references are valid.
func (m *SecretsManifest) Validate() error {
	seen := make(map[string]bool)

	for _, s := range m.Secrets {
		if err := s.Validate(); err != nil {
			return fmt.Errorf("secret %s: %w", s.Name, err)
		}

		if seen[s.Name] {
			return fmt.Errorf("%w: %s", ErrDuplicateSecretName, s.Name)
		}
		seen[s.Name] = true
	}

	return nil
}

// GenerateEnvTemplate generates the content for .env.template file.
// This contains placeholders and comments for all secrets.
func (m *SecretsManifest) GenerateEnvTemplate() string {
	var sb strings.Builder

	sb.WriteString("# Environment Variables Template\n")
	sb.WriteString("# Generated by Homeport - DO NOT commit actual values\n")
	sb.WriteString("#\n")
	sb.WriteString("# Instructions:\n")
	sb.WriteString("# 1. Copy this file to .env\n")
	sb.WriteString("# 2. Fill in the actual secret values\n")
	sb.WriteString("# 3. Keep .env out of version control\n")
	sb.WriteString("#\n\n")

	for _, s := range m.Secrets {
		if s.Description != "" {
			sb.WriteString(fmt.Sprintf("# %s\n", s.Description))
		}

		if s.Required {
			sb.WriteString("# REQUIRED\n")
		} else {
			sb.WriteString("# OPTIONAL\n")
		}

		if s.Source != SourceManual {
			sb.WriteString(fmt.Sprintf("# Source: %s\n", s.Source))
			if s.Key != "" {
				sb.WriteString(fmt.Sprintf("# Key: %s\n", s.Key))
			}
		}

		sb.WriteString(fmt.Sprintf("%s=\n\n", s.Name))
	}

	return sb.String()
}

// ResolvedSecret represents a secret that has been resolved to its actual value.
// This is used at deploy time and should NEVER be persisted.
type ResolvedSecret struct {
	// Reference is the original secret reference.
	Reference *SecretReference

	// Value is the actual secret value.
	// WARNING: This should never be logged or persisted.
	Value string

	// ResolvedFrom indicates how/where the secret was resolved.
	ResolvedFrom string

	// ResolvedAt is when the secret was resolved.
	ResolvedAt string
}

// ResolvedSecrets is a collection of resolved secrets.
type ResolvedSecrets struct {
	secrets map[string]*ResolvedSecret
}

// NewResolvedSecrets creates a new resolved secrets collection.
func NewResolvedSecrets() *ResolvedSecrets {
	return &ResolvedSecrets{
		secrets: make(map[string]*ResolvedSecret),
	}
}

// Add adds a resolved secret.
func (rs *ResolvedSecrets) Add(name, value, resolvedFrom string, ref *SecretReference) {
	rs.secrets[name] = &ResolvedSecret{
		Reference:    ref,
		Value:        value,
		ResolvedFrom: resolvedFrom,
	}
}

// Get returns a resolved secret by name.
func (rs *ResolvedSecrets) Get(name string) (*ResolvedSecret, bool) {
	s, ok := rs.secrets[name]
	return s, ok
}

// GetValue returns just the value of a resolved secret.
func (rs *ResolvedSecrets) GetValue(name string) (string, bool) {
	s, ok := rs.secrets[name]
	if !ok {
		return "", false
	}
	return s.Value, true
}

// Has checks if a secret is resolved.
func (rs *ResolvedSecrets) Has(name string) bool {
	_, ok := rs.secrets[name]
	return ok
}

// Names returns all resolved secret names.
func (rs *ResolvedSecrets) Names() []string {
	names := make([]string, 0, len(rs.secrets))
	for name := range rs.secrets {
		names = append(names, name)
	}
	return names
}

// Count returns the number of resolved secrets.
func (rs *ResolvedSecrets) Count() int {
	return len(rs.secrets)
}

// ToEnvMap converts resolved secrets to an environment variable map.
func (rs *ResolvedSecrets) ToEnvMap() map[string]string {
	env := make(map[string]string, len(rs.secrets))
	for name, s := range rs.secrets {
		env[name] = s.Value
	}
	return env
}

// ToEnvFile generates the content for a .env file.
func (rs *ResolvedSecrets) ToEnvFile() string {
	var sb strings.Builder
	sb.WriteString("# Generated by Homeport\n")
	sb.WriteString("# WARNING: This file contains sensitive values\n\n")

	for name, s := range rs.secrets {
		// Escape special characters in values
		value := escapeEnvValue(s.Value)
		sb.WriteString(fmt.Sprintf("%s=%s\n", name, value))
	}

	return sb.String()
}

// escapeEnvValue escapes a value for use in a .env file.
func escapeEnvValue(value string) string {
	// If value contains special characters, quote it
	if strings.ContainsAny(value, " \t\n\r\"'`$\\") {
		// Use double quotes and escape internal quotes and backslashes
		value = strings.ReplaceAll(value, "\\", "\\\\")
		value = strings.ReplaceAll(value, "\"", "\\\"")
		value = strings.ReplaceAll(value, "$", "\\$")
		return fmt.Sprintf("\"%s\"", value)
	}
	return value
}

// Clear securely clears all resolved secrets from memory.
func (rs *ResolvedSecrets) Clear() {
	for name, s := range rs.secrets {
		// Overwrite the value before clearing
		s.Value = strings.Repeat("0", len(s.Value))
		s.Reference = nil
		delete(rs.secrets, name)
	}
}

// Common errors for secrets.
var (
	ErrEmptySecretName     = errors.New("secret name cannot be empty")
	ErrInvalidSecretName   = errors.New("invalid secret name: must be uppercase alphanumeric with underscores")
	ErrInvalidSecretSource = errors.New("invalid secret source")
	ErrMissingSecretKey    = errors.New("secret key is required for this source")
	ErrDuplicateSecretName = errors.New("duplicate secret name")
	ErrSecretNotFound      = errors.New("secret not found")
	ErrSecretNotResolved   = errors.New("required secret not resolved")
	ErrResolverNotFound    = errors.New("no resolver found for secret source")
)
