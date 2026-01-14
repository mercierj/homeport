package middleware

import (
	"context"
	"net/http"
	"os"
	"strings"

	"github.com/homeport/homeport/internal/app/auth"
	"github.com/homeport/homeport/internal/pkg/httputil"
	"github.com/homeport/homeport/internal/pkg/logger"
)

type contextKey string

const (
	SessionContextKey contextKey = "session"
)

// IsProductionEnvironment checks if running in production.
func IsProductionEnvironment() bool {
	for _, env := range []string{"AGNOSTECH_ENV", "GO_ENV", "NODE_ENV"} {
		if strings.EqualFold(os.Getenv(env), "production") {
			return true
		}
	}
	return false
}

func AuthMiddleware(authService *auth.Service, noAuth bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// SECURITY: Block --no-auth in production
			if noAuth && IsProductionEnvironment() {
				logger.Error("SECURITY: --no-auth blocked in production")
				httputil.Forbidden(w, r, "Authentication bypass disabled in production")
				return
			}

			// Skip auth if disabled (dev mode only)
			if noAuth {
				next.ServeHTTP(w, r)
				return
			}

			// Get token from Authorization header or cookie
			token := ""
			if authHeader := r.Header.Get("Authorization"); authHeader != "" {
				if strings.HasPrefix(authHeader, "Bearer ") {
					token = strings.TrimPrefix(authHeader, "Bearer ")
				}
			} else if cookie, err := r.Cookie("session"); err == nil {
				token = cookie.Value
			}

			if token == "" {
				httputil.Unauthorized(w, r, "")
				return
			}

			session, err := authService.ValidateSession(token)
			if err != nil {
				httputil.Unauthorized(w, r, "")
				return
			}

			// Add session to context
			ctx := context.WithValue(r.Context(), SessionContextKey, session)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func GetSession(r *http.Request) *auth.Session {
	if session, ok := r.Context().Value(SessionContextKey).(*auth.Session); ok {
		return session
	}
	return nil
}
