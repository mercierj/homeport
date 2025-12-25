package azure

import (
	"github.com/cloudexit/cloudexit/internal/domain/parser"
)

// RegisterAll registers all Azure parsers with the provided registry.
func RegisterAll(registry *parser.Registry) {
	// Terraform parsers (dedicated)
	registry.Register(NewTFStateParser())
	registry.Register(NewHCLParser())

	// Legacy combined parser (for backwards compatibility)
	registry.Register(NewTerraformParser())

	// ARM parser
	registry.Register(NewARMParser())

	// Bicep parser
	registry.Register(NewBicepParser())

	// API parser
	registry.Register(NewAPIParser())
}

// RegisterDefaults registers all Azure parsers with the default registry.
func RegisterDefaults() {
	RegisterAll(parser.DefaultRegistry())
}

// init registers the Azure parsers on package import.
func init() {
	RegisterDefaults()
}
