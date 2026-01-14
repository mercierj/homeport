// Package providers implements secret resolution providers for various sources.
package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/homeport/homeport/internal/domain/secrets"
)

// AWSSecretsManagerProvider resolves secrets from AWS Secrets Manager.
// Requires AWS CLI to be installed and configured.
type AWSSecretsManagerProvider struct {
	// Profile is the AWS profile to use (optional).
	Profile string

	// Region is the AWS region (optional, uses default if not set).
	Region string
}

// NewAWSSecretsManagerProvider creates a new AWS Secrets Manager provider.
func NewAWSSecretsManagerProvider() *AWSSecretsManagerProvider {
	return &AWSSecretsManagerProvider{}
}

// WithProfile sets the AWS profile.
func (p *AWSSecretsManagerProvider) WithProfile(profile string) *AWSSecretsManagerProvider {
	p.Profile = profile
	return p
}

// WithRegion sets the AWS region.
func (p *AWSSecretsManagerProvider) WithRegion(region string) *AWSSecretsManagerProvider {
	p.Region = region
	return p
}

// Name returns the provider identifier.
func (p *AWSSecretsManagerProvider) Name() secrets.SecretSource {
	return secrets.SourceAWSSecretsManager
}

// CanResolve checks if this provider can handle the secret reference.
func (p *AWSSecretsManagerProvider) CanResolve(ref *secrets.SecretReference) bool {
	return ref.Source == secrets.SourceAWSSecretsManager && ref.Key != ""
}

// Resolve retrieves a secret from AWS Secrets Manager.
func (p *AWSSecretsManagerProvider) Resolve(ctx context.Context, ref *secrets.SecretReference) (string, error) {
	if !p.CanResolve(ref) {
		return "", fmt.Errorf("cannot resolve secret %s: invalid source or missing key", ref.Name)
	}

	// Build AWS CLI command
	args := []string{"secretsmanager", "get-secret-value", "--secret-id", ref.Key}

	if p.Profile != "" {
		args = append(args, "--profile", p.Profile)
	}
	if p.Region != "" {
		args = append(args, "--region", p.Region)
	}
	if ref.Version != "" {
		args = append(args, "--version-stage", ref.Version)
	}

	args = append(args, "--query", "SecretString", "--output", "text")

	cmd := exec.CommandContext(ctx, "aws", args...)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("AWS CLI error: %s", string(exitErr.Stderr))
		}
		return "", fmt.Errorf("failed to run AWS CLI: %w", err)
	}

	value := strings.TrimSpace(string(output))

	// Check if it's a JSON secret and extract a specific field
	if strings.HasPrefix(value, "{") {
		// Try to parse as JSON
		var jsonSecret map[string]interface{}
		if err := json.Unmarshal([]byte(value), &jsonSecret); err == nil {
			// If the secret name suggests a specific field, try to extract it
			fieldName := extractFieldName(ref.Key)
			if fieldName != "" {
				if fieldValue, ok := jsonSecret[fieldName]; ok {
					if strValue, ok := fieldValue.(string); ok {
						return strValue, nil
					}
				}
			}
		}
	}

	return value, nil
}

// ValidateConfig checks if AWS CLI is available and configured.
func (p *AWSSecretsManagerProvider) ValidateConfig() error {
	// Check if aws CLI is available
	if _, err := exec.LookPath("aws"); err != nil {
		return fmt.Errorf("AWS CLI not found in PATH: %w", err)
	}

	// Try to get caller identity
	args := []string{"sts", "get-caller-identity"}
	if p.Profile != "" {
		args = append(args, "--profile", p.Profile)
	}

	cmd := exec.Command("aws", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("AWS credentials not configured: %w", err)
	}

	return nil
}

// ListSecrets lists available secrets in AWS Secrets Manager.
func (p *AWSSecretsManagerProvider) ListSecrets(ctx context.Context) ([]string, error) {
	args := []string{"secretsmanager", "list-secrets", "--query", "SecretList[].Name", "--output", "json"}

	if p.Profile != "" {
		args = append(args, "--profile", p.Profile)
	}
	if p.Region != "" {
		args = append(args, "--region", p.Region)
	}

	cmd := exec.CommandContext(ctx, "aws", args...)
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

// extractFieldName attempts to extract a field name from a secret key.
// For example, "prod/myapp/db-password" might map to "password" field.
func extractFieldName(key string) string {
	parts := strings.Split(key, "/")
	if len(parts) > 0 {
		lastPart := parts[len(parts)-1]
		// Common patterns: db-password -> password, api-key -> key
		if idx := strings.LastIndex(lastPart, "-"); idx != -1 {
			return lastPart[idx+1:]
		}
	}
	return ""
}

// AWSSecretsManagerBatchResult contains results from a batch secret fetch.
type AWSSecretsManagerBatchResult struct {
	Secrets map[string]string
	Errors  map[string]error
}

// ResolveBatch retrieves multiple secrets in a single call (more efficient).
func (p *AWSSecretsManagerProvider) ResolveBatch(ctx context.Context, refs []*secrets.SecretReference) (*AWSSecretsManagerBatchResult, error) {
	result := &AWSSecretsManagerBatchResult{
		Secrets: make(map[string]string),
		Errors:  make(map[string]error),
	}

	// Build list of secret IDs
	secretIDs := make([]string, 0, len(refs))
	refMap := make(map[string]*secrets.SecretReference)
	for _, ref := range refs {
		if p.CanResolve(ref) {
			secretIDs = append(secretIDs, ref.Key)
			refMap[ref.Key] = ref
		}
	}

	if len(secretIDs) == 0 {
		return result, nil
	}

	// Use batch-get-secret-value (available in newer AWS CLI versions)
	args := []string{"secretsmanager", "batch-get-secret-value", "--secret-id-list"}
	args = append(args, secretIDs...)

	if p.Profile != "" {
		args = append(args, "--profile", p.Profile)
	}
	if p.Region != "" {
		args = append(args, "--region", p.Region)
	}

	cmd := exec.CommandContext(ctx, "aws", args...)
	output, err := cmd.Output()
	if err != nil {
		// Fall back to individual fetches
		for _, ref := range refs {
			value, err := p.Resolve(ctx, ref)
			if err != nil {
				result.Errors[ref.Name] = err
			} else {
				result.Secrets[ref.Name] = value
			}
		}
		return result, nil
	}

	// Parse batch response
	var batchResponse struct {
		SecretValues []struct {
			Name         string `json:"Name"`
			SecretString string `json:"SecretString"`
		} `json:"SecretValues"`
		Errors []struct {
			SecretID string `json:"SecretId"`
			Message  string `json:"Message"`
		} `json:"Errors"`
	}

	if err := json.Unmarshal(output, &batchResponse); err != nil {
		return nil, fmt.Errorf("failed to parse batch response: %w", err)
	}

	for _, sv := range batchResponse.SecretValues {
		if ref, ok := refMap[sv.Name]; ok {
			result.Secrets[ref.Name] = sv.SecretString
		}
	}

	for _, e := range batchResponse.Errors {
		if ref, ok := refMap[e.SecretID]; ok {
			result.Errors[ref.Name] = fmt.Errorf("aws error: %s", e.Message)
		}
	}

	return result, nil
}
