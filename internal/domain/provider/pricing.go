// Package provider defines pricing structures for self-hosted infrastructure providers.
// This includes compute instances, storage, and network pricing for providers like
// Hetzner, Scaleway, OVH, and other alternatives to major cloud vendors.
package provider

import (
	"time"

	"github.com/homeport/homeport/internal/domain/resource"
)

// InstancePricing represents the pricing and specifications for a compute instance type.
type InstancePricing struct {
	// Type is the instance type identifier (e.g., "cx21", "DEV1-S", "s-1vcpu-1gb")
	Type string `json:"type"`

	// VCPUs is the number of virtual CPU cores
	VCPUs int `json:"vcpus"`

	// MemoryGB is the amount of RAM in gigabytes
	MemoryGB float64 `json:"memory_gb"`

	// StorageGB is the included local SSD storage in gigabytes (0 if none included)
	StorageGB int `json:"storage_gb"`

	// PricePerMonth is the monthly cost for this instance type
	PricePerMonth float64 `json:"price_per_month"`

	// PricePerHour is the hourly cost for this instance type
	PricePerHour float64 `json:"price_per_hour"`

	// Currency is the pricing currency (e.g., "EUR", "USD")
	Currency string `json:"currency"`
}

// StoragePricing represents the pricing for block or object storage.
type StoragePricing struct {
	// Type is the storage type identifier (e.g., "ssd", "hdd", "nvme")
	Type string `json:"type"`

	// PricePerGBMonth is the cost per gigabyte per month
	PricePerGBMonth float64 `json:"price_per_gb_month"`

	// MinSizeGB is the minimum storage size that can be provisioned
	MinSizeGB int `json:"min_size_gb"`

	// MaxSizeGB is the maximum storage size that can be provisioned
	MaxSizeGB int `json:"max_size_gb"`

	// Currency is the pricing currency (e.g., "EUR", "USD")
	Currency string `json:"currency"`
}

// NetworkPricing represents the pricing for network traffic and bandwidth.
type NetworkPricing struct {
	// IngressFree indicates whether incoming traffic is free
	IngressFree bool `json:"ingress_free"`

	// EgressPricePerGB is the cost per gigabyte of outgoing traffic
	EgressPricePerGB float64 `json:"egress_price_per_gb"`

	// FreeEgressGB is the amount of free outgoing traffic included per month
	FreeEgressGB int `json:"free_egress_gb"`

	// Currency is the pricing currency (e.g., "EUR", "USD")
	Currency string `json:"currency"`
}

// ProviderPricing aggregates all pricing information for a specific provider.
type ProviderPricing struct {
	// Provider is the cloud/hosting provider identifier
	Provider resource.Provider `json:"provider"`

	// Instances contains the available instance types and their pricing
	Instances []InstancePricing `json:"instances"`

	// Storage contains the storage pricing information
	Storage StoragePricing `json:"storage"`

	// Network contains the network traffic pricing information
	Network NetworkPricing `json:"network"`

	// LastUpdated is when this pricing information was last refreshed
	LastUpdated time.Time `json:"last_updated"`
}

// CostBreakdown provides a detailed breakdown of estimated costs.
type CostBreakdown struct {
	// ComputeCost is the monthly cost for compute resources
	ComputeCost float64 `json:"compute_cost"`

	// StorageCost is the monthly cost for storage resources
	StorageCost float64 `json:"storage_cost"`

	// NetworkCost is the monthly cost for network traffic
	NetworkCost float64 `json:"network_cost"`

	// TotalMonthly is the total estimated monthly cost
	TotalMonthly float64 `json:"total_monthly"`

	// Currency is the pricing currency (e.g., "EUR", "USD")
	Currency string `json:"currency"`
}

// CalculateTotal computes the total monthly cost from individual components.
func (c *CostBreakdown) CalculateTotal() {
	c.TotalMonthly = c.ComputeCost + c.StorageCost + c.NetworkCost
}

// EstimateStorageCost calculates the monthly storage cost for a given size.
func (s *StoragePricing) EstimateStorageCost(sizeGB int) float64 {
	if sizeGB < s.MinSizeGB {
		sizeGB = s.MinSizeGB
	}
	if s.MaxSizeGB > 0 && sizeGB > s.MaxSizeGB {
		sizeGB = s.MaxSizeGB
	}
	return float64(sizeGB) * s.PricePerGBMonth
}

// EstimateEgressCost calculates the monthly egress cost for a given amount of traffic.
func (n *NetworkPricing) EstimateEgressCost(egressGB int) float64 {
	billableGB := egressGB - n.FreeEgressGB
	if billableGB <= 0 {
		return 0
	}
	return float64(billableGB) * n.EgressPricePerGB
}
