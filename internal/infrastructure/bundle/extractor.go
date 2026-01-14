package bundle

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/homeport/homeport/internal/domain/bundle"
)

// Extractor handles bundle extraction and file management.
type Extractor struct {
	archiver *Archiver

	// PreservePermissions maintains file permissions from bundle.
	PreservePermissions bool

	// OverwriteExisting allows overwriting existing files.
	OverwriteExisting bool
}

// NewExtractor creates a new bundle extractor.
func NewExtractor() *Extractor {
	return &Extractor{
		archiver:            NewArchiver(),
		PreservePermissions: true,
		OverwriteExisting:   false,
	}
}

// ExtractOptions configures extraction behavior.
type ExtractOptions struct {
	// OutputDir is the directory to extract to.
	OutputDir string

	// Filter is an optional function to filter files.
	Filter func(path string) bool

	// OverwriteExisting allows overwriting existing files.
	OverwriteExisting bool

	// DryRun only lists files without extracting.
	DryRun bool
}

// ExtractResult contains extraction results.
type ExtractResult struct {
	ExtractedFiles []string
	SkippedFiles   []string
	TotalBytes     int64
}

// Extract extracts a bundle archive to a directory.
func (e *Extractor) Extract(archivePath string, opts ExtractOptions) (*ExtractResult, error) {
	b, err := e.archiver.ExtractArchive(archivePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read archive: %w", err)
	}

	return e.ExtractBundle(b, opts)
}

// ExtractBundle extracts a bundle object to a directory.
func (e *Extractor) ExtractBundle(b *bundle.Bundle, opts ExtractOptions) (*ExtractResult, error) {
	result := &ExtractResult{}

	// Create output directory
	if !opts.DryRun {
		if err := os.MkdirAll(opts.OutputDir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create output directory: %w", err)
		}
	}

	// Write manifest first
	manifestData, err := b.Manifest.ToJSON()
	if err != nil {
		return nil, fmt.Errorf("failed to serialize manifest: %w", err)
	}

	manifestPath := filepath.Join(opts.OutputDir, "manifest.json")
	if !opts.DryRun {
		if err := e.writeFile(manifestPath, manifestData, 0644, opts.OverwriteExisting); err != nil {
			return nil, fmt.Errorf("failed to write manifest: %w", err)
		}
	}
	result.ExtractedFiles = append(result.ExtractedFiles, "manifest.json")
	result.TotalBytes += int64(len(manifestData))

	// Extract all files
	for path, file := range b.Files {
		// Apply filter if provided
		if opts.Filter != nil && !opts.Filter(path) {
			result.SkippedFiles = append(result.SkippedFiles, path)
			continue
		}

		fullPath := filepath.Join(opts.OutputDir, path)

		if !opts.DryRun {
			// Create parent directories
			if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
				return nil, fmt.Errorf("failed to create directory for %s: %w", path, err)
			}

			mode := os.FileMode(file.Mode)
			if mode == 0 {
				mode = 0644
			}

			if err := e.writeFile(fullPath, file.Content, mode, opts.OverwriteExisting); err != nil {
				return nil, fmt.Errorf("failed to write %s: %w", path, err)
			}
		}

		result.ExtractedFiles = append(result.ExtractedFiles, path)
		result.TotalBytes += int64(len(file.Content))
	}

	return result, nil
}

// ExtractFile extracts a single file from a bundle archive.
func (e *Extractor) ExtractFile(archivePath, filePath, outputPath string) error {
	b, err := e.archiver.ExtractArchive(archivePath)
	if err != nil {
		return fmt.Errorf("failed to read archive: %w", err)
	}

	file, ok := b.Files[filePath]
	if !ok {
		return fmt.Errorf("file not found in bundle: %s", filePath)
	}

	// Create parent directories
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	mode := os.FileMode(file.Mode)
	if mode == 0 {
		mode = 0644
	}

	return e.writeFile(outputPath, file.Content, mode, e.OverwriteExisting)
}

// ExtractToWriter extracts a single file to a writer.
func (e *Extractor) ExtractToWriter(archivePath, filePath string, w io.Writer) error {
	b, err := e.archiver.ExtractArchive(archivePath)
	if err != nil {
		return fmt.Errorf("failed to read archive: %w", err)
	}

	file, ok := b.Files[filePath]
	if !ok {
		return fmt.Errorf("file not found in bundle: %s", filePath)
	}

	_, err = w.Write(file.Content)
	return err
}

// ListFiles lists all files in a bundle archive.
func (e *Extractor) ListFiles(archivePath string) ([]FileInfo, error) {
	b, err := e.archiver.ExtractArchive(archivePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read archive: %w", err)
	}

	var files []FileInfo
	for path, file := range b.Files {
		files = append(files, FileInfo{
			Path:     path,
			Size:     int64(len(file.Content)),
			Mode:     file.Mode,
			Checksum: file.Checksum,
		})
	}

	return files, nil
}

// FileInfo contains information about a file in the bundle.
type FileInfo struct {
	Path     string
	Size     int64
	Mode     uint32
	Checksum string
}

// writeFile writes content to a file.
func (e *Extractor) writeFile(path string, content []byte, mode os.FileMode, overwrite bool) error {
	// Check if file exists
	if _, err := os.Stat(path); err == nil && !overwrite {
		return fmt.Errorf("file already exists: %s", path)
	}

	return os.WriteFile(path, content, mode)
}

// GetManifest reads only the manifest from a bundle.
func (e *Extractor) GetManifest(archivePath string) (*bundle.Manifest, error) {
	b, err := e.archiver.ExtractArchive(archivePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read archive: %w", err)
	}

	return b.Manifest, nil
}

// VerifyBundle verifies bundle integrity without extracting.
func (e *Extractor) VerifyBundle(archivePath string) error {
	b, err := e.archiver.ExtractArchive(archivePath)
	if err != nil {
		return fmt.Errorf("failed to read archive: %w", err)
	}

	if err := bundle.VerifyChecksums(b); err != nil {
		return fmt.Errorf("checksum verification failed: %w", err)
	}

	return nil
}
