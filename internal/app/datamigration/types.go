package datamigration

import "time"

// ============================================================================
// Service Configuration Types
// These extend the existing MigrationConfig with structured service configs.
// ============================================================================

// DatabaseConfig holds configuration for database migration.
type DatabaseConfig struct {
	Enabled              bool     `json:"enabled"`
	SourceType           string   `json:"sourceType"` // rds, aurora, dynamodb, documentdb
	ConnectionString     string   `json:"connectionString"`
	Database             string   `json:"database"`
	Tables               []string `json:"tables"`
	BatchSize            int      `json:"batchSize"`
	ParallelWorkers      int      `json:"parallelWorkers"`
	IncludeSchema        bool     `json:"includeSchema"`
	TruncateBeforeImport bool     `json:"truncateBeforeImport"`
}

// BucketMigration defines a single bucket migration mapping.
type BucketMigration struct {
	SourceBucket string `json:"sourceBucket"`
	TargetBucket string `json:"targetBucket"`
	Prefix       string `json:"prefix,omitempty"`
}

// StorageConfig holds configuration for storage migration.
type StorageConfig struct {
	Enabled          bool              `json:"enabled"`
	SourceType       string            `json:"sourceType"` // s3, azure-blob, gcs
	Buckets          []BucketMigration `json:"buckets"`
	PreserveMetadata bool              `json:"preserveMetadata"`
	PreserveVersions bool              `json:"preserveVersions"`
	FilterPattern    string            `json:"filterPattern,omitempty"`
	ExcludePattern   string            `json:"excludePattern,omitempty"`
}

// QueueMigration defines a single queue migration mapping.
type QueueMigration struct {
	SourceQueue    string `json:"sourceQueue"`
	TargetQueue    string `json:"targetQueue"`
	TargetExchange string `json:"targetExchange,omitempty"`
}

// QueueConfig holds configuration for queue/messaging migration.
type QueueConfig struct {
	Enabled                   bool             `json:"enabled"`
	SourceType                string           `json:"sourceType"` // sqs, sns, eventbridge
	Queues                    []QueueMigration `json:"queues"`
	MigrateDeadLetterQueues   bool             `json:"migrateDeadLetterQueues"`
	PreserveMessageAttributes bool             `json:"preserveMessageAttributes"`
}

// CacheConfig holds configuration for cache migration.
type CacheConfig struct {
	Enabled         bool   `json:"enabled"`
	SourceType      string `json:"sourceType"` // elasticache-redis, elasticache-memcached, azure-cache
	Endpoint        string `json:"endpoint"`
	Databases       []int  `json:"databases"`
	KeyPattern      string `json:"keyPattern,omitempty"`
	ExcludePattern  string `json:"excludePattern,omitempty"`
	TTLPreservation bool   `json:"ttlPreservation"`
}

// AuthConfig holds configuration for authentication/identity migration.
type AuthConfig struct {
	Enabled        bool   `json:"enabled"`
	SourceType     string `json:"sourceType"` // cognito, azure-ad, firebase-auth
	UserPoolID     string `json:"userPoolId"`
	MigrateUsers   bool   `json:"migrateUsers"`
	MigrateGroups  bool   `json:"migrateGroups"`
	MigrateRoles   bool   `json:"migrateRoles"`
	PasswordPolicy string `json:"passwordPolicy"` // reset, preserve-hash, send-reset-email
	MFAHandling    string `json:"mfaHandling"`    // disable, preserve, require-reconfigure
}

// SecretsConfig holds configuration for secrets migration.
type SecretsConfig struct {
	Enabled     bool     `json:"enabled"`
	SourceType  string   `json:"sourceType"` // secrets-manager, ssm-parameter-store, azure-keyvault
	SecretPaths []string `json:"secretPaths"`
	TargetPath  string   `json:"targetPath"`
	Encryption  bool     `json:"encryption"`
}

// DNSConfig holds configuration for DNS migration.
type DNSConfig struct {
	Enabled        bool     `json:"enabled"`
	SourceType     string   `json:"sourceType"` // route53, azure-dns, cloud-dns
	HostedZones    []string `json:"hostedZones"`
	ExportFormat   string   `json:"exportFormat"` // zone-file, json
	TargetProvider string   `json:"targetProvider,omitempty"`
}

// FunctionMigration defines a single function migration mapping.
type FunctionMigration struct {
	FunctionARN         string `json:"functionArn"`
	FunctionName        string `json:"functionName"`
	TargetContainerName string `json:"targetContainerName"`
	Runtime             string `json:"runtime"`
}

// FunctionsConfig holds configuration for serverless function migration.
type FunctionsConfig struct {
	Enabled                     bool                `json:"enabled"`
	SourceType                  string              `json:"sourceType"` // lambda, azure-functions, cloud-functions
	Functions                   []FunctionMigration `json:"functions"`
	IncludeEnvironmentVariables bool                `json:"includeEnvironmentVariables"`
	IncludeLayers               bool                `json:"includeLayers"`
}

// ============================================================================
// Source Credentials
// ============================================================================

// SourceCredentials holds cloud provider credentials.
type SourceCredentials struct {
	Provider string `json:"provider"` // aws, gcp, azure

	// AWS
	AWSAccessKeyID     string `json:"awsAccessKeyId,omitempty"`
	AWSSecretAccessKey string `json:"awsSecretAccessKey,omitempty"`
	AWSRegion          string `json:"awsRegion,omitempty"`

	// GCP
	GCPProjectID          string `json:"gcpProjectId,omitempty"`
	GCPServiceAccountJSON string `json:"gcpServiceAccountJson,omitempty"`

	// Azure
	AzureSubscriptionID string `json:"azureSubscriptionId,omitempty"`
	AzureTenantID       string `json:"azureTenantId,omitempty"`
	AzureClientID       string `json:"azureClientId,omitempty"`
	AzureClientSecret   string `json:"azureClientSecret,omitempty"`
}

// ============================================================================
// Migration Options
// ============================================================================

// MigrationOptions holds global migration settings.
type MigrationOptions struct {
	DryRun               bool   `json:"dryRun"`
	ContinueOnError      bool   `json:"continueOnError"`
	LogLevel             string `json:"logLevel"` // debug, info, warn, error
	MaxConcurrentTasks   int    `json:"maxConcurrentTasks"`
	VerifyAfterMigration bool   `json:"verifyAfterMigration"`
	CreateBackup         bool   `json:"createBackup"`
}

// ============================================================================
// Aggregate Configuration
// ============================================================================

// MigrationConfiguration is the complete configuration for a data migration.
// This is the wizard-based configuration that gets converted to MigrationConfig.
type MigrationConfiguration struct {
	SourceCredentials SourceCredentials `json:"sourceCredentials"`
	Database          DatabaseConfig    `json:"database"`
	Storage           StorageConfig     `json:"storage"`
	Queue             QueueConfig       `json:"queue"`
	Cache             CacheConfig       `json:"cache"`
	Auth              AuthConfig        `json:"auth"`
	Secrets           SecretsConfig     `json:"secrets"`
	DNS               DNSConfig         `json:"dns"`
	Functions         FunctionsConfig   `json:"functions"`
	Options           MigrationOptions  `json:"options"`
}

// ============================================================================
// Category Type
// ============================================================================

// MigrationCategory represents a category of data to migrate.
type MigrationCategory string

const (
	CategoryDatabase  MigrationCategory = "database"
	CategoryStorage   MigrationCategory = "storage"
	CategoryQueue     MigrationCategory = "queue"
	CategoryCache     MigrationCategory = "cache"
	CategoryAuth      MigrationCategory = "auth"
	CategorySecrets   MigrationCategory = "secrets"
	CategoryDNS       MigrationCategory = "dns"
	CategoryFunctions MigrationCategory = "functions"
)

// AllCategories returns all migration categories in order.
func AllCategories() []MigrationCategory {
	return []MigrationCategory{
		CategoryDatabase,
		CategoryStorage,
		CategoryQueue,
		CategoryCache,
		CategoryAuth,
		CategorySecrets,
		CategoryDNS,
		CategoryFunctions,
	}
}

// ============================================================================
// Extended Validation Types (extends ValidationResult from service.go)
// ============================================================================

// ValidateMigrationRequest is the request to validate a migration configuration.
type ValidateMigrationRequest struct {
	Configuration MigrationConfiguration `json:"configuration"`
}

// ValidationFieldError represents a validation error for a specific field.
type ValidationFieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
	Code    string `json:"code"`
}

// ValidationFieldWarning represents a validation warning for a specific field.
type ValidationFieldWarning struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// CategoryValidationResult represents the validation result for a category.
type CategoryValidationResult struct {
	Category MigrationCategory        `json:"category"`
	Valid    bool                     `json:"valid"`
	Errors   []ValidationFieldError   `json:"errors"`
	Warnings []ValidationFieldWarning `json:"warnings"`
}

// ValidateMigrationResponse is the response from migration validation.
type ValidateMigrationResponse struct {
	Valid             bool                       `json:"valid"`
	Results           []CategoryValidationResult `json:"results"`
	EstimatedDuration string                     `json:"estimatedDuration"`
	EstimatedDataSize string                     `json:"estimatedDataSize"`
}

// ============================================================================
// Execution Types
// ============================================================================

// ExecuteMigrationRequest is the request to start a migration.
type ExecuteMigrationRequest struct {
	Configuration MigrationConfiguration `json:"configuration"`
}

// ExecuteMigrationResponse is the response when starting a migration.
type ExecuteMigrationResponse struct {
	MigrationID string `json:"migrationId"`
	Status      string `json:"status"`
}

// ============================================================================
// Progress Types
// ============================================================================

// CategoryProgress represents progress for a specific category.
type CategoryProgress struct {
	Status           MigrationStatus `json:"status"`
	Progress         float64         `json:"progress"`
	ItemsTotal       int64           `json:"itemsTotal"`
	ItemsCompleted   int64           `json:"itemsCompleted"`
	BytesTotal       int64           `json:"bytesTotal"`
	BytesTransferred int64           `json:"bytesTransferred"`
	CurrentItem      string          `json:"currentItem,omitempty"`
	Errors           []string        `json:"errors"`
}

// MigrationProgress represents the overall progress of a migration.
type MigrationProgress struct {
	MigrationID         string                               `json:"migrationId"`
	Status              MigrationStatus                      `json:"status"`
	OverallProgress     float64                              `json:"overallProgress"`
	CurrentCategory     MigrationCategory                    `json:"currentCategory,omitempty"`
	CategoryProgress    map[MigrationCategory]*CategoryProgress `json:"categoryProgress"`
	StartedAt           time.Time                            `json:"startedAt"`
	EstimatedCompletion *time.Time                           `json:"estimatedCompletion,omitempty"`
	Error               string                               `json:"error,omitempty"`
}

// ============================================================================
// Extended SSE Event Types (for category-aware events)
// ============================================================================

// CategoryPhaseEvent is sent when entering a new migration phase for a category.
type CategoryPhaseEvent struct {
	Category MigrationCategory `json:"category"`
	Phase    string            `json:"phase"`
	Index    int               `json:"index"`
	Total    int               `json:"total"`
}

// CategoryProgressEvent is sent to update progress within a category.
type CategoryProgressEvent struct {
	Category         MigrationCategory `json:"category"`
	Progress         float64           `json:"progress"`
	ItemsCompleted   int64             `json:"itemsCompleted"`
	ItemsTotal       int64             `json:"itemsTotal"`
	BytesTransferred int64             `json:"bytesTransferred"`
	BytesTotal       int64             `json:"bytesTotal"`
	CurrentItem      string            `json:"currentItem,omitempty"`
}

// CategoryLogEvent is sent for log messages during migration.
type CategoryLogEvent struct {
	Timestamp time.Time         `json:"timestamp"`
	Level     string            `json:"level"` // debug, info, warn, error
	Category  MigrationCategory `json:"category,omitempty"`
	Message   string            `json:"message"`
}

// MigrationErrorSummary represents an error that occurred during migration.
type MigrationErrorSummary struct {
	Category MigrationCategory `json:"category"`
	Item     string            `json:"item"`
	Error    string            `json:"error"`
}

// MigrationSummary is the summary of a completed migration.
type MigrationSummary struct {
	CategoriesCompleted []MigrationCategory     `json:"categoriesCompleted"`
	TotalItemsMigrated  int64                   `json:"totalItemsMigrated"`
	TotalBytesMigrated  int64                   `json:"totalBytesMigrated"`
	Errors              []MigrationErrorSummary `json:"errors"`
	Warnings            []string                `json:"warnings"`
}

// MigrationCompleteEvent is sent when migration completes.
type MigrationCompleteEvent struct {
	MigrationID string           `json:"migrationId"`
	Duration    string           `json:"duration"`
	Summary     MigrationSummary `json:"summary"`
}

// MigrationErrorEvent is sent when an error occurs.
type MigrationErrorEvent struct {
	MigrationID string            `json:"migrationId"`
	Category    MigrationCategory `json:"category,omitempty"`
	Message     string            `json:"message"`
	Recoverable bool              `json:"recoverable"`
}

// ============================================================================
// Cancel Types
// ============================================================================

// CancelMigrationRequest is the request to cancel a running migration.
type CancelMigrationRequest struct {
	MigrationID string `json:"migrationId"`
	Graceful    bool   `json:"graceful"`
}

// CancelMigrationResponse is the response from cancelling a migration.
type CancelMigrationResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// ============================================================================
// Status Types
// ============================================================================

// GetMigrationStatusRequest is the request to get migration status.
type GetMigrationStatusRequest struct {
	MigrationID string `json:"migrationId"`
}

// GetMigrationStatusResponse is the response with migration status.
// Alias for MigrationProgress for clarity.
type GetMigrationStatusResponse = MigrationProgress
