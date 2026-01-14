// Package sync defines domain types for data synchronization during migrations.
// It handles database, storage, and cache sync operations between cloud and self-hosted targets.
package sync

import (
	"errors"
	"fmt"
	"time"
)

// SyncType represents the type of data being synchronized.
type SyncType string

const (
	// SyncTypeDatabase indicates database synchronization (PostgreSQL, MySQL, etc.)
	SyncTypeDatabase SyncType = "database"
	// SyncTypeStorage indicates storage synchronization (S3, GCS, MinIO, etc.)
	SyncTypeStorage SyncType = "storage"
	// SyncTypeCache indicates cache synchronization (Redis, Memcached, etc.)
	SyncTypeCache SyncType = "cache"
)

// String returns the string representation of the sync type.
func (t SyncType) String() string {
	return string(t)
}

// IsValid checks if the sync type is a recognized type.
func (t SyncType) IsValid() bool {
	switch t {
	case SyncTypeDatabase, SyncTypeStorage, SyncTypeCache:
		return true
	default:
		return false
	}
}

// SyncStatus represents the current state of a sync task.
type SyncStatus string

const (
	// SyncStatusPending indicates the task is queued but not started.
	SyncStatusPending SyncStatus = "pending"
	// SyncStatusRunning indicates the task is currently executing.
	SyncStatusRunning SyncStatus = "running"
	// SyncStatusPaused indicates the task was paused by user or system.
	SyncStatusPaused SyncStatus = "paused"
	// SyncStatusCompleted indicates the task finished successfully.
	SyncStatusCompleted SyncStatus = "completed"
	// SyncStatusFailed indicates the task encountered an error.
	SyncStatusFailed SyncStatus = "failed"
)

// String returns the string representation of the sync status.
func (s SyncStatus) String() string {
	return string(s)
}

// IsTerminal returns true if the status is a final state (completed or failed).
func (s SyncStatus) IsTerminal() bool {
	return s == SyncStatusCompleted || s == SyncStatusFailed
}

// IsActive returns true if the task is currently running or pending.
func (s SyncStatus) IsActive() bool {
	return s == SyncStatusPending || s == SyncStatusRunning
}

// SyncPlan represents a complete data synchronization plan with multiple tasks.
type SyncPlan struct {
	// ID is the unique identifier for this sync plan.
	ID string `json:"id"`
	// Tasks contains all sync tasks to be executed.
	Tasks []*SyncTask `json:"tasks"`
	// TotalSize is the estimated total size in bytes to sync.
	TotalSize int64 `json:"total_size"`
	// Parallelism is the number of tasks that can run concurrently.
	Parallelism int `json:"parallelism"`
	// Order specifies the task execution order based on dependencies.
	// Task IDs should be executed in this order when dependencies matter.
	Order []string `json:"order"`
	// CreatedAt is when this plan was created.
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt is when this plan was last modified.
	UpdatedAt time.Time `json:"updated_at"`
	// BundleID links this sync plan to a specific migration bundle.
	BundleID string `json:"bundle_id,omitempty"`
}

// NewSyncPlan creates a new sync plan with default settings.
func NewSyncPlan(id string) *SyncPlan {
	now := time.Now()
	return &SyncPlan{
		ID:          id,
		Tasks:       make([]*SyncTask, 0),
		Parallelism: 1,
		Order:       make([]string, 0),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

// AddTask adds a sync task to the plan.
func (p *SyncPlan) AddTask(task *SyncTask) {
	p.Tasks = append(p.Tasks, task)
	p.Order = append(p.Order, task.ID)
	p.TotalSize += task.EstimatedSize
	p.UpdatedAt = time.Now()
}

// GetTask returns a task by ID, or nil if not found.
func (p *SyncPlan) GetTask(taskID string) *SyncTask {
	for _, task := range p.Tasks {
		if task.ID == taskID {
			return task
		}
	}
	return nil
}

// GetTasksByType returns all tasks of a specific type.
func (p *SyncPlan) GetTasksByType(syncType SyncType) []*SyncTask {
	var result []*SyncTask
	for _, task := range p.Tasks {
		if task.Type == syncType {
			result = append(result, task)
		}
	}
	return result
}

// GetTasksByStatus returns all tasks with a specific status.
func (p *SyncPlan) GetTasksByStatus(status SyncStatus) []*SyncTask {
	var result []*SyncTask
	for _, task := range p.Tasks {
		if task.Status == status {
			result = append(result, task)
		}
	}
	return result
}

// GetPendingTasks returns all tasks that haven't started yet.
func (p *SyncPlan) GetPendingTasks() []*SyncTask {
	return p.GetTasksByStatus(SyncStatusPending)
}

// GetActiveTasks returns all running or pending tasks.
func (p *SyncPlan) GetActiveTasks() []*SyncTask {
	var result []*SyncTask
	for _, task := range p.Tasks {
		if task.Status.IsActive() {
			result = append(result, task)
		}
	}
	return result
}

// IsComplete returns true if all tasks are in a terminal state.
func (p *SyncPlan) IsComplete() bool {
	for _, task := range p.Tasks {
		if !task.Status.IsTerminal() {
			return false
		}
	}
	return true
}

// HasFailed returns true if any task has failed.
func (p *SyncPlan) HasFailed() bool {
	for _, task := range p.Tasks {
		if task.Status == SyncStatusFailed {
			return true
		}
	}
	return false
}

// GetOverallProgress returns the aggregate progress across all tasks.
func (p *SyncPlan) GetOverallProgress() *Progress {
	if len(p.Tasks) == 0 {
		return nil
	}

	overall := &Progress{
		TaskID: p.ID,
	}

	for _, task := range p.Tasks {
		if task.Progress != nil {
			overall.BytesTotal += task.Progress.BytesTotal
			overall.BytesDone += task.Progress.BytesDone
			overall.ItemsTotal += task.Progress.ItemsTotal
			overall.ItemsDone += task.Progress.ItemsDone
		} else {
			overall.BytesTotal += task.EstimatedSize
		}
	}

	overall.UpdatedAt = time.Now()
	return overall
}

// SyncTask represents a single data synchronization operation.
type SyncTask struct {
	// ID is the unique identifier for this task.
	ID string `json:"id"`
	// Name is a human-readable name for the task.
	Name string `json:"name"`
	// Type indicates what kind of data is being synced.
	Type SyncType `json:"type"`
	// Source is the endpoint to sync data from.
	Source *Endpoint `json:"source"`
	// Target is the endpoint to sync data to.
	Target *Endpoint `json:"target"`
	// Strategy is the name of the sync strategy to use (e.g., "postgres", "mysql", "rclone").
	Strategy string `json:"strategy"`
	// Status is the current execution state.
	Status SyncStatus `json:"status"`
	// Progress contains real-time progress information.
	Progress *Progress `json:"progress,omitempty"`
	// Error contains the error if the task failed.
	Error error `json:"-"`
	// ErrorMessage is the error message for serialization.
	ErrorMessage string `json:"error,omitempty"`
	// EstimatedSize is the estimated size in bytes to sync.
	EstimatedSize int64 `json:"estimated_size"`
	// DependsOn lists task IDs that must complete before this task can start.
	DependsOn []string `json:"depends_on,omitempty"`
	// CreatedAt is when this task was created.
	CreatedAt time.Time `json:"created_at"`
	// StartedAt is when this task started executing.
	StartedAt *time.Time `json:"started_at,omitempty"`
	// CompletedAt is when this task finished (success or failure).
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	// RetryCount tracks how many times this task has been retried.
	RetryCount int `json:"retry_count"`
	// MaxRetries is the maximum number of retry attempts.
	MaxRetries int `json:"max_retries"`
}

// NewSyncTask creates a new sync task with default settings.
func NewSyncTask(id, name string, syncType SyncType, source, target *Endpoint) *SyncTask {
	return &SyncTask{
		ID:         id,
		Name:       name,
		Type:       syncType,
		Source:     source,
		Target:     target,
		Status:     SyncStatusPending,
		DependsOn:  make([]string, 0),
		CreatedAt:  time.Now(),
		MaxRetries: 3,
	}
}

// Start marks the task as running.
func (t *SyncTask) Start() {
	now := time.Now()
	t.Status = SyncStatusRunning
	t.StartedAt = &now
	t.Progress = &Progress{
		TaskID:    t.ID,
		StartedAt: now,
		UpdatedAt: now,
	}
}

// Complete marks the task as successfully completed.
func (t *SyncTask) Complete() {
	now := time.Now()
	t.Status = SyncStatusCompleted
	t.CompletedAt = &now
	if t.Progress != nil {
		t.Progress.UpdatedAt = now
	}
}

// Fail marks the task as failed with an error.
func (t *SyncTask) Fail(err error) {
	now := time.Now()
	t.Status = SyncStatusFailed
	t.CompletedAt = &now
	t.Error = err
	if err != nil {
		t.ErrorMessage = err.Error()
	}
}

// Pause marks the task as paused.
func (t *SyncTask) Pause() {
	t.Status = SyncStatusPaused
	if t.Progress != nil {
		t.Progress.UpdatedAt = time.Now()
	}
}

// Resume marks a paused task as running again.
func (t *SyncTask) Resume() {
	if t.Status == SyncStatusPaused {
		t.Status = SyncStatusRunning
		if t.Progress != nil {
			t.Progress.UpdatedAt = time.Now()
		}
	}
}

// CanRetry returns true if the task can be retried.
func (t *SyncTask) CanRetry() bool {
	return t.Status == SyncStatusFailed && t.RetryCount < t.MaxRetries
}

// Retry resets the task for another attempt.
func (t *SyncTask) Retry() error {
	if !t.CanRetry() {
		return errors.New("task cannot be retried: max retries exceeded or not in failed state")
	}
	t.RetryCount++
	t.Status = SyncStatusPending
	t.Error = nil
	t.ErrorMessage = ""
	t.StartedAt = nil
	t.CompletedAt = nil
	t.Progress = nil
	return nil
}

// Duration returns the elapsed time for the task.
// Returns zero if the task hasn't started.
func (t *SyncTask) Duration() time.Duration {
	if t.StartedAt == nil {
		return 0
	}
	end := time.Now()
	if t.CompletedAt != nil {
		end = *t.CompletedAt
	}
	return end.Sub(*t.StartedAt)
}

// Endpoint represents a data source or destination for sync operations.
type Endpoint struct {
	// Type identifies the endpoint type (e.g., "postgres", "mysql", "redis", "s3", "minio").
	Type string `json:"type"`
	// Host is the hostname or IP address.
	Host string `json:"host,omitempty"`
	// Port is the network port.
	Port int `json:"port,omitempty"`
	// Database is the database name (for database types).
	Database string `json:"database,omitempty"`
	// Bucket is the storage bucket name (for storage types).
	Bucket string `json:"bucket,omitempty"`
	// Path is a path prefix or specific path within the endpoint.
	Path string `json:"path,omitempty"`
	// Region is the cloud region (for cloud storage).
	Region string `json:"region,omitempty"`
	// Credentials contains authentication information.
	Credentials *Credentials `json:"credentials,omitempty"`
	// Options contains type-specific configuration options.
	Options map[string]string `json:"options,omitempty"`
	// SSL indicates whether to use SSL/TLS.
	SSL bool `json:"ssl,omitempty"`
	// SSLMode specifies the SSL mode (e.g., "require", "verify-full").
	SSLMode string `json:"ssl_mode,omitempty"`
}

// NewEndpoint creates a new endpoint with the specified type.
func NewEndpoint(endpointType string) *Endpoint {
	return &Endpoint{
		Type:    endpointType,
		Options: make(map[string]string),
	}
}

// ConnectionString builds a connection string for the endpoint.
// The format depends on the endpoint type.
func (e *Endpoint) ConnectionString() string {
	switch e.Type {
	case "postgres":
		return e.postgresConnectionString()
	case "mysql":
		return e.mysqlConnectionString()
	case "redis":
		return e.redisConnectionString()
	case "s3", "minio":
		return e.s3ConnectionString()
	default:
		return ""
	}
}

func (e *Endpoint) postgresConnectionString() string {
	// postgresql://user:password@host:port/database?sslmode=mode
	connStr := "postgresql://"
	if e.Credentials != nil && e.Credentials.Username != "" {
		connStr += e.Credentials.Username
		if e.Credentials.Password != "" {
			connStr += ":" + e.Credentials.Password
		}
		connStr += "@"
	}
	connStr += e.Host
	if e.Port > 0 {
		connStr += fmt.Sprintf(":%d", e.Port)
	}
	if e.Database != "" {
		connStr += "/" + e.Database
	}
	if e.SSLMode != "" {
		connStr += "?sslmode=" + e.SSLMode
	}
	return connStr
}

func (e *Endpoint) mysqlConnectionString() string {
	// user:password@tcp(host:port)/database
	var connStr string
	if e.Credentials != nil && e.Credentials.Username != "" {
		connStr = e.Credentials.Username
		if e.Credentials.Password != "" {
			connStr += ":" + e.Credentials.Password
		}
		connStr += "@"
	}
	connStr += "tcp(" + e.Host
	if e.Port > 0 {
		connStr += fmt.Sprintf(":%d", e.Port)
	}
	connStr += ")"
	if e.Database != "" {
		connStr += "/" + e.Database
	}
	return connStr
}

func (e *Endpoint) redisConnectionString() string {
	// redis://user:password@host:port/db
	connStr := "redis://"
	if e.Credentials != nil {
		if e.Credentials.Username != "" {
			connStr += e.Credentials.Username
		}
		if e.Credentials.Password != "" {
			connStr += ":" + e.Credentials.Password
		}
		if e.Credentials.Username != "" || e.Credentials.Password != "" {
			connStr += "@"
		}
	}
	connStr += e.Host
	if e.Port > 0 {
		connStr += fmt.Sprintf(":%d", e.Port)
	}
	if e.Database != "" {
		connStr += "/" + e.Database
	}
	return connStr
}

func (e *Endpoint) s3ConnectionString() string {
	// s3://bucket/path or minio://host:port/bucket/path
	if e.Type == "minio" {
		connStr := "minio://" + e.Host
		if e.Port > 0 {
			connStr += fmt.Sprintf(":%d", e.Port)
		}
		connStr += "/" + e.Bucket
		if e.Path != "" {
			connStr += "/" + e.Path
		}
		return connStr
	}
	// S3
	connStr := "s3://" + e.Bucket
	if e.Path != "" {
		connStr += "/" + e.Path
	}
	return connStr
}

// Credentials holds authentication information for endpoints.
type Credentials struct {
	// Username for database or service authentication.
	Username string `json:"username,omitempty"`
	// Password for authentication (should be retrieved from secrets manager in practice).
	Password string `json:"password,omitempty"`
	// AccessKey for cloud storage (AWS access key, MinIO access key).
	AccessKey string `json:"access_key,omitempty"`
	// SecretKey for cloud storage (AWS secret key, MinIO secret key).
	SecretKey string `json:"secret_key,omitempty"`
	// Token for bearer token authentication.
	Token string `json:"token,omitempty"`
	// SSHKey for SSH-based connections.
	SSHKey string `json:"ssh_key,omitempty"`
	// CertFile path to client certificate.
	CertFile string `json:"cert_file,omitempty"`
	// KeyFile path to client key.
	KeyFile string `json:"key_file,omitempty"`
	// CAFile path to CA certificate.
	CAFile string `json:"ca_file,omitempty"`
}

// HasBasicAuth returns true if username/password credentials are set.
func (c *Credentials) HasBasicAuth() bool {
	return c != nil && c.Username != "" && c.Password != ""
}

// HasAccessKeys returns true if access key credentials are set.
func (c *Credentials) HasAccessKeys() bool {
	return c != nil && c.AccessKey != "" && c.SecretKey != ""
}

// HasCertAuth returns true if certificate-based auth is configured.
func (c *Credentials) HasCertAuth() bool {
	return c != nil && c.CertFile != "" && c.KeyFile != ""
}
