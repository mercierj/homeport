package handlers

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/homeport/homeport/internal/app/certificates"
	"github.com/homeport/homeport/internal/pkg/httputil"
	"golang.org/x/crypto/acme"
)

const (
	// MaxDomainLength is the maximum length of a domain name
	MaxDomainLength = 253
)

var (
	// domainRegex validates domain names (basic validation)
	// Allows alphanumeric, hyphens, and periods
	domainRegex = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?)*$`)
)

// validateDomain checks if a domain name is valid
func validateDomain(domain string) error {
	if domain == "" {
		return fmt.Errorf("domain is required")
	}
	if len(domain) > MaxDomainLength {
		return fmt.Errorf("domain must be at most %d characters", MaxDomainLength)
	}
	if !domainRegex.MatchString(domain) {
		return fmt.Errorf("invalid domain format")
	}
	// Additional security check: no path traversal attempts
	if strings.Contains(domain, "..") || strings.Contains(domain, "/") || strings.Contains(domain, "\\") {
		return fmt.Errorf("invalid domain format")
	}
	return nil
}

// CertificatesHandler handles certificate management HTTP requests.
type CertificatesHandler struct {
	service *certificates.Service
}

// CertificatesConfig holds configuration for the certificates handler.
type CertificatesConfig struct {
	Email         string
	DataDir       string
	UseStaging    bool
	AcceptTOS     bool
}

// NewCertificatesHandler creates a new certificates handler.
func NewCertificatesHandler(cfg CertificatesConfig) (*CertificatesHandler, error) {
	dataDir := cfg.DataDir
	if dataDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		dataDir = filepath.Join(homeDir, ".homeport", "certificates")
	}

	acmeDir := acme.LetsEncryptURL
	if cfg.UseStaging {
		// Let's Encrypt staging environment for testing
		acmeDir = "https://acme-staging-v02.api.letsencrypt.org/directory"
	}

	svc, err := certificates.NewService(certificates.Config{
		DataDir:       dataDir,
		Email:         cfg.Email,
		ACMEDirectory: acmeDir,
		AcceptTOS:     cfg.AcceptTOS,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create certificate service: %w", err)
	}

	return &CertificatesHandler{service: svc}, nil
}

// HandleListCertificates handles GET /certificates
func (h *CertificatesHandler) HandleListCertificates(w http.ResponseWriter, r *http.Request) {
	certs, err := h.service.ListCertificates(r.Context())
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]interface{}{
		"certificates": certs,
		"count":        len(certs),
	})
}

// HandleGetCertificate handles GET /certificates/{domain}
func (h *CertificatesHandler) HandleGetCertificate(w http.ResponseWriter, r *http.Request) {
	domain := chi.URLParam(r, "domain")

	if err := validateDomain(domain); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	cert, err := h.service.GetCertificate(r.Context(), domain)
	if err != nil {
		httputil.NotFound(w, r, "Certificate not found")
		return
	}

	render.JSON(w, r, cert)
}

// CertificateRequest represents the request body for creating a certificate.
type CertificateRequestBody struct {
	Domain    string   `json:"domain"`
	SANs      []string `json:"sans,omitempty"`
	AutoRenew bool     `json:"auto_renew"`
}

// HandleRequestCertificate handles POST /certificates
func (h *CertificatesHandler) HandleRequestCertificate(w http.ResponseWriter, r *http.Request) {
	var req CertificateRequestBody
	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	// Validate main domain
	if err := validateDomain(req.Domain); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	// Validate SANs
	for _, san := range req.SANs {
		if err := validateDomain(san); err != nil {
			httputil.BadRequest(w, r, fmt.Sprintf("invalid SAN '%s': %s", san, err.Error()))
			return
		}
	}

	cert, err := h.service.RequestCertificate(r.Context(), certificates.CertificateRequest{
		Domain:    req.Domain,
		SANs:      req.SANs,
		AutoRenew: req.AutoRenew,
	})
	if err != nil {
		httputil.InternalErrorWithMessage(w, r, "Failed to request certificate", err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	render.JSON(w, r, cert)
}

// HandleRenewCertificate handles POST /certificates/{domain}/renew
func (h *CertificatesHandler) HandleRenewCertificate(w http.ResponseWriter, r *http.Request) {
	domain := chi.URLParam(r, "domain")

	if err := validateDomain(domain); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	cert, err := h.service.RenewCertificate(r.Context(), domain)
	if err != nil {
		httputil.InternalErrorWithMessage(w, r, "Failed to renew certificate", err)
		return
	}

	render.JSON(w, r, cert)
}

// HandleDeleteCertificate handles DELETE /certificates/{domain}
func (h *CertificatesHandler) HandleDeleteCertificate(w http.ResponseWriter, r *http.Request) {
	domain := chi.URLParam(r, "domain")

	if err := validateDomain(domain); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	if err := h.service.DeleteCertificate(r.Context(), domain); err != nil {
		httputil.InternalErrorWithMessage(w, r, "Failed to delete certificate", err)
		return
	}

	render.JSON(w, r, map[string]string{
		"status": "deleted",
		"domain": domain,
	})
}

// HandleACMEChallenge handles GET /.well-known/acme-challenge/{token}
// This endpoint must be publicly accessible for ACME HTTP-01 validation.
func (h *CertificatesHandler) HandleACMEChallenge(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")

	// Validate token format (alphanumeric, hyphens, underscores)
	if !regexp.MustCompile(`^[a-zA-Z0-9_-]+$`).MatchString(token) {
		httputil.BadRequest(w, r, "invalid token format")
		return
	}

	response, ok := h.service.GetChallengeResponse(token)
	if !ok {
		httputil.NotFound(w, r, "Challenge not found")
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte(response))
}

// HandleGetChallenges handles GET /certificates/challenges
func (h *CertificatesHandler) HandleGetChallenges(w http.ResponseWriter, r *http.Request) {
	challenges := h.service.GetPendingChallenges()

	render.JSON(w, r, map[string]interface{}{
		"challenges": challenges,
		"count":      len(challenges),
	})
}

// HandleAutoRenew handles POST /certificates/auto-renew
func (h *CertificatesHandler) HandleAutoRenew(w http.ResponseWriter, r *http.Request) {
	renewed, err := h.service.AutoRenewCertificates(r.Context())
	if err != nil {
		httputil.InternalErrorWithMessage(w, r, "Failed to auto-renew certificates", err)
		return
	}

	render.JSON(w, r, map[string]interface{}{
		"renewed": renewed,
		"count":   len(renewed),
	})
}

// HandleGetExpiringCertificates handles GET /certificates/expiring
func (h *CertificatesHandler) HandleGetExpiringCertificates(w http.ResponseWriter, r *http.Request) {
	certs, err := h.service.GetCertificatesNeedingRenewal(r.Context())
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]interface{}{
		"certificates": certs,
		"count":        len(certs),
	})
}
