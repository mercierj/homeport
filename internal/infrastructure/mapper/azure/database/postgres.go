// Package database provides mappers for Azure database services.
package database

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudexit/cloudexit/internal/domain/mapper"
	"github.com/cloudexit/cloudexit/internal/domain/resource"
)

// PostgresMapper converts Azure Database for PostgreSQL to PostgreSQL containers.
type PostgresMapper struct {
	*mapper.BaseMapper
}

// NewPostgresMapper creates a new Azure PostgreSQL mapper.
func NewPostgresMapper() *PostgresMapper {
	return &PostgresMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeAzurePostgres, nil),
	}
}

// Map converts an Azure Database for PostgreSQL to a PostgreSQL service.
func (m *PostgresMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	serverName := res.GetConfigString("name")
	if serverName == "" {
		serverName = res.Name
	}

	version := res.GetConfigString("version")
	if version == "" {
		version = "16"
	}

	result := mapper.NewMappingResult("postgres")
	svc := result.DockerService

	svc.Image = fmt.Sprintf("postgres:%s-alpine", version)
	svc.Environment = map[string]string{
		"POSTGRES_DB":       "azure_db",
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
		"cloudexit.source": "azurerm_postgresql_flexible_server",
		"cloudexit.engine": "postgres",
		"cloudexit.server": serverName,
	}

	migrationScript := m.generateMigrationScript(serverName)
	result.AddScript("migrate_azure_postgres.sh", []byte(migrationScript))

	if res.GetConfigBool("high_availability_enabled") {
		result.AddWarning("High availability is enabled. Consider setting up PostgreSQL streaming replication.")
	}

	result.AddManualStep("Update database credentials in docker-compose.yml")
	result.AddManualStep("Export data from Azure PostgreSQL using pg_dump")
	result.AddManualStep("Import data using pg_restore")

	return result, nil
}

func (m *PostgresMapper) generateMigrationScript(serverName string) string {
	return fmt.Sprintf(`#!/bin/bash
# Azure PostgreSQL Migration Script
set -e

echo "Azure PostgreSQL Migration"
echo "=========================="
echo "Server: %s"

AZURE_HOST="${AZURE_HOST:-%s.postgres.database.azure.com}"
AZURE_USER="${AZURE_USER:-postgres}"
AZURE_DB="${AZURE_DB:-azure_db}"

echo "Step 1: Create dump from Azure PostgreSQL"
pg_dump -h "$AZURE_HOST" -U "$AZURE_USER" -d "$AZURE_DB" -F c -f "/tmp/azure_dump.backup"

echo "Step 2: Restore to local PostgreSQL"
pg_restore -h localhost -U postgres -d azure_db -c -F c "/tmp/azure_dump.backup"

echo "Migration complete!"
`, serverName, serverName)
}
