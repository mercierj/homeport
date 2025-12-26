package security

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// QueryAuditEntry represents a logged query execution
type QueryAuditEntry struct {
	// Identification
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	RequestID string    `json:"request_id,omitempty"`

	// User context
	Username  string `json:"username,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	IPAddress string `json:"ip_address,omitempty"`

	// Query details
	Query           string    `json:"query"`
	QueryHash       string    `json:"query_hash"`
	QueryType       QueryType `json:"query_type"`
	Tables          []string  `json:"tables,omitempty"`
	IsReadOnly      bool      `json:"is_read_only"`
	ParameterCount  int       `json:"parameter_count,omitempty"`

	// Execution details
	Duration   time.Duration `json:"duration"`
	RowCount   int           `json:"row_count"`
	Success    bool          `json:"success"`
	ErrorCode  string        `json:"error_code,omitempty"`
	ErrorMsg   string        `json:"error_message,omitempty"`

	// Security context
	ACLChecked     bool     `json:"acl_checked"`
	MaskedColumns  []string `json:"masked_columns,omitempty"`
	ValidationErrs []string `json:"validation_errors,omitempty"`
}

// AuditConfig holds configuration for query auditing
type AuditConfig struct {
	// Enabled controls whether auditing is active
	Enabled bool
	// LogLevel is the slog level to use for audit logs
	LogLevel slog.Level
	// LogQueries controls whether full query text is logged
	LogQueries bool
	// LogQueryParams controls whether query parameters are logged
	LogQueryParams bool
	// MaxQueryLength is the maximum query length to log (0 = unlimited)
	MaxQueryLength int
	// SampleRate is the percentage of queries to log (0.0-1.0, 1.0 = all)
	SampleRate float64
	// IncludeSelectQueries controls whether SELECT queries are logged
	IncludeSelectQueries bool
	// IncludeSlowQueries controls whether slow queries get extra logging
	IncludeSlowQueries bool
	// SlowQueryThreshold is the duration above which a query is considered slow
	SlowQueryThreshold time.Duration
	// OnAudit is an optional callback for each audit entry
	OnAudit func(*QueryAuditEntry)
}

// DefaultAuditConfig returns secure default audit configuration
func DefaultAuditConfig() *AuditConfig {
	return &AuditConfig{
		Enabled:              true,
		LogLevel:             slog.LevelInfo,
		LogQueries:           true,
		LogQueryParams:       false, // Don't log params by default (could contain sensitive data)
		MaxQueryLength:       1000,  // Truncate long queries
		SampleRate:           1.0,   // Log all queries
		IncludeSelectQueries: true,
		IncludeSlowQueries:   true,
		SlowQueryThreshold:   time.Second,
	}
}

// Auditor handles query audit logging
type Auditor struct {
	config  *AuditConfig
	logger  *slog.Logger
	mu      sync.RWMutex
	counter uint64
}

// NewAuditor creates a new query auditor
func NewAuditor(config *AuditConfig, logger *slog.Logger) *Auditor {
	if config == nil {
		config = DefaultAuditConfig()
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Auditor{
		config: config,
		logger: logger,
	}
}

// StartQuery begins tracking a query execution and returns a completion function
func (a *Auditor) StartQuery(ctx context.Context, query string, analysis *QueryAnalysis) (*QueryAuditEntry, func(rowCount int, err error)) {
	if !a.config.Enabled {
		return nil, func(int, error) {}
	}

	// Create audit entry
	entry := &QueryAuditEntry{
		ID:         a.generateID(),
		Timestamp:  time.Now(),
		Query:      a.sanitizeQuery(query),
		QueryHash:  hashQuery(query),
		IsReadOnly: true, // Default to true
	}

	// Extract context information
	if requestID, ok := ctx.Value("request_id").(string); ok {
		entry.RequestID = requestID
	}
	if username, ok := ctx.Value("username").(string); ok {
		entry.Username = username
	}
	if sessionID, ok := ctx.Value("session_id").(string); ok {
		entry.SessionID = sessionID
	}
	if ipAddress, ok := ctx.Value("ip_address").(string); ok {
		entry.IPAddress = ipAddress
	}

	// Apply analysis if available
	if analysis != nil {
		entry.QueryType = analysis.QueryType
		entry.IsReadOnly = analysis.IsReadOnly
		for _, t := range analysis.Tables {
			tableName := t.Table
			if t.Schema != "" {
				tableName = t.Schema + "." + t.Table
			}
			entry.Tables = append(entry.Tables, tableName)
		}
	}

	start := time.Now()

	// Return completion function
	return entry, func(rowCount int, err error) {
		entry.Duration = time.Since(start)
		entry.RowCount = rowCount
		entry.Success = err == nil

		if err != nil {
			entry.ErrorMsg = err.Error()
			// Extract error code if it's a validation error
			if ve, ok := err.(*ValidationError); ok {
				entry.ErrorCode = ve.Code
			} else if ae, ok := err.(*ACLError); ok {
				entry.ErrorCode = ae.Code
			}
		}

		a.logEntry(entry)
	}
}

// LogQuery logs a completed query execution
func (a *Auditor) LogQuery(entry *QueryAuditEntry) {
	if !a.config.Enabled || entry == nil {
		return
	}
	a.logEntry(entry)
}

// logEntry writes the audit entry to the log
func (a *Auditor) logEntry(entry *QueryAuditEntry) {
	// Apply sampling
	if a.config.SampleRate < 1.0 && !a.shouldSample() {
		return
	}

	// Skip SELECT queries if not configured
	if !a.config.IncludeSelectQueries && entry.QueryType == QueryTypeSelect && entry.Success {
		return
	}

	// Build log attributes
	attrs := []any{
		"audit_id", entry.ID,
		"query_type", entry.QueryType.String(),
		"duration_ms", entry.Duration.Milliseconds(),
		"row_count", entry.RowCount,
		"success", entry.Success,
		"read_only", entry.IsReadOnly,
	}

	// Add optional attributes
	if entry.RequestID != "" {
		attrs = append(attrs, "request_id", entry.RequestID)
	}
	if entry.Username != "" {
		attrs = append(attrs, "username", entry.Username)
	}
	if entry.IPAddress != "" {
		attrs = append(attrs, "ip_address", entry.IPAddress)
	}
	if len(entry.Tables) > 0 {
		attrs = append(attrs, "tables", strings.Join(entry.Tables, ","))
	}
	if a.config.LogQueries {
		attrs = append(attrs, "query", entry.Query)
		attrs = append(attrs, "query_hash", entry.QueryHash)
	}
	if !entry.Success && entry.ErrorCode != "" {
		attrs = append(attrs, "error_code", entry.ErrorCode)
		attrs = append(attrs, "error", entry.ErrorMsg)
	}
	if len(entry.MaskedColumns) > 0 {
		attrs = append(attrs, "masked_columns", strings.Join(entry.MaskedColumns, ","))
	}

	// Determine log level
	level := a.config.LogLevel
	if !entry.Success {
		level = slog.LevelWarn
	}
	if a.config.IncludeSlowQueries && entry.Duration > a.config.SlowQueryThreshold {
		level = slog.LevelWarn
		attrs = append(attrs, "slow_query", true)
	}

	// Log the entry
	a.logger.Log(context.Background(), level, "query_audit", attrs...)

	// Call optional callback
	if a.config.OnAudit != nil {
		a.config.OnAudit(entry)
	}
}

// generateID generates a unique audit entry ID
func (a *Auditor) generateID() string {
	a.mu.Lock()
	a.counter++
	count := a.counter
	a.mu.Unlock()

	timestamp := time.Now().UnixNano()
	return fmt.Sprintf("%d-%d", timestamp, count)
}

// sanitizeQuery truncates and cleans the query for logging
func (a *Auditor) sanitizeQuery(query string) string {
	// Normalize whitespace
	query = strings.Join(strings.Fields(query), " ")

	// Truncate if needed
	if a.config.MaxQueryLength > 0 && len(query) > a.config.MaxQueryLength {
		query = query[:a.config.MaxQueryLength] + "...[truncated]"
	}

	return query
}

// shouldSample determines if this query should be sampled (for sampling rate < 1.0)
func (a *Auditor) shouldSample() bool {
	a.mu.Lock()
	a.counter++
	count := a.counter
	a.mu.Unlock()

	// Simple deterministic sampling based on counter
	sampleEvery := int(1.0 / a.config.SampleRate)
	if sampleEvery <= 0 {
		sampleEvery = 1
	}
	return count%uint64(sampleEvery) == 0
}

// SetEnabled enables or disables auditing
func (a *Auditor) SetEnabled(enabled bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.config.Enabled = enabled
}

// SetSampleRate sets the sampling rate (0.0-1.0)
func (a *Auditor) SetSampleRate(rate float64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if rate < 0 {
		rate = 0
	}
	if rate > 1 {
		rate = 1
	}
	a.config.SampleRate = rate
}

// hashQuery creates a SHA-256 hash of the query for deduplication/analysis
func hashQuery(query string) string {
	// Normalize the query before hashing
	normalized := strings.ToLower(strings.TrimSpace(query))
	hash := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(hash[:8]) // Use first 8 bytes for brevity
}

// AuditContext adds audit-related values to a context
func AuditContext(ctx context.Context, username, sessionID, ipAddress, requestID string) context.Context {
	if username != "" {
		ctx = context.WithValue(ctx, "username", username)
	}
	if sessionID != "" {
		ctx = context.WithValue(ctx, "session_id", sessionID)
	}
	if ipAddress != "" {
		ctx = context.WithValue(ctx, "ip_address", ipAddress)
	}
	if requestID != "" {
		ctx = context.WithValue(ctx, "request_id", requestID)
	}
	return ctx
}
