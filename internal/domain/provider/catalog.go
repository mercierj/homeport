// Package provider defines cloud provider types, regions, metadata, and pricing catalogs
// for both source (AWS, GCP, Azure) and target (Hetzner, Scaleway, OVH) providers.
package provider

import (
	"time"

	"github.com/homeport/homeport/internal/domain/resource"
)

// catalogLastUpdated is the timestamp for when pricing data was last verified.
var catalogLastUpdated = time.Date(2024, 12, 1, 0, 0, 0, 0, time.UTC)

// providerPricingCatalog holds the complete pricing catalog for all providers.
var providerPricingCatalog = map[resource.Provider]*ProviderPricing{
	// ─────────────────────────────────────────────────────────────────────────
	// EU Self-Hosted Providers (Target Providers)
	// ─────────────────────────────────────────────────────────────────────────

	resource.Provider("hetzner"): {
		Provider: resource.Provider("hetzner"),
		Instances: []InstancePricing{
			// CX Series (Intel)
			{Type: "cx11", VCPUs: 1, MemoryGB: 2, StorageGB: 20, PricePerMonth: 3.49, PricePerHour: 0.0049, Currency: "EUR"},
			{Type: "cx21", VCPUs: 2, MemoryGB: 4, StorageGB: 40, PricePerMonth: 5.18, PricePerHour: 0.0072, Currency: "EUR"},
			{Type: "cx31", VCPUs: 2, MemoryGB: 8, StorageGB: 80, PricePerMonth: 9.18, PricePerHour: 0.0128, Currency: "EUR"},
			{Type: "cx41", VCPUs: 4, MemoryGB: 16, StorageGB: 160, PricePerMonth: 17.18, PricePerHour: 0.0239, Currency: "EUR"},
			// CPX Series (AMD)
			{Type: "cpx11", VCPUs: 2, MemoryGB: 2, StorageGB: 40, PricePerMonth: 4.49, PricePerHour: 0.0063, Currency: "EUR"},
			{Type: "cpx21", VCPUs: 3, MemoryGB: 4, StorageGB: 80, PricePerMonth: 8.39, PricePerHour: 0.0117, Currency: "EUR"},
			{Type: "cpx31", VCPUs: 4, MemoryGB: 8, StorageGB: 160, PricePerMonth: 15.49, PricePerHour: 0.0215, Currency: "EUR"},
			{Type: "cpx41", VCPUs: 8, MemoryGB: 16, StorageGB: 240, PricePerMonth: 29.49, PricePerHour: 0.0410, Currency: "EUR"},
		},
		Storage: StoragePricing{
			Type:            "SSD volumes",
			PricePerGBMonth: 0.044,
			MinSizeGB:       10,
			MaxSizeGB:       10240,
			Currency:        "EUR",
		},
		Network: NetworkPricing{
			IngressFree:      true,
			FreeEgressGB:     1024, // 1TB
			EgressPricePerGB: 0.001, // €1.00/TB = €0.001/GB
			Currency:         "EUR",
		},
		LastUpdated: catalogLastUpdated,
	},

	resource.Provider("scaleway"): {
		Provider: resource.Provider("scaleway"),
		Instances: []InstancePricing{
			// DEV1 Series (Development)
			{Type: "DEV1-S", VCPUs: 2, MemoryGB: 2, StorageGB: 20, PricePerMonth: 7.99, PricePerHour: 0.0111, Currency: "EUR"},
			{Type: "DEV1-M", VCPUs: 3, MemoryGB: 4, StorageGB: 40, PricePerMonth: 15.99, PricePerHour: 0.0222, Currency: "EUR"},
			{Type: "DEV1-L", VCPUs: 4, MemoryGB: 8, StorageGB: 80, PricePerMonth: 31.99, PricePerHour: 0.0444, Currency: "EUR"},
			// GP1 Series (General Purpose)
			{Type: "GP1-XS", VCPUs: 4, MemoryGB: 16, StorageGB: 150, PricePerMonth: 34.00, PricePerHour: 0.0472, Currency: "EUR"},
			{Type: "GP1-S", VCPUs: 8, MemoryGB: 32, StorageGB: 300, PricePerMonth: 68.00, PricePerHour: 0.0944, Currency: "EUR"},
		},
		Storage: StoragePricing{
			Type:            "Block storage",
			PricePerGBMonth: 0.06,
			MinSizeGB:       1,
			MaxSizeGB:       10000,
			Currency:        "EUR",
		},
		Network: NetworkPricing{
			IngressFree:      true,
			FreeEgressGB:     75,
			EgressPricePerGB: 0.01,
			Currency:         "EUR",
		},
		LastUpdated: catalogLastUpdated,
	},

	resource.Provider("ovh"): {
		Provider: resource.Provider("ovh"),
		Instances: []InstancePricing{
			// S1 Series (Starter)
			{Type: "s1-2", VCPUs: 1, MemoryGB: 2, StorageGB: 10, PricePerMonth: 5.49, PricePerHour: 0.0076, Currency: "EUR"},
			{Type: "s1-4", VCPUs: 1, MemoryGB: 4, StorageGB: 20, PricePerMonth: 10.99, PricePerHour: 0.0153, Currency: "EUR"},
			{Type: "s1-8", VCPUs: 2, MemoryGB: 8, StorageGB: 40, PricePerMonth: 21.99, PricePerHour: 0.0306, Currency: "EUR"},
			// B2 Series (General Purpose)
			{Type: "b2-7", VCPUs: 2, MemoryGB: 7, StorageGB: 50, PricePerMonth: 26.99, PricePerHour: 0.0375, Currency: "EUR"},
			{Type: "b2-15", VCPUs: 4, MemoryGB: 15, StorageGB: 100, PricePerMonth: 53.99, PricePerHour: 0.0750, Currency: "EUR"},
		},
		Storage: StoragePricing{
			Type:            "Block storage",
			PricePerGBMonth: 0.04,
			MinSizeGB:       10,
			MaxSizeGB:       4000,
			Currency:        "EUR",
		},
		Network: NetworkPricing{
			IngressFree:      true,
			FreeEgressGB:     0, // Unlimited (represented as 0 = no limit applied)
			EgressPricePerGB: 0, // Free egress
			Currency:         "EUR",
		},
		LastUpdated: catalogLastUpdated,
	},

	// ─────────────────────────────────────────────────────────────────────────
	// Reference Providers (Source Providers - for comparison)
	// ─────────────────────────────────────────────────────────────────────────

	resource.ProviderAWS: {
		Provider: resource.ProviderAWS,
		Instances: []InstancePricing{
			// T3 Series (Burstable)
			{Type: "t3.micro", VCPUs: 2, MemoryGB: 1, StorageGB: 0, PricePerMonth: 8.47, PricePerHour: 0.0118, Currency: "USD"},
			{Type: "t3.small", VCPUs: 2, MemoryGB: 2, StorageGB: 0, PricePerMonth: 16.94, PricePerHour: 0.0235, Currency: "USD"},
			{Type: "t3.medium", VCPUs: 2, MemoryGB: 4, StorageGB: 0, PricePerMonth: 33.87, PricePerHour: 0.0471, Currency: "USD"},
			{Type: "t3.large", VCPUs: 2, MemoryGB: 8, StorageGB: 0, PricePerMonth: 67.74, PricePerHour: 0.0941, Currency: "USD"},
			// M5 Series (General Purpose)
			{Type: "m5.large", VCPUs: 2, MemoryGB: 8, StorageGB: 0, PricePerMonth: 89.28, PricePerHour: 0.1240, Currency: "USD"},
		},
		Storage: StoragePricing{
			Type:            "gp3",
			PricePerGBMonth: 0.10,
			MinSizeGB:       1,
			MaxSizeGB:       16384,
			Currency:        "USD",
		},
		Network: NetworkPricing{
			IngressFree:      true,
			FreeEgressGB:     0,
			EgressPricePerGB: 0.09,
			Currency:         "USD",
		},
		LastUpdated: catalogLastUpdated,
	},

	resource.ProviderGCP: {
		Provider: resource.ProviderGCP,
		Instances: []InstancePricing{
			// E2 Series (Cost-optimized)
			{Type: "e2-micro", VCPUs: 2, MemoryGB: 1, StorageGB: 0, PricePerMonth: 6.11, PricePerHour: 0.0085, Currency: "USD"},
			{Type: "e2-small", VCPUs: 2, MemoryGB: 2, StorageGB: 0, PricePerMonth: 12.23, PricePerHour: 0.0170, Currency: "USD"},
			{Type: "e2-medium", VCPUs: 2, MemoryGB: 4, StorageGB: 0, PricePerMonth: 24.46, PricePerHour: 0.0340, Currency: "USD"},
			{Type: "e2-standard-2", VCPUs: 2, MemoryGB: 8, StorageGB: 0, PricePerMonth: 48.92, PricePerHour: 0.0680, Currency: "USD"},
			// N2 Series (Balanced)
			{Type: "n2-standard-2", VCPUs: 2, MemoryGB: 8, StorageGB: 0, PricePerMonth: 71.54, PricePerHour: 0.0994, Currency: "USD"},
		},
		Storage: StoragePricing{
			Type:            "pd-ssd",
			PricePerGBMonth: 0.10,
			MinSizeGB:       10,
			MaxSizeGB:       65536,
			Currency:        "USD",
		},
		Network: NetworkPricing{
			IngressFree:      true,
			FreeEgressGB:     0,
			EgressPricePerGB: 0.12,
			Currency:         "USD",
		},
		LastUpdated: catalogLastUpdated,
	},

	resource.ProviderAzure: {
		Provider: resource.ProviderAzure,
		Instances: []InstancePricing{
			// B Series (Burstable)
			{Type: "B1s", VCPUs: 1, MemoryGB: 1, StorageGB: 0, PricePerMonth: 7.59, PricePerHour: 0.0106, Currency: "USD"},
			{Type: "B1ms", VCPUs: 1, MemoryGB: 2, StorageGB: 0, PricePerMonth: 15.18, PricePerHour: 0.0211, Currency: "USD"},
			{Type: "B2s", VCPUs: 2, MemoryGB: 4, StorageGB: 0, PricePerMonth: 30.37, PricePerHour: 0.0422, Currency: "USD"},
			{Type: "B2ms", VCPUs: 2, MemoryGB: 8, StorageGB: 0, PricePerMonth: 60.74, PricePerHour: 0.0844, Currency: "USD"},
			// D Series (General Purpose)
			{Type: "D2s_v3", VCPUs: 2, MemoryGB: 8, StorageGB: 0, PricePerMonth: 87.60, PricePerHour: 0.1217, Currency: "USD"},
		},
		Storage: StoragePricing{
			Type:            "Premium SSD",
			PricePerGBMonth: 0.12,
			MinSizeGB:       4,
			MaxSizeGB:       32767,
			Currency:        "USD",
		},
		Network: NetworkPricing{
			IngressFree:      true,
			FreeEgressGB:     0,
			EgressPricePerGB: 0.087,
			Currency:         "USD",
		},
		LastUpdated: catalogLastUpdated,
	},
}

// GetProviderPricing returns complete pricing data for the given provider.
// Returns nil if the provider is not recognized.
// Accepts both resource.Provider (aws, gcp, azure) and provider.Provider (hetzner, scaleway, ovh).
func GetProviderPricing(provider Provider) *ProviderPricing {
	// First try with the provider as resource.Provider
	resProvider := resource.Provider(string(provider))
	pricing, ok := providerPricingCatalog[resProvider]
	if !ok {
		return nil
	}
	// Return a copy to prevent modification of the original
	pricingCopy := *pricing
	pricingCopy.Instances = make([]InstancePricing, len(pricing.Instances))
	copy(pricingCopy.Instances, pricing.Instances)
	return &pricingCopy
}

// GetInstanceTypes returns all instance types for the given provider.
// Returns an empty slice if the provider is not recognized.
func GetInstanceTypes(provider Provider) []InstancePricing {
	resProvider := resource.Provider(string(provider))
	pricing := providerPricingCatalog[resProvider]
	if pricing == nil {
		return []InstancePricing{}
	}
	// Return a copy to prevent modification of the original
	instances := make([]InstancePricing, len(pricing.Instances))
	copy(instances, pricing.Instances)
	return instances
}

// FindInstance finds a specific instance type for the given provider.
// Returns nil if the provider or instance type is not found.
func FindInstance(provider Provider, instanceType string) *InstancePricing {
	resProvider := resource.Provider(string(provider))
	pricing := providerPricingCatalog[resProvider]
	if pricing == nil {
		return nil
	}
	for _, instance := range pricing.Instances {
		if instance.Type == instanceType {
			instanceCopy := instance
			return &instanceCopy
		}
	}
	return nil
}

// GetAllProviderPricing returns pricing data for all providers.
func GetAllProviderPricing() map[Provider]*ProviderPricing {
	result := make(map[Provider]*ProviderPricing)
	for resProvider := range providerPricingCatalog {
		provider := Provider(string(resProvider))
		result[provider] = GetProviderPricing(provider)
	}
	return result
}

// GetEUProviderPricing returns pricing data for EU providers only (Hetzner, Scaleway, OVH).
func GetEUProviderPricing() []*ProviderPricing {
	euProviders := []Provider{ProviderHetzner, ProviderScaleway, ProviderOVH}
	var result []*ProviderPricing
	for _, p := range euProviders {
		if pricing := GetProviderPricing(p); pricing != nil {
			result = append(result, pricing)
		}
	}
	return result
}

// GetSupportedProviderPricing returns pricing data for supported target providers.
// These are the EU providers that can be used as migration targets.
func GetSupportedProviderPricing() []*ProviderPricing {
	return GetEUProviderPricing()
}

// GetReferenceProviderPricing returns pricing data for reference (source) providers.
// These are AWS, GCP, and Azure - used for comparison with EU alternatives.
func GetReferenceProviderPricing() []*ProviderPricing {
	refProviders := []resource.Provider{resource.ProviderAWS, resource.ProviderGCP, resource.ProviderAzure}
	var result []*ProviderPricing
	for _, p := range refProviders {
		if pricing := providerPricingCatalog[p]; pricing != nil {
			pricingCopy := *pricing
			pricingCopy.Instances = make([]InstancePricing, len(pricing.Instances))
			copy(pricingCopy.Instances, pricing.Instances)
			result = append(result, &pricingCopy)
		}
	}
	return result
}
