package providers

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/homeport/homeport/internal/domain/secrets"
)

// GCPSecretManagerProvider resolves secrets from GCP Secret Manager.
// Requires gcloud CLI to be installed and configured.
type GCPSecretManagerProvider struct {
	// Project is the GCP project ID (optional, uses default if not set).
	Project string
}

// NewGCPSecretManagerProvider creates a new GCP Secret Manager provider.
func NewGCPSecretManagerProvider() *GCPSecretManagerProvider {
	return &GCPSecretManagerProvider{}
}

// WithProject sets the GCP project ID.
func (p *GCPSecretManagerProvider) WithProject(project string) *GCPSecretManagerProvider {
	p.Project = project
	return p
}

// Name returns the provider identifier.
func (p *GCPSecretManagerProvider) Name() secrets.SecretSource {
	return secrets.SourceGCPSecretManager
}

// CanResolve checks if this provider can handle the secret reference.
func (p *GCPSecretManagerProvider) CanResolve(ref *secrets.SecretReference) bool {
	return ref.Source == secrets.SourceGCPSecretManager && ref.Key != ""
}

// Resolve retrieves a secret from GCP Secret Manager.
func (p *GCPSecretManagerProvider) Resolve(ctx context.Context, ref *secrets.SecretReference) (string, error) {
	if !p.CanResolve(ref) {
		return "", fmt.Errorf("cannot resolve secret %s: invalid source or missing key", ref.Name)
	}

	// Parse the key - can be in formats:
	// - "secret-name" (latest version)
	// - "secret-name/versions/1" (specific version)
	// - "projects/PROJECT/secrets/NAME/versions/VERSION" (full path)
	secretName := ref.Key
	version := "latest"

	if ref.Version != "" {
		version = ref.Version
	}

	// Build gcloud command
	args := []string{"secrets", "versions", "access", version, "--secret", secretName}

	if p.Project != "" {
		args = append(args, "--project", p.Project)
	}

	args = append(args, "--format", "value(payload.data)")

	cmd := exec.CommandContext(ctx, "gcloud", args...)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("gcloud error: %s", string(exitErr.Stderr))
		}
		return "", fmt.Errorf("failed to run gcloud: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// ValidateConfig checks if gcloud CLI is available and configured.
func (p *GCPSecretManagerProvider) ValidateConfig() error {
	// Check if gcloud CLI is available
	if _, err := exec.LookPath("gcloud"); err != nil {
		return fmt.Errorf("gcloud CLI not found in PATH: %w", err)
	}

	// Check authentication
	args := []string{"auth", "print-access-token"}
	if p.Project != "" {
		args = append(args, "--project", p.Project)
	}

	cmd := exec.Command("gcloud", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gcloud not authenticated: %w", err)
	}

	return nil
}

// ListSecrets lists available secrets in GCP Secret Manager.
func (p *GCPSecretManagerProvider) ListSecrets(ctx context.Context) ([]string, error) {
	args := []string{"secrets", "list", "--format", "value(name)"}

	if p.Project != "" {
		args = append(args, "--project", p.Project)
	}

	cmd := exec.CommandContext(ctx, "gcloud", args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list secrets: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var names []string
	for _, line := range lines {
		if line = strings.TrimSpace(line); line != "" {
			names = append(names, line)
		}
	}

	return names, nil
}

// CreateSecret creates a new secret in GCP Secret Manager.
func (p *GCPSecretManagerProvider) CreateSecret(ctx context.Context, name, value string) error {
	// First create the secret
	createArgs := []string{"secrets", "create", name, "--replication-policy", "automatic"}
	if p.Project != "" {
		createArgs = append(createArgs, "--project", p.Project)
	}

	cmd := exec.CommandContext(ctx, "gcloud", createArgs...)
	if err := cmd.Run(); err != nil {
		// Secret might already exist, continue
		if exitErr, ok := err.(*exec.ExitError); ok {
			if !strings.Contains(string(exitErr.Stderr), "ALREADY_EXISTS") {
				return fmt.Errorf("failed to create secret: %s", string(exitErr.Stderr))
			}
		}
	}

	// Add the secret version with the value
	versionArgs := []string{"secrets", "versions", "add", name, "--data-file=-"}
	if p.Project != "" {
		versionArgs = append(versionArgs, "--project", p.Project)
	}

	cmd = exec.CommandContext(ctx, "gcloud", versionArgs...)
	cmd.Stdin = strings.NewReader(value)
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("failed to add secret version: %s", string(exitErr.Stderr))
		}
		return fmt.Errorf("failed to add secret version: %w", err)
	}

	return nil
}
