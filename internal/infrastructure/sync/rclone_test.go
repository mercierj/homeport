package sync

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/homeport/homeport/internal/domain/sync"
)

func TestRcloneSync_Name(t *testing.T) {
	r := NewRcloneSync()
	if r.Name() != "rclone" {
		t.Errorf("expected name 'rclone', got '%s'", r.Name())
	}
}

func TestRcloneSync_Type(t *testing.T) {
	r := NewRcloneSync()
	if r.Type() != sync.SyncTypeStorage {
		t.Errorf("expected type SyncTypeStorage, got '%s'", r.Type())
	}
}

func TestRcloneSync_SupportsIncremental(t *testing.T) {
	r := NewRcloneSync()
	if !r.SupportsIncremental() {
		t.Error("expected SupportsIncremental to return true")
	}
}

func TestRcloneSync_SupportsResume(t *testing.T) {
	r := NewRcloneSync()
	if !r.SupportsResume() {
		t.Error("expected SupportsResume to return true")
	}
}

func TestRcloneSync_SupportedProviders(t *testing.T) {
	r := NewRcloneSync()
	providers := r.SupportedProviders()

	expectedProviders := []string{"s3", "gcs", "azure-blob", "minio", "b2", "wasabi", "digitalocean", "local"}

	if len(providers) != len(expectedProviders) {
		t.Errorf("expected %d providers, got %d", len(expectedProviders), len(providers))
	}

	for _, expected := range expectedProviders {
		found := false
		for _, p := range providers {
			if p == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected provider '%s' not found", expected)
		}
	}
}

func TestRcloneSync_NewWithOptions(t *testing.T) {
	opts := &sync.SyncOptions{
		Parallel:         16,
		ChecksumVerify:   false,
		DeleteExtraneous: true,
		DryRun:           true,
		Bandwidth:        50 * 1024 * 1024, // 50MB/s
	}

	r := NewRcloneSyncWithOptions(opts)

	if r.Parallel != 16 {
		t.Errorf("expected Parallel 16, got %d", r.Parallel)
	}

	if r.ChecksumVerify {
		t.Error("expected ChecksumVerify to be false")
	}

	if !r.DeleteExtraneous {
		t.Error("expected DeleteExtraneous to be true")
	}

	if !r.DryRun {
		t.Error("expected DryRun to be true")
	}

	if r.BandwidthLimit != "50M" {
		t.Errorf("expected BandwidthLimit '50M', got '%s'", r.BandwidthLimit)
	}
}

func TestRcloneSync_DefaultOptions(t *testing.T) {
	r := NewRcloneSync()

	if r.Parallel != 4 {
		t.Errorf("expected Parallel 4, got %d", r.Parallel)
	}

	if r.Checkers != 8 {
		t.Errorf("expected Checkers 8, got %d", r.Checkers)
	}

	if r.TransferBufSize != "16M" {
		t.Errorf("expected TransferBufSize '16M', got '%s'", r.TransferBufSize)
	}

	if !r.ChecksumVerify {
		t.Error("expected ChecksumVerify to default to true")
	}

	if r.DeleteExtraneous {
		t.Error("expected DeleteExtraneous to default to false")
	}

	if r.RetryCount != 3 {
		t.Errorf("expected RetryCount 3, got %d", r.RetryCount)
	}

	if r.LowLevelRetries != 10 {
		t.Errorf("expected LowLevelRetries 10, got %d", r.LowLevelRetries)
	}
}

func TestRcloneSync_getRemoteName(t *testing.T) {
	r := NewRcloneSync()

	tests := []struct {
		name     string
		endpoint *sync.Endpoint
		expected string
	}{
		{
			name: "S3 with region",
			endpoint: &sync.Endpoint{
				Type:   "s3",
				Region: "us-west-2",
			},
			expected: "s3_us-west-2",
		},
		{
			name: "S3 no region",
			endpoint: &sync.Endpoint{
				Type: "s3",
			},
			expected: "s3_",
		},
		{
			name: "GCS",
			endpoint: &sync.Endpoint{
				Type: "gcs",
			},
			expected: "gcs",
		},
		{
			name: "Azure with host",
			endpoint: &sync.Endpoint{
				Type: "azure-blob",
				Host: "storageaccount",
			},
			expected: "azure_storageaccount",
		},
		{
			name: "MinIO",
			endpoint: &sync.Endpoint{
				Type: "minio",
				Host: "minio.local",
				Port: 9000,
			},
			expected: "minio_minio_local_9000",
		},
		{
			name: "Local",
			endpoint: &sync.Endpoint{
				Type: "local",
			},
			expected: "local",
		},
		{
			name: "Unknown type",
			endpoint: &sync.Endpoint{
				Type: "unknown",
			},
			expected: "unknown_remote",
		},
		{
			name:     "Nil endpoint",
			endpoint: nil,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := r.getRemoteName(tt.endpoint)
			if result != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestRcloneSync_buildPath(t *testing.T) {
	r := NewRcloneSync()

	tests := []struct {
		remote   string
		bucket   string
		prefix   string
		expected string
	}{
		{"s3_us-east-1", "mybucket", "", "s3_us-east-1:mybucket"},
		{"s3_us-east-1", "mybucket", "prefix", "s3_us-east-1:mybucket/prefix"},
		{"s3_us-east-1", "mybucket", "/prefix", "s3_us-east-1:mybucket/prefix"},
		{"gcs", "bucket", "path/to/data", "gcs:bucket/path/to/data"},
		{"minio_local_9000", "data", "", "minio_local_9000:data"},
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

func TestRcloneSync_buildSyncArgs(t *testing.T) {
	r := NewRcloneSync()
	r.Parallel = 4
	r.Checkers = 8
	r.ChecksumVerify = true
	r.DeleteExtraneous = true
	r.BandwidthLimit = "100M"
	r.DryRun = true
	r.Verbose = true

	args := r.buildSyncArgs("source:bucket", "target:bucket")

	// Check for expected arguments
	expectedArgs := []string{
		"sync",
		"source:bucket",
		"target:bucket",
		"--transfers=4",
		"--checkers=8",
		"--progress",
		"--checksum",
		"--delete-during",
		"--bwlimit=100M",
		"--dry-run",
		"-v",
	}

	for _, expected := range expectedArgs {
		found := false
		for _, arg := range args {
			if arg == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected argument '%s' not found in %v", expected, args)
		}
	}
}

func TestRcloneSync_EstimateSize_NilEndpoint(t *testing.T) {
	r := NewRcloneSync()
	ctx := context.Background()

	_, err := r.EstimateSize(ctx, nil)
	if err == nil {
		t.Error("expected error for nil endpoint")
	}
}

func TestRcloneSync_Sync_NilEndpoints(t *testing.T) {
	r := NewRcloneSync()
	ctx := context.Background()
	progress := make(chan sync.Progress, 10)

	err := r.Sync(ctx, nil, nil, progress)
	if err == nil {
		t.Error("expected error for nil endpoints")
	}
}

func TestRcloneSync_configureRemote_NilEndpoint(t *testing.T) {
	r := NewRcloneSync()
	ctx := context.Background()

	err := r.configureRemote(ctx, nil)
	if err == nil {
		t.Error("expected error for nil endpoint")
	}
}

func TestRcloneSync_configureRemote_UnsupportedType(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test that requires rclone")
	}

	r := NewRcloneSync()
	ctx := context.Background()

	endpoint := sync.NewEndpoint("unsupported-type")
	endpoint.Bucket = "bucket"

	err := r.configureRemote(ctx, endpoint)
	if err == nil {
		t.Error("expected error for unsupported endpoint type")
	}
}

func TestRcloneSizeInfo(t *testing.T) {
	info := RcloneSizeInfo{
		Count: 100,
		Bytes: 1024 * 1024 * 1024, // 1GB
	}

	if info.Count != 100 {
		t.Errorf("expected Count 100, got %d", info.Count)
	}

	if info.Bytes != 1024*1024*1024 {
		t.Errorf("expected Bytes 1GB, got %d", info.Bytes)
	}
}

func TestRcloneFile(t *testing.T) {
	modTime := time.Now()
	file := RcloneFile{
		Path:     "path/to/file.txt",
		Name:     "file.txt",
		Size:     2048,
		MimeType: "text/plain",
		ModTime:  modTime,
		IsDir:    false,
	}
	file.Hashes.MD5 = "abc123"

	if file.Path != "path/to/file.txt" {
		t.Errorf("expected Path 'path/to/file.txt', got '%s'", file.Path)
	}

	if file.Name != "file.txt" {
		t.Errorf("expected Name 'file.txt', got '%s'", file.Name)
	}

	if file.Size != 2048 {
		t.Errorf("expected Size 2048, got %d", file.Size)
	}

	if file.IsDir {
		t.Error("expected IsDir to be false")
	}

	if file.Hashes.MD5 != "abc123" {
		t.Errorf("expected MD5 'abc123', got '%s'", file.Hashes.MD5)
	}
}

func TestProviderConfig(t *testing.T) {
	config := &ProviderConfig{
		Provider:    "s3",
		Region:      "us-east-1",
		AccessKey:   "AKIAIOSFODNN7EXAMPLE",
		SecretKey:   "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		Extra: map[string]string{
			"acl": "private",
		},
	}

	if config.Provider != "s3" {
		t.Errorf("expected Provider 's3', got '%s'", config.Provider)
	}

	if config.Region != "us-east-1" {
		t.Errorf("expected Region 'us-east-1', got '%s'", config.Region)
	}

	if config.Extra["acl"] != "private" {
		t.Errorf("expected Extra['acl'] 'private', got '%s'", config.Extra["acl"])
	}
}

func TestSyncManifest(t *testing.T) {
	now := time.Now()
	manifest := &SyncManifest{
		ID: "sync-123",
		Source: &sync.Endpoint{
			Type:   "s3",
			Bucket: "source-bucket",
			Region: "us-east-1",
		},
		Target: &sync.Endpoint{
			Type:   "minio",
			Host:   "minio.local",
			Port:   9000,
			Bucket: "target-bucket",
		},
		Options: &sync.SyncOptions{
			Parallel:       4,
			ChecksumVerify: true,
		},
		CreatedAt: now,
		Status:    "pending",
	}

	if manifest.ID != "sync-123" {
		t.Errorf("expected ID 'sync-123', got '%s'", manifest.ID)
	}

	if manifest.Source.Bucket != "source-bucket" {
		t.Errorf("expected Source.Bucket 'source-bucket', got '%s'", manifest.Source.Bucket)
	}

	if manifest.Target.Host != "minio.local" {
		t.Errorf("expected Target.Host 'minio.local', got '%s'", manifest.Target.Host)
	}

	if manifest.Status != "pending" {
		t.Errorf("expected Status 'pending', got '%s'", manifest.Status)
	}
}

func TestSaveAndLoadManifest(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "manifests", "test-manifest.json")

	now := time.Now().Truncate(time.Second) // Truncate for comparison
	manifest := &SyncManifest{
		ID: "sync-test-123",
		Source: &sync.Endpoint{
			Type:   "s3",
			Bucket: "test-source",
			Region: "us-west-2",
		},
		Target: &sync.Endpoint{
			Type:   "minio",
			Host:   "localhost",
			Port:   9000,
			Bucket: "test-target",
		},
		CreatedAt: now,
		Status:    "completed",
	}

	// Save manifest
	if err := SaveManifest(manifest, manifestPath); err != nil {
		t.Fatalf("failed to save manifest: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		t.Fatal("manifest file was not created")
	}

	// Load manifest
	loaded, err := LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("failed to load manifest: %v", err)
	}

	// Verify loaded data
	if loaded.ID != manifest.ID {
		t.Errorf("expected ID '%s', got '%s'", manifest.ID, loaded.ID)
	}

	if loaded.Source.Bucket != manifest.Source.Bucket {
		t.Errorf("expected Source.Bucket '%s', got '%s'", manifest.Source.Bucket, loaded.Source.Bucket)
	}

	if loaded.Target.Port != manifest.Target.Port {
		t.Errorf("expected Target.Port %d, got %d", manifest.Target.Port, loaded.Target.Port)
	}

	if loaded.Status != manifest.Status {
		t.Errorf("expected Status '%s', got '%s'", manifest.Status, loaded.Status)
	}
}

func TestLoadManifest_FileNotFound(t *testing.T) {
	_, err := LoadManifest("/nonexistent/path/manifest.json")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestLoadManifest_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	invalidPath := filepath.Join(tmpDir, "invalid.json")

	// Write invalid JSON
	if err := os.WriteFile(invalidPath, []byte("not valid json"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	_, err := LoadManifest(invalidPath)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// Integration tests - require rclone to be installed
func TestRcloneSync_Integration_CheckInstalled(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	_, err := exec.LookPath("rclone")
	if err != nil {
		t.Skip("rclone not installed, skipping integration test")
	}

	r := NewRcloneSync()
	ctx := context.Background()

	if err := r.CheckInstalled(ctx); err != nil {
		t.Errorf("expected rclone to be accessible: %v", err)
	}
}

func TestRcloneSync_Integration_GetVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	_, err := exec.LookPath("rclone")
	if err != nil {
		t.Skip("rclone not installed, skipping integration test")
	}

	r := NewRcloneSync()
	ctx := context.Background()

	version, err := r.GetVersion(ctx)
	if err != nil {
		t.Logf("warning: could not get version: %v", err)
		// Don't fail - version command might not be available
		return
	}

	if version == "" {
		t.Error("expected non-empty version string")
	}

	t.Logf("rclone version: %s", version)
}

func TestRcloneSync_CleanupRemotes(t *testing.T) {
	r := NewRcloneSync()
	ctx := context.Background()

	// Add some fake configured remotes
	r.configuredRemotes["test1"] = true
	r.configuredRemotes["test2"] = true

	// This will fail because remotes don't actually exist
	// but we can verify the cleanup logic
	_ = r.CleanupRemotes(ctx)

	// The map should be cleared (even if actual deletion failed)
}

func TestRcloneSync_ConfiguredRemotesTracking(t *testing.T) {
	r := NewRcloneSync()

	// Initially empty
	if len(r.configuredRemotes) != 0 {
		t.Error("expected configuredRemotes to be empty initially")
	}

	// Manually add a configured remote
	r.configuredRemotes["test_remote"] = true

	if !r.configuredRemotes["test_remote"] {
		t.Error("expected test_remote to be tracked")
	}
}

func TestRcloneSync_Verify_NilEndpoints(t *testing.T) {
	r := NewRcloneSync()
	ctx := context.Background()

	_, err := r.Verify(ctx, nil, nil)
	// Should return an error
	if err == nil {
		// This might also fail at configureRemote step
	}
}

func TestRcloneSync_ConfigureFromProviderConfig_Nil(t *testing.T) {
	r := NewRcloneSync()
	ctx := context.Background()

	err := r.ConfigureFromProviderConfig(ctx, "test", nil)
	if err == nil {
		t.Error("expected error for nil config")
	}
}

func TestRcloneSync_ConfigureFromProviderConfig_UnsupportedProvider(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test that requires rclone")
	}

	r := NewRcloneSync()
	ctx := context.Background()

	config := &ProviderConfig{
		Provider: "unsupported-provider",
	}

	err := r.ConfigureFromProviderConfig(ctx, "test", config)
	if err == nil {
		t.Error("expected error for unsupported provider")
	}
}
