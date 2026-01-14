package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/homeport/homeport/internal/app/datamigration"
	"github.com/homeport/homeport/internal/pkg/logger"
	"github.com/go-chi/chi/v5"
)

// DataMigrationHandler handles data migration API endpoints.
type DataMigrationHandler struct {
	service *datamigration.Service
}

// NewDataMigrationHandler creates a new data migration handler.
func NewDataMigrationHandler() *DataMigrationHandler {
	return &DataMigrationHandler{
		service: datamigration.NewService(),
	}
}

// ValidateRequest represents a validation request.
type ValidateRequest struct {
	Type        string                 `json:"type"`
	Source      map[string]interface{} `json:"source"`
	Destination map[string]interface{} `json:"destination"`
	Options     map[string]interface{} `json:"options,omitempty"`
}

// ValidateResponse represents a validation response.
type ValidateResponse struct {
	Valid    bool     `json:"valid"`
	Errors   []string `json:"errors,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// ExecuteRequest represents an execution request.
type ExecuteRequest struct {
	Type        string                 `json:"type"`
	Source      map[string]interface{} `json:"source"`
	Destination map[string]interface{} `json:"destination"`
	Options     map[string]interface{} `json:"options,omitempty"`
}

// ExecuteResponse represents an execution response.
type ExecuteResponse struct {
	MigrationID string `json:"migration_id"`
}

// MigrationStatusResponse represents a migration status response.
type MigrationStatusResponse struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	CurrentPhase int    `json:"current_phase"`
	TotalPhases  int    `json:"total_phases"`
	Error        string `json:"error,omitempty"`
}

// ListTypesResponse represents the available migration types.
type ListTypesResponse struct {
	Types []string `json:"types"`
}

// HandleValidate validates a migration configuration.
// POST /api/v1/migrate/data/validate
func (h *DataMigrationHandler) HandleValidate(w http.ResponseWriter, r *http.Request) {
	var req ValidateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	config := &datamigration.MigrationConfig{
		Type:        req.Type,
		Source:      req.Source,
		Destination: req.Destination,
		Options:     req.Options,
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30000000000) // 30 seconds
	defer cancel()

	result, err := h.service.Validate(ctx, config)
	if err != nil {
		logger.Error("Validation failed", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ValidateResponse{
		Valid:    result.Valid,
		Errors:   result.Errors,
		Warnings: result.Warnings,
	})
}

// HandleExecute starts a new data migration.
// POST /api/v1/migrate/data/execute
func (h *DataMigrationHandler) HandleExecute(w http.ResponseWriter, r *http.Request) {
	var req ExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	config := &datamigration.MigrationConfig{
		Type:        req.Type,
		Source:      req.Source,
		Destination: req.Destination,
		Options:     req.Options,
	}

	// Validate first
	ctx := r.Context()
	result, err := h.service.Validate(ctx, config)
	if err != nil {
		logger.Error("Validation failed", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if !result.Valid {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(ValidateResponse{
			Valid:  false,
			Errors: result.Errors,
		})
		return
	}

	// Start migration
	migration, err := h.service.Execute(ctx, config)
	if err != nil {
		logger.Error("Failed to start migration", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ExecuteResponse{
		MigrationID: migration.ID,
	})
}

// HandleStream provides SSE endpoint for live migration progress.
// GET /api/v1/migrate/data/{id}/stream
func (h *DataMigrationHandler) HandleStream(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	migration := h.service.GetMigration(id)
	if migration == nil {
		http.Error(w, "Migration not found", http.StatusNotFound)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Subscribe to migration events
	eventCh := migration.Subscribe()
	defer migration.Unsubscribe(eventCh)

	ctx := r.Context()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-eventCh:
			if !ok {
				// Channel closed, migration complete
				return
			}

			data, err := json.Marshal(event.Data)
			if err != nil {
				logger.Error("Failed to marshal event", "error", err)
				continue
			}

			fmt.Fprintf(w, "event: %s\n", event.Type)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// HandleStatus provides poll fallback for migration status.
// GET /api/v1/migrate/data/{id}/status
func (h *DataMigrationHandler) HandleStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	migration := h.service.GetMigration(id)
	if migration == nil {
		http.Error(w, "Migration not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(MigrationStatusResponse{
		ID:           migration.ID,
		Status:       string(migration.Status),
		CurrentPhase: migration.CurrentPhase,
		TotalPhases:  migration.TotalPhases,
		Error:        migration.Error,
	})
}

// HandleCancel cancels an ongoing migration.
// POST /api/v1/migrate/data/{id}/cancel
func (h *DataMigrationHandler) HandleCancel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.service.CancelMigration(id); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"cancelled"}`))
}

// HandleListTypes lists available migration types.
// GET /api/v1/migrate/data/types
func (h *DataMigrationHandler) HandleListTypes(w http.ResponseWriter, r *http.Request) {
	types := h.service.ListExecutors()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ListTypesResponse{
		Types: types,
	})
}
