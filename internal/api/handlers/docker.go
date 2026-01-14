package handlers

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"

	"github.com/homeport/homeport/internal/app/docker"
	"github.com/homeport/homeport/internal/pkg/httputil"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

const (
	// MinTailLines is the minimum number of log lines to retrieve
	MinTailLines = 1
	// MaxTailLines is the maximum number of log lines to retrieve
	MaxTailLines = 10000
	// DefaultTailLines is the default number of log lines to retrieve
	DefaultTailLines = 100
	// MaxContainerNameLength is the maximum length of a container name
	MaxContainerNameLength = 256
)

var (
	// containerNameRegex validates Docker container names
	// Allows alphanumeric, hyphens, underscores, and periods
	containerNameRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.\-]*$`)
)

// validateContainerName checks if a container name is valid
func validateContainerName(name string) error {
	if name == "" {
		return fmt.Errorf("container name is required")
	}
	if len(name) > MaxContainerNameLength {
		return fmt.Errorf("container name must be at most %d characters", MaxContainerNameLength)
	}
	if !containerNameRegex.MatchString(name) {
		return fmt.Errorf("container name must start with alphanumeric and contain only letters, digits, hyphens, underscores, and periods")
	}
	return nil
}

// validateTailLines validates and normalizes the tail parameter
func validateTailLines(tail int) (int, error) {
	if tail < MinTailLines {
		return DefaultTailLines, nil
	}
	if tail > MaxTailLines {
		return 0, fmt.Errorf("tail must be at most %d", MaxTailLines)
	}
	return tail, nil
}

// validateStackID checks if a stack ID is valid
func validateStackID(stackID string) error {
	if stackID == "" || stackID == "default" {
		return nil // empty or "default" is allowed
	}
	if len(stackID) > MaxContainerNameLength {
		return fmt.Errorf("stack ID must be at most %d characters", MaxContainerNameLength)
	}
	if !containerNameRegex.MatchString(stackID) {
		return fmt.Errorf("stack ID must start with alphanumeric and contain only letters, digits, hyphens, underscores, and periods")
	}
	return nil
}

type DockerHandler struct {
	service *docker.Service
}

func NewDockerHandler() (*DockerHandler, error) {
	svc, err := docker.NewService()
	if err != nil {
		return nil, err
	}
	return &DockerHandler{service: svc}, nil
}

// Close closes the Docker client connection.
func (h *DockerHandler) Close() error {
	if h.service != nil {
		return h.service.Close()
	}
	return nil
}

func (h *DockerHandler) HandleListContainers(w http.ResponseWriter, r *http.Request) {
	stackID := chi.URLParam(r, "stackID")

	if err := validateStackID(stackID); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	containers, err := h.service.ListContainers(r.Context(), stackID)
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]interface{}{
		"containers": containers,
		"count":      len(containers),
	})
}

func (h *DockerHandler) HandleGetLogs(w http.ResponseWriter, r *http.Request) {
	containerName := chi.URLParam(r, "name")

	if err := validateContainerName(containerName); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	tail := DefaultTailLines
	if t := r.URL.Query().Get("tail"); t != "" {
		parsed, err := strconv.Atoi(t)
		if err != nil {
			httputil.BadRequest(w, r, "invalid tail parameter: must be a number")
			return
		}
		tail, err = validateTailLines(parsed)
		if err != nil {
			httputil.BadRequest(w, r, err.Error())
			return
		}
	}

	logs, err := h.service.GetContainerLogs(r.Context(), containerName, tail)
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]string{
		"logs": logs,
	})
}

func (h *DockerHandler) HandleRestart(w http.ResponseWriter, r *http.Request) {
	containerName := chi.URLParam(r, "name")

	if err := validateContainerName(containerName); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	if err := h.service.RestartContainer(r.Context(), containerName); err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]string{
		"status": "restarted",
	})
}

func (h *DockerHandler) HandleStop(w http.ResponseWriter, r *http.Request) {
	containerName := chi.URLParam(r, "name")

	if err := validateContainerName(containerName); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	if err := h.service.StopContainer(r.Context(), containerName); err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]string{
		"status": "stopped",
	})
}

func (h *DockerHandler) HandleStart(w http.ResponseWriter, r *http.Request) {
	containerName := chi.URLParam(r, "name")

	if err := validateContainerName(containerName); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	if err := h.service.StartContainer(r.Context(), containerName); err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]string{
		"status": "started",
	})
}

func (h *DockerHandler) HandleRemoveAll(w http.ResponseWriter, r *http.Request) {
	stackID := chi.URLParam(r, "stackID")

	if err := validateStackID(stackID); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	removed, err := h.service.RemoveAllContainers(r.Context(), stackID)
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]interface{}{
		"status":  "removed",
		"removed": removed,
	})
}
