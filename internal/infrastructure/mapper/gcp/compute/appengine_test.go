package compute

import (
	"context"
	"testing"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

func TestNewAppEngineMapper(t *testing.T) {
	m := NewAppEngineMapper()
	if m == nil {
		t.Fatal("NewAppEngineMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeAppEngine {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeAppEngine)
	}
}

func TestAppEngineMapper_ResourceType(t *testing.T) {
	m := NewAppEngineMapper()
	got := m.ResourceType()
	want := resource.TypeAppEngine

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestAppEngineMapper_Dependencies(t *testing.T) {
	m := NewAppEngineMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestAppEngineMapper_Validate(t *testing.T) {
	m := NewAppEngineMapper()

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
				Type: resource.TypeAppEngine,
				Name: "test-app",
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

func TestAppEngineMapper_Map(t *testing.T) {
	m := NewAppEngineMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic App Engine application",
			res: &resource.AWSResource{
				ID:   "my-project",
				Type: resource.TypeAppEngine,
				Name: "my-app",
				Config: map[string]interface{}{
					"project":     "my-project",
					"location_id": "us-central",
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
				if result.DockerService.Image == "" {
					t.Error("DockerService.Image is empty")
				}
				if result.DockerService.Environment["GAE_APPLICATION"] != "my-project" {
					t.Errorf("Expected GAE_APPLICATION=my-project, got %s", result.DockerService.Environment["GAE_APPLICATION"])
				}
			},
		},
		{
			name: "App Engine with IAP enabled",
			res: &resource.AWSResource{
				ID:   "secure-app",
				Type: resource.TypeAppEngine,
				Name: "secure-app",
				Config: map[string]interface{}{
					"project":     "secure-app",
					"location_id": "us-east1",
					"iap": map[string]interface{}{
						"enabled":          true,
						"oauth2_client_id": "123456789.apps.googleusercontent.com",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about IAP
				hasIAPWarning := false
				for _, w := range result.Warnings {
					if w == "IAP (Identity-Aware Proxy) enabled - Configure alternative authentication" {
						hasIAPWarning = true
						break
					}
				}
				if !hasIAPWarning {
					t.Error("Expected warning about IAP")
				}
			},
		},
		{
			name: "App Engine with feature settings",
			res: &resource.AWSResource{
				ID:   "feature-app",
				Type: resource.TypeAppEngine,
				Name: "feature-app",
				Config: map[string]interface{}{
					"project": "feature-app",
					"feature_settings": map[string]interface{}{
						"split_health_checks": true,
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about split health checks
				hasSplitHealthWarning := false
				for _, w := range result.Warnings {
					if w == "Split health checks enabled - Configure separate liveness and readiness probes" {
						hasSplitHealthWarning = true
						break
					}
				}
				if !hasSplitHealthWarning {
					t.Error("Expected warning about split health checks")
				}
			},
		},
		{
			name: "App Engine with Cloud Firestore",
			res: &resource.AWSResource{
				ID:   "firestore-app",
				Type: resource.TypeAppEngine,
				Name: "firestore-app",
				Config: map[string]interface{}{
					"project":       "firestore-app",
					"database_type": "CLOUD_FIRESTORE",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about Firestore
				hasFirestoreWarning := false
				for _, w := range result.Warnings {
					if w == "Using Cloud Firestore - Migrate to self-hosted document database" {
						hasFirestoreWarning = true
						break
					}
				}
				if !hasFirestoreWarning {
					t.Error("Expected warning about Cloud Firestore")
				}
			},
		},
		{
			name: "App Engine with Cloud Datastore",
			res: &resource.AWSResource{
				ID:   "datastore-app",
				Type: resource.TypeAppEngine,
				Name: "datastore-app",
				Config: map[string]interface{}{
					"project":       "datastore-app",
					"database_type": "CLOUD_DATASTORE",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about Datastore
				hasDatastoreWarning := false
				for _, w := range result.Warnings {
					if w == "Using Cloud Datastore - Migrate to self-hosted key-value store" {
						hasDatastoreWarning = true
						break
					}
				}
				if !hasDatastoreWarning {
					t.Error("Expected warning about Cloud Datastore")
				}
			},
		},
		{
			name: "App Engine with auth domain",
			res: &resource.AWSResource{
				ID:   "auth-app",
				Type: resource.TypeAppEngine,
				Name: "auth-app",
				Config: map[string]interface{}{
					"project":     "auth-app",
					"auth_domain": "gmail.com",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about auth domain
				hasAuthWarning := false
				for _, w := range result.Warnings {
					if w == "Auth domain: gmail.com - Configure authentication provider" {
						hasAuthWarning = true
						break
					}
				}
				if !hasAuthWarning {
					t.Error("Expected warning about auth domain")
				}
			},
		},
		{
			name: "App Engine with code bucket",
			res: &resource.AWSResource{
				ID:   "bucket-app",
				Type: resource.TypeAppEngine,
				Name: "bucket-app",
				Config: map[string]interface{}{
					"project":     "bucket-app",
					"code_bucket": "staging.bucket-app.appspot.com",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about code bucket
				hasBucketWarning := false
				for _, w := range result.Warnings {
					if w == "Code stored in bucket: staging.bucket-app.appspot.com - Migrate code to local storage" {
						hasBucketWarning = true
						break
					}
				}
				if !hasBucketWarning {
					t.Error("Expected warning about code bucket")
				}
			},
		},
		{
			name: "App Engine with default hostname",
			res: &resource.AWSResource{
				ID:   "hostname-app",
				Type: resource.TypeAppEngine,
				Name: "hostname-app",
				Config: map[string]interface{}{
					"project":          "hostname-app",
					"default_hostname": "hostname-app.appspot.com",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about hostname
				hasHostnameWarning := false
				for _, w := range result.Warnings {
					if w == "Default hostname: hostname-app.appspot.com - Update DNS to point to your self-hosted service" {
						hasHostnameWarning = true
						break
					}
				}
				if !hasHostnameWarning {
					t.Error("Expected warning about default hostname")
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

func TestAppEngineMapper_sanitizeName(t *testing.T) {
	m := NewAppEngineMapper()

	tests := []struct {
		input string
		want  string
	}{
		{"my-app", "my-app"},
		{"MY_APP", "my-app"},
		{"my app", "my-app"},
		{"my.app.com", "my-app-com"},
		{"123app", "app"},
		{"", "appengine"},
		{"---", "appengine"},
		{"web-service", "web-service"},
		{"Backend_API", "backend-api"},
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
