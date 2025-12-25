// Package templates embeds template files for code generation.
package templates

import (
	"embed"
	"fmt"
	"io/fs"
	"text/template"
)

// Note: go:embed does not support .. paths.
// Templates are embedded from templates/embed.go at project root level.
// This package provides utilities for working with embedded templates.

// TemplateFS is set by the templates package at project root.
// It will be nil until InitFS is called with the actual embed.FS.
var TemplateFS embed.FS

// InitFS initializes the template filesystem from an external embed.FS.
// This should be called from templates/embed.go at project root.
func InitFS(fs embed.FS) {
	TemplateFS = fs
}

// Templates provides access to all embedded templates.
type Templates struct {
	fs embed.FS
}

// New creates a new Templates instance.
func New() *Templates {
	return &Templates{
		fs: TemplateFS,
	}
}

// Get retrieves a template by path.
// Path should be like "compose/service.yml.tmpl"
func (t *Templates) Get(path string) (*template.Template, error) {
	content, err := t.fs.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read template %s: %w", path, err)
	}

	tmpl, err := template.New(path).Parse(string(content))
	if err != nil {
		return nil, fmt.Errorf("failed to parse template %s: %w", path, err)
	}

	return tmpl, nil
}

// GetWithFuncs retrieves a template with custom functions.
func (t *Templates) GetWithFuncs(path string, funcs template.FuncMap) (*template.Template, error) {
	content, err := t.fs.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read template %s: %w", path, err)
	}

	tmpl, err := template.New(path).Funcs(funcs).Parse(string(content))
	if err != nil {
		return nil, fmt.Errorf("failed to parse template %s: %w", path, err)
	}

	return tmpl, nil
}

// List returns all available template paths.
func (t *Templates) List() ([]string, error) {
	var paths []string

	err := fs.WalkDir(t.fs, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.IsDir() && (len(path) > 5 && path[len(path)-5:] == ".tmpl") {
			paths = append(paths, path)
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk template directory: %w", err)
	}

	return paths, nil
}

// Exists checks if a template exists.
func (t *Templates) Exists(path string) bool {
	_, err := t.fs.ReadFile(path)
	return err == nil
}

// ReadFile reads a template file as a string.
func (t *Templates) ReadFile(path string) (string, error) {
	content, err := t.fs.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read template %s: %w", path, err)
	}

	return string(content), nil
}

// DefaultFuncs returns default template functions.
func DefaultFuncs() template.FuncMap {
	return template.FuncMap{
		"contains": func(s, substr string) bool {
			return len(s) > 0 && len(substr) > 0 &&
				   s != substr &&
				   (s == substr || len(s) >= len(substr) && contains(s, substr))
		},
		"hasPrefix": func(s, prefix string) bool {
			return len(s) >= len(prefix) && s[:len(prefix)] == prefix
		},
		"hasSuffix": func(s, suffix string) bool {
			return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
		},
	}
}

// Helper function for contains
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
