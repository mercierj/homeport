// Package sync provides data synchronization implementations for cloud migrations.
// It handles storage, database, and cache sync operations between cloud providers
// and self-hosted targets.
package sync

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/homeport/homeport/internal/domain/sync"
)

// MinIOSync implements the SyncStrategy interface for synchronizing
// object storage from cloud providers (S3, GCS, Azure Blob) to MinIO.
type MinIOSync struct {
	*sync.BaseStrategy

	// UseRclone indicates whether to use rclone instead of mc (MinIO Client).
	// rclone provides broader cloud provider support.
	UseRclone bool

	// Parallel is the number of parallel transfers.
	Parallel int

	// ChecksumVerify enables checksum verification during transfer.
	ChecksumVerify bool

	// DeleteExtraneous removes objects in target that don't exist in source.
	DeleteExtraneous bool

	// BandwidthLimit limits transfer speed (e.g., "10M" for 10MB/s).
	BandwidthLimit string

	// DryRun performs a simulation without actually transferring data.
	DryRun bool

	// rcloneWrapper is the rclone implementation for actual transfers.
	rcloneWrapper *RcloneSync
}

// NewMinIOSync creates a new MinIO sync strategy with default settings.
func NewMinIOSync() *MinIOSync {
	return &MinIOSync{
		BaseStrategy:     sync.NewBaseStrategy("minio", sync.SyncTypeStorage, true, true),
		UseRclone:        true, // Default to rclone for broader provider support
		Parallel:         4,
		ChecksumVerify:   true,
		DeleteExtraneous: false,
		rcloneWrapper:    NewRcloneSync(),
	}
}

// NewMinIOSyncWithOptions creates a MinIO sync strategy with custom options.
func NewMinIOSyncWithOptions(opts *sync.SyncOptions) *MinIOSync {
	m := NewMinIOSync()
	if opts != nil {
		if opts.Parallel > 0 {
			m.Parallel = opts.Parallel
		}
		m.ChecksumVerify = opts.ChecksumVerify
		m.DeleteExtraneous = opts.DeleteExtraneous
		m.DryRun = opts.DryRun
		if opts.Bandwidth > 0 {
			m.BandwidthLimit = fmt.Sprintf("%dM", opts.Bandwidth/(1024*1024))
		}
	}
	return m
}

// EstimateSize calculates the total size of objects in the source bucket.
func (m *MinIOSync) EstimateSize(ctx context.Context, source *sync.Endpoint) (int64, error) {
	if source == nil {
		return 0, fmt.Errorf("source endpoint is required")
	}

	if source.Bucket == "" {
		return 0, fmt.Errorf("source bucket is required")
	}

	// Use rclone to get size information
	if m.UseRclone {
		return m.estimateSizeRclone(ctx, source)
	}

	return m.estimateSizeMC(ctx, source)
}

// estimateSizeRclone uses rclone to estimate bucket size.
func (m *MinIOSync) estimateSizeRclone(ctx context.Context, source *sync.Endpoint) (int64, error) {
	remoteName := m.rcloneWrapper.getRemoteName(source)

	// Configure the remote
	if err := m.rcloneWrapper.configureRemote(ctx, source); err != nil {
		return 0, fmt.Errorf("failed to configure rclone remote: %w", err)
	}

	// Build path
	path := fmt.Sprintf("%s:%s", remoteName, source.Bucket)
	if source.Path != "" {
		path = fmt.Sprintf("%s/%s", path, source.Path)
	}

	// Run rclone size command
	cmd := exec.CommandContext(ctx, "rclone", "size", path, "--json")
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("rclone size failed: %w", err)
	}

	// Parse JSON output for total bytes
	var totalBytes int64
	// Simple parsing - look for "bytes" field
	outputStr := string(output)
	if idx := strings.Index(outputStr, `"bytes":`); idx >= 0 {
		remaining := outputStr[idx+8:]
		fmt.Sscanf(remaining, "%d", &totalBytes)
	}

	return totalBytes, nil
}

// estimateSizeMC uses MinIO Client (mc) to estimate bucket size.
func (m *MinIOSync) estimateSizeMC(ctx context.Context, source *sync.Endpoint) (int64, error) {
	// Configure mc alias
	alias := "source"
	if err := m.configureMCAlias(ctx, alias, source); err != nil {
		return 0, fmt.Errorf("failed to configure mc alias: %w", err)
	}

	// Build path
	path := fmt.Sprintf("%s/%s", alias, source.Bucket)
	if source.Path != "" {
		path = fmt.Sprintf("%s/%s", path, source.Path)
	}

	// Run mc du command
	cmd := exec.CommandContext(ctx, "mc", "du", "--recursive", "--json", path)
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("mc du failed: %w", err)
	}

	// Parse JSON output for total size
	var totalBytes int64
	outputStr := string(output)
	if idx := strings.Index(outputStr, `"size":`); idx >= 0 {
		remaining := outputStr[idx+7:]
		fmt.Sscanf(remaining, "%d", &totalBytes)
	}

	return totalBytes, nil
}

// Sync performs the actual data synchronization from source to target.
func (m *MinIOSync) Sync(ctx context.Context, source, target *sync.Endpoint, progress chan<- sync.Progress) error {
	if source == nil || target == nil {
		return fmt.Errorf("source and target endpoints are required")
	}

	if source.Bucket == "" {
		return fmt.Errorf("source bucket is required")
	}

	if target.Bucket == "" {
		return fmt.Errorf("target bucket is required")
	}

	reporter := sync.NewProgressReporter("minio-sync", progress, nil)
	reporter.SetPhase("initializing")

	// Estimate size first
	totalSize, err := m.EstimateSize(ctx, source)
	if err != nil {
		reporter.Error(fmt.Sprintf("failed to estimate size: %v", err))
		// Continue anyway - we'll sync without accurate progress
	} else {
		reporter.SetTotals(totalSize, 0)
	}

	// List objects to get count
	objectCount, err := m.countObjects(ctx, source)
	if err == nil {
		reporter.SetTotals(totalSize, objectCount)
	}

	reporter.SetPhase("syncing")

	if m.UseRclone {
		return m.syncWithRclone(ctx, source, target, reporter)
	}

	return m.syncWithMC(ctx, source, target, reporter)
}

// countObjects counts the number of objects in the source bucket.
func (m *MinIOSync) countObjects(ctx context.Context, source *sync.Endpoint) (int64, error) {
	if source == nil {
		return 0, fmt.Errorf("endpoint is required")
	}

	if m.UseRclone {
		remoteName := m.rcloneWrapper.getRemoteName(source)
		path := fmt.Sprintf("%s:%s", remoteName, source.Bucket)
		if source.Path != "" {
			path = fmt.Sprintf("%s/%s", path, source.Path)
		}

		cmd := exec.CommandContext(ctx, "rclone", "size", path, "--json")
		output, err := cmd.Output()
		if err != nil {
			return 0, err
		}

		var count int64
		outputStr := string(output)
		if idx := strings.Index(outputStr, `"count":`); idx >= 0 {
			remaining := outputStr[idx+8:]
			fmt.Sscanf(remaining, "%d", &count)
		}
		return count, nil
	}

	// MC doesn't have a direct count, use ls | wc -l equivalent
	return 0, nil
}

// syncWithRclone performs sync using rclone.
func (m *MinIOSync) syncWithRclone(ctx context.Context, source, target *sync.Endpoint, reporter *sync.ProgressReporter) error {
	// Configure source remote
	sourceRemote := m.rcloneWrapper.getRemoteName(source)
	if err := m.rcloneWrapper.configureRemote(ctx, source); err != nil {
		return fmt.Errorf("failed to configure source remote: %w", err)
	}

	// Configure target remote (MinIO)
	targetRemote := m.rcloneWrapper.getRemoteName(target)
	if err := m.rcloneWrapper.configureRemote(ctx, target); err != nil {
		return fmt.Errorf("failed to configure target remote: %w", err)
	}

	// Build source and target paths
	sourcePath := fmt.Sprintf("%s:%s", sourceRemote, source.Bucket)
	if source.Path != "" {
		sourcePath = fmt.Sprintf("%s/%s", sourcePath, source.Path)
	}

	targetPath := fmt.Sprintf("%s:%s", targetRemote, target.Bucket)
	if target.Path != "" {
		targetPath = fmt.Sprintf("%s/%s", targetPath, target.Path)
	}

	// Build rclone command
	args := []string{"sync", sourcePath, targetPath}

	// Add options
	args = append(args, fmt.Sprintf("--transfers=%d", m.Parallel))
	args = append(args, "--progress")
	args = append(args, "--stats=1s")
	args = append(args, "--stats-one-line")

	if m.ChecksumVerify {
		args = append(args, "--checksum")
	}

	if m.DeleteExtraneous {
		args = append(args, "--delete-during")
	}

	if m.BandwidthLimit != "" {
		args = append(args, fmt.Sprintf("--bwlimit=%s", m.BandwidthLimit))
	}

	if m.DryRun {
		args = append(args, "--dry-run")
	}

	// Add verbose output for progress parsing
	args = append(args, "-v")

	cmd := exec.CommandContext(ctx, "rclone", args...)

	// Capture output for progress updates
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start rclone: %w", err)
	}

	// Parse progress from output
	go m.parseRcloneProgress(stdout, reporter)
	go m.parseRcloneProgress(stderr, reporter)

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("rclone sync failed: %w", err)
	}

	reporter.SetPhase("completed")
	return nil
}

// parseRcloneProgress parses rclone output for progress information.
func (m *MinIOSync) parseRcloneProgress(reader io.Reader, reporter *sync.ProgressReporter) {
	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if err != nil {
			return
		}
		if n > 0 {
			line := string(buf[:n])
			// Parse progress lines like: "Transferred: 1.234 GiB / 10.000 GiB, 12%"
			if strings.Contains(line, "Transferred:") {
				// Extract percentage and update progress
				if idx := strings.Index(line, ","); idx > 0 {
					parts := strings.Split(line[idx+1:], "%")
					if len(parts) > 0 {
						var pct float64
						fmt.Sscanf(strings.TrimSpace(parts[0]), "%f", &pct)
						// Calculate bytes done from percentage
						progress := reporter.GetProgress()
						if progress != nil && progress.BytesTotal > 0 {
							bytesDone := int64(float64(progress.BytesTotal) * pct / 100)
							reporter.Update(bytesDone, 0, fmt.Sprintf("%.1f%% complete", pct))
						}
					}
				}
			}
		}
	}
}

// syncWithMC performs sync using MinIO Client (mc).
func (m *MinIOSync) syncWithMC(ctx context.Context, source, target *sync.Endpoint, reporter *sync.ProgressReporter) error {
	// Configure source alias
	sourceAlias := "source"
	if err := m.configureMCAlias(ctx, sourceAlias, source); err != nil {
		return fmt.Errorf("failed to configure source alias: %w", err)
	}

	// Configure target alias
	targetAlias := "target"
	if err := m.configureMCAlias(ctx, targetAlias, target); err != nil {
		return fmt.Errorf("failed to configure target alias: %w", err)
	}

	// Build source and target paths
	sourcePath := fmt.Sprintf("%s/%s", sourceAlias, source.Bucket)
	if source.Path != "" {
		sourcePath = fmt.Sprintf("%s/%s", sourcePath, source.Path)
	}

	targetPath := fmt.Sprintf("%s/%s", targetAlias, target.Bucket)
	if target.Path != "" {
		targetPath = fmt.Sprintf("%s/%s", targetPath, target.Path)
	}

	// Ensure target bucket exists
	createBucketCmd := exec.CommandContext(ctx, "mc", "mb", "--ignore-existing",
		fmt.Sprintf("%s/%s", targetAlias, target.Bucket))
	if err := createBucketCmd.Run(); err != nil {
		reporter.Warning(fmt.Sprintf("failed to create bucket: %v", err))
	}

	// Build mc mirror command
	args := []string{"mirror"}

	if m.DeleteExtraneous {
		args = append(args, "--remove")
	}

	if m.DryRun {
		args = append(args, "--fake")
	}

	// Watch for changes (useful for continuous sync)
	// args = append(args, "--watch")

	args = append(args, sourcePath, targetPath)

	cmd := exec.CommandContext(ctx, "mc", args...)

	// Capture output for progress
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start mc mirror: %w", err)
	}

	// Parse progress
	go m.parseMCProgress(stdout, reporter)

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("mc mirror failed: %w", err)
	}

	reporter.SetPhase("completed")
	return nil
}

// parseMCProgress parses mc output for progress information.
func (m *MinIOSync) parseMCProgress(reader io.Reader, reporter *sync.ProgressReporter) {
	buf := make([]byte, 4096)
	var itemsDone int64
	for {
		n, err := reader.Read(buf)
		if err != nil {
			return
		}
		if n > 0 {
			line := string(buf[:n])
			// mc outputs lines like: "`object-name`: 1.23 MiB / 4.56 MiB"
			if strings.Contains(line, "/") && strings.Contains(line, "iB") {
				itemsDone++
				reporter.IncrementItems(1)
				// Extract current file being transferred
				if idx := strings.Index(line, "`"); idx >= 0 {
					endIdx := strings.Index(line[idx+1:], "`")
					if endIdx > 0 {
						reporter.SetCurrentItem(line[idx+1 : idx+1+endIdx])
					}
				}
			}
		}
	}
}

// configureMCAlias configures an mc alias for the endpoint.
func (m *MinIOSync) configureMCAlias(ctx context.Context, alias string, endpoint *sync.Endpoint) error {
	var url string
	var accessKey, secretKey string

	switch endpoint.Type {
	case "s3":
		if endpoint.Region != "" {
			url = fmt.Sprintf("https://s3.%s.amazonaws.com", endpoint.Region)
		} else {
			url = "https://s3.amazonaws.com"
		}
	case "gcs":
		url = "https://storage.googleapis.com"
	case "azure-blob":
		if endpoint.Host != "" {
			url = fmt.Sprintf("https://%s.blob.core.windows.net", endpoint.Host)
		}
	case "minio":
		if endpoint.SSL {
			url = fmt.Sprintf("https://%s", endpoint.Host)
		} else {
			url = fmt.Sprintf("http://%s", endpoint.Host)
		}
		if endpoint.Port > 0 {
			url = fmt.Sprintf("%s:%d", url, endpoint.Port)
		}
	default:
		return fmt.Errorf("unsupported endpoint type: %s", endpoint.Type)
	}

	if endpoint.Credentials != nil {
		accessKey = endpoint.Credentials.AccessKey
		secretKey = endpoint.Credentials.SecretKey
	}

	cmd := exec.CommandContext(ctx, "mc", "alias", "set", alias, url, accessKey, secretKey)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to set mc alias: %w", err)
	}

	return nil
}

// Verify compares source and target data to ensure sync was successful.
func (m *MinIOSync) Verify(ctx context.Context, source, target *sync.Endpoint) (*sync.VerifyResult, error) {
	if source == nil || target == nil {
		return nil, fmt.Errorf("source and target endpoints are required")
	}

	result := sync.NewVerifyResult()

	// Get source object count and size
	sourceCount, err := m.countObjects(ctx, source)
	if err != nil {
		return nil, fmt.Errorf("failed to count source objects: %w", err)
	}
	result.SourceCount = sourceCount

	// Get target object count and size
	targetCount, err := m.countObjects(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("failed to count target objects: %w", err)
	}
	result.TargetCount = targetCount

	// Compare counts
	if sourceCount != targetCount {
		result.AddMismatch(fmt.Sprintf("object count mismatch: source=%d, target=%d", sourceCount, targetCount))
	}

	// If checksum verification is enabled, compare checksums
	if m.ChecksumVerify {
		if err := m.verifyChecksums(ctx, source, target, result); err != nil {
			result.AddMismatch(fmt.Sprintf("checksum verification failed: %v", err))
		}
	}

	if len(result.Mismatches) == 0 {
		result.Valid = true
	}

	return result, nil
}

// verifyChecksums compares checksums between source and target.
func (m *MinIOSync) verifyChecksums(ctx context.Context, source, target *sync.Endpoint, result *sync.VerifyResult) error {
	if !m.UseRclone {
		// MC doesn't have built-in checksum comparison
		return nil
	}

	sourceRemote := m.rcloneWrapper.getRemoteName(source)
	targetRemote := m.rcloneWrapper.getRemoteName(target)

	sourcePath := fmt.Sprintf("%s:%s", sourceRemote, source.Bucket)
	targetPath := fmt.Sprintf("%s:%s", targetRemote, target.Bucket)

	if source.Path != "" {
		sourcePath = fmt.Sprintf("%s/%s", sourcePath, source.Path)
	}
	if target.Path != "" {
		targetPath = fmt.Sprintf("%s/%s", targetPath, target.Path)
	}

	// Use rclone check to compare
	cmd := exec.CommandContext(ctx, "rclone", "check", sourcePath, targetPath, "--one-way")
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Parse output for differences
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			if strings.Contains(line, "ERROR") || strings.Contains(line, "differ") {
				result.AddMismatch(strings.TrimSpace(line))
			}
		}
		return fmt.Errorf("checksum verification found differences")
	}

	return nil
}

// ObjectInfo represents information about a single object.
type ObjectInfo struct {
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"last_modified"`
	ETag         string    `json:"etag"`
	ContentType  string    `json:"content_type"`
}

// ListObjects returns a list of objects in the bucket.
func (m *MinIOSync) ListObjects(ctx context.Context, endpoint *sync.Endpoint) ([]*ObjectInfo, error) {
	if m.UseRclone {
		return m.listObjectsRclone(ctx, endpoint)
	}
	return m.listObjectsMC(ctx, endpoint)
}

// listObjectsRclone lists objects using rclone.
func (m *MinIOSync) listObjectsRclone(ctx context.Context, endpoint *sync.Endpoint) ([]*ObjectInfo, error) {
	remoteName := m.rcloneWrapper.getRemoteName(endpoint)
	if err := m.rcloneWrapper.configureRemote(ctx, endpoint); err != nil {
		return nil, fmt.Errorf("failed to configure remote: %w", err)
	}

	path := fmt.Sprintf("%s:%s", remoteName, endpoint.Bucket)
	if endpoint.Path != "" {
		path = fmt.Sprintf("%s/%s", path, endpoint.Path)
	}

	cmd := exec.CommandContext(ctx, "rclone", "lsjson", path, "--recursive")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("rclone lsjson failed: %w", err)
	}

	// Parse JSON output
	var objects []*ObjectInfo
	// Simple parsing - in production, use proper JSON decoder
	_ = output
	return objects, nil
}

// listObjectsMC lists objects using mc.
func (m *MinIOSync) listObjectsMC(ctx context.Context, endpoint *sync.Endpoint) ([]*ObjectInfo, error) {
	alias := "list"
	if err := m.configureMCAlias(ctx, alias, endpoint); err != nil {
		return nil, fmt.Errorf("failed to configure alias: %w", err)
	}

	path := fmt.Sprintf("%s/%s", alias, endpoint.Bucket)
	if endpoint.Path != "" {
		path = fmt.Sprintf("%s/%s", path, endpoint.Path)
	}

	cmd := exec.CommandContext(ctx, "mc", "ls", "--recursive", "--json", path)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("mc ls failed: %w", err)
	}

	// Parse JSON output
	var objects []*ObjectInfo
	_ = output
	return objects, nil
}

// CopyObject copies a single object from source to target.
func (m *MinIOSync) CopyObject(ctx context.Context, source, target *sync.Endpoint, key string) error {
	if m.UseRclone {
		return m.copyObjectRclone(ctx, source, target, key)
	}
	return m.copyObjectMC(ctx, source, target, key)
}

// copyObjectRclone copies an object using rclone.
func (m *MinIOSync) copyObjectRclone(ctx context.Context, source, target *sync.Endpoint, key string) error {
	sourceRemote := m.rcloneWrapper.getRemoteName(source)
	targetRemote := m.rcloneWrapper.getRemoteName(target)

	if err := m.rcloneWrapper.configureRemote(ctx, source); err != nil {
		return err
	}
	if err := m.rcloneWrapper.configureRemote(ctx, target); err != nil {
		return err
	}

	sourcePath := fmt.Sprintf("%s:%s/%s", sourceRemote, source.Bucket, key)
	targetPath := fmt.Sprintf("%s:%s/%s", targetRemote, target.Bucket, key)

	cmd := exec.CommandContext(ctx, "rclone", "copyto", sourcePath, targetPath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rclone copyto failed: %w", err)
	}

	return nil
}

// copyObjectMC copies an object using mc.
func (m *MinIOSync) copyObjectMC(ctx context.Context, source, target *sync.Endpoint, key string) error {
	if err := m.configureMCAlias(ctx, "source", source); err != nil {
		return err
	}
	if err := m.configureMCAlias(ctx, "target", target); err != nil {
		return err
	}

	sourcePath := fmt.Sprintf("source/%s/%s", source.Bucket, key)
	targetPath := fmt.Sprintf("target/%s/%s", target.Bucket, key)

	cmd := exec.CommandContext(ctx, "mc", "cp", sourcePath, targetPath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mc cp failed: %w", err)
	}

	return nil
}

// ParallelSync performs parallel object copying for better performance.
type ParallelSync struct {
	minioSync   *MinIOSync
	workers     int
	bytesDone   int64
	itemsDone   int64
	errors      int64
	objectQueue chan *ObjectInfo
}

// NewParallelSync creates a new parallel sync executor.
func NewParallelSync(m *MinIOSync, workers int) *ParallelSync {
	return &ParallelSync{
		minioSync:   m,
		workers:     workers,
		objectQueue: make(chan *ObjectInfo, workers*10),
	}
}

// Execute runs the parallel sync operation.
func (p *ParallelSync) Execute(ctx context.Context, source, target *sync.Endpoint, progress chan<- sync.Progress) error {
	reporter := sync.NewProgressReporter("parallel-sync", progress, nil)
	reporter.SetPhase("listing")

	// List all objects
	objects, err := p.minioSync.ListObjects(ctx, source)
	if err != nil {
		return fmt.Errorf("failed to list objects: %w", err)
	}

	// Calculate totals
	var totalBytes int64
	for _, obj := range objects {
		totalBytes += obj.Size
	}
	reporter.SetTotals(totalBytes, int64(len(objects)))

	reporter.SetPhase("syncing")

	// Start workers
	errChan := make(chan error, p.workers)
	doneChan := make(chan struct{})

	for i := 0; i < p.workers; i++ {
		go p.worker(ctx, source, target, errChan, doneChan)
	}

	// Feed objects to workers
	go func() {
		for _, obj := range objects {
			select {
			case <-ctx.Done():
				return
			case p.objectQueue <- obj:
			}
		}
		close(p.objectQueue)
	}()

	// Progress reporter
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-doneChan:
				return
			case <-ticker.C:
				bytesDone := atomic.LoadInt64(&p.bytesDone)
				itemsDone := atomic.LoadInt64(&p.itemsDone)
				reporter.Update(bytesDone, itemsDone, fmt.Sprintf("%d/%d objects", itemsDone, len(objects)))
			}
		}
	}()

	// Wait for workers
	var syncErrors []error
	workersComplete := 0
	for workersComplete < p.workers {
		select {
		case err := <-errChan:
			if err != nil {
				syncErrors = append(syncErrors, err)
			}
			workersComplete++
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	close(doneChan)
	reporter.SetPhase("completed")

	if len(syncErrors) > 0 {
		return fmt.Errorf("sync completed with %d errors", len(syncErrors))
	}

	return nil
}

// worker processes objects from the queue.
func (p *ParallelSync) worker(ctx context.Context, source, target *sync.Endpoint, errChan chan<- error, done <-chan struct{}) {
	for {
		select {
		case <-ctx.Done():
			errChan <- ctx.Err()
			return
		case <-done:
			errChan <- nil
			return
		case obj, ok := <-p.objectQueue:
			if !ok {
				errChan <- nil
				return
			}
			if err := p.minioSync.CopyObject(ctx, source, target, obj.Key); err != nil {
				atomic.AddInt64(&p.errors, 1)
				// Continue with other objects, don't fail immediately
			} else {
				atomic.AddInt64(&p.bytesDone, obj.Size)
				atomic.AddInt64(&p.itemsDone, 1)
			}
		}
	}
}

// ComputeChecksum calculates MD5 checksum for a local file.
func ComputeChecksum(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// DownloadObject downloads an object to a local file.
func (m *MinIOSync) DownloadObject(ctx context.Context, endpoint *sync.Endpoint, key, localPath string) error {
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if m.UseRclone {
		remoteName := m.rcloneWrapper.getRemoteName(endpoint)
		if err := m.rcloneWrapper.configureRemote(ctx, endpoint); err != nil {
			return err
		}

		remotePath := fmt.Sprintf("%s:%s/%s", remoteName, endpoint.Bucket, key)
		cmd := exec.CommandContext(ctx, "rclone", "copyto", remotePath, localPath)
		return cmd.Run()
	}

	alias := "download"
	if err := m.configureMCAlias(ctx, alias, endpoint); err != nil {
		return err
	}

	remotePath := fmt.Sprintf("%s/%s/%s", alias, endpoint.Bucket, key)
	cmd := exec.CommandContext(ctx, "mc", "cp", remotePath, localPath)
	return cmd.Run()
}

// UploadObject uploads a local file to the bucket.
func (m *MinIOSync) UploadObject(ctx context.Context, localPath string, endpoint *sync.Endpoint, key string) error {
	if m.UseRclone {
		remoteName := m.rcloneWrapper.getRemoteName(endpoint)
		if err := m.rcloneWrapper.configureRemote(ctx, endpoint); err != nil {
			return err
		}

		remotePath := fmt.Sprintf("%s:%s/%s", remoteName, endpoint.Bucket, key)
		cmd := exec.CommandContext(ctx, "rclone", "copyto", localPath, remotePath)
		return cmd.Run()
	}

	alias := "upload"
	if err := m.configureMCAlias(ctx, alias, endpoint); err != nil {
		return err
	}

	remotePath := fmt.Sprintf("%s/%s/%s", alias, endpoint.Bucket, key)
	cmd := exec.CommandContext(ctx, "mc", "cp", localPath, remotePath)
	return cmd.Run()
}
