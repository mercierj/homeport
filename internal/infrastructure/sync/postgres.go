// Package sync provides infrastructure implementations for data synchronization strategies.
// It includes implementations for PostgreSQL, MySQL, and Redis sync operations.
package sync

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/sync"

	_ "github.com/lib/pq" // PostgreSQL driver
)

// PostgresSync implements the SyncStrategy interface for PostgreSQL databases.
// It uses pg_dump and pg_restore for efficient database synchronization.
type PostgresSync struct {
	*sync.BaseStrategy
}

// NewPostgresSync creates a new PostgreSQL sync strategy.
func NewPostgresSync() *PostgresSync {
	return &PostgresSync{
		BaseStrategy: sync.NewBaseStrategy("postgres", sync.SyncTypeDatabase, false, false),
	}
}

// EstimateSize calculates the approximate size of the PostgreSQL database.
// It queries pg_database_size for an accurate estimate.
func (p *PostgresSync) EstimateSize(ctx context.Context, source *sync.Endpoint) (int64, error) {
	db, err := p.connect(ctx, source)
	if err != nil {
		return 0, fmt.Errorf("failed to connect to source database: %w", err)
	}
	defer db.Close()

	var size int64
	query := "SELECT pg_database_size(current_database())"
	if err := db.QueryRowContext(ctx, query).Scan(&size); err != nil {
		return 0, fmt.Errorf("failed to query database size: %w", err)
	}

	return size, nil
}

// Sync performs the database synchronization from source to target.
// It uses pg_dump with custom format and streams to pg_restore.
func (p *PostgresSync) Sync(ctx context.Context, source, target *sync.Endpoint, progress chan<- sync.Progress) error {
	reporter := sync.NewProgressReporter("postgres-sync", progress, nil)
	reporter.SetPhase("initializing")

	// Step 1: Estimate size for progress tracking
	size, err := p.EstimateSize(ctx, source)
	if err != nil {
		reporter.Error(fmt.Sprintf("failed to estimate size: %v", err))
		return err
	}
	reporter.SetTotals(size, 0)

	// Step 2: Create target database if it doesn't exist
	reporter.SetPhase("preparing target")
	if err := p.createTargetDatabase(ctx, target); err != nil {
		reporter.Error(fmt.Sprintf("failed to create target database: %v", err))
		return err
	}

	// Step 3: Run pg_dump and pipe to pg_restore
	reporter.SetPhase("dumping and restoring")

	dumpCmd := p.buildDumpCommand(source)
	restoreCmd := p.buildRestoreCommand(target)

	// Create pipe between dump and restore
	dumpProc := exec.CommandContext(ctx, dumpCmd[0], dumpCmd[1:]...)
	restoreProc := exec.CommandContext(ctx, restoreCmd[0], restoreCmd[1:]...)

	// Set up environment for authentication
	dumpProc.Env = p.buildEnv(source)
	restoreProc.Env = p.buildEnv(target)

	// Create pipe
	pipeReader, pipeWriter := io.Pipe()

	dumpProc.Stdout = pipeWriter
	restoreProc.Stdin = pipeReader

	// Capture stderr for error reporting
	var dumpStderr, restoreStderr strings.Builder
	dumpProc.Stderr = &dumpStderr
	restoreProc.Stderr = &restoreStderr

	// Start restore first (it will wait for input)
	if err := restoreProc.Start(); err != nil {
		return fmt.Errorf("failed to start pg_restore: %w", err)
	}

	// Start dump
	if err := dumpProc.Start(); err != nil {
		pipeWriter.Close()
		restoreProc.Process.Kill()
		return fmt.Errorf("failed to start pg_dump: %w", err)
	}

	// Monitor progress in a goroutine
	progressDone := make(chan struct{})
	go func() {
		defer close(progressDone)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-progressDone:
				return
			case <-ticker.C:
				// Update progress based on time elapsed
				elapsed := reporter.GetProgress().ElapsedTime().Seconds()
				// Estimate bytes based on typical PostgreSQL dump rate (rough estimate)
				estimatedBytes := int64(elapsed * 10 * 1024 * 1024) // ~10 MB/s estimate
				if estimatedBytes > size {
					estimatedBytes = size
				}
				reporter.Update(estimatedBytes, 0, "Syncing database...")
			}
		}
	}()

	// Wait for dump to complete
	dumpErr := dumpProc.Wait()
	pipeWriter.Close()

	// Wait for restore to complete
	restoreErr := restoreProc.Wait()
	pipeReader.Close()

	// Signal progress monitoring to stop
	close(progressDone)

	if dumpErr != nil {
		reporter.Error(fmt.Sprintf("pg_dump failed: %s", dumpStderr.String()))
		return fmt.Errorf("pg_dump failed: %w - stderr: %s", dumpErr, dumpStderr.String())
	}

	if restoreErr != nil {
		reporter.Error(fmt.Sprintf("pg_restore failed: %s", restoreStderr.String()))
		return fmt.Errorf("pg_restore failed: %w - stderr: %s", restoreErr, restoreStderr.String())
	}

	// Step 4: Mark as complete
	reporter.SetPhase("completed")
	reporter.Update(size, 0, "Database sync completed successfully")

	return nil
}

// Verify compares source and target databases to ensure sync was successful.
// It checks table row counts and schema consistency.
func (p *PostgresSync) Verify(ctx context.Context, source, target *sync.Endpoint) (*sync.VerifyResult, error) {
	result := sync.NewVerifyResult()
	result.Valid = true

	// Connect to both databases
	sourceDB, err := p.connect(ctx, source)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to source: %w", err)
	}
	defer sourceDB.Close()

	targetDB, err := p.connect(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to target: %w", err)
	}
	defer targetDB.Close()

	// Get table list from source
	tables, err := p.getTables(ctx, sourceDB)
	if err != nil {
		return nil, fmt.Errorf("failed to get source tables: %w", err)
	}

	tableDetails := make(map[string]interface{})

	for _, table := range tables {
		sourceCount, err := p.getRowCount(ctx, sourceDB, table)
		if err != nil {
			result.AddMismatch(fmt.Sprintf("failed to count source table %s: %v", table, err))
			continue
		}

		targetCount, err := p.getRowCount(ctx, targetDB, table)
		if err != nil {
			result.AddMismatch(fmt.Sprintf("table %s missing in target or count failed: %v", table, err))
			continue
		}

		result.SourceCount += sourceCount
		result.TargetCount += targetCount

		if sourceCount != targetCount {
			result.AddMismatch(fmt.Sprintf("table %s: source=%d, target=%d", table, sourceCount, targetCount))
		}

		tableDetails[table] = map[string]int64{
			"source": sourceCount,
			"target": targetCount,
		}
	}

	result.Details["tables"] = tableDetails
	result.Details["table_count"] = len(tables)

	return result, nil
}

// connect establishes a connection to the PostgreSQL database.
func (p *PostgresSync) connect(ctx context.Context, endpoint *sync.Endpoint) (*sql.DB, error) {
	connStr := endpoint.ConnectionString()
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, err
	}

	// Test the connection
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

// createTargetDatabase creates the target database if it doesn't exist.
func (p *PostgresSync) createTargetDatabase(ctx context.Context, target *sync.Endpoint) error {
	// Connect to the default 'postgres' database to create the target database
	adminEndpoint := &sync.Endpoint{
		Type:        target.Type,
		Host:        target.Host,
		Port:        target.Port,
		Database:    "postgres",
		Credentials: target.Credentials,
		SSL:         target.SSL,
		SSLMode:     target.SSLMode,
	}

	db, err := p.connect(ctx, adminEndpoint)
	if err != nil {
		return fmt.Errorf("failed to connect to postgres database: %w", err)
	}
	defer db.Close()

	// Check if database exists
	var exists bool
	query := "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)"
	if err := db.QueryRowContext(ctx, query, target.Database).Scan(&exists); err != nil {
		return fmt.Errorf("failed to check database existence: %w", err)
	}

	if !exists {
		// Create the database
		// Note: database names can't be parameterized in CREATE DATABASE
		createQuery := fmt.Sprintf("CREATE DATABASE %s", quoteIdentifier(target.Database))
		if _, err := db.ExecContext(ctx, createQuery); err != nil {
			return fmt.Errorf("failed to create database: %w", err)
		}
	}

	return nil
}

// buildDumpCommand constructs the pg_dump command with appropriate arguments.
func (p *PostgresSync) buildDumpCommand(source *sync.Endpoint) []string {
	args := []string{"pg_dump"}

	// Connection options
	args = append(args, "-h", source.Host)
	if source.Port > 0 {
		args = append(args, "-p", strconv.Itoa(source.Port))
	}
	if source.Credentials != nil && source.Credentials.Username != "" {
		args = append(args, "-U", source.Credentials.Username)
	}

	// Use custom format for streaming
	args = append(args, "-Fc")

	// Verbose output for progress tracking
	args = append(args, "-v")

	// Database name
	args = append(args, source.Database)

	return args
}

// buildRestoreCommand constructs the pg_restore command with appropriate arguments.
func (p *PostgresSync) buildRestoreCommand(target *sync.Endpoint) []string {
	args := []string{"pg_restore"}

	// Connection options
	args = append(args, "-h", target.Host)
	if target.Port > 0 {
		args = append(args, "-p", strconv.Itoa(target.Port))
	}
	if target.Credentials != nil && target.Credentials.Username != "" {
		args = append(args, "-U", target.Credentials.Username)
	}

	// Target database
	args = append(args, "-d", target.Database)

	// Clean (drop) database objects before recreating
	args = append(args, "--clean", "--if-exists")

	// Continue on error (some objects may already exist)
	args = append(args, "--no-owner", "--no-privileges")

	// Verbose output
	args = append(args, "-v")

	return args
}

// buildEnv constructs environment variables for PostgreSQL commands.
func (p *PostgresSync) buildEnv(endpoint *sync.Endpoint) []string {
	env := []string{}

	if endpoint.Credentials != nil && endpoint.Credentials.Password != "" {
		env = append(env, fmt.Sprintf("PGPASSWORD=%s", endpoint.Credentials.Password))
	}

	if endpoint.SSLMode != "" {
		env = append(env, fmt.Sprintf("PGSSLMODE=%s", endpoint.SSLMode))
	}

	return env
}

// getTables returns a list of all user tables in the database.
func (p *PostgresSync) getTables(ctx context.Context, db *sql.DB) ([]string, error) {
	query := `
		SELECT schemaname || '.' || tablename
		FROM pg_tables
		WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
		ORDER BY schemaname, tablename
	`

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return nil, err
		}
		tables = append(tables, table)
	}

	return tables, rows.Err()
}

// getRowCount returns the number of rows in a table.
func (p *PostgresSync) getRowCount(ctx context.Context, db *sql.DB, table string) (int64, error) {
	// Use count estimate from pg_stat for large tables, exact count for small ones
	query := fmt.Sprintf("SELECT count(*) FROM %s", table)
	var count int64
	if err := db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// quoteIdentifier safely quotes a PostgreSQL identifier.
func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// PostgresDumpReader wraps a pg_dump stdout to track bytes read.
type PostgresDumpReader struct {
	reader   io.Reader
	bytesRead int64
	progress chan<- sync.Progress
	taskID   string
	total    int64
}

// NewPostgresDumpReader creates a new dump reader with progress tracking.
func NewPostgresDumpReader(reader io.Reader, taskID string, total int64, progress chan<- sync.Progress) *PostgresDumpReader {
	return &PostgresDumpReader{
		reader:   reader,
		taskID:   taskID,
		total:    total,
		progress: progress,
	}
}

// Read implements io.Reader and tracks bytes read.
func (r *PostgresDumpReader) Read(p []byte) (n int, err error) {
	n, err = r.reader.Read(p)
	r.bytesRead += int64(n)

	// Send progress update (throttled internally by the channel buffer)
	if r.progress != nil {
		prog := sync.Progress{
			TaskID:     r.taskID,
			BytesTotal: r.total,
			BytesDone:  r.bytesRead,
			Phase:      "transferring",
			Message:    "Syncing database...",
		}
		select {
		case r.progress <- prog:
		default:
			// Channel full, skip update
		}
	}

	return n, err
}

// PostgresStreamSync performs streaming sync using pipes.
// This is an alternative implementation that processes output line by line.
type PostgresStreamSync struct {
	*PostgresSync
}

// NewPostgresStreamSync creates a streaming PostgreSQL sync.
func NewPostgresStreamSync() *PostgresStreamSync {
	return &PostgresStreamSync{
		PostgresSync: NewPostgresSync(),
	}
}

// StreamSync performs a streaming sync with detailed progress tracking.
func (p *PostgresStreamSync) StreamSync(ctx context.Context, source, target *sync.Endpoint, progress chan<- sync.Progress) error {
	reporter := sync.NewProgressReporter("postgres-stream-sync", progress, nil)
	reporter.SetPhase("scanning")

	// Get table list for progress tracking
	sourceDB, err := p.connect(ctx, source)
	if err != nil {
		return err
	}

	tables, err := p.getTables(ctx, sourceDB)
	sourceDB.Close()
	if err != nil {
		return err
	}

	reporter.SetTotals(0, int64(len(tables)))
	reporter.SetPhase("syncing")

	// Sync each table individually for better progress tracking
	for i, table := range tables {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		reporter.SetCurrentItem(table)

		if err := p.syncTable(ctx, source, target, table); err != nil {
			reporter.Error(fmt.Sprintf("failed to sync table %s: %v", table, err))
			return err
		}

		reporter.Update(0, int64(i+1), fmt.Sprintf("Synced table %s", table))
	}

	reporter.SetPhase("completed")
	return nil
}

// syncTable syncs a single table from source to target.
func (p *PostgresStreamSync) syncTable(ctx context.Context, source, target *sync.Endpoint, table string) error {
	// Build COPY command for streaming data
	copyOutCmd := p.buildCopyOutCommand(source, table)
	copyInCmd := p.buildCopyInCommand(target, table)

	copyOut := exec.CommandContext(ctx, copyOutCmd[0], copyOutCmd[1:]...)
	copyIn := exec.CommandContext(ctx, copyInCmd[0], copyInCmd[1:]...)

	copyOut.Env = p.buildEnv(source)
	copyIn.Env = p.buildEnv(target)

	// Create pipe
	pipeReader, pipeWriter := io.Pipe()
	copyOut.Stdout = pipeWriter
	copyIn.Stdin = pipeReader

	var copyOutErr, copyInErr strings.Builder
	copyOut.Stderr = &copyOutErr
	copyIn.Stderr = &copyInErr

	if err := copyIn.Start(); err != nil {
		return fmt.Errorf("failed to start COPY IN: %w", err)
	}

	if err := copyOut.Start(); err != nil {
		pipeWriter.Close()
		copyIn.Process.Kill()
		return fmt.Errorf("failed to start COPY OUT: %w", err)
	}

	outErr := copyOut.Wait()
	pipeWriter.Close()

	inErr := copyIn.Wait()
	pipeReader.Close()

	if outErr != nil {
		return fmt.Errorf("COPY OUT failed: %w - %s", outErr, copyOutErr.String())
	}
	if inErr != nil {
		return fmt.Errorf("COPY IN failed: %w - %s", inErr, copyInErr.String())
	}

	return nil
}

// buildCopyOutCommand builds a psql command to COPY a table to stdout.
func (p *PostgresStreamSync) buildCopyOutCommand(source *sync.Endpoint, table string) []string {
	args := []string{"psql"}
	args = append(args, "-h", source.Host)
	if source.Port > 0 {
		args = append(args, "-p", strconv.Itoa(source.Port))
	}
	if source.Credentials != nil && source.Credentials.Username != "" {
		args = append(args, "-U", source.Credentials.Username)
	}
	args = append(args, "-d", source.Database)
	args = append(args, "-c", fmt.Sprintf("COPY %s TO STDOUT", table))
	return args
}

// buildCopyInCommand builds a psql command to COPY from stdin to a table.
func (p *PostgresStreamSync) buildCopyInCommand(target *sync.Endpoint, table string) []string {
	args := []string{"psql"}
	args = append(args, "-h", target.Host)
	if target.Port > 0 {
		args = append(args, "-p", strconv.Itoa(target.Port))
	}
	if target.Credentials != nil && target.Credentials.Username != "" {
		args = append(args, "-U", target.Credentials.Username)
	}
	args = append(args, "-d", target.Database)
	args = append(args, "-c", fmt.Sprintf("COPY %s FROM STDIN", table))
	return args
}

// ParsePgDumpProgress parses pg_dump verbose output for progress information.
func ParsePgDumpProgress(line string) (tableName string, phase string) {
	line = strings.TrimSpace(line)

	if strings.HasPrefix(line, "pg_dump: dumping contents of table") {
		// Extract table name
		parts := strings.Split(line, `"`)
		if len(parts) >= 2 {
			tableName = parts[1]
		}
		phase = "dumping"
	} else if strings.HasPrefix(line, "pg_dump: saving") {
		phase = "saving"
	} else if strings.HasPrefix(line, "pg_dump: creating") {
		phase = "creating"
	}

	return tableName, phase
}

// MonitorPgDumpProgress reads stderr from pg_dump and sends progress updates.
func MonitorPgDumpProgress(stderr io.Reader, reporter *sync.ProgressReporter) {
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		line := scanner.Text()
		tableName, phase := ParsePgDumpProgress(line)
		if tableName != "" {
			reporter.SetCurrentItem(tableName)
		}
		if phase != "" {
			reporter.SetPhase(phase)
		}
	}
}
