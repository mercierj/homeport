package cutover

import (
	"context"
	"time"
)

// DNSRecordType represents the type of DNS record.
type DNSRecordType string

const (
	// DNSRecordTypeA is an IPv4 address record.
	DNSRecordTypeA DNSRecordType = "A"

	// DNSRecordTypeAAAA is an IPv6 address record.
	DNSRecordTypeAAAA DNSRecordType = "AAAA"

	// DNSRecordTypeCNAME is a canonical name record (alias).
	DNSRecordTypeCNAME DNSRecordType = "CNAME"

	// DNSRecordTypeMX is a mail exchange record.
	DNSRecordTypeMX DNSRecordType = "MX"

	// DNSRecordTypeTXT is a text record.
	DNSRecordTypeTXT DNSRecordType = "TXT"

	// DNSRecordTypeNS is a name server record.
	DNSRecordTypeNS DNSRecordType = "NS"

	// DNSRecordTypeSRV is a service record.
	DNSRecordTypeSRV DNSRecordType = "SRV"

	// DNSRecordTypeCAA is a certificate authority authorization record.
	DNSRecordTypeCAA DNSRecordType = "CAA"

	// DNSRecordTypePTR is a pointer record (reverse DNS).
	DNSRecordTypePTR DNSRecordType = "PTR"
)

// String returns the string representation of the DNS record type.
func (t DNSRecordType) String() string {
	return string(t)
}

// IsValid checks if the DNS record type is a recognized type.
func (t DNSRecordType) IsValid() bool {
	switch t {
	case DNSRecordTypeA, DNSRecordTypeAAAA, DNSRecordTypeCNAME,
		DNSRecordTypeMX, DNSRecordTypeTXT, DNSRecordTypeNS,
		DNSRecordTypeSRV, DNSRecordTypeCAA, DNSRecordTypePTR:
		return true
	default:
		return false
	}
}

// DNSProviderType represents the DNS provider service.
type DNSProviderType string

const (
	// DNSProviderCloudflare is Cloudflare DNS.
	DNSProviderCloudflare DNSProviderType = "cloudflare"

	// DNSProviderRoute53 is AWS Route 53.
	DNSProviderRoute53 DNSProviderType = "route53"

	// DNSProviderCloudDNS is Google Cloud DNS.
	DNSProviderCloudDNS DNSProviderType = "clouddns"

	// DNSProviderAzureDNS is Azure DNS.
	DNSProviderAzureDNS DNSProviderType = "azuredns"

	// DNSProviderDigitalOcean is DigitalOcean DNS.
	DNSProviderDigitalOcean DNSProviderType = "digitalocean"

	// DNSProviderManual indicates manual DNS changes (no API).
	DNSProviderManual DNSProviderType = "manual"
)

// String returns the string representation of the DNS provider type.
func (p DNSProviderType) String() string {
	return string(p)
}

// DisplayName returns a human-friendly display name for the DNS provider.
func (p DNSProviderType) DisplayName() string {
	switch p {
	case DNSProviderCloudflare:
		return "Cloudflare"
	case DNSProviderRoute53:
		return "AWS Route 53"
	case DNSProviderCloudDNS:
		return "Google Cloud DNS"
	case DNSProviderAzureDNS:
		return "Azure DNS"
	case DNSProviderDigitalOcean:
		return "DigitalOcean"
	case DNSProviderManual:
		return "Manual"
	default:
		return string(p)
	}
}

// SupportsAPI returns true if this provider supports automated DNS changes.
func (p DNSProviderType) SupportsAPI() bool {
	return p != DNSProviderManual
}

// DNSChange represents a DNS record change to be made during cutover.
// It captures both the old and new values to enable rollback.
type DNSChange struct {
	// ID is the unique identifier for this DNS change.
	ID string `json:"id"`

	// Domain is the root domain (e.g., "example.com").
	Domain string `json:"domain"`

	// RecordType is the DNS record type (A, CNAME, AAAA, TXT, etc.).
	RecordType string `json:"record_type"`

	// Name is the subdomain or @ for root (e.g., "www", "@", "api").
	Name string `json:"name"`

	// OldValue is the current record value (for rollback).
	OldValue string `json:"old_value"`

	// NewValue is the target record value after cutover.
	NewValue string `json:"new_value"`

	// TTL is the time-to-live in seconds.
	TTL int `json:"ttl"`

	// Priority is used for MX and SRV records (nil for other types).
	Priority *int `json:"priority,omitempty"`

	// Weight is used for SRV records (nil for other types).
	Weight *int `json:"weight,omitempty"`

	// Port is used for SRV records (nil for other types).
	Port *int `json:"port,omitempty"`

	// Provider is the DNS provider managing this record.
	Provider string `json:"provider"`

	// ProviderRecordID is the record ID in the DNS provider's system.
	ProviderRecordID string `json:"provider_record_id,omitempty"`

	// ProxyEnabled indicates if Cloudflare proxy is enabled (Cloudflare-specific).
	ProxyEnabled bool `json:"proxy_enabled,omitempty"`

	// Status tracks the change status (pending, applied, rolled_back).
	Status DNSChangeStatus `json:"status"`

	// AppliedAt is when the change was applied.
	AppliedAt *time.Time `json:"applied_at,omitempty"`

	// RolledBackAt is when the change was rolled back.
	RolledBackAt *time.Time `json:"rolled_back_at,omitempty"`

	// Error contains any error message from applying/rolling back.
	Error string `json:"error,omitempty"`
}

// DNSChangeStatus represents the status of a DNS change.
type DNSChangeStatus string

const (
	// DNSChangeStatusPending indicates the change has not been applied.
	DNSChangeStatusPending DNSChangeStatus = "pending"

	// DNSChangeStatusApplied indicates the change was successfully applied.
	DNSChangeStatusApplied DNSChangeStatus = "applied"

	// DNSChangeStatusRolledBack indicates the change was rolled back.
	DNSChangeStatusRolledBack DNSChangeStatus = "rolled_back"

	// DNSChangeStatusFailed indicates the change failed to apply.
	DNSChangeStatusFailed DNSChangeStatus = "failed"
)

// String returns the string representation of the DNS change status.
func (s DNSChangeStatus) String() string {
	return string(s)
}

// NewDNSChange creates a new DNSChange with default values.
func NewDNSChange(id, domain, recordType, name, oldValue, newValue string) *DNSChange {
	return &DNSChange{
		ID:         id,
		Domain:     domain,
		RecordType: recordType,
		Name:       name,
		OldValue:   oldValue,
		NewValue:   newValue,
		TTL:        300, // 5 minutes default
		Provider:   string(DNSProviderManual),
		Status:     DNSChangeStatusPending,
	}
}

// FullName returns the fully qualified domain name.
func (c *DNSChange) FullName() string {
	if c.Name == "@" || c.Name == "" {
		return c.Domain
	}
	return c.Name + "." + c.Domain
}

// IsApplied returns true if the change has been applied.
func (c *DNSChange) IsApplied() bool {
	return c.Status == DNSChangeStatusApplied
}

// CanRollback returns true if the change can be rolled back.
func (c *DNSChange) CanRollback() bool {
	return c.Status == DNSChangeStatusApplied && c.OldValue != ""
}

// Validate checks if the DNS change is valid.
func (c *DNSChange) Validate() []string {
	var errors []string

	if c.ID == "" {
		errors = append(errors, "DNS change ID is required")
	}

	if c.Domain == "" {
		errors = append(errors, "domain is required")
	}

	if c.RecordType == "" {
		errors = append(errors, "record type is required")
	}

	if c.Name == "" {
		errors = append(errors, "name is required (use @ for root)")
	}

	if c.NewValue == "" {
		errors = append(errors, "new value is required")
	}

	if c.TTL <= 0 {
		errors = append(errors, "TTL must be positive")
	}

	// Validate MX records require priority
	if c.RecordType == "MX" && c.Priority == nil {
		errors = append(errors, "MX records require priority")
	}

	// Validate SRV records require weight and port
	if c.RecordType == "SRV" {
		if c.Priority == nil {
			errors = append(errors, "SRV records require priority")
		}
		if c.Weight == nil {
			errors = append(errors, "SRV records require weight")
		}
		if c.Port == nil {
			errors = append(errors, "SRV records require port")
		}
	}

	return errors
}

// DNSRecord represents an existing DNS record from a provider.
type DNSRecord struct {
	// ID is the record's unique identifier in the provider's system.
	ID string `json:"id"`

	// Domain is the root domain (e.g., "example.com").
	Domain string `json:"domain"`

	// Type is the DNS record type.
	Type string `json:"type"`

	// Name is the subdomain or @ for root.
	Name string `json:"name"`

	// Value is the record value.
	Value string `json:"value"`

	// TTL is the time-to-live in seconds.
	TTL int `json:"ttl"`

	// Priority is the priority for MX/SRV records.
	Priority *int `json:"priority,omitempty"`

	// Weight is the weight for SRV records.
	Weight *int `json:"weight,omitempty"`

	// Port is the port for SRV records.
	Port *int `json:"port,omitempty"`

	// ProxyEnabled indicates if Cloudflare proxy is enabled.
	ProxyEnabled bool `json:"proxy_enabled,omitempty"`

	// CreatedAt is when the record was created.
	CreatedAt *time.Time `json:"created_at,omitempty"`

	// ModifiedAt is when the record was last modified.
	ModifiedAt *time.Time `json:"modified_at,omitempty"`
}

// FullName returns the fully qualified domain name.
func (r *DNSRecord) FullName() string {
	if r.Name == "@" || r.Name == "" {
		return r.Domain
	}
	return r.Name + "." + r.Domain
}

// ToChange creates a DNSChange from this record with a new target value.
func (r *DNSRecord) ToChange(id, newValue string) *DNSChange {
	return &DNSChange{
		ID:               id,
		Domain:           r.Domain,
		RecordType:       r.Type,
		Name:             r.Name,
		OldValue:         r.Value,
		NewValue:         newValue,
		TTL:              r.TTL,
		Priority:         r.Priority,
		Weight:           r.Weight,
		Port:             r.Port,
		ProviderRecordID: r.ID,
		ProxyEnabled:     r.ProxyEnabled,
		Status:           DNSChangeStatusPending,
	}
}

// DNSProvider defines the interface for DNS provider implementations.
// Implementations handle the actual API calls to manage DNS records.
type DNSProvider interface {
	// Name returns the provider name (e.g., "cloudflare", "route53").
	Name() string

	// ListRecords retrieves all DNS records for a domain.
	ListRecords(ctx context.Context, domain string) ([]*DNSRecord, error)

	// GetRecord retrieves a specific DNS record by ID.
	GetRecord(ctx context.Context, domain, recordID string) (*DNSRecord, error)

	// CreateRecord creates a new DNS record.
	CreateRecord(ctx context.Context, change *DNSChange) error

	// UpdateRecord updates an existing DNS record.
	UpdateRecord(ctx context.Context, change *DNSChange) error

	// DeleteRecord deletes a DNS record.
	DeleteRecord(ctx context.Context, domain, recordID string) error

	// ValidateCredentials checks if the provider credentials are valid.
	ValidateCredentials(ctx context.Context) error
}

// DNSProviderConfig contains configuration for a DNS provider.
type DNSProviderConfig struct {
	// Type is the provider type.
	Type DNSProviderType `json:"type"`

	// APIKey is the API key for authentication.
	APIKey string `json:"api_key,omitempty"`

	// APISecret is the API secret for authentication.
	APISecret string `json:"api_secret,omitempty"`

	// APIToken is an API token (alternative to key/secret).
	APIToken string `json:"api_token,omitempty"`

	// ZoneID is the zone identifier (Cloudflare, etc.).
	ZoneID string `json:"zone_id,omitempty"`

	// Region is the AWS region (for Route 53).
	Region string `json:"region,omitempty"`

	// ProjectID is the GCP project (for Cloud DNS).
	ProjectID string `json:"project_id,omitempty"`

	// SubscriptionID is the Azure subscription (for Azure DNS).
	SubscriptionID string `json:"subscription_id,omitempty"`

	// ResourceGroup is the Azure resource group (for Azure DNS).
	ResourceGroup string `json:"resource_group,omitempty"`
}

// DNSPropagationCheck represents a check for DNS propagation.
type DNSPropagationCheck struct {
	// Domain is the domain to check.
	Domain string `json:"domain"`

	// RecordType is the DNS record type.
	RecordType string `json:"record_type"`

	// ExpectedValue is the expected record value.
	ExpectedValue string `json:"expected_value"`

	// Nameservers is the list of nameservers to check.
	Nameservers []string `json:"nameservers,omitempty"`

	// Timeout is the maximum time to wait for propagation.
	Timeout time.Duration `json:"timeout"`

	// CheckInterval is how often to check for propagation.
	CheckInterval time.Duration `json:"check_interval"`

	// Propagated indicates if the record has propagated.
	Propagated bool `json:"propagated"`

	// PropagatedAt is when propagation was confirmed.
	PropagatedAt *time.Time `json:"propagated_at,omitempty"`

	// Results contains check results from each nameserver.
	Results []DNSPropagationResult `json:"results,omitempty"`
}

// NewDNSPropagationCheck creates a new propagation check with defaults.
func NewDNSPropagationCheck(domain, recordType, expectedValue string) *DNSPropagationCheck {
	return &DNSPropagationCheck{
		Domain:        domain,
		RecordType:    recordType,
		ExpectedValue: expectedValue,
		Nameservers: []string{
			"8.8.8.8",        // Google
			"1.1.1.1",        // Cloudflare
			"208.67.222.222", // OpenDNS
		},
		Timeout:       5 * time.Minute,
		CheckInterval: 10 * time.Second,
		Results:       make([]DNSPropagationResult, 0),
	}
}

// DNSPropagationResult represents the result of a propagation check.
type DNSPropagationResult struct {
	// Nameserver is the nameserver that was checked.
	Nameserver string `json:"nameserver"`

	// Value is the value returned by the nameserver.
	Value string `json:"value"`

	// Matches indicates if the value matches the expected value.
	Matches bool `json:"matches"`

	// Error contains any error from the check.
	Error string `json:"error,omitempty"`

	// CheckedAt is when this result was obtained.
	CheckedAt time.Time `json:"checked_at"`
}
