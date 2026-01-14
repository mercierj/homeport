// Package functions provides serverless function management for Homeport.
package functions

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Supported runtimes for serverless functions.
const (
	RuntimeNodeJS20  = "nodejs20"
	RuntimeNodeJS18  = "nodejs18"
	RuntimePython311 = "python3.11"
	RuntimePython310 = "python3.10"
	RuntimeGo121     = "go1.21"
	RuntimeGo120     = "go1.20"
)

// SupportedRuntimes is a list of all supported function runtimes.
var SupportedRuntimes = []string{
	RuntimeNodeJS20,
	RuntimeNodeJS18,
	RuntimePython311,
	RuntimePython310,
	RuntimeGo121,
	RuntimeGo120,
}

// FunctionStatus represents the current status of a function.
type FunctionStatus string

const (
	StatusPending  FunctionStatus = "pending"
	StatusBuilding FunctionStatus = "building"
	StatusReady    FunctionStatus = "ready"
	StatusError    FunctionStatus = "error"
	StatusInactive FunctionStatus = "inactive"
)

// FunctionConfig holds the configuration for creating or updating a function.
type FunctionConfig struct {
	// Name is the unique name of the function
	Name string `json:"name"`
	// Runtime specifies the language runtime (e.g., nodejs20, python3.11)
	Runtime string `json:"runtime"`
	// Handler is the entry point for the function (e.g., index.handler)
	Handler string `json:"handler"`
	// MemoryMB is the memory allocation in megabytes (default: 128)
	MemoryMB int `json:"memory_mb,omitempty"`
	// TimeoutSeconds is the maximum execution time in seconds (default: 30)
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
	// Environment variables for the function
	Environment map[string]string `json:"environment,omitempty"`
	// SourceCode is the inline source code (for simple functions)
	SourceCode string `json:"source_code,omitempty"`
	// SourcePath is the path to the source code directory or archive
	SourcePath string `json:"source_path,omitempty"`
	// Description is an optional description of the function
	Description string `json:"description,omitempty"`
}

// FunctionInfo represents the details of a deployed function.
type FunctionInfo struct {
	// ID is the unique identifier for the function
	ID string `json:"id"`
	// Name is the function name
	Name string `json:"name"`
	// Runtime is the language runtime
	Runtime string `json:"runtime"`
	// Handler is the function entry point
	Handler string `json:"handler"`
	// MemoryMB is the memory allocation
	MemoryMB int `json:"memory_mb"`
	// TimeoutSeconds is the maximum execution time
	TimeoutSeconds int `json:"timeout_seconds"`
	// Environment variables
	Environment map[string]string `json:"environment,omitempty"`
	// Description is the function description
	Description string `json:"description,omitempty"`
	// Status is the current status of the function
	Status FunctionStatus `json:"status"`
	// LastInvoked is the timestamp of the last invocation
	LastInvoked *time.Time `json:"last_invoked,omitempty"`
	// InvocationCount is the total number of invocations
	InvocationCount int64 `json:"invocation_count"`
	// CreatedAt is when the function was created
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt is when the function was last updated
	UpdatedAt time.Time `json:"updated_at"`
}

// InvocationResult represents the result of a function invocation.
type InvocationResult struct {
	// RequestID is the unique identifier for this invocation
	RequestID string `json:"request_id"`
	// StatusCode is the HTTP-like status code (200 for success, 500 for error, etc.)
	StatusCode int `json:"status_code"`
	// Body is the response body from the function
	Body string `json:"body"`
	// DurationMS is the execution time in milliseconds
	DurationMS int64 `json:"duration_ms"`
	// Logs contains the function logs from this invocation
	Logs string `json:"logs,omitempty"`
	// Error contains any error message if the invocation failed
	Error string `json:"error,omitempty"`
}

// FunctionFilter provides filtering options for listing functions.
type FunctionFilter struct {
	// Runtime filters by runtime (optional)
	Runtime string `json:"runtime,omitempty"`
	// Status filters by status (optional)
	Status FunctionStatus `json:"status,omitempty"`
	// NamePrefix filters by name prefix (optional)
	NamePrefix string `json:"name_prefix,omitempty"`
}

// LogEntry represents a single log entry from a function.
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	RequestID string    `json:"request_id,omitempty"`
}

// Service manages serverless functions.
type Service struct {
	mu        sync.RWMutex
	functions map[string]*FunctionInfo
	logs      map[string][]LogEntry
}

// NewService creates a new function management service.
func NewService() (*Service, error) {
	return &Service{
		functions: make(map[string]*FunctionInfo),
		logs:      make(map[string][]LogEntry),
	}, nil
}

// ListFunctions returns all functions matching the optional filter.
func (s *Service) ListFunctions(ctx context.Context, filter *FunctionFilter) ([]FunctionInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]FunctionInfo, 0, len(s.functions))
	for _, fn := range s.functions {
		// Apply filters if provided
		if filter != nil {
			if filter.Runtime != "" && fn.Runtime != filter.Runtime {
				continue
			}
			if filter.Status != "" && fn.Status != filter.Status {
				continue
			}
			if filter.NamePrefix != "" && !hasPrefix(fn.Name, filter.NamePrefix) {
				continue
			}
		}
		result = append(result, *fn)
	}

	return result, nil
}

// GetFunction retrieves a function by its ID.
func (s *Service) GetFunction(ctx context.Context, id string) (*FunctionInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	fn, exists := s.functions[id]
	if !exists {
		return nil, fmt.Errorf("function not found: %s", id)
	}

	return fn, nil
}

// CreateFunction creates a new function with the given configuration.
func (s *Service) CreateFunction(ctx context.Context, config FunctionConfig) (*FunctionInfo, error) {
	// Validate configuration
	if err := validateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// Check for duplicate name
	s.mu.RLock()
	for _, fn := range s.functions {
		if fn.Name == config.Name {
			s.mu.RUnlock()
			return nil, fmt.Errorf("function with name '%s' already exists", config.Name)
		}
	}
	s.mu.RUnlock()

	// Apply defaults
	if config.MemoryMB <= 0 {
		config.MemoryMB = 128
	}
	if config.TimeoutSeconds <= 0 {
		config.TimeoutSeconds = 30
	}

	now := time.Now()
	fn := &FunctionInfo{
		ID:              uuid.New().String(),
		Name:            config.Name,
		Runtime:         config.Runtime,
		Handler:         config.Handler,
		MemoryMB:        config.MemoryMB,
		TimeoutSeconds:  config.TimeoutSeconds,
		Environment:     config.Environment,
		Description:     config.Description,
		Status:          StatusReady, // In a real implementation, this would start as StatusBuilding
		InvocationCount: 0,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	s.mu.Lock()
	s.functions[fn.ID] = fn
	s.logs[fn.ID] = []LogEntry{}
	s.mu.Unlock()

	// Add creation log entry
	s.addLogEntry(fn.ID, LogEntry{
		Timestamp: now,
		Level:     "info",
		Message:   fmt.Sprintf("Function '%s' created with runtime %s", config.Name, config.Runtime),
	})

	return fn, nil
}

// UpdateFunction updates an existing function with the given configuration.
func (s *Service) UpdateFunction(ctx context.Context, id string, config FunctionConfig) (*FunctionInfo, error) {
	// Validate configuration
	if err := validateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	fn, exists := s.functions[id]
	if !exists {
		return nil, fmt.Errorf("function not found: %s", id)
	}

	// Check for name conflict if name is changing
	if config.Name != fn.Name {
		for _, other := range s.functions {
			if other.ID != id && other.Name == config.Name {
				return nil, fmt.Errorf("function with name '%s' already exists", config.Name)
			}
		}
	}

	// Update function
	fn.Name = config.Name
	fn.Runtime = config.Runtime
	fn.Handler = config.Handler
	if config.MemoryMB > 0 {
		fn.MemoryMB = config.MemoryMB
	}
	if config.TimeoutSeconds > 0 {
		fn.TimeoutSeconds = config.TimeoutSeconds
	}
	if config.Environment != nil {
		fn.Environment = config.Environment
	}
	if config.Description != "" {
		fn.Description = config.Description
	}
	fn.UpdatedAt = time.Now()

	// Add update log entry
	s.addLogEntryLocked(fn.ID, LogEntry{
		Timestamp: fn.UpdatedAt,
		Level:     "info",
		Message:   fmt.Sprintf("Function '%s' updated", config.Name),
	})

	return fn, nil
}

// DeleteFunction removes a function by its ID.
func (s *Service) DeleteFunction(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	fn, exists := s.functions[id]
	if !exists {
		return fmt.Errorf("function not found: %s", id)
	}

	delete(s.functions, id)
	delete(s.logs, id)

	// Log deletion (we don't add to the function's logs since it's being deleted)
	_ = fn // Used for any cleanup in real implementation
	return nil
}

// InvokeFunction executes a function synchronously with the given payload.
func (s *Service) InvokeFunction(ctx context.Context, id string, payload []byte) (*InvocationResult, error) {
	s.mu.Lock()
	fn, exists := s.functions[id]
	if !exists {
		s.mu.Unlock()
		return nil, fmt.Errorf("function not found: %s", id)
	}

	if fn.Status != StatusReady {
		s.mu.Unlock()
		return nil, fmt.Errorf("function is not ready for invocation: status=%s", fn.Status)
	}

	// Update invocation count and last invoked time
	fn.InvocationCount++
	now := time.Now()
	fn.LastInvoked = &now
	s.mu.Unlock()

	requestID := uuid.New().String()
	startTime := time.Now()

	// Simulate function execution
	// In a real implementation, this would:
	// 1. Start a container with the appropriate runtime
	// 2. Pass the payload to the function
	// 3. Capture stdout/stderr
	// 4. Return the result
	result := &InvocationResult{
		RequestID:  requestID,
		StatusCode: 200,
		Body:       fmt.Sprintf(`{"message": "Function executed successfully", "input_size": %d}`, len(payload)),
		DurationMS: time.Since(startTime).Milliseconds() + 10, // Simulated execution time
		Logs:       fmt.Sprintf("[%s] Function invoked with %d bytes payload\n[%s] Execution completed", startTime.Format(time.RFC3339), len(payload), time.Now().Format(time.RFC3339)),
	}

	// Add invocation log entry
	s.addLogEntry(id, LogEntry{
		Timestamp: now,
		Level:     "info",
		Message:   fmt.Sprintf("Function invoked (request_id=%s, duration=%dms)", requestID, result.DurationMS),
		RequestID: requestID,
	})

	return result, nil
}

// GetFunctionLogs retrieves logs for a function since the specified time.
func (s *Service) GetFunctionLogs(ctx context.Context, id string, since *time.Time) ([]LogEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, exists := s.functions[id]; !exists {
		return nil, fmt.Errorf("function not found: %s", id)
	}

	logs, exists := s.logs[id]
	if !exists {
		return []LogEntry{}, nil
	}

	if since == nil {
		// Return all logs
		result := make([]LogEntry, len(logs))
		copy(result, logs)
		return result, nil
	}

	// Filter logs by timestamp
	var result []LogEntry
	for _, entry := range logs {
		if entry.Timestamp.After(*since) || entry.Timestamp.Equal(*since) {
			result = append(result, entry)
		}
	}

	return result, nil
}

// addLogEntry adds a log entry for a function (thread-safe).
func (s *Service) addLogEntry(functionID string, entry LogEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.addLogEntryLocked(functionID, entry)
}

// addLogEntryLocked adds a log entry for a function (must be called with lock held).
func (s *Service) addLogEntryLocked(functionID string, entry LogEntry) {
	if logs, exists := s.logs[functionID]; exists {
		s.logs[functionID] = append(logs, entry)
		// Keep only last 1000 log entries per function
		if len(s.logs[functionID]) > 1000 {
			s.logs[functionID] = s.logs[functionID][len(s.logs[functionID])-1000:]
		}
	}
}

// validateConfig validates a function configuration.
func validateConfig(config FunctionConfig) error {
	if config.Name == "" {
		return fmt.Errorf("name is required")
	}
	if config.Runtime == "" {
		return fmt.Errorf("runtime is required")
	}
	if !isValidRuntime(config.Runtime) {
		return fmt.Errorf("unsupported runtime: %s", config.Runtime)
	}
	if config.Handler == "" {
		return fmt.Errorf("handler is required")
	}
	if config.MemoryMB < 0 {
		return fmt.Errorf("memory_mb must be non-negative")
	}
	if config.MemoryMB > 10240 {
		return fmt.Errorf("memory_mb must be at most 10240 MB")
	}
	if config.TimeoutSeconds < 0 {
		return fmt.Errorf("timeout_seconds must be non-negative")
	}
	if config.TimeoutSeconds > 900 {
		return fmt.Errorf("timeout_seconds must be at most 900 seconds")
	}
	return nil
}

// isValidRuntime checks if a runtime is supported.
func isValidRuntime(runtime string) bool {
	for _, r := range SupportedRuntimes {
		if r == runtime {
			return true
		}
	}
	return false
}

// hasPrefix checks if s starts with prefix.
func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
