// Package database provides mappers for AWS database services.
package database

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

// RDSMapper converts AWS RDS instances to PostgreSQL/MySQL containers.
type RDSMapper struct {
	*mapper.BaseMapper
}

// NewRDSMapper creates a new RDS to PostgreSQL/MySQL mapper.
func NewRDSMapper() *RDSMapper {
	return &RDSMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeRDSInstance, nil),
	}
}

// Map converts an RDS instance to a PostgreSQL or MySQL service.
func (m *RDSMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	engine := res.GetConfigString("engine")
	dbName := res.GetConfigString("db_name")
	if dbName == "" {
		dbName = res.Name
	}
	instanceClass := res.GetConfigString("instance_class")
	allocatedStorage := res.GetConfigInt("allocated_storage")

	// Determine the database engine and create appropriate service
	switch {
	case strings.Contains(engine, "postgres"):
		return m.createPostgresService(res, dbName, instanceClass, allocatedStorage, engine)
	case strings.Contains(engine, "mysql"), strings.Contains(engine, "mariadb"):
		return m.createMySQLService(res, dbName, engine, instanceClass, allocatedStorage)
	default:
		return nil, fmt.Errorf("unsupported RDS engine: %s", engine)
	}
}

// createPostgresService creates a PostgreSQL service.
func (m *RDSMapper) createPostgresService(res *resource.AWSResource, dbName, instanceClass string, allocatedStorage int, engine string) (*mapper.MappingResult, error) {
	engineVersion := res.GetConfigString("engine_version")
	if engineVersion == "" {
		engineVersion = "16"
	} else {
		// Extract major version
		parts := strings.Split(engineVersion, ".")
		if len(parts) > 0 {
			engineVersion = parts[0]
		}
	}

	// Create result using new API
	result := mapper.NewMappingResult("postgres")
	svc := result.DockerService

	// Configure PostgreSQL service
	svc.Image = fmt.Sprintf("postgres:%s-alpine", engineVersion)
	svc.Environment = map[string]string{
		"POSTGRES_DB":       dbName,
		"POSTGRES_USER":     "postgres",
		"POSTGRES_PASSWORD": "changeme",
		"PGDATA":            "/var/lib/postgresql/data/pgdata",
	}
	svc.Ports = []string{"5432:5432"}
	svc.Volumes = []string{
		"./data/postgres:/var/lib/postgresql/data",
	}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD-SHELL", "pg_isready -U postgres"},
		Interval: 10 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  5,
	}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"homeport.source":   "aws_db_instance",
		"homeport.engine":   "postgres",
		"homeport.database": dbName,
	}

	// Note: Resources field has been removed from the new API
	// Resource limits should be handled at the Docker/Compose level if needed

	// Add PostgreSQL configuration file
	pgConfig := m.generatePostgresConfig(res, allocatedStorage)
	result.AddConfig("config/postgres/postgresql.conf", []byte(pgConfig))

	// Handle backup configuration
	backupRetention := res.GetConfigInt("backup_retention_period")
	if backupRetention > 0 {
		m.addPostgresBackupService(res, result, dbName, backupRetention)
	}

	// Handle parameter groups
	if paramGroup := res.GetConfigString("parameter_group_name"); paramGroup != "" {
		result.AddWarning(fmt.Sprintf("Parameter group '%s' detected. Review and apply custom parameters in the database config file.", paramGroup))
	}

	// Generate migration script
	migrationScript := m.generatePostgresMigrationScript(res, dbName)
	result.AddScript("migrate_database.sh", []byte(migrationScript))

	// Add warnings for RDS-specific features
	if res.GetConfigBool("multi_az") {
		result.AddWarning("Multi-AZ deployment detected. Consider setting up database replication manually for high availability.")
	}

	if res.GetConfigBool("publicly_accessible") {
		result.AddWarning("Database is publicly accessible in AWS. Ensure proper firewall rules in your self-hosted environment.")
	}

	if res.GetConfigBool("storage_encrypted") {
		result.AddWarning("Storage encryption is enabled in RDS. Configure encryption at rest for your self-hosted database.")
	}

	// Add manual steps
	result.AddManualStep("Review and update database credentials in docker-compose.yml")
	result.AddManualStep("Import existing database dump if migrating from AWS RDS")
	result.AddManualStep("Configure database backups according to your retention policy")

	return result, nil
}

// createMySQLService creates a MySQL or MariaDB service.
func (m *RDSMapper) createMySQLService(res *resource.AWSResource, dbName, engine, instanceClass string, allocatedStorage int) (*mapper.MappingResult, error) {
	engineVersion := res.GetConfigString("engine_version")
	imageName := "mysql"
	defaultVersion := "8.0"

	if strings.Contains(engine, "mariadb") {
		imageName = "mariadb"
		defaultVersion = "11"
	}

	if engineVersion == "" {
		engineVersion = defaultVersion
	} else {
		// Extract major.minor version
		parts := strings.Split(engineVersion, ".")
		if len(parts) >= 2 {
			engineVersion = parts[0] + "." + parts[1]
		}
	}

	// Create result using new API
	result := mapper.NewMappingResult(imageName)
	svc := result.DockerService

	// Configure MySQL service
	svc.Image = fmt.Sprintf("%s:%s", imageName, engineVersion)
	svc.Environment = map[string]string{
		"MYSQL_ROOT_PASSWORD": "changeme",
		"MYSQL_DATABASE":      dbName,
		"MYSQL_USER":          "appuser",
		"MYSQL_PASSWORD":      "changeme",
	}
	svc.Ports = []string{"3306:3306"}
	svc.Volumes = []string{
		"./data/mysql:/var/lib/mysql",
	}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "mysqladmin", "ping", "-h", "localhost"},
		Interval: 10 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  5,
	}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"homeport.source":   "aws_db_instance",
		"homeport.engine":   engine,
		"homeport.database": dbName,
	}

	// Add MySQL configuration file
	mysqlConfig := m.generateMySQLConfig(res, allocatedStorage)
	result.AddConfig("config/mysql/my.cnf", []byte(mysqlConfig))

	// Handle backup configuration
	backupRetention := res.GetConfigInt("backup_retention_period")
	if backupRetention > 0 {
		backupScript := m.generateMySQLBackupScript(dbName, backupRetention)
		result.AddScript("backup_mysql.sh", []byte(backupScript))
		result.AddManualStep("Set up a cron job to run the MySQL backup script daily")
	}

	// Handle parameter groups
	if paramGroup := res.GetConfigString("parameter_group_name"); paramGroup != "" {
		result.AddWarning(fmt.Sprintf("Parameter group '%s' detected. Review and apply custom parameters in the database config file.", paramGroup))
	}

	// Generate migration script
	migrationScript := m.generateMySQLMigrationScript(res, dbName)
	result.AddScript("migrate_database.sh", []byte(migrationScript))

	// Add warnings for RDS-specific features
	if res.GetConfigBool("multi_az") {
		result.AddWarning("Multi-AZ deployment detected. Consider setting up database replication manually for high availability.")
	}

	if res.GetConfigBool("publicly_accessible") {
		result.AddWarning("Database is publicly accessible in AWS. Ensure proper firewall rules in your self-hosted environment.")
	}

	if res.GetConfigBool("storage_encrypted") {
		result.AddWarning("Storage encryption is enabled in RDS. Configure encryption at rest for your self-hosted database.")
	}

	// Add manual steps
	result.AddManualStep("Review and update database credentials in docker-compose.yml")
	result.AddManualStep("Import existing database dump if migrating from AWS RDS")
	result.AddManualStep("Configure database backups according to your retention policy")

	return result, nil
}

// addPostgresBackupService adds a backup service for PostgreSQL.
func (m *RDSMapper) addPostgresBackupService(res *resource.AWSResource, result *mapper.MappingResult, dbName string, retentionDays int) {
	// Note: Since we can only have one DockerService, we'll provide the backup service as a separate config
	backupServiceConfig := fmt.Sprintf(`# PostgreSQL Backup Service
# Add this to your docker-compose.yml services section:

postgres-backup:
  image: prodrigestivill/postgres-backup-local:16-alpine
  environment:
    POSTGRES_HOST: postgres
    POSTGRES_DB: %s
    POSTGRES_USER: postgres
    POSTGRES_PASSWORD: changeme
    SCHEDULE: "@daily"
    BACKUP_KEEP_DAYS: "%d"
    BACKUP_KEEP_WEEKS: "4"
    BACKUP_KEEP_MONTHS: "6"
    HEALTHCHECK_PORT: "8080"
  volumes:
    - ./backups/postgres:/backups
  depends_on:
    - postgres
  networks:
    - homeport
  labels:
    homeport.service: backup
    homeport.database: %s
`, dbName, retentionDays, dbName)

	result.AddConfig("config/postgres/backup-service.yml", []byte(backupServiceConfig))
	result.AddManualStep("Add the PostgreSQL backup service from config/postgres/backup-service.yml to docker-compose.yml")
}

// generatePostgresConfig creates a PostgreSQL configuration file.
func (m *RDSMapper) generatePostgresConfig(res *resource.AWSResource, allocatedStorage int) string {
	// Calculate shared_buffers as 25% of allocated storage (rough estimate)
	sharedBuffers := allocatedStorage * 256 / 4 // MB

	config := fmt.Sprintf(`# PostgreSQL Configuration
# Generated from RDS instance settings

# Memory Settings
shared_buffers = %dMB
effective_cache_size = %dMB
maintenance_work_mem = 256MB
work_mem = 16MB

# Connection Settings
max_connections = 100

# WAL Settings
wal_buffers = 16MB
min_wal_size = 1GB
max_wal_size = 4GB

# Query Tuning
random_page_cost = 1.1
effective_io_concurrency = 200

# Logging
log_destination = 'stderr'
logging_collector = on
log_directory = 'log'
log_filename = 'postgresql-%%Y-%%m-%%d_%%H%%M%%S.log'
log_line_prefix = '%%t [%%p]: [%%l-1] user=%%u,db=%%d,app=%%a,client=%%h '
log_timezone = 'UTC'

# Localization
timezone = 'UTC'
lc_messages = 'en_US.utf8'
lc_monetary = 'en_US.utf8'
lc_numeric = 'en_US.utf8'
lc_time = 'en_US.utf8'
`, sharedBuffers, sharedBuffers*4)

	return config
}

// generateMySQLConfig creates a MySQL configuration file.
func (m *RDSMapper) generateMySQLConfig(res *resource.AWSResource, allocatedStorage int) string {
	// Calculate innodb_buffer_pool_size as 70% of allocated storage
	bufferPoolSize := allocatedStorage * 1024 * 70 / 100 // MB to KB

	config := fmt.Sprintf(`[mysqld]
# MySQL Configuration
# Generated from RDS instance settings

# InnoDB Settings
innodb_buffer_pool_size = %dM
innodb_log_file_size = 512M
innodb_flush_log_at_trx_commit = 1
innodb_flush_method = O_DIRECT

# Connection Settings
max_connections = 151

# Query Cache (for MySQL 5.7 and earlier)
# query_cache_type = 1
# query_cache_size = 64M

# Logging
log_error = /var/log/mysql/error.log
slow_query_log = 1
slow_query_log_file = /var/log/mysql/slow-query.log
long_query_time = 2

# Character Set
character-set-server = utf8mb4
collation-server = utf8mb4_unicode_ci

# Binary Logging (for replication and point-in-time recovery)
log_bin = mysql-bin
binlog_format = ROW
expire_logs_days = 7

# General
sql_mode = STRICT_TRANS_TABLES,NO_ZERO_IN_DATE,NO_ZERO_DATE,ERROR_FOR_DIVISION_BY_ZERO,NO_ENGINE_SUBSTITUTION
`, bufferPoolSize)

	return config
}

// generatePostgresMigrationScript creates a PostgreSQL migration script.
func (m *RDSMapper) generatePostgresMigrationScript(res *resource.AWSResource, dbName string) string {
	script := fmt.Sprintf(`#!/bin/bash
# PostgreSQL Migration Script
# Migrates data from AWS RDS to local PostgreSQL

set -e

echo "PostgreSQL Migration Script"
echo "==========================="

# Variables
RDS_HOST="${RDS_HOST:-your-rds-instance.region.rds.amazonaws.com}"
RDS_USER="${RDS_USER:-postgres}"
RDS_DB="%s"
LOCAL_HOST="localhost"
LOCAL_USER="postgres"

echo "Step 1: Create dump from RDS"
echo "This will prompt for RDS password..."
pg_dump -h "$RDS_HOST" -U "$RDS_USER" -d "$RDS_DB" -F c -f "/tmp/${RDS_DB}_dump.backup"

echo "Step 2: Restore to local PostgreSQL"
echo "This will prompt for local password..."
pg_restore -h "$LOCAL_HOST" -U "$LOCAL_USER" -d "%s" -c -F c "/tmp/${RDS_DB}_dump.backup"

echo "Migration complete!"
echo "Clean up dump file: rm /tmp/${RDS_DB}_dump.backup"
`, dbName, dbName)

	return script
}

// generateMySQLMigrationScript creates a MySQL migration script.
func (m *RDSMapper) generateMySQLMigrationScript(res *resource.AWSResource, dbName string) string {
	script := fmt.Sprintf(`#!/bin/bash
# MySQL Migration Script
# Migrates data from AWS RDS to local MySQL

set -e

echo "MySQL Migration Script"
echo "====================="

# Variables
RDS_HOST="${RDS_HOST:-your-rds-instance.region.rds.amazonaws.com}"
RDS_USER="${RDS_USER:-admin}"
RDS_DB="%s"
LOCAL_HOST="localhost"
LOCAL_USER="root"

echo "Step 1: Create dump from RDS"
echo "This will prompt for RDS password..."
mysqldump -h "$RDS_HOST" -u "$RDS_USER" -p --databases "$RDS_DB" \
  --single-transaction --routines --triggers --events \
  > "/tmp/${RDS_DB}_dump.sql"

echo "Step 2: Restore to local MySQL"
echo "This will prompt for local password..."
mysql -h "$LOCAL_HOST" -u "$LOCAL_USER" -p < "/tmp/${RDS_DB}_dump.sql"

echo "Migration complete!"
echo "Clean up dump file: rm /tmp/${RDS_DB}_dump.sql"
`, dbName)

	return script
}

// generateMySQLBackupScript creates a backup script for MySQL.
func (m *RDSMapper) generateMySQLBackupScript(dbName string, retentionDays int) string {
	script := fmt.Sprintf(`#!/bin/bash
# MySQL Backup Script
# Automated daily backup with rotation

set -e

BACKUP_DIR="./backups/mysql"
DB_NAME="%s"
RETENTION_DAYS=%d

# Create backup directory
mkdir -p "$BACKUP_DIR"

# Create backup filename with timestamp
BACKUP_FILE="$BACKUP_DIR/${DB_NAME}_$(date +%%Y%%m%%d_%%H%%M%%S).sql.gz"

echo "Creating backup: $BACKUP_FILE"

# Create backup
docker exec mysql mysqldump -u root -pchangeme --databases "$DB_NAME" \
  --single-transaction --routines --triggers --events | gzip > "$BACKUP_FILE"

# Remove old backups
echo "Removing backups older than $RETENTION_DAYS days..."
find "$BACKUP_DIR" -name "${DB_NAME}_*.sql.gz" -mtime +$RETENTION_DAYS -delete

echo "Backup complete!"
`, dbName, retentionDays)

	return script
}
