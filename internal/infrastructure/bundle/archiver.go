package bundle

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/homeport/homeport/internal/domain/bundle"
)

// Archiver handles tar.gz archive operations for .hprt bundles.
type Archiver struct {
	// CompressionLevel sets gzip compression level (1-9, default 6).
	CompressionLevel int
}

// NewArchiver creates a new archiver with default settings.
func NewArchiver() *Archiver {
	return &Archiver{
		CompressionLevel: gzip.DefaultCompression,
	}
}

// CreateArchive creates a .hprt archive from a bundle.
func (a *Archiver) CreateArchive(b *bundle.Bundle, outputPath string) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create archive file: %w", err)
	}
	defer func() { _ = file.Close() }()

	return a.WriteArchive(b, file)
}

// WriteArchive writes a bundle as a tar.gz archive to a writer.
func (a *Archiver) WriteArchive(b *bundle.Bundle, w io.Writer) error {
	gzWriter, err := gzip.NewWriterLevel(w, a.CompressionLevel)
	if err != nil {
		return fmt.Errorf("failed to create gzip writer: %w", err)
	}
	defer func() { _ = gzWriter.Close() }()

	tarWriter := tar.NewWriter(gzWriter)
	defer func() { _ = tarWriter.Close() }()

	// Write manifest.json first
	manifestData, err := b.Manifest.ToJSON()
	if err != nil {
		return fmt.Errorf("failed to serialize manifest: %w", err)
	}

	if err := a.writeFile(tarWriter, "manifest.json", manifestData, 0644, b.CreatedAt); err != nil {
		return fmt.Errorf("failed to write manifest: %w", err)
	}

	// Write all bundle files
	for path, file := range b.Files {
		mode := file.Mode
		if mode == 0 {
			mode = 0644
		}
		if err := a.writeFile(tarWriter, path, file.Content, mode, b.CreatedAt); err != nil {
			return fmt.Errorf("failed to write %s: %w", path, err)
		}
	}

	return nil
}

// writeFile writes a single file to the tar archive.
func (a *Archiver) writeFile(tw *tar.Writer, name string, content []byte, mode uint32, modTime time.Time) error {
	header := &tar.Header{
		Name:    name,
		Mode:    int64(mode),
		Size:    int64(len(content)),
		ModTime: modTime,
	}

	if err := tw.WriteHeader(header); err != nil {
		return err
	}

	if _, err := tw.Write(content); err != nil {
		return err
	}

	return nil
}

// ExtractArchive extracts a .hprt archive to a bundle.
func (a *Archiver) ExtractArchive(archivePath string) (*bundle.Bundle, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open archive: %w", err)
	}
	defer func() { _ = file.Close() }()

	return a.ReadArchive(file)
}

// ReadArchive reads a tar.gz archive from a reader into a bundle.
func (a *Archiver) ReadArchive(r io.Reader) (*bundle.Bundle, error) {
	gzReader, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer func() { _ = gzReader.Close() }()

	tarReader := tar.NewReader(gzReader)

	b := &bundle.Bundle{
		Files:     make(map[string]*bundle.BundleFile),
		CreatedAt: time.Now().UTC(),
	}

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read tar header: %w", err)
		}

		// Skip directories
		if header.Typeflag == tar.TypeDir {
			continue
		}

		content, err := io.ReadAll(tarReader)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", header.Name, err)
		}

		if header.Name == "manifest.json" {
			manifest, err := bundle.FromJSON(content)
			if err != nil {
				return nil, fmt.Errorf("failed to parse manifest: %w", err)
			}
			b.Manifest = manifest
			b.CreatedAt = manifest.Created
		} else {
			b.Files[header.Name] = &bundle.BundleFile{
				Path:     header.Name,
				Content:  content,
				Checksum: bundle.ComputeChecksum(content),
				Mode:     uint32(header.Mode),
			}
		}
	}

	if b.Manifest == nil {
		return nil, fmt.Errorf("archive missing manifest.json")
	}

	return b, nil
}

// ExtractToDirectory extracts a .hprt archive to a directory.
func (a *Archiver) ExtractToDirectory(archivePath, outputDir string) error {
	b, err := a.ExtractArchive(archivePath)
	if err != nil {
		return err
	}

	// Write manifest
	manifestData, err := b.Manifest.ToJSON()
	if err != nil {
		return fmt.Errorf("failed to serialize manifest: %w", err)
	}

	manifestPath := filepath.Join(outputDir, "manifest.json")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	if err := os.WriteFile(manifestPath, manifestData, 0644); err != nil {
		return fmt.Errorf("failed to write manifest: %w", err)
	}

	// Write all files
	for path, file := range b.Files {
		fullPath := filepath.Join(outputDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", path, err)
		}

		mode := os.FileMode(file.Mode)
		if mode == 0 {
			mode = 0644
		}

		if err := os.WriteFile(fullPath, file.Content, mode); err != nil {
			return fmt.Errorf("failed to write %s: %w", path, err)
		}
	}

	return nil
}
