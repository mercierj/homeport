package storage

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestGCSConformanceManagedAToZ(t *testing.T) {
	result, err := NewGCSMapper().Map(context.Background(), managedGCSFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Cloud Storage migration", result.ManualSteps)
	}
	if result.DockerService.Image != "minio/minio:latest" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA MinIO target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/minio/app-change.env", "config/minio/assets-cors.json", "config/minio/encryption.env"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/minio/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_GCS_BUCKET=assets", "TARGET_BUCKET=assets", "HOMEPORT_STORAGE_ENDPOINT=http://minio:9000"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"setup_minio.sh", "configure_versioning.sh", "configure_cors.sh", "configure_encryption.sh", "configure_website.sh", "configure_retention.sh", "configure_audit_logging.sh", "validate_gcs_api.sh", "backup_gcs_config.sh", "cutover_gcs_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"provision-minio-bucket":         domainrunbook.StepTypeCommand,
		"estimate-object-source":         domainrunbook.StepTypeCommand,
		"sync-objects-to-minio":          domainrunbook.StepTypeCommand,
		"verify-object-migration":        domainrunbook.StepTypeCommand,
		"validate-object-api":            domainrunbook.StepTypeCommand,
		"rollback-keep-source-authority": domainrunbook.StepTypeRollback,
	} {
		if !hasGCSRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewGCSMapper(t *testing.T) {
	m := NewGCSMapper()
	if m == nil {
		t.Fatal("NewGCSMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeGCSBucket {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeGCSBucket)
	}
}

func managedGCSFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "google_storage_bucket.assets",
		Type: resource.TypeGCSBucket,
		Name: "assets",
		Config: map[string]interface{}{
			"name":                        "assets",
			"location":                    "EU",
			"storage_class":               "STANDARD",
			"public_access_prevention":    "enforced",
			"uniform_bucket_level_access": map[string]interface{}{"enabled": true},
			"versioning":                  map[string]interface{}{"enabled": true},
			"encryption":                  map[string]interface{}{"default_kms_key_name": "projects/demo/locations/eu/keyRings/main/cryptoKeys/storage"},
			"website":                     map[string]interface{}{"main_page_suffix": "index.html"},
			"retention_policy":            map[string]interface{}{"retention_period": float64(86400)},
			"logging":                     map[string]interface{}{"log_bucket": "assets-logs"},
			"cors":                        []interface{}{map[string]interface{}{"origin": []interface{}{"*"}, "method": []interface{}{"GET"}, "response_header": []interface{}{"Content-Type"}, "max_age_seconds": float64(3600)}},
			"lifecycle_rule":              []interface{}{map[string]interface{}{"action": map[string]interface{}{"type": "Delete"}, "condition": map[string]interface{}{"age": float64(30)}}},
		},
	}
}

func hasGCSRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}

func TestGCSMapper_ResourceType(t *testing.T) {
	m := NewGCSMapper()
	got := m.ResourceType()
	want := resource.TypeGCSBucket

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestGCSMapper_Dependencies(t *testing.T) {
	m := NewGCSMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestGCSMapper_Validate(t *testing.T) {
	m := NewGCSMapper()

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
				Type: resource.TypeCloudSQL,
				Name: "test",
			},
			wantErr: true,
		},
		{
			name: "valid resource",
			res: &resource.AWSResource{
				ID:   "test-id",
				Type: resource.TypeGCSBucket,
				Name: "test-bucket",
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

func TestGCSMapper_Map(t *testing.T) {
	m := NewGCSMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic GCS bucket",
			res: &resource.AWSResource{
				ID:   "my-bucket",
				Type: resource.TypeGCSBucket,
				Name: "my-bucket",
				Config: map[string]interface{}{
					"name":     "my-bucket",
					"location": "US",
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
				if result.DockerService.Image != "minio/minio:latest" {
					t.Errorf("Expected MinIO image, got %s", result.DockerService.Image)
				}
			},
		},
		{
			name: "GCS bucket with versioning",
			res: &resource.AWSResource{
				ID:   "versioned-bucket",
				Type: resource.TypeGCSBucket,
				Name: "versioned-bucket",
				Config: map[string]interface{}{
					"name": "versioned-bucket",
					"versioning": map[string]interface{}{
						"enabled": true,
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if _, ok := result.Scripts["configure_versioning.sh"]; !ok {
					t.Error("Expected generated versioning script")
				}
			},
		},
		{
			name: "GCS bucket with storage class",
			res: &resource.AWSResource{
				ID:   "standard-bucket",
				Type: resource.TypeGCSBucket,
				Name: "standard-bucket",
				Config: map[string]interface{}{
					"name":          "standard-bucket",
					"storage_class": "STANDARD",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about storage class
				if len(result.Warnings) == 0 {
					t.Error("Expected warnings about storage class")
				}
			},
		},
		{
			name: "GCS bucket with lifecycle rules",
			res: &resource.AWSResource{
				ID:   "lifecycle-bucket",
				Type: resource.TypeGCSBucket,
				Name: "lifecycle-bucket",
				Config: map[string]interface{}{
					"name": "lifecycle-bucket",
					"lifecycle_rule": []interface{}{
						map[string]interface{}{
							"action": map[string]interface{}{
								"type": "Delete",
							},
							"condition": map[string]interface{}{
								"age": float64(30),
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
			},
		},
		{
			name: "GCS bucket with CORS",
			res: &resource.AWSResource{
				ID:   "cors-bucket",
				Type: resource.TypeGCSBucket,
				Name: "cors-bucket",
				Config: map[string]interface{}{
					"name": "cors-bucket",
					"cors": []interface{}{
						map[string]interface{}{
							"origin":          []interface{}{"*"},
							"method":          []interface{}{"GET", "POST"},
							"response_header": []interface{}{"Content-Type"},
							"max_age_seconds": float64(3600),
						},
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

func TestGCSMapper_hasVersioningBlock(t *testing.T) {
	m := NewGCSMapper()

	tests := []struct {
		name   string
		res    *resource.AWSResource
		expect bool
	}{
		{
			name: "with versioning enabled",
			res: &resource.AWSResource{
				Config: map[string]interface{}{
					"versioning": map[string]interface{}{
						"enabled": true,
					},
				},
			},
			expect: true,
		},
		{
			name: "with versioning disabled",
			res: &resource.AWSResource{
				Config: map[string]interface{}{
					"versioning": map[string]interface{}{
						"enabled": false,
					},
				},
			},
			expect: false,
		},
		{
			name: "without versioning block",
			res: &resource.AWSResource{
				Config: map[string]interface{}{},
			},
			expect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.hasVersioningBlock(tt.res)
			if got != tt.expect {
				t.Errorf("hasVersioningBlock() = %v, want %v", got, tt.expect)
			}
		})
	}
}

func TestGCSMapper_getLifecycleRules(t *testing.T) {
	m := NewGCSMapper()

	tests := []struct {
		name   string
		res    *resource.AWSResource
		expect int
	}{
		{
			name: "with lifecycle rules",
			res: &resource.AWSResource{
				Config: map[string]interface{}{
					"lifecycle_rule": []interface{}{
						map[string]interface{}{
							"action": map[string]interface{}{"type": "Delete"},
						},
						map[string]interface{}{
							"action": map[string]interface{}{"type": "SetStorageClass"},
						},
					},
				},
			},
			expect: 2,
		},
		{
			name: "without lifecycle rules",
			res: &resource.AWSResource{
				Config: map[string]interface{}{},
			},
			expect: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.getLifecycleRules(tt.res)
			if len(got) != tt.expect {
				t.Errorf("getLifecycleRules() returned %d rules, want %d", len(got), tt.expect)
			}
		})
	}
}

// containsGCS is a helper to check if a string contains a substring
func containsGCS(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
