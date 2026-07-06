package networking

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestCloudCDNConformanceManagedAToZ(t *testing.T) {
	result, err := NewCloudCDNMapper().Map(context.Background(), managedCloudCDNFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Cloud CDN migration", result.ManualSteps)
	}
	if result.DockerService.Image != "varnish:7.4" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Varnish target: %#v", result.DockerService)
	}
	for _, file := range []string{"varnish/default.vcl", "nginx-alternative/nginx.conf", "config/cloud-cdn/app-change.env", "config/cloud-cdn/cache-policy.yaml"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/cloud-cdn/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_CLOUD_CDN_BACKEND=edge-assets", "TARGET_CDN_ENDPOINT=http://edge-assets:80"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	policy := string(result.Configs["config/cloud-cdn/cache-policy.yaml"])
	for _, want := range []string{"google_compute_backend_bucket", "ttl: 7200", "cache_mode: CACHE_ALL_STATIC"} {
		if !strings.Contains(policy, want) {
			t.Fatalf("cache policy missing %q:\n%s", want, policy)
		}
	}
	for _, file := range []string{"varnish-manage.sh", "backup_cloud_cdn.sh", "validate_cloud_cdn.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"discover-cloud-cdn-backend": domainrunbook.StepTypeCommand,
		"provision-varnish-cdn":      domainrunbook.StepTypeCommand,
		"migrate-cloud-cdn-policy":   domainrunbook.StepTypeCommand,
		"validate-varnish-cdn":       domainrunbook.StepTypeCommand,
		"backup-cloud-cdn-config":    domainrunbook.StepTypeCommand,
		"cutover-cloud-cdn-endpoint": domainrunbook.StepTypeAPICall,
		"rollback-cloud-cdn-backend": domainrunbook.StepTypeRollback,
	} {
		if !hasCloudCDNRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewCloudCDNMapper(t *testing.T) {
	m := NewCloudCDNMapper()
	if m == nil {
		t.Fatal("NewCloudCDNMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeCloudCDN {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeCloudCDN)
	}
}

func TestCloudCDNMapper_ResourceType(t *testing.T) {
	m := NewCloudCDNMapper()
	got := m.ResourceType()
	want := resource.TypeCloudCDN

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestCloudCDNMapper_Dependencies(t *testing.T) {
	m := NewCloudCDNMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestCloudCDNMapper_Validate(t *testing.T) {
	m := NewCloudCDNMapper()

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
				Type: resource.TypeGCSBucket,
				Name: "test",
			},
			wantErr: true,
		},
		{
			name: "valid resource",
			res: &resource.AWSResource{
				ID:   "test-id",
				Type: resource.TypeCloudCDN,
				Name: "test-cdn",
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

func TestCloudCDNMapper_Map(t *testing.T) {
	m := NewCloudCDNMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Cloud CDN backend bucket",
			res: &resource.AWSResource{
				ID:   "my-project/my-cdn",
				Type: resource.TypeCloudCDN,
				Name: "my-cdn",
				Config: map[string]interface{}{
					"name":        "my-cdn",
					"bucket_name": "my-static-bucket",
					"enable_cdn":  true,
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if result.DockerService == nil {
					t.Fatal("DockerService is nil")
				}
				if result.DockerService.Image != "varnish:7.4" {
					t.Errorf("Expected Varnish image, got %s", result.DockerService.Image)
				}
			},
		},
		{
			name: "Cloud CDN with CDN policy",
			res: &resource.AWSResource{
				ID:   "my-project/policy-cdn",
				Type: resource.TypeCloudCDN,
				Name: "policy-cdn",
				Config: map[string]interface{}{
					"name":        "policy-cdn",
					"bucket_name": "policy-bucket",
					"cdn_policy": map[string]interface{}{
						"default_ttl":      float64(7200),
						"cache_mode":       "CACHE_ALL_STATIC",
						"negative_caching": true,
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
			},
		},
		{
			name: "Cloud CDN with signed URLs",
			res: &resource.AWSResource{
				ID:   "my-project/signed-cdn",
				Type: resource.TypeCloudCDN,
				Name: "signed-cdn",
				Config: map[string]interface{}{
					"name":                         "signed-cdn",
					"bucket_name":                  "signed-bucket",
					"signed_url_cache_max_age_sec": float64(3600),
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about signed URLs
				hasSignedURLWarning := false
				for _, w := range result.Warnings {
					if containsCDN(w, "Signed URL") {
						hasSignedURLWarning = true
						break
					}
				}
				if !hasSignedURLWarning {
					t.Error("Expected warning about signed URLs")
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

func TestCloudCDNMapper_extractCacheTTL(t *testing.T) {
	m := NewCloudCDNMapper()

	tests := []struct {
		name   string
		policy map[string]interface{}
		expect int
	}{
		{
			name: "with default_ttl",
			policy: map[string]interface{}{
				"default_ttl": float64(7200),
			},
			expect: 7200,
		},
		{
			name: "with client_ttl",
			policy: map[string]interface{}{
				"client_ttl": float64(1800),
			},
			expect: 1800,
		},
		{
			name:   "empty policy",
			policy: map[string]interface{}{},
			expect: 3600,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.extractCacheTTL(tt.policy)
			if got != tt.expect {
				t.Errorf("extractCacheTTL() = %v, want %v", got, tt.expect)
			}
		})
	}
}

func TestCloudCDNMapper_sanitizeName(t *testing.T) {
	m := NewCloudCDNMapper()

	tests := []struct {
		input string
		want  string
	}{
		{"my-cdn", "my-cdn"},
		{"MY_CDN", "my-cdn"},
		{"my cdn", "my-cdn"},
		{"123cdn", "cdn"},
		{"", "cdn"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := m.sanitizeName(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// containsCDN is a helper to check if a string contains a substring
func containsCDN(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func managedCloudCDNFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "projects/demo/global/backendBuckets/edge-assets",
		Type: resource.TypeCloudCDN,
		Name: "edge-assets",
		Config: map[string]interface{}{
			"name":        "edge-assets",
			"bucket_name": "assets-prod",
			"enable_cdn":  true,
			"cdn_policy": map[string]interface{}{
				"default_ttl":      float64(7200),
				"cache_mode":       "CACHE_ALL_STATIC",
				"negative_caching": true,
			},
		},
	}
}

func hasCloudCDNRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
