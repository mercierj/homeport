package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/homeport/homeport/internal/app/migrate"
	"github.com/homeport/homeport/internal/pkg/httputil"
	"github.com/homeport/homeport/internal/pkg/logger"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

// MigrateHandler handles migration-related HTTP requests.
type MigrateHandler struct {
	service *migrate.Service
}

// NewMigrateHandler creates a new migration handler with state persistence.
func NewMigrateHandler() *MigrateHandler {
	// Try to create service with state persistence
	service, err := migrate.NewServiceWithState("")
	if err != nil {
		logger.Warn("Failed to initialize state store, using in-memory only", "error", err)
		service = migrate.NewService()
	}

	return &MigrateHandler{
		service: service,
	}
}

// HandleAnalyze handles POST /api/v1/migrate/analyze
// It parses infrastructure files and returns discovered resources.
func (h *MigrateHandler) HandleAnalyze(w http.ResponseWriter, r *http.Request) {
	var req migrate.AnalyzeRequest
	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	resp, err := h.service.Analyze(r.Context(), req)
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, resp)
}

// HandleGenerate handles POST /api/v1/migrate/generate
// It generates a Docker Compose stack from resources.
func (h *MigrateHandler) HandleGenerate(w http.ResponseWriter, r *http.Request) {
	var req migrate.GenerateRequest
	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	resp, err := h.service.Generate(r.Context(), req)
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, resp)
}

// HandleDiscover handles POST /api/v1/migrate/discover
// It discovers infrastructure via cloud provider APIs.
func (h *MigrateHandler) HandleDiscover(w http.ResponseWriter, r *http.Request) {
	var req migrate.DiscoverRequest
	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	resp, err := h.service.Discover(r.Context(), req)
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, resp)
}

// HandleDiscoverStream handles POST /api/v1/migrate/discover/stream
// It discovers infrastructure via cloud provider APIs with SSE progress updates.
func (h *MigrateHandler) HandleDiscoverStream(w http.ResponseWriter, r *http.Request) {
	var req migrate.DiscoverRequest
	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	// Set up SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	flusher, ok := w.(http.Flusher)
	if !ok {
		httputil.InternalError(w, r, fmt.Errorf("streaming not supported"))
		return
	}

	// Send progress updates via callback
	progressCallback := func(event migrate.ProgressEvent) {
		data, err := json.Marshal(event)
		if err != nil {
			return
		}
		fmt.Fprintf(w, "event: progress\ndata: %s\n\n", data)
		flusher.Flush()
	}

	resp, err := h.service.DiscoverWithProgress(r.Context(), req, progressCallback)
	if err != nil {
		// Send error event
		errEvent := migrate.ProgressEvent{
			Type:    "error",
			Message: err.Error(),
		}
		data, _ := json.Marshal(errEvent)
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", data)
		flusher.Flush()
		return
	}

	// Send completion event with the full response
	data, err := json.Marshal(resp)
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}
	fmt.Fprintf(w, "event: complete\ndata: %s\n\n", data)
	flusher.Flush()
}

// HandleDownload handles POST /api/v1/migrate/download
// It generates and returns a ZIP file containing all migration artifacts.
func (h *MigrateHandler) HandleDownload(w http.ResponseWriter, r *http.Request) {
	var req migrate.GenerateRequest
	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	zipData, err := h.service.GenerateZip(r.Context(), req)
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=homeport-stack.zip")
	if _, err := w.Write(zipData); err != nil {
		// Client likely disconnected; log but don't try to write error response
		return
	}
}

// SaveDiscoveryRequest represents a request to save a discovery.
type SaveDiscoveryRequest struct {
	Name      string                `json:"name"`
	Discovery *migrate.AnalyzeResponse `json:"discovery"`
}

// HandleListDiscoveries handles GET /api/v1/migrate/discoveries
// Returns all saved discoveries (summary without full resources).
func (h *MigrateHandler) HandleListDiscoveries(w http.ResponseWriter, r *http.Request) {
	discoveries := h.service.ListDiscoveries()
	if discoveries == nil {
		discoveries = []*migrate.DiscoveryState{}
	}
	render.JSON(w, r, discoveries)
}

// HandleGetDiscovery handles GET /api/v1/migrate/discoveries/{id}
// Returns a specific saved discovery with full resources.
func (h *MigrateHandler) HandleGetDiscovery(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.BadRequest(w, r, "discovery ID is required")
		return
	}

	discovery, err := h.service.GetDiscovery(id)
	if err != nil {
		httputil.NotFound(w, r, err.Error())
		return
	}

	render.JSON(w, r, discovery)
}

// HandleSaveDiscovery handles POST /api/v1/migrate/discoveries
// Saves a discovery result for later use.
func (h *MigrateHandler) HandleSaveDiscovery(w http.ResponseWriter, r *http.Request) {
	var req SaveDiscoveryRequest
	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	if req.Name == "" {
		httputil.BadRequest(w, r, "name is required")
		return
	}

	if req.Discovery == nil {
		httputil.BadRequest(w, r, "discovery data is required")
		return
	}

	state, err := h.service.SaveDiscovery(req.Name, req.Discovery)
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.Status(r, http.StatusCreated)
	render.JSON(w, r, state)
}

// HandleDeleteDiscovery handles DELETE /api/v1/migrate/discoveries/{id}
// Deletes a saved discovery.
func (h *MigrateHandler) HandleDeleteDiscovery(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.BadRequest(w, r, "discovery ID is required")
		return
	}

	if err := h.service.DeleteDiscovery(id); err != nil {
		httputil.NotFound(w, r, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// RenameDiscoveryRequest represents a request to rename a discovery.
type RenameDiscoveryRequest struct {
	Name string `json:"name"`
}

// HandleRenameDiscovery handles PATCH /api/v1/migrate/discoveries/{id}
// Renames a saved discovery.
func (h *MigrateHandler) HandleRenameDiscovery(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.BadRequest(w, r, "discovery ID is required")
		return
	}

	var req RenameDiscoveryRequest
	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	if req.Name == "" {
		httputil.BadRequest(w, r, "name is required")
		return
	}

	state, err := h.service.RenameDiscovery(id, req.Name)
	if err != nil {
		httputil.NotFound(w, r, err.Error())
		return
	}

	render.JSON(w, r, state)
}

// ExportTerraformRequest represents a request to export Terraform configuration.
type ExportTerraformRequest struct {
	Resources []migrate.ResourceInfo `json:"resources"`
	Config    ExportConfig           `json:"config"`
}

// ExportConfig configures the Terraform export.
type ExportConfig struct {
	Provider    string `json:"provider"`
	ProjectName string `json:"project_name"`
	Domain      string `json:"domain"`
	Region      string `json:"region"`
}

// HandleExportProvider handles POST /api/v1/migrate/export/{provider}
// It generates and returns a ZIP file containing Terraform configuration.
func (h *MigrateHandler) HandleExportProvider(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	if provider == "" {
		httputil.BadRequest(w, r, "provider is required")
		return
	}

	// Validate provider
	validProviders := map[string]bool{"hetzner": true, "scaleway": true, "ovh": true}
	if !validProviders[provider] {
		httputil.BadRequest(w, r, "invalid provider: must be hetzner, scaleway, or ovh")
		return
	}

	var req ExportTerraformRequest
	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	// Override provider from URL
	req.Config.Provider = provider

	// Convert to service config type
	serviceConfig := &migrate.TerraformExportConfig{
		Provider:    req.Config.Provider,
		ProjectName: req.Config.ProjectName,
		Domain:      req.Config.Domain,
		Region:      req.Config.Region,
	}

	zipData, err := h.service.GenerateTerraformZip(r.Context(), req.Resources, serviceConfig)
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=homeport-terraform-%s.zip", provider))
	if _, err := w.Write(zipData); err != nil {
		return
	}
}
