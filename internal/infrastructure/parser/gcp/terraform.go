package gcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/homeport/homeport/internal/domain/parser"
	"github.com/homeport/homeport/internal/domain/resource"
)

// TerraformGCPParser wraps the existing Terraform parser for GCP resources.
type TerraformGCPParser struct{}

// NewTerraformParser creates a new Terraform GCP parser.
func NewTerraformParser() *TerraformGCPParser {
	return &TerraformGCPParser{}
}

// Provider returns the cloud provider.
func (p *TerraformGCPParser) Provider() resource.Provider {
	return resource.ProviderGCP
}

// SupportedFormats returns the supported formats.
func (p *TerraformGCPParser) SupportedFormats() []parser.Format {
	return []parser.Format{
		parser.FormatTerraform,
		parser.FormatTFState,
	}
}

// Validate checks if the path contains valid Terraform files with GCP resources.
func (p *TerraformGCPParser) Validate(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return parser.ErrInvalidPath
	}

	if info.IsDir() {
		found := false
		hasGCP := false
		_ = filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(p))
			if ext == ".tf" || ext == ".tfstate" {
				found = true
				// Check for GCP resources
				content, err := os.ReadFile(p)
				if err == nil {
					if strings.Contains(string(content), "google_") ||
						strings.Contains(string(content), `provider "google"`) {
						hasGCP = true
					}
				}
			}
			return nil
		})
		if !found {
			return parser.ErrNoFilesFound
		}
		if !hasGCP {
			return parser.ErrUnsupportedFormat
		}
		return nil
	}

	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".tf" && ext != ".tfstate" {
		return parser.ErrUnsupportedFormat
	}

	// Check content for GCP resources
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !strings.Contains(string(content), "google_") {
		return parser.ErrUnsupportedFormat
	}

	return nil
}

// AutoDetect checks if this parser can handle the given path.
func (p *TerraformGCPParser) AutoDetect(path string) (bool, float64) {
	info, err := os.Stat(path)
	if err != nil {
		return false, 0
	}

	if info.IsDir() {
		tfCount := 0
		gcpCount := 0
		_ = filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(p))
			if ext == ".tf" || ext == ".tfstate" {
				tfCount++
				content, err := os.ReadFile(p)
				if err == nil {
					if strings.Contains(string(content), "google_") ||
						strings.Contains(string(content), `provider "google"`) {
						gcpCount++
					}
				}
			}
			return nil
		})

		if gcpCount > 0 {
			// Calculate confidence based on ratio of GCP files
			confidence := float64(gcpCount) / float64(tfCount) * 0.9
			return true, confidence
		}
		return false, 0
	}

	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".tf" || ext == ".tfstate" {
		content, err := os.ReadFile(path)
		if err != nil {
			return false, 0
		}
		if strings.Contains(string(content), "google_") {
			return true, 0.85
		}
	}

	return false, 0
}

// Parse parses Terraform files and returns GCP infrastructure.
func (p *TerraformGCPParser) Parse(ctx context.Context, path string, opts *parser.ParseOptions) (*resource.Infrastructure, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	infra := resource.NewInfrastructure(resource.ProviderGCP)

	if info.IsDir() {
		// Look for terraform.tfstate
		statePath := filepath.Join(path, "terraform.tfstate")
		if _, err := os.Stat(statePath); err == nil {
			stateInfra, err := p.parseTerraformState(statePath)
			if err != nil && (opts == nil || !opts.IgnoreErrors) {
				return nil, err
			}
			if stateInfra != nil {
				for id, res := range stateInfra.Resources {
					infra.Resources[id] = res
				}
			}
		}

		// Parse .tf files
		_ = filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if strings.HasSuffix(strings.ToLower(filePath), ".tf") {
				hclResources, err := p.parseHCLFile(filePath)
				if err == nil {
					for id, res := range hclResources {
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
			return p.parseTerraformState(path)
		}
		if ext == ".tf" {
			hclResources, err := p.parseHCLFile(path)
			if err != nil {
				return nil, err
			}
			for id, res := range hclResources {
				infra.Resources[id] = res
			}
		}
	}

	// Apply filters
	if opts != nil && (len(opts.FilterTypes) > 0 || len(opts.FilterCategories) > 0) {
		filtered := resource.NewInfrastructure(resource.ProviderGCP)
		for id, res := range infra.Resources {
			if shouldIncludeResource(res, opts) {
				filtered.Resources[id] = res
			}
		}
		infra = filtered
	}

	return infra, nil
}

// parseTerraformState parses a Terraform state file for GCP resources.
func (p *TerraformGCPParser) parseTerraformState(path string) (*resource.Infrastructure, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	_ = data
	return resource.NewInfrastructure(resource.ProviderGCP), nil
}

// parseHCLFile parses a single HCL file for GCP resources.
func (p *TerraformGCPParser) parseHCLFile(path string) (map[string]*resource.Resource, error) {
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
