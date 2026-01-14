package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/homeport/homeport/internal/app/stacks"
	"github.com/homeport/homeport/internal/pkg/httputil"
)

// StacksHandler handles stack-related HTTP requests.
type StacksHandler struct {
	service *stacks.Service
}

// Validation constants
const (
	MaxStackNameLength        = 64
	MaxStackDescriptionLength = 512
	MaxComposeFileSize        = 1024 * 1024 // 1MB
)

var stackNameRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,62}[a-zA-Z0-9]$|^[a-zA-Z]$`)

// NewStacksHandler creates a new stacks handler.
func NewStacksHandler(cfg *stacks.Config) (*StacksHandler, error) {
	svc, err := stacks.NewService(cfg)
	if err != nil {
		return nil, err
	}
	return &StacksHandler{service: svc}, nil
}

// Close closes the handler and releases resources.
func (h *StacksHandler) Close() error {
	return h.service.Close()
}

// RegisterRoutes registers stack routes on the router.
func (h *StacksHandler) RegisterRoutes(r chi.Router) {
	r.Route("/stacks", func(r chi.Router) {
		r.Get("/", h.HandleListStacks)
		r.Post("/", h.HandleCreateStack)
		r.Post("/pending", h.HandleCreatePendingStack)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", h.HandleGetStack)
			r.Put("/", h.HandleUpdateStack)
			r.Delete("/", h.HandleDeleteStack)
			r.Post("/start", h.HandleStartStack)
			r.Post("/stop", h.HandleStopStack)
			r.Post("/restart", h.HandleRestartStack)
			r.Get("/status", h.HandleGetStackStatus)
			r.Get("/logs", h.HandleGetStackLogs)
		})
	})
}

// HandleListStacks handles GET /stacks
func (h *StacksHandler) HandleListStacks(w http.ResponseWriter, r *http.Request) {
	stackList, err := h.service.ListStacks(r.Context())
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]interface{}{
		"stacks": stackList,
		"count":  len(stackList),
	})
}

// HandleCreateStack handles POST /stacks
func (h *StacksHandler) HandleCreateStack(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string            `json:"name"`
		Description string            `json:"description"`
		ComposeFile string            `json:"compose_file"`
		EnvVars     map[string]string `json:"env_vars"`
		Labels      map[string]string `json:"labels"`
	}

	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	// Validate name
	if err := validateStackName(req.Name); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	// Validate description
	if len(req.Description) > MaxStackDescriptionLength {
		httputil.BadRequest(w, r, fmt.Sprintf("description must be at most %d characters", MaxStackDescriptionLength))
		return
	}

	// Validate compose file
	if req.ComposeFile == "" {
		httputil.BadRequest(w, r, "compose_file is required")
		return
	}

	if len(req.ComposeFile) > MaxComposeFileSize {
		httputil.BadRequest(w, r, fmt.Sprintf("compose_file must be at most %d bytes", MaxComposeFileSize))
		return
	}

	stack, err := h.service.CreateStack(r.Context(), req.Name, req.Description, req.ComposeFile, req.EnvVars, req.Labels)
	if err != nil {
		if errors.Is(err, stacks.ErrStackExists) {
			httputil.BadRequest(w, r, "stack with this name already exists")
			return
		}
		if errors.Is(err, stacks.ErrInvalidCompose) {
			httputil.BadRequest(w, r, err.Error())
			return
		}
		if errors.Is(err, stacks.ErrInvalidName) {
			httputil.BadRequest(w, r, err.Error())
			return
		}
		httputil.InternalError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	render.JSON(w, r, stack)
}

// HandleCreatePendingStack handles POST /stacks/pending
func (h *StacksHandler) HandleCreatePendingStack(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		DeploymentConfig struct {
			Provider      string  `json:"provider"`
			Region        string  `json:"region"`
			HALevel       string  `json:"ha_level"`
			TerraformPath string  `json:"terraform_path,omitempty"`
			EstimatedCost *struct {
				Currency string  `json:"currency"`
				Compute  float64 `json:"compute"`
				Storage  float64 `json:"storage"`
				Database float64 `json:"database"`
				Network  float64 `json:"network"`
				Other    float64 `json:"other"`
				Total    float64 `json:"total"`
			} `json:"estimated_cost,omitempty"`
		} `json:"deployment_config"`
	}

	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	// Validate name
	if err := validateStackName(req.Name); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	// Validate description
	if len(req.Description) > MaxStackDescriptionLength {
		httputil.BadRequest(w, r, fmt.Sprintf("description must be at most %d characters", MaxStackDescriptionLength))
		return
	}

	// Validate provider
	validProviders := map[string]bool{"hetzner": true, "scaleway": true, "ovh": true}
	if !validProviders[req.DeploymentConfig.Provider] {
		httputil.BadRequest(w, r, "provider must be one of: hetzner, scaleway, ovh")
		return
	}

	// Validate HA level
	validHALevels := map[string]bool{"none": true, "basic": true, "multi": true, "cluster": true}
	if req.DeploymentConfig.HALevel != "" && !validHALevels[req.DeploymentConfig.HALevel] {
		httputil.BadRequest(w, r, "ha_level must be one of: none, basic, multi, cluster")
		return
	}

	// Build deployment config
	config := &stacks.DeploymentConfig{
		Provider:      req.DeploymentConfig.Provider,
		Region:        req.DeploymentConfig.Region,
		HALevel:       req.DeploymentConfig.HALevel,
		TerraformPath: req.DeploymentConfig.TerraformPath,
	}

	stack, err := h.service.CreatePendingStack(r.Context(), req.Name, req.Description, config)
	if err != nil {
		if errors.Is(err, stacks.ErrStackExists) {
			httputil.BadRequest(w, r, "stack with this name already exists")
			return
		}
		if errors.Is(err, stacks.ErrInvalidName) {
			httputil.BadRequest(w, r, err.Error())
			return
		}
		httputil.InternalError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	render.JSON(w, r, stack)
}

// HandleGetStack handles GET /stacks/{id}
func (h *StacksHandler) HandleGetStack(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	stack, err := h.service.GetStack(r.Context(), id)
	if err != nil {
		if errors.Is(err, stacks.ErrStackNotFound) {
			httputil.NotFound(w, r, "stack not found")
			return
		}
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, stack)
}

// HandleUpdateStack handles PUT /stacks/{id}
func (h *StacksHandler) HandleUpdateStack(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req struct {
		Name        *string           `json:"name"`
		Description *string           `json:"description"`
		ComposeFile *string           `json:"compose_file"`
		EnvVars     map[string]string `json:"env_vars"`
		Labels      map[string]string `json:"labels"`
	}

	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	// Validate name if provided
	if req.Name != nil {
		if err := validateStackName(*req.Name); err != nil {
			httputil.BadRequest(w, r, err.Error())
			return
		}
	}

	// Validate description if provided
	if req.Description != nil && len(*req.Description) > MaxStackDescriptionLength {
		httputil.BadRequest(w, r, fmt.Sprintf("description must be at most %d characters", MaxStackDescriptionLength))
		return
	}

	// Validate compose file if provided
	if req.ComposeFile != nil && len(*req.ComposeFile) > MaxComposeFileSize {
		httputil.BadRequest(w, r, fmt.Sprintf("compose_file must be at most %d bytes", MaxComposeFileSize))
		return
	}

	stack, err := h.service.UpdateStack(r.Context(), id, req.Name, req.Description, req.ComposeFile, req.EnvVars, req.Labels)
	if err != nil {
		if errors.Is(err, stacks.ErrStackNotFound) {
			httputil.NotFound(w, r, "stack not found")
			return
		}
		if errors.Is(err, stacks.ErrStackExists) {
			httputil.BadRequest(w, r, "stack with this name already exists")
			return
		}
		if errors.Is(err, stacks.ErrInvalidCompose) {
			httputil.BadRequest(w, r, err.Error())
			return
		}
		if errors.Is(err, stacks.ErrOperationPending) {
			httputil.BadRequest(w, r, "operation already in progress")
			return
		}
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, stack)
}

// HandleDeleteStack handles DELETE /stacks/{id}
func (h *StacksHandler) HandleDeleteStack(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	err := h.service.DeleteStack(r.Context(), id)
	if err != nil {
		if errors.Is(err, stacks.ErrStackNotFound) {
			httputil.NotFound(w, r, "stack not found")
			return
		}
		if errors.Is(err, stacks.ErrStackRunning) {
			httputil.BadRequest(w, r, "stack is running, stop it first")
			return
		}
		if errors.Is(err, stacks.ErrOperationPending) {
			httputil.BadRequest(w, r, "operation already in progress")
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

// HandleStartStack handles POST /stacks/{id}/start
func (h *StacksHandler) HandleStartStack(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	err := h.service.StartStack(r.Context(), id)
	if err != nil {
		if errors.Is(err, stacks.ErrStackNotFound) {
			httputil.NotFound(w, r, "stack not found")
			return
		}
		if errors.Is(err, stacks.ErrOperationPending) {
			httputil.BadRequest(w, r, "operation already in progress")
			return
		}
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]string{
		"status": "starting",
		"id":     id,
	})
}

// HandleStopStack handles POST /stacks/{id}/stop
func (h *StacksHandler) HandleStopStack(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	err := h.service.StopStack(r.Context(), id)
	if err != nil {
		if errors.Is(err, stacks.ErrStackNotFound) {
			httputil.NotFound(w, r, "stack not found")
			return
		}
		if errors.Is(err, stacks.ErrOperationPending) {
			httputil.BadRequest(w, r, "operation already in progress")
			return
		}
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]string{
		"status": "stopping",
		"id":     id,
	})
}

// HandleRestartStack handles POST /stacks/{id}/restart
func (h *StacksHandler) HandleRestartStack(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	err := h.service.RestartStack(r.Context(), id)
	if err != nil {
		if errors.Is(err, stacks.ErrStackNotFound) {
			httputil.NotFound(w, r, "stack not found")
			return
		}
		if errors.Is(err, stacks.ErrOperationPending) {
			httputil.BadRequest(w, r, "operation already in progress")
			return
		}
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]string{
		"status": "restarting",
		"id":     id,
	})
}

// HandleGetStackStatus handles GET /stacks/{id}/status
func (h *StacksHandler) HandleGetStackStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	stack, err := h.service.GetStackStatus(r.Context(), id)
	if err != nil {
		if errors.Is(err, stacks.ErrStackNotFound) {
			httputil.NotFound(w, r, "stack not found")
			return
		}
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, stack)
}

// HandleGetStackLogs handles GET /stacks/{id}/logs
func (h *StacksHandler) HandleGetStackLogs(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	service := r.URL.Query().Get("service")
	tailStr := r.URL.Query().Get("tail")

	tail := 100
	if tailStr != "" {
		if t, err := strconv.Atoi(tailStr); err == nil && t > 0 {
			tail = t
		}
	}

	logs, err := h.service.GetStackLogs(r.Context(), id, service, tail)
	if err != nil {
		if errors.Is(err, stacks.ErrStackNotFound) {
			httputil.NotFound(w, r, "stack not found")
			return
		}
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]string{
		"logs": logs,
	})
}

// validateStackName validates a stack name.
func validateStackName(name string) error {
	if name == "" {
		return fmt.Errorf("stack name is required")
	}
	if len(name) > MaxStackNameLength {
		return fmt.Errorf("stack name must be at most %d characters", MaxStackNameLength)
	}
	if !stackNameRegex.MatchString(name) {
		return fmt.Errorf("stack name must start with a letter and contain only alphanumeric, underscore, or hyphen")
	}
	return nil
}
