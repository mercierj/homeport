// Package templates provides embedded template files for CloudExit code generation.
package templates

import (
	"embed"

	infratemplates "github.com/cloudexit/cloudexit/internal/infrastructure/templates"
)

// FS embeds all template files from this directory.
//
//go:embed compose/*.tmpl traefik/*.tmpl scripts/*.tmpl docs/*.tmpl
var FS embed.FS

func init() {
	// Initialize the infrastructure templates package with our embedded FS.
	infratemplates.InitFS(FS)
}
