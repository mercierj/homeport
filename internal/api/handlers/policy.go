package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"

	appPolicy "github.com/homeport/homeport/internal/app/policy"
	"github.com/homeport/homeport/internal/domain/policy"
)

// PolicyHandler handles policy API requests.
type PolicyHandler struct {
	service *appPolicy.Service
}

// NewPolicyHandler creates a new policy handler.
func NewPolicyHandler(cfg *appPolicy.Config) (*PolicyHandler, error) {
	svc, err := appPolicy.NewService(cfg)
	if err != nil {
		return nil, err
	}

	return &PolicyHandler{
		service: svc,
	}, nil
}

// RegisterRoutes registers policy routes on the router.
func (h *PolicyHandler) RegisterRoutes(r chi.Router) {
	r.Route("/policies", func(r chi.Router) {
		r.Get("/", h.HandleListPolicies)
		r.Get("/summary", h.HandleGetSummary)
		r.Post("/", h.HandleCreatePolicy)
		r.Post("/import", h.HandleImportPolicies)

		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", h.HandleGetPolicy)
			r.Put("/", h.HandleUpdatePolicy)
			r.Delete("/", h.HandleDeletePolicy)
			r.Post("/validate", h.HandleValidatePolicy)
			r.Get("/keycloak-preview", h.HandleKeycloakPreview)
			r.Post("/keycloak-regenerate", h.HandleKeycloakRegenerate)
			r.Get("/original", h.HandleExportOriginal)
		})
	})
}

// ListPoliciesRequest contains filters for listing policies.
type ListPoliciesRequest struct {
	Types        []string `json:"types,omitempty"`
	Providers    []string `json:"providers,omitempty"`
	ResourceType string   `json:"resource_type,omitempty"`
	Search       string   `json:"search,omitempty"`
	HasWarnings  *bool    `json:"has_warnings,omitempty"`
}

// PolicyResponse wraps a policy for API response.
type PolicyResponse struct {
	*policy.Policy
}

// HandleListPolicies returns all policies with optional filtering.
func (h *PolicyHandler) HandleListPolicies(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Build filter from query params
	filter := &policy.PolicyFilter{}

	if types := r.URL.Query()["type"]; len(types) > 0 {
		for _, t := range types {
			filter.Types = append(filter.Types, policy.PolicyType(t))
		}
	}

	if providers := r.URL.Query()["provider"]; len(providers) > 0 {
		for _, p := range providers {
			filter.Providers = append(filter.Providers, policy.Provider(p))
		}
	}

	if resourceType := r.URL.Query().Get("resource_type"); resourceType != "" {
		filter.ResourceType = resourceType
	}

	if search := r.URL.Query().Get("search"); search != "" {
		filter.Search = search
	}

	if hasWarnings := r.URL.Query().Get("has_warnings"); hasWarnings != "" {
		val := hasWarnings == "true"
		filter.HasWarnings = &val
	}

	collection, err := h.service.List(ctx, filter)
	if err != nil {
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": err.Error()})
		return
	}

	render.JSON(w, r, collection)
}

// HandleGetPolicy returns a single policy by ID.
func (h *PolicyHandler) HandleGetPolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	p, err := h.service.Get(ctx, id)
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, map[string]string{"error": err.Error()})
		return
	}

	render.JSON(w, r, p)
}

// CreatePolicyRequest is the request body for creating a policy.
type CreatePolicyRequest struct {
	Name             string                   `json:"name"`
	Type             policy.PolicyType        `json:"type"`
	Provider         policy.Provider          `json:"provider"`
	ResourceID       string                   `json:"resource_id"`
	ResourceType     string                   `json:"resource_type"`
	ResourceName     string                   `json:"resource_name"`
	OriginalDocument json.RawMessage          `json:"original_document,omitempty"`
	NormalizedPolicy *policy.NormalizedPolicy `json:"normalized_policy,omitempty"`
}

// HandleCreatePolicy creates a new policy.
func (h *PolicyHandler) HandleCreatePolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req CreatePolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "Invalid request body"})
		return
	}

	p := policy.NewPolicy("", req.Name, req.Type, req.Provider)
	p.ResourceID = req.ResourceID
	p.ResourceType = req.ResourceType
	p.ResourceName = req.ResourceName
	p.OriginalDocument = req.OriginalDocument
	p.NormalizedPolicy = req.NormalizedPolicy

	created, err := h.service.Create(ctx, p)
	if err != nil {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": err.Error()})
		return
	}

	render.Status(r, http.StatusCreated)
	render.JSON(w, r, created)
}

// UpdatePolicyRequest is the request body for updating a policy.
type UpdatePolicyRequest struct {
	Name             string                   `json:"name,omitempty"`
	OriginalDocument json.RawMessage          `json:"original_document,omitempty"`
	NormalizedPolicy *policy.NormalizedPolicy `json:"normalized_policy,omitempty"`
	Warnings         []string                 `json:"warnings,omitempty"`
}

// HandleUpdatePolicy updates an existing policy.
func (h *PolicyHandler) HandleUpdatePolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	var req UpdatePolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "Invalid request body"})
		return
	}

	updates := &policy.Policy{
		Name:             req.Name,
		OriginalDocument: req.OriginalDocument,
		NormalizedPolicy: req.NormalizedPolicy,
		Warnings:         req.Warnings,
	}

	updated, err := h.service.Update(ctx, id, updates)
	if err != nil {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": err.Error()})
		return
	}

	render.JSON(w, r, updated)
}

// HandleDeletePolicy deletes a policy.
func (h *PolicyHandler) HandleDeletePolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	if err := h.service.Delete(ctx, id); err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, map[string]string{"error": err.Error()})
		return
	}

	render.Status(r, http.StatusNoContent)
}

// HandleValidatePolicy validates a policy.
func (h *PolicyHandler) HandleValidatePolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	p, err := h.service.Get(ctx, id)
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, map[string]string{"error": err.Error()})
		return
	}

	result, err := h.service.Validate(ctx, p)
	if err != nil {
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": err.Error()})
		return
	}

	render.JSON(w, r, result)
}

// HandleKeycloakPreview returns a Keycloak mapping preview.
func (h *PolicyHandler) HandleKeycloakPreview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	mapping, err := h.service.GetKeycloakPreview(ctx, id)
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, map[string]string{"error": err.Error()})
		return
	}

	if mapping == nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, map[string]string{"error": "No Keycloak mapping available for this policy"})
		return
	}

	render.JSON(w, r, mapping)
}

// HandleKeycloakRegenerate regenerates the Keycloak mapping.
func (h *PolicyHandler) HandleKeycloakRegenerate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	updated, err := h.service.RegenerateKeycloakMapping(ctx, id)
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, map[string]string{"error": err.Error()})
		return
	}

	render.JSON(w, r, updated)
}

// HandleExportOriginal exports the original policy document.
func (h *PolicyHandler) HandleExportOriginal(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	doc, format, err := h.service.ExportOriginal(ctx, id)
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, map[string]string{"error": err.Error()})
		return
	}

	// Set content type based on format
	var contentType string
	switch format {
	case "yaml":
		contentType = "application/x-yaml"
	case "hcl":
		contentType = "text/plain"
	default:
		contentType = "application/json"
	}

	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(doc)
}

// HandleGetSummary returns policy statistics.
func (h *PolicyHandler) HandleGetSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	summary, err := h.service.GetSummary(ctx)
	if err != nil {
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": err.Error()})
		return
	}

	render.JSON(w, r, summary)
}

// ImportPoliciesRequest is the request body for importing policies.
type ImportPoliciesRequest struct {
	Policies []*policy.Policy `json:"policies"`
}

// HandleImportPolicies imports policies from a migration.
func (h *PolicyHandler) HandleImportPolicies(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req ImportPoliciesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "Invalid request body"})
		return
	}

	if err := h.service.ImportPolicies(ctx, req.Policies); err != nil {
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": err.Error()})
		return
	}

	render.Status(r, http.StatusCreated)
	render.JSON(w, r, map[string]interface{}{
		"imported": len(req.Policies),
		"message":  "Policies imported successfully",
	})
}

// Close cleans up handler resources.
func (h *PolicyHandler) Close() error {
	if h.service != nil {
		return h.service.Close()
	}
	return nil
}
