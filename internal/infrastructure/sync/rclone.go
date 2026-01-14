package sync

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/sync"
)

// RcloneSync implements the SyncStrategy interface using rclone for cross-cloud
// storage synchronization. Supports AWS S3, GCP Cloud Storage, Azure Blob Storage,
// and MinIO as source/target.
type RcloneSync struct {
	*sync.BaseStrategy

	// ConfigPath is the path to the rclone config file.
	// If empty, uses default location or in-memory config.
	ConfigPath string

	// Parallel is the number of parallel file transfers.
	Parallel int

	// Checkers is the number of checkers to run in parallel.
	Checkers int

	// TransferBufSize is the buffer size for transfers.
	TransferBufSize string

	// ChecksumVerify enables checksum verification.
	ChecksumVerify bool

	// DeleteExtraneous removes files in destination not in source.
	DeleteExtraneous bool

	// BandwidthLimit limits transfer bandwidth (e.g., "10M").
	BandwidthLimit string

	// DryRun simulates the sync without making changes.
	DryRun bool

	// Verbose enables verbose output.
	Verbose bool

	// RetryCount is the number of retries for failed transfers.
	RetryCount int

	// LowLevelRetries is the number of low-level retries.
	LowLevelRetries int

	// configuredRemotes tracks which remotes have been configured.
	configuredRemotes map[string]bool
}

// NewRcloneSync creates a new rclone sync strategy with default settings.
func NewRcloneSync() *RcloneSync {
	return &RcloneSync{
		BaseStrategy:      sync.NewBaseStrategy("rclone", sync.SyncTypeStorage, true, true),
		Parallel:          4,
		Checkers:          8,
		TransferBufSize:   "16M",
		ChecksumVerify:    true,
		DeleteExtraneous:  false,
		RetryCount:        3,
		LowLevelRetries:   10,
		configuredRemotes: make(map[string]bool),
	}
}

// NewRcloneSyncWithOptions creates an rclone sync strategy with custom options.
func NewRcloneSyncWithOptions(opts *sync.SyncOptions) *RcloneSync {
	r := NewRcloneSync()
	if opts != nil {
		if opts.Parallel > 0 {
			r.Parallel = opts.Parallel
		}
		r.ChecksumVerify = opts.ChecksumVerify
		r.DeleteExtraneous = opts.DeleteExtraneous
		r.DryRun = opts.DryRun
		if opts.Bandwidth > 0 {
			r.BandwidthLimit = fmt.Sprintf("%dM", opts.Bandwidth/(1024*1024))
		}
	}
	return r
}

// SupportedProviders returns the list of cloud storage providers supported.
func (r *RcloneSync) SupportedProviders() []string {
	return []string{
		"s3",          // AWS S3
		"gcs",         // Google Cloud Storage
		"azure-blob",  // Azure Blob Storage
		"minio",       // MinIO
		"b2",          // Backblaze B2
		"wasabi",      // Wasabi
		"digitalocean", // DigitalOcean Spaces
		"local",       // Local filesystem
	}
}

// EstimateSize calculates the total size of objects in the source.
func (r *RcloneSync) EstimateSize(ctx context.Context, source *sync.Endpoint) (int64, error) {
	if source == nil {
		return 0, fmt.Errorf("source endpoint is required")
	}

	remoteName := r.getRemoteName(source)
	if err := r.configureRemote(ctx, source); err != nil {
		return 0, fmt.Errorf("failed to configure remote: %w", err)
	}

	path := r.buildPath(remoteName, source.Bucket, source.Path)

	cmd := exec.CommandContext(ctx, "rclone", "size", path, "--json")
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("rclone size failed: %w", err)
	}

	var sizeInfo RcloneSizeInfo
	if err := json.Unmarshal(output, &sizeInfo); err != nil {
		return 0, fmt.Errorf("failed to parse size info: %w", err)
	}

	return sizeInfo.Bytes, nil
}

// RcloneSizeInfo represents the output of rclone size --json.
type RcloneSizeInfo struct {
	Count int64 `json:"count"`
	Bytes int64 `json:"bytes"`
}

// Sync performs the data synchronization from source to target.
func (r *RcloneSync) Sync(ctx context.Context, source, target *sync.Endpoint, progress chan<- sync.Progress) error {
	if source == nil || target == nil {
		return fmt.Errorf("source and target endpoints are required")
	}

	reporter := sync.NewProgressReporter("rclone-sync", progress, nil)
	reporter.SetPhase("initializing")

	// Configure remotes
	if err := r.configureRemote(ctx, source); err != nil {
		return fmt.Errorf("failed to configure source remote: %w", err)
	}

	if err := r.configureRemote(ctx, target); err != nil {
		return fmt.Errorf("failed to configure target remote: %w", err)
	}

	// Get size estimate
	totalSize, err := r.EstimateSize(ctx, source)
	if err != nil {
		reporter.Warning(fmt.Sprintf("could not estimate size: %v", err))
	} else {
		reporter.SetTotals(totalSize, 0)
	}

	// Get object count
	count, err := r.countObjects(ctx, source)
	if err == nil {
		reporter.SetTotals(totalSize, count)
	}

	reporter.SetPhase("syncing")

	// Build paths
	sourceRemote := r.getRemoteName(source)
	targetRemote := r.getRemoteName(target)
	sourcePath := r.buildPath(sourceRemote, source.Bucket, source.Path)
	targetPath := r.buildPath(targetRemote, target.Bucket, target.Path)

	// Build command
	args := r.buildSyncArgs(sourcePath, targetPath)

	cmd := exec.CommandContext(ctx, "rclone", args...)

	// Capture stderr for progress parsing
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start rclone: %w", err)
	}

	// Parse progress in background
	go r.parseProgress(stderr, reporter)
	go r.parseProgress(stdout, reporter)

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("rclone sync failed: %w", err)
	}

	reporter.SetPhase("completed")
	return nil
}

// countObjects counts objects in the source.
func (r *RcloneSync) countObjects(ctx context.Context, source *sync.Endpoint) (int64, error) {
	remoteName := r.getRemoteName(source)
	path := r.buildPath(remoteName, source.Bucket, source.Path)

	cmd := exec.CommandContext(ctx, "rclone", "size", path, "--json")
	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	var sizeInfo RcloneSizeInfo
	if err := json.Unmarshal(output, &sizeInfo); err != nil {
		return 0, err
	}

	return sizeInfo.Count, nil
}

// buildSyncArgs builds the rclone sync command arguments.
func (r *RcloneSync) buildSyncArgs(sourcePath, targetPath string) []string {
	args := []string{"sync", sourcePath, targetPath}

	// Parallel transfers
	args = append(args, fmt.Sprintf("--transfers=%d", r.Parallel))
	args = append(args, fmt.Sprintf("--checkers=%d", r.Checkers))

	// Progress and stats
	args = append(args, "--progress")
	args = append(args, "--stats=1s")
	args = append(args, "--stats-one-line")

	// Buffer size
	if r.TransferBufSize != "" {
		args = append(args, fmt.Sprintf("--buffer-size=%s", r.TransferBufSize))
	}

	// Checksum verification
	if r.ChecksumVerify {
		args = append(args, "--checksum")
	}

	// Delete extraneous files
	if r.DeleteExtraneous {
		args = append(args, "--delete-during")
	}

	// Bandwidth limit
	if r.BandwidthLimit != "" {
		args = append(args, fmt.Sprintf("--bwlimit=%s", r.BandwidthLimit))
	}

	// Dry run
	if r.DryRun {
		args = append(args, "--dry-run")
	}

	// Verbose
	if r.Verbose {
		args = append(args, "-v")
	}

	// Retries
	args = append(args, fmt.Sprintf("--retries=%d", r.RetryCount))
	args = append(args, fmt.Sprintf("--low-level-retries=%d", r.LowLevelRetries))

	// Use server-side copy when possible
	args = append(args, "--s3-no-check-bucket")

	return args
}

// parseProgress parses rclone output and updates progress.
func (r *RcloneSync) parseProgress(reader io.Reader, reporter *sync.ProgressReporter) {
	scanner := bufio.NewScanner(reader)

	// Regex patterns for parsing rclone output
	// Example: "Transferred:   1.234 GiB / 10.000 GiB, 12%, 50.000 MiB/s, ETA 3m15s"
	transferredPattern := regexp.MustCompile(`Transferred:\s+([0-9.]+)\s*(\w+)\s*/\s*([0-9.]+)\s*(\w+),\s*([0-9]+)%`)
	// Example: "Transferred:   100 / 1000, 10%"
	countPattern := regexp.MustCompile(`Transferred:\s+(\d+)\s*/\s*(\d+),\s*(\d+)%`)

	for scanner.Scan() {
		line := scanner.Text()

		// Parse transferred bytes
		if matches := transferredPattern.FindStringSubmatch(line); matches != nil {
			pct, _ := strconv.ParseFloat(matches[5], 64)
			progress := reporter.GetProgress()
			if progress != nil && progress.BytesTotal > 0 {
				bytesDone := int64(float64(progress.BytesTotal) * pct / 100)
				reporter.Update(bytesDone, 0, fmt.Sprintf("%.1f%% complete", pct))
			}
		}

		// Parse transferred count
		if matches := countPattern.FindStringSubmatch(line); matches != nil {
			done, _ := strconv.ParseInt(matches[1], 10, 64)
			total, _ := strconv.ParseInt(matches[2], 10, 64)
			pct, _ := strconv.ParseFloat(matches[3], 64)
			reporter.Update(0, done, fmt.Sprintf("%d/%d objects (%.1f%%)", done, total, pct))
		}

		// Parse errors
		if strings.Contains(line, "ERROR") {
			reporter.Error(line)
		}
	}
}

// Verify compares source and target to ensure sync was successful.
func (r *RcloneSync) Verify(ctx context.Context, source, target *sync.Endpoint) (*sync.VerifyResult, error) {
	result := sync.NewVerifyResult()

	// Configure remotes
	if err := r.configureRemote(ctx, source); err != nil {
		return nil, fmt.Errorf("failed to configure source: %w", err)
	}
	if err := r.configureRemote(ctx, target); err != nil {
		return nil, fmt.Errorf("failed to configure target: %w", err)
	}

	// Get source size and count
	sourceRemote := r.getRemoteName(source)
	sourcePath := r.buildPath(sourceRemote, source.Bucket, source.Path)

	sourceSizeCmd := exec.CommandContext(ctx, "rclone", "size", sourcePath, "--json")
	sourceOutput, err := sourceSizeCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get source size: %w", err)
	}

	var sourceSize RcloneSizeInfo
	if err := json.Unmarshal(sourceOutput, &sourceSize); err != nil {
		return nil, fmt.Errorf("failed to parse source size: %w", err)
	}
	result.SourceCount = sourceSize.Count

	// Get target size and count
	targetRemote := r.getRemoteName(target)
	targetPath := r.buildPath(targetRemote, target.Bucket, target.Path)

	targetSizeCmd := exec.CommandContext(ctx, "rclone", "size", targetPath, "--json")
	targetOutput, err := targetSizeCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get target size: %w", err)
	}

	var targetSize RcloneSizeInfo
	if err := json.Unmarshal(targetOutput, &targetSize); err != nil {
		return nil, fmt.Errorf("failed to parse target size: %w", err)
	}
	result.TargetCount = targetSize.Count

	// Compare counts
	if sourceSize.Count != targetSize.Count {
		result.AddMismatch(fmt.Sprintf("object count mismatch: source=%d, target=%d",
			sourceSize.Count, targetSize.Count))
	}

	// Compare total bytes
	if sourceSize.Bytes != targetSize.Bytes {
		result.AddMismatch(fmt.Sprintf("total size mismatch: source=%d bytes, target=%d bytes",
			sourceSize.Bytes, targetSize.Bytes))
	}

	// Run rclone check for detailed comparison
	if r.ChecksumVerify {
		checkCmd := exec.CommandContext(ctx, "rclone", "check", sourcePath, targetPath,
			"--one-way", "--combined", "-")
		checkOutput, err := checkCmd.CombinedOutput()
		if err != nil {
			// Parse output for specific differences
			lines := strings.Split(string(checkOutput), "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line != "" && (strings.HasPrefix(line, "-") || strings.HasPrefix(line, "*")) {
					result.AddMismatch(line)
				}
			}
		}
	}

	if len(result.Mismatches) == 0 {
		result.Valid = true
	}

	result.Details["source_bytes"] = sourceSize.Bytes
	result.Details["target_bytes"] = targetSize.Bytes

	return result, nil
}

// getRemoteName generates a unique remote name for an endpoint.
func (r *RcloneSync) getRemoteName(endpoint *sync.Endpoint) string {
	if endpoint == nil {
		return ""
	}

	// Use endpoint type and a hash of the connection details
	switch endpoint.Type {
	case "s3":
		return fmt.Sprintf("s3_%s", endpoint.Region)
	case "gcs":
		return "gcs"
	case "azure-blob":
		return fmt.Sprintf("azure_%s", endpoint.Host)
	case "minio":
		return fmt.Sprintf("minio_%s_%d", sanitizeRemoteName(endpoint.Host), endpoint.Port)
	case "local":
		return "local"
	default:
		return fmt.Sprintf("%s_remote", endpoint.Type)
	}
}

// sanitizeRemoteName removes invalid characters from remote names.
func sanitizeRemoteName(name string) string {
	// Replace dots and special chars with underscores
	result := strings.ReplaceAll(name, ".", "_")
	result = strings.ReplaceAll(result, "-", "_")
	result = strings.ReplaceAll(result, ":", "_")
	return result
}

// buildPath constructs the rclone path from remote name, bucket, and prefix.
func (r *RcloneSync) buildPath(remoteName, bucket, prefix string) string {
	path := fmt.Sprintf("%s:%s", remoteName, bucket)
	if prefix != "" {
		path = fmt.Sprintf("%s/%s", path, strings.TrimPrefix(prefix, "/"))
	}
	return path
}

// configureRemote sets up rclone remote configuration for an endpoint.
func (r *RcloneSync) configureRemote(ctx context.Context, endpoint *sync.Endpoint) error {
	if endpoint == nil {
		return fmt.Errorf("endpoint is required")
	}

	remoteName := r.getRemoteName(endpoint)

	// Skip if already configured
	if r.configuredRemotes[remoteName] {
		return nil
	}

	switch endpoint.Type {
	case "s3":
		return r.configureS3Remote(ctx, remoteName, endpoint)
	case "gcs":
		return r.configureGCSRemote(ctx, remoteName, endpoint)
	case "azure-blob":
		return r.configureAzureRemote(ctx, remoteName, endpoint)
	case "minio":
		return r.configureMinIORemote(ctx, remoteName, endpoint)
	case "local":
		// Local doesn't need configuration
		r.configuredRemotes[remoteName] = true
		return nil
	default:
		return fmt.Errorf("unsupported endpoint type: %s", endpoint.Type)
	}
}

// configureS3Remote configures an S3 remote.
func (r *RcloneSync) configureS3Remote(ctx context.Context, remoteName string, endpoint *sync.Endpoint) error {
	args := []string{"config", "create", remoteName, "s3"}

	// Provider
	args = append(args, "provider=AWS")

	// Region
	if endpoint.Region != "" {
		args = append(args, fmt.Sprintf("region=%s", endpoint.Region))
	}

	// Credentials
	if endpoint.Credentials != nil {
		if endpoint.Credentials.AccessKey != "" {
			args = append(args, fmt.Sprintf("access_key_id=%s", endpoint.Credentials.AccessKey))
		}
		if endpoint.Credentials.SecretKey != "" {
			args = append(args, fmt.Sprintf("secret_access_key=%s", endpoint.Credentials.SecretKey))
		}
	} else {
		// Use environment credentials
		args = append(args, "env_auth=true")
	}

	cmd := exec.CommandContext(ctx, "rclone", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to configure S3 remote: %w", err)
	}

	r.configuredRemotes[remoteName] = true
	return nil
}

// configureGCSRemote configures a Google Cloud Storage remote.
func (r *RcloneSync) configureGCSRemote(ctx context.Context, remoteName string, endpoint *sync.Endpoint) error {
	args := []string{"config", "create", remoteName, "gcs"}

	// Bucket policy only
	args = append(args, "bucket_policy_only=true")

	// Service account file if provided
	if endpoint.Credentials != nil && endpoint.Credentials.KeyFile != "" {
		args = append(args, fmt.Sprintf("service_account_file=%s", endpoint.Credentials.KeyFile))
	}

	// Project if provided
	if project, ok := endpoint.Options["project"]; ok {
		args = append(args, fmt.Sprintf("project_number=%s", project))
	}

	cmd := exec.CommandContext(ctx, "rclone", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to configure GCS remote: %w", err)
	}

	r.configuredRemotes[remoteName] = true
	return nil
}

// configureAzureRemote configures an Azure Blob Storage remote.
func (r *RcloneSync) configureAzureRemote(ctx context.Context, remoteName string, endpoint *sync.Endpoint) error {
	args := []string{"config", "create", remoteName, "azureblob"}

	// Account name
	if endpoint.Host != "" {
		args = append(args, fmt.Sprintf("account=%s", endpoint.Host))
	}

	// SAS URL or Key
	if endpoint.Credentials != nil {
		if endpoint.Credentials.SecretKey != "" {
			args = append(args, fmt.Sprintf("key=%s", endpoint.Credentials.SecretKey))
		}
		if endpoint.Credentials.Token != "" {
			args = append(args, fmt.Sprintf("sas_url=%s", endpoint.Credentials.Token))
		}
	}

	cmd := exec.CommandContext(ctx, "rclone", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to configure Azure remote: %w", err)
	}

	r.configuredRemotes[remoteName] = true
	return nil
}

// configureMinIORemote configures a MinIO remote.
func (r *RcloneSync) configureMinIORemote(ctx context.Context, remoteName string, endpoint *sync.Endpoint) error {
	args := []string{"config", "create", remoteName, "s3"}

	// Provider
	args = append(args, "provider=Minio")

	// Endpoint URL
	scheme := "http"
	if endpoint.SSL {
		scheme = "https"
	}
	endpointURL := fmt.Sprintf("%s://%s", scheme, endpoint.Host)
	if endpoint.Port > 0 {
		endpointURL = fmt.Sprintf("%s:%d", endpointURL, endpoint.Port)
	}
	args = append(args, fmt.Sprintf("endpoint=%s", endpointURL))

	// Credentials
	if endpoint.Credentials != nil {
		if endpoint.Credentials.AccessKey != "" {
			args = append(args, fmt.Sprintf("access_key_id=%s", endpoint.Credentials.AccessKey))
		}
		if endpoint.Credentials.SecretKey != "" {
			args = append(args, fmt.Sprintf("secret_access_key=%s", endpoint.Credentials.SecretKey))
		}
	}

	cmd := exec.CommandContext(ctx, "rclone", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to configure MinIO remote: %w", err)
	}

	r.configuredRemotes[remoteName] = true
	return nil
}

// ListFiles returns a list of files from the remote.
func (r *RcloneSync) ListFiles(ctx context.Context, endpoint *sync.Endpoint) ([]*RcloneFile, error) {
	if err := r.configureRemote(ctx, endpoint); err != nil {
		return nil, err
	}

	remoteName := r.getRemoteName(endpoint)
	path := r.buildPath(remoteName, endpoint.Bucket, endpoint.Path)

	cmd := exec.CommandContext(ctx, "rclone", "lsjson", path, "--recursive")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list files: %w", err)
	}

	var files []*RcloneFile
	if err := json.Unmarshal(output, &files); err != nil {
		return nil, fmt.Errorf("failed to parse file list: %w", err)
	}

	return files, nil
}

// RcloneFile represents a file/object in rclone lsjson output.
type RcloneFile struct {
	Path     string    `json:"Path"`
	Name     string    `json:"Name"`
	Size     int64     `json:"Size"`
	MimeType string    `json:"MimeType"`
	ModTime  time.Time `json:"ModTime"`
	IsDir    bool      `json:"IsDir"`
	Hashes   struct {
		MD5    string `json:"MD5,omitempty"`
		SHA1   string `json:"SHA1,omitempty"`
		SHA256 string `json:"SHA256,omitempty"`
	} `json:"Hashes,omitempty"`
}

// CopyFile copies a single file from source to target.
func (r *RcloneSync) CopyFile(ctx context.Context, source, target *sync.Endpoint, filePath string) error {
	if err := r.configureRemote(ctx, source); err != nil {
		return err
	}
	if err := r.configureRemote(ctx, target); err != nil {
		return err
	}

	sourceRemote := r.getRemoteName(source)
	targetRemote := r.getRemoteName(target)

	sourcePath := fmt.Sprintf("%s:%s/%s", sourceRemote, source.Bucket, filePath)
	targetPath := fmt.Sprintf("%s:%s/%s", targetRemote, target.Bucket, filePath)

	args := []string{"copyto", sourcePath, targetPath}
	if r.ChecksumVerify {
		args = append(args, "--checksum")
	}

	cmd := exec.CommandContext(ctx, "rclone", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to copy file: %w", err)
	}

	return nil
}

// DeleteFile removes a file from the remote.
func (r *RcloneSync) DeleteFile(ctx context.Context, endpoint *sync.Endpoint, filePath string) error {
	if err := r.configureRemote(ctx, endpoint); err != nil {
		return err
	}

	remoteName := r.getRemoteName(endpoint)
	path := fmt.Sprintf("%s:%s/%s", remoteName, endpoint.Bucket, filePath)

	cmd := exec.CommandContext(ctx, "rclone", "deletefile", path)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to delete file: %w", err)
	}

	return nil
}

// CreateBucket creates a new bucket/container.
func (r *RcloneSync) CreateBucket(ctx context.Context, endpoint *sync.Endpoint) error {
	if err := r.configureRemote(ctx, endpoint); err != nil {
		return err
	}

	remoteName := r.getRemoteName(endpoint)
	path := fmt.Sprintf("%s:%s", remoteName, endpoint.Bucket)

	cmd := exec.CommandContext(ctx, "rclone", "mkdir", path)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create bucket: %w", err)
	}

	return nil
}

// DeleteBucket removes a bucket and all its contents.
func (r *RcloneSync) DeleteBucket(ctx context.Context, endpoint *sync.Endpoint) error {
	if err := r.configureRemote(ctx, endpoint); err != nil {
		return err
	}

	remoteName := r.getRemoteName(endpoint)
	path := fmt.Sprintf("%s:%s", remoteName, endpoint.Bucket)

	cmd := exec.CommandContext(ctx, "rclone", "purge", path)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to delete bucket: %w", err)
	}

	return nil
}

// DownloadToLocal downloads files from remote to local filesystem.
func (r *RcloneSync) DownloadToLocal(ctx context.Context, endpoint *sync.Endpoint, localPath string) error {
	if err := r.configureRemote(ctx, endpoint); err != nil {
		return err
	}

	// Ensure local directory exists
	if err := os.MkdirAll(localPath, 0755); err != nil {
		return fmt.Errorf("failed to create local directory: %w", err)
	}

	remoteName := r.getRemoteName(endpoint)
	remotePath := r.buildPath(remoteName, endpoint.Bucket, endpoint.Path)

	args := []string{"copy", remotePath, localPath}
	args = append(args, fmt.Sprintf("--transfers=%d", r.Parallel))

	if r.ChecksumVerify {
		args = append(args, "--checksum")
	}

	cmd := exec.CommandContext(ctx, "rclone", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to download files: %w", err)
	}

	return nil
}

// UploadFromLocal uploads files from local filesystem to remote.
func (r *RcloneSync) UploadFromLocal(ctx context.Context, localPath string, endpoint *sync.Endpoint) error {
	if err := r.configureRemote(ctx, endpoint); err != nil {
		return err
	}

	remoteName := r.getRemoteName(endpoint)
	remotePath := r.buildPath(remoteName, endpoint.Bucket, endpoint.Path)

	args := []string{"copy", localPath, remotePath}
	args = append(args, fmt.Sprintf("--transfers=%d", r.Parallel))

	if r.ChecksumVerify {
		args = append(args, "--checksum")
	}

	cmd := exec.CommandContext(ctx, "rclone", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to upload files: %w", err)
	}

	return nil
}

// GetConfig returns the current rclone configuration.
func (r *RcloneSync) GetConfig(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "rclone", "config", "dump")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get config: %w", err)
	}
	return string(output), nil
}

// RemoveRemote removes a configured remote.
func (r *RcloneSync) RemoveRemote(ctx context.Context, remoteName string) error {
	cmd := exec.CommandContext(ctx, "rclone", "config", "delete", remoteName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to remove remote: %w", err)
	}

	delete(r.configuredRemotes, remoteName)
	return nil
}

// CleanupRemotes removes all configured remotes.
func (r *RcloneSync) CleanupRemotes(ctx context.Context) error {
	for remoteName := range r.configuredRemotes {
		if err := r.RemoveRemote(ctx, remoteName); err != nil {
			return err
		}
	}
	return nil
}

// CheckInstalled verifies that rclone is installed and accessible.
func (r *RcloneSync) CheckInstalled(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "rclone", "version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rclone is not installed or not in PATH: %w", err)
	}
	return nil
}

// GetVersion returns the installed rclone version.
func (r *RcloneSync) GetVersion(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "rclone", "version", "--check")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get version: %w", err)
	}

	// Parse version from output
	lines := strings.Split(string(output), "\n")
	if len(lines) > 0 {
		parts := strings.Fields(lines[0])
		if len(parts) >= 2 {
			return parts[1], nil
		}
	}

	return strings.TrimSpace(string(output)), nil
}

// ProviderConfig contains provider-specific configuration.
type ProviderConfig struct {
	Provider    string            `json:"provider"`
	Endpoint    string            `json:"endpoint,omitempty"`
	Region      string            `json:"region,omitempty"`
	AccessKey   string            `json:"access_key,omitempty"`
	SecretKey   string            `json:"secret_key,omitempty"`
	AccountName string            `json:"account_name,omitempty"`
	AccountKey  string            `json:"account_key,omitempty"`
	SASUrl      string            `json:"sas_url,omitempty"`
	ServiceFile string            `json:"service_file,omitempty"`
	Project     string            `json:"project,omitempty"`
	Extra       map[string]string `json:"extra,omitempty"`
}

// ConfigureFromProviderConfig sets up a remote from a ProviderConfig.
func (r *RcloneSync) ConfigureFromProviderConfig(ctx context.Context, remoteName string, config *ProviderConfig) error {
	if config == nil {
		return fmt.Errorf("provider config is required")
	}

	var args []string

	switch config.Provider {
	case "s3", "aws":
		args = []string{"config", "create", remoteName, "s3",
			"provider=AWS",
			fmt.Sprintf("region=%s", config.Region),
		}
		if config.AccessKey != "" {
			args = append(args, fmt.Sprintf("access_key_id=%s", config.AccessKey))
			args = append(args, fmt.Sprintf("secret_access_key=%s", config.SecretKey))
		} else {
			args = append(args, "env_auth=true")
		}

	case "gcs", "google":
		args = []string{"config", "create", remoteName, "gcs",
			"bucket_policy_only=true",
		}
		if config.ServiceFile != "" {
			args = append(args, fmt.Sprintf("service_account_file=%s", config.ServiceFile))
		}
		if config.Project != "" {
			args = append(args, fmt.Sprintf("project_number=%s", config.Project))
		}

	case "azure", "azureblob":
		args = []string{"config", "create", remoteName, "azureblob",
			fmt.Sprintf("account=%s", config.AccountName),
		}
		if config.AccountKey != "" {
			args = append(args, fmt.Sprintf("key=%s", config.AccountKey))
		}
		if config.SASUrl != "" {
			args = append(args, fmt.Sprintf("sas_url=%s", config.SASUrl))
		}

	case "minio":
		args = []string{"config", "create", remoteName, "s3",
			"provider=Minio",
			fmt.Sprintf("endpoint=%s", config.Endpoint),
		}
		if config.AccessKey != "" {
			args = append(args, fmt.Sprintf("access_key_id=%s", config.AccessKey))
			args = append(args, fmt.Sprintf("secret_access_key=%s", config.SecretKey))
		}

	default:
		return fmt.Errorf("unsupported provider: %s", config.Provider)
	}

	// Add extra options
	for k, v := range config.Extra {
		args = append(args, fmt.Sprintf("%s=%s", k, v))
	}

	cmd := exec.CommandContext(ctx, "rclone", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to configure remote: %w", err)
	}

	r.configuredRemotes[remoteName] = true
	return nil
}

// SyncManifest contains information about a sync operation to be performed.
type SyncManifest struct {
	ID          string       `json:"id"`
	Source      *sync.Endpoint `json:"source"`
	Target      *sync.Endpoint `json:"target"`
	Options     *sync.SyncOptions `json:"options"`
	CreatedAt   time.Time    `json:"created_at"`
	ExecutedAt  *time.Time   `json:"executed_at,omitempty"`
	CompletedAt *time.Time   `json:"completed_at,omitempty"`
	Status      string       `json:"status"`
	Error       string       `json:"error,omitempty"`
}

// SaveManifest saves a sync manifest to a file.
func SaveManifest(manifest *SyncManifest, path string) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write manifest: %w", err)
	}

	return nil
}

// LoadManifest loads a sync manifest from a file.
func LoadManifest(path string) (*SyncManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	var manifest SyncManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	return &manifest, nil
}
