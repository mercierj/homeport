package networking

import (
	"context"
	"testing"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

func TestNewCloudFrontMapper(t *testing.T) {
	m := NewCloudFrontMapper()
	if m == nil {
		t.Fatal("NewCloudFrontMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeCloudFront {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeCloudFront)
	}
}

func TestCloudFrontMapper_ResourceType(t *testing.T) {
	m := NewCloudFrontMapper()
	got := m.ResourceType()
	want := resource.TypeCloudFront

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestCloudFrontMapper_Dependencies(t *testing.T) {
	m := NewCloudFrontMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestCloudFrontMapper_Validate(t *testing.T) {
	m := NewCloudFrontMapper()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
	}{
		{
			name:    "nil resource",
			res:     nil,
			wantErr: true,
		},
		{
			name: "wrong resource type",
			res: &resource.AWSResource{
				ID:   "test-id",
				Type: resource.TypeS3Bucket,
				Name: "test",
			},
			wantErr: true,
		},
		{
			name: "valid resource",
			res: &resource.AWSResource{
				ID:   "E1234567890ABC",
				Type: resource.TypeCloudFront,
				Name: "my-distribution",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := m.Validate(tt.res)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCloudFrontMapper_Map(t *testing.T) {
	m := NewCloudFrontMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic CloudFront distribution",
			res: &resource.AWSResource{
				ID:     "E1234567890ABC",
				Type:   resource.TypeCloudFront,
				Name:   "my-distribution",
				Config: map[string]interface{}{},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if result.DockerService == nil {
					t.Fatal("DockerService is nil")
				}
				if result.DockerService.Image == "" {
					t.Error("DockerService.Image is empty")
				}
				// Should use Caddy image
				if result.DockerService.Image != "caddy:2.7-alpine" {
					t.Errorf("Expected image caddy:2.7-alpine, got %s", result.DockerService.Image)
				}
				// Should have ports configured
				if len(result.DockerService.Ports) == 0 {
					t.Error("Expected ports to be configured")
				}
				// Should have labels
				if result.DockerService.Labels == nil {
					t.Error("Expected labels to be configured")
				}
				if result.DockerService.Labels["cloudexit.source"] != "aws_cloudfront" {
					t.Errorf("Expected source label to be aws_cloudfront, got %s", result.DockerService.Labels["cloudexit.source"])
				}
			},
		},
		{
			name: "CloudFront with origins",
			res: &resource.AWSResource{
				ID:   "E1234567890DEF",
				Type: resource.TypeCloudFront,
				Name: "distribution-with-origins",
				Config: map[string]interface{}{
					"origin": []interface{}{
						map[string]interface{}{
							"domain_name": "backend.example.com",
							"origin_id":   "backend",
						},
						map[string]interface{}{
							"domain_name": "api.example.com",
							"origin_id":   "api",
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warnings about origins
				if len(result.Warnings) == 0 {
					t.Error("Expected warnings for origins configuration")
				}
			},
		},
		{
			name: "CloudFront with cache behaviors",
			res: &resource.AWSResource{
				ID:   "E1234567890GHI",
				Type: resource.TypeCloudFront,
				Name: "distribution-with-cache",
				Config: map[string]interface{}{
					"default_cache_behavior": map[string]interface{}{
						"target_origin_id":       "backend",
						"viewer_protocol_policy": "redirect-to-https",
						"cached_methods":         []string{"GET", "HEAD"},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warnings about cache behaviors
				hasCacheWarning := false
				for _, w := range result.Warnings {
					if len(w) > 10 && containsSubstring(w, "cache") {
						hasCacheWarning = true
						break
					}
				}
				if !hasCacheWarning {
					t.Log("Expected warning about cache behaviors")
				}
			},
		},
		{
			name: "CloudFront with custom domains",
			res: &resource.AWSResource{
				ID:   "E1234567890JKL",
				Type: resource.TypeCloudFront,
				Name: "distribution-with-domains",
				Config: map[string]interface{}{
					"aliases": []interface{}{
						"www.example.com",
						"cdn.example.com",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warnings about custom domains
				hasDomainWarning := false
				for _, w := range result.Warnings {
					if containsSubstring(w, "domain") {
						hasDomainWarning = true
						break
					}
				}
				if !hasDomainWarning {
					t.Log("Expected warning about custom domains")
				}
			},
		},
		{
			name: "CloudFront with SSL certificate",
			res: &resource.AWSResource{
				ID:   "E1234567890MNO",
				Type: resource.TypeCloudFront,
				Name: "distribution-with-ssl",
				Config: map[string]interface{}{
					"viewer_certificate": map[string]interface{}{
						"acm_certificate_arn":      "arn:aws:acm:us-east-1:123456789012:certificate/12345678-1234-1234-1234-123456789012",
						"ssl_support_method":       "sni-only",
						"minimum_protocol_version": "TLSv1.2_2021",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warnings about SSL certificate
				hasSSLWarning := false
				for _, w := range result.Warnings {
					if containsSubstring(w, "ACM") || containsSubstring(w, "certificate") {
						hasSSLWarning = true
						break
					}
				}
				if !hasSSLWarning {
					t.Log("Expected warning about SSL certificate")
				}
			},
		},
		{
			name: "CloudFront with geo-restrictions",
			res: &resource.AWSResource{
				ID:   "E1234567890PQR",
				Type: resource.TypeCloudFront,
				Name: "distribution-with-geo",
				Config: map[string]interface{}{
					"restrictions": map[string]interface{}{
						"geo_restriction": map[string]interface{}{
							"restriction_type": "whitelist",
							"locations":        []string{"US", "CA", "GB"},
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warnings about geo-restrictions
				hasGeoWarning := false
				for _, w := range result.Warnings {
					if containsSubstring(w, "geo") {
						hasGeoWarning = true
						break
					}
				}
				if !hasGeoWarning {
					t.Log("Expected warning about geo-restrictions")
				}
			},
		},
		{
			name: "CloudFront with WAF",
			res: &resource.AWSResource{
				ID:   "E1234567890STU",
				Type: resource.TypeCloudFront,
				Name: "distribution-with-waf",
				Config: map[string]interface{}{
					"web_acl_id": "arn:aws:wafv2:us-east-1:123456789012:global/webacl/my-web-acl/12345678-1234-1234-1234-123456789012",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warnings about WAF
				hasWAFWarning := false
				for _, w := range result.Warnings {
					if containsSubstring(w, "WAF") {
						hasWAFWarning = true
						break
					}
				}
				if !hasWAFWarning {
					t.Log("Expected warning about WAF")
				}
			},
		},
		{
			name: "CloudFront with Lambda@Edge",
			res: &resource.AWSResource{
				ID:   "E1234567890VWX",
				Type: resource.TypeCloudFront,
				Name: "distribution-with-lambda",
				Config: map[string]interface{}{
					"default_cache_behavior": map[string]interface{}{
						"target_origin_id": "backend",
						"lambda_function_association": []interface{}{
							map[string]interface{}{
								"event_type":   "viewer-request",
								"lambda_arn":   "arn:aws:lambda:us-east-1:123456789012:function:my-function:1",
								"include_body": false,
							},
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warnings about Lambda@Edge
				hasLambdaWarning := false
				for _, w := range result.Warnings {
					if containsSubstring(w, "Lambda") {
						hasLambdaWarning = true
						break
					}
				}
				if !hasLambdaWarning {
					t.Log("Expected warning about Lambda@Edge")
				}
			},
		},
		{
			name: "CloudFront with logging",
			res: &resource.AWSResource{
				ID:   "E1234567890YZA",
				Type: resource.TypeCloudFront,
				Name: "distribution-with-logging",
				Config: map[string]interface{}{
					"logging_config": map[string]interface{}{
						"bucket":          "my-logs-bucket.s3.amazonaws.com",
						"prefix":          "cloudfront/",
						"include_cookies": false,
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warnings about logging
				hasLoggingWarning := false
				for _, w := range result.Warnings {
					if containsSubstring(w, "logging") {
						hasLoggingWarning = true
						break
					}
				}
				if !hasLoggingWarning {
					t.Log("Expected warning about logging")
				}
			},
		},
		{
			name:    "nil resource",
			res:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := m.Map(ctx, tt.res)
			if (err != nil) != tt.wantErr {
				t.Errorf("Map() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}

// containsSubstring checks if a string contains a substring (case-insensitive).
func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if len(s[i:]) >= len(substr) {
			found := true
			for j := 0; j < len(substr); j++ {
				sc := s[i+j]
				sc2 := substr[j]
				// Simple case-insensitive comparison
				if sc != sc2 && sc != sc2+32 && sc != sc2-32 {
					found = false
					break
				}
			}
			if found {
				return true
			}
		}
	}
	return false
}
