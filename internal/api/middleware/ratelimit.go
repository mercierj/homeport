// Package middleware provides HTTP middleware for the API server.
package middleware

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/homeport/homeport/internal/pkg/httputil"
	"golang.org/x/time/rate"
)

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

var (
	visitors      sync.Map
	cleanupOnce   sync.Once
	cleanupCancel context.CancelFunc
	cleanupMu     sync.Mutex
)

// RateLimit returns a middleware that limits requests per IP address.
// rps is requests per second, burst is the maximum burst size.
func RateLimit(rps float64, burst int) func(http.Handler) http.Handler {
	// Start cleanup goroutine once
	cleanupOnce.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		cleanupMu.Lock()
		cleanupCancel = cancel
		cleanupMu.Unlock()
		go cleanupVisitors(ctx)
	})

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := getClientIP(r)
			limiter := getVisitor(ip, rps, burst)

			if !limiter.Allow() {
				w.Header().Set("Retry-After", "60")
				httputil.WriteError(w, r, http.StatusTooManyRequests,
					"RATE_LIMITED", "Too many requests", "Please wait before retrying")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// StopRateLimitCleanup stops the background cleanup goroutine.
// This should be called during server shutdown.
func StopRateLimitCleanup() {
	cleanupMu.Lock()
	defer cleanupMu.Unlock()
	if cleanupCancel != nil {
		cleanupCancel()
		cleanupCancel = nil
	}
}

// getClientIP extracts the real client IP from proxy headers or RemoteAddr.
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first (can contain multiple IPs)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			clientIP := strings.TrimSpace(ips[0])
			if clientIP != "" {
				return clientIP
			}
		}
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// Fall back to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

func getVisitor(ip string, rps float64, burst int) *rate.Limiter {
	now := time.Now()
	newVisitor := &visitor{
		limiter:  rate.NewLimiter(rate.Limit(rps), burst),
		lastSeen: now,
	}

	// Use LoadOrStore to avoid race condition between Load and Store
	actual, loaded := visitors.LoadOrStore(ip, newVisitor)
	vis := actual.(*visitor)

	if loaded {
		// Visitor already existed, update lastSeen atomically
		vis.lastSeen = now
	}

	return vis.limiter
}

// cleanupVisitors removes stale visitor entries every 10 minutes.
func cleanupVisitors(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			threshold := time.Now().Add(-10 * time.Minute)
			visitors.Range(func(key, value interface{}) bool {
				v := value.(*visitor)
				if v.lastSeen.Before(threshold) {
					visitors.Delete(key)
				}
				return true
			})
		}
	}
}
