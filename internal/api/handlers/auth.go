package handlers

import (
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/render"
	"github.com/homeport/homeport/internal/app/auth"
	"github.com/homeport/homeport/internal/pkg/httputil"
)

type AuthHandler struct {
	service   *auth.Service
	tlsEnabled bool
}

func NewAuthHandler(service *auth.Service) *AuthHandler {
	// Detect TLS mode from environment
	tlsEnabled := os.Getenv("TLS_ENABLED") == "true" ||
		os.Getenv("TLS_CERT_FILE") != "" ||
		strings.HasPrefix(os.Getenv("BASE_URL"), "https://")

	return &AuthHandler{
		service:   service,
		tlsEnabled: tlsEnabled,
	}
}

func (h *AuthHandler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	session, err := h.service.Login(req.Username, req.Password)
	if err != nil {
		// Check if account is locked due to brute force protection
		if errors.Is(err, auth.ErrAccountLocked) {
			httputil.AccountLocked(w, r, "Account temporarily locked due to too many failed login attempts. Please try again later.")
			return
		}
		httputil.Unauthorized(w, r, "Invalid credentials")
		return
	}

	// Set session cookie with security hardening
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    session.Token,
		Path:     "/",
		Expires:  session.ExpiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   h.tlsEnabled,
	})

	render.JSON(w, r, map[string]interface{}{
		"token":      session.Token,
		"username":   session.Username,
		"expires_at": session.ExpiresAt,
	})
}

func (h *AuthHandler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	// Get token from cookie or header
	token := ""
	if cookie, err := r.Cookie("session"); err == nil {
		token = cookie.Value
	}

	if token != "" {
		h.service.Logout(token)
	}

	// Clear cookie with consistent security settings
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   h.tlsEnabled,
	})

	render.JSON(w, r, map[string]string{"status": "logged out"})
}

func (h *AuthHandler) HandleMe(w http.ResponseWriter, r *http.Request) {
	// Get token
	token := ""
	if cookie, err := r.Cookie("session"); err == nil {
		token = cookie.Value
	}

	if token == "" {
		httputil.Unauthorized(w, r, "Not authenticated")
		return
	}

	session, err := h.service.ValidateSession(token)
	if err != nil {
		httputil.Unauthorized(w, r, "Not authenticated")
		return
	}

	render.JSON(w, r, map[string]interface{}{
		"username":   session.Username,
		"expires_at": session.ExpiresAt,
	})
}

func (h *AuthHandler) HandleChangePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	// Get current user from session
	token := ""
	if cookie, err := r.Cookie("session"); err == nil {
		token = cookie.Value
	}

	session, err := h.service.ValidateSession(token)
	if err != nil {
		httputil.Unauthorized(w, r, "Not authenticated")
		return
	}

	if err := h.service.ChangePassword(session.Username, req.OldPassword, req.NewPassword); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	render.JSON(w, r, map[string]string{"status": "password changed"})
}
