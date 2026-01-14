package handlers

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/homeport/homeport/internal/app/functions"
	"github.com/homeport/homeport/internal/pkg/httputil"
)

const (
	MaxFunctionNameLength = 64
	MaxHandlerLength      = 256
	MaxDescriptionLength  = 1024
)

var (
	functionNameRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)
	uuidRegex         = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

func validateFunctionName(name string) error {
	if name == "" {
		return fmt.Errorf("function name is required")
	}
	if len(name) > MaxFunctionNameLength {
		return fmt.Errorf("function name must be at most %d characters", MaxFunctionNameLength)
	}
	if !functionNameRegex.MatchString(name) {
		return fmt.Errorf("function name must start with a letter and contain only letters, numbers, hyphens, and underscores")
	}
	return nil
}

func validateRuntime(runtime string) error {
	for _, r := range functions.SupportedRuntimes {
		if r == runtime {
			return nil
		}
	}
	return fmt.Errorf("unsupported runtime: %s. Supported: %s", runtime, strings.Join(functions.SupportedRuntimes, ", "))
}

func validateFunctionID(id string) error {
	if id == "" {
		return fmt.Errorf("function ID is required")
	}
	if !uuidRegex.MatchString(id) {
		return fmt.Errorf("invalid function ID format")
	}
	return nil
}

// FunctionsHandler handles function management HTTP requests.
type FunctionsHandler struct {
	service *functions.Service
}

// NewFunctionsHandler creates a new functions handler.
func NewFunctionsHandler() (*FunctionsHandler, error) {
	svc, err := functions.NewService()
	if err != nil {
		return nil, fmt.Errorf("failed to create functions service: %w", err)
	}
	return &FunctionsHandler{service: svc}, nil
}

// CreateFunctionRequest represents the request body for creating a function.
type CreateFunctionRequest struct {
	Name           string            `json:"name"`
	Runtime        string            `json:"runtime"`
	Handler        string            `json:"handler"`
	MemoryMB       int               `json:"memory_mb,omitempty"`
	TimeoutSeconds int               `json:"timeout_seconds,omitempty"`
	Environment    map[string]string `json:"environment,omitempty"`
	SourceCode     string            `json:"source_code,omitempty"`
	Description    string            `json:"description,omitempty"`
}

// UpdateFunctionRequest represents the request body for updating a function.
type UpdateFunctionRequest struct {
	Name           string            `json:"name"`
	Runtime        string            `json:"runtime"`
	Handler        string            `json:"handler"`
	MemoryMB       int               `json:"memory_mb,omitempty"`
	TimeoutSeconds int               `json:"timeout_seconds,omitempty"`
	Environment    map[string]string `json:"environment,omitempty"`
	SourceCode     string            `json:"source_code,omitempty"`
	Description    string            `json:"description,omitempty"`
}

// HandleListFunctions handles GET /functions
func (h *FunctionsHandler) HandleListFunctions(w http.ResponseWriter, r *http.Request) {
	// Parse optional filter parameters
	var filter *functions.FunctionFilter
	runtime := r.URL.Query().Get("runtime")
	status := r.URL.Query().Get("status")
	namePrefix := r.URL.Query().Get("name_prefix")

	if runtime != "" || status != "" || namePrefix != "" {
		filter = &functions.FunctionFilter{
			Runtime:    runtime,
			Status:     functions.FunctionStatus(status),
			NamePrefix: namePrefix,
		}
	}

	fns, err := h.service.ListFunctions(r.Context(), filter)
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]interface{}{
		"functions": fns,
		"count":     len(fns),
	})
}

// HandleGetFunction handles GET /functions/{id}
func (h *FunctionsHandler) HandleGetFunction(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := validateFunctionID(id); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	fn, err := h.service.GetFunction(r.Context(), id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			httputil.NotFound(w, r, "Function not found")
			return
		}
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, fn)
}

// HandleCreateFunction handles POST /functions
func (h *FunctionsHandler) HandleCreateFunction(w http.ResponseWriter, r *http.Request) {
	var req CreateFunctionRequest
	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	if err := validateFunctionName(req.Name); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	if err := validateRuntime(req.Runtime); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	if req.Handler == "" {
		httputil.BadRequest(w, r, "handler is required")
		return
	}

	if len(req.Handler) > MaxHandlerLength {
		httputil.BadRequest(w, r, fmt.Sprintf("handler must be at most %d characters", MaxHandlerLength))
		return
	}

	if len(req.Description) > MaxDescriptionLength {
		httputil.BadRequest(w, r, fmt.Sprintf("description must be at most %d characters", MaxDescriptionLength))
		return
	}

	config := functions.FunctionConfig{
		Name:           req.Name,
		Runtime:        req.Runtime,
		Handler:        req.Handler,
		MemoryMB:       req.MemoryMB,
		TimeoutSeconds: req.TimeoutSeconds,
		Environment:    req.Environment,
		SourceCode:     req.SourceCode,
		Description:    req.Description,
	}

	fn, err := h.service.CreateFunction(r.Context(), config)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			httputil.BadRequest(w, r, err.Error())
			return
		}
		httputil.InternalErrorWithMessage(w, r, "Failed to create function", err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	render.JSON(w, r, fn)
}

// HandleUpdateFunction handles PUT /functions/{id}
func (h *FunctionsHandler) HandleUpdateFunction(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := validateFunctionID(id); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	var req UpdateFunctionRequest
	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	if err := validateFunctionName(req.Name); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	if err := validateRuntime(req.Runtime); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	if req.Handler == "" {
		httputil.BadRequest(w, r, "handler is required")
		return
	}

	config := functions.FunctionConfig{
		Name:           req.Name,
		Runtime:        req.Runtime,
		Handler:        req.Handler,
		MemoryMB:       req.MemoryMB,
		TimeoutSeconds: req.TimeoutSeconds,
		Environment:    req.Environment,
		SourceCode:     req.SourceCode,
		Description:    req.Description,
	}

	fn, err := h.service.UpdateFunction(r.Context(), id, config)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			httputil.NotFound(w, r, "Function not found")
			return
		}
		if strings.Contains(err.Error(), "already exists") {
			httputil.BadRequest(w, r, err.Error())
			return
		}
		httputil.InternalErrorWithMessage(w, r, "Failed to update function", err)
		return
	}

	render.JSON(w, r, fn)
}

// HandleDeleteFunction handles DELETE /functions/{id}
func (h *FunctionsHandler) HandleDeleteFunction(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := validateFunctionID(id); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	if err := h.service.DeleteFunction(r.Context(), id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			httputil.NotFound(w, r, "Function not found")
			return
		}
		httputil.InternalErrorWithMessage(w, r, "Failed to delete function", err)
		return
	}

	render.JSON(w, r, map[string]string{
		"status": "deleted",
		"id":     id,
	})
}

// HandleInvokeFunction handles POST /functions/{id}/invoke
func (h *FunctionsHandler) HandleInvokeFunction(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := validateFunctionID(id); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	// Read request body as payload
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		httputil.BadRequest(w, r, "Failed to read request body")
		return
	}

	result, err := h.service.InvokeFunction(r.Context(), id, payload)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			httputil.NotFound(w, r, "Function not found")
			return
		}
		if strings.Contains(err.Error(), "not ready") {
			httputil.BadRequest(w, r, err.Error())
			return
		}
		httputil.InternalErrorWithMessage(w, r, "Failed to invoke function", err)
		return
	}

	render.JSON(w, r, result)
}

// HandleGetFunctionLogs handles GET /functions/{id}/logs
func (h *FunctionsHandler) HandleGetFunctionLogs(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := validateFunctionID(id); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	// Parse optional since parameter
	var since *time.Time
	sinceStr := r.URL.Query().Get("since")
	if sinceStr != "" {
		t, err := time.Parse(time.RFC3339, sinceStr)
		if err != nil {
			httputil.BadRequest(w, r, "Invalid 'since' parameter format. Use RFC3339.")
			return
		}
		since = &t
	}

	logs, err := h.service.GetFunctionLogs(r.Context(), id, since)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			httputil.NotFound(w, r, "Function not found")
			return
		}
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]interface{}{
		"logs":  logs,
		"count": len(logs),
	})
}
