package dns

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/homeport/homeport/internal/domain/cutover"
)

const cloudflareAPIBase = "https://api.cloudflare.com/client/v4"

// CloudflareProvider implements DNS operations via Cloudflare API.
type CloudflareProvider struct {
	// apiToken is the Cloudflare API token.
	apiToken string

	// zoneID is the Cloudflare zone ID.
	zoneID string

	// client is the HTTP client.
	client *http.Client
}

// CloudflareConfig contains Cloudflare-specific configuration.
type CloudflareConfig struct {
	APIToken string
	ZoneID   string
}

// NewCloudflareProvider creates a new Cloudflare DNS provider.
func NewCloudflareProvider(config *CloudflareConfig) *CloudflareProvider {
	return &CloudflareProvider{
		apiToken: config.APIToken,
		zoneID:   config.ZoneID,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Name returns the provider name.
func (p *CloudflareProvider) Name() string {
	return "cloudflare"
}

// cloudflareResponse is the standard Cloudflare API response wrapper.
type cloudflareResponse struct {
	Success  bool                     `json:"success"`
	Errors   []cloudflareError        `json:"errors"`
	Messages []string                 `json:"messages"`
	Result   json.RawMessage          `json:"result"`
	ResultInfo *cloudflareResultInfo  `json:"result_info,omitempty"`
}

type cloudflareError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type cloudflareResultInfo struct {
	Page       int `json:"page"`
	PerPage    int `json:"per_page"`
	TotalCount int `json:"total_count"`
	TotalPages int `json:"total_pages"`
}

// cloudflareRecord represents a DNS record in Cloudflare.
type cloudflareRecord struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Name      string `json:"name"`
	Content   string `json:"content"`
	TTL       int    `json:"ttl"`
	Proxied   bool   `json:"proxied"`
	ZoneID    string `json:"zone_id"`
	ZoneName  string `json:"zone_name"`
	CreatedOn string `json:"created_on"`
	ModifiedOn string `json:"modified_on"`
	Priority  *int   `json:"priority,omitempty"`
}

// ListRecords retrieves all DNS records for a domain.
func (p *CloudflareProvider) ListRecords(ctx context.Context, domain string) ([]*cutover.DNSRecord, error) {
	url := fmt.Sprintf("%s/zones/%s/dns_records", cloudflareAPIBase, p.zoneID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	p.setHeaders(req)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var cfResp cloudflareResponse
	if err := json.NewDecoder(resp.Body).Decode(&cfResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !cfResp.Success {
		return nil, fmt.Errorf("cloudflare API error: %v", cfResp.Errors)
	}

	var records []cloudflareRecord
	if err := json.Unmarshal(cfResp.Result, &records); err != nil {
		return nil, fmt.Errorf("failed to parse records: %w", err)
	}

	result := make([]*cutover.DNSRecord, 0, len(records))
	for _, r := range records {
		record := &cutover.DNSRecord{
			ID:           r.ID,
			Domain:       domain,
			Type:         r.Type,
			Name:         r.Name,
			Value:        r.Content,
			TTL:          r.TTL,
			Priority:     r.Priority,
			ProxyEnabled: r.Proxied,
		}
		result = append(result, record)
	}

	return result, nil
}

// GetRecord retrieves a specific DNS record by ID.
func (p *CloudflareProvider) GetRecord(ctx context.Context, domain, recordID string) (*cutover.DNSRecord, error) {
	url := fmt.Sprintf("%s/zones/%s/dns_records/%s", cloudflareAPIBase, p.zoneID, recordID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	p.setHeaders(req)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var cfResp cloudflareResponse
	if err := json.NewDecoder(resp.Body).Decode(&cfResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !cfResp.Success {
		return nil, fmt.Errorf("cloudflare API error: %v", cfResp.Errors)
	}

	var record cloudflareRecord
	if err := json.Unmarshal(cfResp.Result, &record); err != nil {
		return nil, fmt.Errorf("failed to parse record: %w", err)
	}

	return &cutover.DNSRecord{
		ID:           record.ID,
		Domain:       domain,
		Type:         record.Type,
		Name:         record.Name,
		Value:        record.Content,
		TTL:          record.TTL,
		Priority:     record.Priority,
		ProxyEnabled: record.Proxied,
	}, nil
}

// CreateRecord creates a new DNS record.
func (p *CloudflareProvider) CreateRecord(ctx context.Context, change *cutover.DNSChange) error {
	url := fmt.Sprintf("%s/zones/%s/dns_records", cloudflareAPIBase, p.zoneID)

	body := map[string]interface{}{
		"type":    change.RecordType,
		"name":    change.Name,
		"content": change.NewValue,
		"ttl":     change.TTL,
		"proxied": change.ProxyEnabled,
	}

	if change.Priority != nil {
		body["priority"] = *change.Priority
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	p.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var cfResp cloudflareResponse
	if err := json.NewDecoder(resp.Body).Decode(&cfResp); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if !cfResp.Success {
		return fmt.Errorf("cloudflare API error: %v", cfResp.Errors)
	}

	// Extract the record ID from the response
	var record cloudflareRecord
	if err := json.Unmarshal(cfResp.Result, &record); err == nil {
		change.ProviderRecordID = record.ID
	}

	change.Status = cutover.DNSChangeStatusApplied
	now := time.Now()
	change.AppliedAt = &now

	return nil
}

// UpdateRecord updates an existing DNS record.
func (p *CloudflareProvider) UpdateRecord(ctx context.Context, change *cutover.DNSChange) error {
	// If we don't have a record ID, we need to find the record first
	if change.ProviderRecordID == "" {
		records, err := p.ListRecords(ctx, change.Domain)
		if err != nil {
			return fmt.Errorf("failed to list records: %w", err)
		}

		for _, r := range records {
			// Match by name and type
			if r.Name == change.FullName() && r.Type == change.RecordType {
				change.ProviderRecordID = r.ID
				break
			}
		}

		if change.ProviderRecordID == "" {
			// Record doesn't exist, create it
			return p.CreateRecord(ctx, change)
		}
	}

	url := fmt.Sprintf("%s/zones/%s/dns_records/%s", cloudflareAPIBase, p.zoneID, change.ProviderRecordID)

	body := map[string]interface{}{
		"type":    change.RecordType,
		"name":    change.Name,
		"content": change.NewValue,
		"ttl":     change.TTL,
		"proxied": change.ProxyEnabled,
	}

	if change.Priority != nil {
		body["priority"] = *change.Priority
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "PUT", url, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	p.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)

	var cfResp cloudflareResponse
	if err := json.Unmarshal(respBody, &cfResp); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if !cfResp.Success {
		return fmt.Errorf("cloudflare API error: %v", cfResp.Errors)
	}

	change.Status = cutover.DNSChangeStatusApplied
	now := time.Now()
	change.AppliedAt = &now

	return nil
}

// DeleteRecord deletes a DNS record.
func (p *CloudflareProvider) DeleteRecord(ctx context.Context, domain, recordID string) error {
	url := fmt.Sprintf("%s/zones/%s/dns_records/%s", cloudflareAPIBase, p.zoneID, recordID)

	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	p.setHeaders(req)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var cfResp cloudflareResponse
	if err := json.NewDecoder(resp.Body).Decode(&cfResp); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if !cfResp.Success {
		return fmt.Errorf("cloudflare API error: %v", cfResp.Errors)
	}

	return nil
}

// ValidateCredentials checks if the provider credentials are valid.
func (p *CloudflareProvider) ValidateCredentials(ctx context.Context) error {
	url := fmt.Sprintf("%s/user/tokens/verify", cloudflareAPIBase)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	p.setHeaders(req)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var cfResp cloudflareResponse
	if err := json.NewDecoder(resp.Body).Decode(&cfResp); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if !cfResp.Success {
		return fmt.Errorf("invalid credentials: %v", cfResp.Errors)
	}

	return nil
}

// setHeaders sets the required Cloudflare API headers.
func (p *CloudflareProvider) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+p.apiToken)
	req.Header.Set("Content-Type", "application/json")
}
