// Package providers provides services for cloud provider information,
// pricing, and cost comparison for migration planning.
package providers

import (
	"context"
	"errors"

	"github.com/homeport/homeport/internal/domain/provider"
)

// Common errors for provider operations.
var (
	ErrProviderNotFound = errors.New("provider not found")
)

// Service provides provider-related operations.
type Service struct{}

// NewService creates a new providers service.
func NewService() *Service {
	return &Service{}
}

// ListProviders returns a list of all known providers with their info.
func (s *Service) ListProviders(ctx context.Context) ([]*provider.ProviderInfo, error) {
	allProviders := provider.AllProviders()
	result := make([]*provider.ProviderInfo, 0, len(allProviders))

	for _, p := range allProviders {
		info := provider.GetProviderInfo(p)
		if info != nil {
			result = append(result, info)
		}
	}

	return result, nil
}

// GetProvider returns information about a specific provider.
func (s *Service) GetProvider(ctx context.Context, p provider.Provider) (*provider.ProviderInfo, error) {
	info := provider.GetProviderInfo(p)
	if info == nil {
		return nil, ErrProviderNotFound
	}
	return info, nil
}

// GetProviderRegions returns the available regions for a specific provider.
func (s *Service) GetProviderRegions(ctx context.Context, p provider.Provider) ([]provider.Region, error) {
	info := provider.GetProviderInfo(p)
	if info == nil {
		return nil, ErrProviderNotFound
	}
	return info.Regions, nil
}

// GetProviderInstances returns instance types and pricing for a specific provider.
func (s *Service) GetProviderInstances(ctx context.Context, p provider.Provider) ([]provider.InstancePricing, error) {
	instances := provider.GetInstanceTypes(p)
	if len(instances) == 0 {
		// Check if provider exists but has no instances
		info := provider.GetProviderInfo(p)
		if info == nil {
			return nil, ErrProviderNotFound
		}
	}
	return instances, nil
}

// CompareRequest represents a request to compare costs across providers.
type CompareRequest struct {
	// MappingResults contains the resource mappings from a previous analysis
	MappingResults interface{}

	// Providers is the list of providers to compare
	Providers []provider.Provider

	// HALevel is the high-availability level (e.g., "none", "basic", "full")
	HALevel string

	// EstimatedStorageGB is the estimated storage requirement in GB
	EstimatedStorageGB int

	// EstimatedEgressGB is the estimated monthly egress in GB
	EstimatedEgressGB int
}

// CostEstimate represents a cost estimate for a single provider.
type CostEstimate struct {
	Provider          provider.Provider
	DisplayName       string
	IsEU              bool
	Breakdown         *provider.CostBreakdown
	TotalMonthly      float64
	Currency          string
	Savings           float64
	SavingsPercentage float64
}

// CompareResponse represents the response from a provider cost comparison.
type CompareResponse struct {
	Estimates   []CostEstimate
	BestValue   provider.Provider
	CurrentCost float64
	Currency    string
}

// CompareProviders compares costs across multiple providers for the given infrastructure.
func (s *Service) CompareProviders(ctx context.Context, req CompareRequest) (*CompareResponse, error) {
	estimates := make([]CostEstimate, 0, len(req.Providers))

	var lowestCost float64
	var bestValue provider.Provider

	for _, p := range req.Providers {
		info := provider.GetProviderInfo(p)
		if info == nil {
			continue
		}

		pricing := provider.GetProviderPricing(p)
		if pricing == nil {
			continue
		}

		// Calculate costs based on the request parameters
		breakdown := &provider.CostBreakdown{
			Currency: pricing.Storage.Currency,
		}

		// Estimate storage cost
		if req.EstimatedStorageGB > 0 {
			breakdown.StorageCost = pricing.Storage.EstimateStorageCost(req.EstimatedStorageGB)
		}

		// Estimate egress cost
		if req.EstimatedEgressGB > 0 {
			breakdown.NetworkCost = pricing.Network.EstimateEgressCost(req.EstimatedEgressGB)
		}

		// For compute, use a simple heuristic based on HA level
		// In a real implementation, this would analyze the mapping results
		if len(pricing.Instances) > 0 {
			// Pick a mid-tier instance as baseline
			midIdx := len(pricing.Instances) / 2
			baseInstance := pricing.Instances[midIdx]

			instanceCount := 1
			switch req.HALevel {
			case "basic":
				instanceCount = 2
			case "full":
				instanceCount = 3
			}

			breakdown.ComputeCost = baseInstance.PricePerMonth * float64(instanceCount)
		}

		breakdown.CalculateTotal()

		estimate := CostEstimate{
			Provider:     p,
			DisplayName:  info.DisplayName,
			IsEU:         info.IsEU,
			Breakdown:    breakdown,
			TotalMonthly: breakdown.TotalMonthly,
			Currency:     breakdown.Currency,
		}

		estimates = append(estimates, estimate)

		// Track best value
		if lowestCost == 0 || breakdown.TotalMonthly < lowestCost {
			lowestCost = breakdown.TotalMonthly
			bestValue = p
		}
	}

	// Calculate savings relative to the most expensive option
	var highestCost float64
	for _, est := range estimates {
		if est.TotalMonthly > highestCost {
			highestCost = est.TotalMonthly
		}
	}

	for i := range estimates {
		if highestCost > 0 && estimates[i].TotalMonthly < highestCost {
			estimates[i].Savings = highestCost - estimates[i].TotalMonthly
			estimates[i].SavingsPercentage = (estimates[i].Savings / highestCost) * 100
		}
	}

	// Determine primary currency (prefer EUR for EU providers)
	currency := "EUR"
	if len(estimates) > 0 {
		currency = estimates[0].Currency
	}

	return &CompareResponse{
		Estimates:   estimates,
		BestValue:   bestValue,
		CurrentCost: highestCost,
		Currency:    currency,
	}, nil
}
