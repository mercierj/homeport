// Package database provides mappers for GCP database services.
package database

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
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
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD-SHELL", "pg_isready -U postgres"},
		Interval: 10 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  5,
	}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"cloudexit.source":   "google_sql_database_instance",
		"cloudexit.engine":   "postgres",
		"cloudexit.instance": instanceName,
	}

	migrationScript := m.generatePostgresMigrationScript(instanceName)
	result.AddScript("migrate_cloudsql.sh", []byte(migrationScript))

	result.AddManualStep("Update database credentials in docker-compose.yml")
	result.AddManualStep("Use Cloud SQL Proxy or direct export to migrate data")

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
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "mysqladmin", "ping", "-h", "localhost"},
		Interval: 10 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  5,
	}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"cloudexit.source":   "google_sql_database_instance",
		"cloudexit.engine":   "mysql",
		"cloudexit.instance": instanceName,
	}

	migrationScript := m.generateMySQLMigrationScript(instanceName)
	result.AddScript("migrate_cloudsql.sh", []byte(migrationScript))

	result.AddManualStep("Update database credentials in docker-compose.yml")
	result.AddManualStep("Use Cloud SQL Proxy or gcloud sql export to migrate data")

	return result, nil
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
