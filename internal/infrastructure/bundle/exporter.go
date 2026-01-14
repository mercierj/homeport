package bundle

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/homeport/homeport/internal/domain/bundle"
)

// Exporter creates .hprt bundle files from migration results.
type Exporter struct {
	archiver  *Archiver
	collector *Collector

	// HomeportVersion is the current tool version.
	HomeportVersion string
}

// ExportOptions configures bundle export behavior.
type ExportOptions struct {
	// OutputPath is the path for the .hprt file.
	OutputPath string

	// SourceProvider is the cloud provider (aws, gcp, azure).
	SourceProvider string

	// SourceRegion is the cloud region.
	SourceRegion string

	// SourceAccountID is the cloud account/project ID.
	SourceAccountID string

	// ResourceCount is the number of resources being migrated.
	ResourceCount int

	// TargetType is the target deployment type (docker-compose, swarm, k8s).
	TargetType string

	// TargetHost is the target host for deployment (e.g., "192.168.1.100" or "local").
	TargetHost string

	// Domain is the domain name for services (optional, empty for local deployments).
	Domain string

	// Consolidation indicates if stacks were consolidated.
	Consolidation bool

	// DetectSecrets enables secret reference detection.
	DetectSecrets bool
}

// NewExporter creates a new bundle exporter.
func NewExporter(homeportVersion string) *Exporter {
	return &Exporter{
		archiver:        NewArchiver(),
		collector:       NewCollector(""),
		HomeportVersion: homeportVersion,
	}
}

// Export creates a .hprt bundle from a generated output directory.
func (e *Exporter) Export(outputDir string, opts ExportOptions) error {
	// Collect files from generator output
	e.collector.BasePath = outputDir
	b, err := e.collector.CollectFromGenerator(outputDir)
	if err != nil {
		return fmt.Errorf("failed to collect files: %w", err)
	}

	// Configure manifest
	e.configureManifest(b, opts)

	// Compute all checksums
	bundle.ComputeAllChecksums(b)

	// Create the archive
	if err := e.archiver.CreateArchive(b, opts.OutputPath); err != nil {
		return fmt.Errorf("failed to create archive: %w", err)
	}

	return nil
}

// ExportBundle creates a .hprt file from an existing bundle.
func (e *Exporter) ExportBundle(b *bundle.Bundle, outputPath string) error {
	// Ensure manifest is up to date
	b.Manifest.HomeportVersion = e.HomeportVersion
	b.Manifest.Created = time.Now().UTC()

	// Compute all checksums
	bundle.ComputeAllChecksums(b)

	// Create the archive
	if err := e.archiver.CreateArchive(b, outputPath); err != nil {
		return fmt.Errorf("failed to create archive: %w", err)
	}

	return nil
}

// ExportFromFiles creates a bundle from a list of file paths.
func (e *Exporter) ExportFromFiles(files map[string]string, opts ExportOptions) error {
	b := bundle.NewBundle()

	for bundlePath, localPath := range files {
		content, err := os.ReadFile(localPath)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", localPath, err)
		}

		info, err := os.Stat(localPath)
		if err != nil {
			return fmt.Errorf("failed to stat %s: %w", localPath, err)
		}

		checksum := bundle.ComputeChecksum(content)
		b.Files[bundlePath] = &bundle.BundleFile{
			Path:     bundlePath,
			Content:  content,
			Checksum: checksum,
			Mode:     uint32(info.Mode().Perm()),
		}
	}

	e.configureManifest(b, opts)
	bundle.ComputeAllChecksums(b)

	return e.archiver.CreateArchive(b, opts.OutputPath)
}

// configureManifest sets up the manifest with export options.
func (e *Exporter) configureManifest(b *bundle.Bundle, opts ExportOptions) {
	m := b.Manifest
	m.HomeportVersion = e.HomeportVersion
	m.Created = time.Now().UTC()

	m.Source = &bundle.SourceInfo{
		Provider:      opts.SourceProvider,
		Region:        opts.SourceRegion,
		AccountID:     opts.SourceAccountID,
		ResourceCount: opts.ResourceCount,
		AnalyzedAt:    time.Now().UTC(),
	}

	m.Target = &bundle.TargetInfo{
		Type:          opts.TargetType,
		Consolidation: opts.Consolidation,
		StackCount:    e.countStacks(b),
		Host:          opts.TargetHost,
		Domain:        opts.Domain,
	}

	// Count data sync requirements
	m.DataSync = e.detectDataSync(b)

	// Set dependencies
	m.Dependencies = &bundle.Dependencies{
		Docker:        ">=20.10",
		DockerCompose: ">=2.0",
	}

	// Enable rollback
	m.Rollback = &bundle.RollbackInfo{
		Supported:        true,
		SnapshotRequired: m.DataSync != nil && len(m.DataSync.Databases) > 0,
	}
}

// countStacks counts the number of compose files (stacks) in the bundle.
func (e *Exporter) countStacks(b *bundle.Bundle) int {
	count := 0
	for path := range b.Files {
		dir := filepath.Dir(path)
		ext := filepath.Ext(path)
		if dir == "compose" && (ext == ".yml" || ext == ".yaml") {
			count++
		}
	}
	if count == 0 {
		count = 1 // At least one stack
	}
	return count
}

// detectDataSync analyzes the bundle for data sync requirements.
func (e *Exporter) detectDataSync(b *bundle.Bundle) *bundle.DataSyncInfo {
	var databases, storage []string

	for path := range b.Files {
		// Check for database migration scripts
		if filepath.Dir(path) == "migrations" {
			subdir := filepath.Base(filepath.Dir(path))
			if subdir == "postgres" || subdir == "mysql" || subdir == "redis" {
				databases = append(databases, subdir)
			}
		}

		// Check for storage sync scripts
		if filepath.Dir(path) == "data-sync" {
			if filepath.Base(path) == "s3-to-minio.sh" {
				storage = append(storage, "s3")
			}
		}
	}

	if len(databases) == 0 && len(storage) == 0 {
		return nil
	}

	return &bundle.DataSyncInfo{
		Databases: databases,
		Storage:   storage,
	}
}

// ValidateExport checks if an export was successful by reading it back.
func (e *Exporter) ValidateExport(archivePath string) error {
	b, err := e.archiver.ExtractArchive(archivePath)
	if err != nil {
		return fmt.Errorf("failed to read archive: %w", err)
	}

	if err := b.Validate(); err != nil {
		return fmt.Errorf("bundle validation failed: %w", err)
	}

	return nil
}
