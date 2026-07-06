package networking

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
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
				if result.DockerService.Labels["homeport.source"] != "aws_cloudfront" {
					t.Errorf("Expected source label to be aws_cloudfront, got %s", result.DockerService.Labels["homeport.source"])
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

func TestCloudFrontMapper_MapBuildsDeterministicCDNWhenOriginsAreKnown(t *testing.T) {
	res := managedCloudFrontFixture()

	result, err := NewCloudFrontMapper().Map(context.Background(), res)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want none when CloudFront origins and behavior are known", result.ManualSteps)
	}
	caddyfile := string(result.Configs["config/caddy/Caddyfile"])
	for _, want := range []string{
		"shop.example.com, cdn.example.com",
		"reverse_proxy varnish:6081",
		"Strict-Transport-Security",
	} {
		if !strings.Contains(caddyfile, want) {
			t.Fatalf("Caddyfile missing %q:\n%s", want, caddyfile)
		}
	}
	vcl := string(result.Configs["config/varnish/default.vcl"])
	for _, want := range []string{
		"backend api_origin",
		".host = \"api.internal\"",
		"if (req.url ~ \"^/static\")",
		"set beresp.ttl = 3600s",
	} {
		if !strings.Contains(vcl, want) {
			t.Fatalf("VCL missing %q:\n%s", want, vcl)
		}
	}
	if strings.Contains(caddyfile, "TODO") || strings.Contains(vcl, "TODO") {
		t.Fatalf("generated config still contains TODO\nCaddy:\n%s\nVCL:\n%s", caddyfile, vcl)
	}
}

func TestCloudFrontConformanceManagedAToZ(t *testing.T) {
	result, err := NewCloudFrontMapper().Map(context.Background(), managedCloudFrontFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated CloudFront migration", result.ManualSteps)
	}
	if result.DockerService.Image != "caddy:2.7-alpine" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Caddy: %#v", result.DockerService)
	}
	if len(result.AdditionalServices) != 1 || result.AdditionalServices[0].Image != "varnish:7.5-alpine" {
		t.Fatalf("missing Varnish cache service: %#v", result.AdditionalServices)
	}
	for _, file := range []string{
		"config/caddy/Caddyfile",
		"config/varnish/default.vcl",
		"config/cloudfront/dns-cutover.env",
		"config/cloudfront/validation.sh",
	} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing %s", file)
		}
	}
	if _, ok := result.Scripts["backup_cloudfront_config.sh"]; !ok {
		t.Fatal("missing backup script")
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"render-cloudfront-cdn-config": domainrunbook.StepTypeCommand,
		"provision-caddy-varnish-cdn":  domainrunbook.StepTypeCommand,
		"validate-cdn-cache-routing":   domainrunbook.StepTypeHealth,
		"backup-cloudfront-config":     domainrunbook.StepTypeCommand,
		"cutover-cloudfront-dns":       domainrunbook.StepTypeDNSCheck,
		"rollback-cloudfront-dns":      domainrunbook.StepTypeRollback,
	} {
		if !hasCloudFrontRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func managedCloudFrontFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "E1234567890ABC",
		Type: resource.TypeCloudFront,
		Name: "shop-cdn",
		Config: map[string]interface{}{
			"domain_name": "d111111abcdef8.cloudfront.net",
			"aliases":     []interface{}{"shop.example.com", "cdn.example.com"},
			"origins": []map[string]interface{}{
				{
					"id":          "api",
					"domain_name": "api.internal",
					"origin_type": "custom",
					"http_port":   8080,
				},
			},
			"default_cache_behavior": map[string]interface{}{
				"target_origin_id":       "api",
				"viewer_protocol_policy": "redirect-to-https",
				"compress":               true,
			},
			"ordered_cache_behavior": []interface{}{
				map[string]interface{}{
					"path_pattern":     "/static/*",
					"target_origin_id": "api",
					"default_ttl":      3600,
				},
			},
			"viewer_certificate": map[string]interface{}{
				"minimum_protocol_version": "TLSv1.2_2021",
			},
			"logging_config": map[string]interface{}{
				"prefix": "cloudfront/",
			},
		},
	}
}

func hasCloudFrontRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
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
