package storage

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestNewPersistentDiskMapper(t *testing.T) {
	m := NewPersistentDiskMapper()
	if m == nil {
		t.Fatal("NewPersistentDiskMapper() returned nil")
	}
	if m.ResourceType() != resource.TypePersistentDisk {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypePersistentDisk)
	}
}

func TestPersistentDiskMapper_ResourceType(t *testing.T) {
	m := NewPersistentDiskMapper()
	got := m.ResourceType()
	want := resource.TypePersistentDisk

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestPersistentDiskMapper_Dependencies(t *testing.T) {
	m := NewPersistentDiskMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestPersistentDiskMapper_Validate(t *testing.T) {
	m := NewPersistentDiskMapper()

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
				Type: resource.TypePersistentDisk,
				Name: "test-disk",
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

func TestPersistentDiskMapper_Map(t *testing.T) {
	m := NewPersistentDiskMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic persistent disk",
			res: &resource.AWSResource{
				ID:   "my-project/us-central1-a/my-disk",
				Type: resource.TypePersistentDisk,
				Name: "my-disk",
				Config: map[string]interface{}{
					"name": "my-disk",
					"size": float64(100),
					"type": "pd-standard",
					"zone": "us-central1-a",
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
				// Check labels
				if result.DockerService.Labels["homeport.source"] != "google_compute_disk" {
					t.Error("Expected homeport.source label")
				}
			},
		},
		{
			name: "SSD persistent disk",
			res: &resource.AWSResource{
				ID:   "my-project/us-central1-a/ssd-disk",
				Type: resource.TypePersistentDisk,
				Name: "ssd-disk",
				Config: map[string]interface{}{
					"name": "ssd-disk",
					"size": float64(500),
					"type": "pd-ssd",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if result.DockerService.Labels["homeport.disk_type"] != "pd-ssd" {
					t.Error("Expected disk type label to be pd-ssd")
				}
			},
		},
		{
			name: "persistent disk with snapshot",
			res: &resource.AWSResource{
				ID:   "my-project/us-central1-a/snapshot-disk",
				Type: resource.TypePersistentDisk,
				Name: "snapshot-disk",
				Config: map[string]interface{}{
					"name":     "snapshot-disk",
					"snapshot": "projects/my-project/global/snapshots/my-snapshot",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about snapshot
				hasSnapshotWarning := false
				for _, w := range result.Warnings {
					if containsPD(w, "snapshot") {
						hasSnapshotWarning = true
						break
					}
				}
				if !hasSnapshotWarning {
					t.Error("Expected warning about snapshot")
				}
			},
		},
		{
			name: "regional persistent disk",
			res: &resource.AWSResource{
				ID:   "my-project/us-central1/regional-disk",
				Type: resource.TypePersistentDisk,
				Name: "regional-disk",
				Config: map[string]interface{}{
					"name": "regional-disk",
					"size": float64(200),
					"replica_zones": []interface{}{
						"us-central1-a",
						"us-central1-b",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about regional disk
				hasRegionalWarning := false
				for _, w := range result.Warnings {
					if containsPD(w, "Regional persistent disk") {
						hasRegionalWarning = true
						break
					}
				}
				if !hasRegionalWarning {
					t.Error("Expected warning about regional disk")
				}
			},
		},
		{
			name: "persistent disk with encryption",
			res: &resource.AWSResource{
				ID:   "my-project/us-central1-a/encrypted-disk",
				Type: resource.TypePersistentDisk,
				Name: "encrypted-disk",
				Config: map[string]interface{}{
					"name": "encrypted-disk",
					"disk_encryption_key": map[string]interface{}{
						"kms_key_self_link": "projects/my-project/locations/global/keyRings/my-keyring/cryptoKeys/my-key",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about encryption
				hasEncryptionWarning := false
				for _, w := range result.Warnings {
					if containsPD(w, "encryption") {
						hasEncryptionWarning = true
						break
					}
				}
				if !hasEncryptionWarning {
					t.Error("Expected warning about encryption")
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

func TestPersistentDiskMapper_sanitizeName(t *testing.T) {
	m := NewPersistentDiskMapper()

	tests := []struct {
		input string
		want  string
	}{
		{"my-disk", "my-disk"},
		{"MY_DISK", "my-disk"},
		{"my disk", "my-disk"},
		{"123disk", "123disk"}, // PersistentDisk allows leading numbers (Docker volume names allow digits)
		{"", "disk"},
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

func TestPersistentDiskMapper_mapDiskTypeToVolumeOpts(t *testing.T) {
	m := NewPersistentDiskMapper()

	tests := []struct {
		diskType string
		sizeGB   int
	}{
		{"pd-standard", 100},
		{"pd-balanced", 200},
		{"pd-ssd", 500},
		{"pd-extreme", 1000},
		{"unknown", 50},
	}

	for _, tt := range tests {
		t.Run(tt.diskType, func(t *testing.T) {
			opts := m.mapDiskTypeToVolumeOpts(tt.diskType, tt.sizeGB)
			if opts == nil {
				t.Error("mapDiskTypeToVolumeOpts returned nil")
			}
		})
	}
}

// containsPD is a helper to check if a string contains a substring
func containsPD(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
