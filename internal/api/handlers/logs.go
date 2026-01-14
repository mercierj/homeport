package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/homeport/homeport/internal/app/logs"
	"github.com/homeport/homeport/internal/pkg/httputil"
)

const (
	// MinLogTailLines is the minimum number of log lines to retrieve
	MinLogTailLines = 1
	// MaxLogTailLines is the maximum number of log lines to retrieve
	MaxLogTailLines = 10000
	// DefaultLogTailLines is the default number of log lines to retrieve
	DefaultLogTailLines = 100
	// MaxSearchPatternLength is the maximum length of a search pattern
	MaxSearchPatternLength = 256
	// MaxLogContainerIDLength is the maximum length of a container ID
	MaxLogContainerIDLength = 128
	// DefaultStreamBufferSize is the default buffer size for streaming logs
	DefaultStreamBufferSize = 100
	// MaxSinceValue is the maximum "since" duration (7 days)
	MaxSinceValue = 7 * 24 * time.Hour
)

var (
	// logContainerIDRegex validates container IDs (Docker format: alphanumeric and colons)
	logContainerIDRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.\-:]*$`)
	// grepPatternRegex validates grep/filter patterns (no control characters)
	grepPatternRegex = regexp.MustCompile(`^[^\x00-\x1f]*$`)
)

// LogSeverityLevel represents log severity levels
type LogSeverityLevel string

const (
	LogSeverityDebug   LogSeverityLevel = "debug"
	LogSeverityInfo    LogSeverityLevel = "info"
	LogSeverityWarning LogSeverityLevel = "warning"
	LogSeverityError   LogSeverityLevel = "error"
	LogSeverityFatal   LogSeverityLevel = "fatal"
)

// LogEntry represents a single log entry
type LogEntry struct {
	Timestamp   time.Time        `json:"timestamp"`
	Container   string           `json:"container"`
	Message     string           `json:"message"`
	Severity    LogSeverityLevel `json:"severity,omitempty"`
	Stream      string           `json:"stream,omitempty"` // stdout or stderr
	ContainerID string           `json:"container_id,omitempty"`
}

// LogSearchResult represents search results across container logs
type LogSearchResult struct {
	Entries    []LogEntry `json:"entries"`
	TotalCount int        `json:"total_count"`
	Containers []string   `json:"containers"`
	Query      string     `json:"query"`
}

// LogStats represents log statistics
type LogStats struct {
	ContainerID    string           `json:"container_id"`
	ContainerName  string           `json:"container_name"`
	TotalLines     int64            `json:"total_lines"`
	TotalBytes     int64            `json:"total_bytes"`
	SeverityCounts map[string]int64 `json:"severity_counts"`
	FirstEntry     *time.Time       `json:"first_entry,omitempty"`
	LastEntry      *time.Time       `json:"last_entry,omitempty"`
	StreamCounts   map[string]int64 `json:"stream_counts"`
}

// LogQueryOptions represents options for querying logs
type LogQueryOptions struct {
	Since    time.Time          `json:"since,omitempty"`
	Until    time.Time          `json:"until,omitempty"`
	Tail     int                `json:"tail,omitempty"`
	Follow   bool               `json:"follow,omitempty"`
	Pattern  string             `json:"pattern,omitempty"`
	Severity []LogSeverityLevel `json:"severity,omitempty"`
	Stream   string             `json:"stream,omitempty"` // stdout, stderr, or empty for both
}

// LogSearchOptions represents options for searching logs
type LogSearchOptions struct {
	LogQueryOptions
	ContainerIDs  []string `json:"container_ids,omitempty"`
	Limit         int      `json:"limit,omitempty"`
	Offset        int      `json:"offset,omitempty"`
	CaseSensitive bool     `json:"case_sensitive,omitempty"`
	Regex         bool     `json:"regex,omitempty"`
}

// LogsService defines the interface for log operations
type LogsService interface {
	// GetContainerLogs retrieves logs for a specific container
	GetContainerLogs(ctx context.Context, containerID string, opts logs.LogQueryOptions) ([]logs.LogEntry, error)

	// StreamContainerLogs streams logs for a specific container
	StreamContainerLogs(ctx context.Context, containerID string, opts logs.LogQueryOptions) (<-chan logs.LogEntry, <-chan error)

	// SearchLogsCompat searches across container logs
	SearchLogsCompat(ctx context.Context, opts logs.LogSearchOptions) (*logs.LogSearchResult, error)

	// GetLogStatsSingle retrieves log statistics for a container
	GetLogStatsSingle(ctx context.Context, containerID string) (*logs.LogStats, error)

	// GetAllLogStats retrieves log statistics for multiple containers
	GetAllLogStats(ctx context.Context, containerIDs []string) ([]*logs.LogStats, error)

	// ListContainerIDs returns all available container IDs for log searching
	ListContainerIDs(ctx context.Context) ([]string, error)

	// Close closes the service and releases resources
	Close() error
}

// LogsHandler handles log-related HTTP requests
type LogsHandler struct {
	service LogsService
}

// toLogsQueryOptions converts handler LogQueryOptions to logs package type.
func toLogsQueryOptions(opts LogQueryOptions) logs.LogQueryOptions {
	severities := make([]logs.LogSeverity, 0, len(opts.Severity))
	for _, s := range opts.Severity {
		severities = append(severities, logs.LogSeverity(s))
	}
	var since, until string
	if !opts.Since.IsZero() {
		since = opts.Since.Format(time.RFC3339)
	}
	if !opts.Until.IsZero() {
		until = opts.Until.Format(time.RFC3339)
	}
	return logs.LogQueryOptions{
		Since:      since,
		Until:      until,
		Tail:       opts.Tail,
		Follow:     opts.Follow,
		Filter:     opts.Pattern,
		Severities: severities,
	}
}

// toLogsSearchOptions converts handler LogSearchOptions to logs package type.
func toLogsSearchOptions(opts LogSearchOptions) logs.LogSearchOptions {
	severities := make([]logs.LogSeverity, 0, len(opts.Severity))
	for _, s := range opts.Severity {
		severities = append(severities, logs.LogSeverity(s))
	}
	var since, until string
	if !opts.Since.IsZero() {
		since = opts.Since.Format(time.RFC3339)
	}
	if !opts.Until.IsZero() {
		until = opts.Until.Format(time.RFC3339)
	}
	return logs.LogSearchOptions{
		LogQueryOptions: logs.LogQueryOptions{
			Since:      since,
			Until:      until,
			Tail:       opts.Tail,
			Filter:     opts.Pattern,
			Severities: severities,
		},
		ContainerIDs:  opts.ContainerIDs,
		Limit:         opts.Limit,
		Offset:        opts.Offset,
		CaseSensitive: opts.CaseSensitive,
		Regex:         opts.Regex,
	}
}

// NewLogsHandler creates a new logs handler with the given service
func NewLogsHandler(service LogsService) *LogsHandler {
	return &LogsHandler{service: service}
}

// Close closes the logs handler resources
func (h *LogsHandler) Close() error {
	if h.service != nil {
		return h.service.Close()
	}
	return nil
}

// validateLogContainerID checks if a container ID is valid
func validateLogContainerID(id string) error {
	if id == "" {
		return fmt.Errorf("container ID is required")
	}
	if len(id) > MaxLogContainerIDLength {
		return fmt.Errorf("container ID must be at most %d characters", MaxLogContainerIDLength)
	}
	if !logContainerIDRegex.MatchString(id) {
		return fmt.Errorf("container ID must start with alphanumeric and contain only letters, digits, hyphens, underscores, periods, and colons")
	}
	return nil
}

// validateLogTailLines validates and normalizes the tail parameter
func validateLogTailLines(tail int) (int, error) {
	if tail < MinLogTailLines {
		return DefaultLogTailLines, nil
	}
	if tail > MaxLogTailLines {
		return 0, fmt.Errorf("tail must be at most %d", MaxLogTailLines)
	}
	return tail, nil
}

// validateGrepPattern validates the grep/filter pattern
func validateGrepPattern(pattern string) error {
	if pattern == "" {
		return nil
	}
	if len(pattern) > MaxSearchPatternLength {
		return fmt.Errorf("pattern must be at most %d characters", MaxSearchPatternLength)
	}
	if !grepPatternRegex.MatchString(pattern) {
		return fmt.Errorf("pattern contains invalid characters")
	}
	return nil
}

// validateLogSeverityLevels validates severity level strings
func validateLogSeverityLevels(levels []string) ([]LogSeverityLevel, error) {
	if len(levels) == 0 {
		return nil, nil
	}

	validLevels := map[string]LogSeverityLevel{
		"debug":   LogSeverityDebug,
		"info":    LogSeverityInfo,
		"warning": LogSeverityWarning,
		"warn":    LogSeverityWarning,
		"error":   LogSeverityError,
		"err":     LogSeverityError,
		"fatal":   LogSeverityFatal,
	}

	result := make([]LogSeverityLevel, 0, len(levels))
	for _, l := range levels {
		l = strings.ToLower(strings.TrimSpace(l))
		if level, ok := validLevels[l]; ok {
			result = append(result, level)
		} else {
			return nil, fmt.Errorf("invalid severity level: %s", l)
		}
	}
	return result, nil
}

// parseLogTimeParam parses a time parameter from various formats
func parseLogTimeParam(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}

	// Try RFC3339 format first
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, nil
	}

	// Try RFC3339Nano
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t, nil
	}

	// Try Unix timestamp
	if ts, err := strconv.ParseInt(value, 10, 64); err == nil {
		return time.Unix(ts, 0), nil
	}

	// Try duration (e.g., "1h", "30m", "24h")
	if d, err := time.ParseDuration(value); err == nil {
		return time.Now().Add(-d), nil
	}

	return time.Time{}, fmt.Errorf("invalid time format: %s (use RFC3339, Unix timestamp, or duration like '1h')", value)
}

// parseLogSinceParam parses the "since" parameter which can be a duration
func parseLogSinceParam(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}

	// Try duration first (e.g., "1h", "30m", "24h")
	if d, err := time.ParseDuration(value); err == nil {
		if d > MaxSinceValue {
			return time.Time{}, fmt.Errorf("since duration must be at most %s", MaxSinceValue)
		}
		return time.Now().Add(-d), nil
	}

	// Fall back to parseLogTimeParam
	return parseLogTimeParam(value)
}

// parseLogQueryOptions parses common query options from the request
func parseLogQueryOptions(r *http.Request) (LogQueryOptions, error) {
	opts := LogQueryOptions{}

	// Parse "since" parameter
	if since := r.URL.Query().Get("since"); since != "" {
		t, err := parseLogSinceParam(since)
		if err != nil {
			return opts, err
		}
		opts.Since = t
	}

	// Parse "from" parameter (alias for since)
	if from := r.URL.Query().Get("from"); from != "" && opts.Since.IsZero() {
		t, err := parseLogTimeParam(from)
		if err != nil {
			return opts, err
		}
		opts.Since = t
	}

	// Parse "to" / "until" parameter
	if to := r.URL.Query().Get("to"); to != "" {
		t, err := parseLogTimeParam(to)
		if err != nil {
			return opts, err
		}
		opts.Until = t
	}
	if until := r.URL.Query().Get("until"); until != "" && opts.Until.IsZero() {
		t, err := parseLogTimeParam(until)
		if err != nil {
			return opts, err
		}
		opts.Until = t
	}

	// Parse "tail" parameter
	if tailStr := r.URL.Query().Get("tail"); tailStr != "" {
		tail, err := strconv.Atoi(tailStr)
		if err != nil {
			return opts, fmt.Errorf("invalid tail parameter: must be a number")
		}
		tail, err = validateLogTailLines(tail)
		if err != nil {
			return opts, err
		}
		opts.Tail = tail
	} else {
		opts.Tail = DefaultLogTailLines
	}

	// Parse "follow" parameter
	if follow := r.URL.Query().Get("follow"); follow != "" {
		opts.Follow = follow == "true" || follow == "1"
	}

	// Parse "grep" / "filter" / "pattern" / "search" parameter
	pattern := r.URL.Query().Get("grep")
	if pattern == "" {
		pattern = r.URL.Query().Get("filter")
	}
	if pattern == "" {
		pattern = r.URL.Query().Get("pattern")
	}
	if pattern == "" {
		pattern = r.URL.Query().Get("search")
	}
	if err := validateGrepPattern(pattern); err != nil {
		return opts, err
	}
	opts.Pattern = pattern

	// Parse "severity" / "level" parameter
	severityStr := r.URL.Query().Get("severity")
	if severityStr == "" {
		severityStr = r.URL.Query().Get("level")
	}
	if severityStr != "" {
		levels := strings.Split(severityStr, ",")
		severityLevels, err := validateLogSeverityLevels(levels)
		if err != nil {
			return opts, err
		}
		opts.Severity = severityLevels
	}

	// Parse "stream" parameter (stdout, stderr)
	stream := r.URL.Query().Get("stream")
	if stream != "" {
		stream = strings.ToLower(stream)
		if stream != "stdout" && stream != "stderr" {
			return opts, fmt.Errorf("stream must be 'stdout' or 'stderr'")
		}
		opts.Stream = stream
	}

	return opts, nil
}

// HandleGetContainerLogs handles GET /api/logs/containers/:id
// Returns logs for a specific container
func (h *LogsHandler) HandleGetContainerLogs(w http.ResponseWriter, r *http.Request) {
	containerID := chi.URLParam(r, "id")
	// Also support "containerID" for backward compatibility
	if containerID == "" {
		containerID = chi.URLParam(r, "containerID")
	}

	if err := validateLogContainerID(containerID); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	opts, err := parseLogQueryOptions(r)
	if err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	entries, err := h.service.GetContainerLogs(r.Context(), containerID, toLogsQueryOptions(opts))
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]interface{}{
		"container_id": containerID,
		"entries":      entries,
		"count":        len(entries),
		"options": map[string]interface{}{
			"since":    opts.Since,
			"until":    opts.Until,
			"tail":     opts.Tail,
			"pattern":  opts.Pattern,
			"severity": opts.Severity,
			"stream":   opts.Stream,
		},
	})
}

// HandleStreamContainerLogs handles GET /api/logs/containers/:id/stream
// Streams logs for a specific container via Server-Sent Events (SSE)
func (h *LogsHandler) HandleStreamContainerLogs(w http.ResponseWriter, r *http.Request) {
	containerID := chi.URLParam(r, "id")
	// Also support "containerID" for backward compatibility
	if containerID == "" {
		containerID = chi.URLParam(r, "containerID")
	}

	if err := validateLogContainerID(containerID); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	opts, err := parseLogQueryOptions(r)
	if err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	// Force follow mode for streaming
	opts.Follow = true

	// Set up SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	// Ensure the writer supports flushing
	flusher, ok := w.(http.Flusher)
	if !ok {
		httputil.InternalError(w, r, fmt.Errorf("streaming not supported"))
		return
	}

	// Create a context that cancels when the client disconnects
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Start streaming logs
	entryChan, errChan := h.service.StreamContainerLogs(ctx, containerID, toLogsQueryOptions(opts))

	// Send initial connection event
	_, _ = fmt.Fprintf(w, "event: connected\ndata: {\"container_id\":\"%s\"}\n\n", containerID)
	flusher.Flush()

	// Stream logs
	for {
		select {
		case <-ctx.Done():
			// Client disconnected
			_, _ = fmt.Fprintf(w, "event: disconnected\ndata: {\"reason\":\"client_closed\"}\n\n")
			flusher.Flush()
			return

		case err, ok := <-errChan:
			if !ok {
				errChan = nil
				continue
			}
			if err != nil {
				_, _ = fmt.Fprintf(w, "event: error\ndata: {\"error\":\"%s\"}\n\n", escapeSSEData(err.Error()))
				flusher.Flush()
				return
			}

		case entry, ok := <-entryChan:
			if !ok {
				// Channel closed, send completion event
				_, _ = fmt.Fprintf(w, "event: complete\ndata: {\"reason\":\"stream_ended\"}\n\n")
				flusher.Flush()
				return
			}

			// Format log entry as JSON SSE event
			data, err := json.Marshal(entry)
			if err != nil {
				continue
			}
			_, _ = fmt.Fprintf(w, "event: log\ndata: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// escapeSSEData escapes special characters in SSE data
func escapeSSEData(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	return s
}

// HandleSearchLogs handles GET /api/logs/search
// Searches across container logs
func (h *LogsHandler) HandleSearchLogs(w http.ResponseWriter, r *http.Request) {
	queryOpts, err := parseLogQueryOptions(r)
	if err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	opts := LogSearchOptions{
		LogQueryOptions: queryOpts,
	}

	// Parse container IDs filter
	if containers := r.URL.Query().Get("containers"); containers != "" {
		ids := strings.Split(containers, ",")
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if err := validateLogContainerID(id); err != nil {
				httputil.BadRequest(w, r, fmt.Sprintf("invalid container ID '%s': %s", id, err.Error()))
				return
			}
			opts.ContainerIDs = append(opts.ContainerIDs, id)
		}
	}

	// Parse limit parameter
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		limit, err := strconv.Atoi(limitStr)
		if err != nil {
			httputil.BadRequest(w, r, "invalid limit parameter: must be a number")
			return
		}
		if limit < 1 || limit > MaxLogTailLines {
			httputil.BadRequest(w, r, fmt.Sprintf("limit must be between 1 and %d", MaxLogTailLines))
			return
		}
		opts.Limit = limit
	} else {
		opts.Limit = DefaultLogTailLines
	}

	// Parse offset parameter
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		offset, err := strconv.Atoi(offsetStr)
		if err != nil {
			httputil.BadRequest(w, r, "invalid offset parameter: must be a number")
			return
		}
		if offset < 0 {
			httputil.BadRequest(w, r, "offset must be non-negative")
			return
		}
		opts.Offset = offset
	}

	// Parse caseSensitive parameter
	if caseSensitive := r.URL.Query().Get("caseSensitive"); caseSensitive == "true" {
		opts.CaseSensitive = true
	}

	// Parse regex parameter
	if regex := r.URL.Query().Get("regex"); regex == "true" {
		opts.Regex = true
	}

	// Require a search pattern or severity filter
	if opts.Pattern == "" && len(opts.Severity) == 0 {
		httputil.BadRequest(w, r, "at least one of 'grep', 'filter', 'pattern', 'search', or 'severity' parameter is required for search")
		return
	}

	result, err := h.service.SearchLogsCompat(r.Context(), toLogsSearchOptions(opts))
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, result)
}

// HandleGetLogStats handles GET /api/logs/stats
// Returns log statistics for containers
func (h *LogsHandler) HandleGetLogStats(w http.ResponseWriter, r *http.Request) {
	containerID := r.URL.Query().Get("container")

	// If no specific container, get stats for all containers
	if containerID == "" {
		// Parse container IDs filter
		var containerIDs []string
		if containers := r.URL.Query().Get("containers"); containers != "" {
			ids := strings.Split(containers, ",")
			for _, id := range ids {
				id = strings.TrimSpace(id)
				if err := validateLogContainerID(id); err != nil {
					httputil.BadRequest(w, r, fmt.Sprintf("invalid container ID '%s': %s", id, err.Error()))
					return
				}
				containerIDs = append(containerIDs, id)
			}
		} else {
			// List all containers if none specified
			var err error
			containerIDs, err = h.service.ListContainerIDs(r.Context())
			if err != nil {
				httputil.InternalError(w, r, err)
				return
			}
		}

		allStats, err := h.service.GetAllLogStats(r.Context(), containerIDs)
		if err != nil {
			httputil.InternalError(w, r, err)
			return
		}

		render.JSON(w, r, map[string]interface{}{
			"stats":           allStats,
			"count":           len(allStats),
			"totalContainers": len(containerIDs),
			"containers":      containerIDs,
		})
		return
	}

	// Validate container ID
	if err := validateLogContainerID(containerID); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	stats, err := h.service.GetLogStatsSingle(r.Context(), containerID)
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, stats)
}

// RegisterRoutes registers the logs handler routes on a chi router
func (h *LogsHandler) RegisterRoutes(r chi.Router) {
	r.Route("/logs", func(r chi.Router) {
		// Container-specific logs
		r.Get("/containers/{id}", h.HandleGetContainerLogs)
		r.Get("/containers/{id}/stream", h.HandleStreamContainerLogs)

		// Search and stats
		r.Get("/search", h.HandleSearchLogs)
		r.Get("/stats", h.HandleGetLogStats)
	})
}
