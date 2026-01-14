// Package provider defines cloud provider types, regions, and metadata
// for both source (AWS, GCP, Azure) and target (Hetzner, Scaleway, OVH) providers.
package provider

// Provider represents a cloud provider identifier.
type Provider string

// Provider constants for all supported cloud providers.
const (
	// EU self-hosted providers (target providers for migration)
	ProviderHetzner  Provider = "hetzner"
	ProviderScaleway Provider = "scaleway"
	ProviderOVH      Provider = "ovh"

	// Reference providers (source providers for migration)
	ProviderAWS   Provider = "aws"
	ProviderGCP   Provider = "gcp"
	ProviderAzure Provider = "azure"
)

// Region represents a datacenter region for a cloud provider.
type Region struct {
	ID        string // Region identifier (e.g., "fsn1", "eu-west-1")
	Name      string // Display name (e.g., "Falkenstein")
	Location  string // City/country location (e.g., "Germany")
	Available bool   // Whether the region is currently available
}

// ProviderInfo contains metadata about a cloud provider.
type ProviderInfo struct {
	ID          Provider // Provider identifier
	DisplayName string   // Human-readable name
	Regions     []Region // Available regions
	IsEU        bool     // Whether provider is EU-based/GDPR compliant
	IsSupported bool     // Whether we can generate Terraform for this provider
}

// providerInfoMap holds static provider information.
var providerInfoMap = map[Provider]*ProviderInfo{
	ProviderHetzner: {
		ID:          ProviderHetzner,
		DisplayName: "Hetzner Cloud",
		IsEU:        true,
		IsSupported: true,
		Regions: []Region{
			{ID: "fsn1", Name: "Falkenstein", Location: "Germany", Available: true},
			{ID: "nbg1", Name: "Nuremberg", Location: "Germany", Available: true},
			{ID: "hel1", Name: "Helsinki", Location: "Finland", Available: true},
			{ID: "ash", Name: "Ashburn", Location: "USA", Available: true},
		},
	},
	ProviderScaleway: {
		ID:          ProviderScaleway,
		DisplayName: "Scaleway",
		IsEU:        true,
		IsSupported: true,
		Regions: []Region{
			{ID: "fr-par-1", Name: "Paris 1", Location: "France", Available: true},
			{ID: "fr-par-2", Name: "Paris 2", Location: "France", Available: true},
			{ID: "fr-par-3", Name: "Paris 3", Location: "France", Available: true},
			{ID: "nl-ams-1", Name: "Amsterdam", Location: "Netherlands", Available: true},
			{ID: "nl-ams-2", Name: "Amsterdam 2", Location: "Netherlands", Available: true},
			{ID: "nl-ams-3", Name: "Amsterdam 3", Location: "Netherlands", Available: true},
			{ID: "pl-waw-1", Name: "Warsaw 1", Location: "Poland", Available: true},
			{ID: "pl-waw-2", Name: "Warsaw 2", Location: "Poland", Available: true},
			{ID: "pl-waw-3", Name: "Warsaw 3", Location: "Poland", Available: true},
		},
	},
	ProviderOVH: {
		ID:          ProviderOVH,
		DisplayName: "OVHcloud",
		IsEU:        true,
		IsSupported: true,
		Regions: []Region{
			{ID: "gra", Name: "Gravelines", Location: "France", Available: true},
			{ID: "sbg", Name: "Strasbourg", Location: "France", Available: true},
			{ID: "bhs", Name: "Beauharnois", Location: "Canada", Available: true},
			{ID: "waw", Name: "Warsaw", Location: "Poland", Available: true},
			{ID: "uk1", Name: "UK", Location: "United Kingdom", Available: true},
		},
	},
	ProviderAWS: {
		ID:          ProviderAWS,
		DisplayName: "Amazon Web Services",
		IsEU:        false,
		IsSupported: false,
		Regions: []Region{
			{ID: "us-east-1", Name: "US East (N. Virginia)", Location: "USA", Available: true},
			{ID: "eu-west-1", Name: "EU (Ireland)", Location: "Ireland", Available: true},
			{ID: "eu-central-1", Name: "EU (Frankfurt)", Location: "Germany", Available: true},
		},
	},
	ProviderGCP: {
		ID:          ProviderGCP,
		DisplayName: "Google Cloud Platform",
		IsEU:        false,
		IsSupported: false,
		Regions: []Region{
			{ID: "us-central1", Name: "Iowa", Location: "USA", Available: true},
			{ID: "europe-west1", Name: "Belgium", Location: "Belgium", Available: true},
			{ID: "europe-west3", Name: "Frankfurt", Location: "Germany", Available: true},
		},
	},
	ProviderAzure: {
		ID:          ProviderAzure,
		DisplayName: "Microsoft Azure",
		IsEU:        false,
		IsSupported: false,
		Regions: []Region{
			{ID: "eastus", Name: "East US", Location: "USA", Available: true},
			{ID: "westeurope", Name: "West Europe", Location: "Netherlands", Available: true},
			{ID: "germanywestcentral", Name: "Germany West Central", Location: "Germany", Available: true},
		},
	},
}

// GetProviderInfo returns the provider information for the given provider.
// Returns nil if the provider is not recognized.
func GetProviderInfo(p Provider) *ProviderInfo {
	info, ok := providerInfoMap[p]
	if !ok {
		return nil
	}
	// Return a copy to prevent modification of the original
	infoCopy := *info
	infoCopy.Regions = make([]Region, len(info.Regions))
	copy(infoCopy.Regions, info.Regions)
	return &infoCopy
}

// AllProviders returns a list of all known providers.
func AllProviders() []Provider {
	return []Provider{
		ProviderHetzner,
		ProviderScaleway,
		ProviderOVH,
		ProviderAWS,
		ProviderGCP,
		ProviderAzure,
	}
}

// SupportedProviders returns a list of EU providers that support
// Terraform generation (target providers for migration).
func SupportedProviders() []Provider {
	var supported []Provider
	for _, p := range AllProviders() {
		info := providerInfoMap[p]
		if info != nil && info.IsSupported && info.IsEU {
			supported = append(supported, p)
		}
	}
	return supported
}

// IsEUProvider returns true if the provider is EU-based/GDPR compliant.
func IsEUProvider(p Provider) bool {
	info := providerInfoMap[p]
	if info == nil {
		return false
	}
	return info.IsEU
}

// String returns the string representation of the provider.
func (p Provider) String() string {
	return string(p)
}

// IsValid returns true if the provider is a recognized provider.
func (p Provider) IsValid() bool {
	_, ok := providerInfoMap[p]
	return ok
}

// GetRegion returns the region info for a specific region ID within this provider.
// Returns nil if the region is not found.
func (p Provider) GetRegion(regionID string) *Region {
	info := providerInfoMap[p]
	if info == nil {
		return nil
	}
	for _, r := range info.Regions {
		if r.ID == regionID {
			regionCopy := r
			return &regionCopy
		}
	}
	return nil
}
