package bundle

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/homeport/homeport/internal/domain/bundle"
)

// Importer handles importing .hprt bundles for deployment.
type Importer struct {
	archiver  *Archiver
	extractor *Extractor
	validator *Validator

	// HomeportVersion is the current tool version.
	HomeportVersion string
}

// ImportOptions configures bundle import behavior.
type ImportOptions struct {
	// OutputDir is the directory to extract to.
	OutputDir string

	// TargetHost is the SSH target for remote deployment.
	TargetHost string

	// TargetUser is the SSH user for remote deployment.
	TargetUser string

	// SecretsFile is the path to a secrets file (.env format).
	SecretsFile string

	// PullSecretsFrom is the cloud provider to pull secrets from.
	PullSecretsFrom string

	// SkipValidation skips bundle validation.
	SkipValidation bool

	// SkipDependencyCheck skips external tool checks.
	SkipDependencyCheck bool

	// DryRun only validates without extracting.
	DryRun bool

	// Deploy automatically runs docker-compose up after import.
	Deploy bool
}

// ImportResult contains the result of a bundle import.
type ImportResult struct {
	Bundle           *bundle.Bundle
	ExtractedTo      string
	Validation       *ValidationResult
	RequiredSecrets  []*bundle.SecretReference
	ProvidedSecrets  map[string]bool
	MissingSecrets   []string
	Ready            bool
}

// NewImporter creates a new bundle importer.
func NewImporter(homeportVersion string) *Importer {
	return &Importer{
		archiver:        NewArchiver(),
		extractor:       NewExtractor(),
		validator:       NewValidator(homeportVersion),
		HomeportVersion: homeportVersion,
	}
}

// Import imports a .hprt bundle file.
func (i *Importer) Import(archivePath string, opts ImportOptions) (*ImportResult, error) {
	result := &ImportResult{
		ProvidedSecrets: make(map[string]bool),
	}

	// Read the bundle
	b, err := i.archiver.ExtractArchive(archivePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read bundle: %w", err)
	}
	result.Bundle = b

	// Validate unless skipped
	if !opts.SkipValidation {
		i.validator.SkipDependencyCheck = opts.SkipDependencyCheck
		validation, err := i.validator.ValidateBundle(b)
		if err != nil {
			return nil, fmt.Errorf("validation error: %w", err)
		}
		result.Validation = validation

		if validation.HasFatalErrors() {
			return result, fmt.Errorf("bundle validation failed: see result.Validation for details")
		}
	}

	// Check required secrets
	result.RequiredSecrets = b.Manifest.GetRequiredSecrets()
	if len(result.RequiredSecrets) > 0 {
		i.checkSecrets(result, opts)
	}

	// Dry run stops here
	if opts.DryRun {
		result.Ready = len(result.MissingSecrets) == 0
		return result, nil
	}

	// Determine output directory
	outputDir := opts.OutputDir
	if outputDir == "" {
		outputDir = filepath.Join(".", "homeport-import")
	}

	// Extract bundle
	extractOpts := ExtractOptions{
		OutputDir:         outputDir,
		OverwriteExisting: true,
	}

	_, err = i.extractor.ExtractBundle(b, extractOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to extract bundle: %w", err)
	}
	result.ExtractedTo = outputDir

	// Apply secrets if provided
	if opts.SecretsFile != "" {
		if err := i.applySecretsFile(outputDir, opts.SecretsFile); err != nil {
			return nil, fmt.Errorf("failed to apply secrets: %w", err)
		}
	}

	result.Ready = len(result.MissingSecrets) == 0
	return result, nil
}

// ImportFromReader imports a bundle from an io.Reader.
func (i *Importer) ImportFromReader(r *os.File, opts ImportOptions) (*ImportResult, error) {
	b, err := i.archiver.ReadArchive(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read bundle: %w", err)
	}

	return i.importBundle(b, opts)
}

// importBundle imports an already-loaded bundle.
func (i *Importer) importBundle(b *bundle.Bundle, opts ImportOptions) (*ImportResult, error) {
	result := &ImportResult{
		Bundle:          b,
		ProvidedSecrets: make(map[string]bool),
	}

	// Validate unless skipped
	if !opts.SkipValidation {
		validation, err := i.validator.ValidateBundle(b)
		if err != nil {
			return nil, fmt.Errorf("validation error: %w", err)
		}
		result.Validation = validation

		if validation.HasFatalErrors() {
			return result, nil
		}
	}

	// Check required secrets
	result.RequiredSecrets = b.Manifest.GetRequiredSecrets()
	if len(result.RequiredSecrets) > 0 {
		i.checkSecrets(result, opts)
	}

	if opts.DryRun {
		result.Ready = len(result.MissingSecrets) == 0
		return result, nil
	}

	// Determine output directory
	outputDir := opts.OutputDir
	if outputDir == "" {
		outputDir = filepath.Join(".", "homeport-import")
	}

	// Extract bundle
	extractOpts := ExtractOptions{
		OutputDir:         outputDir,
		OverwriteExisting: true,
	}

	_, err := i.extractor.ExtractBundle(b, extractOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to extract bundle: %w", err)
	}
	result.ExtractedTo = outputDir

	result.Ready = len(result.MissingSecrets) == 0
	return result, nil
}

// checkSecrets checks which required secrets are provided.
func (i *Importer) checkSecrets(result *ImportResult, opts ImportOptions) {
	for _, secret := range result.RequiredSecrets {
		// Check environment variable
		envKey := "HOMEPORT_SECRET_" + secret.Name
		if os.Getenv(envKey) != "" {
			result.ProvidedSecrets[secret.Name] = true
			continue
		}

		// Check if secrets file will provide it (we check later)
		if opts.SecretsFile != "" {
			// Mark as potentially provided, will verify during apply
			result.ProvidedSecrets[secret.Name] = true
			continue
		}

		// Check if we can pull from cloud
		if opts.PullSecretsFrom != "" && secret.Source == opts.PullSecretsFrom {
			result.ProvidedSecrets[secret.Name] = true
			continue
		}

		result.MissingSecrets = append(result.MissingSecrets, secret.Name)
	}
}

// applySecretsFile copies secrets from a file to the bundle's .env.
func (i *Importer) applySecretsFile(outputDir, secretsFile string) error {
	content, err := os.ReadFile(secretsFile)
	if err != nil {
		return fmt.Errorf("failed to read secrets file: %w", err)
	}

	envPath := filepath.Join(outputDir, "secrets", ".env")
	if err := os.MkdirAll(filepath.Dir(envPath), 0755); err != nil {
		return fmt.Errorf("failed to create secrets directory: %w", err)
	}

	if err := os.WriteFile(envPath, content, 0600); err != nil {
		return fmt.Errorf("failed to write .env file: %w", err)
	}

	return nil
}

// Preview returns information about a bundle without extracting.
func (i *Importer) Preview(archivePath string) (*ImportResult, error) {
	return i.Import(archivePath, ImportOptions{DryRun: true})
}

// GetRequiredSecrets returns the list of secrets needed for a bundle.
func (i *Importer) GetRequiredSecrets(archivePath string) ([]*bundle.SecretReference, error) {
	b, err := i.archiver.ExtractArchive(archivePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read bundle: %w", err)
	}

	return b.Manifest.GetRequiredSecrets(), nil
}
