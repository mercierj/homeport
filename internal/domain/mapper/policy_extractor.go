package mapper

import (
	"context"

	"github.com/homeport/homeport/internal/domain/policy"
	"github.com/homeport/homeport/internal/domain/resource"
)

// PolicyExtractor is an interface for mappers that can extract policies.
type PolicyExtractor interface {
	// ExtractPolicies extracts policies from a resource.
	// Returns a slice of policies found on the resource.
	ExtractPolicies(ctx context.Context, res *resource.Resource) ([]*policy.Policy, error)
}

// GCPPolicyExtractor is an interface for GCP mappers that can extract policies.
type GCPPolicyExtractor interface {
	// ExtractPolicies extracts policies from a GCP resource.
	ExtractPolicies(ctx context.Context, res *resource.Resource) ([]*policy.Policy, error)
}

// AzurePolicyExtractor is an interface for Azure mappers that can extract policies.
type AzurePolicyExtractor interface {
	// ExtractPolicies extracts policies from an Azure resource.
	ExtractPolicies(ctx context.Context, res *resource.Resource) ([]*policy.Policy, error)
}
