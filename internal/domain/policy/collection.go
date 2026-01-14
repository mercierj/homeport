package policy

// PolicyCollection groups policies with summary stats.
type PolicyCollection struct {
	// Policies are all policies in the collection
	Policies []*Policy `json:"policies"`

	// Summary contains aggregate statistics
	Summary *PolicySummary `json:"summary"`
}

// PolicySummary contains aggregate statistics for a set of policies.
type PolicySummary struct {
	// TotalCount is the total number of policies
	TotalCount int `json:"total_count"`

	// ByType counts policies by type
	ByType map[PolicyType]int `json:"by_type"`

	// ByProvider counts policies by provider
	ByProvider map[Provider]int `json:"by_provider"`

	// WithWarnings counts policies that have warnings
	WithWarnings int `json:"with_warnings"`

	// HighConfidenceCount counts policies with mapping confidence > 0.8
	HighConfidenceCount int `json:"high_confidence_count"`

	// LowConfidenceCount counts policies with mapping confidence <= 0.5
	LowConfidenceCount int `json:"low_confidence_count"`

	// UnmappableCount counts policies that couldn't be mapped to Keycloak
	UnmappableCount int `json:"unmappable_count"`
}

// NewPolicyCollection creates a new collection from a slice of policies.
func NewPolicyCollection(policies []*Policy) *PolicyCollection {
	collection := &PolicyCollection{
		Policies: policies,
		Summary:  computeSummary(policies),
	}
	return collection
}

// computeSummary calculates summary statistics for policies.
func computeSummary(policies []*Policy) *PolicySummary {
	summary := &PolicySummary{
		TotalCount: len(policies),
		ByType:     make(map[PolicyType]int),
		ByProvider: make(map[Provider]int),
	}

	for _, p := range policies {
		summary.ByType[p.Type]++
		summary.ByProvider[p.Provider]++

		if p.HasWarnings() {
			summary.WithWarnings++
		}

		if p.KeycloakMapping != nil {
			if p.KeycloakMapping.MappingConfidence > 0.8 {
				summary.HighConfidenceCount++
			} else if p.KeycloakMapping.MappingConfidence <= 0.5 {
				summary.LowConfidenceCount++
			}
		} else if p.Type != PolicyTypeNetwork {
			// Non-network policies without mapping are unmappable
			summary.UnmappableCount++
		}
	}

	return summary
}

// Filter returns a new collection with policies matching the filter.
func (c *PolicyCollection) Filter(filter PolicyFilter) *PolicyCollection {
	var filtered []*Policy

	for _, p := range c.Policies {
		if filter.Matches(p) {
			filtered = append(filtered, p)
		}
	}

	return NewPolicyCollection(filtered)
}

// PolicyFilter defines criteria for filtering policies.
type PolicyFilter struct {
	// Types filters by policy type (nil means all)
	Types []PolicyType

	// Providers filters by provider (nil means all)
	Providers []Provider

	// HasWarnings filters policies with warnings
	HasWarnings *bool

	// ResourceID filters by resource ID (exact match)
	ResourceID string

	// ResourceType filters by resource type (exact match)
	ResourceType string

	// MinConfidence filters by minimum mapping confidence
	MinConfidence *float64

	// MaxConfidence filters by maximum mapping confidence
	MaxConfidence *float64

	// Search is a text search across name and resource name
	Search string
}

// Matches returns true if the policy matches the filter.
func (f PolicyFilter) Matches(p *Policy) bool {
	// Type filter
	if len(f.Types) > 0 {
		found := false
		for _, t := range f.Types {
			if p.Type == t {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Provider filter
	if len(f.Providers) > 0 {
		found := false
		for _, prov := range f.Providers {
			if p.Provider == prov {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Warnings filter
	if f.HasWarnings != nil {
		if *f.HasWarnings != p.HasWarnings() {
			return false
		}
	}

	// Resource ID filter
	if f.ResourceID != "" && p.ResourceID != f.ResourceID {
		return false
	}

	// Resource type filter
	if f.ResourceType != "" && p.ResourceType != f.ResourceType {
		return false
	}

	// Confidence filters
	if p.KeycloakMapping != nil {
		if f.MinConfidence != nil && p.KeycloakMapping.MappingConfidence < *f.MinConfidence {
			return false
		}
		if f.MaxConfidence != nil && p.KeycloakMapping.MappingConfidence > *f.MaxConfidence {
			return false
		}
	}

	// Text search
	if f.Search != "" {
		search := f.Search
		if !containsIgnoreCase(p.Name, search) && !containsIgnoreCase(p.ResourceName, search) {
			return false
		}
	}

	return true
}

// containsIgnoreCase checks if s contains substr (case-insensitive).
func containsIgnoreCase(s, substr string) bool {
	sLower := toLower(s)
	substrLower := toLower(substr)
	return len(substr) <= len(s) && indexOf(sLower, substrLower) >= 0
}

// toLower converts string to lowercase (simple ASCII).
func toLower(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		result[i] = c
	}
	return string(result)
}

// indexOf finds substring index.
func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
