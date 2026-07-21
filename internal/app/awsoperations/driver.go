package awsoperations

import (
	"context"
	"errors"
	"time"
)

var (
	ErrServiceUnavailable    = errors.New("AWS operations service unavailable")
	ErrCapabilityUnavailable = errors.New("AWS operations capability unavailable")
	ErrResourceNotBound      = errors.New("AWS operations resource is not bound")
)

type Driver interface {
	Service() ServiceKey
	List(context.Context, Workspace) ([]any, error)
	Capabilities(Workspace) []Capability
}

type FunctionInput struct {
	Name, Runtime, Handler   string
	MemoryMB, TimeoutSeconds int
	Environment              map[string]string
	Description              string
}
type FunctionRecord struct {
	ID                 string            `json:"id"`
	Name               string            `json:"name"`
	Runtime            string            `json:"runtime"`
	Handler            string            `json:"handler"`
	MemoryMB           int               `json:"memory_mb"`
	TimeoutSeconds     int               `json:"timeout_seconds"`
	Environment        map[string]string `json:"environment,omitempty"`
	Description        string            `json:"description,omitempty"`
	Status             string            `json:"status"`
	InvocationCount    int64             `json:"invocation_count"`
	LastInvoked        *time.Time        `json:"last_invoked,omitempty"`
	CreatedAt          time.Time         `json:"created_at"`
	UpdatedAt          time.Time         `json:"updated_at"`
	ImportedResourceID string            `json:"imported_resource_id"`
	Region             string            `json:"region,omitempty"`
	Tags               map[string]string `json:"tags,omitempty"`
	LocalStackID       string            `json:"local_stack_id"`
}
type InvocationRecord struct {
	RequestID  string `json:"request_id"`
	StatusCode int    `json:"status_code"`
	Body       string `json:"body"`
	DurationMS int64  `json:"duration_ms"`
	Logs       string `json:"logs,omitempty"`
	Error      string `json:"error,omitempty"`
}
type LogRecord struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	RequestID string    `json:"request_id,omitempty"`
}
type QueueRecord struct {
	Name               string            `json:"name"`
	PendingCount       int64             `json:"pending_count"`
	ActiveCount        int64             `json:"active_count"`
	CompletedCount     int64             `json:"completed_count"`
	FailedCount        int64             `json:"failed_count"`
	TotalCount         int64             `json:"total_count"`
	CreatedAt          time.Time         `json:"created_at,omitempty"`
	UpdatedAt          time.Time         `json:"updated_at,omitempty"`
	ImportedResourceID string            `json:"imported_resource_id"`
	Region             string            `json:"region,omitempty"`
	Tags               map[string]string `json:"tags,omitempty"`
	LocalStackID       string            `json:"local_stack_id"`
}
type MessageRecord struct {
	ID          string         `json:"id"`
	QueueName   string         `json:"queue_name"`
	Status      string         `json:"status"`
	Data        map[string]any `json:"data,omitempty"`
	Attempts    int            `json:"attempts"`
	MaxAttempts int            `json:"max_attempts"`
	Error       string         `json:"error,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	ProcessedAt *time.Time     `json:"processed_at,omitempty"`
	CompletedAt *time.Time     `json:"completed_at,omitempty"`
	FailedAt    *time.Time     `json:"failed_at,omitempty"`
}
type FunctionsBackend interface {
	List(context.Context) ([]FunctionRecord, error)
	Get(context.Context, string) (*FunctionRecord, error)
	Create(context.Context, FunctionInput) (*FunctionRecord, error)
	Update(context.Context, string, FunctionInput) (*FunctionRecord, error)
	Delete(context.Context, string) error
	Invoke(context.Context, string, []byte) (*InvocationRecord, error)
	Logs(context.Context, string) ([]LogRecord, error)
}
type QueuesBackend interface {
	List(context.Context, string) ([]QueueRecord, error)
	Messages(context.Context, string, string, string) ([]MessageRecord, error)
	Retry(context.Context, string, string, string) error
	Delete(context.Context, string, string, string) error
	Purge(context.Context, string, string, string) (int64, error)
}

func serviceState(w Workspace, service ServiceKey, capability Capability) (ServiceState, error) {
	state, ok := w.Services[service]
	if !ok || state.Status != ServiceStatusAvailable {
		return ServiceState{}, ErrServiceUnavailable
	}
	for _, current := range state.Capabilities {
		if current == capability {
			return state, nil
		}
	}
	return ServiceState{}, ErrCapabilityUnavailable
}

func serviceBinding(w Workspace, service ServiceKey, id string, capability Capability) (ResourceBinding, error) {
	if _, err := serviceState(w, service, capability); err != nil {
		return ResourceBinding{}, err
	}
	for _, b := range w.Bindings {
		if b.Service == service && b.LocalResourceID == id {
			return b, nil
		}
	}
	return ResourceBinding{}, ErrResourceNotBound
}

func bindingsFor(w Workspace, service ServiceKey) []ResourceBinding {
	bindings := make([]ResourceBinding, 0)
	for _, binding := range w.Bindings {
		if binding.Service == service {
			bindings = append(bindings, binding)
		}
	}
	return bindings
}
