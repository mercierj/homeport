// Package database provides mappers for Azure database services.
package database

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

// MySQLMapper converts Azure Database for MySQL to MySQL containers.
type MySQLMapper struct {
	*mapper.BaseMapper
}

// NewMySQLMapper creates a new Azure MySQL mapper.
func NewMySQLMapper() *MySQLMapper {
	return &MySQLMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeAzureMySQL, nil),
	}
}

// Map converts an Azure Database for MySQL to a MySQL service.
func (m *MySQLMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	serverName := res.GetConfigString("name")
	if serverName == "" {
		serverName = res.Name
	}

	version := res.GetConfigString("version")
	if version == "" {
		version = "8.0"
	}

	result := mapper.NewMappingResult("mysql")
	svc := result.DockerService

	svc.Image = fmt.Sprintf("mysql:%s", version)
	svc.Environment = map[string]string{
		"MYSQL_ROOT_PASSWORD": "changeme",
		"MYSQL_DATABASE":      "azure_db",
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
		"homeport.source": "azurerm_mysql_flexible_server",
		"homeport.engine": "mysql",
		"homeport.server": serverName,
	}

	migrationScript := m.generateMigrationScript(serverName)
	result.AddScript("migrate_azure_mysql.sh", []byte(migrationScript))

	if res.GetConfigBool("high_availability_enabled") {
		result.AddWarning("High availability is enabled. Consider setting up MySQL replication.")
	}

	result.AddManualStep("Update database credentials in docker-compose.yml")
	result.AddManualStep("Export data from Azure MySQL using mysqldump")
	result.AddManualStep("Import data using mysql client")

	return result, nil
}

func (m *MySQLMapper) generateMigrationScript(serverName string) string {
	return fmt.Sprintf(`#!/bin/bash
# Azure MySQL Migration Script
set -e

echo "Azure MySQL Migration"
echo "====================="
echo "Server: %s"

AZURE_HOST="${AZURE_HOST:-%s.mysql.database.azure.com}"
AZURE_USER="${AZURE_USER:-admin}"
AZURE_DB="${AZURE_DB:-azure_db}"

echo "Step 1: Create dump from Azure MySQL"
mysqldump -h "$AZURE_HOST" -u "$AZURE_USER" -p --databases "$AZURE_DB" \
  --single-transaction --routines --triggers --events > "/tmp/azure_dump.sql"

echo "Step 2: Restore to local MySQL"
mysql -h localhost -u root -pchangeme < "/tmp/azure_dump.sql"

echo "Migration complete!"
`, serverName, serverName)
}
