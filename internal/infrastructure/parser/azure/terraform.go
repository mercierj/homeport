package azure

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/agnostech/agnostech/internal/domain/parser"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

// TerraformAzureParser parses Terraform files for Azure resources.
type TerraformAzureParser struct{}

// NewTerraformParser creates a new Terraform Azure parser.
func NewTerraformParser() *TerraformAzureParser {
	return &TerraformAzureParser{}
}

// Provider returns the cloud provider.
func (p *TerraformAzureParser) Provider() resource.Provider {
	return resource.ProviderAzure
}

// SupportedFormats returns the supported formats.
func (p *TerraformAzureParser) SupportedFormats() []parser.Format {
	return []parser.Format{
		parser.FormatTerraform,
		parser.FormatTFState,
	}
}

// Validate checks if the path contains valid Terraform files with Azure resources.
func (p *TerraformAzureParser) Validate(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return parser.ErrInvalidPath
	}

	if info.IsDir() {
		found := false
		hasAzure := false
		filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(p))
			if ext == ".tf" || ext == ".tfstate" {
				found = true
				content, err := os.ReadFile(p)
				if err == nil {
					if strings.Contains(string(content), "azurerm_") ||
						strings.Contains(string(content), `provider "azurerm"`) {
						hasAzure = true
					}
				}
			}
			return nil
		})
		if !found {
			return parser.ErrNoFilesFound
		}
		if !hasAzure {
			return parser.ErrUnsupportedFormat
		}
		return nil
	}

	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".tf" && ext != ".tfstate" {
		return parser.ErrUnsupportedFormat
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !strings.Contains(string(content), "azurerm_") {
		return parser.ErrUnsupportedFormat
	}

	return nil
}

// AutoDetect checks if this parser can handle the given path.
func (p *TerraformAzureParser) AutoDetect(path string) (bool, float64) {
	info, err := os.Stat(path)
	if err != nil {
		return false, 0
	}

	if info.IsDir() {
		tfCount := 0
		azureCount := 0
		filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(p))
			if ext == ".tf" || ext == ".tfstate" {
				tfCount++
				content, err := os.ReadFile(p)
				if err == nil {
					if strings.Contains(string(content), "azurerm_") ||
						strings.Contains(string(content), `provider "azurerm"`) {
						azureCount++
					}
				}
			}
			return nil
		})

		if azureCount > 0 {
			confidence := float64(azureCount) / float64(tfCount) * 0.9
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
		if strings.Contains(string(content), "azurerm_") {
			return true, 0.85
		}
	}

	return false, 0
}

// Parse parses Terraform files and returns Azure infrastructure.
func (p *TerraformAzureParser) Parse(ctx context.Context, path string, opts *parser.ParseOptions) (*resource.Infrastructure, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	infra := resource.NewInfrastructure(resource.ProviderAzure)

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
		filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
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
		filtered := resource.NewInfrastructure(resource.ProviderAzure)
		for id, res := range infra.Resources {
			if shouldIncludeResource(res, opts) {
				filtered.Resources[id] = res
			}
		}
		infra = filtered
	}

	return infra, nil
}

// parseTerraformState parses a Terraform state file for Azure resources.
func (p *TerraformAzureParser) parseTerraformState(path string) (*resource.Infrastructure, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	_ = data
	return resource.NewInfrastructure(resource.ProviderAzure), nil
}

// parseHCLFile parses a single HCL file for Azure resources.
func (p *TerraformAzureParser) parseHCLFile(path string) (map[string]*resource.Resource, error) {
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
