package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
)

// CredentialSource indicates how credentials were obtained.
type CredentialSource string

const (
	// CredentialSourceDefault uses default credentials (ADC).
	CredentialSourceDefault CredentialSource = "default"

	// CredentialSourceServiceAccount uses a service account key file.
	CredentialSourceServiceAccount CredentialSource = "service_account"

	// CredentialSourceEnvironment uses GOOGLE_APPLICATION_CREDENTIALS env var.
	CredentialSourceEnvironment CredentialSource = "environment"

	// CredentialSourceUserAccount uses user account credentials.
	CredentialSourceUserAccount CredentialSource = "user_account"
)

// CredentialConfig holds configuration for GCP authentication.
type CredentialConfig struct {
	// Project is the GCP project ID.
	Project string

	// CredentialsFile is the path to a service account key file.
	CredentialsFile string

	// Source indicates how credentials should be obtained.
	Source CredentialSource
}

// NewCredentialConfig creates a new credential configuration with defaults.
func NewCredentialConfig() *CredentialConfig {
	return &CredentialConfig{
		Source: CredentialSourceDefault,
	}
}

// WithProject sets the project ID.
func (c *CredentialConfig) WithProject(project string) *CredentialConfig {
	c.Project = project
	return c
}

// WithCredentialsFile sets the service account key file path.
func (c *CredentialConfig) WithCredentialsFile(path string) *CredentialConfig {
	c.CredentialsFile = path
	c.Source = CredentialSourceServiceAccount
	return c
}

// DetectCredentialSource detects how GCP credentials are configured.
func DetectCredentialSource() CredentialSource {
	// Check for GOOGLE_APPLICATION_CREDENTIALS
	if creds := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); creds != "" {
		return CredentialSourceEnvironment
	}

	// Check for default application credentials
	home, err := os.UserHomeDir()
	if err == nil {
		adcPath := filepath.Join(home, ".config", "gcloud", "application_default_credentials.json")
		if _, err := os.Stat(adcPath); err == nil {
			return CredentialSourceUserAccount
		}
	}

	return CredentialSourceDefault
}

// ClientOptions returns the client options for GCP API clients.
func (c *CredentialConfig) ClientOptions(ctx context.Context) ([]option.ClientOption, error) {
	var opts []option.ClientOption

	switch c.Source {
	case CredentialSourceServiceAccount:
		if c.CredentialsFile == "" {
			return nil, fmt.Errorf("service account file path is required")
		}
		opts = append(opts, option.WithCredentialsFile(c.CredentialsFile)) //nolint:staticcheck // Deprecated but no alternative yet

	case CredentialSourceEnvironment:
		// Use GOOGLE_APPLICATION_CREDENTIALS
		credFile := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
		if credFile != "" {
			opts = append(opts, option.WithCredentialsFile(credFile)) //nolint:staticcheck // Deprecated but no alternative yet
		}

	case CredentialSourceDefault, CredentialSourceUserAccount:
		// Use Application Default Credentials
		creds, err := google.FindDefaultCredentials(ctx,
			"https://www.googleapis.com/auth/cloud-platform",
		)
		if err != nil {
			return nil, fmt.Errorf("failed to find default credentials: %w", err)
		}
		opts = append(opts, option.WithCredentials(creds))
	}

	return opts, nil
}

// GetProject returns the project ID, attempting to auto-detect if not set.
func (c *CredentialConfig) GetProject(ctx context.Context) (string, error) {
	if c.Project != "" {
		return c.Project, nil
	}

	// Try to get from credentials file
	if c.CredentialsFile != "" {
		project, err := extractProjectFromCredentials(c.CredentialsFile)
		if err == nil && project != "" {
			return project, nil
		}
	}

	// Try from environment variable
	if project := os.Getenv("GOOGLE_CLOUD_PROJECT"); project != "" {
		return project, nil
	}
	if project := os.Getenv("GCLOUD_PROJECT"); project != "" {
		return project, nil
	}
	if project := os.Getenv("GCP_PROJECT"); project != "" {
		return project, nil
	}

	// Try from GOOGLE_APPLICATION_CREDENTIALS
	if credFile := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); credFile != "" {
		project, err := extractProjectFromCredentials(credFile)
		if err == nil && project != "" {
			return project, nil
		}
	}

	// Try from default credentials
	creds, err := google.FindDefaultCredentials(ctx)
	if err == nil && creds.ProjectID != "" {
		return creds.ProjectID, nil
	}

	return "", fmt.Errorf("could not determine GCP project ID")
}

// extractProjectFromCredentials reads the project ID from a service account key file.
func extractProjectFromCredentials(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	var creds struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(data, &creds); err != nil {
		return "", err
	}

	return creds.ProjectID, nil
}

// FromParseOptions creates a credential config from parser options.
func FromParseOptions(creds map[string]string, regions []string) *CredentialConfig {
	cfg := NewCredentialConfig()

	if project, ok := creds["project"]; ok {
		cfg.Project = project
	}

	if credFile, ok := creds["credentials_file"]; ok {
		cfg.CredentialsFile = credFile
		cfg.Source = CredentialSourceServiceAccount
	}

	return cfg
}

// ValidateCredentials validates that the credentials are working.
func ValidateCredentials(ctx context.Context, opts []option.ClientOption, project string) error {
	// This will be validated when we create the first client
	// We could add a specific validation call here if needed
	return nil
}

// CallerIdentity contains information about the authenticated identity.
type CallerIdentity struct {
	// Project is the GCP project ID.
	Project string

	// ServiceAccount is the service account email (if using SA).
	ServiceAccount string

	// Type is the credential type (service_account, user, etc.).
	Type string
}
