package cutover

import (
	"fmt"
	"strings"
)

type PreviewInput struct {
	BundleID      string            `json:"bundle_id"`
	Domain        string            `json:"domain"`
	TargetIP      string            `json:"target_ip"`
	ServicePaths  map[string]string `json:"service_paths,omitempty"`
	HealthBaseURL string            `json:"health_base_url,omitempty"`
}

type Preview struct {
	PreChecks  []PreviewHealthCheck `json:"pre_checks"`
	DNSChanges []PreviewDNSChange   `json:"dns_changes"`
	PostChecks []PreviewHealthCheck `json:"post_checks"`
	Warnings   []string             `json:"warnings"`
}

type PreviewHealthCheck struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Endpoint string `json:"endpoint"`
}

type PreviewDNSChange struct {
	ID         string `json:"id"`
	Domain     string `json:"domain"`
	RecordType string `json:"record_type"`
	OldValue   string `json:"old_value"`
	NewValue   string `json:"new_value"`
	TTL        int    `json:"ttl"`
}

func BuildPreview(input PreviewInput) Preview {
	preview := Preview{}
	domain := strings.TrimSpace(input.Domain)
	targetIP := strings.TrimSpace(input.TargetIP)
	if domain == "" {
		preview.Warnings = append(preview.Warnings, "domain is required to suggest DNS changes")
	}
	if targetIP == "" {
		preview.Warnings = append(preview.Warnings, "target IP is required to suggest DNS changes")
	}
	if domain != "" && targetIP != "" {
		preview.DNSChanges = append(preview.DNSChanges, PreviewDNSChange{
			ID: "dns-root", Domain: domain, RecordType: "A", OldValue: "", NewValue: targetIP, TTL: 300,
		})
	}
	baseURL := strings.TrimRight(input.HealthBaseURL, "/")
	if baseURL == "" && domain != "" {
		baseURL = "https://" + domain
	}
	if baseURL != "" {
		preview.PreChecks = append(preview.PreChecks, PreviewHealthCheck{ID: "pre-health", Name: "Current service health", Type: "http", Endpoint: baseURL + "/health"})
		preview.PostChecks = append(preview.PostChecks, PreviewHealthCheck{ID: "post-health", Name: "Migrated service health", Type: "http", Endpoint: baseURL + "/health"})
		for name, path := range input.ServicePaths {
			preview.PostChecks = append(preview.PostChecks, PreviewHealthCheck{
				ID:       fmt.Sprintf("post-%s", strings.ToLower(strings.ReplaceAll(name, " ", "-"))),
				Name:     name + " health",
				Type:     "http",
				Endpoint: baseURL + "/" + strings.TrimLeft(path, "/"),
			})
		}
	}
	return preview
}
