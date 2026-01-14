package handlers

import (
	"net/http"
	"time"

	"github.com/homeport/homeport/internal/app/docker"
	"github.com/homeport/homeport/internal/app/metrics"
	"github.com/homeport/homeport/internal/pkg/httputil"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

// MetricsHandler handles metrics-related HTTP requests
type MetricsHandler struct {
	service *metrics.Service
}

// NewMetricsHandler creates a new metrics handler
func NewMetricsHandler(dockerService *docker.Service) (*MetricsHandler, error) {
	svc, err := metrics.NewService(dockerService.Client())
	if err != nil {
		return nil, err
	}
	return &MetricsHandler{service: svc}, nil
}

// Close closes the metrics handler resources
func (h *MetricsHandler) Close() error {
	if h.service != nil {
		return h.service.Close()
	}
	return nil
}

// transformContainerMetrics converts backend container metrics to frontend format
func transformContainerMetrics(cm metrics.ContainerMetrics) map[string]interface{} {
	return map[string]interface{}{
		"containerId":   cm.ContainerID,
		"containerName": cm.ContainerName,
		"timestamp":     cm.Timestamp,
		"cpu": map[string]interface{}{
			"usagePercent":       cm.CPU.UsagePercent,
			"systemUsagePercent": 0.0,
			"throttledPeriods":   cm.CPU.ThrottledPeriods,
			"throttledTime":      cm.CPU.ThrottledTime,
		},
		"memory": map[string]interface{}{
			"usage":        cm.Memory.UsageBytes,
			"limit":        cm.Memory.LimitBytes,
			"usagePercent": cm.Memory.UsagePercent,
			"cache":        cm.Memory.CacheBytes,
			"rss":          cm.Memory.RSSBytes,
		},
		"network": map[string]interface{}{
			"rxBytes":   cm.Network.RxBytes,
			"txBytes":   cm.Network.TxBytes,
			"rxPackets": cm.Network.RxPackets,
			"txPackets": cm.Network.TxPackets,
			"rxErrors":  cm.Network.RxErrors,
			"txErrors":  cm.Network.TxErrors,
			"rxDropped": cm.Network.RxDropped,
			"txDropped": cm.Network.TxDropped,
		},
		"disk": map[string]interface{}{
			"readBytes":  cm.DiskIO.ReadBytes,
			"writeBytes": cm.DiskIO.WriteBytes,
			"readOps":    cm.DiskIO.ReadOps,
			"writeOps":   cm.DiskIO.WriteOps,
		},
	}
}

// HandleGetContainerMetrics handles GET /api/v1/stacks/{stackID}/metrics/containers
func (h *MetricsHandler) HandleGetContainerMetrics(w http.ResponseWriter, r *http.Request) {
	stackID := chi.URLParam(r, "stackID")

	if err := validateStackID(stackID); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	_ = stackID // TODO: Filter by stackID

	containerMetrics, err := h.service.GetAllContainerMetrics(r.Context())
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	// Transform to frontend format
	transformed := make([]map[string]interface{}, 0, len(containerMetrics))
	for _, cm := range containerMetrics {
		transformed = append(transformed, transformContainerMetrics(cm))
	}

	render.JSON(w, r, map[string]interface{}{
		"metrics": transformed,
		"count":   len(transformed),
	})
}

// HandleGetSingleContainerMetrics handles GET /api/v1/stacks/{stackID}/metrics/containers/{containerID}
func (h *MetricsHandler) HandleGetSingleContainerMetrics(w http.ResponseWriter, r *http.Request) {
	containerID := chi.URLParam(r, "containerID")

	if err := validateContainerName(containerID); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	containerMetrics, err := h.service.GetContainerMetrics(r.Context(), containerID)
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, containerMetrics)
}

// HandleGetSystemMetrics handles GET /api/v1/stacks/{stackID}/metrics/system
func (h *MetricsHandler) HandleGetSystemMetrics(w http.ResponseWriter, r *http.Request) {
	systemMetrics, err := h.service.GetSystemMetrics(r.Context())
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	// Transform to match frontend expected format
	// Aggregate disk metrics into a single object
	var diskTotal, diskUsed, diskFree uint64
	var diskUsagePercent float64
	var inodesFree, inodesTotal uint64
	if len(systemMetrics.Disk) > 0 {
		for _, d := range systemMetrics.Disk {
			diskTotal += d.TotalBytes
			diskUsed += d.UsedBytes
			diskFree += d.FreeBytes
			inodesFree += d.InodesFree
			inodesTotal += d.InodesTotal
		}
		if diskTotal > 0 {
			diskUsagePercent = float64(diskUsed) / float64(diskTotal) * 100
		}
	}

	transformedMetrics := map[string]interface{}{
		"cpu": map[string]interface{}{
			"usagePercent":  systemMetrics.CPU.UsagePercent,
			"userPercent":   systemMetrics.CPU.UserPercent,
			"systemPercent": systemMetrics.CPU.SystemPercent,
			"idlePercent":   systemMetrics.CPU.IdlePercent,
			"cores":         systemMetrics.CPU.NumCores,
		},
		"memory": map[string]interface{}{
			"total":        systemMetrics.Memory.TotalBytes,
			"used":         systemMetrics.Memory.UsedBytes,
			"free":         systemMetrics.Memory.FreeBytes,
			"available":    systemMetrics.Memory.AvailableBytes,
			"usagePercent": systemMetrics.Memory.UsagePercent,
			"swapTotal":    systemMetrics.Memory.SwapTotalBytes,
			"swapUsed":     systemMetrics.Memory.SwapUsedBytes,
			"swapFree":     systemMetrics.Memory.SwapFreeBytes,
		},
		"disk": map[string]interface{}{
			"total":        diskTotal,
			"used":         diskUsed,
			"free":         diskFree,
			"usagePercent": diskUsagePercent,
			"inodesFree":   inodesFree,
			"inodesTotal":  inodesTotal,
		},
		"network": map[string]interface{}{
			"interfaces":   []interface{}{},
			"totalRxBytes": uint64(0),
			"totalTxBytes": uint64(0),
		},
		"load": map[string]interface{}{
			"load1":  systemMetrics.Load.Load1,
			"load5":  systemMetrics.Load.Load5,
			"load15": systemMetrics.Load.Load15,
		},
		"uptime":    int64(0),
		"timestamp": systemMetrics.Timestamp,
	}

	render.JSON(w, r, map[string]interface{}{
		"metrics": transformedMetrics,
	})
}

// HandleGetMetricsHistory handles GET /api/v1/stacks/{stackID}/metrics/history
func (h *MetricsHandler) HandleGetMetricsHistory(w http.ResponseWriter, r *http.Request) {
	timeRangeStr := r.URL.Query().Get("range")
	if timeRangeStr == "" {
		timeRangeStr = "1h"
	}

	containerID := r.URL.Query().Get("container")

	// Parse time range to duration and create TimeRange struct
	now := time.Now()
	var duration time.Duration
	switch timeRangeStr {
	case "5m":
		duration = 5 * time.Minute
	case "15m":
		duration = 15 * time.Minute
	case "1h":
		duration = time.Hour
	case "6h":
		duration = 6 * time.Hour
	case "24h":
		duration = 24 * time.Hour
	case "7d":
		duration = 7 * 24 * time.Hour
	default:
		duration = time.Hour
	}
	timeRange := metrics.TimeRange{
		Start: now.Add(-duration),
		End:   now,
	}

	response := map[string]interface{}{
		"timeRange": timeRangeStr,
	}

	// Always get system metrics history
	systemHistory, err := h.service.GetSystemMetricsHistory(r.Context(), timeRange, metrics.Resolution1m)
	if err != nil {
		// Return empty system history on error instead of failing
		response["systemHistory"] = map[string]interface{}{
			"cpuUsage":    []interface{}{},
			"memoryUsage": []interface{}{},
			"diskUsage":   []interface{}{},
			"networkRx":   []interface{}{},
			"networkTx":   []interface{}{},
			"load1":       []interface{}{},
		}
	} else {
		response["systemHistory"] = systemHistory
	}

	// Get container history if containerID is specified
	if containerID != "" {
		containerHistory, err := h.service.GetMetricsHistory(r.Context(), containerID, timeRange, metrics.Resolution1m)
		if err == nil && containerHistory != nil {
			response["containerHistory"] = []interface{}{containerHistory}
		} else {
			response["containerHistory"] = []interface{}{}
		}
	} else {
		response["containerHistory"] = []interface{}{}
	}

	render.JSON(w, r, response)
}

// HandleGetMetricsSummary handles GET /api/v1/stacks/{stackID}/metrics/summary
func (h *MetricsHandler) HandleGetMetricsSummary(w http.ResponseWriter, r *http.Request) {
	stackID := chi.URLParam(r, "stackID")

	if err := validateStackID(stackID); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	_ = stackID // TODO: Filter by stackID

	// Default to last hour
	now := time.Now()
	timeRange := metrics.TimeRange{
		Start: now.Add(-time.Hour),
		End:   now,
	}

	summary, err := h.service.GetMetricsSummary(r.Context(), timeRange)
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	// Ensure containers slice is not null
	if summary.Containers == nil {
		summary.Containers = make([]metrics.ContainerMetricsSummary, 0)
	}

	render.JSON(w, r, map[string]interface{}{
		"summary": summary,
	})
}
