package bundle

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/homeport/homeport/internal/domain/bundle"
)

// Validator validates bundles and checks dependencies.
type Validator struct {
	// ToolVersion is the current homeport version.
	ToolVersion string

	// SkipDependencyCheck skips external tool checks.
	SkipDependencyCheck bool
}

// NewValidator creates a new bundle validator.
func NewValidator(toolVersion string) *Validator {
	return &Validator{
		ToolVersion: toolVersion,
	}
}

// ValidationResult contains the result of bundle validation.
type ValidationResult struct {
	Valid    bool
	Errors   []ValidationError
	Warnings []string
}

// ValidationError describes a validation failure.
type ValidationError struct {
	Field   string
	Message string
	Fatal   bool
}

// ValidateBundle performs comprehensive bundle validation.
func (v *Validator) ValidateBundle(b *bundle.Bundle) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	// Check manifest
	if b.Manifest == nil {
		result.addError("manifest", "missing manifest", true)
		return result, nil
	}

	// Validate manifest fields
	v.validateManifest(b.Manifest, result)

	// Check version compatibility
	if err := bundle.CheckCompatibility(b.Manifest.Version, v.ToolVersion); err != nil {
		result.addError("version", err.Error(), true)
	}

	// Verify checksums
	checksumResults := bundle.VerifyChecksumsDetailed(b)
	for _, cr := range checksumResults {
		if !cr.Valid {
			result.addError(cr.Path, fmt.Sprintf("checksum mismatch: expected %s, got %s", cr.Expected, cr.Actual), true)
		}
	}

	// Check required files
	v.validateRequiredFiles(b, result)

	// Check dependencies if not skipped
	if !v.SkipDependencyCheck && b.Manifest.Dependencies != nil {
		v.validateDependencies(b.Manifest.Dependencies, result)
	}

	return result, nil
}

// validateManifest validates manifest fields.
func (v *Validator) validateManifest(m *bundle.Manifest, result *ValidationResult) {
	if m.Version == "" {
		result.addError("manifest.version", "version is required", true)
	}

	if m.Format != "hprt" {
		result.addError("manifest.format", fmt.Sprintf("invalid format: %s", m.Format), true)
	}

	if m.Source == nil {
		result.addError("manifest.source", "source info is required", true)
	} else {
		if m.Source.Provider == "" {
			result.addError("manifest.source.provider", "provider is required", true)
		}
	}

	if m.Target == nil {
		result.addError("manifest.target", "target info is required", true)
	}

	// Check for secrets in manifest (should only be references)
	if m.Secrets != nil {
		for _, s := range m.Secrets.Secrets {
			if s.Source == "" {
				result.addError(fmt.Sprintf("secrets.%s", s.Name), "secret source is required", false)
			}
		}
	}
}

// validateRequiredFiles checks that essential files exist.
func (v *Validator) validateRequiredFiles(b *bundle.Bundle, result *ValidationResult) {
	// Must have docker-compose.yml
	if !b.HasFile("compose/docker-compose.yml") && !b.HasFile("compose/docker-compose.yaml") {
		result.addError("files", "missing compose/docker-compose.yml", true)
	}

	// Warn if no README
	if !b.HasFile("README.md") {
		result.addWarning("missing README.md - consider adding documentation")
	}

	// Check scripts are executable
	for path, file := range b.Files {
		if strings.HasSuffix(path, ".sh") && file.Mode&0100 == 0 {
			result.addWarning(fmt.Sprintf("%s should be executable", path))
		}
	}
}

// validateDependencies checks that required tools are available.
func (v *Validator) validateDependencies(deps *bundle.Dependencies, result *ValidationResult) {
	if deps.Docker != "" {
		if err := v.checkToolVersion("docker", "--version", deps.Docker); err != nil {
			result.addError("dependencies.docker", err.Error(), false)
		}
	}

	if deps.DockerCompose != "" {
		if err := v.checkToolVersion("docker", "compose version", deps.DockerCompose); err != nil {
			result.addError("dependencies.docker-compose", err.Error(), false)
		}
	}

	// Check additional tools
	if deps.Tools != nil {
		for tool, constraint := range deps.Tools {
			if err := v.checkToolVersion(tool, "--version", constraint); err != nil {
				result.addWarning(fmt.Sprintf("%s: %s", tool, err.Error()))
			}
		}
	}
}

// checkToolVersion verifies a tool meets the version constraint.
func (v *Validator) checkToolVersion(tool, versionArg, constraint string) error {
	args := strings.Fields(versionArg)
	cmd := exec.Command(tool, args...)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("%s not found or not executable", tool)
	}

	// Parse version from output
	version := v.extractVersion(string(output))
	if version == "" {
		return fmt.Errorf("could not determine %s version", tool)
	}

	// Check constraint
	parsedConstraint, err := bundle.ParseConstraint(constraint)
	if err != nil {
		return fmt.Errorf("invalid constraint %s: %w", constraint, err)
	}

	parsedVersion, err := bundle.ParseVersion(version)
	if err != nil {
		return fmt.Errorf("could not parse version %s: %w", version, err)
	}

	if !parsedVersion.Satisfies(parsedConstraint) {
		return fmt.Errorf("%s version %s does not satisfy %s", tool, version, constraint)
	}

	return nil
}

// extractVersion extracts a semantic version from text.
func (v *Validator) extractVersion(text string) string {
	re := regexp.MustCompile(`(\d+\.\d+\.\d+)`)
	matches := re.FindStringSubmatch(text)
	if len(matches) > 0 {
		return matches[1]
	}
	return ""
}

// addError adds an error to the result.
func (r *ValidationResult) addError(field, message string, fatal bool) {
	r.Errors = append(r.Errors, ValidationError{
		Field:   field,
		Message: message,
		Fatal:   fatal,
	})
	if fatal {
		r.Valid = false
	}
}

// addWarning adds a warning to the result.
func (r *ValidationResult) addWarning(message string) {
	r.Warnings = append(r.Warnings, message)
}

// HasFatalErrors returns true if there are any fatal errors.
func (r *ValidationResult) HasFatalErrors() bool {
	for _, err := range r.Errors {
		if err.Fatal {
			return true
		}
	}
	return false
}

// String returns a human-readable validation summary.
func (r *ValidationResult) String() string {
	var sb strings.Builder

	if r.Valid {
		sb.WriteString("Bundle validation passed\n")
	} else {
		sb.WriteString("Bundle validation failed\n")
	}

	if len(r.Errors) > 0 {
		sb.WriteString("\nErrors:\n")
		for _, err := range r.Errors {
			severity := "ERROR"
			if !err.Fatal {
				severity = "WARNING"
			}
			sb.WriteString(fmt.Sprintf("  [%s] %s: %s\n", severity, err.Field, err.Message))
		}
	}

	if len(r.Warnings) > 0 {
		sb.WriteString("\nWarnings:\n")
		for _, w := range r.Warnings {
			sb.WriteString(fmt.Sprintf("  - %s\n", w))
		}
	}

	return sb.String()
}
