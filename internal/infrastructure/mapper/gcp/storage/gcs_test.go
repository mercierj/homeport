package storage

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestNewGCSMapper(t *testing.T) {
	m := NewGCSMapper()
	if m == nil {
		t.Fatal("NewGCSMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeGCSBucket {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeGCSBucket)
	}
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
				// Should have manual step about versioning
				hasVersioningStep := false
				for _, step := range result.ManualSteps {
					if containsGCS(step, "versioning") {
						hasVersioningStep = true
						break
					}
				}
				if !hasVersioningStep {
					t.Error("Expected manual step about versioning")
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
