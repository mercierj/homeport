package logs

import (
	"bufio"
	"context"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/homeport/homeport/internal/app/docker"
)

// Service handles log operations for Docker containers
type Service struct {
	dockerService *docker.Service
	dockerClient  *client.Client
}

// NewService creates a new logs service
func NewService(dockerService *docker.Service) (*Service, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}

	return &Service{
		dockerService: dockerService,
		dockerClient:  cli,
	}, nil
}

// Close closes the logs service
func (s *Service) Close() error {
	if s.dockerClient != nil {
		return s.dockerClient.Close()
	}
	return nil
}

// LogSeverity represents log severity levels
type LogSeverity string

const (
	SeverityError LogSeverity = "error"
	SeverityWarn  LogSeverity = "warn"
	SeverityInfo  LogSeverity = "info"
	SeverityDebug LogSeverity = "debug"
	SeverityTrace LogSeverity = "trace"
)

// LogEntry represents a parsed log entry
type LogEntry struct {
	Timestamp time.Time   `json:"timestamp"`
	Severity  LogSeverity `json:"severity"`
	Message   string      `json:"message"`
	Container string      `json:"container"`
	Source    string      `json:"source,omitempty"`
}

// GetLogsParams contains parameters for fetching logs
type GetLogsParams struct {
	Tail       int           `json:"tail,omitempty"`
	Since      string        `json:"since,omitempty"`
	Until      string        `json:"until,omitempty"`
	Severity   []LogSeverity `json:"severity,omitempty"`
	Search     string        `json:"search,omitempty"`
	Timestamps bool          `json:"timestamps,omitempty"`
}

// ContainerLogsResponse contains the response from fetching container logs
type ContainerLogsResponse struct {
	Logs          []LogEntry `json:"logs"`
	Container     string     `json:"container"`
	TotalLines    int        `json:"totalLines"`
	FromTimestamp *time.Time `json:"fromTimestamp,omitempty"`
	ToTimestamp   *time.Time `json:"toTimestamp,omitempty"`
}

// LogStats contains log statistics for a container
type LogStats struct {
	Container          string     `json:"container"`
	TotalLines         int        `json:"totalLines"`
	ErrorCount         int        `json:"errorCount"`
	WarnCount          int        `json:"warnCount"`
	InfoCount          int        `json:"infoCount"`
	DebugCount         int        `json:"debugCount"`
	TraceCount         int        `json:"traceCount"`
	FirstLogTimestamp  *time.Time `json:"firstLogTimestamp,omitempty"`
	LastLogTimestamp   *time.Time `json:"lastLogTimestamp,omitempty"`
}

// SearchLogsParams contains parameters for searching logs
type SearchLogsParams struct {
	Pattern       string        `json:"pattern"`
	Containers    []string      `json:"containers"`
	Severity      []LogSeverity `json:"severity,omitempty"`
	Since         string        `json:"since,omitempty"`
	Until         string        `json:"until,omitempty"`
	Limit         int           `json:"limit,omitempty"`
	CaseSensitive bool          `json:"caseSensitive,omitempty"`
	Regex         bool          `json:"regex,omitempty"`
}

// SearchLogsResponse contains search results
type SearchLogsResponse struct {
	Results      []LogEntry `json:"results"`
	TotalMatches int        `json:"totalMatches"`
	Pattern      string     `json:"pattern"`
	Containers   []string   `json:"containers"`
}

var (
	// Common log format patterns
	isoTimestampRegex = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z?)\s*`)
	severityRegex     = regexp.MustCompile(`\[(ERROR|WARN(?:ING)?|INFO|DEBUG|TRACE)\]|\b(error|warn(?:ing)?|info|debug|trace)\b:?`)
)

// ParseLogLine parses a raw log line into a structured LogEntry
func ParseLogLine(line string, containerName string) LogEntry {
	timestamp := time.Now()
	severity := SeverityInfo
	message := line

	// Strip Docker log header (8 bytes) if present
	if len(line) > 8 && (line[0] == 1 || line[0] == 2) {
		line = line[8:]
		message = line
	}

	// Extract timestamp
	if match := isoTimestampRegex.FindStringSubmatch(line); match != nil {
		if t, err := time.Parse(time.RFC3339Nano, match[1]); err == nil {
			timestamp = t
		} else if t, err := time.Parse("2006-01-02T15:04:05.999999999Z07:00", match[1]); err == nil {
			timestamp = t
		}
		message = strings.TrimSpace(line[len(match[0]):])
	}

	// Extract severity
	if match := severityRegex.FindStringSubmatch(strings.ToLower(message)); match != nil {
		severityStr := match[1]
		if severityStr == "" {
			severityStr = match[2]
		}
		severityStr = strings.ToLower(severityStr)

		switch {
		case strings.HasPrefix(severityStr, "error"):
			severity = SeverityError
		case strings.HasPrefix(severityStr, "warn"):
			severity = SeverityWarn
		case severityStr == "info":
			severity = SeverityInfo
		case severityStr == "debug":
			severity = SeverityDebug
		case severityStr == "trace":
			severity = SeverityTrace
		}
	} else {
		// Heuristic-based severity detection
		lowerMessage := strings.ToLower(message)
		switch {
		case strings.Contains(lowerMessage, "error") || strings.Contains(lowerMessage, "exception") || strings.Contains(lowerMessage, "failed"):
			severity = SeverityError
		case strings.Contains(lowerMessage, "warn"):
			severity = SeverityWarn
		case strings.Contains(lowerMessage, "debug"):
			severity = SeverityDebug
		}
	}

	return LogEntry{
		Timestamp: timestamp,
		Severity:  severity,
		Message:   strings.TrimSpace(message),
		Container: containerName,
	}
}

// GetContainerLogsOld retrieves logs for a specific container (internal implementation).
func (s *Service) GetContainerLogsOld(ctx context.Context, containerName string, params GetLogsParams) (*ContainerLogsResponse, error) {
	tail := "100"
	if params.Tail > 0 {
		tail = string(rune(params.Tail))
	}

	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       tail,
		Timestamps: true,
	}

	if params.Since != "" {
		options.Since = params.Since
	}
	if params.Until != "" {
		options.Until = params.Until
	}

	reader, err := s.dockerClient.ContainerLogs(ctx, containerName, options)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	var logs []LogEntry
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		entry := ParseLogLine(line, containerName)

		// Filter by severity if specified
		if len(params.Severity) > 0 {
			found := false
			for _, sev := range params.Severity {
				if entry.Severity == sev {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		// Filter by search term if specified
		if params.Search != "" {
			if !strings.Contains(strings.ToLower(entry.Message), strings.ToLower(params.Search)) {
				continue
			}
		}

		logs = append(logs, entry)
	}

	if err := scanner.Err(); err != nil && err != io.EOF {
		return nil, err
	}

	response := &ContainerLogsResponse{
		Logs:       logs,
		Container:  containerName,
		TotalLines: len(logs),
	}

	if len(logs) > 0 {
		response.FromTimestamp = &logs[0].Timestamp
		response.ToTimestamp = &logs[len(logs)-1].Timestamp
	}

	return response, nil
}

// StreamLogs streams logs for a container and calls the callback for each log entry
func (s *Service) StreamLogs(ctx context.Context, containerName string, callback func(LogEntry)) error {
	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Tail:       "0",
		Timestamps: true,
	}

	reader, err := s.dockerClient.ContainerLogs(ctx, containerName, options)
	if err != nil {
		return err
	}
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			line := scanner.Text()
			if line == "" {
				continue
			}
			entry := ParseLogLine(line, containerName)
			callback(entry)
		}
	}

	return scanner.Err()
}

// SearchLogs searches logs across multiple containers
func (s *Service) SearchLogs(ctx context.Context, params SearchLogsParams) (*SearchLogsResponse, error) {
	if params.Limit == 0 {
		params.Limit = 1000
	}

	var allResults []LogEntry

	for _, containerName := range params.Containers {
		logsResponse, err := s.GetContainerLogsOld(ctx, containerName, GetLogsParams{
			Tail:     params.Limit,
			Since:    params.Since,
			Until:    params.Until,
			Severity: params.Severity,
		})
		if err != nil {
			continue
		}

		// Build search pattern
		var pattern *regexp.Regexp
		var searchErr error

		if params.Regex {
			flags := ""
			if !params.CaseSensitive {
				flags = "(?i)"
			}
			pattern, searchErr = regexp.Compile(flags + params.Pattern)
		} else {
			escaped := regexp.QuoteMeta(params.Pattern)
			flags := ""
			if !params.CaseSensitive {
				flags = "(?i)"
			}
			pattern, searchErr = regexp.Compile(flags + escaped)
		}

		if searchErr != nil {
			// Fall back to simple string matching
			searchPattern := params.Pattern
			if !params.CaseSensitive {
				searchPattern = strings.ToLower(params.Pattern)
			}

			for _, log := range logsResponse.Logs {
				message := log.Message
				if !params.CaseSensitive {
					message = strings.ToLower(message)
				}
				if strings.Contains(message, searchPattern) {
					allResults = append(allResults, log)
				}
			}
		} else {
			for _, log := range logsResponse.Logs {
				if pattern.MatchString(log.Message) {
					allResults = append(allResults, log)
				}
			}
		}
	}

	// Sort by timestamp
	// Simple bubble sort since we expect limited results
	for i := 0; i < len(allResults)-1; i++ {
		for j := 0; j < len(allResults)-i-1; j++ {
			if allResults[j].Timestamp.After(allResults[j+1].Timestamp) {
				allResults[j], allResults[j+1] = allResults[j+1], allResults[j]
			}
		}
	}

	// Limit results
	if len(allResults) > params.Limit {
		allResults = allResults[:params.Limit]
	}

	return &SearchLogsResponse{
		Results:      allResults,
		TotalMatches: len(allResults),
		Pattern:      params.Pattern,
		Containers:   params.Containers,
	}, nil
}

// GetLogStats returns log statistics for containers
func (s *Service) GetLogStats(ctx context.Context, containerNames []string) ([]LogStats, error) {
	var stats []LogStats

	for _, containerName := range containerNames {
		logsResponse, err := s.GetContainerLogsOld(ctx, containerName, GetLogsParams{
			Tail: 500,
		})
		if err != nil {
			continue
		}

		stat := LogStats{
			Container:  containerName,
			TotalLines: len(logsResponse.Logs),
		}

		for _, log := range logsResponse.Logs {
			switch log.Severity {
			case SeverityError:
				stat.ErrorCount++
			case SeverityWarn:
				stat.WarnCount++
			case SeverityInfo:
				stat.InfoCount++
			case SeverityDebug:
				stat.DebugCount++
			case SeverityTrace:
				stat.TraceCount++
			}
		}

		if len(logsResponse.Logs) > 0 {
			stat.FirstLogTimestamp = &logsResponse.Logs[0].Timestamp
			stat.LastLogTimestamp = &logsResponse.Logs[len(logsResponse.Logs)-1].Timestamp
		}

		stats = append(stats, stat)
	}

	return stats, nil
}

// ============================================================================
// Interface adapter methods for handlers.LogsService compatibility
// ============================================================================

// LogQueryOptions matches the handler's query options.
type LogQueryOptions struct {
	Since      string
	Until      string
	Tail       int
	Follow     bool
	Timestamps bool
	Filter     string
	Severities []LogSeverity
}

// LogSearchOptions matches the handler's search options.
type LogSearchOptions struct {
	LogQueryOptions
	ContainerIDs  []string
	Limit         int
	Offset        int
	CaseSensitive bool
	Regex         bool
}

// LogSearchResult matches the handler's search result.
type LogSearchResult struct {
	Entries    []LogEntry `json:"entries"`
	TotalCount int        `json:"total_count"`
	HasMore    bool       `json:"has_more"`
}

// GetContainerLogsCompat retrieves logs for a specific container (interface compatible).
func (s *Service) GetContainerLogs(ctx context.Context, containerID string, opts LogQueryOptions) ([]LogEntry, error) {
	params := GetLogsParams{
		Since:    opts.Since,
		Until:    opts.Until,
		Tail:     opts.Tail,
		Severity: opts.Severities,
		Search:   opts.Filter,
	}
	resp, err := s.getContainerLogsInternal(ctx, containerID, params)
	if err != nil {
		return nil, err
	}
	return resp.Logs, nil
}

// getContainerLogsInternal is the internal implementation.
func (s *Service) getContainerLogsInternal(ctx context.Context, containerName string, params GetLogsParams) (*ContainerLogsResponse, error) {
	// Call the existing GetContainerLogs implementation
	// This is a workaround since the method is already defined
	return s.GetContainerLogsOld(ctx, containerName, params)
}

// StreamContainerLogs streams logs for a specific container.
func (s *Service) StreamContainerLogs(ctx context.Context, containerID string, opts LogQueryOptions) (<-chan LogEntry, <-chan error) {
	logChan := make(chan LogEntry, 100)
	errChan := make(chan error, 1)

	go func() {
		defer close(logChan)
		defer close(errChan)

		err := s.StreamLogs(ctx, containerID, func(entry LogEntry) {
			select {
			case logChan <- entry:
			case <-ctx.Done():
				return
			}
		})
		if err != nil {
			errChan <- err
		}
	}()

	return logChan, errChan
}

// SearchLogsCompat searches across container logs (interface compatible).
func (s *Service) SearchLogsCompat(ctx context.Context, opts LogSearchOptions) (*LogSearchResult, error) {
	params := SearchLogsParams{
		Containers:    opts.ContainerIDs,
		Pattern:       opts.Filter,
		Regex:         opts.Regex,
		Since:         opts.Since,
		Until:         opts.Until,
		Limit:         opts.Limit,
		Severity:      opts.Severities,
		CaseSensitive: opts.CaseSensitive,
	}
	resp, err := s.SearchLogs(ctx, params)
	if err != nil {
		return nil, err
	}
	return &LogSearchResult{
		Entries:    resp.Results,
		TotalCount: resp.TotalMatches,
		HasMore:    resp.TotalMatches > len(resp.Results),
	}, nil
}

// GetLogStatsSingle retrieves log statistics for a single container.
func (s *Service) GetLogStatsSingle(ctx context.Context, containerID string) (*LogStats, error) {
	stats, err := s.GetLogStats(ctx, []string{containerID})
	if err != nil {
		return nil, err
	}
	if len(stats) == 0 {
		return &LogStats{Container: containerID}, nil
	}
	return &stats[0], nil
}

// GetAllLogStats retrieves log statistics for multiple containers.
func (s *Service) GetAllLogStats(ctx context.Context, containerIDs []string) ([]*LogStats, error) {
	stats, err := s.GetLogStats(ctx, containerIDs)
	if err != nil {
		return nil, err
	}
	result := make([]*LogStats, len(stats))
	for i := range stats {
		result[i] = &stats[i]
	}
	return result, nil
}

// ListContainerIDs returns all available container IDs for log searching.
func (s *Service) ListContainerIDs(ctx context.Context) ([]string, error) {
	containers, err := s.dockerClient.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(containers))
	for i, c := range containers {
		if len(c.Names) > 0 {
			ids[i] = strings.TrimPrefix(c.Names[0], "/")
		} else {
			ids[i] = c.ID[:12]
		}
	}
	return ids, nil
}
