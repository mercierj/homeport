// Package templates provides embedded template files for Homeport code generation.
package templates

import "embed"

// FS embeds all template files from this directory.
//
//go:embed compose/*.tmpl traefik/*.tmpl scripts/*.tmpl docs/*.tmpl
var FS embed.FS
