// Package bundle defines domain types for HPRT bundle format.
// A bundle is a self-contained migration package that contains all configuration,
// scripts, and metadata needed to migrate cloud infrastructure to self-hosted Docker.
//
// SECURITY: Bundles NEVER contain secret values - only references.
// Secrets are resolved at import time via prompts, env vars, or secret managers.
package bundle

import (
	"errors"
	"fmt"
	"time"
)

// Bundle represents a complete migration bundle (.hprt file).
// It contains all files, configurations, and metadata needed for migration.
type Bundle struct {
	// Manifest contains bundle metadata, checksums, and deployment info.
	Manifest *Manifest

	// Files maps relative paths to bundle file contents.
	// Example: "compose/docker-compose.yml" -> BundleFile
	Files map[string]*BundleFile

	// CreatedAt is when this bundle was created.
	CreatedAt time.Time
}

// BundleFile represents a single file within the bundle.
type BundleFile struct {
	// Path is the relative path within the bundle.
	Path string

	// Content is the raw file content.
	Content []byte

	// Checksum is the SHA-256 checksum of the content.
	Checksum string

	// Mode is the file permission mode (e.g., 0644).
	Mode uint32
}

// BundleMetadata contains high-level bundle information.
type BundleMetadata struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Version     string   `json:"version"`
	Author      string   `json:"author"`
	Tags        []string `json:"tags,omitempty"`
}

// NewBundle creates a new empty bundle with the current timestamp.
func NewBundle() *Bundle {
	return &Bundle{
		Manifest:  NewManifest(),
		Files:     make(map[string]*BundleFile),
		CreatedAt: time.Now().UTC(),
	}
}

// AddFile adds a file to the bundle.
// The checksum is computed automatically if not provided.
func (b *Bundle) AddFile(path string, content []byte, checksum string) error {
	if path == "" {
		return ErrEmptyPath
	}
	if content == nil {
		content = []byte{}
	}

	if checksum == "" {
		checksum = ComputeChecksum(content)
	}

	b.Files[path] = &BundleFile{
		Path:     path,
		Content:  content,
		Checksum: checksum,
		Mode:     0644,
	}

	if b.Manifest != nil {
		if b.Manifest.Checksums == nil {
			b.Manifest.Checksums = make(map[string]string)
		}
		b.Manifest.Checksums[path] = checksum
	}

	return nil
}

// AddExecutableFile adds an executable file to the bundle (mode 0755).
func (b *Bundle) AddExecutableFile(path string, content []byte) error {
	if err := b.AddFile(path, content, ""); err != nil {
		return err
	}
	if file, ok := b.Files[path]; ok {
		file.Mode = 0755
	}
	return nil
}

// GetFile retrieves a file from the bundle by path.
func (b *Bundle) GetFile(path string) (*BundleFile, bool) {
	file, ok := b.Files[path]
	return file, ok
}

// HasFile checks if a file exists in the bundle.
func (b *Bundle) HasFile(path string) bool {
	_, ok := b.Files[path]
	return ok
}

// ListFiles returns all file paths in the bundle.
func (b *Bundle) ListFiles() []string {
	paths := make([]string, 0, len(b.Files))
	for path := range b.Files {
		paths = append(paths, path)
	}
	return paths
}

// FileCount returns the number of files in the bundle.
func (b *Bundle) FileCount() int {
	return len(b.Files)
}

// TotalSize returns the total size of all files in bytes.
func (b *Bundle) TotalSize() int64 {
	var total int64
	for _, file := range b.Files {
		total += int64(len(file.Content))
	}
	return total
}

// Validate checks if the bundle is valid and complete.
func (b *Bundle) Validate() error {
	if b.Manifest == nil {
		return ErrMissingManifest
	}

	if err := b.Manifest.Validate(); err != nil {
		return fmt.Errorf("manifest validation failed: %w", err)
	}

	if err := VerifyChecksums(b); err != nil {
		return fmt.Errorf("checksum verification failed: %w", err)
	}

	if !b.HasFile("compose/docker-compose.yml") {
		return ErrMissingComposeFile
	}

	return nil
}

// Common bundle errors.
var (
	ErrEmptyPath          = errors.New("file path cannot be empty")
	ErrMissingManifest    = errors.New("bundle missing manifest")
	ErrMissingComposeFile = errors.New("bundle missing compose/docker-compose.yml")
	ErrInvalidChecksum    = errors.New("file checksum mismatch")
	ErrChecksumNotFound   = errors.New("checksum not found in manifest")
	ErrInvalidVersion     = errors.New("invalid version format")
	ErrIncompatibleVersion = errors.New("incompatible bundle version")
)

// BundleError represents a bundle-specific error with context.
type BundleError struct {
	Op   string
	Path string
	Err  error
}

func (e *BundleError) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("bundle %s failed for %s: %v", e.Op, e.Path, e.Err)
	}
	return fmt.Sprintf("bundle %s failed: %v", e.Op, e.Err)
}

func (e *BundleError) Unwrap() error {
	return e.Err
}
