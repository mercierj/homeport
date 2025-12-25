package aws

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudexit/cloudexit/internal/domain/parser"
	"github.com/cloudexit/cloudexit/internal/domain/resource"
)

// TerraformAWSParser wraps the existing Terraform parser to implement the Parser interface.
type TerraformAWSParser struct{}

// NewTerraformParser creates a new Terraform AWS parser.
func NewTerraformParser() *TerraformAWSParser {
	return &TerraformAWSParser{}
}

// Provider returns the cloud provider.
func (p *TerraformAWSParser) Provider() resource.Provider {
	return resource.ProviderAWS
}

// SupportedFormats returns the supported formats.
func (p *TerraformAWSParser) SupportedFormats() []parser.Format {
	return []parser.Format{
		parser.FormatTerraform,
		parser.FormatTFState,
	}
}

// Validate checks if the path contains valid Terraform files.
func (p *TerraformAWSParser) Validate(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return parser.ErrInvalidPath
	}

	if info.IsDir() {
		// Check for .tf or .tfstate files
		found := false
		filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(p))
			if ext == ".tf" || ext == ".tfstate" {
				found = true
				return filepath.SkipAll
			}
			return nil
		})
		if !found {
			return parser.ErrNoFilesFound
		}
		return nil
	}

	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".tf" && ext != ".tfstate" {
		return parser.ErrUnsupportedFormat
	}

	return nil
}

// AutoDetect checks if this parser can handle the given path.
func (p *TerraformAWSParser) AutoDetect(path string) (bool, float64) {
	info, err := os.Stat(path)
	if err != nil {
		return false, 0
	}

	if info.IsDir() {
		// Count Terraform files
		tfCount := 0
		cfnCount := 0
		filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(p))
			if ext == ".tf" || ext == ".tfstate" {
				tfCount++
			}
			// Check for CloudFormation markers
			if ext == ".yaml" || ext == ".yml" || ext == ".json" {
				content, err := os.ReadFile(p)
				if err == nil && strings.Contains(string(content), "AWSTemplateFormatVersion") {
					cfnCount++
				}
			}
			return nil
		})

		if tfCount > 0 {
			// Higher confidence if we found terraform files and no CloudFormation
			if cfnCount == 0 {
				return true, 0.9
			}
			return true, 0.7
		}
		return false, 0
	}

	ext := strings.ToLower(filepath.Ext(path))
	base := strings.ToLower(filepath.Base(path))

	if ext == ".tfstate" || base == "terraform.tfstate" {
		return true, 0.95
	}
	if ext == ".tf" {
		return true, 0.9
	}

	return false, 0
}

// Parse parses Terraform files and returns infrastructure.
func (p *TerraformAWSParser) Parse(ctx context.Context, path string, opts *parser.ParseOptions) (*resource.Infrastructure, error) {
	// Import the existing terraform parser functions
	// This is a wrapper that uses the existing implementation

	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	infra := resource.NewInfrastructure(resource.ProviderAWS)

	if info.IsDir() {
		// Look for terraform.tfstate
		statePath := filepath.Join(path, "terraform.tfstate")
		if _, err := os.Stat(statePath); err == nil {
			// Parse state file using existing implementation
			stateInfra, err := parseTerraformState(statePath)
			if err != nil && (opts == nil || !opts.IgnoreErrors) {
				return nil, err
			}
			if stateInfra != nil {
				for id, res := range stateInfra.Resources {
					infra.Resources[id] = res
				}
			}
		}

		// Also look in .terraform directory
		dotTFStatePath := filepath.Join(path, ".terraform", "terraform.tfstate")
		if _, err := os.Stat(dotTFStatePath); err == nil {
			stateInfra, err := parseTerraformState(dotTFStatePath)
			if err != nil && (opts == nil || !opts.IgnoreErrors) {
				return nil, err
			}
			if stateInfra != nil {
				for id, res := range stateInfra.Resources {
					infra.Resources[id] = res
				}
			}
		}

		// Parse .tf files for additional context
		filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if strings.HasSuffix(strings.ToLower(p), ".tf") {
				// Parse HCL file
				hclResources, err := parseHCLFile(p)
				if err == nil {
					for id, res := range hclResources {
						// Only add if not already present from state
						if _, exists := infra.Resources[id]; !exists {
							infra.Resources[id] = res
						}
					}
				}
			}
			return nil
		})
	} else {
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".tfstate" {
			stateInfra, err := parseTerraformState(path)
			if err != nil {
				return nil, err
			}
			return stateInfra, nil
		}
		if ext == ".tf" {
			hclResources, err := parseHCLFile(path)
			if err != nil {
				return nil, err
			}
			for id, res := range hclResources {
				infra.Resources[id] = res
			}
		}
	}

	// Apply filters if specified
	if opts != nil && (len(opts.FilterTypes) > 0 || len(opts.FilterCategories) > 0) {
		filtered := resource.NewInfrastructure(resource.ProviderAWS)
		for id, res := range infra.Resources {
			if shouldIncludeResource(res, opts) {
				filtered.Resources[id] = res
			}
		}
		infra = filtered
	}

	return infra, nil
}

// parseTerraformState is a helper that wraps the existing state parsing.
func parseTerraformState(path string) (*resource.Infrastructure, error) {
	// Import from the parent package's terraform.go
	// For now, implement inline using the state parsing logic

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Delegate to the existing tfstate parser
	// This would need to be refactored for better modularity
	_ = data

	// Return empty for now - actual implementation should call ParseState
	return resource.NewInfrastructure(resource.ProviderAWS), nil
}

// parseHCLFile parses a single HCL file.
func parseHCLFile(path string) (map[string]*resource.Resource, error) {
	// This would parse HCL files and extract resources
	// For now, return empty map
	return make(map[string]*resource.Resource), nil
}

// shouldIncludeResource checks if a resource matches the filter criteria.
func shouldIncludeResource(res *resource.Resource, opts *parser.ParseOptions) bool {
	if len(opts.FilterTypes) > 0 {
		for _, t := range opts.FilterTypes {
			if t == res.Type {
				return true
			}
		}
		return false
	}

	if len(opts.FilterCategories) > 0 {
		category := res.Type.GetCategory()
		for _, c := range opts.FilterCategories {
			if c == category {
				return true
			}
		}
		return false
	}

	return true
}

// RegisterAll registers all AWS parsers with the provided registry.
func RegisterAll(registry *parser.Registry) {
	// Terraform parsers (dedicated)
	registry.Register(NewTFStateParser())
	registry.Register(NewHCLParser())

	// Legacy combined parser (for backwards compatibility)
	registry.Register(NewTerraformParser())

	// CloudFormation parser
	registry.Register(NewCloudFormationParser())

	// API parser
	registry.Register(NewAPIParser())
}

// RegisterDefaults registers all AWS parsers with the default registry.
func RegisterDefaults() {
	RegisterAll(parser.DefaultRegistry())
}

// init registers the AWS parsers on package import.
func init() {
	RegisterDefaults()
}
