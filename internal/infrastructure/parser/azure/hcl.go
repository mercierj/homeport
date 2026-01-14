package azure

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/zclconf/go-cty/cty"

	"github.com/homeport/homeport/internal/domain/parser"
	"github.com/homeport/homeport/internal/domain/resource"
)

// HCLParser parses Terraform HCL (.tf) files for Azure resources.
type HCLParser struct{}

// NewHCLParser creates a new Azure HCL parser.
func NewHCLParser() *HCLParser {
	return &HCLParser{}
}

// Provider returns the cloud provider.
func (p *HCLParser) Provider() resource.Provider {
	return resource.ProviderAzure
}

// SupportedFormats returns the supported formats.
func (p *HCLParser) SupportedFormats() []parser.Format {
	return []parser.Format{parser.FormatTerraform}
}

// Validate checks if the path contains valid Terraform HCL files with Azure resources.
func (p *HCLParser) Validate(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return parser.ErrInvalidPath
	}

	if info.IsDir() {
		found := false
		hasAzure := false
		err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if !info.IsDir() && strings.HasSuffix(strings.ToLower(filePath), ".tf") {
				found = true
				if p.hasAzureConfig(filePath) {
					hasAzure = true
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		if !found {
			return parser.ErrNoFilesFound
		}
		if !hasAzure {
			return parser.ErrUnsupportedFormat
		}
		return nil
	}

	if !strings.HasSuffix(strings.ToLower(path), ".tf") {
		return parser.ErrUnsupportedFormat
	}

	if !p.hasAzureConfig(path) {
		return parser.ErrUnsupportedFormat
	}

	return nil
}

// AutoDetect checks if this parser can handle the given path.
func (p *HCLParser) AutoDetect(path string) (bool, float64) {
	info, err := os.Stat(path)
	if err != nil {
		return false, 0
	}

	if info.IsDir() {
		tfCount := 0
		azureCount := 0
		filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if strings.HasSuffix(strings.ToLower(filePath), ".tf") {
				tfCount++
				if p.hasAzureConfig(filePath) {
					azureCount++
				}
			}
			return nil
		})

		if azureCount > 0 {
			confidence := 0.8
			if tfCount == azureCount {
				confidence = 0.9
			}
			return true, confidence
		}
		return false, 0
	}

	if strings.HasSuffix(strings.ToLower(path), ".tf") {
		if p.hasAzureConfig(path) {
			return true, 0.85
		}
	}

	return false, 0
}

// Parse parses Terraform HCL files and returns Azure infrastructure.
func (p *HCLParser) Parse(ctx context.Context, path string, opts *parser.ParseOptions) (*resource.Infrastructure, error) {
	if opts == nil {
		opts = parser.NewParseOptions()
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to stat path: %w", err)
	}

	infra := resource.NewInfrastructure(resource.ProviderAzure)

	if info.IsDir() {
		err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if strings.HasSuffix(strings.ToLower(filePath), ".tf") {
				if parseErr := p.parseHCLFile(filePath, infra, opts); parseErr != nil {
					if opts.IgnoreErrors {
						return nil
					}
					return parseErr
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		if err := p.parseHCLFile(path, infra, opts); err != nil {
			return nil, err
		}
	}

	return infra, nil
}

// hasAzureConfig checks if a file contains Azure configuration.
func (p *HCLParser) hasAzureConfig(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	content := string(data)
	return strings.Contains(content, `provider "azurerm"`) ||
		strings.Contains(content, `resource "azurerm_`) ||
		strings.Contains(content, `data "azurerm_`)
}

// parseHCLFile parses a single HCL file.
func (p *HCLParser) parseHCLFile(path string, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	hclParser := hclparse.NewParser()
	file, diags := hclParser.ParseHCLFile(path)
	if diags.HasErrors() {
		return fmt.Errorf("failed to parse HCL file: %s", diags.Error())
	}

	body := file.Body
	content, _, diags := body.PartialContent(&hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "resource", LabelNames: []string{"type", "name"}},
			{Type: "data", LabelNames: []string{"type", "name"}},
			{Type: "variable", LabelNames: []string{"name"}},
			{Type: "output", LabelNames: []string{"name"}},
			{Type: "locals"},
		},
	})
	if diags.HasErrors() {
		return fmt.Errorf("failed to decode HCL content: %s", diags.Error())
	}

	for _, block := range content.Blocks {
		if block.Type == "resource" && len(block.Labels) >= 2 {
			resourceType := block.Labels[0]
			resourceName := block.Labels[1]

			if !strings.HasPrefix(resourceType, "azurerm_") {
				continue
			}

			res := p.parseResourceBlock(resourceType, resourceName, block)

			if !p.shouldIncludeResource(res, opts) {
				continue
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// parseResourceBlock parses a resource block into our Resource model.
func (p *HCLParser) parseResourceBlock(resourceType, resourceName string, block *hcl.Block) *resource.Resource {
	resourceID := fmt.Sprintf("%s.%s", resourceType, resourceName)
	resType := mapAzureTerraformType(resourceType)

	res := resource.NewAWSResource(resourceID, resourceName, resType)
	res.Config["terraform_type"] = resourceType

	attrs, _ := block.Body.JustAttributes()
	for attrName, attr := range attrs {
		val, diags := attr.Expr.Value(nil)
		if !diags.HasErrors() {
			res.Config[attrName] = ctyValueToInterface(val)

			if attrName == "name" {
				if strVal, ok := res.Config[attrName].(string); ok && strVal != "" {
					res.Name = strVal
				}
			}

			if attrName == "location" {
				if strVal, ok := res.Config[attrName].(string); ok {
					res.Region = strVal
				}
			}

			if attrName == "tags" {
				if tags, ok := res.Config[attrName].(map[string]interface{}); ok {
					for k, v := range tags {
						if strVal, ok := v.(string); ok {
							res.Tags[k] = strVal
						}
					}
				}
			}
		}
	}

	return res
}

// ctyValueToInterface converts a cty.Value to a Go interface{}.
func ctyValueToInterface(val cty.Value) interface{} {
	if val.IsNull() {
		return nil
	}

	switch {
	case val.Type() == cty.String:
		return val.AsString()
	case val.Type() == cty.Number:
		f, _ := val.AsBigFloat().Float64()
		return f
	case val.Type() == cty.Bool:
		return val.True()
	case val.Type().IsListType() || val.Type().IsTupleType():
		var result []interface{}
		for it := val.ElementIterator(); it.Next(); {
			_, v := it.Element()
			result = append(result, ctyValueToInterface(v))
		}
		return result
	case val.Type().IsMapType() || val.Type().IsObjectType():
		result := make(map[string]interface{})
		for it := val.ElementIterator(); it.Next(); {
			k, v := it.Element()
			result[k.AsString()] = ctyValueToInterface(v)
		}
		return result
	default:
		return val.GoString()
	}
}

// shouldIncludeResource checks if a resource matches the filter criteria.
func (p *HCLParser) shouldIncludeResource(res *resource.Resource, opts *parser.ParseOptions) bool {
	if len(opts.FilterTypes) > 0 {
		found := false
		for _, ft := range opts.FilterTypes {
			if ft == res.Type {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if len(opts.FilterCategories) > 0 {
		category := res.Type.GetCategory()
		found := false
		for _, fc := range opts.FilterCategories {
			if fc == category {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}
