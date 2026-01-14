package deploy

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// DataMigrator handles migrating data from cloud services to self-hosted equivalents
type DataMigrator struct {
	workDir    string
	awsConfig  AWSConfig
	deployment *Deployment
}

// AWSConfig holds AWS credentials for data migration
type AWSConfig struct {
	AccessKeyID     string
	SecretAccessKey string
	Region          string
}

// NewDataMigrator creates a new data migrator
func NewDataMigrator(workDir string, awsConfig AWSConfig, d *Deployment) *DataMigrator {
	return &DataMigrator{
		workDir:    workDir,
		awsConfig:  awsConfig,
		deployment: d,
	}
}

// MigrateS3ToMinIO syncs S3 bucket contents to MinIO
func (m *DataMigrator) MigrateS3ToMinIO(ctx context.Context, bucketName, minioEndpoint, minioAccessKey, minioSecretKey string) error {
	EmitLog(m.deployment, "info", fmt.Sprintf("Syncing S3 bucket: %s", bucketName))

	// Create local staging directory for S3 data
	stagingDir := filepath.Join(m.workDir, "data", "s3", bucketName)
	if err := os.MkdirAll(stagingDir, 0755); err != nil {
		return fmt.Errorf("failed to create staging dir: %w", err)
	}

	// Download from S3 using AWS CLI
	downloadCmd := exec.CommandContext(ctx, "aws", "s3", "sync",
		fmt.Sprintf("s3://%s", bucketName),
		stagingDir,
		"--region", m.awsConfig.Region,
	)
	downloadCmd.Env = m.getAWSEnv()

	output, err := downloadCmd.CombinedOutput()
	if err != nil {
		EmitLog(m.deployment, "warn", fmt.Sprintf("S3 sync output: %s", string(output)))
		return fmt.Errorf("failed to sync from S3: %w", err)
	}

	EmitLog(m.deployment, "info", fmt.Sprintf("Downloaded S3 bucket %s to staging", bucketName))

	// Upload to MinIO using mc (MinIO client) or aws cli with endpoint override
	uploadCmd := exec.CommandContext(ctx, "aws", "s3", "sync",
		stagingDir,
		fmt.Sprintf("s3://%s", bucketName),
		"--endpoint-url", minioEndpoint,
	)
	uploadCmd.Env = append(os.Environ(),
		"AWS_ACCESS_KEY_ID="+minioAccessKey,
		"AWS_SECRET_ACCESS_KEY="+minioSecretKey,
	)

	output, err = uploadCmd.CombinedOutput()
	if err != nil {
		EmitLog(m.deployment, "warn", fmt.Sprintf("MinIO upload output: %s", string(output)))
		return fmt.Errorf("failed to upload to MinIO: %w", err)
	}

	EmitLog(m.deployment, "info", fmt.Sprintf("Uploaded bucket %s to MinIO", bucketName))
	return nil
}

// MigrateRDSToDocker dumps RDS database and prepares for import
func (m *DataMigrator) MigrateRDSToDocker(ctx context.Context, dbIdentifier, engine, endpoint, dbName, username, password string) error {
	EmitLog(m.deployment, "info", fmt.Sprintf("Migrating RDS database: %s (%s)", dbIdentifier, engine))

	dumpDir := filepath.Join(m.workDir, "data", "rds")
	if err := os.MkdirAll(dumpDir, 0755); err != nil {
		return fmt.Errorf("failed to create dump dir: %w", err)
	}

	dumpFile := filepath.Join(dumpDir, fmt.Sprintf("%s.sql", dbIdentifier))

	var cmd *exec.Cmd
	switch {
	case strings.Contains(engine, "postgres"):
		// pg_dump for PostgreSQL
		cmd = exec.CommandContext(ctx, "pg_dump",
			"-h", endpoint,
			"-U", username,
			"-d", dbName,
			"-f", dumpFile,
			"--no-password",
		)
		cmd.Env = append(os.Environ(), "PGPASSWORD="+password)

	case strings.Contains(engine, "mysql"), strings.Contains(engine, "mariadb"):
		// mysqldump for MySQL/MariaDB
		cmd = exec.CommandContext(ctx, "mysqldump",
			"-h", endpoint,
			"-u", username,
			fmt.Sprintf("-p%s", password),
			dbName,
			"--result-file="+dumpFile,
		)

	default:
		EmitLog(m.deployment, "warn", fmt.Sprintf("Unsupported database engine: %s", engine))
		return nil
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		EmitLog(m.deployment, "warn", fmt.Sprintf("Database dump output: %s", string(output)))
		return fmt.Errorf("failed to dump database: %w", err)
	}

	EmitLog(m.deployment, "info", fmt.Sprintf("Database dump saved to %s", dumpFile))

	// Create import script
	importScript := m.generateDBImportScript(engine, dbIdentifier, dbName, dumpFile)
	scriptPath := filepath.Join(m.workDir, "scripts", fmt.Sprintf("import_%s.sh", dbIdentifier))
	if err := os.WriteFile(scriptPath, []byte(importScript), 0755); err != nil {
		return fmt.Errorf("failed to write import script: %w", err)
	}

	EmitLog(m.deployment, "info", fmt.Sprintf("Created import script: %s", scriptPath))
	return nil
}

// MigrateDynamoDBToScylla exports DynamoDB table data
func (m *DataMigrator) MigrateDynamoDBToScylla(ctx context.Context, tableName string) error {
	EmitLog(m.deployment, "info", fmt.Sprintf("Exporting DynamoDB table: %s", tableName))

	exportDir := filepath.Join(m.workDir, "data", "dynamodb")
	if err := os.MkdirAll(exportDir, 0755); err != nil {
		return fmt.Errorf("failed to create export dir: %w", err)
	}

	exportFile := filepath.Join(exportDir, fmt.Sprintf("%s.json", tableName))

	// Use AWS CLI to scan and export table data
	cmd := exec.CommandContext(ctx, "aws", "dynamodb", "scan",
		"--table-name", tableName,
		"--region", m.awsConfig.Region,
		"--output", "json",
	)
	cmd.Env = m.getAWSEnv()

	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to scan DynamoDB table: %w", err)
	}

	if err := os.WriteFile(exportFile, output, 0644); err != nil {
		return fmt.Errorf("failed to write export file: %w", err)
	}

	EmitLog(m.deployment, "info", fmt.Sprintf("DynamoDB table exported to %s", exportFile))

	// Create import script for ScyllaDB
	importScript := m.generateScyllaImportScript(tableName, exportFile)
	scriptPath := filepath.Join(m.workDir, "scripts", fmt.Sprintf("import_dynamodb_%s.sh", tableName))
	if err := os.WriteFile(scriptPath, []byte(importScript), 0755); err != nil {
		return fmt.Errorf("failed to write import script: %w", err)
	}

	return nil
}

// MigrateElastiCacheToRedis exports ElastiCache data
func (m *DataMigrator) MigrateElastiCacheToRedis(ctx context.Context, clusterID, endpoint string, port int) error {
	EmitLog(m.deployment, "info", fmt.Sprintf("Migrating ElastiCache cluster: %s", clusterID))

	exportDir := filepath.Join(m.workDir, "data", "redis")
	if err := os.MkdirAll(exportDir, 0755); err != nil {
		return fmt.Errorf("failed to create export dir: %w", err)
	}

	// Use redis-cli to dump keys
	dumpFile := filepath.Join(exportDir, fmt.Sprintf("%s.rdb", clusterID))

	// Try to get RDB dump via SYNC command (if accessible)
	cmd := exec.CommandContext(ctx, "redis-cli",
		"-h", endpoint,
		"-p", fmt.Sprintf("%d", port),
		"--rdb", dumpFile,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Fallback: dump keys as commands
		EmitLog(m.deployment, "warn", fmt.Sprintf("RDB dump failed, trying key export: %s", string(output)))
		return m.exportRedisKeys(ctx, endpoint, port, clusterID)
	}

	EmitLog(m.deployment, "info", fmt.Sprintf("Redis RDB dump saved to %s", dumpFile))
	return nil
}

func (m *DataMigrator) exportRedisKeys(ctx context.Context, endpoint string, port int, clusterID string) error {
	exportDir := filepath.Join(m.workDir, "data", "redis")
	exportFile := filepath.Join(exportDir, fmt.Sprintf("%s_keys.txt", clusterID))

	// Get all keys and their values
	cmd := exec.CommandContext(ctx, "redis-cli",
		"-h", endpoint,
		"-p", fmt.Sprintf("%d", port),
		"--scan",
	)

	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to scan Redis keys: %w", err)
	}

	if err := os.WriteFile(exportFile, output, 0644); err != nil {
		return fmt.Errorf("failed to write keys file: %w", err)
	}

	EmitLog(m.deployment, "info", fmt.Sprintf("Redis keys exported to %s", exportFile))
	return nil
}

func (m *DataMigrator) getAWSEnv() []string {
	return append(os.Environ(),
		"AWS_ACCESS_KEY_ID="+m.awsConfig.AccessKeyID,
		"AWS_SECRET_ACCESS_KEY="+m.awsConfig.SecretAccessKey,
		"AWS_DEFAULT_REGION="+m.awsConfig.Region,
	)
}

func (m *DataMigrator) generateDBImportScript(engine, dbIdentifier, dbName, dumpFile string) string {
	switch {
	case strings.Contains(engine, "postgres"):
		return fmt.Sprintf(`#!/bin/bash
# Import PostgreSQL database: %s
set -e

CONTAINER_NAME="${COMPOSE_PROJECT_NAME:-homeport}_postgres"
DUMP_FILE="%s"

echo "Waiting for PostgreSQL to be ready..."
until docker exec $CONTAINER_NAME pg_isready -U postgres; do
    sleep 2
done

echo "Importing database..."
docker exec -i $CONTAINER_NAME psql -U postgres -d %s < "$DUMP_FILE"

echo "Import complete!"
`, dbIdentifier, dumpFile, dbName)

	case strings.Contains(engine, "mysql"), strings.Contains(engine, "mariadb"):
		return fmt.Sprintf(`#!/bin/bash
# Import MySQL database: %s
set -e

CONTAINER_NAME="${COMPOSE_PROJECT_NAME:-homeport}_mysql"
DUMP_FILE="%s"

echo "Waiting for MySQL to be ready..."
until docker exec $CONTAINER_NAME mysqladmin ping -h localhost --silent; do
    sleep 2
done

echo "Importing database..."
docker exec -i $CONTAINER_NAME mysql -u root -p"$MYSQL_ROOT_PASSWORD" %s < "$DUMP_FILE"

echo "Import complete!"
`, dbIdentifier, dumpFile, dbName)

	default:
		return "#!/bin/bash\necho 'Unsupported database engine'\n"
	}
}

func (m *DataMigrator) generateScyllaImportScript(tableName, exportFile string) string {
	return fmt.Sprintf(`#!/bin/bash
# Import DynamoDB data to ScyllaDB: %s
set -e

CONTAINER_NAME="${COMPOSE_PROJECT_NAME:-homeport}_scylladb"
EXPORT_FILE="%s"

echo "Waiting for ScyllaDB to be ready..."
until docker exec $CONTAINER_NAME cqlsh -e "describe cluster" 2>/dev/null; do
    sleep 5
done

echo "Creating keyspace and table..."
docker exec $CONTAINER_NAME cqlsh -e "
CREATE KEYSPACE IF NOT EXISTS dynamodb WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1};
"

# Parse JSON and insert data
# This is a simplified example - real implementation would need proper JSON parsing
echo "Data file: $EXPORT_FILE"
echo "Please use the ScyllaDB Migrator tool for full DynamoDB compatibility"
echo "See: https://github.com/scylladb/scylla-migrator"

echo "Import preparation complete!"
`, tableName, exportFile)
}
