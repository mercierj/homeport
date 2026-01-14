// Package middleware provides HTTP middleware for the API server.
package middleware

import (
	"net/http"
	"strings"
	"time"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

// TimeoutWithExclusions returns a timeout middleware that skips certain paths.
// SSE/streaming endpoints need to bypass timeout as it wraps ResponseWriter
// and breaks http.Flusher interface.
func TimeoutWithExclusions(d time.Duration, excludePaths ...string) func(http.Handler) http.Handler {
	timeoutHandler := chimiddleware.Timeout(d)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, path := range excludePaths {
				if strings.Contains(r.URL.Path, path) {
					next.ServeHTTP(w, r)
					return
				}
			}
			timeoutHandler(next).ServeHTTP(w, r)
		})
	}
}

// Timeout returns a timeout middleware with configurable duration.
// Use for routes that need different timeouts than the global default.
func Timeout(d time.Duration) func(http.Handler) http.Handler {
	return chimiddleware.Timeout(d)
}

// LongTimeout is a preset for long-running operations (5 minutes).
// Use for database queries, file uploads, and other slow operations.
func LongTimeout() func(http.Handler) http.Handler {
	return Timeout(5 * time.Minute)
}

// ShortTimeout is a preset for quick operations (10 seconds).
// Use for health checks and simple status endpoints.
func ShortTimeout() func(http.Handler) http.Handler {
	return Timeout(10 * time.Second)
}

// NoTimeout bypasses the timeout middleware for SSE and streaming endpoints.
// chi's timeout middleware wraps ResponseWriter and breaks http.Flusher.
func NoTimeout(next http.Handler) http.Handler {
	return next
}
