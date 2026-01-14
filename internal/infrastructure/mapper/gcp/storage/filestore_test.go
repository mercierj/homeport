package storage

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestNewFilestoreMapper(t *testing.T) {
	m := NewFilestoreMapper()
	if m == nil {
		t.Fatal("NewFilestoreMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeFilestore {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeFilestore)
	}
}

func TestFilestoreMapper_ResourceType(t *testing.T) {
	m := NewFilestoreMapper()
	got := m.ResourceType()
	want := resource.TypeFilestore

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestFilestoreMapper_Dependencies(t *testing.T) {
	m := NewFilestoreMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestFilestoreMapper_Validate(t *testing.T) {
	m := NewFilestoreMapper()

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
				Type: resource.TypeFilestore,
				Name: "test-filestore",
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

func TestFilestoreMapper_Map(t *testing.T) {
	m := NewFilestoreMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Filestore instance",
			res: &resource.AWSResource{
				ID:   "my-project/us-central1-a/my-filestore",
				Type: resource.TypeFilestore,
				Name: "my-filestore",
				Config: map[string]interface{}{
					"name": "my-filestore",
					"tier": "BASIC_HDD",
					"file_shares": []interface{}{
						map[string]interface{}{
							"name":        "share1",
							"capacity_gb": float64(1024),
						},
					},
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
				if result.DockerService.Image != "itsthenetwork/nfs-server-alpine:latest" {
					t.Errorf("Expected NFS server image, got %s", result.DockerService.Image)
				}
			},
		},
		{
			name: "Filestore with BASIC_SSD tier",
			res: &resource.AWSResource{
				ID:   "my-project/us-central1-a/ssd-filestore",
				Type: resource.TypeFilestore,
				Name: "ssd-filestore",
				Config: map[string]interface{}{
					"name": "ssd-filestore",
					"tier": "BASIC_SSD",
					"file_shares": []interface{}{
						map[string]interface{}{
							"name":        "ssd-share",
							"capacity_gb": float64(2560),
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Check resource limits are set for SSD tier
				if result.DockerService.Deploy == nil {
					t.Error("Expected Deploy config for resource limits")
				}
			},
		},
		{
			name: "Filestore with HIGH_SCALE_SSD tier",
			res: &resource.AWSResource{
				ID:   "my-project/us-central1-a/highscale-filestore",
				Type: resource.TypeFilestore,
				Name: "highscale-filestore",
				Config: map[string]interface{}{
					"name": "highscale-filestore",
					"tier": "HIGH_SCALE_SSD",
					"file_shares": []interface{}{
						map[string]interface{}{
							"name":        "scale-share",
							"capacity_gb": float64(10240),
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about high-scale tier
				hasHighScaleWarning := false
				for _, w := range result.Warnings {
					if containsFilestore(w, "HIGH_SCALE_SSD") {
						hasHighScaleWarning = true
						break
					}
				}
				if !hasHighScaleWarning {
					t.Error("Expected warning about HIGH_SCALE_SSD tier")
				}
			},
		},
		{
			name: "Filestore with ENTERPRISE tier",
			res: &resource.AWSResource{
				ID:   "my-project/us-central1-a/enterprise-filestore",
				Type: resource.TypeFilestore,
				Name: "enterprise-filestore",
				Config: map[string]interface{}{
					"name": "enterprise-filestore",
					"tier": "ENTERPRISE",
					"file_shares": []interface{}{
						map[string]interface{}{
							"name":        "enterprise-share",
							"capacity_gb": float64(1024),
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about enterprise tier
				hasEnterpriseWarning := false
				for _, w := range result.Warnings {
					if containsFilestore(w, "ENTERPRISE") {
						hasEnterpriseWarning = true
						break
					}
				}
				if !hasEnterpriseWarning {
					t.Error("Expected warning about ENTERPRISE tier")
				}
			},
		},
		{
			name: "Filestore with KMS encryption",
			res: &resource.AWSResource{
				ID:   "my-project/us-central1-a/encrypted-filestore",
				Type: resource.TypeFilestore,
				Name: "encrypted-filestore",
				Config: map[string]interface{}{
					"name":         "encrypted-filestore",
					"kms_key_name": "projects/my-project/locations/us-central1/keyRings/my-keyring/cryptoKeys/my-key",
					"file_shares": []interface{}{
						map[string]interface{}{
							"name":        "encrypted-share",
							"capacity_gb": float64(1024),
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about KMS encryption
				hasKMSWarning := false
				for _, w := range result.Warnings {
					if containsFilestore(w, "KMS encryption") {
						hasKMSWarning = true
						break
					}
				}
				if !hasKMSWarning {
					t.Error("Expected warning about KMS encryption")
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

func TestFilestoreMapper_extractFileShares(t *testing.T) {
	m := NewFilestoreMapper()

	tests := []struct {
		name   string
		res    *resource.AWSResource
		expect int
	}{
		{
			name: "with file shares",
			res: &resource.AWSResource{
				Config: map[string]interface{}{
					"file_shares": []interface{}{
						map[string]interface{}{
							"name":        "share1",
							"capacity_gb": float64(1024),
						},
						map[string]interface{}{
							"name":        "share2",
							"capacity_gb": float64(2048),
						},
					},
				},
			},
			expect: 2,
		},
		{
			name: "without file shares",
			res: &resource.AWSResource{
				Config: map[string]interface{}{},
			},
			expect: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.extractFileShares(tt.res)
			if len(got) != tt.expect {
				t.Errorf("extractFileShares() returned %d shares, want %d", len(got), tt.expect)
			}
		})
	}
}

func TestFilestoreMapper_sanitizeName(t *testing.T) {
	m := NewFilestoreMapper()

	tests := []struct {
		input string
		want  string
	}{
		{"my-filestore", "my-filestore"},
		{"MY_FILESTORE", "my-filestore"},
		{"my filestore", "my-filestore"},
		{"123filestore", "filestore"},
		{"", "filestore"},
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

// containsFilestore is a helper to check if a string contains a substring
func containsFilestore(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
