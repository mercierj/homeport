package handlers

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/homeport/homeport/internal/app/backup"
	"github.com/homeport/homeport/internal/pkg/httputil"
)

// BackupHandler handles backup-related HTTP requests.
type BackupHandler struct {
	service *backup.Service
}

// Validation constants
const (
	MaxBackupNameLength        = 64
	MaxBackupDescriptionLength = 256
)

var backupNameRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}[a-zA-Z0-9]$|^[a-zA-Z0-9]$`)

// NewBackupHandler creates a new backup handler.
func NewBackupHandler(cfg *backup.Config) (*BackupHandler, error) {
	svc, err := backup.NewService(cfg)
	if err != nil {
		return nil, err
	}
	return &BackupHandler{service: svc}, nil
}

// Close closes the handler and releases resources.
func (h *BackupHandler) Close() error {
	return h.service.Close()
}

// RegisterRoutes registers backup routes on the router.
func (h *BackupHandler) RegisterRoutes(r chi.Router) {
	r.Route("/backups", func(r chi.Router) {
		r.Get("/", h.HandleListBackups)
		r.Post("/", h.HandleCreateBackup)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", h.HandleGetBackup)
			r.Delete("/", h.HandleDeleteBackup)
			r.Post("/restore", h.HandleRestoreBackup)
			r.Get("/download", h.HandleDownloadBackup)
		})
	})
	r.Get("/volumes", h.HandleListVolumes)
}

// HandleListBackups handles GET /backups
func (h *BackupHandler) HandleListBackups(w http.ResponseWriter, r *http.Request) {
	stackID := r.URL.Query().Get("stack_id")

	backups, err := h.service.ListBackups(r.Context(), stackID)
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]interface{}{
		"backups": backups,
		"count":   len(backups),
	})
}

// HandleCreateBackup handles POST /backups
func (h *BackupHandler) HandleCreateBackup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		StackID     string   `json:"stack_id"`
		Volumes     []string `json:"volumes"`
	}

	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	// Validate name
	if err := validateBackupName(req.Name); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	// Validate description
	if len(req.Description) > MaxBackupDescriptionLength {
		httputil.BadRequest(w, r, fmt.Sprintf("description must be at most %d characters", MaxBackupDescriptionLength))
		return
	}

	// Validate volumes
	if len(req.Volumes) == 0 {
		httputil.BadRequest(w, r, "at least one volume is required")
		return
	}

	bkp, err := h.service.CreateBackup(r.Context(), req.Name, req.Description, req.StackID, req.Volumes)
	if err != nil {
		if errors.Is(err, backup.ErrInvalidName) {
			httputil.BadRequest(w, r, err.Error())
			return
		}
		httputil.InternalError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	render.JSON(w, r, bkp)
}

// HandleGetBackup handles GET /backups/{id}
func (h *BackupHandler) HandleGetBackup(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	bkp, err := h.service.GetBackup(r.Context(), id)
	if err != nil {
		if errors.Is(err, backup.ErrBackupNotFound) {
			httputil.NotFound(w, r, "backup not found")
			return
		}
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, bkp)
}

// HandleDeleteBackup handles DELETE /backups/{id}
func (h *BackupHandler) HandleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	err := h.service.DeleteBackup(r.Context(), id)
	if err != nil {
		if errors.Is(err, backup.ErrBackupNotFound) {
			httputil.NotFound(w, r, "backup not found")
			return
		}
		if errors.Is(err, backup.ErrBackupInProgress) {
			httputil.BadRequest(w, r, "backup operation is in progress")
			return
		}
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]string{
		"status": "deleted",
		"id":     id,
	})
}

// HandleRestoreBackup handles POST /backups/{id}/restore
func (h *BackupHandler) HandleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req struct {
		TargetStackID string   `json:"target_stack_id"`
		Volumes       []string `json:"volumes"`
	}

	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	err := h.service.RestoreBackup(r.Context(), id, req.TargetStackID, req.Volumes)
	if err != nil {
		if errors.Is(err, backup.ErrBackupNotFound) {
			httputil.NotFound(w, r, "backup not found")
			return
		}
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]string{
		"status":    "restored",
		"backup_id": id,
	})
}

// HandleDownloadBackup handles GET /backups/{id}/download
func (h *BackupHandler) HandleDownloadBackup(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	filePath, err := h.service.GetBackupFile(r.Context(), id)
	if err != nil {
		if errors.Is(err, backup.ErrBackupNotFound) {
			httputil.NotFound(w, r, "backup not found")
			return
		}
		httputil.BadRequest(w, r, err.Error())
		return
	}

	// Open the file
	file, err := os.Open(filePath)
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}
	defer file.Close()

	// Get file info
	info, err := file.Stat()
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	// Set headers for download
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"backup-%s.tar.gz\"", id))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))

	// Stream the file
	_, _ = io.Copy(w, file)
}

// HandleListVolumes handles GET /volumes
func (h *BackupHandler) HandleListVolumes(w http.ResponseWriter, r *http.Request) {
	stackID := r.URL.Query().Get("stack_id")

	volumes, err := h.service.ListVolumes(r.Context(), stackID)
	if err != nil {
		// Return empty array on error (e.g., Docker not available)
		render.JSON(w, r, map[string]interface{}{
			"volumes": []interface{}{},
			"count":   0,
			"error":   err.Error(),
		})
		return
	}

	// Ensure we never return null
	if volumes == nil {
		volumes = make([]backup.VolumeInfo, 0)
	}

	render.JSON(w, r, map[string]interface{}{
		"volumes": volumes,
		"count":   len(volumes),
	})
}

// validateBackupName validates a backup name.
func validateBackupName(name string) error {
	if name == "" {
		return fmt.Errorf("backup name is required")
	}
	if len(name) > MaxBackupNameLength {
		return fmt.Errorf("backup name must be at most %d characters", MaxBackupNameLength)
	}
	if !backupNameRegex.MatchString(name) {
		return fmt.Errorf("backup name must start and end with alphanumeric character and contain only alphanumeric, underscore, dot, or hyphen")
	}
	return nil
}
