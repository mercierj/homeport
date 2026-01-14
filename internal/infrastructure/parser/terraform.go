// Package parser provides functionality for parsing Terraform state files and HCL configurations
// to build Infrastructure models for the Homeport project.
package parser

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/homeport/homeport/internal/domain/resource"
)

// ParseState parses a Terraform state file and returns an Infrastructure model
//
// This function reads a terraform.tfstate file (JSON format) and converts it
// into our internal Infrastructure representation. It supports both v3 and v4
// state file formats.
//
// Parameters:
//   - path: Path to the terraform.tfstate file
//
// Returns:
//   - *resource.Infrastructure: The parsed infrastructure model
//   - error: Any error encountered during parsing
//
// Example:
//   infra, err := ParseState("/path/to/terraform.tfstate")
func ParseState(path string) (*resource.Infrastructure, error) {
	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("state file not found: %s", path)
	}

	// Parse the state file
	state, err := ParseStateFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to parse state file: %w", err)
	}

	// Build infrastructure from state
	infra, err := BuildInfrastructureFromState(state)
	if err != nil {
		return nil, fmt.Errorf("failed to build infrastructure: %w", err)
	}

	// Extract implicit dependencies
	for _, res := range infra.Resources {
		ExtractResourceDependencies(res, infra.Resources)
	}

	return infra, nil
}

// ParseHCL parses Terraform HCL files from a directory
//
// This function reads all .tf files in the specified directory and extracts
// resource definitions, variables, locals, and outputs. This provides additional
// context beyond what's available in the state file.
//
// Parameters:
//   - dir: Directory containing .tf files
//
// Returns:
//   - *resource.Infrastructure: The parsed infrastructure model
//   - error: Any error encountered during parsing
//
// Example:
//   infra, err := ParseHCL("/path/to/terraform/dir")
func ParseHCL(dir string) (*resource.Infrastructure, error) {
	// Check if directory exists
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, fmt.Errorf("directory not found: %s", dir)
	}

	// Create HCL parser
	parser := NewHCLParser()

	// Parse HCL files
	hclConfig, err := parser.ParseHCLDirectory(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HCL files: %w", err)
	}

	// Create infrastructure from HCL (without state)
	infra := resource.NewInfrastructure(resource.ProviderAWS)

	// Convert HCL resources to infrastructure resources
	for key, hclRes := range hclConfig.Resources {
		resourceType := mapTerraformTypeToResourceType(hclRes.Type)

		// Extract name from attributes or use HCL name
		name := hclRes.Name
		if nameAttr, ok := hclRes.Attributes["name"].(string); ok && nameAttr != "" {
			name = nameAttr
		}

		res := resource.NewAWSResource(key, name, resourceType)
		res.Config = hclRes.Attributes

		// Extract tags if present
		if tags, ok := hclRes.Attributes["tags"].(map[string]interface{}); ok {
			for k, v := range tags {
				if strVal, ok := v.(string); ok {
					res.Tags[k] = strVal
				}
			}
		}

		infra.AddResource(res)
	}

	// Merge HCL metadata
	if err := MergeHCLWithInfrastructure(infra, hclConfig); err != nil {
		return nil, fmt.Errorf("failed to merge HCL config: %w", err)
	}

	return infra, nil
}

// BuildInfrastructure builds a complete Infrastructure model from both Terraform state and HCL files
//
// This is the main entry point for parsing a Terraform project. It combines information
// from both the state file (actual deployed resources with runtime values) and HCL files
// (configuration, variables, outputs) to create a comprehensive infrastructure model.
//
// The function follows this process:
//  1. Parse the Terraform state file to get deployed resources
//  2. If tfDir is provided, parse HCL files for additional context
//  3. Merge HCL configuration with state data
//  4. Extract and resolve resource dependencies
//  5. Validate the resulting infrastructure
//
// Parameters:
//   - statePath: Path to terraform.tfstate file (required)
//   - tfDir: Directory containing .tf files (optional, can be empty string)
//
// Returns:
//   - *resource.Infrastructure: Complete infrastructure model
//   - error: Any error encountered during parsing or validation
//
// Example:
//   // With state file only
//   infra, err := BuildInfrastructure("/path/to/terraform.tfstate", "")
//
//   // With state file and HCL directory
//   infra, err := BuildInfrastructure("/path/to/terraform.tfstate", "/path/to/terraform")
func BuildInfrastructure(statePath string, tfDir string) (*resource.Infrastructure, error) {
	var infra *resource.Infrastructure
	var err error

	// Parse state file if provided
	if statePath != "" {
		infra, err = ParseState(statePath)
		if err != nil {
			return nil, fmt.Errorf("failed to parse state: %w", err)
		}
	} else {
		// If no state file, create empty infrastructure
		infra = resource.NewInfrastructure(resource.ProviderAWS)
	}

	// Parse HCL files if directory is provided
	if tfDir != "" {
		parser := NewHCLParser()
		hclConfig, err := parser.ParseHCLDirectory(tfDir)
		if err != nil {
			// Log warning but continue - HCL parsing is optional
			fmt.Fprintf(os.Stderr, "Warning: failed to parse HCL files: %v\n", err)
		} else {
			// Merge HCL configuration with state
			if err := MergeHCLWithInfrastructure(infra, hclConfig); err != nil {
				return nil, fmt.Errorf("failed to merge HCL config: %w", err)
			}
		}
	}

	// Validate infrastructure
	if err := infra.Validate(); err != nil {
		return nil, fmt.Errorf("infrastructure validation failed: %w", err)
	}

	return infra, nil
}

// ParseTerraformProject auto-detects and parses a Terraform project
//
// This convenience function automatically locates the terraform.tfstate file
// and .tf files in a directory and parses them together.
//
// Parameters:
//   - projectDir: Root directory of the Terraform project
//
// Returns:
//   - *resource.Infrastructure: Parsed infrastructure model
//   - error: Any error encountered
func ParseTerraformProject(projectDir string) (*resource.Infrastructure, error) {
	// Look for terraform.tfstate in the project directory
	statePath := filepath.Join(projectDir, "terraform.tfstate")

	// Check if state file exists
	if _, err := os.Stat(statePath); os.IsNotExist(err) {
		// Try .terraform directory
		statePath = filepath.Join(projectDir, ".terraform", "terraform.tfstate")
		if _, err := os.Stat(statePath); os.IsNotExist(err) {
			return nil, fmt.Errorf("terraform.tfstate not found in %s", projectDir)
		}
	}

	// Build infrastructure from state and HCL files
	return BuildInfrastructure(statePath, projectDir)
}

// ParseOptions configures how Terraform files should be parsed
type ParseOptions struct {
	// StatePath is the path to terraform.tfstate
	StatePath string

	// TerraformDir is the directory containing .tf files
	TerraformDir string

	// IncludeModules controls whether to parse nested modules
	IncludeModules bool

	// ValidateResources controls whether to validate parsed resources
	ValidateResources bool

	// ExtractDependencies controls whether to extract implicit dependencies
	ExtractDependencies bool
}

// ParseWithOptions parses Terraform files with custom options
func ParseWithOptions(opts ParseOptions) (*resource.Infrastructure, error) {
	if opts.StatePath == "" && opts.TerraformDir == "" {
		return nil, fmt.Errorf("either StatePath or TerraformDir must be provided")
	}

	var infra *resource.Infrastructure

	// Parse state if provided
	if opts.StatePath != "" {
		state, err := ParseStateFile(opts.StatePath)
		if err != nil {
			return nil, fmt.Errorf("failed to parse state: %w", err)
		}

		var buildErr error
		infra, buildErr = BuildInfrastructureFromState(state)
		if buildErr != nil {
			return nil, fmt.Errorf("failed to build infrastructure: %w", buildErr)
		}

		// Extract dependencies if requested
		if opts.ExtractDependencies {
			for _, res := range infra.Resources {
				ExtractResourceDependencies(res, infra.Resources)
			}
		}
	} else {
		infra = resource.NewInfrastructure(resource.ProviderAWS)
	}

	// Parse HCL if directory provided
	if opts.TerraformDir != "" {
		parser := NewHCLParser()
		hclConfig, err := parser.ParseHCLDirectory(opts.TerraformDir)
		if err != nil {
			return nil, fmt.Errorf("failed to parse HCL: %w", err)
		}

		if err := MergeHCLWithInfrastructure(infra, hclConfig); err != nil {
			return nil, fmt.Errorf("failed to merge HCL: %w", err)
		}
	}

	// Validate if requested
	if opts.ValidateResources {
		if err := infra.Validate(); err != nil {
			return nil, fmt.Errorf("validation failed: %w", err)
		}
	}

	return infra, nil
}
