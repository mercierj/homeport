package cli

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/homeport/homeport/internal/cli/ui"
	"github.com/homeport/homeport/internal/domain/sync"
	bundledomain "github.com/homeport/homeport/internal/domain/bundle"
	bundleinfra "github.com/homeport/homeport/internal/infrastructure/bundle"
	syncinfra "github.com/homeport/homeport/internal/infrastructure/sync"
	"github.com/spf13/cobra"
)

var (
	syncBundlePath  string
	syncType        string
	syncSource      string
	syncTarget      string
	syncContinuous  bool
	syncVerifyOnly  bool
	syncParallel    int
	syncDryRun      bool
)

// syncCmd represents the sync command
var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Synchronize data between cloud and self-hosted infrastructure",
	Long: `Synchronize data between cloud sources and self-hosted Docker targets.

The sync command handles data migration for databases, storage, and caches
as defined in a migration bundle (.hprt file).

Supported sync types:
  - database: PostgreSQL, MySQL database synchronization
  - storage:  S3, MinIO, GCS object storage synchronization
  - cache:    Redis, Valkey cache synchronization

Examples:
  # Sync all data defined in bundle
  homeport sync --bundle migration.hprt

  # Sync specific resource types
  homeport sync --bundle migration.hprt --type database

  # Sync with source/target override
  homeport sync \
    --source "postgres://aws-rds:5432/mydb" \
    --target "postgres://localhost:5432/mydb"

  # Continuous sync (CDC mode for databases)
  homeport sync --bundle migration.hprt --continuous

  # Verify only (no sync)
  homeport sync --bundle migration.hprt --verify-only

  # Dry run - show what would be synced
  homeport sync --bundle migration.hprt --dry-run

  # Parallel sync with 8 workers
  homeport sync --bundle migration.hprt --parallel 8`,
	RunE: runSync,
}

func init() {
	rootCmd.AddCommand(syncCmd)

	syncCmd.Flags().StringVarP(&syncBundlePath, "bundle", "b", "", "path to migration bundle (.hprt file)")
	syncCmd.Flags().StringVarP(&syncType, "type", "t", "", "sync type filter (database, storage, cache)")
	syncCmd.Flags().StringVar(&syncSource, "source", "", "source connection string (overrides bundle)")
	syncCmd.Flags().StringVar(&syncTarget, "target", "", "target connection string (overrides bundle)")
	syncCmd.Flags().BoolVarP(&syncContinuous, "continuous", "c", false, "enable continuous sync (CDC mode)")
	syncCmd.Flags().BoolVar(&syncVerifyOnly, "verify-only", false, "verify data without syncing")
	syncCmd.Flags().IntVarP(&syncParallel, "parallel", "p", 4, "number of parallel sync workers")
	syncCmd.Flags().BoolVar(&syncDryRun, "dry-run", false, "show what would be synced without executing")
}

func runSync(cmd *cobra.Command, args []string) error {
	// Validate inputs
	if syncBundlePath == "" && (syncSource == "" || syncTarget == "") {
		return fmt.Errorf("either --bundle or both --source and --target are required")
	}

	if syncType != "" && !isValidSyncType(syncType) {
		return fmt.Errorf("invalid sync type: %s (must be 'database', 'storage', or 'cache')", syncType)
	}

	if !IsQuiet() {
		ui.Header("Homeport - Data Synchronization")
		ui.Divider()
	}

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		if !IsQuiet() {
			ui.Warning("Received interrupt signal, gracefully stopping...")
		}
		cancel()
	}()

	// Build sync plan
	var plan *sync.SyncPlan
	var err error

	if syncBundlePath != "" {
		plan, err = buildPlanFromBundle(syncBundlePath, syncType)
		if err != nil {
			return fmt.Errorf("failed to build sync plan from bundle: %w", err)
		}
	} else {
		plan, err = buildPlanFromEndpoints(syncSource, syncTarget)
		if err != nil {
			return fmt.Errorf("failed to build sync plan: %w", err)
		}
	}

	if plan == nil || len(plan.Tasks) == 0 {
		ui.Info("No sync tasks found")
		return nil
	}

	// Set parallelism
	plan.Parallelism = syncParallel

	// Display sync plan
	if !IsQuiet() {
		displaySyncPlan(plan)
	}

	// Dry run - just show the plan
	if syncDryRun {
		if !IsQuiet() {
			ui.Info("Dry run mode - no changes will be made")
		}
		return nil
	}

	// Verify only mode
	if syncVerifyOnly {
		return runVerifyOnly(ctx, plan)
	}

	// Run sync
	if syncContinuous {
		return runContinuousSync(ctx, plan)
	}

	return runOneTimeSync(ctx, plan)
}

// isValidSyncType checks if the provided type is valid
func isValidSyncType(t string) bool {
	switch strings.ToLower(t) {
	case "database", "storage", "cache":
		return true
	default:
		return false
	}
}

// buildPlanFromBundle creates a sync plan from a migration bundle
func buildPlanFromBundle(bundlePath, filterType string) (*sync.SyncPlan, error) {
	// Verify bundle exists
	if _, err := os.Stat(bundlePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("bundle file not found: %s", bundlePath)
	}

	// Extract manifest from bundle
	extractor := bundleinfra.NewExtractor()
	manifest, err := extractor.GetManifest(bundlePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read bundle manifest: %w", err)
	}

	if !IsQuiet() {
		ui.Info(fmt.Sprintf("Bundle: %s", bundlePath))
		if manifest.Source != nil {
			ui.Info(fmt.Sprintf("Source provider: %s", manifest.Source.Provider))
		}
		if manifest.Target != nil {
			ui.Info(fmt.Sprintf("Target: %s", manifest.Target.Type))
		}
	}

	// Check if bundle has data sync info
	if manifest.DataSync == nil || len(manifest.DataSync.Tasks) == 0 {
		if !manifest.HasDataSync() {
			return nil, nil // No sync tasks in bundle
		}
		// Create default tasks based on stacks that need sync
		return buildDefaultPlanFromStacks(manifest, filterType)
	}

	// Build plan from manifest sync tasks
	plan := sync.NewSyncPlan(fmt.Sprintf("sync-%d", time.Now().Unix()))
	plan.BundleID = bundlePath

	for _, task := range manifest.DataSync.Tasks {
		// Apply type filter if specified
		if filterType != "" && !strings.EqualFold(task.Type, filterType) {
			continue
		}

		syncTask := convertManifestTask(task)
		if syncTask != nil {
			plan.AddTask(syncTask)
		}
	}

	return plan, nil
}

// buildDefaultPlanFromStacks creates sync tasks from stacks that need data sync
func buildDefaultPlanFromStacks(manifest *bundledomain.Manifest, filterType string) (*sync.SyncPlan, error) {
	plan := sync.NewSyncPlan(fmt.Sprintf("sync-%d", time.Now().Unix()))

	for _, stack := range manifest.Stacks {
		if !stack.DataSyncRequired {
			continue
		}

		// Infer sync type from stack type
		stackSyncType := inferSyncType(stack.Type)
		if filterType != "" && !strings.EqualFold(string(stackSyncType), filterType) {
			continue
		}

		// Create a placeholder task - actual endpoints need to be provided via --source/--target
		task := sync.NewSyncTask(
			fmt.Sprintf("sync-%s", stack.Name),
			fmt.Sprintf("Sync %s", stack.Name),
			stackSyncType,
			nil, // Source will need to be provided
			nil, // Target will need to be provided
		)
		task.Strategy = inferStrategy(stack.Type)
		plan.AddTask(task)
	}

	if len(plan.Tasks) > 0 && plan.Tasks[0].Source == nil {
		return nil, fmt.Errorf("bundle requires sync but no endpoints defined; use --source and --target flags")
	}

	return plan, nil
}

// convertManifestTask converts a bundle SyncTask to domain SyncTask
func convertManifestTask(bt *bundledomain.SyncTask) *sync.SyncTask {
	syncType := sync.SyncType(bt.Type)
	if !syncType.IsValid() {
		syncType = sync.SyncTypeDatabase // Default
	}

	source := convertEndpoint(bt.SourceEndpoint)
	target := convertEndpoint(bt.TargetEndpoint)

	task := sync.NewSyncTask(bt.ID, bt.ID, syncType, source, target)
	task.Strategy = bt.Strategy
	task.DependsOn = bt.Dependencies

	return task
}

// convertEndpoint converts a bundle Endpoint to domain Endpoint
func convertEndpoint(be *bundledomain.Endpoint) *sync.Endpoint {
	if be == nil {
		return nil
	}

	return &sync.Endpoint{
		Type:     be.Type,
		Host:     be.Host,
		Port:     be.Port,
		Database: be.Database,
		Bucket:   be.Bucket,
		Path:     be.Path,
	}
}

// inferSyncType infers sync type from stack type
func inferSyncType(stackType string) sync.SyncType {
	switch strings.ToLower(stackType) {
	case "postgresql", "postgres", "mysql", "mariadb", "sql":
		return sync.SyncTypeDatabase
	case "redis", "valkey", "memcached", "cache":
		return sync.SyncTypeCache
	case "minio", "storage", "s3", "gcs", "blob":
		return sync.SyncTypeStorage
	default:
		return sync.SyncTypeDatabase
	}
}

// inferStrategy infers sync strategy from stack type
func inferStrategy(stackType string) string {
	switch strings.ToLower(stackType) {
	case "postgresql", "postgres":
		return "postgres"
	case "mysql", "mariadb":
		return "mysql"
	case "redis", "valkey":
		return "redis"
	case "minio", "s3", "gcs", "blob", "storage":
		return "minio"
	default:
		return "postgres"
	}
}

// buildPlanFromEndpoints creates a sync plan from connection strings
func buildPlanFromEndpoints(source, target string) (*sync.SyncPlan, error) {
	sourceEndpoint, err := parseConnectionString(source)
	if err != nil {
		return nil, fmt.Errorf("invalid source: %w", err)
	}

	targetEndpoint, err := parseConnectionString(target)
	if err != nil {
		return nil, fmt.Errorf("invalid target: %w", err)
	}

	plan := sync.NewSyncPlan(fmt.Sprintf("sync-%d", time.Now().Unix()))

	// Infer sync type from endpoint type
	syncType := sync.SyncTypeDatabase
	switch sourceEndpoint.Type {
	case "redis", "valkey":
		syncType = sync.SyncTypeCache
	case "s3", "minio", "gcs":
		syncType = sync.SyncTypeStorage
	}

	task := sync.NewSyncTask(
		"sync-task-1",
		fmt.Sprintf("Sync %s to %s", sourceEndpoint.Type, targetEndpoint.Type),
		syncType,
		sourceEndpoint,
		targetEndpoint,
	)
	task.Strategy = sourceEndpoint.Type

	plan.AddTask(task)

	return plan, nil
}

// parseConnectionString parses a connection string into an Endpoint
func parseConnectionString(connStr string) (*sync.Endpoint, error) {
	u, err := url.Parse(connStr)
	if err != nil {
		return nil, fmt.Errorf("invalid connection string: %w", err)
	}

	endpoint := sync.NewEndpoint(u.Scheme)
	endpoint.Host = u.Hostname()

	if portStr := u.Port(); portStr != "" {
		port, err := strconv.Atoi(portStr)
		if err == nil {
			endpoint.Port = port
		}
	}

	// Set database/bucket from path
	path := strings.TrimPrefix(u.Path, "/")
	switch u.Scheme {
	case "postgres", "postgresql", "mysql", "mariadb", "redis":
		endpoint.Database = path
		endpoint.Type = normalizeScheme(u.Scheme)
	case "s3", "minio", "gcs":
		endpoint.Bucket = path
	}

	// Extract credentials
	if u.User != nil {
		endpoint.Credentials = &sync.Credentials{
			Username: u.User.Username(),
		}
		if pass, ok := u.User.Password(); ok {
			endpoint.Credentials.Password = pass
		}
	}

	// Parse query parameters for SSL options
	query := u.Query()
	if sslMode := query.Get("sslmode"); sslMode != "" {
		endpoint.SSLMode = sslMode
		endpoint.SSL = sslMode != "disable"
	}

	return endpoint, nil
}

// normalizeScheme normalizes database scheme names
func normalizeScheme(scheme string) string {
	switch scheme {
	case "postgresql":
		return "postgres"
	case "mariadb":
		return "mysql"
	default:
		return scheme
	}
}

// displaySyncPlan shows the sync plan to the user
func displaySyncPlan(plan *sync.SyncPlan) {
	ui.Info(fmt.Sprintf("Sync Plan: %d task(s), parallelism: %d", len(plan.Tasks), plan.Parallelism))
	ui.Divider()

	table := ui.NewTable([]string{"#", "Task", "Type", "Strategy", "Source", "Target"})

	for i, task := range plan.Tasks {
		sourceStr := formatEndpoint(task.Source)
		targetStr := formatEndpoint(task.Target)

		table.AddRow([]string{
			fmt.Sprintf("%d", i+1),
			task.Name,
			string(task.Type),
			task.Strategy,
			sourceStr,
			targetStr,
		})
	}

	fmt.Println(table.Render())
	ui.Divider()
}

// formatEndpoint formats an endpoint for display
func formatEndpoint(ep *sync.Endpoint) string {
	if ep == nil {
		return "(not set)"
	}

	switch ep.Type {
	case "postgres", "mysql":
		if ep.Host != "" && ep.Database != "" {
			return fmt.Sprintf("%s://%s/%s", ep.Type, ep.Host, ep.Database)
		}
		return fmt.Sprintf("%s://%s", ep.Type, ep.Host)
	case "redis":
		return fmt.Sprintf("redis://%s:%d", ep.Host, ep.Port)
	case "s3", "minio":
		return fmt.Sprintf("%s://%s", ep.Type, ep.Bucket)
	default:
		return fmt.Sprintf("%s://%s", ep.Type, ep.Host)
	}
}

// runOneTimeSync performs a one-time synchronization
func runOneTimeSync(ctx context.Context, plan *sync.SyncPlan) error {
	if !IsQuiet() {
		ui.Info("Starting one-time sync...")
	}

	registry := syncinfra.NewDefaultRegistry()
	totalTasks := len(plan.Tasks)

	for i, task := range plan.Tasks {
		if !IsQuiet() {
			fmt.Println(ui.SimpleProgress(i+1, totalTasks, fmt.Sprintf("Syncing %s", task.Name)))
		}

		if err := runSyncTask(ctx, registry, task); err != nil {
			ui.Error(fmt.Sprintf("Task %s failed: %v", task.Name, err))
			if !IsQuiet() {
				// Ask if user wants to continue
				if !ui.PromptYesNo("Continue with remaining tasks", true) {
					return fmt.Errorf("sync aborted by user")
				}
			} else {
				return err
			}
		}
	}

	if !IsQuiet() {
		ui.Divider()
		ui.Success("Sync completed successfully")
	}

	return nil
}

// runContinuousSync performs continuous synchronization (CDC mode)
func runContinuousSync(ctx context.Context, plan *sync.SyncPlan) error {
	if !IsQuiet() {
		ui.Info("Starting continuous sync (CDC mode)...")
		ui.Warning("Press Ctrl+C to stop")
	}

	registry := syncinfra.NewDefaultRegistry()

	// For continuous sync, we loop until cancelled
	ticker := time.NewTicker(5 * time.Second) // Check interval
	defer ticker.Stop()

	syncCount := 0
	for {
		select {
		case <-ctx.Done():
			if !IsQuiet() {
				ui.Info(fmt.Sprintf("Continuous sync stopped after %d sync cycles", syncCount))
			}
			return nil
		case <-ticker.C:
			syncCount++
			if IsVerbose() {
				ui.Info(fmt.Sprintf("Sync cycle %d", syncCount))
			}

			for _, task := range plan.Tasks {
				if ctx.Err() != nil {
					return nil
				}

				if err := runSyncTask(ctx, registry, task); err != nil {
					ui.Error(fmt.Sprintf("Sync task %s failed: %v", task.Name, err))
					// Continue with next task in continuous mode
				}
			}
		}
	}
}

// runVerifyOnly verifies data without syncing
func runVerifyOnly(ctx context.Context, plan *sync.SyncPlan) error {
	if !IsQuiet() {
		ui.Info("Running verification only...")
	}

	registry := syncinfra.NewDefaultRegistry()
	allValid := true
	totalTasks := len(plan.Tasks)

	for i, task := range plan.Tasks {
		if !IsQuiet() {
			fmt.Println(ui.SimpleProgress(i+1, totalTasks, fmt.Sprintf("Verifying %s", task.Name)))
		}

		strategy := registry.GetForEndpoint(task.Strategy)
		if strategy == nil {
			strategy = registry.GetForEndpoint(task.Source.Type)
		}

		if strategy == nil {
			ui.Warning(fmt.Sprintf("No strategy available for %s, skipping verification", task.Name))
			continue
		}

		result, err := strategy.Verify(ctx, task.Source, task.Target)
		if err != nil {
			ui.Error(fmt.Sprintf("Verification failed for %s: %v", task.Name, err))
			allValid = false
			continue
		}

		if result.Valid {
			if !IsQuiet() {
				ui.Success(fmt.Sprintf("%s: %s", task.Name, result.String()))
			}
		} else {
			ui.Error(fmt.Sprintf("%s: %s", task.Name, result.String()))
			allValid = false
		}
	}

	ui.Divider()
	if allValid {
		ui.Success("All verifications passed")
		return nil
	}
	return fmt.Errorf("verification failed for one or more tasks")
}

// runSyncTask executes a single sync task
func runSyncTask(ctx context.Context, registry *sync.StrategyRegistry, task *sync.SyncTask) error {
	if task.Source == nil || task.Target == nil {
		return fmt.Errorf("task %s has missing source or target endpoint", task.Name)
	}

	// Get strategy for this task
	strategy := registry.GetForEndpoint(task.Strategy)
	if strategy == nil {
		strategy = registry.GetForEndpoint(task.Source.Type)
	}

	if strategy == nil {
		return fmt.Errorf("no sync strategy available for type: %s", task.Source.Type)
	}

	// Mark task as running
	task.Start()

	// Create progress channel
	progressChan := make(chan sync.Progress, 100)
	defer close(progressChan)

	// Display progress in a goroutine
	go func() {
		for progress := range progressChan {
			if IsVerbose() && !IsQuiet() {
				fmt.Printf("\r  %s", progress.String())
			}
		}
	}()

	// Run sync
	if err := strategy.Sync(ctx, task.Source, task.Target, progressChan); err != nil {
		task.Fail(err)
		return err
	}

	task.Complete()

	// Verify after sync if verbose
	if IsVerbose() {
		result, err := strategy.Verify(ctx, task.Source, task.Target)
		if err != nil {
			ui.Warning(fmt.Sprintf("Post-sync verification failed: %v", err))
		} else if !result.Valid {
			ui.Warning(fmt.Sprintf("Post-sync verification: %s", result.String()))
		} else {
			ui.Success(fmt.Sprintf("Verified: %s", result.String()))
		}
	}

	return nil
}
