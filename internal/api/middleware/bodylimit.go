package middleware

import (
	"net/http"

	"github.com/homeport/homeport/internal/pkg/httputil"
)

// DefaultMaxBodySize is the default maximum request body size (1MB).
const DefaultMaxBodySize = 1 << 20

// BodyLimit returns a middleware that limits the request body size.
// If maxBytes is 0, the default of 1MB is used.
func BodyLimit(maxBytes int64) func(http.Handler) http.Handler {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBodySize
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only limit POST, PUT, PATCH requests
			if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
				// Check Content-Length header first for early rejection
				if r.ContentLength > maxBytes {
					httputil.RequestTooLarge(w, r, maxBytes)
					return
				}

				// Wrap the body with a limited reader
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}

			next.ServeHTTP(w, r)
		})
	}
}

// BodyLimitForUpload returns a middleware with a higher limit for upload endpoints.
// Default is 32MB.
func BodyLimitForUpload(maxBytes int64) func(http.Handler) http.Handler {
	if maxBytes <= 0 {
		maxBytes = 32 << 20 // 32MB default for uploads
	}
	return BodyLimit(maxBytes)
}
