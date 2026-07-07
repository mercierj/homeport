// Package database provides mappers for GCP database services.
package database

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
	"github.com/homeport/homeport/internal/infrastructure/mapper/shared/datarunbook"
)

// CloudSQLMapper converts GCP CloudSQL instances to PostgreSQL/MySQL containers.
type CloudSQLMapper struct {
	*mapper.BaseMapper
}

// NewCloudSQLMapper creates a new CloudSQL mapper.
func NewCloudSQLMapper() *CloudSQLMapper {
	return &CloudSQLMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeCloudSQL, nil),
	}
}

// Map converts a CloudSQL instance to a PostgreSQL or MySQL service.
func (m *CloudSQLMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	databaseVersion := res.GetConfigString("database_version")
	instanceName := res.GetConfigString("name")
	if instanceName == "" {
		instanceName = res.Name
	}

	switch {
	case strings.HasPrefix(databaseVersion, "POSTGRES"):
		return m.createPostgresService(res, instanceName, databaseVersion)
	case strings.HasPrefix(databaseVersion, "MYSQL"):
		return m.createMySQLService(res, instanceName, databaseVersion)
	default:
		return nil, fmt.Errorf("unsupported CloudSQL database version: %s", databaseVersion)
	}
}

func (m *CloudSQLMapper) createPostgresService(res *resource.AWSResource, instanceName, databaseVersion string) (*mapper.MappingResult, error) {
	version := "16"
	if strings.HasPrefix(databaseVersion, "POSTGRES_") {
		version = strings.TrimPrefix(databaseVersion, "POSTGRES_")
	}

	result := mapper.NewMappingResult("postgres")
	svc := result.DockerService

	svc.Image = fmt.Sprintf("postgres:%s-alpine", version)
	svc.Environment = map[string]string{
		"POSTGRES_DB":       "cloudsql_db",
		"POSTGRES_USER":     "postgres",
		"POSTGRES_PASSWORD": "changeme",
		"PGDATA":            "/var/lib/postgresql/data/pgdata",
	}
	svc.Ports = []string{"5432:5432"}
	svc.Volumes = []string{"./data/postgres:/var/lib/postgresql/data"}
	svc.Networks = []string{"homeport"}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD-SHELL", "pg_isready -U postgres"},
		Interval: 10 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  5,
	}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"homeport.source":   "google_sql_database_instance",
		"homeport.engine":   "postgres",
		"homeport.instance": instanceName,
	}

	migrationScript := m.generatePostgresMigrationScript(instanceName)
	result.AddScript("migrate_cloudsql.sh", []byte(migrationScript))
	m.decorateCloudSQLResult(result, instanceName, "postgres", "postgres://postgres:changeme@postgres:5432/cloudsql_db")
	for _, step := range datarunbook.SQL("postgres", "cloudsql_db", "migrate_cloudsql.sh") {
		result.AddRunbookStep(step)
	}

	return result, nil
}

func (m *CloudSQLMapper) createMySQLService(res *resource.AWSResource, instanceName, databaseVersion string) (*mapper.MappingResult, error) {
	version := "8.0"
	if strings.HasPrefix(databaseVersion, "MYSQL_") {
		v := strings.TrimPrefix(databaseVersion, "MYSQL_")
		version = strings.ReplaceAll(v, "_", ".")
	}

	result := mapper.NewMappingResult("mysql")
	svc := result.DockerService

	svc.Image = fmt.Sprintf("mysql:%s", version)
	svc.Environment = map[string]string{
		"MYSQL_ROOT_PASSWORD": "changeme",
		"MYSQL_DATABASE":      "cloudsql_db",
		"MYSQL_USER":          "appuser",
		"MYSQL_PASSWORD":      "changeme",
	}
	svc.Ports = []string{"3306:3306"}
	svc.Volumes = []string{"./data/mysql:/var/lib/mysql"}
	svc.Networks = []string{"homeport"}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "mysqladmin", "ping", "-h", "localhost"},
		Interval: 10 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  5,
	}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"homeport.source":   "google_sql_database_instance",
		"homeport.engine":   "mysql",
		"homeport.instance": instanceName,
	}

	migrationScript := m.generateMySQLMigrationScript(instanceName)
	result.AddScript("migrate_cloudsql.sh", []byte(migrationScript))
	m.decorateCloudSQLResult(result, instanceName, "mysql", "mysql://appuser:changeme@mysql:3306/cloudsql_db")
	for _, step := range datarunbook.SQL("mysql", "cloudsql_db", "migrate_cloudsql.sh") {
		result.AddRunbookStep(step)
	}

	return result, nil
}

func (m *CloudSQLMapper) decorateCloudSQLResult(result *mapper.MappingResult, instanceName, engine, databaseURL string) {
	result.AddConfig("config/cloud-sql/app-change.env", []byte(m.generateAppChangeConfig(instanceName, databaseURL)))
	result.AddConfig("config/cloud-sql/database-report.yaml", []byte(m.generateDatabaseReport(instanceName, engine)))
	result.AddScript("backup_cloud_sql.sh", []byte(m.generateBackupScript(instanceName, engine)))
	result.AddScript("validate_cloud_sql.sh", []byte(m.generateValidateScript(instanceName, engine)))
	for _, step := range cloudSQLRunbook(instanceName) {
		result.AddRunbookStep(step)
	}
}

func (m *CloudSQLMapper) generateAppChangeConfig(instanceName, databaseURL string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=generated_patch
SOURCE_CLOUD_SQL_INSTANCE=%s
TARGET_DATABASE_URL=%s
TARGET_DATABASE_NAME=cloudsql_db
`, instanceName, databaseURL)
}

func (m *CloudSQLMapper) generateDatabaseReport(instanceName, engine string) string {
	return fmt.Sprintf(`source: google_sql_database_instance
instance: %s
engine: %s
database: cloudsql_db
target: docker
`, instanceName, engine)
}

func (m *CloudSQLMapper) generatePostgresMigrationScript(instanceName string) string {
	return fmt.Sprintf(`#!/bin/bash
# CloudSQL PostgreSQL Migration Script
set -e

echo "CloudSQL PostgreSQL Migration"
echo "============================="
echo "Instance: %s"

echo "Option 1: Using Cloud SQL Proxy"
echo "  ./cloud_sql_proxy -instances=PROJECT:REGION:INSTANCE=tcp:5433"
echo "  pg_dump -h localhost -p 5433 -U postgres -d cloudsql_db -F c -f dump.backup"
echo "  pg_restore -h localhost -U postgres -d cloudsql_db -F c dump.backup"

echo "Option 2: Using gcloud sql export"
echo "  gcloud sql export sql INSTANCE gs://BUCKET/export.sql --database=cloudsql_db"
echo "  gsutil cp gs://BUCKET/export.sql ."
echo "  psql -h localhost -U postgres -d cloudsql_db < export.sql"
`, instanceName)
}

func (m *CloudSQLMapper) generateMySQLMigrationScript(instanceName string) string {
	return fmt.Sprintf(`#!/bin/bash
# CloudSQL MySQL Migration Script
set -e

echo "CloudSQL MySQL Migration"
echo "========================"
echo "Instance: %s"

echo "Option 1: Using Cloud SQL Proxy"
echo "  ./cloud_sql_proxy -instances=PROJECT:REGION:INSTANCE=tcp:3307"
echo "  mysqldump -h localhost -P 3307 -u root -p cloudsql_db > dump.sql"
echo "  mysql -h localhost -u root -p cloudsql_db < dump.sql"

echo "Option 2: Using gcloud sql export"
echo "  gcloud sql export sql INSTANCE gs://BUCKET/export.sql --database=cloudsql_db"
echo "  gsutil cp gs://BUCKET/export.sql ."
echo "  mysql -h localhost -u root -p cloudsql_db < export.sql"
`, instanceName)
}

func (m *CloudSQLMapper) generateBackupScript(instanceName, engine string) string {
	dataDir := "postgres"
	if engine == "mysql" {
		dataDir = "mysql"
	}
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/cloud-sql-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/cloud-sql data/%s
echo "$archive"
`, sanitizeCloudSQLName(instanceName), dataDir)
}

func (m *CloudSQLMapper) generateValidateScript(instanceName, engine string) string {
	if engine == "mysql" {
		return fmt.Sprintf(`#!/bin/sh
set -eu
test -s config/cloud-sql/app-change.env
mysqladmin ping -h mysql -u appuser -pchangeme
echo "Cloud SQL instance %s validated on MySQL"
`, instanceName)
	}
	return fmt.Sprintf(`#!/bin/sh
set -eu
test -s config/cloud-sql/app-change.env
pg_isready -h postgres -U postgres -d cloudsql_db
echo "Cloud SQL instance %s validated on PostgreSQL"
`, instanceName)
}

func cloudSQLRunbook(instanceName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "sql-database", "source": "google_sql_database_instance", "instance": instanceName}
	return []domainrunbook.Step{
		cloudSQLStep("discover-cloud-sql-instance", "Discover Cloud SQL instance", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "-c", fmt.Sprintf("gcloud sql instances describe %q --format=json", instanceName)}, "source database configuration is exported", metadata),
		cloudSQLStep("provision-cloud-sql-target", "Provision SQL target", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s config/cloud-sql/database-report.yaml"}, "target database config is rendered", metadata),
		cloudSQLStep("migrate-cloud-sql-data", "Migrate Cloud SQL data", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_cloudsql.sh"}, "Cloud SQL export/import script runs", metadata),
		cloudSQLStep("validate-cloud-sql-target", "Validate SQL target", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_cloud_sql.sh"}, "target database responds", metadata),
		cloudSQLStep("backup-cloud-sql-target", "Backup SQL target", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_cloud_sql.sh"}, "target database archive is produced", metadata),
		cloudSQLStep("cutover-cloud-sql-client", "Cut over SQL clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "-c", "test -s config/cloud-sql/app-change.env"}, "generated patch points clients at target database", metadata),
		cloudSQLStep("rollback-cloud-sql-source", "Keep Cloud SQL as rollback", "Rollback", domainrunbook.StepTypeRollback, nil, "source Cloud SQL remains available until validation passes", metadata),
	}
}

func cloudSQLStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}

func sanitizeCloudSQLName(value string) string {
	value = strings.ToLower(value)
	value = strings.NewReplacer("/", "-", " ", "-", ":", "-").Replace(value)
	if value == "" {
		return "cloud-sql"
	}
	return value
}
