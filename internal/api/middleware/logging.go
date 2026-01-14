package middleware

import (
	"net/http"
	"time"

	"github.com/homeport/homeport/internal/pkg/logger"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	bytes       int
}

func wrapResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w}
}

func (rw *responseWriter) WriteHeader(code int) {
	if rw.wroteHeader {
		return
	}
	rw.status = code
	rw.wroteHeader = true
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	n, err := rw.ResponseWriter.Write(b)
	rw.bytes += n
	return n, err
}

func (rw *responseWriter) Status() int {
	if rw.status == 0 {
		return http.StatusOK
	}
	return rw.status
}

// Flush implements http.Flusher for SSE support.
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap returns the underlying ResponseWriter for middleware compatibility.
func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// RequestLogger returns a middleware that logs HTTP requests using structured logging.
func RequestLogger(verbose bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			wrapped := wrapResponseWriter(w)

			defer func() {
				duration := time.Since(start)
				status := wrapped.Status()

				// Skip logging for health checks unless verbose
				if !verbose && r.URL.Path == "/health" {
					return
				}

				// Get request ID from context
				reqID := chimiddleware.GetReqID(r.Context())

				// Build log attributes
				attrs := []any{
					"method", r.Method,
					"path", r.URL.Path,
					"status", status,
					"duration_ms", duration.Milliseconds(),
					"bytes", wrapped.bytes,
					"remote_addr", r.RemoteAddr,
				}

				if reqID != "" {
					attrs = append(attrs, "request_id", reqID)
				}

				if r.URL.RawQuery != "" && verbose {
					attrs = append(attrs, "query", r.URL.RawQuery)
				}

				if r.Header.Get("User-Agent") != "" && verbose {
					attrs = append(attrs, "user_agent", r.Header.Get("User-Agent"))
				}

				// Log based on status code
				msg := "HTTP request"
				if status >= 500 {
					logger.Error(msg, attrs...)
				} else if status >= 400 {
					logger.Warn(msg, attrs...)
				} else if verbose {
					logger.Debug(msg, attrs...)
				} else {
					logger.Info(msg, attrs...)
				}
			}()

			next.ServeHTTP(wrapped, r)
		})
	}
}
