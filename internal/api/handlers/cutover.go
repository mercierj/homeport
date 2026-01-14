package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	domaincutover "github.com/homeport/homeport/internal/domain/cutover"
	appcutover "github.com/homeport/homeport/internal/app/cutover"
)

// CutoverHandler handles cutover-related HTTP requests.
type CutoverHandler struct {
	service *appcutover.Service
}

// NewCutoverHandler creates a new cutover handler.
func NewCutoverHandler() *CutoverHandler {
	return &CutoverHandler{
		service: appcutover.NewService(),
	}
}

// RegisterRoutes registers cutover routes.
func (h *CutoverHandler) RegisterRoutes(r chi.Router) {
	r.Route("/cutover", func(r chi.Router) {
		r.Post("/validate", h.ValidatePlan)
		r.Post("/start", h.StartCutover)
		r.Route("/{cutoverId}", func(r chi.Router) {
			r.Get("/status", h.GetStatus)
			r.Get("/stream", h.StreamProgress)
			r.Post("/cancel", h.CancelCutover)
			r.Post("/rollback", h.RollbackCutover)
		})
	})
}

// HealthCheckRequest represents a health check in the request.
type HealthCheckRequest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"` // "http", "tcp"
	Endpoint    string `json:"endpoint"`
	Timeout     int    `json:"timeout_seconds,omitempty"`
	ExpectCode  int    `json:"expect_code,omitempty"`
	ExpectBody  string `json:"expect_body,omitempty"`
}

// DNSChangeRequest represents a DNS change in the request.
type DNSChangeRequest struct {
	ID         string `json:"id"`
	Domain     string `json:"domain"`
	RecordType string `json:"record_type"` // "A", "CNAME", "AAAA"
	OldValue   string `json:"old_value"`
	NewValue   string `json:"new_value"`
	TTL        int    `json:"ttl,omitempty"`
}

// CreateCutoverRequest represents a request to create a cutover plan.
type CreateCutoverRequest struct {
	BundleID    string               `json:"bundle_id"`
	Name        string               `json:"name,omitempty"`
	PreChecks   []HealthCheckRequest `json:"pre_checks"`
	DNSChanges  []DNSChangeRequest   `json:"dns_changes"`
	PostChecks  []HealthCheckRequest `json:"post_checks"`
	DryRun      bool                 `json:"dry_run"`
	DNSProvider string               `json:"dns_provider,omitempty"`
}

// CreateCutoverResponse represents the response.
type CreateCutoverResponse struct {
	CutoverID string `json:"cutover_id"`
}

// ValidatePlan validates a cutover plan without executing it.
func (h *CutoverHandler) ValidatePlan(w http.ResponseWriter, r *http.Request) {
	var req CreateCutoverRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate the plan
	var errors []string
	var warnings []string

	if len(req.DNSChanges) == 0 {
		warnings = append(warnings, "No DNS changes specified")
	}

	if len(req.PreChecks) == 0 {
		warnings = append(warnings, "No pre-cutover health checks specified")
	}

	if len(req.PostChecks) == 0 {
		warnings = append(warnings, "No post-cutover health checks specified - rollback won't be automated")
	}

	for _, check := range req.PreChecks {
		if check.Endpoint == "" {
			errors = append(errors, fmt.Sprintf("Pre-check '%s' missing endpoint", check.Name))
		}
	}

	for _, change := range req.DNSChanges {
		if change.Domain == "" {
			errors = append(errors, "DNS change missing domain")
		}
		if change.NewValue == "" {
			errors = append(errors, fmt.Sprintf("DNS change for '%s' missing new value", change.Domain))
		}
	}

	respondJSON(w, r, http.StatusOK, map[string]interface{}{
		"valid":    len(errors) == 0,
		"errors":   errors,
		"warnings": warnings,
	})
}

// StartCutover starts a new cutover operation.
func (h *CutoverHandler) StartCutover(w http.ResponseWriter, r *http.Request) {
	var req CreateCutoverRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Convert request to domain types
	preChecks := make([]*domaincutover.HealthCheck, len(req.PreChecks))
	for i, check := range req.PreChecks {
		preChecks[i] = &domaincutover.HealthCheck{
			ID:       check.ID,
			Name:     check.Name,
			Type:     domaincutover.HealthCheckType(check.Type),
			Endpoint: check.Endpoint,
			Timeout:  time.Duration(check.Timeout) * time.Second,
		}
		if preChecks[i].Timeout == 0 {
			preChecks[i].Timeout = 30 * time.Second
		}
	}

	dnsChanges := make([]*domaincutover.DNSChange, len(req.DNSChanges))
	for i, change := range req.DNSChanges {
		dnsChanges[i] = &domaincutover.DNSChange{
			ID:         change.ID,
			Domain:     change.Domain,
			RecordType: change.RecordType,
			OldValue:   change.OldValue,
			NewValue:   change.NewValue,
			TTL:        change.TTL,
		}
		if dnsChanges[i].TTL == 0 {
			dnsChanges[i].TTL = 300
		}
	}

	postChecks := make([]*domaincutover.HealthCheck, len(req.PostChecks))
	for i, check := range req.PostChecks {
		postChecks[i] = &domaincutover.HealthCheck{
			ID:       check.ID,
			Name:     check.Name,
			Type:     domaincutover.HealthCheckType(check.Type),
			Endpoint: check.Endpoint,
			Timeout:  time.Duration(check.Timeout) * time.Second,
		}
		if postChecks[i].Timeout == 0 {
			postChecks[i].Timeout = 30 * time.Second
		}
	}

	// Create the plan
	plan, err := h.service.CreatePlan(&appcutover.CreatePlanRequest{
		BundleID:    req.BundleID,
		Name:        req.Name,
		PreChecks:   preChecks,
		DNSChanges:  dnsChanges,
		PostChecks:  postChecks,
		DryRun:      req.DryRun,
		DNSProvider: req.DNSProvider,
	})

	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, r, http.StatusOK, CreateCutoverResponse{
		CutoverID: plan.ID,
	})
}

// CutoverStatusResponse represents cutover status.
type CutoverStatusResponse struct {
	CutoverID string              `json:"cutover_id"`
	Status    string              `json:"status"`
	Progress  int                 `json:"progress"`
	Steps     []CutoverStepStatus `json:"steps"`
	StartedAt *time.Time          `json:"started_at,omitempty"`
	Logs      []string            `json:"logs"`
	Error     string              `json:"error,omitempty"`
}

// CutoverStepStatus represents the status of a cutover step.
type CutoverStepStatus struct {
	Order       int    `json:"order"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
}

// GetStatus returns the status of a cutover operation.
func (h *CutoverHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	cutoverID := chi.URLParam(r, "cutoverId")

	exec, err := h.service.GetPlan(cutoverID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, err.Error())
		return
	}

	// Calculate progress
	totalSteps := len(exec.Plan.Steps)
	completedSteps := 0
	for _, step := range exec.Plan.Steps {
		if step.Status == domaincutover.CutoverStepStatusCompleted {
			completedSteps++
		}
	}

	progress := 0
	if totalSteps > 0 {
		progress = (completedSteps * 100) / totalSteps
	}

	// Convert steps
	steps := make([]CutoverStepStatus, len(exec.Plan.Steps))
	for i, step := range exec.Plan.Steps {
		steps[i] = CutoverStepStatus{
			Order:       step.Order,
			Type:        string(step.Type),
			Description: step.Description,
			Status:      string(step.Status),
			Error:       step.Error,
		}
	}

	respondJSON(w, r, http.StatusOK, CutoverStatusResponse{
		CutoverID: cutoverID,
		Status:    string(exec.Plan.Status),
		Progress:  progress,
		Steps:     steps,
		StartedAt: exec.StartedAt,
		Logs:      exec.Logs,
		Error:     exec.Plan.Error,
	})
}

// StreamProgress streams cutover progress via SSE.
func (h *CutoverHandler) StreamProgress(w http.ResponseWriter, r *http.Request) {
	cutoverID := chi.URLParam(r, "cutoverId")

	exec, err := h.service.GetPlan(cutoverID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, err.Error())
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		respondError(w, r, http.StatusInternalServerError, "Streaming not supported")
		return
	}

	// Create event channel
	eventCh := make(chan appcutover.CutoverEvent, 100)

	// Start cutover execution with callback
	ctx := r.Context()
	err = h.service.Execute(ctx, cutoverID, func(event appcutover.CutoverEvent) {
		select {
		case eventCh <- event:
		default:
			// Channel full, skip event
		}
	})

	if err != nil {
		if err.Error() != "cutover already running" {
			respondError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
	}

	// Send initial status
	sendSSEEvent(w, flusher, "status", map[string]interface{}{
		"cutover_id": cutoverID,
		"status":     exec.Plan.Status,
	})

	// Stream events
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-eventCh:
			if !ok {
				return
			}

			switch event.Type {
			case "step_start":
				sendSSEEvent(w, flusher, "step_start", event)
			case "step_complete":
				sendSSEEvent(w, flusher, "step_complete", event)
			case "step_failed":
				sendSSEEvent(w, flusher, "step_failed", event)
			case "rollback":
				sendSSEEvent(w, flusher, "rollback", event)
			case "complete":
				sendSSEEvent(w, flusher, "complete", event)
				return
			case "error":
				sendSSEEvent(w, flusher, "error", event)
			}
		case <-time.After(30 * time.Second):
			// Send keepalive
			_, _ = fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

// CancelCutover cancels a running cutover.
func (h *CutoverHandler) CancelCutover(w http.ResponseWriter, r *http.Request) {
	cutoverID := chi.URLParam(r, "cutoverId")

	if err := h.service.Cancel(cutoverID); err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, r, http.StatusOK, map[string]string{
		"status": "cancelled",
	})
}

// RollbackCutover manually triggers a rollback.
func (h *CutoverHandler) RollbackCutover(w http.ResponseWriter, r *http.Request) {
	cutoverID := chi.URLParam(r, "cutoverId")

	if err := h.service.Rollback(context.Background(), cutoverID, nil); err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, r, http.StatusOK, map[string]string{
		"status": "rolling_back",
	})
}
