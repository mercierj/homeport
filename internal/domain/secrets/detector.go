// Package secrets provides secret detection capabilities for cloud resources.
package secrets

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/homeport/homeport/internal/domain/resource"
)

// DetectedSecret represents a secret discovered from analyzing a cloud resource.
type DetectedSecret struct {
	// Name is the environment variable name for this secret (e.g., "PROD_DB_PASSWORD").
	Name string

	// Source indicates where this secret should be retrieved from.
	Source SecretSource

	// Key is the path/ARN/reference to the secret in the source system.
	// For aws-secrets-manager: ARN or secret name
	// For gcp-secret-manager: projects/PROJECT/secrets/NAME
	// For azure-key-vault: vault-name/secret-name
	Key string

	// Description provides human-readable context about the secret.
	Description string

	// Required indicates if this secret must be provided for deployment.
	Required bool

	// Type indicates the secret type for validation purposes.
	Type SecretType

	// ResourceID is the ID of the resource this secret was detected from.
	ResourceID string

	// ResourceName is the name of the resource this secret was detected from.
	ResourceName string

	// ResourceType is the type of resource this secret was detected from.
	ResourceType resource.Type

	// DeduplicationKey is used to identify duplicate secrets across resources.
	// Typically: Source + Key for cloud secrets, or Name for manual secrets.
	DeduplicationKey string
}

// ToSecretReference converts a DetectedSecret to a SecretReference.
func (d *DetectedSecret) ToSecretReference() *SecretReference {
	ref := NewSecretReference(d.Name, d.Source)
	ref.Key = d.Key
	ref.Description = d.Description
	ref.Required = d.Required
	ref.Type = d.Type
	if d.ResourceName != "" {
		ref.AddUsedBy(d.ResourceName)
	}
	return ref
}

// Detector is the interface for detecting secrets from cloud resources.
type Detector interface {
	// Detect analyzes a resource and returns detected secrets.
	// Returns an empty slice if no secrets are detected.
	Detect(ctx context.Context, res *resource.AWSResource) ([]*DetectedSecret, error)

	// SupportedTypes returns the resource types this detector supports.
	SupportedTypes() []resource.Type

	// Provider returns the cloud provider this detector is for.
	Provider() resource.Provider
}

// DetectorRegistry manages a collection of secret detectors.
type DetectorRegistry struct {
	mu        sync.RWMutex
	detectors map[resource.Provider]map[resource.Type]Detector
}

// NewDetectorRegistry creates a new detector registry.
func NewDetectorRegistry() *DetectorRegistry {
	return &DetectorRegistry{
		detectors: make(map[resource.Provider]map[resource.Type]Detector),
	}
}

// Register adds a detector to the registry.
func (r *DetectorRegistry) Register(detector Detector) {
	r.mu.Lock()
	defer r.mu.Unlock()

	provider := detector.Provider()
	if r.detectors[provider] == nil {
		r.detectors[provider] = make(map[resource.Type]Detector)
	}

	for _, t := range detector.SupportedTypes() {
		r.detectors[provider][t] = detector
	}
}

// GetDetector returns the detector for a specific resource type.
func (r *DetectorRegistry) GetDetector(resType resource.Type) (Detector, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	provider := resType.Provider()
	if r.detectors[provider] == nil {
		return nil, false
	}

	detector, ok := r.detectors[provider][resType]
	return detector, ok
}

// DetectAll analyzes all resources and returns a deduplicated secrets manifest.
func (r *DetectorRegistry) DetectAll(ctx context.Context, resources []*resource.AWSResource) (*SecretsManifest, error) {
	// Map for deduplication: deduplicationKey -> DetectedSecret
	secretsMap := make(map[string]*DetectedSecret)
	// Track which resources use each secret
	usedBy := make(map[string][]string)

	for _, res := range resources {
		detector, ok := r.GetDetector(res.Type)
		if !ok {
			continue
		}

		detected, err := detector.Detect(ctx, res)
		if err != nil {
			// Log error but continue with other resources
			continue
		}

		for _, secret := range detected {
			key := secret.DeduplicationKey
			if key == "" {
				// Generate default deduplication key
				key = generateDeduplicationKey(secret)
			}

			if existing, ok := secretsMap[key]; ok {
				// Secret already exists, just add to UsedBy
				if res.Name != "" && !contains(usedBy[key], res.Name) {
					usedBy[key] = append(usedBy[key], res.Name)
				}
				// If existing is optional but new is required, upgrade to required
				if secret.Required && !existing.Required {
					existing.Required = true
				}
			} else {
				secretsMap[key] = secret
				if res.Name != "" {
					usedBy[key] = []string{res.Name}
				}
			}
		}
	}

	// Convert map to manifest
	manifest := NewSecretsManifest()
	for key, secret := range secretsMap {
		ref := secret.ToSecretReference()

		// Add all UsedBy entries
		ref.UsedBy = usedBy[key]

		// Manual secrets don't have a key, skip validation for those
		if ref.Source == SourceManual {
			ref.Key = ""
		}

		// Add to manifest, handling duplicates gracefully
		if err := manifest.AddSecret(ref); err != nil {
			// If duplicate name, try with a suffix
			if strings.Contains(err.Error(), "duplicate") {
				ref.Name = ref.Name + "_" + strings.ToUpper(secret.ResourceID[:min(4, len(secret.ResourceID))])
				manifest.AddSecret(ref) // Ignore error on retry
			}
		}
	}

	// Sort secrets by name for consistent output
	sort.Slice(manifest.Secrets, func(i, j int) bool {
		return manifest.Secrets[i].Name < manifest.Secrets[j].Name
	})

	return manifest, nil
}

// generateDeduplicationKey creates a key for deduplication.
func generateDeduplicationKey(secret *DetectedSecret) string {
	if secret.Source.IsCloudProvider() && secret.Key != "" {
		// For cloud secrets, use source + key
		return string(secret.Source) + ":" + secret.Key
	}
	// For manual secrets, use the name
	return "manual:" + secret.Name
}

// contains checks if a string slice contains a value.
func contains(slice []string, value string) bool {
	for _, v := range slice {
		if v == value {
			return true
		}
	}
	return false
}

// min returns the minimum of two integers.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// BaseDetector provides common functionality for detectors.
type BaseDetector struct {
	provider       resource.Provider
	supportedTypes []resource.Type
}

// NewBaseDetector creates a new base detector.
func NewBaseDetector(provider resource.Provider, types ...resource.Type) *BaseDetector {
	return &BaseDetector{
		provider:       provider,
		supportedTypes: types,
	}
}

// Provider returns the cloud provider.
func (b *BaseDetector) Provider() resource.Provider {
	return b.provider
}

// SupportedTypes returns the supported resource types.
func (b *BaseDetector) SupportedTypes() []resource.Type {
	return b.supportedTypes
}

// GetConfigString safely extracts a string from resource config.
func GetConfigString(config map[string]interface{}, key string) string {
	if val, ok := config[key]; ok {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return ""
}

// GetConfigMap safely extracts a map from resource config.
func GetConfigMap(config map[string]interface{}, key string) map[string]interface{} {
	if val, ok := config[key]; ok {
		if m, ok := val.(map[string]interface{}); ok {
			return m
		}
	}
	return nil
}

// GetConfigList safely extracts a list from resource config.
func GetConfigList(config map[string]interface{}, key string) []interface{} {
	if val, ok := config[key]; ok {
		if l, ok := val.([]interface{}); ok {
			return l
		}
	}
	return nil
}

// GetConfigBool safely extracts a bool from resource config.
func GetConfigBool(config map[string]interface{}, key string) bool {
	if val, ok := config[key]; ok {
		if b, ok := val.(bool); ok {
			return b
		}
	}
	return false
}
