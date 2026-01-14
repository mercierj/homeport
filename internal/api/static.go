package api

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed static/*
var staticFiles embed.FS

// StaticHandler serves embedded static files with SPA fallback.
// Returns an error if the static files cannot be loaded.
func StaticHandler() (http.Handler, error) {
	// Get the static subdirectory from embedded FS
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return nil, fmt.Errorf("failed to get static subdirectory: %w", err)
	}

	fileServer := http.FileServer(http.FS(staticFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Strip leading slash for fs.Open
		fsPath := strings.TrimPrefix(path, "/")
		if fsPath == "" {
			fsPath = "index.html"
		}

		// Try to open the file
		f, err := staticFS.Open(fsPath)
		if err != nil {
			// File not found - serve index.html for SPA routing
			r.URL.Path = "/"
			fileServer.ServeHTTP(w, r)
			return
		}
		f.Close()

		// File exists - serve it
		fileServer.ServeHTTP(w, r)
	}), nil
}
