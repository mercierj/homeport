package sync

import (
	"context"
	"os/exec"
	"testing"

	"github.com/homeport/homeport/internal/domain/sync"
)

func TestMinIOSync_Name(t *testing.T) {
	m := NewMinIOSync()
	if m.Name() != "minio" {
		t.Errorf("expected name 'minio', got '%s'", m.Name())
	}
}

func TestMinIOSync_Type(t *testing.T) {
	m := NewMinIOSync()
	if m.Type() != sync.SyncTypeStorage {
		t.Errorf("expected type SyncTypeStorage, got '%s'", m.Type())
	}
}

func TestMinIOSync_SupportsIncremental(t *testing.T) {
	m := NewMinIOSync()
	if !m.SupportsIncremental() {
		t.Error("expected SupportsIncremental to return true")
	}
}

func TestMinIOSync_SupportsResume(t *testing.T) {
	m := NewMinIOSync()
	if !m.SupportsResume() {
		t.Error("expected SupportsResume to return true")
	}
}

func TestMinIOSync_NewWithOptions(t *testing.T) {
	opts := &sync.SyncOptions{
		Parallel:         8,
		ChecksumVerify:   false,
		DeleteExtraneous: true,
		DryRun:           true,
		Bandwidth:        10 * 1024 * 1024, // 10MB/s
	}

	m := NewMinIOSyncWithOptions(opts)

	if m.Parallel != 8 {
		t.Errorf("expected Parallel 8, got %d", m.Parallel)
	}

	if m.ChecksumVerify {
		t.Error("expected ChecksumVerify to be false")
	}

	if !m.DeleteExtraneous {
		t.Error("expected DeleteExtraneous to be true")
	}

	if !m.DryRun {
		t.Error("expected DryRun to be true")
	}

	if m.BandwidthLimit != "10M" {
		t.Errorf("expected BandwidthLimit '10M', got '%s'", m.BandwidthLimit)
	}
}

func TestMinIOSync_EstimateSize_NilSource(t *testing.T) {
	m := NewMinIOSync()
	ctx := context.Background()

	_, err := m.EstimateSize(ctx, nil)
	if err == nil {
		t.Error("expected error for nil source")
	}
}

func TestMinIOSync_EstimateSize_NoBucket(t *testing.T) {
	m := NewMinIOSync()
	ctx := context.Background()

	source := sync.NewEndpoint("s3")
	// No bucket set

	_, err := m.EstimateSize(ctx, source)
	if err == nil {
		t.Error("expected error for missing bucket")
	}
}

func TestMinIOSync_Sync_NilEndpoints(t *testing.T) {
	m := NewMinIOSync()
	ctx := context.Background()
	progress := make(chan sync.Progress, 10)

	err := m.Sync(ctx, nil, nil, progress)
	if err == nil {
		t.Error("expected error for nil endpoints")
	}
}

func TestMinIOSync_Sync_MissingSourceBucket(t *testing.T) {
	m := NewMinIOSync()
	ctx := context.Background()
	progress := make(chan sync.Progress, 10)

	source := sync.NewEndpoint("s3")
	target := sync.NewEndpoint("minio")
	target.Bucket = "target-bucket"

	err := m.Sync(ctx, source, target, progress)
	if err == nil {
		t.Error("expected error for missing source bucket")
	}
}

func TestMinIOSync_Sync_MissingTargetBucket(t *testing.T) {
	m := NewMinIOSync()
	ctx := context.Background()
	progress := make(chan sync.Progress, 10)

	source := sync.NewEndpoint("s3")
	source.Bucket = "source-bucket"
	target := sync.NewEndpoint("minio")

	err := m.Sync(ctx, source, target, progress)
	if err == nil {
		t.Error("expected error for missing target bucket")
	}
}

func TestMinIOSync_getRemoteName(t *testing.T) {
	m := NewMinIOSync()

	tests := []struct {
		name     string
		endpoint *sync.Endpoint
		expected string
	}{
		{
			name: "S3 endpoint with region",
			endpoint: &sync.Endpoint{
				Type:   "s3",
				Region: "us-east-1",
			},
			expected: "s3_us-east-1",
		},
		{
			name: "GCS endpoint",
			endpoint: &sync.Endpoint{
				Type: "gcs",
			},
			expected: "gcs",
		},
		{
			name: "Azure endpoint",
			endpoint: &sync.Endpoint{
				Type: "azure-blob",
				Host: "myaccount",
			},
			expected: "azure_myaccount",
		},
		{
			name: "MinIO endpoint",
			endpoint: &sync.Endpoint{
				Type: "minio",
				Host: "minio.example.com",
				Port: 9000,
			},
			expected: "minio_minio_example_com_9000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.rcloneWrapper.getRemoteName(tt.endpoint)
			if result != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestParallelSync_NewParallelSync(t *testing.T) {
	m := NewMinIOSync()
	ps := NewParallelSync(m, 8)

	if ps.workers != 8 {
		t.Errorf("expected 8 workers, got %d", ps.workers)
	}

	if ps.minioSync != m {
		t.Error("expected minioSync to be set correctly")
	}
}

func TestComputeChecksum_FileNotFound(t *testing.T) {
	_, err := ComputeChecksum("/nonexistent/file/path")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

// Integration test - requires rclone to be installed
func TestMinIOSync_Integration_CheckRcloneInstalled(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	_, err := exec.LookPath("rclone")
	if err != nil {
		t.Skip("rclone not installed, skipping integration test")
	}

	m := NewMinIOSync()
	ctx := context.Background()

	// This test verifies rclone is accessible
	if err := m.rcloneWrapper.CheckInstalled(ctx); err != nil {
		t.Errorf("expected rclone to be installed: %v", err)
	}
}

func TestMinIOSync_BuildPath(t *testing.T) {
	r := NewRcloneSync()

	tests := []struct {
		remote   string
		bucket   string
		prefix   string
		expected string
	}{
		{"s3", "mybucket", "", "s3:mybucket"},
		{"s3", "mybucket", "path/to/prefix", "s3:mybucket/path/to/prefix"},
		{"s3", "mybucket", "/leading/slash", "s3:mybucket/leading/slash"},
		{"minio", "data", "backup/2024", "minio:data/backup/2024"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := r.buildPath(tt.remote, tt.bucket, tt.prefix)
			if result != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestObjectInfo(t *testing.T) {
	obj := &ObjectInfo{
		Key:         "path/to/object.txt",
		Size:        1024,
		ETag:        "abc123",
		ContentType: "text/plain",
	}

	if obj.Key != "path/to/object.txt" {
		t.Errorf("expected key 'path/to/object.txt', got '%s'", obj.Key)
	}

	if obj.Size != 1024 {
		t.Errorf("expected size 1024, got %d", obj.Size)
	}
}

func TestMinIOSync_Verify_NilSource(t *testing.T) {
	m := NewMinIOSync()
	ctx := context.Background()

	target := sync.NewEndpoint("minio")
	target.Bucket = "bucket"

	_, err := m.Verify(ctx, nil, target)
	if err == nil {
		t.Error("expected error for nil source")
	}
}

func TestMinIOSync_Verify_NilTarget(t *testing.T) {
	m := NewMinIOSync()
	ctx := context.Background()

	source := sync.NewEndpoint("s3")
	source.Bucket = "bucket"

	_, err := m.Verify(ctx, source, nil)
	if err == nil {
		t.Error("expected error for nil target")
	}
}

func TestMinIOSync_DefaultOptions(t *testing.T) {
	m := NewMinIOSync()

	if !m.UseRclone {
		t.Error("expected UseRclone to default to true")
	}

	if m.Parallel != 4 {
		t.Errorf("expected Parallel 4, got %d", m.Parallel)
	}

	if !m.ChecksumVerify {
		t.Error("expected ChecksumVerify to default to true")
	}

	if m.DeleteExtraneous {
		t.Error("expected DeleteExtraneous to default to false")
	}

	if m.DryRun {
		t.Error("expected DryRun to default to false")
	}
}

func TestSanitizeRemoteName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"with.dots", "with_dots"},
		{"with-dashes", "with_dashes"},
		{"with:colons", "with_colons"},
		{"192.168.1.1", "192_168_1_1"},
		{"minio.example.com", "minio_example_com"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sanitizeRemoteName(tt.input)
			if result != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}
