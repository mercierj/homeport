package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudexit/cloudexit/internal/domain/resource"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/zclconf/go-cty/cty"
)

// HCLParser handles parsing of Terraform HCL files
type HCLParser struct {
	parser *hclparse.Parser
}

// NewHCLParser creates a new HCL parser instance
func NewHCLParser() *HCLParser {
	return &HCLParser{
		parser: hclparse.NewParser(),
	}
}

// ParseHCLDirectory parses all .tf files in a directory
func (p *HCLParser) ParseHCLDirectory(dir string) (*HCLConfig, error) {
	config := &HCLConfig{
		Resources: make(map[string]*HCLResource),
		Variables: make(map[string]*HCLVariable),
		Locals:    make(map[string]interface{}),
		Outputs:   make(map[string]*HCLOutput),
	}

	// Find all .tf files
	tfFiles, err := filepath.Glob(filepath.Join(dir, "*.tf"))
	if err != nil {
		return nil, fmt.Errorf("failed to find .tf files: %w", err)
	}

	// Parse each file
	for _, file := range tfFiles {
		if err := p.parseHCLFile(file, config); err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", file, err)
		}
	}

	return config, nil
}

// parseHCLFile parses a single HCL file and adds content to config
func (p *HCLParser) parseHCLFile(path string, config *HCLConfig) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	var file *hcl.File
	var diags hcl.Diagnostics

	// Try to parse as HCL native syntax first
	if strings.HasSuffix(path, ".tf") {
		file, diags = p.parser.ParseHCL(src, path)
	} else {
		file, diags = p.parser.ParseJSON(src, path)
	}

	if diags.HasErrors() {
		return fmt.Errorf("HCL parse errors: %s", diags.Error())
	}

	// Extract blocks from the file
	body := file.Body
	content, _, diags := body.PartialContent(&hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "resource", LabelNames: []string{"type", "name"}},
			{Type: "variable", LabelNames: []string{"name"}},
			{Type: "locals"},
			{Type: "output", LabelNames: []string{"name"}},
			{Type: "provider", LabelNames: []string{"name"}},
		},
	})

	if diags.HasErrors() {
		return fmt.Errorf("failed to extract content: %s", diags.Error())
	}

	// Process blocks
	for _, block := range content.Blocks {
		switch block.Type {
		case "resource":
			if err := p.parseResourceBlock(block, config); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to parse resource block: %v\n", err)
			}
		case "variable":
			if err := p.parseVariableBlock(block, config); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to parse variable block: %v\n", err)
			}
		case "locals":
			if err := p.parseLocalsBlock(block, config); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to parse locals block: %v\n", err)
			}
		case "output":
			if err := p.parseOutputBlock(block, config); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to parse output block: %v\n", err)
			}
		}
	}

	return nil
}

// parseResourceBlock parses a resource block
func (p *HCLParser) parseResourceBlock(block *hcl.Block, config *HCLConfig) error {
	if len(block.Labels) < 2 {
		return fmt.Errorf("resource block requires two labels")
	}

	resourceType := block.Labels[0]
	resourceName := block.Labels[1]
	resourceKey := fmt.Sprintf("%s.%s", resourceType, resourceName)

	attrs, diags := block.Body.JustAttributes()
	if diags.HasErrors() {
		return fmt.Errorf("failed to extract attributes: %s", diags.Error())
	}

	hclRes := &HCLResource{
		Type:       resourceType,
		Name:       resourceName,
		Attributes: make(map[string]interface{}),
	}

	// Extract attributes
	for name, attr := range attrs {
		val, diags := attr.Expr.Value(nil)
		if diags.HasErrors() {
			// Store as string if we can't evaluate
			hclRes.Attributes[name] = fmt.Sprintf("%s", attr.Expr)
			continue
		}
		hclRes.Attributes[name] = ctyToInterface(val)
	}

	config.Resources[resourceKey] = hclRes
	return nil
}

// parseVariableBlock parses a variable block
func (p *HCLParser) parseVariableBlock(block *hcl.Block, config *HCLConfig) error {
	if len(block.Labels) < 1 {
		return fmt.Errorf("variable block requires a name label")
	}

	varName := block.Labels[0]
	attrs, diags := block.Body.JustAttributes()
	if diags.HasErrors() {
		return fmt.Errorf("failed to extract attributes: %s", diags.Error())
	}

	variable := &HCLVariable{
		Name: varName,
	}

	// Extract variable properties
	if descAttr, exists := attrs["description"]; exists {
		val, _ := descAttr.Expr.Value(nil)
		if val.Type() == cty.String {
			variable.Description = val.AsString()
		}
	}

	if typeAttr, exists := attrs["type"]; exists {
		variable.Type = fmt.Sprintf("%s", typeAttr.Expr)
	}

	if defaultAttr, exists := attrs["default"]; exists {
		val, diags := defaultAttr.Expr.Value(nil)
		if !diags.HasErrors() {
			variable.Default = ctyToInterface(val)
		}
	}

	config.Variables[varName] = variable
	return nil
}

// parseLocalsBlock parses a locals block
func (p *HCLParser) parseLocalsBlock(block *hcl.Block, config *HCLConfig) error {
	attrs, diags := block.Body.JustAttributes()
	if diags.HasErrors() {
		return fmt.Errorf("failed to extract attributes: %s", diags.Error())
	}

	for name, attr := range attrs {
		val, diags := attr.Expr.Value(nil)
		if diags.HasErrors() {
			config.Locals[name] = fmt.Sprintf("%s", attr.Expr)
			continue
		}
		config.Locals[name] = ctyToInterface(val)
	}

	return nil
}

// parseOutputBlock parses an output block
func (p *HCLParser) parseOutputBlock(block *hcl.Block, config *HCLConfig) error {
	if len(block.Labels) < 1 {
		return fmt.Errorf("output block requires a name label")
	}

	outputName := block.Labels[0]
	attrs, diags := block.Body.JustAttributes()
	if diags.HasErrors() {
		return fmt.Errorf("failed to extract attributes: %s", diags.Error())
	}

	output := &HCLOutput{
		Name: outputName,
	}

	if descAttr, exists := attrs["description"]; exists {
		val, _ := descAttr.Expr.Value(nil)
		if val.Type() == cty.String {
			output.Description = val.AsString()
		}
	}

	if valueAttr, exists := attrs["value"]; exists {
		val, diags := valueAttr.Expr.Value(nil)
		if !diags.HasErrors() {
			output.Value = ctyToInterface(val)
		} else {
			output.Value = fmt.Sprintf("%s", valueAttr.Expr)
		}
	}

	config.Outputs[outputName] = output
	return nil
}

// ctyToInterface converts a cty.Value to a Go interface{} type
func ctyToInterface(val cty.Value) interface{} {
	if val.IsNull() {
		return nil
	}

	valType := val.Type()

	switch {
	case valType == cty.String:
		return val.AsString()
	case valType == cty.Number:
		bf := val.AsBigFloat()
		if bf.IsInt() {
			i, _ := bf.Int64()
			return int(i)
		}
		f, _ := bf.Float64()
		return f
	case valType == cty.Bool:
		return val.True()
	case valType.IsListType() || valType.IsTupleType() || valType.IsSetType():
		var result []interface{}
		it := val.ElementIterator()
		for it.Next() {
			_, v := it.Element()
			result = append(result, ctyToInterface(v))
		}
		return result
	case valType.IsMapType() || valType.IsObjectType():
		result := make(map[string]interface{})
		it := val.ElementIterator()
		for it.Next() {
			k, v := it.Element()
			result[k.AsString()] = ctyToInterface(v)
		}
		return result
	default:
		return fmt.Sprintf("%#v", val)
	}
}

// HCLConfig represents the parsed HCL configuration
type HCLConfig struct {
	Resources map[string]*HCLResource
	Variables map[string]*HCLVariable
	Locals    map[string]interface{}
	Outputs   map[string]*HCLOutput
}

// HCLResource represents a resource definition in HCL
type HCLResource struct {
	Type       string
	Name       string
	Attributes map[string]interface{}
}

// HCLVariable represents a variable definition
type HCLVariable struct {
	Name        string
	Type        string
	Description string
	Default     interface{}
}

// HCLOutput represents an output definition
type HCLOutput struct {
	Name        string
	Description string
	Value       interface{}
	Sensitive   bool
}

// MergeHCLWithInfrastructure merges HCL configuration with infrastructure from state
func MergeHCLWithInfrastructure(infra *resource.Infrastructure, hclConfig *HCLConfig) error {
	if hclConfig == nil {
		return nil
	}

	// Add variables to metadata
	for varName, variable := range hclConfig.Variables {
		key := fmt.Sprintf("var.%s", varName)
		if variable.Default != nil {
			infra.Metadata[key] = fmt.Sprintf("%v", variable.Default)
		}
		if variable.Description != "" {
			infra.Metadata[fmt.Sprintf("%s.description", key)] = variable.Description
		}
	}

	// Add outputs to metadata
	for outName, output := range hclConfig.Outputs {
		key := fmt.Sprintf("output.%s", outName)
		if output.Value != nil {
			infra.Metadata[key] = fmt.Sprintf("%v", output.Value)
		}
		if output.Description != "" {
			infra.Metadata[fmt.Sprintf("%s.description", key)] = output.Description
		}
	}

	// Enrich resources with HCL configuration
	for _, res := range infra.Resources {
		// Find matching HCL resource
		parts := strings.SplitN(res.ID, ".", 2)
		if len(parts) < 2 {
			continue
		}

		resourceKey := fmt.Sprintf("%s.%s", parts[0], strings.Split(parts[1], "[")[0])
		if hclRes, exists := hclConfig.Resources[resourceKey]; exists {
			// Add any additional attributes from HCL that aren't in state
			for k, v := range hclRes.Attributes {
				if _, exists := res.Config[k]; !exists {
					res.Config[k] = v
				}
			}
		}
	}

	return nil
}
