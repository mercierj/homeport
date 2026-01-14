package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	domainsync "github.com/homeport/homeport/internal/domain/sync"
	appsync "github.com/homeport/homeport/internal/app/sync"
)

// SyncHandler handles sync-related HTTP requests.
type SyncHandler struct {
	service *appsync.Service
}

// NewSyncHandler creates a new sync handler.
func NewSyncHandler() *SyncHandler {
	return &SyncHandler{
		service: appsync.NewService(),
	}
}

// RegisterRoutes registers sync routes.
func (h *SyncHandler) RegisterRoutes(r chi.Router) {
	r.Route("/sync", func(r chi.Router) {
		r.Post("/start", h.StartSync)
		r.Get("/strategies", h.GetStrategies)
		r.Route("/{syncId}", func(r chi.Router) {
			r.Get("/status", h.GetStatus)
			r.Get("/stream", h.StreamProgress)
			r.Post("/pause", h.PauseSync)
			r.Post("/resume", h.ResumeSync)
			r.Post("/cancel", h.CancelSync)
		})
	})
}

// StartSyncRequest represents a request to start sync.
type StartSyncRequest struct {
	Tasks []SyncTaskRequest `json:"tasks"`
}

// SyncTaskRequest represents a sync task in the request.
type SyncTaskRequest struct {
	ID       string              `json:"id"`
	Name     string              `json:"name"`
	Type     string              `json:"type"` // "database", "storage", "cache"
	Strategy string              `json:"strategy,omitempty"`
	Source   SyncEndpointRequest `json:"source"`
	Target   SyncEndpointRequest `json:"target"`
}

// SyncEndpointRequest represents a sync endpoint.
type SyncEndpointRequest struct {
	Type        string            `json:"type"`
	Host        string            `json:"host,omitempty"`
	Port        int               `json:"port,omitempty"`
	Database    string            `json:"database,omitempty"`
	Bucket      string            `json:"bucket,omitempty"`
	Path        string            `json:"path,omitempty"`
	Region      string            `json:"region,omitempty"`
	Username    string            `json:"username,omitempty"`
	Password    string            `json:"password,omitempty"`
	AccessKey   string            `json:"access_key,omitempty"`
	SecretKey   string            `json:"secret_key,omitempty"`
	SSL         bool              `json:"ssl,omitempty"`
	SSLMode     string            `json:"ssl_mode,omitempty"`
	Options     map[string]string `json:"options,omitempty"`
}

// StartSyncResponse represents the response to starting sync.
type StartSyncResponse struct {
	SyncID string `json:"sync_id"`
}

// StartSync starts a new sync operation.
func (h *SyncHandler) StartSync(w http.ResponseWriter, r *http.Request) {
	var req StartSyncRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "Invalid request body")
		return
	}

	if len(req.Tasks) == 0 {
		respondError(w, r, http.StatusBadRequest, "No sync tasks provided")
		return
	}

	// Convert request tasks to domain tasks
	tasks := make([]*domainsync.SyncTask, len(req.Tasks))
	for i, t := range req.Tasks {
		syncType := domainsync.SyncType(t.Type)
		if !syncType.IsValid() {
			respondError(w, r, http.StatusBadRequest, fmt.Sprintf("Invalid sync type: %s", t.Type))
			return
		}

		source := convertEndpoint(t.Source)
		target := convertEndpoint(t.Target)

		task := domainsync.NewSyncTask(t.ID, t.Name, syncType, source, target)
		task.Strategy = t.Strategy
		if task.Strategy == "" {
			task.Strategy = source.Type
		}
		tasks[i] = task
	}

	// Create plan
	plan, err := h.service.CreatePlan(tasks)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, r, http.StatusOK, StartSyncResponse{
		SyncID: plan.ID,
	})
}

func convertEndpoint(req SyncEndpointRequest) *domainsync.Endpoint {
	endpoint := domainsync.NewEndpoint(req.Type)
	endpoint.Host = req.Host
	endpoint.Port = req.Port
	endpoint.Database = req.Database
	endpoint.Bucket = req.Bucket
	endpoint.Path = req.Path
	endpoint.Region = req.Region
	endpoint.SSL = req.SSL
	endpoint.SSLMode = req.SSLMode
	endpoint.Options = req.Options

	if req.Username != "" || req.Password != "" || req.AccessKey != "" {
		endpoint.Credentials = &domainsync.Credentials{
			Username:  req.Username,
			Password:  req.Password,
			AccessKey: req.AccessKey,
			SecretKey: req.SecretKey,
		}
	}

	return endpoint
}

// GetStrategies returns available sync strategies.
func (h *SyncHandler) GetStrategies(w http.ResponseWriter, r *http.Request) {
	strategies := h.service.GetStrategies()
	respondJSON(w, r, http.StatusOK, map[string]interface{}{
		"strategies": strategies,
	})
}

// SyncStatusResponse represents sync status.
type SyncStatusResponse struct {
	SyncID    string             `json:"sync_id"`
	Status    string             `json:"status"`
	Progress  int                `json:"progress"`
	Tasks     []SyncTaskStatus   `json:"tasks"`
	StartedAt *time.Time         `json:"started_at,omitempty"`
	Error     string             `json:"error,omitempty"`
}

// SyncTaskStatus represents the status of a sync task.
type SyncTaskStatus struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Type           string `json:"type"`
	Status         string `json:"status"`
	Progress       int    `json:"progress"`
	BytesTotal     int64  `json:"bytes_total"`
	BytesDone      int64  `json:"bytes_done"`
	ItemsTotal     int64  `json:"items_total"`
	ItemsDone      int64  `json:"items_done"`
	Error          string `json:"error,omitempty"`
}

// GetStatus returns the status of a sync operation.
func (h *SyncHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	syncID := chi.URLParam(r, "syncId")

	exec, err := h.service.GetPlan(syncID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, err.Error())
		return
	}

	// Calculate overall progress
	totalTasks := len(exec.Plan.Tasks)
	completedTasks := 0
	taskStatuses := make([]SyncTaskStatus, len(exec.Plan.Tasks))

	for i, task := range exec.Plan.Tasks {
		progress := 0
		var bytesTotal, bytesDone, itemsTotal, itemsDone int64

		if task.Progress != nil {
			if task.Progress.BytesTotal > 0 {
				progress = int(float64(task.Progress.BytesDone) / float64(task.Progress.BytesTotal) * 100)
			}
			bytesTotal = task.Progress.BytesTotal
			bytesDone = task.Progress.BytesDone
			itemsTotal = task.Progress.ItemsTotal
			itemsDone = task.Progress.ItemsDone
		}

		if task.Status == domainsync.SyncStatusCompleted {
			completedTasks++
			progress = 100
		}

		taskStatuses[i] = SyncTaskStatus{
			ID:         task.ID,
			Name:       task.Name,
			Type:       string(task.Type),
			Status:     string(task.Status),
			Progress:   progress,
			BytesTotal: bytesTotal,
			BytesDone:  bytesDone,
			ItemsTotal: itemsTotal,
			ItemsDone:  itemsDone,
			Error:      task.ErrorMessage,
		}
	}

	overallProgress := 0
	if totalTasks > 0 {
		overallProgress = (completedTasks * 100) / totalTasks
	}

	respondJSON(w, r, http.StatusOK, SyncStatusResponse{
		SyncID:    syncID,
		Status:    exec.Status,
		Progress:  overallProgress,
		Tasks:     taskStatuses,
		StartedAt: exec.StartedAt,
		Error:     exec.Error,
	})
}

// StreamProgress streams sync progress via SSE.
func (h *SyncHandler) StreamProgress(w http.ResponseWriter, r *http.Request) {
	syncID := chi.URLParam(r, "syncId")

	exec, err := h.service.GetPlan(syncID)
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
	eventCh := make(chan appsync.SyncEvent, 100)

	// Start sync execution with callback
	ctx := r.Context()
	err = h.service.Start(ctx, syncID, func(event appsync.SyncEvent) {
		select {
		case eventCh <- event:
		default:
			// Channel full, skip event
		}
	})

	if err != nil {
		// If already running, just stream status updates
		if err.Error() != "sync plan already running" {
			respondError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
	}

	// Send initial status
	sendSSEEvent(w, flusher, "status", map[string]interface{}{
		"sync_id": syncID,
		"status":  exec.Status,
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
			case "task_start":
				sendSSEEvent(w, flusher, "task_start", event)
			case "task_progress":
				sendSSEEvent(w, flusher, "progress", event)
			case "task_complete":
				sendSSEEvent(w, flusher, "task_complete", event)
			case "task_error":
				sendSSEEvent(w, flusher, "error", event)
			case "plan_complete":
				sendSSEEvent(w, flusher, "complete", event)
				return
			}
		case <-time.After(30 * time.Second):
			// Send keepalive
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func sendSSEEvent(w http.ResponseWriter, flusher http.Flusher, eventType string, data interface{}) {
	jsonData, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\n", eventType)
	fmt.Fprintf(w, "data: %s\n\n", jsonData)
	flusher.Flush()
}

// PauseSync pauses a running sync.
func (h *SyncHandler) PauseSync(w http.ResponseWriter, r *http.Request) {
	syncID := chi.URLParam(r, "syncId")

	if err := h.service.Pause(syncID); err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, r, http.StatusOK, map[string]string{
		"status": "paused",
	})
}

// ResumeSync resumes a paused sync.
func (h *SyncHandler) ResumeSync(w http.ResponseWriter, r *http.Request) {
	syncID := chi.URLParam(r, "syncId")

	if err := h.service.Resume(context.Background(), syncID, nil); err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, r, http.StatusOK, map[string]string{
		"status": "running",
	})
}

// CancelSync cancels a running sync.
func (h *SyncHandler) CancelSync(w http.ResponseWriter, r *http.Request) {
	syncID := chi.URLParam(r, "syncId")

	if err := h.service.Cancel(syncID); err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, r, http.StatusOK, map[string]string{
		"status": "cancelled",
	})
}
