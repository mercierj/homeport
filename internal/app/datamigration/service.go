// Package datamigration provides data migration functionality between cloud and self-hosted services.
package datamigration

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// MigrationStatus represents the current state of a migration job.
type MigrationStatus string

const (
	StatusPending   MigrationStatus = "pending"
	StatusValidating MigrationStatus = "validating"
	StatusRunning   MigrationStatus = "running"
	StatusCompleted MigrationStatus = "completed"
	StatusFailed    MigrationStatus = "failed"
	StatusCancelled MigrationStatus = "cancelled"
)

// EventType represents the type of migration event.
type EventType string

const (
	EventValidation EventType = "validation"
	EventPhase      EventType = "phase"
	EventProgress   EventType = "progress"
	EventLog        EventType = "log"
	EventComplete   EventType = "complete"
	EventError      EventType = "error"
)

// Event represents a migration event for SSE streaming.
type Event struct {
	Type EventType   `json:"type"`
	Data interface{} `json:"data"`
}

// ValidationEvent contains validation results.
type ValidationEvent struct {
	Valid   bool     `json:"valid"`
	Errors  []string `json:"errors,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// PhaseEvent contains phase progress information.
type PhaseEvent struct {
	Phase string `json:"phase"`
	Index int    `json:"index"`
	Total int    `json:"total"`
}

// ProgressEvent contains progress percentage.
type ProgressEvent struct {
	Percent int    `json:"percent"`
	Message string `json:"message,omitempty"`
}

// LogEvent contains log messages.
type LogEvent struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Message   string `json:"message"`
}

// CompleteEvent contains completion information.
type CompleteEvent struct {
	ItemsMigrated int               `json:"items_migrated"`
	Duration      string            `json:"duration"`
	Summary       map[string]int    `json:"summary"`
}

// ErrorEvent contains error information.
type ErrorEvent struct {
	Message     string `json:"message"`
	Phase       string `json:"phase,omitempty"`
	Recoverable bool   `json:"recoverable"`
}

// MigrationConfig represents the configuration for a data migration.
type MigrationConfig struct {
	Type        string                 `json:"type"`        // e.g., "s3_to_minio", "rds_to_postgres"
	Source      map[string]interface{} `json:"source"`      // Source connection details
	Destination map[string]interface{} `json:"destination"` // Destination connection details
	Options     map[string]interface{} `json:"options"`     // Migration-specific options
}

// Migration represents an active migration job.
type Migration struct {
	ID           string
	Status       MigrationStatus
	Config       *MigrationConfig
	CurrentPhase int
	TotalPhases  int
	Error        string
	StartTime    time.Time
	EndTime      time.Time
	subscribers  []chan Event
	mu           sync.RWMutex
	cancel       chan struct{}
	cancelled    bool
}

// Executor defines the interface for migration executors.
type Executor interface {
	// Type returns the migration type this executor handles.
	Type() string
	// Validate validates the migration configuration.
	Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error)
	// Execute performs the migration.
	Execute(ctx context.Context, m *Migration, config *MigrationConfig) error
	// GetPhases returns the list of phases for this migration type.
	GetPhases() []string
}

// ValidationResult contains the result of configuration validation.
type ValidationResult struct {
	Valid    bool     `json:"valid"`
	Errors   []string `json:"errors,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// Manager manages migration jobs.
type Manager struct {
	migrations map[string]*Migration
	mu         sync.RWMutex
}

// NewManager creates a new migration manager.
func NewManager() *Manager {
	return &Manager{
		migrations: make(map[string]*Migration),
	}
}

// CreateMigration creates a new migration job.
func (m *Manager) CreateMigration(config *MigrationConfig) *Migration {
	id := uuid.New().String()
	migration := &Migration{
		ID:          id,
		Status:      StatusPending,
		Config:      config,
		StartTime:   time.Now(),
		subscribers: make([]chan Event, 0),
		cancel:      make(chan struct{}),
	}
	m.mu.Lock()
	m.migrations[id] = migration
	m.mu.Unlock()
	return migration
}

// GetMigration retrieves a migration by ID.
func (m *Manager) GetMigration(id string) *Migration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.migrations[id]
}

// ListMigrations returns all migrations.
func (m *Manager) ListMigrations() []*Migration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := make([]*Migration, 0, len(m.migrations))
	for _, migration := range m.migrations {
		list = append(list, migration)
	}
	return list
}

// Subscribe creates a new event channel for the migration.
func (m *Migration) Subscribe() chan Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	ch := make(chan Event, 100)
	m.subscribers = append(m.subscribers, ch)
	return ch
}

// Unsubscribe removes an event channel.
func (m *Migration) Unsubscribe(ch chan Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, sub := range m.subscribers {
		if sub == ch {
			m.subscribers = append(m.subscribers[:i], m.subscribers[i+1:]...)
			close(ch)
			break
		}
	}
}

// Emit sends an event to all subscribers.
func (m *Migration) Emit(event Event) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, ch := range m.subscribers {
		select {
		case ch <- event:
		default: // drop if buffer full
		}
	}
}

// Cancel cancels the migration.
func (m *Migration) Cancel() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancelled {
		return
	}
	m.cancelled = true
	close(m.cancel)
	m.Status = StatusCancelled
}

// IsCancelled checks if the migration has been cancelled.
func (m *Migration) IsCancelled() bool {
	select {
	case <-m.cancel:
		return true
	default:
		return false
	}
}

// Service provides data migration functionality.
type Service struct {
	manager   *Manager
	executors map[string]Executor
	mu        sync.RWMutex
}

// NewService creates a new data migration service.
func NewService() *Service {
	s := &Service{
		manager:   NewManager(),
		executors: make(map[string]Executor),
	}
	// Register default executors

	// AWS Storage executors
	s.RegisterExecutor(NewS3ToMinIOExecutor())
	s.RegisterExecutor(NewEBSToLocalExecutor())
	s.RegisterExecutor(NewEFSToNFSExecutor())

	// AWS Database executors
	s.RegisterExecutor(NewRDSToPostgresExecutor())
	s.RegisterExecutor(NewDynamoDBToScyllaExecutor())
	s.RegisterExecutor(NewElastiCacheToRedisExecutor())

	// AWS Compute executors
	s.RegisterExecutor(NewECSToComposeExecutor())
	s.RegisterExecutor(NewLambdaToDockerExecutor())
	s.RegisterExecutor(NewEKSToK3sExecutor())

	// AWS Messaging executors
	s.RegisterExecutor(NewSQSToRabbitMQExecutor())
	s.RegisterExecutor(NewSNSToNATSExecutor())
	s.RegisterExecutor(NewEventBridgeToRabbitMQExecutor())
	s.RegisterExecutor(NewKinesisToRedpandaExecutor())
	s.RegisterExecutor(NewSESToMailhogExecutor())

	// AWS Security executors
	s.RegisterExecutor(NewCognitoToKeycloakExecutor())
	s.RegisterExecutor(NewSecretsToVaultExecutor())
	s.RegisterExecutor(NewKMSToVaultExecutor())
	s.RegisterExecutor(NewACMToLetsEncryptExecutor())

	// AWS Networking executors
	s.RegisterExecutor(NewAPIGatewayToTraefikExecutor())

	// AWS Monitoring executors
	s.RegisterExecutor(NewCloudWatchToPrometheusExecutor())

	// GCP Storage executors
	s.RegisterExecutor(NewGCSToMinIOExecutor())
	s.RegisterExecutor(NewFilestoreToNFSExecutor())
	s.RegisterExecutor(NewPersistentDiskToLocalExecutor())

	// GCP Security executors
	s.RegisterExecutor(NewSecretManagerToVaultExecutor())
	s.RegisterExecutor(NewIdentityPlatformToKeycloakExecutor())
	s.RegisterExecutor(NewCloudDNSToCoreDNSExecutor())

	// GCP Messaging executors
	s.RegisterExecutor(NewPubSubToRabbitMQExecutor())
	s.RegisterExecutor(NewCloudTasksToRabbitMQExecutor())

	// GCP Compute executors
	s.RegisterExecutor(NewCloudRunToDockerExecutor())
	s.RegisterExecutor(NewCloudFunctionsToOpenFaaSExecutor())
	s.RegisterExecutor(NewGKEToK3sExecutor())
	s.RegisterExecutor(NewAppEngineToDockerExecutor())

	// GCP Database executors
	s.RegisterExecutor(NewCloudSQLToPostgresExecutor())
	s.RegisterExecutor(NewFirestoreToMongoDBExecutor())
	s.RegisterExecutor(NewMemorystoreToRedisExecutor())
	s.RegisterExecutor(NewBigtableToScyllaExecutor())
	s.RegisterExecutor(NewSpannerToCockroachExecutor())

	// Azure Storage executors
	s.RegisterExecutor(NewBlobToMinIOExecutor())
	s.RegisterExecutor(NewFilesToNFSExecutor())
	s.RegisterExecutor(NewManagedDiskToLocalExecutor())

	// Azure Database executors
	s.RegisterExecutor(NewAzureSQLToPostgresExecutor())
	s.RegisterExecutor(NewCosmosDBToMongoDBExecutor())
	s.RegisterExecutor(NewAzureCacheToRedisExecutor())
	s.RegisterExecutor(NewAzureMySQLToMySQLExecutor())
	s.RegisterExecutor(NewAzurePostgresToPostgresExecutor())

	// Azure Compute executors
	s.RegisterExecutor(NewAppServiceToDockerExecutor())
	s.RegisterExecutor(NewFunctionsToOpenFaaSExecutor())
	s.RegisterExecutor(NewAKSToK3sExecutor())
	s.RegisterExecutor(NewACIToDockerExecutor())

	// Azure Messaging executors
	s.RegisterExecutor(NewServiceBusToRabbitMQExecutor())
	s.RegisterExecutor(NewEventHubToRedpandaExecutor())
	s.RegisterExecutor(NewEventGridToRabbitMQExecutor())

	// Azure Security executors
	s.RegisterExecutor(NewKeyVaultToVaultExecutor())
	s.RegisterExecutor(NewADB2CToKeycloakExecutor())
	s.RegisterExecutor(NewAzureDNSToCoreDNSExecutor())

	return s
}

// Manager returns the migration manager.
func (s *Service) Manager() *Manager {
	return s.manager
}

// RegisterExecutor registers a migration executor.
func (s *Service) RegisterExecutor(executor Executor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.executors[executor.Type()] = executor
}

// GetExecutor returns an executor for the given type.
func (s *Service) GetExecutor(migrationType string) (Executor, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	executor, ok := s.executors[migrationType]
	if !ok {
		return nil, fmt.Errorf("no executor registered for type: %s", migrationType)
	}
	return executor, nil
}

// ListExecutors returns all registered executor types.
func (s *Service) ListExecutors() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	types := make([]string, 0, len(s.executors))
	for t := range s.executors {
		types = append(types, t)
	}
	return types
}

// Validate validates a migration configuration.
func (s *Service) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	executor, err := s.GetExecutor(config.Type)
	if err != nil {
		return &ValidationResult{
			Valid:  false,
			Errors: []string{err.Error()},
		}, nil
	}
	return executor.Validate(ctx, config)
}

// Execute starts a new migration.
func (s *Service) Execute(ctx context.Context, config *MigrationConfig) (*Migration, error) {
	executor, err := s.GetExecutor(config.Type)
	if err != nil {
		return nil, err
	}

	// Create migration
	m := s.manager.CreateMigration(config)
	phases := executor.GetPhases()
	m.TotalPhases = len(phases)

	// Start migration in background
	go s.runMigration(m, executor)

	return m, nil
}

// runMigration executes the migration in a goroutine.
func (s *Service) runMigration(m *Migration, executor Executor) {
	m.mu.Lock()
	m.Status = StatusRunning
	m.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Monitor for cancellation
	go func() {
		<-m.cancel
		cancel()
	}()

	err := executor.Execute(ctx, m, m.Config)

	m.mu.Lock()
	m.EndTime = time.Now()
	if err != nil {
		m.Status = StatusFailed
		m.Error = err.Error()
		m.Emit(Event{
			Type: EventError,
			Data: ErrorEvent{
				Message:     err.Error(),
				Phase:       fmt.Sprintf("Phase %d", m.CurrentPhase),
				Recoverable: true,
			},
		})
	} else if m.Status != StatusCancelled {
		m.Status = StatusCompleted
		m.Emit(Event{
			Type: EventComplete,
			Data: CompleteEvent{
				Duration: m.EndTime.Sub(m.StartTime).String(),
			},
		})
	}
	m.mu.Unlock()

	// Close all subscriber channels
	m.mu.Lock()
	for _, ch := range m.subscribers {
		close(ch)
	}
	m.subscribers = nil
	m.mu.Unlock()
}

// CancelMigration cancels an ongoing migration.
func (s *Service) CancelMigration(id string) error {
	m := s.manager.GetMigration(id)
	if m == nil {
		return fmt.Errorf("migration not found: %s", id)
	}
	m.Cancel()
	return nil
}

// GetMigration retrieves a migration by ID.
func (s *Service) GetMigration(id string) *Migration {
	return s.manager.GetMigration(id)
}

// Helper functions for emitting events

// EmitPhase emits a phase update.
func EmitPhase(m *Migration, phase string, index int) {
	m.mu.Lock()
	m.CurrentPhase = index
	m.mu.Unlock()

	m.Emit(Event{
		Type: EventPhase,
		Data: PhaseEvent{
			Phase: phase,
			Index: index,
			Total: m.TotalPhases,
		},
	})
}

// EmitLog emits a log message.
func EmitLog(m *Migration, level, message string) {
	m.Emit(Event{
		Type: EventLog,
		Data: LogEvent{
			Timestamp: time.Now().Format(time.RFC3339),
			Level:     level,
			Message:   message,
		},
	})
}

// EmitProgress emits a progress update.
func EmitProgress(m *Migration, percent int, message string) {
	m.Emit(Event{
		Type: EventProgress,
		Data: ProgressEvent{
			Percent: percent,
			Message: message,
		},
	})
}
