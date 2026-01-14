package handlers

import (
	"context"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/homeport/homeport/internal/app/docker"
	"github.com/go-chi/render"
)

// HealthStatus represents the overall health status.
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
)

// DependencyStatus represents the health of a single dependency.
type DependencyStatus struct {
	Status  HealthStatus `json:"status"`
	Latency string       `json:"latency,omitempty"`
	Error   string       `json:"error,omitempty"`
}

// HealthResponse represents the health check response.
type HealthResponse struct {
	Status       HealthStatus                `json:"status"`
	Version      string                      `json:"version,omitempty"`
	Uptime       string                      `json:"uptime,omitempty"`
	StartedAt    string                      `json:"started_at,omitempty"`
	Dependencies map[string]DependencyStatus `json:"dependencies,omitempty"`
	System       *SystemInfo                 `json:"system,omitempty"`
}

// SystemInfo contains system-level information.
type SystemInfo struct {
	GoVersion   string `json:"go_version"`
	NumGoroutine int    `json:"num_goroutine"`
	NumCPU      int    `json:"num_cpu"`
}

// HealthHandler handles health check requests.
type HealthHandler struct {
	startTime     time.Time
	version       string
	dockerService *docker.Service
}

// NewHealthHandler creates a new health handler.
func NewHealthHandler(version string) *HealthHandler {
	h := &HealthHandler{
		startTime: time.Now(),
		version:   version,
	}

	// Try to initialize docker service (optional dependency)
	if svc, err := docker.NewService(); err == nil {
		h.dockerService = svc
	}

	return h
}

// HandleHealth handles GET /health - basic health check.
func (h *HealthHandler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	render.JSON(w, r, HealthResponse{
		Status: HealthStatusHealthy,
	})
}

// HandleHealthDetailed handles GET /health/detailed - detailed health with dependencies.
func (h *HealthHandler) HandleHealthDetailed(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	resp := HealthResponse{
		Status:       HealthStatusHealthy,
		Version:      h.version,
		Uptime:       time.Since(h.startTime).Round(time.Second).String(),
		StartedAt:    h.startTime.Format(time.RFC3339),
		Dependencies: make(map[string]DependencyStatus),
		System: &SystemInfo{
			GoVersion:    runtime.Version(),
			NumGoroutine: runtime.NumGoroutine(),
			NumCPU:       runtime.NumCPU(),
		},
	}

	// Check dependencies concurrently
	var wg sync.WaitGroup
	var mu sync.Mutex

	// Check Docker
	wg.Add(1)
	go func() {
		defer wg.Done()
		status := h.checkDocker(ctx)
		mu.Lock()
		resp.Dependencies["docker"] = status
		mu.Unlock()
	}()

	wg.Wait()

	// Determine overall status based on dependencies
	hasUnhealthy := false
	hasDegraded := false
	for _, dep := range resp.Dependencies {
		switch dep.Status {
		case HealthStatusUnhealthy:
			hasUnhealthy = true
		case HealthStatusDegraded:
			hasDegraded = true
		}
	}

	if hasUnhealthy {
		resp.Status = HealthStatusDegraded // Degraded, not unhealthy, since these are optional
	} else if hasDegraded {
		resp.Status = HealthStatusDegraded
	}

	// Set appropriate status code
	statusCode := http.StatusOK
	if resp.Status == HealthStatusUnhealthy {
		statusCode = http.StatusServiceUnavailable
	}

	w.WriteHeader(statusCode)
	render.JSON(w, r, resp)
}

// HandleReadiness handles GET /health/ready - readiness probe for k8s.
func (h *HealthHandler) HandleReadiness(w http.ResponseWriter, r *http.Request) {
	// Basic readiness - we're ready if we can serve requests
	render.JSON(w, r, map[string]string{"status": "ready"})
}

// HandleLiveness handles GET /health/live - liveness probe for k8s.
func (h *HealthHandler) HandleLiveness(w http.ResponseWriter, r *http.Request) {
	// Basic liveness - we're alive if this handler can respond
	render.JSON(w, r, map[string]string{"status": "alive"})
}

// checkDocker checks if Docker is available and responding.
func (h *HealthHandler) checkDocker(ctx context.Context) DependencyStatus {
	if h.dockerService == nil {
		return DependencyStatus{
			Status: HealthStatusDegraded,
			Error:  "Docker service not initialized",
		}
	}

	start := time.Now()
	_, err := h.dockerService.ListContainers(ctx, "")
	latency := time.Since(start)

	if err != nil {
		return DependencyStatus{
			Status:  HealthStatusDegraded,
			Latency: latency.Round(time.Millisecond).String(),
			Error:   err.Error(),
		}
	}

	return DependencyStatus{
		Status:  HealthStatusHealthy,
		Latency: latency.Round(time.Millisecond).String(),
	}
}
