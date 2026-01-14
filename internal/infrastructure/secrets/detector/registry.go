package detector

import (
	"github.com/homeport/homeport/internal/domain/secrets"
)

// NewDefaultRegistry creates a registry with all default detectors.
func NewDefaultRegistry() *secrets.DetectorRegistry {
	registry := secrets.NewDetectorRegistry()

	// Register AWS detector
	registry.Register(NewAWSDetector())

	// Register GCP detector
	registry.Register(NewGCPDetector())

	// Register Azure detector
	registry.Register(NewAzureDetector())

	return registry
}
