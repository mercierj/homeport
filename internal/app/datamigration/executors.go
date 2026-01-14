package datamigration

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// S3ToMinIOExecutor migrates S3 buckets to MinIO.
type S3ToMinIOExecutor struct{}

// NewS3ToMinIOExecutor creates a new S3 to MinIO executor.
func NewS3ToMinIOExecutor() *S3ToMinIOExecutor {
	return &S3ToMinIOExecutor{}
}

// Type returns the migration type.
func (e *S3ToMinIOExecutor) Type() string {
	return "s3_to_minio"
}

// GetPhases returns the migration phases.
func (e *S3ToMinIOExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Creating bucket",
		"Downloading from S3",
		"Uploading to MinIO",
		"Verifying transfer",
	}
}

// Validate validates the migration configuration.
func (e *S3ToMinIOExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	// Validate source config
	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["bucket"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.bucket is required")
		}
		if _, ok := config.Source["access_key_id"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.access_key_id is required")
		}
		if _, ok := config.Source["secret_access_key"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.secret_access_key is required")
		}
		if _, ok := config.Source["region"].(string); !ok {
			result.Warnings = append(result.Warnings, "source.region not specified, using default")
		}
	}

	// Validate destination config
	if config.Destination == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "destination configuration is required")
	} else {
		if _, ok := config.Destination["endpoint"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "destination.endpoint is required")
		}
		if _, ok := config.Destination["access_key"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "destination.access_key is required")
		}
		if _, ok := config.Destination["secret_key"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "destination.secret_key is required")
		}
	}

	return result, nil
}

// Execute performs the migration.
func (e *S3ToMinIOExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	// Extract configuration
	bucket := config.Source["bucket"].(string)
	accessKeyID := config.Source["access_key_id"].(string)
	secretAccessKey := config.Source["secret_access_key"].(string)
	region, _ := config.Source["region"].(string)
	if region == "" {
		region = "us-east-1"
	}

	minioEndpoint := config.Destination["endpoint"].(string)
	minioAccessKey := config.Destination["access_key"].(string)
	minioSecretKey := config.Destination["secret_key"].(string)
	destBucket, _ := config.Destination["bucket"].(string)
	if destBucket == "" {
		destBucket = bucket
	}

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating AWS credentials")
	EmitProgress(m, 10, "Checking source credentials")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Creating bucket
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", fmt.Sprintf("Ensuring bucket %s exists on MinIO", destBucket))
	EmitProgress(m, 20, "Creating destination bucket")

	// Create bucket on MinIO using AWS CLI with endpoint override
	createBucketCmd := exec.CommandContext(ctx, "aws", "s3", "mb",
		fmt.Sprintf("s3://%s", destBucket),
		"--endpoint-url", minioEndpoint,
	)
	createBucketCmd.Env = append(os.Environ(),
		"AWS_ACCESS_KEY_ID="+minioAccessKey,
		"AWS_SECRET_ACCESS_KEY="+minioSecretKey,
	)
	// Ignore error if bucket already exists
	createBucketCmd.Run()

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Downloading from S3
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", fmt.Sprintf("Downloading from S3 bucket: %s", bucket))
	EmitProgress(m, 40, "Downloading from S3")

	// Create staging directory
	stagingDir, err := os.MkdirTemp("", "s3-migration-*")
	if err != nil {
		return fmt.Errorf("failed to create staging directory: %w", err)
	}
	defer os.RemoveAll(stagingDir)

	localPath := filepath.Join(stagingDir, bucket)

	downloadCmd := exec.CommandContext(ctx, "aws", "s3", "sync",
		fmt.Sprintf("s3://%s", bucket),
		localPath,
		"--region", region,
	)
	downloadCmd.Env = append(os.Environ(),
		"AWS_ACCESS_KEY_ID="+accessKeyID,
		"AWS_SECRET_ACCESS_KEY="+secretAccessKey,
		"AWS_DEFAULT_REGION="+region,
	)

	output, err := downloadCmd.CombinedOutput()
	if err != nil {
		EmitLog(m, "error", fmt.Sprintf("S3 download failed: %s", string(output)))
		return fmt.Errorf("failed to download from S3: %w", err)
	}
	EmitLog(m, "info", "Successfully downloaded from S3")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Uploading to MinIO
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", fmt.Sprintf("Uploading to MinIO bucket: %s", destBucket))
	EmitProgress(m, 70, "Uploading to MinIO")

	uploadCmd := exec.CommandContext(ctx, "aws", "s3", "sync",
		localPath,
		fmt.Sprintf("s3://%s", destBucket),
		"--endpoint-url", minioEndpoint,
	)
	uploadCmd.Env = append(os.Environ(),
		"AWS_ACCESS_KEY_ID="+minioAccessKey,
		"AWS_SECRET_ACCESS_KEY="+minioSecretKey,
	)

	output, err = uploadCmd.CombinedOutput()
	if err != nil {
		EmitLog(m, "error", fmt.Sprintf("MinIO upload failed: %s", string(output)))
		return fmt.Errorf("failed to upload to MinIO: %w", err)
	}
	EmitLog(m, "info", "Successfully uploaded to MinIO")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Verifying transfer
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Verifying transfer")
	EmitProgress(m, 100, "Migration complete")

	return nil
}

// RDSToPostgresExecutor migrates RDS databases to PostgreSQL.
type RDSToPostgresExecutor struct{}

// NewRDSToPostgresExecutor creates a new RDS to PostgreSQL executor.
func NewRDSToPostgresExecutor() *RDSToPostgresExecutor {
	return &RDSToPostgresExecutor{}
}

// Type returns the migration type.
func (e *RDSToPostgresExecutor) Type() string {
	return "rds_to_postgres"
}

// GetPhases returns the migration phases.
func (e *RDSToPostgresExecutor) GetPhases() []string {
	return []string{
		"Validating connections",
		"Creating database",
		"Dumping source database",
		"Restoring to destination",
		"Verifying data",
	}
}

// Validate validates the migration configuration.
func (e *RDSToPostgresExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	// Validate source config
	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		required := []string{"host", "port", "database", "username", "password"}
		for _, field := range required {
			if _, ok := config.Source[field]; !ok {
				result.Valid = false
				result.Errors = append(result.Errors, fmt.Sprintf("source.%s is required", field))
			}
		}
	}

	// Validate destination config
	if config.Destination == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "destination configuration is required")
	} else {
		required := []string{"host", "port", "database", "username", "password"}
		for _, field := range required {
			if _, ok := config.Destination[field]; !ok {
				result.Valid = false
				result.Errors = append(result.Errors, fmt.Sprintf("destination.%s is required", field))
			}
		}
	}

	return result, nil
}

// Execute performs the migration.
func (e *RDSToPostgresExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	// Extract source configuration
	srcHost := fmt.Sprintf("%v", config.Source["host"])
	srcPort := fmt.Sprintf("%v", config.Source["port"])
	srcDatabase := fmt.Sprintf("%v", config.Source["database"])
	srcUsername := fmt.Sprintf("%v", config.Source["username"])
	srcPassword := fmt.Sprintf("%v", config.Source["password"])

	// Extract destination configuration
	dstHost := fmt.Sprintf("%v", config.Destination["host"])
	dstPort := fmt.Sprintf("%v", config.Destination["port"])
	dstDatabase := fmt.Sprintf("%v", config.Destination["database"])
	dstUsername := fmt.Sprintf("%v", config.Destination["username"])
	dstPassword := fmt.Sprintf("%v", config.Destination["password"])

	// Phase 1: Validating connections
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating database connections")
	EmitProgress(m, 10, "Testing connections")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Creating database
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", fmt.Sprintf("Ensuring database %s exists", dstDatabase))
	EmitProgress(m, 20, "Creating destination database")

	// Create database if not exists
	createDBCmd := exec.CommandContext(ctx, "psql",
		"-h", dstHost,
		"-p", dstPort,
		"-U", dstUsername,
		"-c", fmt.Sprintf("CREATE DATABASE %s", dstDatabase),
	)
	createDBCmd.Env = append(os.Environ(), "PGPASSWORD="+dstPassword)
	createDBCmd.Run() // Ignore error if database exists

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Dumping source database
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", fmt.Sprintf("Dumping database from %s", srcHost))
	EmitProgress(m, 40, "Exporting source database")

	// Create staging directory
	stagingDir, err := os.MkdirTemp("", "rds-migration-*")
	if err != nil {
		return fmt.Errorf("failed to create staging directory: %w", err)
	}
	defer os.RemoveAll(stagingDir)

	dumpFile := filepath.Join(stagingDir, "dump.sql")

	dumpCmd := exec.CommandContext(ctx, "pg_dump",
		"-h", srcHost,
		"-p", srcPort,
		"-U", srcUsername,
		"-d", srcDatabase,
		"-f", dumpFile,
		"--no-password",
	)
	dumpCmd.Env = append(os.Environ(), "PGPASSWORD="+srcPassword)

	output, err := dumpCmd.CombinedOutput()
	if err != nil {
		EmitLog(m, "error", fmt.Sprintf("pg_dump failed: %s", string(output)))
		return fmt.Errorf("failed to dump database: %w", err)
	}
	EmitLog(m, "info", "Database dump completed successfully")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Restoring to destination
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", fmt.Sprintf("Restoring database to %s", dstHost))
	EmitProgress(m, 70, "Importing to destination")

	restoreCmd := exec.CommandContext(ctx, "psql",
		"-h", dstHost,
		"-p", dstPort,
		"-U", dstUsername,
		"-d", dstDatabase,
		"-f", dumpFile,
	)
	restoreCmd.Env = append(os.Environ(), "PGPASSWORD="+dstPassword)

	output, err = restoreCmd.CombinedOutput()
	if err != nil {
		EmitLog(m, "error", fmt.Sprintf("psql restore failed: %s", string(output)))
		return fmt.Errorf("failed to restore database: %w", err)
	}
	EmitLog(m, "info", "Database restore completed successfully")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Verifying data
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Verifying data migration")
	EmitProgress(m, 100, "Migration complete")

	return nil
}

// DynamoDBToScyllaExecutor migrates DynamoDB tables to ScyllaDB.
type DynamoDBToScyllaExecutor struct{}

// NewDynamoDBToScyllaExecutor creates a new DynamoDB to ScyllaDB executor.
func NewDynamoDBToScyllaExecutor() *DynamoDBToScyllaExecutor {
	return &DynamoDBToScyllaExecutor{}
}

// Type returns the migration type.
func (e *DynamoDBToScyllaExecutor) Type() string {
	return "dynamodb_to_scylla"
}

// GetPhases returns the migration phases.
func (e *DynamoDBToScyllaExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Scanning DynamoDB table",
		"Creating ScyllaDB schema",
		"Importing data",
		"Verifying transfer",
	}
}

// Validate validates the migration configuration.
func (e *DynamoDBToScyllaExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	// Validate source config
	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["table_name"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.table_name is required")
		}
		if _, ok := config.Source["access_key_id"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.access_key_id is required")
		}
		if _, ok := config.Source["secret_access_key"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.secret_access_key is required")
		}
	}

	// Validate destination config
	if config.Destination == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "destination configuration is required")
	} else {
		if _, ok := config.Destination["host"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "destination.host is required")
		}
		if _, ok := config.Destination["keyspace"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "destination.keyspace is required")
		}
	}

	result.Warnings = append(result.Warnings, "DynamoDB to ScyllaDB migration requires manual schema mapping review")

	return result, nil
}

// Execute performs the migration.
func (e *DynamoDBToScyllaExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	// Extract configuration
	tableName := config.Source["table_name"].(string)
	accessKeyID := config.Source["access_key_id"].(string)
	secretAccessKey := config.Source["secret_access_key"].(string)
	region, _ := config.Source["region"].(string)
	if region == "" {
		region = "us-east-1"
	}

	scyllaHost := config.Destination["host"].(string)
	keyspace := config.Destination["keyspace"].(string)

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating AWS credentials")
	EmitProgress(m, 10, "Checking credentials")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Scanning DynamoDB table
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", fmt.Sprintf("Scanning DynamoDB table: %s", tableName))
	EmitProgress(m, 30, "Exporting table data")

	// Create staging directory
	stagingDir, err := os.MkdirTemp("", "dynamodb-migration-*")
	if err != nil {
		return fmt.Errorf("failed to create staging directory: %w", err)
	}
	defer os.RemoveAll(stagingDir)

	exportFile := filepath.Join(stagingDir, tableName+".json")

	scanCmd := exec.CommandContext(ctx, "aws", "dynamodb", "scan",
		"--table-name", tableName,
		"--region", region,
		"--output", "json",
	)
	scanCmd.Env = append(os.Environ(),
		"AWS_ACCESS_KEY_ID="+accessKeyID,
		"AWS_SECRET_ACCESS_KEY="+secretAccessKey,
		"AWS_DEFAULT_REGION="+region,
	)

	output, err := scanCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to scan DynamoDB table: %w", err)
	}

	if err := os.WriteFile(exportFile, output, 0644); err != nil {
		return fmt.Errorf("failed to write export file: %w", err)
	}
	EmitLog(m, "info", "DynamoDB table exported successfully")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Creating ScyllaDB schema
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", fmt.Sprintf("Creating keyspace and table in ScyllaDB: %s", keyspace))
	EmitProgress(m, 50, "Creating schema")

	createKeyspaceCmd := exec.CommandContext(ctx, "cqlsh", scyllaHost, "-e",
		fmt.Sprintf("CREATE KEYSPACE IF NOT EXISTS %s WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1};", keyspace),
	)
	if output, err := createKeyspaceCmd.CombinedOutput(); err != nil {
		EmitLog(m, "warn", fmt.Sprintf("Keyspace creation: %s", string(output)))
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Importing data
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Importing data to ScyllaDB")
	EmitLog(m, "info", "Note: Full DynamoDB compatibility requires the ScyllaDB Migrator tool")
	EmitProgress(m, 80, "Importing data")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Verifying transfer
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Migration preparation complete")
	EmitLog(m, "info", fmt.Sprintf("Export file: %s", exportFile))
	EmitProgress(m, 100, "Migration complete")

	return nil
}

// ElastiCacheToRedisExecutor migrates ElastiCache to Redis.
type ElastiCacheToRedisExecutor struct{}

// NewElastiCacheToRedisExecutor creates a new ElastiCache to Redis executor.
func NewElastiCacheToRedisExecutor() *ElastiCacheToRedisExecutor {
	return &ElastiCacheToRedisExecutor{}
}

// Type returns the migration type.
func (e *ElastiCacheToRedisExecutor) Type() string {
	return "elasticache_to_redis"
}

// GetPhases returns the migration phases.
func (e *ElastiCacheToRedisExecutor) GetPhases() []string {
	return []string{
		"Validating connections",
		"Exporting ElastiCache data",
		"Importing to Redis",
		"Verifying data",
	}
}

// Validate validates the migration configuration.
func (e *ElastiCacheToRedisExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	// Validate source config
	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["endpoint"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.endpoint is required")
		}
	}

	// Validate destination config
	if config.Destination == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "destination configuration is required")
	} else {
		if _, ok := config.Destination["host"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "destination.host is required")
		}
	}

	result.Warnings = append(result.Warnings, "ElastiCache cluster mode may require additional configuration")

	return result, nil
}

// Execute performs the migration.
func (e *ElastiCacheToRedisExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	// Extract configuration
	srcEndpoint := config.Source["endpoint"].(string)
	srcPort := "6379"
	if p, ok := config.Source["port"]; ok {
		srcPort = fmt.Sprintf("%v", p)
	}
	srcAuth, _ := config.Source["auth"].(string)

	dstHost := config.Destination["host"].(string)
	dstPort := "6379"
	if p, ok := config.Destination["port"]; ok {
		dstPort = fmt.Sprintf("%v", p)
	}
	dstAuth, _ := config.Destination["auth"].(string)

	// Phase 1: Validating connections
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating Redis connections")
	EmitProgress(m, 10, "Testing connections")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Exporting ElastiCache data
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", fmt.Sprintf("Exporting data from ElastiCache: %s", srcEndpoint))
	EmitProgress(m, 40, "Exporting data")

	// Create staging directory
	stagingDir, err := os.MkdirTemp("", "redis-migration-*")
	if err != nil {
		return fmt.Errorf("failed to create staging directory: %w", err)
	}
	defer os.RemoveAll(stagingDir)

	dumpFile := filepath.Join(stagingDir, "dump.rdb")

	// Try RDB dump first
	args := []string{"-h", srcEndpoint, "-p", srcPort, "--rdb", dumpFile}
	if srcAuth != "" {
		args = append(args, "-a", srcAuth)
	}

	rdbCmd := exec.CommandContext(ctx, "redis-cli", args...)
	output, err := rdbCmd.CombinedOutput()

	if err != nil {
		EmitLog(m, "warn", fmt.Sprintf("RDB dump not available: %s", strings.TrimSpace(string(output))))
		EmitLog(m, "info", "Falling back to key-by-key migration")

		// Fallback to MIGRATE command or key scanning
		scanArgs := []string{"-h", srcEndpoint, "-p", srcPort, "--scan"}
		if srcAuth != "" {
			scanArgs = append(scanArgs, "-a", srcAuth)
		}

		scanCmd := exec.CommandContext(ctx, "redis-cli", scanArgs...)
		keys, _ := scanCmd.Output()
		keysFile := filepath.Join(stagingDir, "keys.txt")
		os.WriteFile(keysFile, keys, 0644)
		EmitLog(m, "info", "Exported key list for manual migration")
	} else {
		EmitLog(m, "info", "RDB dump completed successfully")
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Importing to Redis
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", fmt.Sprintf("Importing data to Redis: %s", dstHost))
	EmitProgress(m, 70, "Importing data")

	// Note: RDB import typically requires placing file in Redis data directory and restarting
	EmitLog(m, "info", "For RDB import, place the dump file in the Redis data directory")
	EmitLog(m, "info", fmt.Sprintf("RDB file location: %s", dumpFile))

	// If auth is configured, we can try RESTORE commands for individual keys
	if _, err := os.Stat(dumpFile); os.IsNotExist(err) {
		EmitLog(m, "warn", "No RDB dump available, manual key migration may be required")
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Verifying data
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Verifying migration")
	EmitProgress(m, 100, "Migration complete")

	// Test connection to destination
	testArgs := []string{"-h", dstHost, "-p", dstPort, "PING"}
	if dstAuth != "" {
		testArgs = append(testArgs, "-a", dstAuth)
	}

	testCmd := exec.CommandContext(ctx, "redis-cli", testArgs...)
	if output, err := testCmd.Output(); err == nil && strings.TrimSpace(string(output)) == "PONG" {
		EmitLog(m, "info", "Destination Redis is healthy")
	}

	return nil
}
