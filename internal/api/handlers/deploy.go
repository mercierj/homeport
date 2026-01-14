package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/homeport/homeport/internal/app/deploy"
	"github.com/homeport/homeport/internal/pkg/logger"
	"github.com/go-chi/chi/v5"
)

type DeployHandler struct {
	service *deploy.Service
}

func NewDeployHandler() *DeployHandler {
	return &DeployHandler{
		service: deploy.NewService(),
	}
}

type StartDeploymentRequest struct {
	Target string          `json:"target"` // "local" or "ssh"
	Config json.RawMessage `json:"config"`
}

type StartDeploymentResponse struct {
	DeploymentID string `json:"deployment_id"`
}

type DeploymentStatusResponse struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	CurrentPhase int    `json:"current_phase"`
	TotalPhases  int    `json:"total_phases"`
	Error        string `json:"error,omitempty"`
}

// HandleStart starts a new deployment
// POST /api/v1/deploy/start
func (h *DeployHandler) HandleStart(w http.ResponseWriter, r *http.Request) {
	var req StartDeploymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var config interface{}
	switch req.Target {
	case "local":
		var localConfig deploy.LocalConfig
		if err := json.Unmarshal(req.Config, &localConfig); err != nil {
			http.Error(w, "Invalid local config", http.StatusBadRequest)
			return
		}
		config = &localConfig
	case "ssh":
		var sshConfig deploy.SSHConfig
		if err := json.Unmarshal(req.Config, &sshConfig); err != nil {
			http.Error(w, "Invalid SSH config", http.StatusBadRequest)
			return
		}
		config = &sshConfig
	default:
		http.Error(w, "Invalid target, must be 'local' or 'ssh'", http.StatusBadRequest)
		return
	}

	deployment, err := h.service.StartDeployment(req.Target, config)
	if err != nil {
		logger.Error("Failed to start deployment", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(StartDeploymentResponse{
		DeploymentID: deployment.ID,
	})
}

// HandleStream provides SSE endpoint for live progress
// GET /api/v1/deploy/{id}/stream
func (h *DeployHandler) HandleStream(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	deployment := h.service.Manager().GetDeployment(id)
	if deployment == nil {
		http.Error(w, "Deployment not found", http.StatusNotFound)
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

	// Subscribe to deployment events
	eventCh := deployment.Subscribe()
	defer deployment.Unsubscribe(eventCh)

	ctx := r.Context()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-eventCh:
			if !ok {
				// Channel closed, deployment complete
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

// HandleStatus provides poll fallback for deployment status
// GET /api/v1/deploy/{id}/status
func (h *DeployHandler) HandleStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	deployment := h.service.Manager().GetDeployment(id)
	if deployment == nil {
		http.Error(w, "Deployment not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(DeploymentStatusResponse{
		ID:           deployment.ID,
		Status:       string(deployment.Status),
		CurrentPhase: deployment.CurrentPhase,
		TotalPhases:  deployment.TotalPhases,
		Error:        deployment.Error,
	})
}

// HandleCancel cancels an ongoing deployment
// POST /api/v1/deploy/{id}/cancel
func (h *DeployHandler) HandleCancel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.service.CancelDeployment(id); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"cancelled"}`))
}

// HandleRetry retries a failed deployment
// POST /api/v1/deploy/{id}/retry
func (h *DeployHandler) HandleRetry(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	deployment, err := h.service.RetryDeployment(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(StartDeploymentResponse{
		DeploymentID: deployment.ID,
	})
}
