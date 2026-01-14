package bundle

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/homeport/homeport/internal/domain/bundle"
)

// Collector collects files from a directory structure into a bundle.
type Collector struct {
	// BasePath is the root directory to collect from.
	BasePath string

	// IncludePatterns are glob patterns for files to include.
	IncludePatterns []string

	// ExcludePatterns are glob patterns for files to exclude.
	ExcludePatterns []string
}

// NewCollector creates a new collector for the given base path.
func NewCollector(basePath string) *Collector {
	return &Collector{
		BasePath: basePath,
		IncludePatterns: []string{
			"compose/*.yml",
			"compose/*.yaml",
			"configs/**/*",
			"scripts/*.sh",
			"migrations/**/*",
			"data-sync/*",
			"secrets/.env.template",
			"secrets/secrets-manifest.json",
			"secrets/README.md",
			"dns/*.json",
			"validation/*.json",
			"README.md",
		},
		ExcludePatterns: []string{
			"**/.git/**",
			"**/.DS_Store",
			"**/*.tmp",
			"**/node_modules/**",
		},
	}
}

// Collect collects all matching files into a bundle.
func (c *Collector) Collect() (*bundle.Bundle, error) {
	b := bundle.NewBundle()

	err := filepath.Walk(c.BasePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(c.BasePath, path)
		if err != nil {
			return err
		}

		// Normalize path separators
		relPath = filepath.ToSlash(relPath)

		// Check exclusions first
		if c.shouldExclude(relPath) {
			return nil
		}

		// Check inclusions
		if !c.shouldInclude(relPath) {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", path, err)
		}

		mode := uint32(info.Mode().Perm())
		checksum := bundle.ComputeChecksum(content)

		b.Files[relPath] = &bundle.BundleFile{
			Path:     relPath,
			Content:  content,
			Checksum: checksum,
			Mode:     mode,
		}

		b.Manifest.Checksums[relPath] = checksum

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to collect files: %w", err)
	}

	return b, nil
}

// CollectFromGenerator collects files from generator output directories.
func (c *Collector) CollectFromGenerator(outputDir string) (*bundle.Bundle, error) {
	b := bundle.NewBundle()

	// Standard bundle directories
	dirs := map[string][]string{
		"compose":    {".yml", ".yaml"},
		"configs":    {"*"},
		"scripts":    {".sh"},
		"migrations": {"*"},
		"data-sync":  {"*"},
		"dns":        {".json"},
		"validation": {".json"},
	}

	for dir, exts := range dirs {
		dirPath := filepath.Join(outputDir, dir)
		if _, err := os.Stat(dirPath); os.IsNotExist(err) {
			continue
		}

		err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}

			if !c.matchesExtensions(path, exts) {
				return nil
			}

			relPath, _ := filepath.Rel(outputDir, path)
			relPath = filepath.ToSlash(relPath)

			content, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("failed to read %s: %w", path, err)
			}

			mode := uint32(info.Mode().Perm())
			checksum := bundle.ComputeChecksum(content)

			b.Files[relPath] = &bundle.BundleFile{
				Path:     relPath,
				Content:  content,
				Checksum: checksum,
				Mode:     mode,
			}
			b.Manifest.Checksums[relPath] = checksum

			return nil
		})

		if err != nil {
			return nil, err
		}
	}

	// Add secrets template (never actual secrets)
	c.addSecretsTemplate(b, outputDir)

	// Add README if exists
	readmePath := filepath.Join(outputDir, "README.md")
	if content, err := os.ReadFile(readmePath); err == nil {
		checksum := bundle.ComputeChecksum(content)
		b.Files["README.md"] = &bundle.BundleFile{
			Path:     "README.md",
			Content:  content,
			Checksum: checksum,
			Mode:     0644,
		}
		b.Manifest.Checksums["README.md"] = checksum
	}

	return b, nil
}

// AddFile adds a single file to an existing bundle.
func (c *Collector) AddFile(b *bundle.Bundle, path string, content []byte) error {
	if path == "" {
		return bundle.ErrEmptyPath
	}

	checksum := bundle.ComputeChecksum(content)
	mode := uint32(0644)

	// Executable scripts
	if strings.HasSuffix(path, ".sh") {
		mode = 0755
	}

	b.Files[path] = &bundle.BundleFile{
		Path:     path,
		Content:  content,
		Checksum: checksum,
		Mode:     mode,
	}
	b.Manifest.Checksums[path] = checksum

	return nil
}

// shouldInclude checks if a path matches any include pattern.
func (c *Collector) shouldInclude(path string) bool {
	if len(c.IncludePatterns) == 0 {
		return true
	}

	for _, pattern := range c.IncludePatterns {
		if matched, _ := filepath.Match(pattern, path); matched {
			return true
		}
		// Handle ** patterns
		if strings.Contains(pattern, "**") {
			simplePattern := strings.ReplaceAll(pattern, "**", "*")
			if matched, _ := filepath.Match(simplePattern, path); matched {
				return true
			}
		}
	}

	return false
}

// shouldExclude checks if a path matches any exclude pattern.
func (c *Collector) shouldExclude(path string) bool {
	for _, pattern := range c.ExcludePatterns {
		if matched, _ := filepath.Match(pattern, path); matched {
			return true
		}
		// Handle ** patterns
		if strings.Contains(pattern, "**") {
			parts := strings.Split(pattern, "**")
			if len(parts) == 2 {
				if strings.HasPrefix(path, strings.TrimSuffix(parts[0], "/")) &&
					strings.HasSuffix(path, strings.TrimPrefix(parts[1], "/")) {
					return true
				}
			}
		}
	}
	return false
}

// matchesExtensions checks if a file matches the given extensions.
func (c *Collector) matchesExtensions(path string, exts []string) bool {
	for _, ext := range exts {
		if ext == "*" {
			return true
		}
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}

// addSecretsTemplate adds the secrets template files to the bundle.
func (c *Collector) addSecretsTemplate(b *bundle.Bundle, outputDir string) {
	secretsDir := filepath.Join(outputDir, "secrets")
	if _, err := os.Stat(secretsDir); os.IsNotExist(err) {
		return
	}

	// Only include template and manifest, never actual secrets
	safeFiles := []string{".env.template", "secrets-manifest.json", "README.md"}

	for _, filename := range safeFiles {
		path := filepath.Join(secretsDir, filename)
		if content, err := os.ReadFile(path); err == nil {
			relPath := filepath.Join("secrets", filename)
			relPath = filepath.ToSlash(relPath)
			checksum := bundle.ComputeChecksum(content)

			b.Files[relPath] = &bundle.BundleFile{
				Path:     relPath,
				Content:  content,
				Checksum: checksum,
				Mode:     0644,
			}
			b.Manifest.Checksums[relPath] = checksum
		}
	}
}
