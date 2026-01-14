package sync

import (
	"context"
	"fmt"
)

// SyncStrategy defines the interface for data synchronization strategies.
// Each strategy implements synchronization logic for a specific data type
// (e.g., PostgreSQL, MySQL, Redis, MinIO).
type SyncStrategy interface {
	// Name returns the unique identifier for this strategy (e.g., "postgres", "mysql", "redis").
	Name() string

	// Type returns the category of data this strategy handles.
	Type() SyncType

	// EstimateSize calculates the approximate size of data to be synchronized.
	// This is used for progress tracking and time estimation.
	EstimateSize(ctx context.Context, source *Endpoint) (int64, error)

	// Sync performs the actual data synchronization from source to target.
	// Progress updates should be sent to the progress channel throughout the operation.
	// The channel should not be closed by the implementation; the caller manages it.
	Sync(ctx context.Context, source, target *Endpoint, progress chan<- Progress) error

	// Verify compares source and target data to ensure sync was successful.
	// This should be called after Sync to validate the data transfer.
	Verify(ctx context.Context, source, target *Endpoint) (*VerifyResult, error)

	// SupportsIncremental returns true if the strategy can perform incremental syncs.
	// Incremental syncs only transfer changed data since the last sync.
	SupportsIncremental() bool

	// SupportsResume returns true if the strategy can resume interrupted syncs.
	// This is important for large data transfers that may be interrupted.
	SupportsResume() bool
}

// VerifyResult contains the outcome of a data verification check.
type VerifyResult struct {
	// Valid indicates whether the source and target data match.
	Valid bool `json:"valid"`
	// SourceCount is the number of records/objects/bytes in the source.
	SourceCount int64 `json:"source_count"`
	// TargetCount is the number of records/objects/bytes in the target.
	TargetCount int64 `json:"target_count"`
	// Mismatches contains descriptions of any data that doesn't match.
	// For databases, this might be table names; for storage, object keys.
	Mismatches []string `json:"mismatches,omitempty"`
	// SourceChecksum is an optional checksum of the source data.
	SourceChecksum string `json:"source_checksum,omitempty"`
	// TargetChecksum is an optional checksum of the target data.
	TargetChecksum string `json:"target_checksum,omitempty"`
	// Details contains additional verification information.
	Details map[string]interface{} `json:"details,omitempty"`
}

// NewVerifyResult creates a new verification result.
func NewVerifyResult() *VerifyResult {
	return &VerifyResult{
		Mismatches: make([]string, 0),
		Details:    make(map[string]interface{}),
	}
}

// AddMismatch records a mismatch during verification.
func (v *VerifyResult) AddMismatch(description string) {
	v.Mismatches = append(v.Mismatches, description)
	v.Valid = false
}

// IsComplete returns true if source and target have matching counts.
func (v *VerifyResult) IsComplete() bool {
	return v.SourceCount == v.TargetCount
}

// MismatchCount returns the number of detected mismatches.
func (v *VerifyResult) MismatchCount() int {
	return len(v.Mismatches)
}

// String returns a human-readable summary of the verification result.
func (v *VerifyResult) String() string {
	if v.Valid {
		return fmt.Sprintf("Verification passed: %d/%d items synced", v.TargetCount, v.SourceCount)
	}
	return fmt.Sprintf("Verification failed: %d mismatches, %d/%d items synced",
		len(v.Mismatches), v.TargetCount, v.SourceCount)
}

// StrategyRegistry manages available sync strategies.
type StrategyRegistry struct {
	strategies map[string]SyncStrategy
}

// NewStrategyRegistry creates a new empty strategy registry.
func NewStrategyRegistry() *StrategyRegistry {
	return &StrategyRegistry{
		strategies: make(map[string]SyncStrategy),
	}
}

// Register adds a strategy to the registry.
func (r *StrategyRegistry) Register(strategy SyncStrategy) {
	r.strategies[strategy.Name()] = strategy
}

// Get returns a strategy by name, or nil if not found.
func (r *StrategyRegistry) Get(name string) SyncStrategy {
	return r.strategies[name]
}

// GetForEndpoint returns the appropriate strategy for an endpoint type.
// Returns nil if no matching strategy is found.
func (r *StrategyRegistry) GetForEndpoint(endpointType string) SyncStrategy {
	// Try exact match first
	if strategy, ok := r.strategies[endpointType]; ok {
		return strategy
	}
	// Try common aliases
	switch endpointType {
	case "postgresql":
		return r.strategies["postgres"]
	case "mariadb":
		return r.strategies["mysql"]
	case "valkey":
		return r.strategies["redis"]
	case "s3", "gcs", "azure-blob":
		return r.strategies["minio"]
	}
	return nil
}

// List returns all registered strategy names.
func (r *StrategyRegistry) List() []string {
	names := make([]string, 0, len(r.strategies))
	for name := range r.strategies {
		names = append(names, name)
	}
	return names
}

// ListByType returns all strategies that handle a specific sync type.
func (r *StrategyRegistry) ListByType(syncType SyncType) []SyncStrategy {
	var result []SyncStrategy
	for _, strategy := range r.strategies {
		if strategy.Type() == syncType {
			result = append(result, strategy)
		}
	}
	return result
}

// StrategyCapabilities describes what a sync strategy can do.
type StrategyCapabilities struct {
	// Name is the strategy identifier.
	Name string `json:"name"`
	// Type is the sync type (database, storage, cache).
	Type SyncType `json:"type"`
	// SupportsIncremental indicates incremental sync capability.
	SupportsIncremental bool `json:"supports_incremental"`
	// SupportsResume indicates resume capability for interrupted syncs.
	SupportsResume bool `json:"supports_resume"`
	// SupportedSources lists source endpoint types this strategy can handle.
	SupportedSources []string `json:"supported_sources"`
	// SupportedTargets lists target endpoint types this strategy can handle.
	SupportedTargets []string `json:"supported_targets"`
}

// GetCapabilities returns the capabilities of a registered strategy.
func (r *StrategyRegistry) GetCapabilities(name string) *StrategyCapabilities {
	strategy := r.strategies[name]
	if strategy == nil {
		return nil
	}
	return &StrategyCapabilities{
		Name:                strategy.Name(),
		Type:                strategy.Type(),
		SupportsIncremental: strategy.SupportsIncremental(),
		SupportsResume:      strategy.SupportsResume(),
	}
}

// BaseStrategy provides common functionality for sync strategies.
// Embed this in concrete strategy implementations.
type BaseStrategy struct {
	name                string
	syncType            SyncType
	supportsIncremental bool
	supportsResume      bool
}

// NewBaseStrategy creates a new base strategy with the given properties.
func NewBaseStrategy(name string, syncType SyncType, incremental, resume bool) *BaseStrategy {
	return &BaseStrategy{
		name:                name,
		syncType:            syncType,
		supportsIncremental: incremental,
		supportsResume:      resume,
	}
}

// Name returns the strategy name.
func (b *BaseStrategy) Name() string {
	return b.name
}

// Type returns the sync type.
func (b *BaseStrategy) Type() SyncType {
	return b.syncType
}

// SupportsIncremental returns whether incremental sync is supported.
func (b *BaseStrategy) SupportsIncremental() bool {
	return b.supportsIncremental
}

// SupportsResume returns whether resume is supported.
func (b *BaseStrategy) SupportsResume() bool {
	return b.supportsResume
}

// SyncOptions contains optional configuration for sync operations.
type SyncOptions struct {
	// Parallel specifies the number of parallel workers for sync.
	Parallel int `json:"parallel,omitempty"`
	// BatchSize specifies the number of records to process at once.
	BatchSize int `json:"batch_size,omitempty"`
	// Incremental enables incremental sync if the strategy supports it.
	Incremental bool `json:"incremental,omitempty"`
	// DryRun performs a simulation without actually transferring data.
	DryRun bool `json:"dry_run,omitempty"`
	// VerifyAfterSync automatically runs verification after sync completes.
	VerifyAfterSync bool `json:"verify_after_sync,omitempty"`
	// DeleteExtraneous removes items in target that don't exist in source.
	DeleteExtraneous bool `json:"delete_extraneous,omitempty"`
	// ChecksumVerify enables checksum verification during transfer.
	ChecksumVerify bool `json:"checksum_verify,omitempty"`
	// Timeout is the maximum duration for the sync operation.
	Timeout int64 `json:"timeout,omitempty"` // seconds
	// Bandwidth limits the transfer rate in bytes per second.
	Bandwidth int64 `json:"bandwidth,omitempty"`
	// ResumeFrom specifies a checkpoint to resume from.
	ResumeFrom string `json:"resume_from,omitempty"`
}

// DefaultSyncOptions returns sensible default options.
func DefaultSyncOptions() *SyncOptions {
	return &SyncOptions{
		Parallel:        4,
		BatchSize:       1000,
		Incremental:     false,
		DryRun:          false,
		VerifyAfterSync: true,
		DeleteExtraneous: false,
		ChecksumVerify:  true,
		Timeout:         3600, // 1 hour
		Bandwidth:       0,    // unlimited
	}
}
