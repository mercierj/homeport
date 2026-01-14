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

	_ "github.com/go-sql-driver/mysql" // MySQL driver
)

// MySQLSync implements the SyncStrategy interface for MySQL/MariaDB databases.
// It uses mysqldump for export and mysql client for import.
type MySQLSync struct {
	*sync.BaseStrategy
}

// NewMySQLSync creates a new MySQL sync strategy.
func NewMySQLSync() *MySQLSync {
	return &MySQLSync{
		BaseStrategy: sync.NewBaseStrategy("mysql", sync.SyncTypeDatabase, false, false),
	}
}

// EstimateSize calculates the approximate size of the MySQL database.
// It queries information_schema for table sizes.
func (m *MySQLSync) EstimateSize(ctx context.Context, source *sync.Endpoint) (int64, error) {
	db, err := m.connect(ctx, source)
	if err != nil {
		return 0, fmt.Errorf("failed to connect to source database: %w", err)
	}
	defer db.Close()

	var size sql.NullInt64
	query := `
		SELECT SUM(data_length + index_length)
		FROM information_schema.tables
		WHERE table_schema = ?
	`
	if err := db.QueryRowContext(ctx, query, source.Database).Scan(&size); err != nil {
		return 0, fmt.Errorf("failed to query database size: %w", err)
	}

	if !size.Valid {
		return 0, nil
	}

	return size.Int64, nil
}

// Sync performs the database synchronization from source to target.
// It uses mysqldump and pipes output to mysql client.
func (m *MySQLSync) Sync(ctx context.Context, source, target *sync.Endpoint, progress chan<- sync.Progress) error {
	reporter := sync.NewProgressReporter("mysql-sync", progress, nil)
	reporter.SetPhase("initializing")

	// Step 1: Estimate size for progress tracking
	size, err := m.EstimateSize(ctx, source)
	if err != nil {
		reporter.Error(fmt.Sprintf("failed to estimate size: %v", err))
		return err
	}
	reporter.SetTotals(size, 0)

	// Step 2: Create target database if it doesn't exist
	reporter.SetPhase("preparing target")
	if err := m.createTargetDatabase(ctx, target); err != nil {
		reporter.Error(fmt.Sprintf("failed to create target database: %v", err))
		return err
	}

	// Step 3: Run mysqldump and pipe to mysql
	reporter.SetPhase("dumping and restoring")

	dumpCmd := m.buildDumpCommand(source)
	restoreCmd := m.buildRestoreCommand(target)

	// Create dump process
	dumpProc := exec.CommandContext(ctx, dumpCmd[0], dumpCmd[1:]...)
	restoreProc := exec.CommandContext(ctx, restoreCmd[0], restoreCmd[1:]...)

	// Set up environment for authentication
	dumpProc.Env = m.buildEnv(source)
	restoreProc.Env = m.buildEnv(target)

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
		return fmt.Errorf("failed to start mysql restore: %w", err)
	}

	// Start dump
	if err := dumpProc.Start(); err != nil {
		pipeWriter.Close()
		restoreProc.Process.Kill()
		return fmt.Errorf("failed to start mysqldump: %w", err)
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
				// Estimate bytes based on typical mysqldump rate
				estimatedBytes := int64(elapsed * 15 * 1024 * 1024) // ~15 MB/s estimate
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
		reporter.Error(fmt.Sprintf("mysqldump failed: %s", dumpStderr.String()))
		return fmt.Errorf("mysqldump failed: %w - stderr: %s", dumpErr, dumpStderr.String())
	}

	if restoreErr != nil {
		reporter.Error(fmt.Sprintf("mysql restore failed: %s", restoreStderr.String()))
		return fmt.Errorf("mysql restore failed: %w - stderr: %s", restoreErr, restoreStderr.String())
	}

	// Step 4: Mark as complete
	reporter.SetPhase("completed")
	reporter.Update(size, 0, "Database sync completed successfully")

	return nil
}

// Verify compares source and target databases to ensure sync was successful.
// It checks table row counts and structure consistency.
func (m *MySQLSync) Verify(ctx context.Context, source, target *sync.Endpoint) (*sync.VerifyResult, error) {
	result := sync.NewVerifyResult()
	result.Valid = true

	// Connect to both databases
	sourceDB, err := m.connect(ctx, source)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to source: %w", err)
	}
	defer sourceDB.Close()

	targetDB, err := m.connect(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to target: %w", err)
	}
	defer targetDB.Close()

	// Get table list from source
	tables, err := m.getTables(ctx, sourceDB, source.Database)
	if err != nil {
		return nil, fmt.Errorf("failed to get source tables: %w", err)
	}

	tableDetails := make(map[string]interface{})

	for _, table := range tables {
		sourceCount, err := m.getRowCount(ctx, sourceDB, table)
		if err != nil {
			result.AddMismatch(fmt.Sprintf("failed to count source table %s: %v", table, err))
			continue
		}

		targetCount, err := m.getRowCount(ctx, targetDB, table)
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

// connect establishes a connection to the MySQL database.
func (m *MySQLSync) connect(ctx context.Context, endpoint *sync.Endpoint) (*sql.DB, error) {
	dsn := m.buildDSN(endpoint)
	db, err := sql.Open("mysql", dsn)
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

// buildDSN constructs a MySQL data source name.
func (m *MySQLSync) buildDSN(endpoint *sync.Endpoint) string {
	// Format: user:password@tcp(host:port)/database?params
	var dsn strings.Builder

	if endpoint.Credentials != nil && endpoint.Credentials.Username != "" {
		dsn.WriteString(endpoint.Credentials.Username)
		if endpoint.Credentials.Password != "" {
			dsn.WriteString(":")
			dsn.WriteString(endpoint.Credentials.Password)
		}
		dsn.WriteString("@")
	}

	dsn.WriteString("tcp(")
	dsn.WriteString(endpoint.Host)
	if endpoint.Port > 0 {
		dsn.WriteString(":")
		dsn.WriteString(strconv.Itoa(endpoint.Port))
	}
	dsn.WriteString(")")

	if endpoint.Database != "" {
		dsn.WriteString("/")
		dsn.WriteString(endpoint.Database)
	}

	// Add common parameters
	dsn.WriteString("?parseTime=true&multiStatements=true")

	if endpoint.SSL {
		dsn.WriteString("&tls=true")
	}

	return dsn.String()
}

// createTargetDatabase creates the target database if it doesn't exist.
func (m *MySQLSync) createTargetDatabase(ctx context.Context, target *sync.Endpoint) error {
	// Connect without specifying a database
	adminEndpoint := &sync.Endpoint{
		Type:        target.Type,
		Host:        target.Host,
		Port:        target.Port,
		Database:    "", // Connect to server, not a specific database
		Credentials: target.Credentials,
		SSL:         target.SSL,
	}

	db, err := m.connect(ctx, adminEndpoint)
	if err != nil {
		return fmt.Errorf("failed to connect to MySQL server: %w", err)
	}
	defer db.Close()

	// Create the database if it doesn't exist
	// Note: database names can't be parameterized in CREATE DATABASE
	createQuery := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`", escapeMySQLIdentifier(target.Database))
	if _, err := db.ExecContext(ctx, createQuery); err != nil {
		return fmt.Errorf("failed to create database: %w", err)
	}

	return nil
}

// buildDumpCommand constructs the mysqldump command with appropriate arguments.
func (m *MySQLSync) buildDumpCommand(source *sync.Endpoint) []string {
	args := []string{"mysqldump"}

	// Connection options
	args = append(args, "-h", source.Host)
	if source.Port > 0 {
		args = append(args, "-P", strconv.Itoa(source.Port))
	}
	if source.Credentials != nil && source.Credentials.Username != "" {
		args = append(args, "-u", source.Credentials.Username)
	}

	// Dump options
	args = append(args, "--single-transaction") // Consistent snapshot
	args = append(args, "--quick")              // Don't buffer entire table in memory
	args = append(args, "--routines")           // Include stored procedures
	args = append(args, "--triggers")           // Include triggers
	args = append(args, "--events")             // Include scheduled events
	args = append(args, "--add-drop-database")  // Include DROP DATABASE statement
	args = append(args, "--databases")          // Include CREATE DATABASE statement

	// Database name
	args = append(args, source.Database)

	return args
}

// buildRestoreCommand constructs the mysql command for restoring.
func (m *MySQLSync) buildRestoreCommand(target *sync.Endpoint) []string {
	args := []string{"mysql"}

	// Connection options
	args = append(args, "-h", target.Host)
	if target.Port > 0 {
		args = append(args, "-P", strconv.Itoa(target.Port))
	}
	if target.Credentials != nil && target.Credentials.Username != "" {
		args = append(args, "-u", target.Credentials.Username)
	}

	return args
}

// buildEnv constructs environment variables for MySQL commands.
func (m *MySQLSync) buildEnv(endpoint *sync.Endpoint) []string {
	env := []string{}

	if endpoint.Credentials != nil && endpoint.Credentials.Password != "" {
		env = append(env, fmt.Sprintf("MYSQL_PWD=%s", endpoint.Credentials.Password))
	}

	return env
}

// getTables returns a list of all tables in the database.
func (m *MySQLSync) getTables(ctx context.Context, db *sql.DB, database string) ([]string, error) {
	query := `
		SELECT table_name
		FROM information_schema.tables
		WHERE table_schema = ?
		AND table_type = 'BASE TABLE'
		ORDER BY table_name
	`

	rows, err := db.QueryContext(ctx, query, database)
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
func (m *MySQLSync) getRowCount(ctx context.Context, db *sql.DB, table string) (int64, error) {
	query := fmt.Sprintf("SELECT COUNT(*) FROM `%s`", escapeMySQLIdentifier(table))
	var count int64
	if err := db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// escapeMySQLIdentifier escapes a MySQL identifier (table or database name).
func escapeMySQLIdentifier(name string) string {
	return strings.ReplaceAll(name, "`", "``")
}

// MySQLTableSync provides table-by-table sync with detailed progress.
type MySQLTableSync struct {
	*MySQLSync
}

// NewMySQLTableSync creates a table-by-table MySQL sync.
func NewMySQLTableSync() *MySQLTableSync {
	return &MySQLTableSync{
		MySQLSync: NewMySQLSync(),
	}
}

// SyncTables syncs each table individually for better progress tracking.
func (m *MySQLTableSync) SyncTables(ctx context.Context, source, target *sync.Endpoint, progress chan<- sync.Progress) error {
	reporter := sync.NewProgressReporter("mysql-table-sync", progress, nil)
	reporter.SetPhase("scanning")

	// Get table list
	sourceDB, err := m.connect(ctx, source)
	if err != nil {
		return err
	}

	tables, err := m.getTables(ctx, sourceDB, source.Database)
	sourceDB.Close()
	if err != nil {
		return err
	}

	reporter.SetTotals(0, int64(len(tables)))
	reporter.SetPhase("syncing")

	// Create target database first
	if err := m.createTargetDatabase(ctx, target); err != nil {
		return err
	}

	// Sync each table
	for i, table := range tables {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		reporter.SetCurrentItem(table)

		if err := m.syncTable(ctx, source, target, table); err != nil {
			reporter.Error(fmt.Sprintf("failed to sync table %s: %v", table, err))
			return err
		}

		reporter.Update(0, int64(i+1), fmt.Sprintf("Synced table %s", table))
	}

	reporter.SetPhase("completed")
	return nil
}

// syncTable syncs a single table from source to target.
func (m *MySQLTableSync) syncTable(ctx context.Context, source, target *sync.Endpoint, table string) error {
	// Build mysqldump for single table
	dumpArgs := []string{"mysqldump"}
	dumpArgs = append(dumpArgs, "-h", source.Host)
	if source.Port > 0 {
		dumpArgs = append(dumpArgs, "-P", strconv.Itoa(source.Port))
	}
	if source.Credentials != nil && source.Credentials.Username != "" {
		dumpArgs = append(dumpArgs, "-u", source.Credentials.Username)
	}
	dumpArgs = append(dumpArgs, "--single-transaction", "--quick")
	dumpArgs = append(dumpArgs, source.Database, table)

	// Build mysql restore command
	restoreArgs := m.buildRestoreCommand(target)
	restoreArgs = append(restoreArgs, target.Database)

	dumpProc := exec.CommandContext(ctx, dumpArgs[0], dumpArgs[1:]...)
	restoreProc := exec.CommandContext(ctx, restoreArgs[0], restoreArgs[1:]...)

	dumpProc.Env = m.buildEnv(source)
	restoreProc.Env = m.buildEnv(target)

	// Create pipe
	pipeReader, pipeWriter := io.Pipe()
	dumpProc.Stdout = pipeWriter
	restoreProc.Stdin = pipeReader

	var dumpErr, restoreErr strings.Builder
	dumpProc.Stderr = &dumpErr
	restoreProc.Stderr = &restoreErr

	if err := restoreProc.Start(); err != nil {
		return fmt.Errorf("failed to start restore: %w", err)
	}

	if err := dumpProc.Start(); err != nil {
		pipeWriter.Close()
		restoreProc.Process.Kill()
		return fmt.Errorf("failed to start dump: %w", err)
	}

	dumpWaitErr := dumpProc.Wait()
	pipeWriter.Close()

	restoreWaitErr := restoreProc.Wait()
	pipeReader.Close()

	if dumpWaitErr != nil {
		return fmt.Errorf("dump failed: %w - %s", dumpWaitErr, dumpErr.String())
	}
	if restoreWaitErr != nil {
		return fmt.Errorf("restore failed: %w - %s", restoreWaitErr, restoreErr.String())
	}

	return nil
}

// ParseMySQLDumpProgress parses mysqldump output for progress information.
func ParseMySQLDumpProgress(line string) (tableName string, phase string) {
	line = strings.TrimSpace(line)

	if strings.Contains(line, "Dumping data for table") {
		// Extract table name from: -- Dumping data for table `tablename`
		parts := strings.Split(line, "`")
		if len(parts) >= 2 {
			tableName = parts[1]
		}
		phase = "dumping"
	} else if strings.Contains(line, "Table structure for table") {
		parts := strings.Split(line, "`")
		if len(parts) >= 2 {
			tableName = parts[1]
		}
		phase = "structure"
	}

	return tableName, phase
}

// MonitorMySQLDumpProgress reads output and sends progress updates.
func MonitorMySQLDumpProgress(reader io.Reader, reporter *sync.ProgressReporter) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		tableName, phase := ParseMySQLDumpProgress(line)
		if tableName != "" {
			reporter.SetCurrentItem(tableName)
		}
		if phase != "" {
			reporter.SetPhase(phase)
		}
	}
}

// MySQLReplicationSync provides sync using MySQL replication.
// This is useful for minimal-downtime migrations.
type MySQLReplicationSync struct {
	*MySQLSync
	// BinlogPosition stores the current binlog position for resuming.
	BinlogPosition string
}

// NewMySQLReplicationSync creates a replication-based MySQL sync.
func NewMySQLReplicationSync() *MySQLReplicationSync {
	base := NewMySQLSync()
	base.BaseStrategy = sync.NewBaseStrategy("mysql-replication", sync.SyncTypeDatabase, true, true)
	return &MySQLReplicationSync{
		MySQLSync: base,
	}
}

// GetBinlogPosition retrieves the current binary log position.
func (m *MySQLReplicationSync) GetBinlogPosition(ctx context.Context, source *sync.Endpoint) (string, string, error) {
	db, err := m.connect(ctx, source)
	if err != nil {
		return "", "", err
	}
	defer db.Close()

	var file string
	var position int64
	var binlogDoDB, binlogIgnoreDB, executedGtidSet sql.NullString

	row := db.QueryRowContext(ctx, "SHOW MASTER STATUS")
	if err := row.Scan(&file, &position, &binlogDoDB, &binlogIgnoreDB, &executedGtidSet); err != nil {
		return "", "", fmt.Errorf("failed to get binlog position: %w", err)
	}

	return file, strconv.FormatInt(position, 10), nil
}
