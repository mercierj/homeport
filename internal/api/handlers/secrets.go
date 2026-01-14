package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"

	"github.com/homeport/homeport/internal/app/secrets"
	"github.com/homeport/homeport/internal/pkg/httputil"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

const (
	MaxSecretNameLength       = 256
	MaxSecretValueLength      = 64 * 1024 // 64KB
	MaxSecretDescriptionLength = 1024
)

var (
	secretNameRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_\-]*$`)
)

func validateSecretName(name string) error {
	if name == "" {
		return fmt.Errorf("secret name is required")
	}
	if len(name) > MaxSecretNameLength {
		return fmt.Errorf("secret name must be at most %d characters", MaxSecretNameLength)
	}
	if !secretNameRegex.MatchString(name) {
		return fmt.Errorf("secret name must start with a letter and contain only letters, digits, hyphens, and underscores")
	}
	return nil
}

func validateSecretValue(value string) error {
	if value == "" {
		return fmt.Errorf("secret value is required")
	}
	if len(value) > MaxSecretValueLength {
		return fmt.Errorf("secret value must be at most %d bytes", MaxSecretValueLength)
	}
	return nil
}

// SecretsHandler handles secret-related HTTP requests.
type SecretsHandler struct {
	service *secrets.Service
}

// NewSecretsHandler creates a new secrets handler.
func NewSecretsHandler(cfg secrets.Config) (*SecretsHandler, error) {
	svc, err := secrets.NewService(cfg)
	if err != nil {
		return nil, err
	}
	return &SecretsHandler{service: svc}, nil
}

// HandleListSecrets handles GET /stacks/{stackID}/secrets
func (h *SecretsHandler) HandleListSecrets(w http.ResponseWriter, r *http.Request) {
	stackID := chi.URLParam(r, "stackID")
	if err := validateStackID(stackID); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	secretsList, err := h.service.ListSecrets(r.Context(), stackID)
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]interface{}{
		"secrets": secretsList,
		"count":   len(secretsList),
	})
}

// HandleGetSecret handles GET /stacks/{stackID}/secrets/{name}
func (h *SecretsHandler) HandleGetSecret(w http.ResponseWriter, r *http.Request) {
	stackID := chi.URLParam(r, "stackID")
	name := chi.URLParam(r, "name")

	if err := validateStackID(stackID); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	decodedName, err := url.PathUnescape(name)
	if err != nil {
		httputil.BadRequest(w, r, "invalid secret name encoding")
		return
	}

	if err := validateSecretName(decodedName); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	secret, err := h.service.GetSecret(r.Context(), stackID, decodedName)
	if err != nil {
		httputil.NotFound(w, r, err.Error())
		return
	}

	render.JSON(w, r, secret)
}

// HandleCreateSecret handles POST /stacks/{stackID}/secrets
func (h *SecretsHandler) HandleCreateSecret(w http.ResponseWriter, r *http.Request) {
	stackID := chi.URLParam(r, "stackID")
	if err := validateStackID(stackID); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	var req secrets.CreateSecretRequest
	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	if err := validateSecretName(req.Name); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}
	if err := validateSecretValue(req.Value); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}
	if len(req.Description) > MaxSecretDescriptionLength {
		httputil.BadRequest(w, r, fmt.Sprintf("description must be at most %d characters", MaxSecretDescriptionLength))
		return
	}

	metadata, err := h.service.CreateSecret(r.Context(), stackID, req)
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	render.JSON(w, r, metadata)
}

// HandleUpdateSecret handles PUT /stacks/{stackID}/secrets/{name}
func (h *SecretsHandler) HandleUpdateSecret(w http.ResponseWriter, r *http.Request) {
	stackID := chi.URLParam(r, "stackID")
	name := chi.URLParam(r, "name")

	if err := validateStackID(stackID); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	decodedName, err := url.PathUnescape(name)
	if err != nil {
		httputil.BadRequest(w, r, "invalid secret name encoding")
		return
	}

	if err := validateSecretName(decodedName); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	var req struct {
		Value string `json:"value"`
	}
	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	if err := validateSecretValue(req.Value); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	metadata, err := h.service.UpdateSecret(r.Context(), stackID, decodedName, req.Value)
	if err != nil {
		httputil.NotFound(w, r, err.Error())
		return
	}

	render.JSON(w, r, metadata)
}

// HandleDeleteSecret handles DELETE /stacks/{stackID}/secrets/{name}
func (h *SecretsHandler) HandleDeleteSecret(w http.ResponseWriter, r *http.Request) {
	stackID := chi.URLParam(r, "stackID")
	name := chi.URLParam(r, "name")

	if err := validateStackID(stackID); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	decodedName, err := url.PathUnescape(name)
	if err != nil {
		httputil.BadRequest(w, r, "invalid secret name encoding")
		return
	}

	if err := validateSecretName(decodedName); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	if err := h.service.DeleteSecret(r.Context(), stackID, decodedName); err != nil {
		httputil.NotFound(w, r, err.Error())
		return
	}

	render.JSON(w, r, map[string]string{"status": "deleted", "name": decodedName})
}

// HandleGetSecretMetadata handles GET /stacks/{stackID}/secrets/{name}/metadata
func (h *SecretsHandler) HandleGetSecretMetadata(w http.ResponseWriter, r *http.Request) {
	stackID := chi.URLParam(r, "stackID")
	name := chi.URLParam(r, "name")

	if err := validateStackID(stackID); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	decodedName, err := url.PathUnescape(name)
	if err != nil {
		httputil.BadRequest(w, r, "invalid secret name encoding")
		return
	}

	if err := validateSecretName(decodedName); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	metadata, err := h.service.GetSecretMetadata(r.Context(), stackID, decodedName)
	if err != nil {
		httputil.NotFound(w, r, err.Error())
		return
	}

	render.JSON(w, r, metadata)
}

// HandleListVersions handles GET /stacks/{stackID}/secrets/{name}/versions
func (h *SecretsHandler) HandleListVersions(w http.ResponseWriter, r *http.Request) {
	stackID := chi.URLParam(r, "stackID")
	name := chi.URLParam(r, "name")

	if err := validateStackID(stackID); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	decodedName, err := url.PathUnescape(name)
	if err != nil {
		httputil.BadRequest(w, r, "invalid secret name encoding")
		return
	}

	if err := validateSecretName(decodedName); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	versions, err := h.service.ListVersions(r.Context(), stackID, decodedName)
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]interface{}{
		"versions": versions,
		"count":    len(versions),
	})
}
