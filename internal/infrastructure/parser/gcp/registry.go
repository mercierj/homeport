package gcp

import (
	"github.com/homeport/homeport/internal/domain/parser"
)

// RegisterAll registers all GCP parsers with the provided registry.
func RegisterAll(registry *parser.Registry) {
	// Terraform parsers (dedicated)
	registry.Register(NewTFStateParser())
	registry.Register(NewHCLParser())

	// Legacy combined parser (for backwards compatibility)
	registry.Register(NewTerraformParser())

	// Deployment Manager parser
	registry.Register(NewDeploymentManagerParser())

	// API parser
	registry.Register(NewAPIParser())
}

// RegisterDefaults registers all GCP parsers with the default registry.
func RegisterDefaults() {
	RegisterAll(parser.DefaultRegistry())
}

// init registers the GCP parsers on package import.
func init() {
	RegisterDefaults()
}
